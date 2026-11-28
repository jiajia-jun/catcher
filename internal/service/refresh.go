package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"catcher/internal/domain/entity"
)

// RefreshStart 是一次刷新请求的受理结果
type RefreshStart struct {
	JobID          string
	Platforms      []entity.Platform
	AlreadyRunning bool
}

// PlatformResult 是单平台刷新的完整结果
// 事件与同步刷新共用同一份数据，保证两条路径的状态一致
type PlatformResult struct {
	Platform entity.Platform
	Status   entity.RunStatus
	Run      entity.FetchRun
	Issues   []entity.FetchIssue
	Error    *ErrorEvent
	// SkipReason 在 Status 为 skipped 时说明原因，目前只有 not_logged_in
	SkipReason string
}

type refreshJob struct {
	id        string
	platforms []entity.Platform
	cancel    context.CancelFunc
}

var jobSeq atomic.Int64

func newJobID(prefix string) string {
	return fmt.Sprintf("%s-%d-%d", prefix, time.Now().UnixMilli(), jobSeq.Add(1))
}

// StartRefresh 为每个平台启动独立刷新任务；同一平台同时只允许一个
// 请求上下文只用于受理，任务本身绑定可取消的独立 context
func (s *Service) StartRefresh(ctx context.Context, platforms []entity.Platform) (*RefreshStart, error) {
	_ = ctx
	if len(s.fetchers) == 0 {
		return nil, fmt.Errorf("没有可用的平台适配器")
	}
	// 「全部刷新」只包含已接入适配器的平台，其余平台跳过
	if len(platforms) == 0 {
		for _, p := range entity.AllPlatforms() {
			if _, ok := s.fetchers[p]; ok {
				platforms = append(platforms, p)
			}
		}
	}

	s.jobsMu.Lock()
	seen := make(map[entity.Platform]struct{}, len(platforms))
	var started []entity.Platform
	for _, p := range platforms {
		if !p.Valid() {
			s.jobsMu.Unlock()
			return nil, fmt.Errorf("未知平台 %q", p)
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		if _, busy := s.running[p]; busy {
			continue
		}
		started = append(started, p)
	}
	if len(started) == 0 {
		s.jobsMu.Unlock()
		return &RefreshStart{Platforms: platforms, AlreadyRunning: true}, nil
	}

	jobID := newJobID("refresh")
	jobCtx, cancel := context.WithCancel(context.Background())
	job := &refreshJob{id: jobID, platforms: started, cancel: cancel}
	s.refreshJobs[jobID] = job
	for _, p := range started {
		s.running[p] = jobID
	}
	s.jobsMu.Unlock()

	go s.runRefreshJob(jobCtx, job)
	return &RefreshStart{JobID: jobID, Platforms: started}, nil
}

// CancelRefresh 取消一次刷新任务；已完成的 job 返回错误
func (s *Service) CancelRefresh(jobID string) error {
	s.jobsMu.Lock()
	job, ok := s.refreshJobs[jobID]
	s.jobsMu.Unlock()
	if !ok {
		return fmt.Errorf("刷新任务不存在或已结束")
	}
	job.cancel()
	return nil
}

// CancelAll 取消所有刷新与扫码任务，应用退出时调用
func (s *Service) CancelAll() {
	s.jobsMu.Lock()
	jobs := make([]*refreshJob, 0, len(s.refreshJobs))
	for _, job := range s.refreshJobs {
		jobs = append(jobs, job)
	}
	logins := make([]*loginJob, 0, len(s.loginJobs))
	for _, job := range s.loginJobs {
		logins = append(logins, job)
	}
	s.jobsMu.Unlock()

	for _, job := range jobs {
		job.cancel()
	}
	for _, job := range logins {
		job.cancel()
	}
}

func (s *Service) runRefreshJob(ctx context.Context, job *refreshJob) {
	results := make([]PlatformResult, len(job.platforms))
	var wg sync.WaitGroup

	for i, p := range job.platforms {
		i, p := i, p
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = s.refreshPlatform(ctx, job.id, p)
		}()
	}
	wg.Wait()

	s.jobsMu.Lock()
	for _, p := range job.platforms {
		if s.running[p] == job.id {
			delete(s.running, p)
		}
	}
	delete(s.refreshJobs, job.id)
	s.jobsMu.Unlock()

	statuses := make(map[string]string, len(results))
	for _, result := range results {
		statuses[string(result.Platform)] = string(result.Status)
		s.emitRefreshFinished(job.id, result)
	}

	job.cancel()
	s.emitEvent("refresh.all-finished", map[string]any{
		"jobId":    job.id,
		"statuses": statuses,
	})
}

