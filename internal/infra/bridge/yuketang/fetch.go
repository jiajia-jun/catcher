package yuketang

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"catcher/internal/domain/entity"
)

const (
	learningListPath  = "/v/course_meta/learning_list/"
	classroomLogsPath = "/v/course_meta/classroom_logs"
	// todoListPathFormat 需要 classroom_id 与 classroom 级 sku_id
	todoListPathFormat = "/c27/online_courseware/course/classroom/%s/%s/todo_list/?client_type=h5"
)

// Fetch 抓取当前学期的课程与作业
//
// 提交状态按课堂待办列表换算：待办里的是未完成，不在待办里的习题视为已完成；
// 视频的「看完」不等同「提交」，不在待办时保持 unknown
func (c *Client) Fetch(ctx context.Context, account entity.Account, session entity.Session,
	opts entity.FetchOptions, progress entity.ProgressFunc) (*entity.FetchResult, error) {
	secret, err := decodeSecret(session.Secret)
	if err != nil {
		return nil, err
	}
	if progress == nil {
		progress = func(entity.Progress) {}
	}
	now := c.now()
	if opts.Now != nil {
		now = opts.Now()
	}

	progress(entity.Progress{Platform: entity.PlatformYuketang, Phase: "courses", Message: "读取课程列表"})
	items, err := c.learningList(ctx, secret.SessionID)
	if err != nil {
		return nil, err
	}

	selected, semesterKey, warnings, complete := selectCurrentSemester(items, now)
	if semesterKey == "" {
		// 连一个学期前缀都读不到时不做软删除，避免把旧数据清空
		complete = false
	}

	result := &entity.FetchResult{
		Complete: complete,
		Warnings: warnings,
	}
	for i, item := range selected {
		progress(entity.Progress{
			Platform: entity.PlatformYuketang,
			Phase:    "assignments",
			Current:  i + 1,
			Total:    len(selected),
			Message:  item.CourseName,
		})

		logs, err := c.classroomLogs(ctx, item, secret.SessionID)
		if err != nil {
			// 认证失效/取消立即中止，本次结果不落库
			if errors.Is(err, entity.ErrAuthExpired) || errors.Is(err, entity.ErrCanceled) {
				return nil, err
			}
			// 单门课失败：该课程数据保持不动，其余课程继续
			result.Complete = false
			result.Warnings = append(result.Warnings, entity.FetchIssue{
				Scope:   entity.ScopeCourse,
				Ref:     item.CourseID.String(),
				Kind:    issueKindOf(err),
				Message: fmt.Sprintf("课程 %s 抓取失败: %v", item.CourseName, err),
			})
			continue
		}

		// 待办列表是提交状态的唯一来源。失败只降级成 unknown 并记 warning，
		// 不影响课程与作业本身落库
		unfinished, todoErr := c.unfinishedLeaves(ctx, item, logs, secret.SessionID)
		if todoErr != nil {
			if errors.Is(todoErr, entity.ErrAuthExpired) || errors.Is(todoErr, entity.ErrCanceled) {
				return nil, todoErr
			}
			result.Warnings = append(result.Warnings, entity.FetchIssue{
				Scope:   entity.ScopeCourse,
				Ref:     item.CourseID.String(),
				Kind:    issueKindOf(todoErr),
				Message: fmt.Sprintf("课程 %s 待办列表获取失败，作业状态保持未知: %v", item.CourseName, todoErr),
			})
		}

		course, assignments, parseWarnings, ok := parseClassroomLogs(item, logs, unfinished)
		result.Courses = append(result.Courses, course)
		result.Assignments = append(result.Assignments, assignments...)
		result.Warnings = append(result.Warnings, parseWarnings...)
		if !ok {
			result.Complete = false
		}
		if logs.Data.NowTime.String() != "" && result.ServerNow == nil {
			if nowTime, valid := parseSeconds(logs.Data.NowTime); valid {
				result.ServerNow = &nowTime
			} else {
				result.Warnings = append(result.Warnings, entity.FetchIssue{
					Scope:   entity.ScopeCourse,
					Ref:     item.CourseID.String(),
					Kind:    entity.IssueParse,
					Message: fmt.Sprintf("now_time 位数异常: %s", logs.Data.NowTime.String()),
				})
			}
		}
	}

	progress(entity.Progress{
		Platform: entity.PlatformYuketang,
		Phase:    "done",
		Current:  len(selected),
		Total:    len(selected),
		Message:  "抓取完成",
	})
	return result, nil
}

