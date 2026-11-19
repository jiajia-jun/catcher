package handle

import (
	"errors"
	"fmt"
	"time"

	"catcher/internal/domain/entity"
	"catcher/internal/dto"
)

// ListAssignments 返回统一作业表的数据
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

// SetAssignmentStatus 手动标记一条作业的状态，空状态表示恢复平台值
func (a *App) SetAssignmentStatus(req dto.AssignmentStatusRequest) (res dto.AssignmentStatusResult) {
	defer func() {
		if r := recover(); r != nil {
			res = dto.AssignmentStatusResult{
				OK:    false,
				Error: &dto.APIError{Code: "INTERNAL", Message: "更新作业状态时发生内部错误", Detail: fmt.Sprint(r)},
			}
		}
	}()

	if req.ID <= 0 {
		return dto.AssignmentStatusResult{
			OK:    false,
			Error: &dto.APIError{Code: "INVALID_ARGUMENT", Message: "缺少作业 id"},
		}
	}

	var status *entity.SubmissionStatus
	if req.Status != "" {
		s := entity.SubmissionStatus(req.Status)
		if s != entity.StatusSubmitted && s != entity.StatusUnsubmitted {
			return dto.AssignmentStatusResult{
				OK:    false,
				Error: &dto.APIError{Code: "INVALID_ARGUMENT", Message: "状态只能是已提交或未提交", Detail: req.Status},
			}
		}
		status = &s
	}

	item, err := a.svc.SetAssignmentStatus(a.context(), req.ID, status)
	if err != nil {
		if errors.Is(err, entity.ErrNotFound) {
			return dto.AssignmentStatusResult{
				OK:    false,
				Error: &dto.APIError{Code: "NOT_FOUND", Message: "作业不存在或已被删除"},
			}
		}
		return dto.AssignmentStatusResult{
			OK:    false,
			Error: &dto.APIError{Code: "STORAGE", Message: "更新作业状态失败", Detail: err.Error()},
		}
	}

	view := toAssignmentView(item, time.Now())
	return dto.AssignmentStatusResult{OK: true, Item: &view}
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