// RefreshSync 同步刷新给定平台并等待全部结束，供命令行使用
// platforms 为空时使用「参与刷新」勾选的平台；从未登录的平台标 skipped
func (s *Service) RefreshSync(ctx context.Context, platforms []entity.Platform) []PlatformResult {
	if len(platforms) == 0 {
		platforms = s.EnabledPlatforms()
	}

	seen := make(map[entity.Platform]struct{}, len(platforms))
	list := make([]entity.Platform, 0, len(platforms))
	for _, p := range platforms {
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		list = append(list, p)
	}

	states, err := s.loginStates(ctx)
	if err != nil {
		results := make([]PlatformResult, len(list))
		for i, p := range list {
			results[i] = PlatformResult{
				Platform: p,
				Status:   entity.RunFailed,
				Error:    &ErrorEvent{Code: "STORAGE", Message: "读取账号状态失败: " + err.Error()},
			}
		}
		return results
	}

	results := make([]PlatformResult, len(list))
	var wg sync.WaitGroup
	for i, p := range list {
		i, p := i, p
		if states[p] == entity.LoginDisconnected {
			results[i] = PlatformResult{
				Platform:   p,
				Status:     entity.RunSkipped,
				SkipReason: "not_logged_in",
			}
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = s.refreshPlatform(ctx, "refresh-sync", p)
		}()
	}
	wg.Wait()
	return results
}

func (s *Service) loginStates(ctx context.Context) (map[entity.Platform]entity.LoginState, error) {
	accounts, err := s.accounts.ListAccounts(ctx)
	if err != nil {
		return nil, err
	}
	states := make(map[entity.Platform]entity.LoginState, len(accounts))
	for _, account := range accounts {
		states[account.Platform] = account.LoginState
	}
	return states, nil
}

// tryLockPlatform 尝试拿平台锁；没有配置 Locker 时视为总是成功
func (s *Service) tryLockPlatform(p entity.Platform) (release func(), ok bool, err error) {
	if s.locker == nil {
		return nil, true, nil
	}
	return s.locker.TryLock(p)
}

