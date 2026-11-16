package fiforal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"catcher/internal/domain/entity"
)

const (
	// fiforalCourseID 是合成课程的固定 ExternalID。平台没有课程概念
	fiforalCourseID   = "fiforal:oral"
	fiforalCourseName = "FiF 口语"

	// 两个任务列表的生命周期
	statusInProgress = 1
	statusFinished   = 2

	// maxPages 防御服务端忽略 page 时死循环
	maxPages = 100
)

// fetchRun 保存一次抓取过程中的告警与服务器时间
type fetchRun struct {
	client    *Client
	result    *entity.FetchResult
	serverNow *time.Time
}

// Fetch 抓取进行中与已结束两个任务列表
//
// 完整性只在两个列表都翻到空页时成立；进行中列表失败整次失败，
// 已结束列表失败降级成警告，避免把「没抓到」表现成「没有作业」
func (c *Client) Fetch(ctx context.Context, account entity.Account, session entity.Session,
	opts entity.FetchOptions, progress entity.ProgressFunc) (*entity.FetchResult, error) {
	_ = account
	secret, err := decodeSecret(session.Secret)
	if err != nil {
		return nil, err
	}
	if progress == nil {
		progress = func(entity.Progress) {}
	}

	jar := newCookieJar(secret.Cookies, c.now)
	if jar.isEmpty() {
		return nil, fmt.Errorf("%w: 会话缺少 cookie", entity.ErrAuthRequired)
	}
	run := &fetchRun{client: c, result: &entity.FetchResult{}}

	progress(entity.Progress{Platform: entity.PlatformFiforal, Phase: "courses", Message: "换用 H5 token"})
	token, err := c.ensureToken(ctx, jar)
	if err != nil {
		return nil, err
	}
	if err := c.persistCookies(jar, token.UserID, opts); err != nil {
		return nil, fmt.Errorf("保存 FiF 口语 cookie 失败: %w", err)
	}

	persist := func() error {
		return c.persistCookies(jar, token.UserID, opts)
	}

	progress(entity.Progress{Platform: entity.PlatformFiforal, Phase: "courses", Message: "读取进行中任务"})
	inProgress, complete1, err := c.fetchList(ctx, jar, statusInProgress, run, persist)
	if err != nil {
		// 进行中列表失败就整次失败，不能返回空列表冒充「没有作业」
		return nil, err
	}
	if err := c.persistCookies(jar, token.UserID, opts); err != nil {
		return nil, fmt.Errorf("保存 FiF 口语 cookie 失败: %w", err)
	}

	progress(entity.Progress{Platform: entity.PlatformFiforal, Phase: "completed", Message: "读取已结束任务"})
	finished, complete2, err := c.fetchList(ctx, jar, statusFinished, run, persist)
	if err != nil {
		if errors.Is(err, entity.ErrAuthExpired) || errors.Is(err, entity.ErrAuthRequired) ||
			errors.Is(err, entity.ErrCanceled) {
			return nil, err
		}
		run.warn(entity.ScopePlatform, "", issueKindOf(err),
			fmt.Sprintf("已结束任务列表读取失败，本次只写入进行中任务: %v", err))
		finished = nil
		complete2 = false
	}
	if err := c.persistCookies(jar, token.UserID, opts); err != nil {
		return nil, fmt.Errorf("保存 FiF 口语 cookie 失败: %w", err)
	}

	items := mergeTasks(inProgress, finished, run)

	progress(entity.Progress{
		Platform: entity.PlatformFiforal,
		Phase:    "details",
		Current:  0,
		Total:    len(items),
		Message:  "整理任务",
	})
	for i, item := range items {
		progress(entity.Progress{
			Platform: entity.PlatformFiforal,
			Phase:    "details",
			Current:  i + 1,
			Total:    len(items),
			Message:  taskTitle(item),
		})
		run.result.Assignments = append(run.result.Assignments, run.toAssignment(item))
	}

	run.result.Courses = []entity.Course{{
		Platform:   entity.PlatformFiforal,
		ExternalID: fiforalCourseID,
		Name:       fiforalCourseName,
		// 平台没有学期字段；两个列表全抓回来，由界面的「只看本学期」过滤
		SemesterKey: "",
	}}
	complete := complete1 && complete2
	run.result.Complete = complete
	run.result.CourseListComplete = complete
	run.result.ServerNow = run.serverNow

	progress(entity.Progress{
		Platform: entity.PlatformFiforal,
		Phase:    "done",
		Current:  len(items),
		Total:    len(items),
		Message:  "抓取完成",
	})
	return run.result, nil
}

