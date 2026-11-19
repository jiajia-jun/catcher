package handle

import (
	"fmt"
	"time"

	"catcher/internal/dto"
	"catcher/internal/service"
)

// CheckForUpdate 查询最新正式发布并与当前版本比较
// manual 为 false 时开发模式直接跳过；自动检查的失败由前端静默处理
func (a *App) CheckForUpdate(manual bool) (res dto.UpdateCheckResult) {
	defer func() {
		if r := recover(); r != nil {
			res = dto.UpdateCheckResult{
				OK:    false,
				Error: &dto.APIError{Code: "INTERNAL", Message: "检查更新时发生内部错误", Detail: fmt.Sprint(r)},
			}
		}
	}()

	check, err := a.svc.CheckUpdate(a.context(), manual)
	if err != nil {
		return dto.UpdateCheckResult{
			OK:      false,
			Current: check.Current,
			Error:   updateAPIError(err),
		}
	}

	res = dto.UpdateCheckResult{
		OK:          true,
		Current:     check.Current,
		Latest:      check.Latest,
		ReleaseName: check.ReleaseName,
		ReleaseURL:  check.ReleaseURL,
		HasUpdate:   check.HasUpdate,
		Skipped:     check.Skipped,
	}
	if !check.CheckedAt.IsZero() {
		res.CheckedAt = check.CheckedAt.UTC().Format(time.RFC3339)
	}
	return res
}

// updateAPIError 把更新检查的哨兵错误翻译成用户能看懂的四类文案
func updateAPIError(err error) *dto.APIError {
	code := service.ErrorCode(err)
	message := "检查更新失败"
	switch code {
	case "NETWORK":
		message = "检查更新失败：网络不可达"
	case "RELEASE_UNAVAILABLE":
		message = "检查更新失败：发布仓库暂不可访问"
	case "RELEASE_RATE_LIMITED":
		message = "检查更新失败：GitHub 请求过于频繁，请稍后再试"
	case "RELEASE_INVALID":
		message = "检查更新失败：发布信息格式不正确"
	}
	return &dto.APIError{Code: code, Message: message, Detail: err.Error()}
}
