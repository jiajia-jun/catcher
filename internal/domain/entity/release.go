package entity

import "time"

// ReleaseInfo 是一次版本查询得到的最新正式发布。
// GitHub 的 latest 接口会跳过草稿与预发布，这里不再区分
type ReleaseInfo struct {
	// Version 是去掉 v 前缀的 tag，例如 1.1.0
	Version     string
	Tag         string
	Name        string
	URL         string
	PublishedAt time.Time
}
