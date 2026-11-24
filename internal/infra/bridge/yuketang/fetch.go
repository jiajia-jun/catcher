package yuketang

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"catcher/internal/domain/entity"
)

const (
	learningListPath  = "/v/course_meta/learning_list/"
	classroomLogsPath = "/v/course_meta/classroom_logs"

	// activityTypeQuiz 是已确认网页路由的习题类型
	// type 19 与视频 15/17 的链接规则尚未确认
	activityTypeQuiz = 20
)

// Fetch 抓取当前学期的课程与作业
//
// 平台只提供活动数据，提交状态统一记为 unknown
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
		// 学期前缀全部缺失时保留旧数据，跳过软删除
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
			// 认证失效或取消时立即中止，本次结果丢弃
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

		course, assignments, parseWarnings, ok := parseClassroomLogs(c.baseURL, item, logs)
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
	// 登录态以响应体的 op 字段为准
	if out.Op == "web_redirect" {
		return nil, fmt.Errorf("%w: 课程列表被重定向到登录页", entity.ErrAuthExpired)
	}
	// 课程列表可能缺少 success 字段，「缺失」按成功处理
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

// selectCurrentSemester 只保留当前学期课程
// 目标学期缺失时退化为列表中最新的学期前缀，并记 warning
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
// ok 表示本次全部条目解析成功，false 时调用方跳过软删除
func parseClassroomLogs(baseURL string, item courseItem, logs *classroomLogsResp) (entity.Course, []entity.Assignment, []entity.FetchIssue, bool) {
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

	// activities 是嵌套数组，摊平后才能取到条目
	for _, group := range logs.Data.Activities {
		for _, act := range group {
			assignment, warns, valid := toAssignment(item, act)
			warnings = append(warnings, warns...)
			if !valid {
				ok = false
			}
			if assignment != nil {
				// type 20 的网页路由已验证，其他类型保留空链接
				if act.Type == activityTypeQuiz && act.Content != nil {
					assignment.DetailURL = quizDetailURL(
						baseURL, item.ClassroomID.String(), act.Content.LeafID.String())
				}
				assignments = append(assignments, *assignment)
			}
		}
	}
	return course, assignments, warnings, ok
}

// quizDetailURL 只处理已经验证过的 type 20 习题
func quizDetailURL(baseURL, classroomID, leafID string) string {
	if baseURL == "" || classroomID == "" || leafID == "" {
		return ""
	}
	return strings.TrimRight(baseURL, "/") + "/ai-workspace/lms-graph/" +
		classroomID + "/quiz/" + leafID + "?is_chapter=1"
}

// 活动类型有两套编码，同一账号的课程里并存
//
// 旧课使用 20（作业）与 15（视频），新课使用 19（习题/练习/作业）与 17（视频）
// 标题能一一对应，19 按作业类、17 按视频类处理
func isExerciseType(t int) bool { return t == 19 || t == 20 }

func isKnownActivityType(t int) bool {
	switch t {
	case 9, 14, 15, 17, 19, 20:
		return true
	}
	return false
}

// toAssignment 把一条活动转成作业，nil 表示无需落库的活动
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
		if isExerciseType(act.Type) {
			// 作业类活动都带 content，content 缺失说明响应结构变化
			return nil, []entity.FetchIssue{warn(fmt.Sprintf("作业活动 %s 缺少 content", act.ID.String()))}, false
		}
		return nil, nil, true
	}

	var warnings []entity.FetchIssue
	// 视频类（15/17）是带期限的学习任务，保留且计入已知类型
	// type=15 的 content.score_d 是课堂 class_end，与作业截止时间无关
	if !isKnownActivityType(act.Type) {
		warnings = append(warnings,
			warn(fmt.Sprintf("未知活动类型 type=%d 带 content，请核对是否为作业", act.Type)))
	}

	scoreRaw := act.Content.ScoreD.String()
	if scoreRaw == "" {
		if !isExerciseType(act.Type) {
			// 带 content 的非作业类型（如授课记录）跳过落库
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
		// 平台提供活动数据，提交状态统一记为 unknown
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

// parseMilliseconds 断言 13 位，位数不符会解出 1970 年
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