func (c *Client) learningList(ctx context.Context, sessionID string) ([]courseItem, error) {
	resp, err := c.get(ctx, learningListPath, sessionID)
	if err != nil {
		return nil, err
	}

	var out courseListResp
	if err := json.Unmarshal(resp.Raw, &out); err != nil {
		return nil, fmt.Errorf("%w: 课程列表不是 JSON: %v", entity.ErrPlatformChanged, err)
	}
	// 登录态只看响应体，不看 HTTP 200
	if out.Op == "web_redirect" {
		return nil, fmt.Errorf("%w: 课程列表被重定向到登录页", entity.ErrAuthExpired)
	}
	// 实测 2026-10 课程列表没有 success 字段，不能把「缺失」当成 false
	if out.Success != nil && !*out.Success {
		return nil, fmt.Errorf("%w: 课程列表 success=false msg=%s", entity.ErrPlatformChanged, out.Msg)
	}
	return out.Data, nil
}

func (c *Client) classroomLogs(ctx context.Context, item courseItem, sessionID string) (*classroomLogsResp, error) {
	q := url.Values{}
	q.Set("course_id", item.CourseID.String())
	q.Set("classroom_id", item.ClassroomID.String())
	q.Set("activity_type", "-1")

	resp, err := c.get(ctx, classroomLogsPath+"?"+q.Encode(), sessionID)
	if err != nil {
		return nil, err
	}

	var out classroomLogsResp
	if err := json.Unmarshal(resp.Raw, &out); err != nil {
		return nil, fmt.Errorf("%w: 作业列表不是 JSON: %v", entity.ErrPlatformChanged, err)
	}
	if out.Op == "web_redirect" {
		return nil, fmt.Errorf("%w: 作业列表被重定向到登录页", entity.ErrAuthExpired)
	}
	if out.Success != nil && !*out.Success {
		return nil, fmt.Errorf("%w: 作业列表 success=false msg=%s", entity.ErrPlatformChanged, out.Msg)
	}
	return &out, nil
}

// unfinishedLeaves 返回课堂待办列表里未完成叶子的集合
//
// 返回 nil,nil 表示这节课没有需要跟踪状态的学习任务，或缺少 sku 无法查询；
// 返回 nil,err 表示查询失败。两种情况调用方都按 unknown 处理
func (c *Client) unfinishedLeaves(ctx context.Context, item courseItem,
	logs *classroomLogsResp, sessionID string) (map[string]struct{}, error) {
	if !hasLearningTask(logs) {
		return nil, nil
	}
	sku := classroomSku(logs)
	if sku == "" {
		return nil, fmt.Errorf("%w: 课堂缺少 sku_id，无法读取待办列表", entity.ErrPlatformChanged)
	}

	uni := item.UniversityID.String()
	if uni == "" {
		uni = logs.Data.Classroom.UVId.String()
	}
	path := fmt.Sprintf(todoListPathFormat, item.ClassroomID.String(), sku)
	resp, err := c.getWithHeaders(ctx, path, sessionID, map[string]string{
		"Accept":        "application/json, text/plain, */*",
		"xtbz":          "ykt",
		"Xt-Agent":      "web",
		"X-Client":      "web",
		"university-id": uni,
		"uv-id":         uni,
		"classroom-id":  item.ClassroomID.String(),
	})
	if err != nil {
		return nil, err
	}

	var out todoListResp
	if err := json.Unmarshal(resp.Raw, &out); err != nil {
		return nil, fmt.Errorf("%w: 待办列表不是 JSON: %v", entity.ErrPlatformChanged, err)
	}
	if out.Success != nil && !*out.Success {
		return nil, fmt.Errorf("%w: 待办列表 success=false msg=%s", entity.ErrPlatformChanged, out.Msg)
	}
	// total 与 result 长度对不上说明列表被截断，宁可全部保持 unknown
	if total := out.Data.Count.Total.String(); total != "" {
		if n, err := strconv.Atoi(total); err != nil || n != len(out.Data.Result) {
			return nil, fmt.Errorf("%w: 待办列表不完整 total=%s items=%d",
				entity.ErrPlatformChanged, total, len(out.Data.Result))
		}
	}

	unfinished := make(map[string]struct{}, len(out.Data.Result))
	for _, it := range out.Data.Result {
		if leaf := it.LeafID.String(); leaf != "" {
			unfinished[leaf] = struct{}{}
		}
	}
	return unfinished, nil
}

// hasLearningTask 判断这节课有没有需要状态的学习任务
//
// 只认作业类（19/20）与视频类（15/17）；纯课件/授课记录不查待办
func hasLearningTask(logs *classroomLogsResp) bool {
	for _, group := range logs.Data.Activities {
		for _, act := range group {
			if act.Content != nil && (isExerciseType(act.Type) || isVideoType(act.Type)) {
				return true
			}
		}
	}
	return false
}