// refreshPlatform 执行单平台刷新并返回完整结果
// 事件由调用方按结果发出，命令行与桌面版共用这一条路径
func (s *Service) refreshPlatform(ctx context.Context, jobID string, p entity.Platform) PlatformResult {
	release, locked, err := s.tryLockPlatform(p)
	if err != nil {
		return PlatformResult{Platform: p, Status: entity.RunFailed,
			Error: &ErrorEvent{Code: "STORAGE", Message: "获取平台锁失败: " + err.Error()}}
	}
	if !locked {
		return PlatformResult{Platform: p, Status: entity.RunBusy,
			Error: &ErrorEvent{Code: "PLATFORM_BUSY", Message: "该平台正在被其他进程刷新"}}
	}
	if release != nil {
		defer release()
	}
	if err := ctx.Err(); err != nil {
		return PlatformResult{Platform: p, Status: entity.RunCanceled,
			Error: &ErrorEvent{Code: "CANCELED", Message: entity.ErrCanceled.Error()}}
	}

	fetcher, ok := s.fetchers[p]
	if !ok {
		return PlatformResult{Platform: p, Status: entity.RunFailed,
			Error: &ErrorEvent{Code: "INTERNAL", Message: "该平台适配器尚未实现"}}
	}

	account, err := s.accounts.GetAccount(ctx, p)
	if err != nil {
		return PlatformResult{Platform: p, Status: entity.RunFailed, Error: errorEvent(err)}
	}

	session, err := s.sessions.Get(ctx, account.ID)
	if err != nil {
		if errors.Is(err, entity.ErrNotFound) {
			s.setLoginState(ctx, p, entity.LoginDisconnected, nil)
			return PlatformResult{Platform: p, Status: entity.RunFailed,
				Issues: []entity.FetchIssue{{
					Scope: entity.ScopePlatform, Kind: entity.IssueAuth, Message: "尚未登录",
				}},
				Error: &ErrorEvent{Code: "AUTH_REQUIRED", Message: entity.ErrAuthRequired.Error()}}
		}
		// 解密失败视为登录态不可用，要求重新登录
		s.setLoginState(ctx, p, entity.LoginNeedsLogin, err)
		return PlatformResult{Platform: p, Status: entity.RunFailed,
			Issues: []entity.FetchIssue{{
				Scope: entity.ScopePlatform, Kind: entity.IssueAuth, Message: "读取会话失败，请重新登录",
			}},
			Error: &ErrorEvent{Code: "AUTH_EXPIRED", Message: entity.ErrAuthExpired.Error()}}
	}

	run := &entity.FetchRun{
		AccountID: account.ID,
		Platform:  p,
		StartedAt: s.now(),
		Status:    entity.RunRunning,
	}
	if s.runs != nil {
		if err := s.runs.Create(ctx, run); err != nil {
			s.logger.Error("创建刷新记录失败", "platform", p, "error", err)
			return PlatformResult{Platform: p, Status: entity.RunFailed, Run: *run, Error: errorEvent(err)}
		}
	}

	progress := func(ev entity.Progress) {
		s.emitEvent("refresh.progress", ProgressEvent{
			JobID:    jobID,
			Platform: string(p),
			Phase:    ev.Phase,
			Current:  ev.Current,
			Total:    ev.Total,
			Message:  ev.Message,
		})
	}
	opts := entity.FetchOptions{
		Now: s.now,
		OnSessionUpdated: func(updated entity.Session) error {
			if err := s.sessions.Save(ctx, account.ID, updated.Secret, nil, s.now()); err != nil {
				return err
			}
			s.setLoginState(ctx, p, entity.LoginConnected, nil)
			return nil
		},
	}

	result, err := fetcher.Fetch(ctx, *account, *session, opts, progress)

	// 自动续期只做一次，防止无限循环
	if err != nil && errors.Is(err, entity.ErrAuthExpired) {
		renewed, renewErr := s.renew(ctx, *account, *session)
		if renewErr == nil && renewed == nil {
			renewErr = fmt.Errorf("续期实现没有返回会话")
		}
		if renewErr == nil {
			if saveErr := s.sessions.Save(ctx, account.ID, renewed.Secret, nil, s.now()); saveErr != nil {
				err = saveErr
			} else {
				session = renewed
				s.logger.Info("会话已自动续期", "platform", p)
				result, err = fetcher.Fetch(ctx, *account, *session, opts, progress)
			}
		} else {
			err = fmt.Errorf("%w: 自动续期失败: %v", entity.ErrAuthExpired, renewErr)
		}
	}

	if err != nil {
		return s.finishWithError(ctx, run, p, err)
	}

	persist := entity.PersistInput{
		AccountID: account.ID,
		Platform:  p,
		RunID:     run.ID,
		Result:    *result,
		At:        s.now(),
	}
	upserted, err := s.assignments.SaveSnapshot(ctx, persist)
	if err != nil {
		return s.finishWithError(ctx, run, p, err)
	}

	// 部分失败以列表完整性为准
	// PartialByDesign 属于平台限制，按成功处理
	incomplete := !result.Complete && !result.PartialByDesign
	status := entity.RunSuccess
	if incomplete || len(result.Warnings) > 0 {
		status = entity.RunPartial
	}
	run.Status = status
	run.CoursesTotal = len(result.Courses)
	run.CoursesOK = len(result.Courses) - countCourseFailures(result.Warnings)
	if run.CoursesOK < 0 {
		run.CoursesOK = 0
	}
	run.ItemsSeen = len(result.Assignments)
	run.ItemsUpserted = upserted
	run.WarningCount = len(result.Warnings)
	run.FinishedAt = timePtr(s.now())
	if s.runs != nil {
		if err := s.runs.Finish(ctx, *run, result.Warnings, s.now()); err != nil {
			s.logger.Error("写入刷新记录失败", "platform", p, "error", err)
		}
	}
	s.setLoginState(ctx, p, entity.LoginConnected, nil)
	return PlatformResult{Platform: p, Status: status, Run: *run, Issues: result.Warnings}
}

