package service

import (
	"context"
	"fmt"
	"time"

	"catcher/internal/domain/entity"
	"catcher/pkg/version"
)

// UpdateCheck 是一次版本检查的结果
type UpdateCheck struct {
	Current     string
	Latest      string
	ReleaseName string
	ReleaseURL  string
	HasUpdate   bool
	CheckedAt   time.Time
	// Skipped 表示开发模式下跳过了自动检查，不代表已是最新
	Skipped bool
}

// CheckUpdate 查询最新正式发布并与当前版本比较。
// manual 为 false 且处于开发模式时直接跳过，不发起任何请求
func (s *Service) CheckUpdate(ctx context.Context, manual bool) (UpdateCheck, error) {
	out := UpdateCheck{Current: s.version}
	if s.releases == nil {
		return out, fmt.Errorf("未配置更新检查")
	}
	if !manual && s.devMode {
		out.Skipped = true
		return out, nil
	}

	info, err := s.releases.LatestRelease(ctx)
	if err != nil {
		return out, err
	}
	newer, err := version.IsNewer(s.version, info.Version)
	if err != nil {
		return out, fmt.Errorf("%w: %v", entity.ErrReleaseInvalid, err)
	}

	out.Latest = info.Version
	out.ReleaseName = info.Name
	out.ReleaseURL = info.URL
	out.HasUpdate = newer
	out.CheckedAt = s.now()
	s.logger.Debug("更新检查完成",
		"current", out.Current,
		"latest", out.Latest,
		"has_update", out.HasUpdate,
	)
	return out, nil
}
