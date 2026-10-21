// Package handle 是 Wails 绑定层：参数校验、错误翻译、DTO 转换。
//
// 它不直接访问数据库或 HTTP；所有用例都经过 service。
package handle

import (
	"context"
	"fmt"
	"time"

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
}

func (a *App) context() context.Context {
	if a.ctx == nil {
		return context.Background()
	}
	return a.ctx
}

// GetBootstrap 返回平台卡、统计、数据目录与版本。
func (a *App) GetBootstrap() (res dto.BootstrapResult) {
	defer func() {
		if r := recover(); r != nil {
			res = dto.BootstrapResult{
				OK:    false,
				Error: &dto.APIError{Code: "INTERNAL", Message: "读取应用状态时发生内部错误", Detail: fmt.Sprint(r)},
			}
		}
	}()

	b, err := a.svc.Bootstrap(a.context())
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

func toEntityFilter(f dto.AssignmentFilter) (entity.AssignmentFilter, *dto.APIError) {
	out := entity.AssignmentFilter{
		ShowSubmitted: f.ShowSubmitted,
		Keyword:       f.Keyword,
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
