package chaoxing

import (
	"context"
	"errors"
	"fmt"
	"time"

	"catcher/internal/domain/entity"
)

// Fetch 抓取学习通的课程与作业
//
// 学习通是全局作业列表，没有学期概念；课程列表只用于补全被截断的课程名
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
	now := c.now()
	if opts.Now != nil {
		now = opts.Now()
	}

	jar := newCookieJar(secret.Cookies)

	progress(entity.Progress{Platform: entity.PlatformChaoxing, Phase: "courses", Message: "读取课程列表"})
	entries, err := c.fetchCourseList(ctx, jar)
	if err != nil {
		return nil, err
	}
	// 登录链路过 302 时可能有新的 cookie；课程列表拿到后立即落盘
	if err := c.persistCookies(jar, opts); err != nil {
		return nil, fmt.Errorf("保存学习通 cookie 失败: %w", err)
	}

	names := courseNameMap(entries)
	result := &entity.FetchResult{
		Courses:  toCourses(entries),
		Complete: true,
	}

	progress(entity.Progress{Platform: entity.PlatformChaoxing, Phase: "assignments", Message: "读取作业列表"})
	page, err := c.fetchWorkList(ctx, jar)
	if err != nil {
		// 登录失效/取消立即中止，本次结果不落库
		return nil, err
	}
	result.Complete = result.Complete && page.Complete
	result.Warnings = append(result.Warnings, page.Warnings...)

	for i, item := range page.Items {
		progress(entity.Progress{
			Platform: entity.PlatformChaoxing,
			Phase:    "assignments",
			Current:  i + 1,
			Total:    len(page.Items),
			Message:  item.Title,
		})

		var detail *detailDeadline
		if item.Status != entity.StatusSubmitted {
			body, err := c.fetchDetail(ctx, item, jar)
			if err != nil {
				// 认证失效/取消是整次抓取的问题，其余错误只影响这一条
				if errors.Is(err, entity.ErrAuthExpired) || errors.Is(err, entity.ErrCanceled) {
					return nil, err
				}
				result.Warnings = append(result.Warnings, entity.FetchIssue{
					Scope:   entity.ScopeAssignment,
					Ref:     item.CourseID,
					Kind:    issueKindOf(err),
					Message: fmt.Sprintf("作业 %q 详情抓取失败: %v", item.Title, err),
				})
			} else {
				detail = parseDetailDeadline(body)
				// 有剩余时间的条目，截止时刻由「now + 剩余」决定；
				// 详情页没有截止时间是常态，不算问题
				if detail == nil && !item.HasRemaining {
					result.Warnings = append(result.Warnings, entity.FetchIssue{
						Scope:   entity.ScopeAssignment,
						Ref:     item.CourseID,
						Kind:    entity.IssueParse,
						Message: fmt.Sprintf("作业 %q 详情页没有截止时间", item.Title),
					})
				}
			}
		}

		var deadline *time.Time
		var source entity.DeadlineSource
		var raw string
		if item.Status != entity.StatusSubmitted || item.HasRemaining {
			var warns []entity.FetchIssue
			deadline, source, raw, warns = resolveDeadline(item, detail, now)
			result.Warnings = append(result.Warnings, warns...)
		} else {
			// 已完成条目常常拿不到截止时间，按未知保留，不猜测
			source = entity.DeadlineUnknown
		}

		courseName := names[item.CourseID]
		if courseName == "" {
			courseName = item.CourseName
			if courseName == "" {
				courseName = "课程 " + item.CourseID
			}
			result.Warnings = append(result.Warnings, entity.FetchIssue{
				Scope:   entity.ScopeAssignment,
				Ref:     item.CourseID,
				Kind:    entity.IssueParse,
				Message: fmt.Sprintf("课程 %s 未在课程列表中找到，使用列表页名称 %q", item.CourseID, courseName),
			})
		}

		result.Assignments = append(result.Assignments, entity.Assignment{
			Platform:         entity.PlatformChaoxing,
			CourseExternalID: item.CourseID,
			CourseName:       courseName,
			DedupKey:         entity.ChaoxingDedupKey(item.CourseID, item.Title),
			Title:            item.Title,
			Status:           item.Status,
			Deadline:         deadline,
			DeadlineSource:   source,
			DeadlineRaw:      raw,
			DetailURL:        item.DetailURL,
		})
	}

	// 逐条详情也可能轮换 cookie，结束时再落盘一次
	if err := c.persistCookies(jar, opts); err != nil {
		return nil, fmt.Errorf("保存学习通 cookie 失败: %w", err)
	}

	progress(entity.Progress{
		Platform: entity.PlatformChaoxing,
		Phase:    "done",
		Current:  len(page.Items),
		Total:    len(page.Items),
		Message:  "抓取完成",
	})
	return result, nil
}
