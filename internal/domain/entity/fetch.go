package entity

import "time"

// FetchOptions 是适配器执行一次抓取时可用的选项
type FetchOptions struct {
	Now func() time.Time
	// OnSessionUpdated 在平台轮换会话或累积 cookie 后立即调用
	// 适配器通过它把最新登录态交给上层加密持久化，降低进程崩溃导致登录态丢失的风险
	OnSessionUpdated func(Session) error
}

// FetchResult 是一次抓取的结果
// Complete 在平台完整枚举了目标范围时为 true，此时才允许软删除旧数据
type FetchResult struct {
	Courses     []Course
	Assignments []Assignment
	Warnings    []FetchIssue
	ServerNow   *time.Time
	// Complete 表示本次完整枚举了目标范围
	Complete bool
	// CourseListComplete 表示课程列表本身被完整枚举
	CourseListComplete bool
	// PartialByDesign 表示本次未抓全，原因是平台只暴露这么多数据
	// （例如批改网只提供列表第一页），本次刷新仍按成功处理
	// 它只影响刷新记录的成功判定，软删除仍按 Complete / CourseListComplete 走
	PartialByDesign bool
	// FailedCourses 是本次作业列表抓取失败的课程 external_id
	// nil 表示课程列表不完整，此时跳过清理
	FailedCourses []string
}

// PersistInput 是一次抓取结果落库所需的全部上下文
//
// Complete 为 false 时只做 upsert，旧记录保留
// 课程列表完整（CourseListComplete）时清理旧课程
// 作业只清理不在 FailedCourses 里的课程
type PersistInput struct {
	AccountID int64
	Platform  Platform
	RunID     int64
	Result    FetchResult
	At        time.Time
}

// FetchIssue 记录一次部分失败或警告
type FetchIssue struct {
	Scope   IssueScope
	Ref     string
	Kind    IssueKind
	Message string
	RawPath string
}

// FetchRun 是 fetch_runs 表对应的刷新记录
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

// Progress 是刷新进度事件的数据
type Progress struct {
	Platform Platform
	Phase    string
	Current  int
	Total    int
	Message  string
}

// ProgressFunc 由 service 注入，适配器在每个阶段调用
type ProgressFunc func(Progress)

// QRState 是扫码登录的轮询状态
type QRState string

const (
	QRWaiting   QRState = "waiting"
	QRScanned   QRState = "scanned"
	QRSucceeded QRState = "succeeded"
	QRExpired   QRState = "expired"
	QRFailed    QRState = "failed"
)

// QRChallenge 是一张待扫描的二维码
type QRChallenge struct {
	ID        string
	Content   string
	ExpiresAt time.Time
}

// PasswordCredential 是密码登录输入，落库前整体交给 secretstore 加密
type PasswordCredential struct {
	Phone    string `json:"phone"`
	Password string `json:"password"`
}