// classroomSku 优先取课堂级 sku，缺失时退化到活动 content 里的 sku
func classroomSku(logs *classroomLogsResp) string {
	if sku := logs.Data.Classroom.SkuID.String(); sku != "" {
		return sku
	}
	for _, group := range logs.Data.Activities {
		for _, act := range group {
			if act.Content != nil && act.Content.SkuID.String() != "" {
				return act.Content.SkuID.String()
			}
		}
	}
	return ""
}

// selectCurrentSemester 只保留当前学期课程
// 找不到目标学期时退化为列表中最新的学期前缀，并记 warning
func selectCurrentSemester(items []courseItem, now time.Time) ([]courseItem, string, []entity.FetchIssue, bool) {
	target := currentSemester(now)
	matched := filterBySemester(items, target)
	warnings := make([]entity.FetchIssue, 0, 1)
	complete := true

	if len(matched) == 0 {
		latest, ok := latestSemester(items)
		if !ok {
			warnings = append(warnings, entity.FetchIssue{
				Scope:   entity.ScopePlatform,
				Kind:    entity.IssueParse,
				Message: "课程列表中没有可识别的学期前缀（如 2026秋-），本次不做软删除",
			})
			return nil, "", warnings, false
		}
		warnings = append(warnings, entity.FetchIssue{
			Scope:   entity.ScopePlatform,
			Kind:    entity.IssueBusiness,
			Message: fmt.Sprintf("未找到当前学期 %s，退化为列表中最新的 %s", target.Key(), latest.Key()),
		})
		matched = filterBySemester(items, latest)
		target = latest
	}

	return matched, target.Key(), warnings, complete
}

func filterBySemester(items []courseItem, target semester) []courseItem {
	out := make([]courseItem, 0, len(items))
	for _, item := range items {
		s, ok := parseSemester(item.ClassroomName)
		if !ok || s.Key() != target.Key() {
			continue
		}
		out = append(out, item)
	}
	return out
}

// parseClassroomLogs 摊平嵌套 activities 并转成领域对象
// unfinished 是待办列表叶子集合；nil 表示状态不可知
// 返回的 ok 表示本次是否正确解析了全部条目；false 时调用方不做软删除
func parseClassroomLogs(item courseItem, logs *classroomLogsResp,
	unfinished map[string]struct{}) (entity.Course, []entity.Assignment, []entity.FetchIssue, bool) {
	course := entity.Course{
		Platform:    entity.PlatformYuketang,
		ExternalID:  item.CourseID.String(),
		ClassroomID: item.ClassroomID.String(),
		Name:        courseDisplayName(item),
		SemesterKey: semesterKeyOf(item.ClassroomName),
		Active:      true,
	}

	var assignments []entity.Assignment
	var warnings []entity.FetchIssue
	ok := true

	// 不摊平会稳定拿到空列表：这是文档实测复现过的静默失败
	for _, group := range logs.Data.Activities {
		for _, act := range group {
			assignment, warns, valid := toAssignment(item, act, unfinished)
			warnings = append(warnings, warns...)
			if !valid {
				ok = false
			}
			if assignment != nil {
				assignments = append(assignments, *assignment)
			}
		}
	}
	return course, assignments, warnings, ok
}

// 活动类型有两套编码，同一账号的课程里并存
//
// 旧课用 20（作业）/15（视频）；2026-10-25 用户账号「计算机组成原理A」实测新课
// 用 19（习题/练习/作业）和 17（视频），整门课没有一条 20/15。标题能一一对上，
// 所以 19 按作业类、17 按视频类处理，不能再当未知类型
func isExerciseType(t int) bool { return t == 19 || t == 20 }

func isVideoType(t int) bool { return t == 15 || t == 17 }

func isKnownActivityType(t int) bool {
	switch t {
	case 9, 14, 15, 17, 19, 20:
		return true
	}
	return false
}

