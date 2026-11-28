//go:build windows

package lockfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

type fileLock struct {
	file *os.File
}

func (l *fileLock) release() {
	if l.file == nil {
		return
	}
	overlapped := new(windows.Overlapped)
	_ = windows.UnlockFileEx(windows.Handle(l.file.Fd()), 0, 1, 0, overlapped)
	_ = l.file.Close()
	l.file = nil
}

// tryLockFile 用 LockFileEx 锁住文件的第一个字节
// 字节区间在文件长度之外同样可以加锁，锁文件无需先写入内容
func tryLockFile(path string) (*fileLock, bool, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, false, fmt.Errorf("创建锁目录失败: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false, fmt.Errorf("打开锁文件 %s 失败: %w", path, err)
	}

	overlapped := new(windows.Overlapped)
	err = windows.LockFileEx(
		windows.Handle(file.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, 1, 0, overlapped,
	)
	if err != nil {
		_ = file.Close()
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("锁定文件 %s 失败: %w", path, err)
	}
	return &fileLock{file: file}, true, nil
}
