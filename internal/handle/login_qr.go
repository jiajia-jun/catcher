package handle

import (
	"context"
	"fmt"

	"catcher/internal/dto"
	"catcher/internal/service"
)

// 四个平台的扫码流程完全一致，只有平台名不同。
// 导出方法保持独立以便 Wails 绑定；recover、参数校验与错误翻译收敛在下面三个 helper

func (a *App) startQRLogin(name string, start func(context.Context) (*service.RefreshStart, error)) (res dto.RefreshStartResult) {
	defer func() {
		if r := recover(); r != nil {
			res = dto.RefreshStartResult{
				OK:    false,
				Error: &dto.APIError{Code: "INTERNAL", Message: "启动扫码登录时发生内部错误", Detail: fmt.Sprint(r)},
			}
		}
	}()

	started, err := start(a.context())
	if err != nil {
		return dto.RefreshStartResult{
			OK:    false,
			Error: &dto.APIError{Code: "LOGIN_FAILED", Message: "启动" + name + "扫码登录失败", Detail: err.Error()},
		}
	}
	return dto.RefreshStartResult{
		OK:        true,
		JobID:     started.JobID,
		Platforms: platformStrings(started.Platforms),
	}
}

func (a *App) cancelQRLogin(jobID string, cancel func(string) error) (res dto.APIResult) {
	defer func() {
		if r := recover(); r != nil {
			res = dto.APIResult{
				OK:    false,
				Error: &dto.APIError{Code: "INTERNAL", Message: "取消扫码登录时发生内部错误", Detail: fmt.Sprint(r)},
			}
		}
	}()

	if jobID == "" {
		return dto.APIResult{OK: false, Error: &dto.APIError{Code: "INVALID_ARGUMENT", Message: "缺少任务 id"}}
	}
	if err := cancel(jobID); err != nil {
		return dto.APIResult{
			OK:    false,
			Error: &dto.APIError{Code: "LOGIN_NOT_FOUND", Message: "扫码任务不存在或已结束", Detail: err.Error()},
		}
	}
	return dto.APIResult{OK: true}
}

func (a *App) refreshQRLogin(jobID string, refresh func(string) error) (res dto.APIResult) {
	defer func() {
		if r := recover(); r != nil {
			res = dto.APIResult{
				OK:    false,
				Error: &dto.APIError{Code: "INTERNAL", Message: "刷新二维码时发生内部错误", Detail: fmt.Sprint(r)},
			}
		}
	}()

	if jobID == "" {
		return dto.APIResult{OK: false, Error: &dto.APIError{Code: "INVALID_ARGUMENT", Message: "缺少任务 id"}}
	}
	if err := refresh(jobID); err != nil {
		return dto.APIResult{
			OK:    false,
			Error: &dto.APIError{Code: "LOGIN_NOT_FOUND", Message: "扫码任务不存在或已结束", Detail: err.Error()},
		}
	}
	return dto.APIResult{OK: true}
}

// StartYuketangLogin 启动扫码登录，二维码与状态通过事件推送
func (a *App) StartYuketangLogin() dto.RefreshStartResult {
	return a.startQRLogin("雨课堂", a.svc.StartYuketangLogin)
}

// CancelYuketangLogin 停止扫码轮询
func (a *App) CancelYuketangLogin(jobID string) dto.APIResult {
	return a.cancelQRLogin(jobID, a.svc.CancelYuketangLogin)
}

// RefreshYuketangLogin 重新获取一张二维码
func (a *App) RefreshYuketangLogin(jobID string) dto.APIResult {
	return a.refreshQRLogin(jobID, a.svc.RefreshYuketangLogin)
}

// StartChaoxingLogin 启动学习通扫码登录，二维码与状态通过事件推送
func (a *App) StartChaoxingLogin() dto.RefreshStartResult {
	return a.startQRLogin("学习通", a.svc.StartChaoxingLogin)
}

// CancelChaoxingLogin 停止学习通扫码轮询
func (a *App) CancelChaoxingLogin(jobID string) dto.APIResult {
	return a.cancelQRLogin(jobID, a.svc.CancelChaoxingLogin)
}

// RefreshChaoxingLogin 重新获取一张学习通二维码
func (a *App) RefreshChaoxingLogin(jobID string) dto.APIResult {
	return a.refreshQRLogin(jobID, a.svc.RefreshChaoxingLogin)
}

// StartPigaiLogin 启动批改网微信扫码登录，二维码与状态通过事件推送
func (a *App) StartPigaiLogin() dto.RefreshStartResult {
	return a.startQRLogin("批改网", a.svc.StartPigaiLogin)
}

// CancelPigaiLogin 停止批改网扫码轮询
func (a *App) CancelPigaiLogin(jobID string) dto.APIResult {
	return a.cancelQRLogin(jobID, a.svc.CancelPigaiLogin)
}

// RefreshPigaiLogin 重新获取一张批改网二维码
func (a *App) RefreshPigaiLogin(jobID string) dto.APIResult {
	return a.refreshQRLogin(jobID, a.svc.RefreshPigaiLogin)
}

// StartFiforalLogin 启动 FiF 口语微信扫码登录，二维码与状态通过事件推送
func (a *App) StartFiforalLogin() dto.RefreshStartResult {
	return a.startQRLogin("FiF 口语", a.svc.StartFiforalLogin)
}

// CancelFiforalLogin 停止 FiF 口语扫码轮询
func (a *App) CancelFiforalLogin(jobID string) dto.APIResult {
	return a.cancelQRLogin(jobID, a.svc.CancelFiforalLogin)
}

// RefreshFiforalLogin 重新获取一张 FiF 口语二维码
func (a *App) RefreshFiforalLogin(jobID string) dto.APIResult {
	return a.refreshQRLogin(jobID, a.svc.RefreshFiforalLogin)
}
