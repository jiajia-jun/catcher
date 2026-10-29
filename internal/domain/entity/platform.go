package entity

import "fmt"

// Platform 是抓取目标平台的稳定标识。它以字符串落库，禁止随意改名
type Platform string

const (
	PlatformChaoxing  Platform = "chaoxing"
	PlatformYuketang  Platform = "yuketang"
	PlatformTronclass Platform = "tronclass"
	PlatformPigai     Platform = "pigai"
)

// AllPlatforms 返回 UI 与初始化使用的固定顺序
func AllPlatforms() []Platform {
	return []Platform{PlatformChaoxing, PlatformYuketang, PlatformTronclass, PlatformPigai}
}

func (p Platform) Valid() bool {
	switch p {
	case PlatformChaoxing, PlatformYuketang, PlatformTronclass, PlatformPigai:
		return true
	default:
		return false
	}
}

// DisplayName 用于界面展示
func (p Platform) DisplayName() string {
	switch p {
	case PlatformChaoxing:
		return "学习通"
	case PlatformYuketang:
		return "雨课堂"
	case PlatformTronclass:
		return "学在重邮"
	case PlatformPigai:
		return "批改网"
	default:
		return string(p)
	}
}

// ParsePlatform 把外部字符串转成 Platform，并拒绝未知值
func ParsePlatform(s string) (Platform, error) {
	p := Platform(s)
	if !p.Valid() {
		return "", fmt.Errorf("未知平台 %q", s)
	}
	return p, nil
}

// LoginState 描述账号当前的登录态
type LoginState string

const (
	LoginDisconnected LoginState = "disconnected" // 从未登录
	LoginConnected    LoginState = "connected"    // 登录态可用
	LoginNeedsLogin   LoginState = "needs_login"  // 登录态失效，需要用户操作
	LoginError        LoginState = "error"        // 登录/续期出现可重试错误
)

func (s LoginState) Valid() bool {
	switch s {
	case LoginDisconnected, LoginConnected, LoginNeedsLogin, LoginError:
		return true
	default:
		return false
	}
}

// RunStatus 是一次平台刷新任务的终态
type RunStatus string

const (
	RunRunning  RunStatus = "running"
	RunSuccess  RunStatus = "success"
	RunPartial  RunStatus = "partial"
	RunFailed   RunStatus = "failed"
	RunCanceled RunStatus = "canceled"
)

// IssueScope 标记问题影响的范围。
type IssueScope string

const (
	ScopePlatform   IssueScope = "platform"
	ScopeCourse     IssueScope = "course"
	ScopeAssignment IssueScope = "assignment"
)

// IssueKind 是问题分类，决定 UI 提示与是否可重试。
type IssueKind string

const (
	IssueAuth     IssueKind = "auth"
	IssueNetwork  IssueKind = "network"
	IssueParse    IssueKind = "parse"
	IssueBusiness IssueKind = "business"
	IssueCanceled IssueKind = "canceled"
	IssueInternal IssueKind = "internal"
)

// SubmissionStatus 只有三态。认不出的状态必须是 unknown，不能推断成已交。
type SubmissionStatus string

const (
	StatusSubmitted   SubmissionStatus = "submitted"
	StatusUnsubmitted SubmissionStatus = "unsubmitted"
	StatusUnknown     SubmissionStatus = "unknown"
)

func (s SubmissionStatus) Valid() bool {
	switch s {
	case StatusSubmitted, StatusUnsubmitted, StatusUnknown:
		return true
	default:
		return false
	}
}

// DeadlineSource 记录截止时间是怎么来的，便于界面解释与排查。
type DeadlineSource string

const (
	DeadlinePlatformExact    DeadlineSource = "platform_exact"    // 平台直接给出
	DeadlineDetailExact      DeadlineSource = "detail_exact"      // 详情页月-日 + 年份
	DeadlineDerivedRemaining DeadlineSource = "derived_remaining" // 当前时间 + 剩余时间
	DeadlineInferredYear     DeadlineSource = "inferred_year"     // 已截止条目的年份推断
	DeadlineUnknown          DeadlineSource = "unknown"
)

func (s DeadlineSource) Valid() bool {
	switch s {
	case DeadlinePlatformExact, DeadlineDetailExact, DeadlineDerivedRemaining,
		DeadlineInferredYear, DeadlineUnknown:
		return true
	default:
		return false
	}
}
