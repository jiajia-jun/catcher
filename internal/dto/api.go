// Package dto 定义 Wails 绑定方法的入参与出参。
//
// 这里只放传输结构，不放领域实体。所有方法都返回显式结果结构：
// Wails 绑定在 JS 侧只能拿到 error 的文本，无法携带错误码。
package dto

type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
	RawPath string `json:"rawPath,omitempty"`
}

type APIResult struct {
	OK    bool      `json:"ok"`
	Error *APIError `json:"error,omitempty"`
}

type CountsView struct {
	OpenUnsubmitted    int `json:"openUnsubmitted"`
	OverdueUnsubmitted int `json:"overdueUnsubmitted"`
	Unknown            int `json:"unknown"`
	Submitted          int `json:"submitted"`
}

type PlatformView struct {
	Platform     string     `json:"platform"`
	DisplayName  string     `json:"displayName"`
	LoginState   string     `json:"loginState"`
	LastRunAt    string     `json:"lastRunAt,omitempty"`
	LastRunState string     `json:"lastRunState,omitempty"`
	LastRunID    int64      `json:"lastRunId,omitempty"`
	Counts       CountsView `json:"counts"`
	Error        *APIError  `json:"error,omitempty"`
	Experimental bool       `json:"experimental"`
}

type BootstrapData struct {
	Platforms []PlatformView `json:"platforms"`
	Stats     CountsView     `json:"stats"`
	DataDir   string         `json:"dataDir"`
	Version   string         `json:"version"`
}

type BootstrapResult struct {
	OK    bool           `json:"ok"`
	Error *APIError      `json:"error,omitempty"`
	Data  *BootstrapData `json:"data,omitempty"`
}

type AssignmentFilter struct {
	Platforms     []string `json:"platforms,omitempty"`
	CourseIDs     []int64  `json:"courseIds,omitempty"`
	Statuses      []string `json:"statuses,omitempty"`
	ShowSubmitted bool     `json:"showSubmitted"`
	Keyword       string   `json:"keyword,omitempty"`
	// TermOnly 只看本学期（9 月 1 日起）；学期起点由后端计算
	TermOnly bool `json:"termOnly"`
}

type AssignmentView struct {
	ID               int64  `json:"id"`
	Platform         string `json:"platform"`
	CourseName       string `json:"courseName"`
	CourseExternalID string `json:"courseExternalId"`
	Title            string `json:"title"`
	Status           string `json:"status"`         // 有效状态：有手动值用手动值
	PlatformStatus   string `json:"platformStatus"` // 平台给出的原始状态
	ManualStatus     string `json:"manualStatus,omitempty"`
	ManualStatusAt   string `json:"manualStatusAt,omitempty"`
	Phase            string `json:"phase"`
	DeadlineAt       string `json:"deadlineAt,omitempty"`
	DeadlineLocal    string `json:"deadlineLocal,omitempty"`
	DeadlineSource   string `json:"deadlineSource"`
	DeadlineRaw      string `json:"deadlineRaw,omitempty"`
	DetailURL        string `json:"detailUrl,omitempty"`
	FirstSeenAt      string `json:"firstSeenAt"`
	LastSeenAt       string `json:"lastSeenAt"`
}

// AssignmentStatusRequest 手动标记一条作业的状态
// Status 为空串表示恢复平台值
type AssignmentStatusRequest struct {
	ID     int64  `json:"id"`
	Status string `json:"status"`
}

type AssignmentStatusResult struct {
	OK    bool            `json:"ok"`
	Item  *AssignmentView `json:"item,omitempty"`
	Error *APIError       `json:"error,omitempty"`
}

type AssignmentListResult struct {
	OK    bool             `json:"ok"`
	Error *APIError        `json:"error,omitempty"`
	Items []AssignmentView `json:"items"`
	Total int              `json:"total"`
}

type SettingsView struct {
	LogLevel       string `json:"logLevel"`
	RawEnabled     bool   `json:"rawEnabled"`
	RawRetention   int    `json:"rawRetentionDays"`
	IncludeHistory bool   `json:"includeHistory"`
	Theme          string `json:"theme"`
	DataDir        string `json:"dataDir"`
	Version        string `json:"version"`
}

type SettingsResult struct {
	OK       bool          `json:"ok"`
	Error    *APIError     `json:"error,omitempty"`
	Settings *SettingsView `json:"settings,omitempty"`
}

// RefreshStartResult 是刷新/扫码任务的受理结果
type RefreshStartResult struct {
	OK             bool      `json:"ok"`
	JobID          string    `json:"jobId,omitempty"`
	Platforms      []string  `json:"platforms,omitempty"`
	AlreadyRunning bool      `json:"alreadyRunning"`
	Error          *APIError `json:"error,omitempty"`
}

// PasswordLoginRequest 是学习通手机号+密码登录的入参。
// Password 只在本次调用内存在，不会出现在响应里
type PasswordLoginRequest struct {
	Phone    string `json:"phone"`
	Password string `json:"password"`
	Remember bool   `json:"remember"`
}

// PasswordLoginResult 是密码登录结果。明文密码绝不回传
type PasswordLoginResult struct {
	OK          bool      `json:"ok"`
	LoginState  string    `json:"loginState,omitempty"`
	DisplayName string    `json:"displayName,omitempty"`
	Error       *APIError `json:"error,omitempty"`
}

// FetchIssueView 是一次刷新的问题摘要，不含任何凭据
type FetchIssueView struct {
	Scope   string `json:"scope"`
	Ref     string `json:"ref,omitempty"`
	Kind    string `json:"kind"`
	Message string `json:"message"`
	RawPath string `json:"rawPath,omitempty"`
}

type FetchIssuesResult struct {
	OK    bool             `json:"ok"`
	Items []FetchIssueView `json:"items"`
	Error *APIError        `json:"error,omitempty"`
}
