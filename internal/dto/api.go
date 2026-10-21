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
}

type AssignmentView struct {
	ID               int64  `json:"id"`
	Platform         string `json:"platform"`
	CourseName       string `json:"courseName"`
	CourseExternalID string `json:"courseExternalId"`
	Title            string `json:"title"`
	Status           string `json:"status"`
	Phase            string `json:"phase"`
	DeadlineAt       string `json:"deadlineAt,omitempty"`
	DeadlineLocal    string `json:"deadlineLocal,omitempty"`
	DeadlineSource   string `json:"deadlineSource"`
	DeadlineRaw      string `json:"deadlineRaw,omitempty"`
	DetailURL        string `json:"detailUrl,omitempty"`
	FirstSeenAt      string `json:"firstSeenAt"`
	LastSeenAt       string `json:"lastSeenAt"`
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
