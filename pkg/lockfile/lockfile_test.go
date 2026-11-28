package lockfile

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// 同一进程的两个句柄也要互斥，桌面版与命令行是不同进程，但测试能覆盖同一把锁
func TestTryLockExcludesSecondHandle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "platform.lock")

	release, ok, err := TryLock(path)
	if err != nil || !ok {
		t.Fatalf("首次加锁失败: ok=%v err=%v", ok, err)
	}

	second, ok, err := TryLock(path)
	if err != nil {
		t.Fatalf("第二次加锁报错: %v", err)
	}
	if ok || second != nil {
		t.Fatal("锁被占用时不应获取成功")
	}

	release()
	third, ok, err := TryLock(path)
	if err != nil || !ok {
		t.Fatalf("释放后应能重新获取: ok=%v err=%v", ok, err)
	}
	third()
}

// 重复调用释放函数不应解锁后又被第二次调用影响
func TestReleaseIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "platform.lock")
	release, ok, err := TryLock(path)
	if err != nil || !ok {
		t.Fatalf("加锁失败: ok=%v err=%v", ok, err)
	}
	release()
	release()

	again, ok, err := TryLock(path)
	if err != nil || !ok {
		t.Fatalf("释放后应能重新获取: ok=%v err=%v", ok, err)
	}
	again()
}

func TestLockTimesOutWhileHeld(t *testing.T) {
	path := filepath.Join(t.TempDir(), "migrate.lock")
	release, ok, err := TryLock(path)
	if err != nil || !ok {
		t.Fatalf("加锁失败: ok=%v err=%v", ok, err)
	}
	defer release()

	start := time.Now()
	_, err = Lock(path, 120*time.Millisecond)
	if err == nil {
		t.Fatal("锁被占用时应超时")
	}
	var timeoutErr *TimeoutError
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("错误类型 = %T, want *TimeoutError", err)
	}
	if elapsed := time.Since(start); elapsed < 100*time.Millisecond {
		t.Fatalf("提前返回: %s", elapsed)
	}
}
