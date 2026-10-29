package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

const (
	// FileName 是数据目录内的配置文件名。
	FileName = "catcher"
	// FileExt 是配置文件类型。
	FileExt = "yaml"
)

// New 读取指定路径下的配置。缺失的配置项会使用默认值。
func New(path string, name string, ext string) (*App, error) {
	v := viper.New()
	v.AddConfigPath(path)
	v.SetConfigName(name)
	v.SetConfigType(ext)

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	app := Default()
	if err := v.Unmarshal(&app); err != nil {
		return nil, fmt.Errorf("解析配置失败: %w", err)
	}
	app.Normalize()
	return &app, nil
}

// LoadOrCreate 从数据目录读取 catcher.yaml；不存在时用默认值创建。
func LoadOrCreate(dir string) (*App, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("创建数据目录失败: %w", err)
	}

	path := filepath.Join(dir, FileName+"."+FileExt)
	app := Default()

	if _, err := os.Stat(path); err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("检查配置文件失败: %w", err)
		}
		if err := app.Save(path); err != nil {
			return nil, err
		}
		return &app, nil
	}

	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType(FileExt)
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}
	if err := v.Unmarshal(&app); err != nil {
		return nil, fmt.Errorf("解析配置失败: %w", err)
	}
	app.Normalize()
	return &app, nil
}

// Save 把配置写到指定文件路径。
func (a *App) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("创建配置目录失败: %w", err)
	}

	v := viper.New()
	v.SetConfigType(FileExt)
	v.Set("log.level", a.Log.Level)
	v.Set("log.max_size_mb", a.Log.MaxSizeMB)
	v.Set("log.max_backups", a.Log.MaxBackups)
	v.Set("log.max_age_days", a.Log.MaxAgeDays)
	v.Set("raw.enabled", a.Raw.Enabled)
	v.Set("raw.retention_days", a.Raw.RetentionDays)
	v.Set("raw.max_total_mb", a.Raw.MaxTotalMB)
	v.Set("fetch.include_history", a.Fetch.IncludeHistory)
	v.Set("ui.theme", a.UI.Theme)
	v.Set("ui.enabled_platforms", a.UI.EnabledPlatforms)
	v.Set("ui.changelog_seen_version", a.UI.ChangelogSeenVersion)

	if err := v.WriteConfigAs(path); err != nil {
		return fmt.Errorf("写入配置文件失败: %w", err)
	}
	return nil
}
