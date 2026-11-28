package app

import (
	"path/filepath"

	"catcher/internal/domain/entity"
	"catcher/internal/service"
	"catcher/pkg/lockfile"
)

// platformLocker 把平台 ID 映射到 data/locks/<platform>.lock
type platformLocker struct {
	dir string
}

func newPlatformLocker(dataDir string) service.PlatformLocker {
	if dataDir == "" {
		return nil
	}
	return &platformLocker{dir: filepath.Join(dataDir, "locks")}
}

func (l *platformLocker) TryLock(platform entity.Platform) (func(), bool, error) {
	return lockfile.TryLock(filepath.Join(l.dir, string(platform)+".lock"))
}