func (s *Service) finishWithError(ctx context.Context, run *entity.FetchRun,
	p entity.Platform, err error) PlatformResult {
	status := entity.RunFailed
	kind := entity.IssueInternal
	switch {
	case errors.Is(err, entity.ErrCanceled):
		status = entity.RunCanceled
		kind = entity.IssueCanceled
	case errors.Is(err, entity.ErrAuthRequired):
		kind = entity.IssueAuth
		s.setLoginState(ctx, p, entity.LoginDisconnected, err)
	case errors.Is(err, entity.ErrAuthExpired), errors.Is(err, entity.ErrAuthFailed):
		kind = entity.IssueAuth
		s.setLoginState(ctx, p, entity.LoginNeedsLogin, err)
	case errors.Is(err, entity.ErrNetwork):
		kind = entity.IssueNetwork
	case errors.Is(err, entity.ErrBusiness):
		kind = entity.IssueBusiness
	case errors.Is(err, entity.ErrPlatformChanged):
		kind = entity.IssueParse
	}

	run.Status = status
	run.ErrorSummary = err.Error()
	run.FinishedAt = timePtr(s.now())
	run.WarningCount = 1
	issues := []entity.FetchIssue{{
		Scope:   entity.ScopePlatform,
		Kind:    kind,
		Message: err.Error(),
	}}
	if s.runs != nil && run.ID > 0 {
		if finishErr := s.runs.Finish(ctx, *run, issues, s.now()); finishErr != nil {
			s.logger.Error("写入刷新失败记录失败", "platform", p, "error", finishErr)
		}
	}
	return PlatformResult{Platform: p, Status: status, Run: *run, Issues: issues, Error: errorEvent(err)}
}

func (s *Service) setLoginState(ctx context.Context, p entity.Platform, state entity.LoginState, cause error) {
	if err := s.accounts.SetLoginState(ctx, p, state, s.now()); err != nil {
		s.logger.Warn("更新登录态失败", "platform", p, "state", state, "error", err)
		return
	}
	payload := map[string]any{
		"platform":   string(p),
		"loginState": string(state),
	}
	if cause != nil {
		payload["error"] = errorEvent(cause)
	}
	s.emitEvent("account.state", payload)
}

func (s *Service) emitRefreshFinished(jobID string, result PlatformResult) {
	s.emitEvent("refresh.finished", FinishedEvent{
		JobID:    jobID,
		Platform: string(result.Platform),
		Status:   string(result.Status),
		Counts: CountsEvent{
			CoursesTotal:  result.Run.CoursesTotal,
			CoursesOK:     result.Run.CoursesOK,
			ItemsSeen:     result.Run.ItemsSeen,
			ItemsUpserted: result.Run.ItemsUpserted,
			WarningCount:  result.Run.WarningCount,
		},
		Warnings: issueEvents(result.Issues),
		Error:    result.Error,
	})
}

