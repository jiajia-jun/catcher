package service

import (
	"reflect"
	"testing"

	"catcher/internal/domain/entity"
	"catcher/pkg/config"
	"catcher/pkg/logger"
)

func TestEnabledPlatformsPersisted(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	svc := New(Deps{Config: &cfg, DataDir: dir, Version: "1.3.0", Logger: logger.Discard()})

	if got := svc.EnabledPlatforms(); !reflect.DeepEqual(got, entity.AllPlatforms()) {
		t.Fatalf("未配置时应等于全部平台: %#v", got)
	}
	if err := svc.SetEnabledPlatforms(nil); err == nil {
		t.Fatal("空列表应报错")
	}
	if err := svc.SetEnabledPlatforms([]entity.Platform{entity.Platform("unknown")}); err == nil {
		t.Fatal("未知平台应报错")
	}

	want := []entity.Platform{entity.PlatformPigai, entity.PlatformChaoxing}
	err := svc.SetEnabledPlatforms([]entity.Platform{
		entity.PlatformPigai, entity.PlatformChaoxing, entity.PlatformPigai,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := svc.EnabledPlatforms(); !reflect.DeepEqual(got, want) {
		t.Fatalf("勾选平台 = %#v, want %#v", got, want)
	}

	loaded, err := config.LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded.UI.EnabledPlatforms, []string{"pigai", "chaoxing"}) {
		t.Fatalf("勾选平台未落盘: %#v", loaded.UI.EnabledPlatforms)
	}

	if err := svc.MarkChangelogSeen(); err != nil {
		t.Fatal(err)
	}
	if got := svc.ChangelogSeenVersion(); got != "1.3.0" {
		t.Fatalf("已读版本 = %q", got)
	}
	loaded, err = config.LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.UI.ChangelogSeenVersion != "1.3.0" {
		t.Fatalf("已读版本未落盘: %q", loaded.UI.ChangelogSeenVersion)
	}
}

func TestEnabledPlatformsRollsBackOnSaveFailure(t *testing.T) {
	cfg := config.Default()
	// 没有数据目录时写盘必然失败，内存值必须回滚
	svc := New(Deps{Config: &cfg, Version: "1.3.0", Logger: logger.Discard()})

	if err := svc.SetEnabledPlatforms([]entity.Platform{entity.PlatformPigai}); err == nil {
		t.Fatal("写盘失败应返回错误")
	}
	if got := svc.EnabledPlatforms(); !reflect.DeepEqual(got, entity.AllPlatforms()) {
		t.Fatalf("失败后应回滚为全部平台: %#v", got)
	}
}
