package config

// App 是 data/catcher.yaml 的结构。配置里不允许放任何凭据。
type App struct {
	Log   LogConfig   `mapstructure:"log"`
	Raw   RawConfig   `mapstructure:"raw"`
	Fetch FetchConfig `mapstructure:"fetch"`
	UI    UIConfig    `mapstructure:"ui"`
}

type LogConfig struct {
	Level      string `mapstructure:"level"`       // debug | info | warn | error
	MaxSizeMB  int    `mapstructure:"max_size_mb"` // 单个日志文件上限
	MaxBackups int    `mapstructure:"max_backups"`
	MaxAgeDays int    `mapstructure:"max_age_days"`
}

type RawConfig struct {
	Enabled       bool `mapstructure:"enabled"`        // 失败响应留底
	RetentionDays int  `mapstructure:"retention_days"` // 保留天数
	MaxTotalMB    int  `mapstructure:"max_total_mb"`   // 总大小上限
}

type FetchConfig struct {
	IncludeHistory bool `mapstructure:"include_history"` // v1 固定 false
}

type UIConfig struct {
	Theme string `mapstructure:"theme"` // auto | light | dark
	// EnabledPlatforms 是「刷新全部」勾选的平台；空表示全部
	EnabledPlatforms []string `mapstructure:"enabled_platforms"`
	// ChangelogSeenVersion 是用户已读更新日志的版本号
	ChangelogSeenVersion string `mapstructure:"changelog_seen_version"`
}

// Default 返回设计文档约定的默认配置。
func Default() App {
	return App{
		Log: LogConfig{
			Level:      "info",
			MaxSizeMB:  20,
			MaxBackups: 5,
			MaxAgeDays: 14,
		},
		Raw: RawConfig{
			Enabled:       true,
			RetentionDays: 7,
			MaxTotalMB:    100,
		},
		Fetch: FetchConfig{IncludeHistory: false},
		UI:    UIConfig{Theme: "auto"},
	}
}

// Normalize 用默认值补齐非法/缺失项。配置错误不应阻止应用启动。
func (a *App) Normalize() {
	d := Default()

	if a.Log.Level != "debug" && a.Log.Level != "info" && a.Log.Level != "warn" && a.Log.Level != "error" {
		a.Log.Level = d.Log.Level
	}
	if a.Log.MaxSizeMB <= 0 {
		a.Log.MaxSizeMB = d.Log.MaxSizeMB
	}
	if a.Log.MaxBackups <= 0 {
		a.Log.MaxBackups = d.Log.MaxBackups
	}
	if a.Log.MaxAgeDays <= 0 {
		a.Log.MaxAgeDays = d.Log.MaxAgeDays
	}
	if a.Raw.RetentionDays <= 0 {
		a.Raw.RetentionDays = d.Raw.RetentionDays
	}
	if a.Raw.MaxTotalMB <= 0 {
		a.Raw.MaxTotalMB = d.Raw.MaxTotalMB
	}
	if a.UI.Theme != "auto" && a.UI.Theme != "light" && a.UI.Theme != "dark" {
		a.UI.Theme = d.UI.Theme
	}
}
