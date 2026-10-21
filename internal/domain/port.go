// Package domain 定义领域模型与端口接口。
//
// 本包是最内层：不 import internal/ 下任何外层包，也不 import cmd/。
// 具体实现（sqlite、DPAPI、各平台适配器）位于 internal/infra，由 cmd/app 注入。
package domain

import (
	"context"
	"time"

	"catcher/internal/domain/entity"
)

// Fetcher 是平台抓取能力。每个平台适配器实现一个。
type Fetcher interface {
	Platform() entity.Platform
	Fetch(ctx context.Context, account entity.Account, session entity.Session,
		opts entity.FetchOptions, progress entity.ProgressFunc) (*entity.FetchResult, error)
}

// PasswordAuthenticator 是密码登录能力（学习通）。
type PasswordAuthenticator interface {
	LoginWithPassword(ctx context.Context, creds entity.PasswordCredential) (*entity.Session, error)
}

// QRLogin 是扫码登录能力（雨课堂）。
type QRLogin interface {
	BeginQR(ctx context.Context) (*entity.QRChallenge, error)
	// PollQR 内部完成「轮询确认 + 换取会话」；返回 waiting 时继续调用。
	PollQR(ctx context.Context, challenge *entity.QRChallenge) (entity.QRState, *entity.Session, error)
}

// SessionImporter 是浏览器 session 导入能力（学在重邮）。
type SessionImporter interface {
	ImportSession(ctx context.Context, raw string) (*entity.Session, error)
}

// SessionRenewer 是自动续期能力。返回 ErrNotFound 表示无法续期，需要用户重新登录。
type SessionRenewer interface {
	Renew(ctx context.Context, session entity.Session) (*entity.Session, error)
}

// AccountRepository 维护每平台唯一的账号行。
type AccountRepository interface {
	EnsurePlatforms(ctx context.Context, now time.Time) error
	ListAccounts(ctx context.Context) ([]entity.Account, error)
	GetAccount(ctx context.Context, platform entity.Platform) (*entity.Account, error)
	SetLoginState(ctx context.Context, platform entity.Platform, state entity.LoginState, at time.Time) error
}

// SessionRepository 维护加密后的会话秘密。
type SessionRepository interface {
	Get(ctx context.Context, accountID int64) (*entity.Session, error)
	Save(ctx context.Context, accountID int64, secret []byte, expiresAt *time.Time, at time.Time) error
	Delete(ctx context.Context, accountID int64) error
}

// AssignmentRepository 读取作业。写入在 M1 随刷新编排一起补齐。
type AssignmentRepository interface {
	ListAssignments(ctx context.Context, filter entity.AssignmentFilter) ([]entity.Assignment, error)
}

// FetchRunRepository 记录刷新任务与问题明细。
type FetchRunRepository interface {
	Create(ctx context.Context, run *entity.FetchRun) error
	Finish(ctx context.Context, run entity.FetchRun, issues []entity.FetchIssue, finishedAt time.Time) error
}
