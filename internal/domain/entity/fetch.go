package entity

import "time"

// FetchOptions 是适配器执行一次抓取时可用的选项。
type FetchOptions struct {
	IncludeHistory bool
	Now            func() time.Time
	// OnSessionUpdated 在平台轮换会话/累积 cookie 后立即调用。
	// 它允许适配器把最新登录态交给上层加密持久化，避免进程崩溃后丢失。
	OnSessionUpdated func(Session) error
}

// FetchResult 是一次抓取的结果。Complete 只在平台完整枚举了目标范围时为 true，
// 也只有此时才允许软删除旧数据。
type FetchResult struct {
	Courses     []Course
	Assignments []Assignment
	Warnings    []FetchIssue
	ServerNow   *time.Time
	Complete    bool
}

// FetchIssue 记录一次部分失败或警告。
type FetchIssue struct {
	Scope   IssueScope
	Ref     string
	Kind    IssueKind
	Message string
	RawPath string
}

// FetchRun 是 fetch_runs 表对应的刷新记录。
type FetchRun struct {
	ID            int64
	AccountID     int64
	Platform      Platform
	StartedAt     time.Time
	FinishedAt    *time.Time
	Status        RunStatus
	CoursesTotal  int
	CoursesOK     int
	ItemsSeen     int
	ItemsUpserted int
	WarningCount  int
	ErrorSummary  string
}

// Progress 是刷新进度事件的数据。
type Progress struct {
	Platform Platform
	Phase    string
	Current  int
	Total    int
	Message  string
}

// ProgressFunc 由 service 注入，适配器在每个阶段调用。
type ProgressFunc func(Progress)

// QRState 是雨课堂扫码登录的轮询状态。
type QRState string

const (
	QRWaiting   QRState = "waiting"
	QRScanned   QRState = "scanned"
	QRSucceeded QRState = "succeeded"
	QRExpired   QRState = "expired"
	QRFailed    QRState = "failed"
)

// QRChallenge 是一张待扫描的二维码。
type QRChallenge struct {
	ID        string
	Content   string
	ExpiresAt time.Time
}

// PasswordCredential 是密码登录输入。
type PasswordCredential struct {
	Phone    string
	Password string
}
