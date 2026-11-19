package handle

import (
	"fmt"

	"catcher/internal/domain/entity"
	"catcher/internal/dto"
)

// StartRefresh 启动刷新任务；platforms 为空表示全部平台
func (a *App) StartRefresh(platforms []string) (res dto.RefreshStartResult) {
	defer func() {
		if r := recover(); r != nil {
			res = dto.RefreshStartResult{
				OK:    false,
				Error: &dto.APIError{Code: "INTERNAL", Message: "启动刷新时发生内部错误", Detail: fmt.Sprint(r)},
			}
		}
	}()

	ps := make([]entity.Platform, 0, len(platforms))
	for _, raw := range platforms {
		p, err := entity.ParsePlatform(raw)
		if err != nil {
			return dto.RefreshStartResult{
				OK:    false,
				Error: &dto.APIError{Code: "INVALID_ARGUMENT", Message: "平台参数无效", Detail: err.Error()},
			}
		}
		ps = append(ps, p)
	}

	start, err := a.svc.StartRefresh(a.context(), ps)
	if err != nil {
		return dto.RefreshStartResult{
			OK:    false,
			Error: &dto.APIError{Code: "REFRESH_FAILED", Message: "启动刷新失败", Detail: err.Error()},
		}
	}
	return dto.RefreshStartResult{
		OK:             true,
		JobID:          start.JobID,
		Platforms:      platformStrings(start.Platforms),
		AlreadyRunning: start.AlreadyRunning,
	}
}

// CancelRefresh 取消一个刷新任务
func (a *App) CancelRefresh(jobID string) (res dto.APIResult) {
	defer func() {
		if r := recover(); r != nil {
			res = dto.APIResult{
				OK:    false,
				Error: &dto.APIError{Code: "INTERNAL", Message: "取消刷新时发生内部错误", Detail: fmt.Sprint(r)},
			}
		}
	}()

	if jobID == "" {
		return dto.APIResult{OK: false, Error: &dto.APIError{Code: "INVALID_ARGUMENT", Message: "缺少任务 id"}}
	}
	if err := a.svc.CancelRefresh(jobID); err != nil {
		return dto.APIResult{
			OK:    false,
			Error: &dto.APIError{Code: "REFRESH_NOT_FOUND", Message: "刷新任务不存在或已结束", Detail: err.Error()},
		}
	}
	return dto.APIResult{OK: true}
}

func platformStrings(platforms []entity.Platform) []string {
	out := make([]string, len(platforms))
	for i, p := range platforms {
		out[i] = string(p)
	}
	return out
}
