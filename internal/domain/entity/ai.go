package entity

import "time"

// AISettings 的 Key 只供后端使用，不能序列化、写日志或放进模型上下文。
type AISettings struct {
	Enabled           bool   `json:"enabled"`
	BaseURL           string `json:"baseUrl"`
	Model             string `json:"model"`
	MaxInputChars     int    `json:"maxInputChars"`
	MaxOutputTokens   int    `json:"maxOutputTokens"`
	MonthlyTokenLimit int    `json:"monthlyTokenLimit"`
	TimeoutSeconds    int    `json:"timeoutSeconds"`
	Key               string `json:"-"`
}

func DefaultAISettings() AISettings {
	return AISettings{BaseURL: "https://api.deepseek.com", Model: "deepseek-flash", MaxInputChars: 12000, MaxOutputTokens: 2048, MonthlyTokenLimit: 200000, TimeoutSeconds: 60}
}

// AISource 是经本地筛选的事实。账号指纹只参与一致性检查，不发送给模型。
type AISource struct {
	ID             int64      `json:"id"`
	Ref            string     `json:"ref"`
	Platform       string     `json:"platform"`
	Course         string     `json:"course"`
	Title          string     `json:"title"`
	Status         string     `json:"status"`
	PlatformStatus string     `json:"platformStatus"`
	Manual         bool       `json:"manual"`
	Deadline       *time.Time `json:"deadline"`
	DeadlineSource string     `json:"deadlineSource"`
	HasDescription bool       `json:"hasDescription"`
	LastSeenAt     time.Time  `json:"lastSeenAt"`
	AccountStamp   string     `json:"-"`
}

type AICoverage struct {
	Platform    string     `json:"platform"`
	SeenAt      *time.Time `json:"seenAt"`
	Complete    bool       `json:"complete"`
	Warnings    int        `json:"warnings"`
	LatestState string     `json:"latestState"`
}

type AISnapshot struct {
	Sources     []AISource   `json:"sources"`
	Coverage    []AICoverage `json:"coverage"`
	Fingerprint string       `json:"fingerprint"`
}

type AIInput struct {
	Kind          string        `json:"kind"` // query | plan | test
	Question      string        `json:"question"`
	AssignmentIDs []int64       `json:"assignmentIds"`
	StartAt       string        `json:"startAt"`
	EndAt         string        `json:"endAt"`
	Snapshot      string        `json:"snapshot"`
	Blocked       []AITimeRange `json:"blocked"`
}

type AITimeRange struct {
	StartAt string `json:"startAt"`
	EndAt   string `json:"endAt"`
}

type StudyTask struct {
	ID           string   `json:"id"`
	AssignmentID int64    `json:"assignmentId"`
	Title        string   `json:"title"`
	StartAt      string   `json:"startAt"`
	Minutes      int      `json:"minutes"`
	Steps        []string `json:"steps"`
	Reason       string   `json:"reason"`
	Done         bool     `json:"done"`
}

type StudyPlan struct {
	ID          string        `json:"id"`
	Title       string        `json:"title"`
	Tasks       []StudyTask   `json:"tasks"`
	Notes       []string      `json:"notes"`
	SourceIDs   []int64       `json:"sourceIds"`
	Fingerprint string        `json:"fingerprint"`
	StartAt     string        `json:"startAt"`
	EndAt       string        `json:"endAt"`
	CreatedAt   time.Time     `json:"createdAt"`
	Blocked     []AITimeRange `json:"blocked"`
	Sources     []AISource    `json:"sources"`
	Stale       bool          `json:"stale"`
}

type AIJob struct {
	ID           string       `json:"id"`
	Kind         string       `json:"kind"`
	State        string       `json:"state"`
	Error        string       `json:"error,omitempty"`
	Sources      []AISource   `json:"sources"`
	Coverage     []AICoverage `json:"coverage"`
	Notes        []string     `json:"notes"`
	Plan         *StudyPlan   `json:"plan,omitempty"`
	InputTokens  int          `json:"inputTokens"`
	OutputTokens int          `json:"outputTokens"`
	UsageKnown   bool         `json:"usageKnown"`
}

type AIModelRequest struct {
	Settings AISettings
	System   string
	User     string
}

type AIModelResult struct {
	Content      string
	InputTokens  int
	OutputTokens int
	UsageKnown   bool
}

type AIUsage struct {
	Month          string `json:"month"`
	ChargedTokens  int    `json:"chargedTokens"`
	ReservedTokens int    `json:"reservedTokens"`
	Runs           int    `json:"runs"`
}
