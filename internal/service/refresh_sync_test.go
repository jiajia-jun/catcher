package service

import (
	"context"
	"testing"
	"time"

	"catcher/internal/domain/entity"
)

// RefreshSync 按请求顺序返回每个平台的结果
func TestRefreshSyncPreservesOrder(t *testing.T) {
	h := newHarness(t)
	h.svc.fetchers[entity.PlatformChaoxing] = h.fetcher
	h.svc.fetchers[entity.PlatformTronclass] = h.fetcher

	results := h.svc.RefreshSync(context.Background(),
		[]entity.Platform{entity.PlatformTronclass, entity.PlatformChaoxing})

	if len(results) != 2 {
		t.Fatalf("结果数 = %d, want 2", len(results))
	}
	if results[0].Platform != entity.PlatformTronclass || results[1].Platform != entity.PlatformChaoxing {
		t.Fatalf("顺序 = %v, want tronclass, chaoxing", []entity.Platform{results[0].Platform, results[1].Platform})
	}
	for _, result := range results {
		if result.Status != entity.RunSuccess {
			t.Fatalf("%s 状态 = %s, want success", result.Platform, result.Status)
		}
	}
}

// 从未登录的平台标 skipped，不发起请求也不写刷新记录
func TestRefreshSyncSkipsDisconnected(t *testing.T) {
	h := newHarness(t)
	h.accounts.states[entity.PlatformYuketang] = entity.LoginDisconnected

	results := h.svc.RefreshSync(context.Background(), []entity.Platform{entity.PlatformYuketang})

	if len(results) != 1 {
		t.Fatalf("结果数 = %d", len(results))
	}
	if results[0].Status != entity.RunSkipped || results[0].SkipReason != "not_logged_in" {
		t.Fatalf("结果 = %#v", results[0])
	}
	if h.fetcher.callCount() != 0 {
		t.Fatal("skipped 不应发起抓取")
	}
	h.runs.mu.Lock()
	defer h.runs.mu.Unlock()
	if len(h.runs.created) != 0 {
		t.Fatalf("skipped 不应写刷新记录: %d", len(h.runs.created))
	}
}

type fakeLocker struct {
	busy map[entity.Platform]bool
}

func (f *fakeLocker) TryLock(p entity.Platform) (func(), bool, error) {
	if f.busy[p] {
		return nil, false, nil
	}
	return func() {}, true, nil
}

// 平台锁被其他进程持有时标 busy，且不写刷新记录
func TestRefreshSyncBusyDoesNotCreateRun(t *testing.T) {
	h := newHarness(t)
	h.svc.locker = &fakeLocker{busy: map[entity.Platform]bool{entity.PlatformYuketang: true}}

	results := h.svc.RefreshSync(context.Background(), []entity.Platform{entity.PlatformYuketang})

	if len(results) != 1 || results[0].Status != entity.RunBusy {
		t.Fatalf("结果 = %#v", results)
	}
	if results[0].Error == nil || results[0].Error.Code != "PLATFORM_BUSY" {
		t.Fatalf("错误 = %#v", results[0].Error)
	}
	if h.fetcher.callCount() != 0 {
		t.Fatal("busy 不应发起抓取")
	}
	h.runs.mu.Lock()
	defer h.runs.mu.Unlock()
	if len(h.runs.created) != 0 {
		t.Fatalf("busy 不应写刷新记录: %d", len(h.runs.created))
	}
}

// 取消传播到在途请求，结果标 canceled
func TestRefreshSyncPropagatesCancel(t *testing.T) {
	h := newHarness(t)
	h.fetcher.blockOn = make(chan struct{})
	h.fetcher.startedCh = make(chan struct{}, 1)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan []PlatformResult, 1)
	go func() {
		done <- h.svc.RefreshSync(ctx, []entity.Platform{entity.PlatformYuketang})
	}()

	select {
	case <-h.fetcher.startedCh:
	case <-time.After(2 * time.Second):
		t.Fatal("抓取未启动")
	}
	cancel()

	select {
	case results := <-done:
		if len(results) != 1 {
			t.Fatalf("结果数 = %d", len(results))
		}
		if results[0].Status != entity.RunCanceled {
			t.Fatalf("状态 = %s, want canceled", results[0].Status)
		}
		if results[0].Error == nil || results[0].Error.Code != "CANCELED" {
			t.Fatalf("错误 = %#v", results[0].Error)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("取消后未返回")
	}
}

// 没有配置 Locker 时 refreshPlatform 退化为不加锁
func TestRefreshSyncWithoutLocker(t *testing.T) {
	h := newHarness(t)
	release, ok, err := h.svc.tryLockPlatform(entity.PlatformYuketang)
	if err != nil || !ok || release != nil {
		t.Fatalf("无 Locker 应总是成功: release 非 nil=%v ok=%v err=%v", release != nil, ok, err)
	}
}

var _ PlatformLocker = (*fakeLocker)(nil)
