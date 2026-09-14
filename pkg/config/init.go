package config

import (
	"fmt"

	"github.com/spf13/viper"
)

func New(path string, name string, ext string) (*App, error) {
	v := viper.New()
	v.AddConfigPath(path)
	v.SetConfigName(name)
	v.SetConfigType(ext)

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	var app App
	if err := v.Unmarshal(&app); err != nil {
		return nil, fmt.Errorf("解析配置失败: %w", err)
	}
	return &app, nil
}
