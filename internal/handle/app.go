// Package handle 是 Wails 绑定层：参数校验、错误翻译、DTO 转换。
//
// 它不直接访问数据库或 HTTP；所有用例都经过 service。
package handle

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"catcher/internal/domain/entity"
	"catcher/internal/dto"
	"catcher/internal/service"
)

// App 是 Wails 绑定的接收者。前端通过 window.go.handle.App 调用这些方法。
type App struct {
	svc *service.Service
	ctx context.Context
}

func New(svc *service.Service) *App {
	return &App{svc: svc}
}

// Startup 由 Wails 在窗口启动后调用。
func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
	// 事件通过 Wails 推给前端；关闭窗口后 runtime 不再发送
	a.svc.SetEmitter(func(name string, payload any) {
		runtime.EventsEmit(ctx, name, payload)
	})
}

func (a *App) context() context.Context {
	if a.ctx == nil {
		return context.Background()
	}
	return a.ctx
}

// GetBootstrap 返回平台卡、统计、数据目录与版本。
// termOnly 与列表的「只看本学期」开关保持一致
func (a *App) GetBootstrap(termOnly bool) (res dto.BootstrapResult) {
	defer func() {
		if r := recover(); r != nil {
			res = dto.BootstrapResult{
				OK:    false,
				Error: &dto.APIError{Code: "INTERNAL", Message: "读取应用状态时发生内部错误", Detail: fmt.Sprint(r)},
			}
		}
	}()

	b, err := a.svc.Bootstrap(a.context(), termOnly)
	if err != nil {
		return dto.BootstrapResult{
			OK:    false,
			Error: &dto.APIError{Code: "STORAGE", Message: "读取应用状态失败", Detail: err.Error()},
		}
	}

	data := dto.BootstrapData{
		Platforms: make([]dto.PlatformView, 0, len(entity.AllPlatforms())),
		Stats:     countsView(b.Stats),
		DataDir:   b.DataDir,
		Version:   b.Version,
	}
	accounts := make(map[entity.Platform]entity.Account, len(b.Accounts))
	for _, account := range b.Accounts {
		accounts[account.Platform] = account
	}
	for _, p := range entity.AllPlatforms() {
		account, ok := accounts[p]
		view := dto.PlatformView{
			Platform:     string(p),
			DisplayName:  "未登录",
			LoginState:   string(entity.LoginDisconnected),
			Counts:       countsView(b.Counts[p]),
			Experimental: p == entity.PlatformTronclass,
		}
		if ok {
			view.LoginState = string(account.LoginState)
			if account.DisplayName != "" {
				view.DisplayName = account.DisplayName
			}
		}
		if run, ok := b.LastRuns[p]; ok {
			view.LastRunAt = run.StartedAt.UTC().Format(time.RFC3339)
			view.LastRunState = string(run.Status)
			view.LastRunID = run.ID
			if run.ErrorSummary != "" {
				code := "FETCH_FAILED"
				if run.Status == entity.RunPartial {
					code = "FETCH_PARTIAL"
				}
				view.Error = &dto.APIError{Code: code, Message: run.ErrorSummary}
			}
		}
		data.Platforms = append(data.Platforms, view)
	}

	return dto.BootstrapResult{OK: true, Data: &data}
}

// ListAssignments 返回统一作业表的数据。
func (a *App) ListAssignments(filter dto.AssignmentFilter) (res dto.AssignmentListResult) {
	defer func() {
		if r := recover(); r != nil {
			res = dto.AssignmentListResult{
				OK:    false,
				Error: &dto.APIError{Code: "INTERNAL", Message: "查询作业时发生内部错误", Detail: fmt.Sprint(r)},
			}
		}
	}()

	entityFilter, apiErr := toEntityFilter(filter)
	if apiErr != nil {
		return dto.AssignmentListResult{OK: false, Error: apiErr}
	}

	items, err := a.svc.ListAssignments(a.context(), entityFilter)
	if err != nil {
		return dto.AssignmentListResult{
			OK:    false,
			Error: &dto.APIError{Code: "STORAGE", Message: "查询作业失败", Detail: err.Error()},
		}
	}

	now := time.Now()
	views := make([]dto.AssignmentView, 0, len(items))
	for _, item := range items {
		views = append(views, toAssignmentView(item, now))
	}
	return dto.AssignmentListResult{OK: true, Items: views, Total: len(views)}
}

