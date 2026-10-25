package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"catcher/internal/domain/entity"
	"catcher/pkg/logger"
)

type fakeReleases struct {
	info  entity.ReleaseInfo
	err   error
	calls int
}

func (f *fakeReleases) LatestRelease(context.Context) (entity.ReleaseInfo, error) {
	f.calls++
	return f.info, f.err
}

func newUpdateService(releases *fakeReleases, current string, devMode bool) *Service {
	return New(Deps{
		Releases: releases,
		Version:  current,
		DevMode:  devMode,
		Logger:   logger.Discard(),
		Now:      func() time.Time { return time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC) },
	})
}

func TestCheckUpdateHasUpdate(t *testing.T) {
	releases := &fakeReleases{info: entity.ReleaseInfo{
		Version: "1.1.0",
		Name:    "Catcher 1.1.0",
		URL:     "https://github.com/o/r/releases/tag/v1.1.0",
	}}
	svc := newUpdateService(releases, "1.0.0", false)

	got, err := svc.CheckUpdate(context.Background(), false)
	if err != nil {
		t.Fatalf("CheckUpdate 返回错误: %v", err)
	}
	if !got.HasUpdate || got.Latest != "1.1.0" || got.Current != "1.0.0" {
		t.Errorf("结果不对: %+v", got)
	}
	if got.ReleaseURL != "https://github.com/o/r/releases/tag/v1.1.0" {
		t.Errorf("ReleaseURL = %q", got.ReleaseURL)
	}
	if got.CheckedAt.IsZero() {
		t.Error("CheckedAt 未填写")
	}
	if releases.calls != 1 {
		t.Errorf("请求次数 = %d, want 1", releases.calls)
	}
}

func TestCheckUpdateAlreadyLatest(t *testing.T) {
	releases := &fakeReleases{info: entity.ReleaseInfo{Version: "1.0.0"}}
	svc := newUpdateService(releases, "1.0.0", false)

	got, err := svc.CheckUpdate(context.Background(), false)
	if err != nil {
		t.Fatalf("CheckUpdate 返回错误: %v", err)
	}
	if got.HasUpdate {
		t.Errorf("同版本不应提示更新: %+v", got)
	}
}

func TestCheckUpdateDevModeSkipsAutoCheck(t *testing.T) {
	releases := &fakeReleases{info: entity.ReleaseInfo{Version: "1.1.0"}}
	svc := newUpdateService(releases, "1.0.0", true)

	got, err := svc.CheckUpdate(context.Background(), false)
	if err != nil {
		t.Fatalf("CheckUpdate 返回错误: %v", err)
	}
	if !got.Skipped || got.HasUpdate {
		t.Errorf("开发模式应跳过自动检查: %+v", got)
	}
	if releases.calls != 0 {
		t.Errorf("开发模式不应发请求，实际 %d 次", releases.calls)
	}
}

func TestCheckUpdateDevModeManualStillChecks(t *testing.T) {
	releases := &fakeReleases{info: entity.ReleaseInfo{Version: "1.1.0"}}
	svc := newUpdateService(releases, "1.0.0", true)

	got, err := svc.CheckUpdate(context.Background(), true)
	if err != nil {
		t.Fatalf("CheckUpdate 返回错误: %v", err)
	}
	if !got.HasUpdate || got.Skipped {
		t.Errorf("手动检查不应跳过: %+v", got)
	}
	if releases.calls != 1 {
		t.Errorf("请求次数 = %d, want 1", releases.calls)
	}
}

func TestCheckUpdatePassesThroughError(t *testing.T) {
	releases := &fakeReleases{err: entity.ErrReleaseUnavailable}
	svc := newUpdateService(releases, "1.0.0", false)

	got, err := svc.CheckUpdate(context.Background(), false)
	if !errors.Is(err, entity.ErrReleaseUnavailable) {
		t.Fatalf("错误 = %v, want ErrReleaseUnavailable", err)
	}
	if got.Current != "1.0.0" {
		t.Errorf("失败时仍应返回当前版本: %+v", got)
	}
}

func TestCheckUpdateInvalidCurrentVersion(t *testing.T) {
	releases := &fakeReleases{info: entity.ReleaseInfo{Version: "1.1.0"}}
	svc := newUpdateService(releases, "dev", false)

	if _, err := svc.CheckUpdate(context.Background(), false); !errors.Is(err, entity.ErrReleaseInvalid) {
		t.Errorf("错误 = %v, want ErrReleaseInvalid", err)
	}
}

func TestCheckUpdateWithoutChecker(t *testing.T) {
	svc := New(Deps{Version: "1.0.0", Logger: logger.Discard()})
	if _, err := svc.CheckUpdate(context.Background(), true); err == nil {
		t.Error("未配置 ReleaseChecker 时应报错")
	}
}
