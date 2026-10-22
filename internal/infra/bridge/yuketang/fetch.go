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
)

// Fetch 抓取当前学期的课程与作业
//
// 提交状态平台没有暴露，全部是 unknown；绝不推断成已交
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

		course, assignments, parseWarnings, ok := parseClassroomLogs(item, logs)
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
	if !out.Success {
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
	if !out.Success {
		return nil, fmt.Errorf("%w: 作业列表 success=false msg=%s", entity.ErrPlatformChanged, out.Msg)
	}
	return &out, nil
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
// 返回的 ok 表示本次是否正确解析了全部条目；false 时调用方不做软删除
func parseClassroomLogs(item courseItem, logs *classroomLogsResp) (entity.Course, []entity.Assignment, []entity.FetchIssue, bool) {
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
			assignment, warns, valid := toAssignment(item, act)
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

// toAssignment 把一条活动转成作业；nil 表示不是作业
func toAssignment(item courseItem, act activity) (*entity.Assignment, []entity.FetchIssue, bool) {
	warn := func(msg string) entity.FetchIssue {
		return entity.FetchIssue{
			Scope:   entity.ScopeAssignment,
			Ref:     act.ID.String(),
			Kind:    entity.IssueParse,
			Message: msg,
		}
	}

	if act.Content == nil {
		if act.Type == 20 {
			// 文档中 type=20 与 content 100% 重合；缺 content 说明结构变了
			return nil, []entity.FetchIssue{warn(fmt.Sprintf("作业活动 %s 缺少 content", act.ID.String()))}, false
		}
		return nil, nil, true
	}

	var warnings []entity.FetchIssue
	if act.Type != 9 && act.Type != 14 && act.Type != 20 {
		warnings = append(warnings,
			warn(fmt.Sprintf("未知活动类型 type=%d 带 content，请核对是否为作业", act.Type)))
	}

	scoreRaw := act.Content.ScoreD.String()
	if scoreRaw == "" {
		if act.Type != 20 {
			// 带 content 的非作业类型（如授课记录）不落库
			return nil, warnings, true
		}
		warnings = append(warnings, warn(fmt.Sprintf("作业活动 %s 缺少 score_d", act.ID.String())))
		return buildAssignment(item, act, nil, entity.DeadlineUnknown, ""), warnings, false
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
		return buildAssignment(item, act, nil, entity.DeadlineUnknown, scoreRaw), warnings, false
	}

	return buildAssignment(item, act, &deadline, entity.DeadlinePlatformExact, scoreRaw), warnings, true
}

func buildAssignment(item courseItem, act activity, deadline *time.Time,
	source entity.DeadlineSource, raw string) *entity.Assignment {
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
		// 平台没有提交状态字段；未知就是未知
		Status:         entity.StatusUnknown,
		Deadline:       deadline,
		DeadlineSource: source,
		DeadlineRaw:    raw,
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
func parseMilliseconds(n json.Number) (time.Time, bool) {
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
func parseSeconds(n json.Number) (time.Time, bool) {
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
