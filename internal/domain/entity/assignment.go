package entity

import (
	"strings"
	"time"
)

// Phase 由截止时间和当前时间即时计算，不落库。
type Phase string

const (
	PhaseOpen    Phase = "open"
	PhaseClosed  Phase = "closed"
	PhaseUnknown Phase = "unknown"
)

// PhaseOf 根据截止时间判断作业阶段。
func PhaseOf(deadline *time.Time, now time.Time) Phase {
	if deadline == nil {
		return PhaseUnknown
	}
	if now.Before(*deadline) {
		return PhaseOpen
	}
	return PhaseClosed
}

// Course 是一门课程。雨课堂用 (ExternalID, ClassroomID) 作为复合身份。
type Course struct {
	ID          int64
	AccountID   int64
	Platform    Platform
	ExternalID  string // 平台课程 id，按原始字面量保存
	ClassroomID string // 雨课堂 classroom_id；其他平台为空串
	Name        string
	SemesterKey string
	Active      bool
	FirstSeenAt time.Time
	LastSeenAt  time.Time
}

// Assignment 是一条作业。
type Assignment struct {
	ID               int64
	AccountID        int64
	Platform         Platform
	CourseID         int64
	CourseExternalID string
	ClassroomID      string // 雨课堂 classroom_id，用于关联复合身份的课程；其他平台为空
	CourseName       string
	ExternalID       string
	DedupKey         string
	Title            string
	Status           SubmissionStatus // 平台给出的状态
	// ManualStatus 是用户手动标记的状态；nil 表示没有手动覆盖
	ManualStatus   *SubmissionStatus
	ManualStatusAt *time.Time
	Deadline       *time.Time
	DeadlineSource DeadlineSource
	DeadlineRaw    string
	DetailURL      string
	Description    string
	FirstSeenAt    time.Time
	LastSeenAt     time.Time
	Active         bool
	LastRunID      int64
}

// EffectiveStatus 返回界面与统计使用的状态：有手动值以手动值为准
func (a Assignment) EffectiveStatus() SubmissionStatus {
	if a.ManualStatus != nil {
		return *a.ManualStatus
	}
	return a.Status
}

// AssignmentFilter 是查询作业的条件。零值表示默认视图：
// 只返回 active 记录，并隐藏已提交作业。
type AssignmentFilter struct {
	Platforms     []Platform
	CourseIDs     []int64
	Statuses      []SubmissionStatus
	ShowSubmitted bool
	Keyword       string
	// TermOnly 只看本学期；学期起点由 service 按当前时间换算成 DeadlineFrom
	TermOnly bool
	// DeadlineFrom 只要截止时间不早于该时刻的作业；截止时间未知的保留
	DeadlineFrom *time.Time
}

// NormalizeTitle 只做 TrimSpace + 连续空白折叠。
// 不要删除日期、括号、全角字符：它们是标题的一部分，也是去重键的一部分。
func NormalizeTitle(s string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
}

// ChaoxingDedupKey 对应「用户 + 平台 + 课程 + 标题」，deadline 不参与。
func ChaoxingDedupKey(courseID, title string) string {
	return "chaoxing:" + courseID + ":" + NormalizeTitle(title)
}

// YuketangDedupKey 使用稳定的活动 id。
func YuketangDedupKey(courseID, classroomID, activityID string) string {
	return "yuketang:" + courseID + ":" + classroomID + ":" + activityID
}

// TronclassDedupKey 使用课程 id + 活动 id；学习通以外的平台活动 id 稳定。
func TronclassDedupKey(courseID, activityID string) string {
	return "tronclass:" + courseID + ":" + activityID
}
