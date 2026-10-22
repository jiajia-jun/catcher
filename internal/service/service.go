// Package service 编排业务用例。它只依赖 domain 定义的端口，
// 具体实现在 cmd/app 中注入
package service

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"catcher/internal/domain"
	"catcher/internal/domain/entity"
	"catcher/pkg/config"
	"catcher/pkg/logger"
)

// Counts 是作业统计。unknown 永远不计入未交
type Counts struct {
	OpenUnsubmitted    int
	OverdueUnsubmitted int
	Unknown            int
	Submitted          int
}

// Bootstrap 是首屏所需的聚合数据
type Bootstrap struct {
	Accounts []entity.Account
	Counts   map[entity.Platform]Counts
	Stats    Counts
	LastRuns map[entity.Platform]entity.FetchRun
	DataDir  string
	Version  string
}

// Deps 是 Service 的依赖集合，由 cmd/app 装配
type Deps struct {
	Accounts    domain.AccountRepository
	Sessions    domain.SessionRepository
	Credentials domain.CredentialRepository
	Assignments domain.AssignmentRepository
	Runs        domain.FetchRunRepository

	Fetchers       map[entity.Platform]domain.Fetcher
	Renewers       map[entity.Platform]domain.SessionRenewer
	Authenticators map[entity.Platform]domain.PasswordAuthenticator
	QRLogin        domain.QRLogin

	Config  *config.App
	DataDir string
	Version string
	Logger  logger.Logger
	Now     func() time.Time
}

type Service struct {
	accounts    domain.AccountRepository
	sessions    domain.SessionRepository
	credentials domain.CredentialRepository
	assignments domain.AssignmentRepository
	runs        domain.FetchRunRepository

	fetchers       map[entity.Platform]domain.Fetcher
	renewers       map[entity.Platform]domain.SessionRenewer
	authenticators map[entity.Platform]domain.PasswordAuthenticator
	qrLogin        domain.QRLogin

	cfg     *config.App
	dataDir string
	version string
	now     func() time.Time
	logger  logger.Logger

	emitMu sync.RWMutex
	emit   func(name string, payload any)

	jobsMu      sync.Mutex
	refreshJobs map[string]*refreshJob
	running     map[entity.Platform]string
	loginJobs   map[string]*loginJob
}

func New(d Deps) *Service {
	s := &Service{
		accounts:       d.Accounts,
		sessions:       d.Sessions,
		credentials:    d.Credentials,
		assignments:    d.Assignments,
		runs:           d.Runs,
		fetchers:       d.Fetchers,
		renewers:       d.Renewers,
		authenticators: d.Authenticators,
		qrLogin:        d.QRLogin,
		cfg:            d.Config,
		dataDir:        d.DataDir,
		version:        d.Version,
		now:            d.Now,
		logger:         d.Logger,

		refreshJobs: make(map[string]*refreshJob),
		running:     make(map[entity.Platform]string),
		loginJobs:   make(map[string]*loginJob),
	}
	if s.fetchers == nil {
		s.fetchers = make(map[entity.Platform]domain.Fetcher)
	}
	if s.renewers == nil {
		s.renewers = make(map[entity.Platform]domain.SessionRenewer)
	}
	if s.authenticators == nil {
		s.authenticators = make(map[entity.Platform]domain.PasswordAuthenticator)
	}
	if s.now == nil {
		s.now = time.Now
	}
	if s.logger == nil {
		s.logger = logger.Discard()
	}
	return s
}

// SetEmitter 注入事件出口。handle 在 Wails 启动后调用一次
func (s *Service) SetEmitter(emit func(name string, payload any)) {
	s.emitMu.Lock()
	s.emit = emit
	s.emitMu.Unlock()
}

func (s *Service) emitEvent(name string, payload any) {
	s.emitMu.RLock()
	emit := s.emit
	s.emitMu.RUnlock()
	if emit != nil {
		emit(name, payload)
	}
}

// Bootstrap 读取账号、active 作业与每平台最近一次刷新，并按平台聚合统计。
// termOnly 为 true 时只统计本学期的作业，与列表口径保持一致
func (s *Service) Bootstrap(ctx context.Context, termOnly bool) (*Bootstrap, error) {
	accounts, err := s.accounts.ListAccounts(ctx)
	if err != nil {
		return nil, err
	}
	filter := s.resolveTerm(entity.AssignmentFilter{ShowSubmitted: true, TermOnly: termOnly})
	items, err := s.assignments.ListAssignments(ctx, filter)
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

	lastRuns := map[entity.Platform]entity.FetchRun{}
	if s.runs != nil {
		lastRuns, err = s.runs.LatestRuns(ctx)
		if err != nil {
			return nil, err
		}
	}

	return &Bootstrap{
		Accounts: accounts,
		Counts:   counts,
		Stats:    stats,
		LastRuns: lastRuns,
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

// ListAssignments 返回符合筛选条件的作业
func (s *Service) ListAssignments(ctx context.Context, filter entity.AssignmentFilter) ([]entity.Assignment, error) {
	return s.assignments.ListAssignments(ctx, s.resolveTerm(filter))
}

// resolveTerm 把「只看本学期」换算成具体的截止时间下界。
// 学期起点按 9 月 1 日、东八区计算，由 service 决定，仓储只认绝对时刻
func (s *Service) resolveTerm(filter entity.AssignmentFilter) entity.AssignmentFilter {
	if !filter.TermOnly {
		return filter
	}
	start := entity.SemesterStart(s.now())
	filter.DeadlineFrom = &start
	return filter
}

// FetchIssues 返回某次刷新的问题明细
func (s *Service) FetchIssues(ctx context.Context, runID int64) ([]entity.FetchIssue, error) {
	if s.runs == nil {
		return nil, nil
	}
	return s.runs.ListIssues(ctx, runID)
}

// Settings 返回配置副本
func (s *Service) Settings() config.App {
	if s.cfg == nil {
		return config.Default()
	}
	return *s.cfg
}

func (s *Service) DataDir() string { return s.dataDir }
func (s *Service) Version() string { return s.version }

// OpenDataDir 在资源管理器中打开数据目录
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