// GetSettings 返回只读设置视图。
func (a *App) GetSettings() (res dto.SettingsResult) {
	defer func() {
		if r := recover(); r != nil {
			res = dto.SettingsResult{
				OK:    false,
				Error: &dto.APIError{Code: "INTERNAL", Message: "读取设置时发生内部错误", Detail: fmt.Sprint(r)},
			}
		}
	}()

	cfg := a.svc.Settings()
	return dto.SettingsResult{
		OK: true,
		Settings: &dto.SettingsView{
			LogLevel:       cfg.Log.Level,
			RawEnabled:     cfg.Raw.Enabled,
			RawRetention:   cfg.Raw.RetentionDays,
			IncludeHistory: cfg.Fetch.IncludeHistory,
			Theme:          cfg.UI.Theme,
			DataDir:        a.svc.DataDir(),
			Version:        a.svc.Version(),
		},
	}
}

// OpenDataDir 在资源管理器中打开数据目录。
func (a *App) OpenDataDir() (res dto.APIResult) {
	defer func() {
		if r := recover(); r != nil {
			res = dto.APIResult{
				OK:    false,
				Error: &dto.APIError{Code: "INTERNAL", Message: "打开数据目录时发生内部错误", Detail: fmt.Sprint(r)},
			}
		}
	}()

	if err := a.svc.OpenDataDir(a.context()); err != nil {
		return dto.APIResult{
			OK:    false,
			Error: &dto.APIError{Code: "OPEN_DIR_FAILED", Message: "打开数据目录失败", Detail: err.Error()},
		}
	}
	return dto.APIResult{OK: true}
}

// StartRefresh 启动刷新任务；platforms 为空表示全部平台
func (a *App) StartRefresh(platforms []string) (res dto.RefreshStartResult) {
	defer func() {
		if r := recover(); r != nil {
			res = dto.RefreshStartResult{
				OK:    false,
				Error: &dto.APIError{Code: "INTERNAL", Message: "启动刷新时发生内部错误", Detail: fmt.Sprint(r)},
			}
		}
	}()

	ps := make([]entity.Platform, 0, len(platforms))
	for _, raw := range platforms {
		p, err := entity.ParsePlatform(raw)
		if err != nil {
			return dto.RefreshStartResult{
				OK:    false,
				Error: &dto.APIError{Code: "INVALID_ARGUMENT", Message: "平台参数无效", Detail: err.Error()},
			}
		}
		ps = append(ps, p)
	}

	start, err := a.svc.StartRefresh(a.context(), ps)
	if err != nil {
		return dto.RefreshStartResult{
			OK:    false,
			Error: &dto.APIError{Code: "REFRESH_FAILED", Message: "启动刷新失败", Detail: err.Error()},
		}
	}
	return dto.RefreshStartResult{
		OK:             true,
		JobID:          start.JobID,
		Platforms:      platformStrings(start.Platforms),
		AlreadyRunning: start.AlreadyRunning,
	}
}

// CancelRefresh 取消一个刷新任务
func (a *App) CancelRefresh(jobID string) (res dto.APIResult) {
	defer func() {
		if r := recover(); r != nil {
			res = dto.APIResult{
				OK:    false,
				Error: &dto.APIError{Code: "INTERNAL", Message: "取消刷新时发生内部错误", Detail: fmt.Sprint(r)},
			}
		}
	}()

	if jobID == "" {
		return dto.APIResult{OK: false, Error: &dto.APIError{Code: "INVALID_ARGUMENT", Message: "缺少任务 id"}}
	}
	if err := a.svc.CancelRefresh(jobID); err != nil {
		return dto.APIResult{
			OK:    false,
			Error: &dto.APIError{Code: "REFRESH_NOT_FOUND", Message: "刷新任务不存在或已结束", Detail: err.Error()},
		}
	}
	return dto.APIResult{OK: true}
}

