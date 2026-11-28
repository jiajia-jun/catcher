// Package lockfile 提供跨进程独占文件锁
//
// 桌面版与命令行共用同一份数据目录，刷新同一平台时需要互斥；
// 锁由操作系统持有，进程退出（含崩溃）后自动释放
package lockfile

import "time"

// pollInterval 是 Lock 轮询重试的间隔
const pollInterval = 50 * time.Millisecond

// TryLock 非阻塞地独占锁文件，path 不存在时创建
// 返回释放函数与是否获取成功；文件被其他句柄持有时 ok=false 且 err=nil
func TryLock(path string) (release func(), ok bool, err error) {
	l, ok, err := tryLockFile(path)
	if err != nil || !ok {
		return nil, ok, err
	}
	var released bool
	return func() {
		if released {
			return
		}
		released = true
		l.release()
	}, true, nil
}

// Lock 阻塞等待独占，timeout 大于 0 时超时返回错误
// timeout 为 0 或负数表示一直等待
func Lock(path string, timeout time.Duration) (func(), error) {
	var deadline time.Time
	if timeout > 0 {
		deadline = time.Now().Add(timeout)
	}
	for {
		release, ok, err := TryLock(path)
		if err != nil {
			return nil, err
		}
		if ok {
			return release, nil
		}
		if !deadline.IsZero() && time.Now().After(deadline) {
			return nil, &TimeoutError{Path: path, Timeout: timeout}
		}
		time.Sleep(pollInterval)
	}
}

// TimeoutError 表示在给定时间内没有拿到锁
type TimeoutError struct {
	Path    string
	Timeout time.Duration
}

func (e *TimeoutError) Error() string {
	return "等待文件锁 " + e.Path + " 超时"
}
