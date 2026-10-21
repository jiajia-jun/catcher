// Package service 编排业务用例。它只依赖 domain 定义的端口，
// 具体实现在 cmd/app 中注入。
package service

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"time"

	"catcher/internal/domain"
	"catcher/internal/domain/entity"
	"catcher/pkg/config"
)

// Counts 是作业统计。unknown 永远不计入未交。
type Counts struct {
	OpenUnsubmitted    int
	OverdueUnsubmitted int
	Unknown            int
	Submitted          int
}

// Bootstrap 是首屏所需的聚合数据。
type Bootstrap struct {
	Accounts []entity.Account
	Counts   map[entity.Platform]Counts
	Stats    Counts
	DataDir  string
	Version  string
}

type Service struct {
	accounts    domain.AccountRepository
	assignments domain.AssignmentRepository
	cfg         *config.App
	dataDir     string
	version     string
	now         func() time.Time
}

func New(accounts domain.AccountRepository, assignments domain.AssignmentRepository,
	cfg *config.App, dataDir, version string) *Service {
	return &Service{
		accounts:    accounts,
		assignments: assignments,
		cfg:         cfg,
		dataDir:     dataDir,
		version:     version,
		now:         time.Now,
	}
}

// Bootstrap 读取账号与全部 active 作业，并按平台聚合统计。
func (s *Service) Bootstrap(ctx context.Context) (*Bootstrap, error) {
	accounts, err := s.accounts.ListAccounts(ctx)
	if err != nil {
		return nil, err
	}
	items, err := s.assignments.ListAssignments(ctx, entity.AssignmentFilter{ShowSubmitted: true})
	if err != nil {
		return nil, err
	}

	counts := make(map[entity.Platform]Counts, len(entity.AllPlatforms()))
	for _, p := range entity.AllPlatforms() {
		counts[p] = Counts{}
	}

	now := s.now()
	var stats Counts
	for _, item := range items {
		c := counts[item.Platform]
		addCount(&c, item, now)
		counts[item.Platform] = c
		addCount(&stats, item, now)
	}

	return &Bootstrap{
		Accounts: accounts,
		Counts:   counts,
		Stats:    stats,
		DataDir:  s.dataDir,
		Version:  s.version,
	}, nil
}

func addCount(c *Counts, item entity.Assignment, now time.Time) {
	switch item.Status {
	case entity.StatusSubmitted:
		c.Submitted++
	case entity.StatusUnsubmitted:
		if entity.PhaseOf(item.Deadline, now) == entity.PhaseClosed {
			c.OverdueUnsubmitted++
		} else {
			c.OpenUnsubmitted++
		}
	default:
		c.Unknown++
	}
}

// ListAssignments 返回符合筛选条件的作业。
func (s *Service) ListAssignments(ctx context.Context, filter entity.AssignmentFilter) ([]entity.Assignment, error) {
	return s.assignments.ListAssignments(ctx, filter)
}

// Settings 返回配置副本。
func (s *Service) Settings() config.App {
	if s.cfg == nil {
		return config.Default()
	}
	return *s.cfg
}

func (s *Service) DataDir() string { return s.dataDir }
func (s *Service) Version() string { return s.version }

// OpenDataDir 在资源管理器中打开数据目录。
func (s *Service) OpenDataDir(_ context.Context) error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("当前平台不支持打开目录: %s", runtime.GOOS)
	}
	cmd := exec.Command("explorer.exe", s.dataDir)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("打开数据目录失败: %w", err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
