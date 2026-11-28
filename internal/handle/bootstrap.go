package handle

import (
	"fmt"
	"time"

	"catcher/internal/domain/entity"
	"catcher/internal/dto"
	"catcher/internal/service"
)

// GetBootstrap 返回平台卡、统计、数据目录与版本
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
			Experimental: p.Experimental(),
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

func countsView(c service.Counts) dto.CountsView {
	return dto.CountsView{
		OpenUnsubmitted:    c.OpenUnsubmitted,
		OverdueUnsubmitted: c.OverdueUnsubmitted,
		Unknown:            c.Unknown,
		Submitted:          c.Submitted,
	}
}
