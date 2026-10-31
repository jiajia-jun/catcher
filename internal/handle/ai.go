package handle

import (
	"encoding/json"
	"fmt"

	"catcher/internal/domain/entity"
	"catcher/internal/dto"
	"catcher/internal/service"
)

func NewWithAI(svc *service.Service, ai *service.AIService) *App {
	app := New(svc)
	app.ai = ai
	return app
}
func aiResult(value any, err error) dto.AIResult {
	if err != nil {
		return dto.AIResult{Error: &dto.APIError{Code: "AI", Message: err.Error()}}
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return dto.AIResult{Error: &dto.APIError{Code: "AI", Message: "AI 展示数据转换失败"}}
	}
	var data map[string]any
	if err = json.Unmarshal(raw, &data); err != nil {
		return dto.AIResult{Error: &dto.APIError{Code: "AI", Message: "AI 展示数据结构无效"}}
	}
	return dto.AIResult{OK: true, Data: data}
}
func (a *App) GetAISettings() dto.AIResult {
	if a.ai == nil {
		return aiResult(nil, fmt.Errorf("AI 功能未装配"))
	}
	v, e := a.ai.Settings(a.context())
	return aiResult(v, e)
}
func (a *App) SaveAISettings(req dto.AISettingsRequest) dto.AIResult {
	if a.ai == nil {
		return aiResult(nil, fmt.Errorf("AI 功能未装配"))
	}
	cfg := entity.AISettings{Enabled: req.Enabled, BaseURL: req.BaseURL, Model: req.Model, MaxInputChars: req.MaxInputChars, MaxOutputTokens: req.MaxOutputTokens, MonthlyTokenLimit: req.MonthlyTokenLimit, TimeoutSeconds: req.TimeoutSeconds}
	if e := a.ai.SaveSettings(a.context(), cfg, req.APIKey, req.ClearKey); e != nil {
		return aiResult(nil, e)
	}
	return a.GetAISettings()
}
func aiInput(req dto.AIInput) entity.AIInput {
	out := entity.AIInput{Kind: req.Kind, Question: req.Question, AssignmentIDs: req.AssignmentIDs, StartAt: req.StartAt, EndAt: req.EndAt, Snapshot: req.Snapshot, Blocked: []entity.AITimeRange{}}
	for _, b := range req.Blocked {
		out.Blocked = append(out.Blocked, entity.AITimeRange{StartAt: b.StartAt, EndAt: b.EndAt})
	}
	return out
}
func (a *App) PreviewAI(req dto.AIInput) dto.AIResult {
	if a.ai == nil {
		return aiResult(nil, fmt.Errorf("AI 功能未装配"))
	}
	v, e := a.ai.Preview(a.context(), aiInput(req))
	return aiResult(v, e)
}
func (a *App) StartAI(req dto.AIInput) dto.AIResult {
	if a.ai == nil {
		return aiResult(nil, fmt.Errorf("AI 功能未装配"))
	}
	id, e := a.ai.Start(a.context(), aiInput(req))
	return aiResult(map[string]any{"jobId": id}, e)
}
func (a *App) GetAIJob(id string) dto.AIResult {
	if a.ai == nil {
		return aiResult(nil, fmt.Errorf("AI 功能未装配"))
	}
	v, e := a.ai.Job(id)
	return aiResult(v, e)
}
func (a *App) CancelAI(id string) dto.AIResult {
	if a.ai == nil {
		return aiResult(nil, fmt.Errorf("AI 功能未装配"))
	}
	e := a.ai.Cancel(id)
	return aiResult(map[string]any{}, e)
}
func (a *App) ConfirmStudyPlan(req dto.AIConfirmRequest) dto.AIResult {
	if a.ai == nil {
		return aiResult(nil, fmt.Errorf("AI 功能未装配"))
	}
	tasks := []entity.StudyTask{}
	for _, t := range req.Tasks {
		tasks = append(tasks, entity.StudyTask{ID: t.ID, AssignmentID: t.AssignmentID, Title: t.Title, StartAt: t.StartAt, Minutes: t.Minutes, Steps: t.Steps, Reason: t.Reason})
	}
	v, e := a.ai.Confirm(a.context(), req.JobID, tasks)
	return aiResult(v, e)
}
func (a *App) ListStudyPlans() dto.AIResult {
	if a.ai == nil {
		return aiResult(nil, fmt.Errorf("AI 功能未装配"))
	}
	v, e := a.ai.Plans(a.context())
	return aiResult(map[string]any{"plans": v}, e)
}
func (a *App) SetStudyTaskDone(req dto.AICompleteRequest) dto.AIResult {
	if a.ai == nil {
		return aiResult(nil, fmt.Errorf("AI 功能未装配"))
	}
	e := a.ai.SetTaskDone(a.context(), req.PlanID, req.TaskID, req.Done)
	return aiResult(map[string]any{}, e)
}