// StartYuketangLogin 启动扫码登录，二维码与状态通过事件推送
func (a *App) StartYuketangLogin() (res dto.RefreshStartResult) {
	defer func() {
		if r := recover(); r != nil {
			res = dto.RefreshStartResult{
				OK:    false,
				Error: &dto.APIError{Code: "INTERNAL", Message: "启动扫码登录时发生内部错误", Detail: fmt.Sprint(r)},
			}
		}
	}()

	start, err := a.svc.StartYuketangLogin(a.context())
	if err != nil {
		return dto.RefreshStartResult{
			OK:    false,
			Error: &dto.APIError{Code: "LOGIN_FAILED", Message: "启动雨课堂扫码登录失败", Detail: err.Error()},
		}
	}
	return dto.RefreshStartResult{
		OK:        true,
		JobID:     start.JobID,
		Platforms: platformStrings(start.Platforms),
	}
}

// CancelYuketangLogin 停止扫码轮询
func (a *App) CancelYuketangLogin(jobID string) (res dto.APIResult) {
	defer func() {
		if r := recover(); r != nil {
			res = dto.APIResult{
				OK:    false,
				Error: &dto.APIError{Code: "INTERNAL", Message: "取消扫码登录时发生内部错误", Detail: fmt.Sprint(r)},
			}
		}
	}()

	if jobID == "" {
		return dto.APIResult{OK: false, Error: &dto.APIError{Code: "INVALID_ARGUMENT", Message: "缺少任务 id"}}
	}
	if err := a.svc.CancelYuketangLogin(jobID); err != nil {
		return dto.APIResult{
			OK:    false,
			Error: &dto.APIError{Code: "LOGIN_NOT_FOUND", Message: "扫码任务不存在或已结束", Detail: err.Error()},
		}
	}
	return dto.APIResult{OK: true}
}

// RefreshYuketangLogin 重新获取一张二维码
func (a *App) RefreshYuketangLogin(jobID string) (res dto.APIResult) {
	defer func() {
		if r := recover(); r != nil {
			res = dto.APIResult{
				OK:    false,
				Error: &dto.APIError{Code: "INTERNAL", Message: "刷新二维码时发生内部错误", Detail: fmt.Sprint(r)},
			}
		}
	}()

	if jobID == "" {
		return dto.APIResult{OK: false, Error: &dto.APIError{Code: "INVALID_ARGUMENT", Message: "缺少任务 id"}}
	}
	if err := a.svc.RefreshYuketangLogin(jobID); err != nil {
		return dto.APIResult{
			OK:    false,
			Error: &dto.APIError{Code: "LOGIN_NOT_FOUND", Message: "扫码任务不存在或已结束", Detail: err.Error()},
		}
	}
	return dto.APIResult{OK: true}
}

// LoginChaoxing 用手机号+密码登录学习通。密码只在本次调用内使用，不回传
func (a *App) LoginChaoxing(req dto.PasswordLoginRequest) (res dto.PasswordLoginResult) {
	defer func() {
		if r := recover(); r != nil {
			res = dto.PasswordLoginResult{
				OK:    false,
				Error: &dto.APIError{Code: "INTERNAL", Message: "学习通登录时发生内部错误", Detail: fmt.Sprint(r)},
			}
		}
	}()

	phone := strings.TrimSpace(req.Phone)
	if phone == "" || req.Password == "" {
		return dto.PasswordLoginResult{
			OK:    false,
			Error: &dto.APIError{Code: "INVALID_ARGUMENT", Message: "请填写手机号和密码"},
		}
	}
	if len(phone) != 11 || strings.Trim(phone, "0123456789") != "" {
		return dto.PasswordLoginResult{
			OK:    false,
			Error: &dto.APIError{Code: "INVALID_ARGUMENT", Message: "学习通账号是 11 位手机号"},
		}
	}

	err := a.svc.LoginChaoxing(a.context(), entity.PasswordCredential{
		Phone:    phone,
		Password: req.Password,
	}, req.Remember)
	if err != nil {
		return dto.PasswordLoginResult{
			OK:          false,
			LoginState:  string(entity.LoginNeedsLogin),
			DisplayName: entity.MaskPhone(phone),
			Error: &dto.APIError{
				Code:    service.ErrorCode(err),
				Message: "学习通登录失败",
				Detail:  err.Error(),
			},
		}
	}
	return dto.PasswordLoginResult{
		OK:          true,
		LoginState:  string(entity.LoginConnected),
		DisplayName: entity.MaskPhone(phone),
	}
}