// ProgressEvent 对应 refresh.progress 事件
type ProgressEvent struct {
	JobID    string `json:"jobId"`
	Platform string `json:"platform"`
	Phase    string `json:"phase"`
	Current  int    `json:"current"`
	Total    int    `json:"total"`
	Message  string `json:"message"`
}

// CountsEvent 是刷新结束时的计数
type CountsEvent struct {
	CoursesTotal  int `json:"coursesTotal"`
	CoursesOK     int `json:"coursesOk"`
	ItemsSeen     int `json:"itemsSeen"`
	ItemsUpserted int `json:"itemsUpserted"`
	WarningCount  int `json:"warningCount"`
}

// IssueEvent 是不含凭据的问题摘要
type IssueEvent struct {
	Scope   string `json:"scope"`
	Ref     string `json:"ref,omitempty"`
	Kind    string `json:"kind"`
	Message string `json:"message"`
	RawPath string `json:"rawPath,omitempty"`
}

// ErrorEvent 是事件的错误摘要
type ErrorEvent struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// FinishedEvent 对应 refresh.finished 事件
type FinishedEvent struct {
	JobID    string       `json:"jobId"`
	Platform string       `json:"platform"`
	Status   string       `json:"status"`
	Counts   CountsEvent  `json:"counts"`
	Warnings []IssueEvent `json:"warnings,omitempty"`
	Error    *ErrorEvent  `json:"error,omitempty"`
}

// ErrorCode 把领域错误翻译成前端使用的错误码
func ErrorCode(err error) string {
	switch {
	case errors.Is(err, entity.ErrAuthRequired):
		return "AUTH_REQUIRED"
	case errors.Is(err, entity.ErrAuthExpired):
		return "AUTH_EXPIRED"
	case errors.Is(err, entity.ErrAuthFailed):
		return "AUTH_FAILED"
	case errors.Is(err, entity.ErrNetwork):
		return "NETWORK"
	case errors.Is(err, entity.ErrBusiness):
		return "BUSINESS"
	case errors.Is(err, ErrRefreshRunning):
		return "REFRESH_RUNNING"
	case errors.Is(err, entity.ErrPlatformChanged):
		return "PLATFORM_CHANGED"
	case errors.Is(err, entity.ErrCanceled):
		return "CANCELED"
	case errors.Is(err, entity.ErrReleaseUnavailable):
		return "RELEASE_UNAVAILABLE"
	case errors.Is(err, entity.ErrReleaseRateLimited):
		return "RELEASE_RATE_LIMITED"
	case errors.Is(err, entity.ErrReleaseInvalid):
		return "RELEASE_INVALID"
	default:
		return "INTERNAL"
	}
}

func errorEvent(err error) *ErrorEvent {
	if err == nil {
		return nil
	}
	return &ErrorEvent{Code: ErrorCode(err), Message: err.Error()}
}

func issueEvents(issues []entity.FetchIssue) []IssueEvent {
	if len(issues) == 0 {
		return nil
	}
	out := make([]IssueEvent, 0, len(issues))
	for _, issue := range issues {
		out = append(out, IssueEvent{
			Scope:   string(issue.Scope),
			Ref:     issue.Ref,
			Kind:    string(issue.Kind),
			Message: issue.Message,
			RawPath: issue.RawPath,
		})
	}
	return out
}

func countCourseFailures(issues []entity.FetchIssue) int {
	seen := make(map[string]struct{}, len(issues))
	for _, issue := range issues {
		if issue.Scope == entity.ScopeCourse && issue.Ref != "" {
			seen[issue.Ref] = struct{}{}
		}
	}
	return len(seen)
}

func timePtr(t time.Time) *time.Time { return &t }
