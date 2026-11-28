package cli

import (
	"context"
	"io"
	"time"

	"catcher/internal/domain/entity"
	"catcher/internal/service"
	"catcher/pkg/logger"
)

// Service 是命令行消费的用例集合，实现是 *service.Service
type Service interface {
	Accounts(ctx context.Context) ([]entity.Account, error)
	ListAssignments(ctx context.Context, f entity.AssignmentFilter) ([]entity.Assignment, error)
	SetAssignmentStatus(ctx context.Context, id int64, s *entity.SubmissionStatus) (entity.Assignment, error)
	Bootstrap(ctx context.Context, termOnly bool) (*service.Bootstrap, error)
	RefreshSync(ctx context.Context, platforms []entity.Platform) []service.PlatformResult
	EnabledPlatforms() []entity.Platform
	Version() string
	DataDir() string
}

// Deps 是执行命令所需的依赖，测试可注入假实现
type Deps struct {
	Service Service
	Logger  logger.Logger
	DataDir string
	Version string
	Stdout  io.Writer
	Stderr  io.Writer
	Now     func() time.Time
}

func (d Deps) withDefaults() Deps {
	if d.Stdout == nil {
		d.Stdout = io.Discard
	}
	if d.Stderr == nil {
		d.Stderr = io.Discard
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Logger == nil {
		d.Logger = logger.Discard()
	}
	return d
}