// GetFetchIssues 返回某次刷新的问题明细
func (a *App) GetFetchIssues(runID int64) (res dto.FetchIssuesResult) {
	defer func() {
		if r := recover(); r != nil {
			res = dto.FetchIssuesResult{
				OK:    false,
				Items: []dto.FetchIssueView{},
				Error: &dto.APIError{Code: "INTERNAL", Message: "读取刷新详情时发生内部错误", Detail: fmt.Sprint(r)},
			}
		}
	}()

	if runID <= 0 {
		return dto.FetchIssuesResult{
			OK:    false,
			Items: []dto.FetchIssueView{},
			Error: &dto.APIError{Code: "INVALID_ARGUMENT", Message: "缺少刷新记录 id"},
		}
	}
	issues, err := a.svc.FetchIssues(a.context(), runID)
	if err != nil {
		return dto.FetchIssuesResult{
			OK:    false,
			Items: []dto.FetchIssueView{},
			Error: &dto.APIError{Code: "STORAGE", Message: "读取刷新详情失败", Detail: err.Error()},
		}
	}

	items := make([]dto.FetchIssueView, 0, len(issues))
	for _, issue := range issues {
		items = append(items, dto.FetchIssueView{
			Scope:   string(issue.Scope),
			Ref:     issue.Ref,
			Kind:    string(issue.Kind),
			Message: issue.Message,
			RawPath: issue.RawPath,
		})
	}
	return dto.FetchIssuesResult{OK: true, Items: items}
}

func platformStrings(platforms []entity.Platform) []string {
	out := make([]string, len(platforms))
	for i, p := range platforms {
		out[i] = string(p)
	}
	return out
}

func toEntityFilter(f dto.AssignmentFilter) (entity.AssignmentFilter, *dto.APIError) {
	out := entity.AssignmentFilter{
		ShowSubmitted: f.ShowSubmitted,
		Keyword:       f.Keyword,
		TermOnly:      f.TermOnly,
	}
	for _, raw := range f.Platforms {
		p, err := entity.ParsePlatform(raw)
		if err != nil {
			return out, &dto.APIError{Code: "INVALID_ARGUMENT", Message: "平台参数无效", Detail: err.Error()}
		}
		out.Platforms = append(out.Platforms, p)
	}
	for _, raw := range f.Statuses {
		status := entity.SubmissionStatus(raw)
		if !status.Valid() {
			return out, &dto.APIError{Code: "INVALID_ARGUMENT", Message: "状态参数无效", Detail: raw}
		}
		out.Statuses = append(out.Statuses, status)
	}
	out.CourseIDs = append(out.CourseIDs, f.CourseIDs...)
	return out, nil
}

func countsView(c service.Counts) dto.CountsView {
	return dto.CountsView{
		OpenUnsubmitted:    c.OpenUnsubmitted,
		OverdueUnsubmitted: c.OverdueUnsubmitted,
		Unknown:            c.Unknown,
		Submitted:          c.Submitted,
	}
}

func toAssignmentView(a entity.Assignment, now time.Time) dto.AssignmentView {
	view := dto.AssignmentView{
		ID:               a.ID,
		Platform:         string(a.Platform),
		CourseName:       a.CourseName,
		CourseExternalID: a.CourseExternalID,
		Title:            a.Title,
		Status:           string(a.Status),
		Phase:            string(entity.PhaseOf(a.Deadline, now)),
		DeadlineSource:   string(a.DeadlineSource),
		DeadlineRaw:      a.DeadlineRaw,
		DetailURL:        a.DetailURL,
		FirstSeenAt:      a.FirstSeenAt.UTC().Format(time.RFC3339),
		LastSeenAt:       a.LastSeenAt.UTC().Format(time.RFC3339),
	}
	if a.Deadline != nil {
		view.DeadlineAt = a.Deadline.UTC().Format(time.RFC3339)
		view.DeadlineLocal = a.Deadline.In(shanghai).Format("2006-01-02 15:04")
	}
	return view
}

var shanghai = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("CST", 8*3600)
	}
	return loc
}()
