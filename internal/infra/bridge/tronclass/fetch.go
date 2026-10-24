package tronclass

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"catcher/internal/domain/entity"
)

const (
	coursesPath      = "/api/my-courses"
	semestersPath    = "/api/my-semesters"
	coursePageSize   = 100
	homeworkPageSize = 100
	// maxCoursePages 与 maxHomeworkPages 防止平台异常时无限翻页
	maxCoursePages   = 50
	maxHomeworkPages = 100
	// courseFields 显式声明字段，带 page 时才能拿到完整学期信息
	courseFields = "id,name,course_code,semester,academic_year,instructors,department"
)

// Fetch 抓取当前学期的课程与逐课作业
//
// 学期 id 来自 my-semesters 的 is_active；课程与作业都按 pages 终止，
// 单课失败记 warning 后继续，认证失效与取消立即中止
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

	state := &fetchState{
		client:  c,
		session: secret.Session,
		uid:     secret.UserID,
		opts:    opts,
	}

	progress(entity.Progress{Platform: entity.PlatformTronclass, Phase: "courses", Message: "读取当前学期"})
	semesterID, err := state.activeSemester(ctx)
	if err != nil {
		return nil, err
	}

	progress(entity.Progress{Platform: entity.PlatformTronclass, Phase: "courses", Message: "读取课程列表"})
	courses, courseListComplete, warnings, err := state.listCourses(ctx, semesterID)
	if err != nil {
		return nil, err
	}

	failed := make([]string, 0)
	var assignments []entity.Assignment
	for i, course := range courses {
		progress(entity.Progress{
			Platform: entity.PlatformTronclass,
			Phase:    "assignments",
			Current:  i + 1,
			Total:    len(courses),
			Message:  course.Name,
		})

		items, warns, err := state.listAssignments(ctx, course)
		if err != nil {
			// 认证失效、取消是整次抓取的问题，本次结果不落库
			if errors.Is(err, entity.ErrAuthExpired) || errors.Is(err, entity.ErrCanceled) {
				return nil, err
			}
			failed = append(failed, course.ExternalID)
			warnings = append(warnings, entity.FetchIssue{
				Scope:   entity.ScopeCourse,
				Ref:     course.ExternalID,
				Kind:    issueKindOf(err),
				Message: fmt.Sprintf("课程 %q 作业列表抓取失败: %v", course.Name, err),
			})
			continue
		}
		warnings = append(warnings, warns...)
		assignments = append(assignments, items...)
	}

	result := &entity.FetchResult{
		Courses:            courses,
		Assignments:        assignments,
		Warnings:           warnings,
		CourseListComplete: courseListComplete,
		FailedCourses:      failed,
	}
	if !courseListComplete {
		// nil 表示课程列表不完整，落库时不做任何软删除
		result.FailedCourses = nil
	}
	result.Complete = courseListComplete && len(result.FailedCourses) == 0

	progress(entity.Progress{
		Platform: entity.PlatformTronclass,
		Phase:    "done",
		Current:  len(courses),
		Total:    len(courses),
		Message:  "抓取完成",
	})
	return result, nil
}

// fetchState 维护单次抓取内轮换后的 session
//
// 学在重邮每个 200 都可能下发新 session；只持久化成功响应里的值
type fetchState struct {
	client  *Client
	session string
	uid     string
	opts    entity.FetchOptions
}

// doJSON 发送请求，每个 200 后立即把会话交给上层保存
func (s *fetchState) doJSON(ctx context.Context, method, path string, body []byte, out any) error {
	resp, err := s.client.do(ctx, method, path, body, s.session)
	if err != nil {
		return err
	}
	if rotated := sessionFromSetCookies(resp.SetCookies); rotated != "" {
		s.session = rotated
	}
	if err := s.persist(); err != nil {
		return fmt.Errorf("保存学在重邮会话失败: %w", err)
	}
	if !isJSONResponse(resp) {
		return fmt.Errorf("%w: 响应不是 JSON", entity.ErrPlatformChanged)
	}
	if out != nil {
		if err := json.Unmarshal(resp.Raw, out); err != nil {
			return fmt.Errorf("%w: 解析响应失败: %v", entity.ErrPlatformChanged, err)
		}
	}
	return nil
}

func (s *fetchState) persist() error {
	if s.opts.OnSessionUpdated == nil {
		return nil
	}
	updated, err := s.client.newSession(s.session, s.uid)
	if err != nil {
		return err
	}
	return s.opts.OnSessionUpdated(*updated)
}

// activeSemester 取唯一的 is_active 学期；0 个或多个都显式报错
func (s *fetchState) activeSemester(ctx context.Context) (int64, error) {
	var data semesterList
	if err := s.doJSON(ctx, http.MethodGet, semestersPath, nil, &data); err != nil {
		return 0, err
	}

	var ids []int64
	for _, item := range data.Semesters {
		if item.IsActive && item.ID > 0 {
			ids = append(ids, item.ID)
		}
	}
	switch len(ids) {
	case 1:
		return ids[0], nil
	case 0:
		return 0, fmt.Errorf("%w: 没有找到当前学期", entity.ErrPlatformChanged)
	default:
		return 0, fmt.Errorf("%w: 返回了 %d 个当前学期", entity.ErrPlatformChanged, len(ids))
	}
}

