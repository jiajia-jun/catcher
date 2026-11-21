package handle

import (
	"fmt"

	"catcher/internal/domain/entity"
	"catcher/internal/dto"
)

// GetSettings 返回只读设置视图
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
			LogLevel:             cfg.Log.Level,
			RawEnabled:           cfg.Raw.Enabled,
			RawRetention:         cfg.Raw.RetentionDays,
			Theme:                cfg.UI.Theme,
			DataDir:              a.svc.DataDir(),
			Version:              a.svc.Version(),
			EnabledPlatforms:     platformStrings(a.svc.EnabledPlatforms()),
			ChangelogSeenVersion: a.svc.ChangelogSeenVersion(),
			FirstRun:             a.svc.FirstRun(),
		},
	}
}

// SetEnabledPlatforms 保存「刷新全部」勾选的平台，至少保留一个
func (a *App) SetEnabledPlatforms(platforms []string) (res dto.APIResult) {
	defer func() {
		if r := recover(); r != nil {
			res = dto.APIResult{
				OK:    false,
				Error: &dto.APIError{Code: "INTERNAL", Message: "保存平台勾选时发生内部错误", Detail: fmt.Sprint(r)},
			}
		}
	}()

	ps := make([]entity.Platform, 0, len(platforms))
	for _, raw := range platforms {
		p, err := entity.ParsePlatform(raw)
		if err != nil {
			return dto.APIResult{
				OK:    false,
				Error: &dto.APIError{Code: "INVALID_ARGUMENT", Message: "平台参数无效", Detail: err.Error()},
			}
		}
		ps = append(ps, p)
	}
	if len(ps) == 0 {
		return dto.APIResult{
			OK:    false,
			Error: &dto.APIError{Code: "INVALID_ARGUMENT", Message: "至少选择一个参与刷新的平台"},
		}
	}
	if err := a.svc.SetEnabledPlatforms(ps); err != nil {
		return dto.APIResult{
			OK:    false,
			Error: &dto.APIError{Code: "STORAGE", Message: "保存平台勾选失败", Detail: err.Error()},
		}
	}
	return dto.APIResult{OK: true}
}

// MarkChangelogSeen 把当前版本记为已读更新日志
func (a *App) MarkChangelogSeen() (res dto.APIResult) {
	defer func() {
		if r := recover(); r != nil {
			res = dto.APIResult{
				OK:    false,
				Error: &dto.APIError{Code: "INTERNAL", Message: "记录更新日志已读时发生内部错误", Detail: fmt.Sprint(r)},
			}
		}
	}()

	if err := a.svc.MarkChangelogSeen(); err != nil {
		return dto.APIResult{
			OK:    false,
			Error: &dto.APIError{Code: "STORAGE", Message: "记录更新日志已读失败", Detail: err.Error()},
		}
	}
	return dto.APIResult{OK: true}
}

// OpenDataDir 在资源管理器中打开数据目录
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