// fetchList 从 page=1 翻到第一个空页。H5 自己只增不减的计数器不能照抄
func (c *Client) fetchList(ctx context.Context, jar *cookieJar,
	status int, run *fetchRun, persist func() error) ([]taskItem, bool, error) {
	out := make([]taskItem, 0, 16)
	seen := make(map[string]struct{}, 16)

	token, err := c.ensureToken(ctx, jar)
	if err != nil {
		return nil, false, err
	}

	for page := 1; page <= maxPages; page++ {
		if err := ctx.Err(); err != nil {
			return nil, false, fmt.Errorf("%w: %v", entity.ErrCanceled, err)
		}

		items, sysTime, err := c.callTaskList(ctx, jar, token, status, page)
		if err != nil && errors.Is(err, entity.ErrAuthExpired) {
			// 换一次 token 重试该页；仍失败按登录失效处理
			token, err = c.remintToken(ctx, jar)
			if err == nil {
				items, sysTime, err = c.callTaskList(ctx, jar, token, status, page)
			}
		}
		if err != nil {
			return nil, false, fmt.Errorf("第 %d 页: %w", page, err)
		}
		// 任务接口轮换 cookie 时立即交给上层保存
		if persist != nil {
			if persistErr := persist(); persistErr != nil {
				return nil, false, fmt.Errorf("保存 FiF 口语 cookie 失败: %w", persistErr)
			}
		}

		run.observeServerNow(sysTime)
		if len(items) == 0 {
			return out, true, nil
		}

		fresh := 0
		for _, item := range items {
			id := item.taskID()
			if id == "" {
				run.warn(entity.ScopePlatform, "", entity.IssueParse,
					fmt.Sprintf("%s列表第 %d 页有条目缺少任务 id，列表不完整", listName(status), page))
				return out, false, nil
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			fresh++
			out = append(out, item)
		}
		if fresh == 0 {
			// 整页都是前面出现过的任务，服务端多半忽略了 page
			run.warn(entity.ScopePlatform, "", entity.IssueParse,
				fmt.Sprintf("%s列表第 %d 页全是重复条目，已停止翻页", listName(status), page))
			return out, false, nil
		}
	}

	run.warn(entity.ScopePlatform, "", entity.IssueParse,
		fmt.Sprintf("%s列表超过 %d 页仍未翻到空页，列表不完整", listName(status), maxPages))
	return out, false, nil
}

func (c *Client) callTaskList(ctx context.Context, jar *cookieJar, token *h5Token,
	status, page int) ([]taskItem, string, error) {
	body := url.Values{
		"userId": {token.UserID},
		"status": {strconv.Itoa(status)},
		"page":   {strconv.Itoa(page)},
	}.Encode()
	source := token.Source
	if source == "" {
		source = defaultSource
	}

	resp, err := c.do(ctx, http.MethodPost, c.taskBaseURL+taskListPath, jar, []byte(body), 0,
		map[string]string{
			"Authorization": "Bearer " + token.Value,
			"source":        source,
			"Content-Type":  "application/x-www-form-urlencoded",
		})
	if err != nil {
		return nil, "", classifySendError(err)
	}

	var env taskListEnvelope
	if err := json.Unmarshal(resp.Raw, &env); err != nil {
		return nil, "", fmt.Errorf("%w: 任务列表响应不是 JSON", entity.ErrPlatformChanged)
	}
	switch env.Status {
	case 1:
		return env.Data.TtiList, env.Data.SysTime, nil
	case -1:
		return nil, "", fmt.Errorf("%w: 任务列表返回未登录", entity.ErrAuthExpired)
	default:
		return nil, "", fmt.Errorf("%w: %s (status=%d)", entity.ErrBusiness, env.Msg, env.Status)
	}
}

// mergeTasks 进行中优先；跨列表重复只保留先抓到的一条并记告警
func mergeTasks(inProgress, finished []taskItem, run *fetchRun) []taskItem {
	items := make([]taskItem, 0, len(inProgress)+len(finished))
	seen := make(map[string]struct{}, len(inProgress)+len(finished))

	for _, item := range inProgress {
		id := item.taskID()
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		items = append(items, item)
	}
	for _, item := range finished {
		id := item.taskID()
		if _, ok := seen[id]; ok {
			run.warn(entity.ScopeAssignment, entity.FiforalDedupKey(id), entity.IssueParse,
				fmt.Sprintf("任务 %s 同时出现在进行中与已结束列表，已保留进行中的记录", id))
			continue
		}
		seen[id] = struct{}{}
		items = append(items, item)
	}
	return items
}

// toAssignment 把一条任务映射成实体。字段异常只告警，不影响列表完整性
func (r *fetchRun) toAssignment(item taskItem) entity.Assignment {
	id := item.taskID()
	title := taskTitle(item)

	deadlineRaw, deadline := r.deadlineOf(item, id)
	source := entity.DeadlineUnknown
	if deadline != nil {
		source = entity.DeadlinePlatformExact
	}

	return entity.Assignment{
		Platform:         entity.PlatformFiforal,
		CourseExternalID: fiforalCourseID,
		CourseName:       fiforalCourseName,
		ExternalID:       id,
		DedupKey:         entity.FiforalDedupKey(id),
		Title:            title,
		Status:           r.statusOf(item, id),
		Deadline:         deadline,
		DeadlineSource:   source,
		DeadlineRaw:      deadlineRaw,
		DetailURL:        r.client.detailURL(item),
		Description:      descriptionOf(item),
	}
}

func taskTitle(item taskItem) string {
	if title := entity.NormalizeTitle(item.TaskName); title != "" {
		return title
	}
	return "任务 " + item.taskID()
}

// descriptionOf 只放教师与说明，不拼 token 或其它字段
func descriptionOf(item taskItem) string {
	parts := make([]string, 0, 2)
	if teacher := strings.TrimSpace(item.TeaName); teacher != "" {
		parts = append(parts, "教师："+teacher)
	}
	if info := strings.TrimSpace(item.TaskInfo); info != "" {
		parts = append(parts, info)
	}
	return strings.Join(parts, "；")
}

func (r *fetchRun) statusOf(item taskItem, id string) entity.SubmissionStatus {
	complete, ok := item.complete()
	if !ok {
		// 缺失、null 或类型漂移都按未知处理，绝不能落到「未提交」
		return entity.StatusUnknown
	}
	switch complete {
	case 100:
		return entity.StatusSubmitted
	case 0:
		return entity.StatusUnsubmitted
	default:
		r.warn(entity.ScopeAssignment, entity.FiforalDedupKey(id), entity.IssueParse,
			fmt.Sprintf("提交状态 complete=%d 无法识别，按未知处理", complete))
		return entity.StatusUnknown
	}
}

// deadlineOf 选择截止时间。isDelay==1 才看 delayTime：
// 实测 isDelay==0 时 delayTime 也可能非空且等于 endTime
func (r *fetchRun) deadlineOf(item taskItem, id string) (string, *time.Time) {
	raw := strings.TrimSpace(item.EndTime)
	if delayed, ok := item.isDelay(); ok && delayed == 1 {
		if delayed := strings.TrimSpace(item.DelayTime); delayed != "" {
			raw = delayed
		} else {
			r.warn(entity.ScopeAssignment, entity.FiforalDedupKey(id), entity.IssueParse,
				"任务标记为延期但 delayTime 为空，已退回 endTime")
		}
	}
	if raw == "" {
		return "", nil
	}
	deadline, err := parsePlatformTime(raw)
	if err != nil {
		r.warn(entity.ScopeAssignment, entity.FiforalDedupKey(id), entity.IssueParse,
			"截止时间无法解析: "+err.Error())
		return raw, nil
	}
	return raw, deadline
}

// parsePlatformTime 解析北京时间的裸字符串，统一存 UTC
func parsePlatformTime(raw string) (*time.Time, error) {
	t, err := time.ParseInLocation("2006-01-02 15:04:05", strings.TrimSpace(raw), shanghai)
	if err != nil {
		return nil, fmt.Errorf("%q 不是 YYYY-MM-DD HH:mm:ss", raw)
	}
	utc := t.UTC()
	return &utc, nil
}

func (r *fetchRun) observeServerNow(raw string) {
	if r.serverNow != nil || strings.TrimSpace(raw) == "" {
		return
	}
	if t, err := parsePlatformTime(raw); err == nil {
		r.serverNow = t
	}
}

func (r *fetchRun) warn(scope entity.IssueScope, ref string, kind entity.IssueKind, message string) {
	r.result.Warnings = append(r.result.Warnings, entity.FetchIssue{
		Scope:   scope,
		Ref:     ref,
		Kind:    kind,
		Message: message,
	})
}

func listName(status int) string {
	if status == statusFinished {
		return "已结束任务"
	}
	return "进行中任务"
}