// listCourses 按学期 id 枚举课程；semester 为 null 的课程跳过
func (s *fetchState) listCourses(ctx context.Context, semesterID int64) (
	[]entity.Course, bool, []entity.FetchIssue, error) {
	courses := make([]entity.Course, 0)
	warnings := make([]entity.FetchIssue, 0)
	complete := true

	for page := 1; ; page++ {
		if page > maxCoursePages {
			warnings = append(warnings, entity.FetchIssue{
				Scope:   entity.ScopePlatform,
				Kind:    entity.IssueParse,
				Message: fmt.Sprintf("课程列表超过 %d 页，已停止翻页", maxCoursePages),
			})
			complete = false
			break
		}

		body, err := json.Marshal(courseQuery{
			Page:       page,
			PageSize:   coursePageSize,
			Fields:     courseFields,
			Conditions: courseConditions{SemesterID: semesterID},
		})
		if err != nil {
			return nil, false, warnings, fmt.Errorf("构造课程查询失败: %w", err)
		}

		var data coursePage
		if err := s.doJSON(ctx, http.MethodPost, coursesPath, body, &data); err != nil {
			return nil, false, warnings, err
		}

		for _, item := range data.Courses {
			if item.ID <= 0 {
				complete = false
				warnings = append(warnings, entity.FetchIssue{
					Scope:   entity.ScopeCourse,
					Kind:    entity.IssueParse,
					Message: "课程缺少有效 id，已跳过",
				})
				continue
			}
			externalID := strconv.FormatInt(item.ID, 10)
			if item.Semester == nil {
				warnings = append(warnings, entity.FetchIssue{
					Scope:   entity.ScopeCourse,
					Ref:     externalID,
					Kind:    entity.IssueParse,
					Message: fmt.Sprintf("课程 %q 缺少学期信息，已跳过", item.Name),
				})
				continue
			}
			name := strings.TrimSpace(item.Name)
			if name == "" {
				name = "课程 " + externalID
			}
			courses = append(courses, entity.Course{
				Platform:    entity.PlatformTronclass,
				ExternalID:  externalID,
				Name:        name,
				SemesterKey: item.Semester.Code,
				Active:      true,
			})
		}

		// 空课程列表返回 pages=0；越界页会重复最后一页，不能用空列表终止
		if page >= data.Pages {
			break
		}
	}
	return courses, complete, warnings, nil
}

// listAssignments 枚举一门课的作业；页数超限按失败处理
func (s *fetchState) listAssignments(ctx context.Context, course entity.Course) (
	[]entity.Assignment, []entity.FetchIssue, error) {
	assignments := make([]entity.Assignment, 0)
	warnings := make([]entity.FetchIssue, 0)

	for page := 1; ; page++ {
		if page > maxHomeworkPages {
			return nil, nil, fmt.Errorf("%w: 作业列表超过 %d 页", entity.ErrPlatformChanged, maxHomeworkPages)
		}

		path := fmt.Sprintf("/api/courses/%s/homework-activities?page=%d&page_size=%d",
			course.ExternalID, page, homeworkPageSize)
		var data homeworkPage
		if err := s.doJSON(ctx, http.MethodGet, path, nil, &data); err != nil {
			return nil, nil, err
		}

		for _, activity := range data.Activities {
			item, warns, ok := parseActivity(course, activity)
			warnings = append(warnings, warns...)
			if ok {
				assignments = append(assignments, item)
			}
		}

		if page >= data.Pages {
			break
		}
	}
	return assignments, warnings, nil
}

// parseActivity 把一条作业映射成领域对象；无法落库的条目跳过并写 warning
func parseActivity(course entity.Course, activity homeworkActivity) (
	entity.Assignment, []entity.FetchIssue, bool) {
	if activity.ID <= 0 {
		return entity.Assignment{}, []entity.FetchIssue{{
			Scope:   entity.ScopeCourse,
			Ref:     course.ExternalID,
			Kind:    entity.IssueParse,
			Message: "作业缺少有效 id，已跳过",
		}}, false
	}
	externalID := strconv.FormatInt(activity.ID, 10)
	warnings := make([]entity.FetchIssue, 0)

	title := entity.NormalizeTitle(activity.Title)
	if title == "" {
		title = "作业 " + externalID
		warnings = append(warnings, entity.FetchIssue{
			Scope:   entity.ScopeAssignment,
			Ref:     course.ExternalID,
			Kind:    entity.IssueParse,
			Message: fmt.Sprintf("作业 %s 缺少标题", externalID),
		})
	}

	status := entity.StatusUnknown
	switch {
	case activity.Submitted.Set && activity.Submitted.Value:
		status = entity.StatusSubmitted
	case activity.Submitted.Set:
		status = entity.StatusUnsubmitted
	default:
		warnings = append(warnings, entity.FetchIssue{
			Scope:   entity.ScopeAssignment,
			Ref:     course.ExternalID,
			Kind:    entity.IssueParse,
			Message: fmt.Sprintf("作业 %q 缺少提交状态，按未知处理", title),
		})
	}

	var deadline *time.Time
	source := entity.DeadlineUnknown
	raw := strings.TrimSpace(activity.Deadline)
	if raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			warnings = append(warnings, entity.FetchIssue{
				Scope:   entity.ScopeAssignment,
				Ref:     course.ExternalID,
				Kind:    entity.IssueParse,
				Message: fmt.Sprintf("作业 %q 截止时间无法解析: %s", title, raw),
			})
		} else {
			utc := parsed.UTC()
			deadline = &utc
			source = entity.DeadlinePlatformExact
		}
	}

	return entity.Assignment{
		Platform:         entity.PlatformTronclass,
		CourseExternalID: course.ExternalID,
		CourseName:       course.Name,
		ExternalID:       externalID,
		DedupKey:         entity.TronclassDedupKey(course.ExternalID, externalID),
		Title:            title,
		Status:           status,
		Deadline:         deadline,
		DeadlineSource:   source,
		DeadlineRaw:      raw,
	}, warnings, true
}