// toAssignment 把一条活动转成作业；nil 表示不是作业
func toAssignment(item courseItem, act activity,
	unfinished map[string]struct{}) (*entity.Assignment, []entity.FetchIssue, bool) {
	warn := func(msg string) entity.FetchIssue {
		return entity.FetchIssue{
			Scope:   entity.ScopeAssignment,
			Ref:     act.ID.String(),
			Kind:    entity.IssueParse,
			Message: msg,
		}
	}

	if act.Content == nil {
		if isExerciseType(act.Type) {
			// 作业类活动与 content 100% 重合；缺 content 说明结构变了
			return nil, []entity.FetchIssue{warn(fmt.Sprintf("作业活动 %s 缺少 content", act.ID.String()))}, false
		}
		return nil, nil, true
	}

	status := statusOf(act, unfinished)
	var warnings []entity.FetchIssue
	// 视频类（15/17）也是带期限的学习任务，保留且不算未知类型。
	// 2026-10-23 实测 type=15 的 content.score_d 等于课堂 class_end，不是作业截止时间
	if !isKnownActivityType(act.Type) {
		warnings = append(warnings,
			warn(fmt.Sprintf("未知活动类型 type=%d 带 content，请核对是否为作业", act.Type)))
	}

	scoreRaw := act.Content.ScoreD.String()
	if scoreRaw == "" {
		if !isExerciseType(act.Type) {
			// 带 content 的非作业类型（如授课记录）不落库
			return nil, warnings, true
		}
		warnings = append(warnings, warn(fmt.Sprintf("作业活动 %s 缺少 score_d", act.ID.String())))
		return buildAssignment(item, act, nil, entity.DeadlineUnknown, "", status), warnings, false
	}

	if act.ID.String() == "" {
		warnings = append(warnings, warn("作业活动缺少 id"))
		return nil, warnings, false
	}
	if strOnly := act.CreateTime.String(); strOnly == "" {
		warnings = append(warnings, warn(fmt.Sprintf("作业活动 %s 缺少 create_time", act.ID.String())))
	} else if len(strOnly) != 10 {
		warnings = append(warnings, warn(fmt.Sprintf("create_time 不是 10 位秒: %s", strOnly)))
	}

	deadline, ok := parseMilliseconds(act.Content.ScoreD)
	if !ok {
		warnings = append(warnings,
			warn(fmt.Sprintf("score_d 不是 13 位毫秒: %s，已保留作业但截止时间未知", scoreRaw)))
		return buildAssignment(item, act, nil, entity.DeadlineUnknown, scoreRaw, status), warnings, false
	}

	return buildAssignment(item, act, &deadline, entity.DeadlinePlatformExact, scoreRaw, status), warnings, true
}

// statusOf 用课堂待办列表换算提交状态
//
// 2026-10-25 实测：待办里的 6 条习题全都未答完（含 2/4 部分作答），
// 已全部答完的 16 条都不在列表里。视频同理，但「看完」不等同「提交」，
// 不在待办的视频保持 unknown，避免把学习进度说成交卷
func statusOf(act activity, unfinished map[string]struct{}) entity.SubmissionStatus {
	if unfinished == nil || act.Content == nil {
		return entity.StatusUnknown
	}
	if !isExerciseType(act.Type) && !isVideoType(act.Type) {
		return entity.StatusUnknown
	}
	leaf := act.Content.LeafID.String()
	if leaf == "" {
		return entity.StatusUnknown
	}
	if _, ok := unfinished[leaf]; ok {
		return entity.StatusUnsubmitted
	}
	if isExerciseType(act.Type) {
		return entity.StatusSubmitted
	}
	return entity.StatusUnknown
}

func buildAssignment(item courseItem, act activity, deadline *time.Time,
	source entity.DeadlineSource, raw string, status entity.SubmissionStatus) *entity.Assignment {
	courseID := item.CourseID.String()
	classroomID := item.ClassroomID.String()
	activityID := act.ID.String()
	return &entity.Assignment{
		Platform:         entity.PlatformYuketang,
		CourseExternalID: courseID,
		ClassroomID:      classroomID,
		CourseName:       courseDisplayName(item),
		ExternalID:       activityID,
		DedupKey:         entity.YuketangDedupKey(courseID, classroomID, activityID),
		Title:            entity.NormalizeTitle(act.Title),
		Status:           status,
		Deadline:         deadline,
		DeadlineSource:   source,
		DeadlineRaw:      raw,
	}
}

func courseDisplayName(item courseItem) string {
	if item.CourseName != "" {
		return item.CourseName
	}
	return item.ClassroomName
}

func semesterKeyOf(classroomName string) string {
	if s, ok := parseSemester(classroomName); ok {
		return s.Key()
	}
	return ""
}

// parseMilliseconds 断言 13 位，拿错会得到 1970 年
func parseMilliseconds(n num) (time.Time, bool) {
	raw := n.String()
	if len(raw) != 13 {
		return time.Time{}, false
	}
	ms, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	return time.UnixMilli(ms).UTC(), true
}

// parseSeconds 断言 10 位
func parseSeconds(n num) (time.Time, bool) {
	raw := n.String()
	if len(raw) != 10 {
		return time.Time{}, false
	}
	sec, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	return time.Unix(sec, 0).UTC(), true
}

func issueKindOf(err error) entity.IssueKind {
	switch {
	case errors.Is(err, entity.ErrNetwork):
		return entity.IssueNetwork
	case errors.Is(err, entity.ErrAuthExpired):
		return entity.IssueAuth
	case errors.Is(err, entity.ErrCanceled):
		return entity.IssueCanceled
	case errors.Is(err, entity.ErrPlatformChanged):
		return entity.IssueParse
	default:
		return entity.IssueInternal
	}
}
