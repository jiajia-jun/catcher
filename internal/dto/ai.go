package dto

// AIResult.Data 是经过显式 JSON 转换的展示数据，不绑定领域实体或密钥。
type AIResult struct {
	OK    bool           `json:"ok"`
	Data  map[string]any `json:"data,omitempty"`
	Error *APIError      `json:"error,omitempty"`
}
type AISettingsRequest struct {
	Enabled           bool   `json:"enabled"`
	BaseURL           string `json:"baseUrl"`
	Model             string `json:"model"`
	MaxInputChars     int    `json:"maxInputChars"`
	MaxOutputTokens   int    `json:"maxOutputTokens"`
	MonthlyTokenLimit int    `json:"monthlyTokenLimit"`
	TimeoutSeconds    int    `json:"timeoutSeconds"`
	APIKey            string `json:"apiKey"`
	ClearKey          bool   `json:"clearKey"`
}
type AITimeRange struct {
	StartAt string `json:"startAt"`
	EndAt   string `json:"endAt"`
}
type AIInput struct {
	Kind          string        `json:"kind"`
	Question      string        `json:"question"`
	AssignmentIDs []int64       `json:"assignmentIds"`
	StartAt       string        `json:"startAt"`
	EndAt         string        `json:"endAt"`
	Snapshot      string        `json:"snapshot"`
	Blocked       []AITimeRange `json:"blocked"`
}
type AIStudyTask struct {
	ID           string   `json:"id"`
	AssignmentID int64    `json:"assignmentId"`
	Title        string   `json:"title"`
	StartAt      string   `json:"startAt"`
	Minutes      int      `json:"minutes"`
	Steps        []string `json:"steps"`
	Reason       string   `json:"reason"`
	Done         bool     `json:"done"`
}
type AIConfirmRequest struct {
	JobID string        `json:"jobId"`
	Tasks []AIStudyTask `json:"tasks"`
}
type AICompleteRequest struct {
	PlanID string `json:"planId"`
	TaskID string `json:"taskId"`
	Done   bool   `json:"done"`
}
