package dto

import (
	"net/url"
	"strings"
	"time"

	"catcher/internal/domain/entity"
)

// AssignmentViewOf 把领域作业转换成界面与命令行共用的视图
// now 用于计算 open / closed，手动状态优先展示
func AssignmentViewOf(a entity.Assignment, now time.Time) AssignmentView {
	view := AssignmentView{
		ID:               a.ID,
		Platform:         string(a.Platform),
		CourseName:       a.CourseName,
		CourseExternalID: a.CourseExternalID,
		Title:            a.Title,
		Status:           string(a.EffectiveStatus()),
		PlatformStatus:   string(a.Status),
		Phase:            string(entity.PhaseOf(a.Deadline, now)),
		DeadlineSource:   string(a.DeadlineSource),
		DeadlineRaw:      a.DeadlineRaw,
		DetailURL:        SafeDetailURL(a.DetailURL),
		FirstSeenAt:      a.FirstSeenAt.UTC().Format(time.RFC3339),
		LastSeenAt:       a.LastSeenAt.UTC().Format(time.RFC3339),
	}
	if a.ManualStatus != nil {
		view.ManualStatus = string(*a.ManualStatus)
	}
	if a.ManualStatusAt != nil {
		view.ManualStatusAt = a.ManualStatusAt.UTC().Format(time.RFC3339)
	}
	if a.Deadline != nil {
		view.DeadlineAt = a.Deadline.UTC().Format(time.RFC3339)
		view.DeadlineLocal = a.Deadline.In(Shanghai()).Format("2006-01-02 15:04")
	}
	return view
}

// SafeDetailURL 只放行绝对 http/https 地址
// 平台 HTML 里的链接按不可信输入处理
func SafeDetailURL(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if !strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https") {
		return ""
	}
	if u.Host == "" {
		return ""
	}
	return raw
}

// Shanghai 返回东八区时区，时区数据库缺失时退回固定偏移
func Shanghai() *time.Location {
	if loc, err := time.LoadLocation("Asia/Shanghai"); err == nil {
		return loc
	}
	return time.FixedZone("CST", 8*3600)
}
