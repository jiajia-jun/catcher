package handle

import (
	"testing"
	"time"

	"catcher/internal/domain/entity"
)

// 手动值优先展示，平台原值仍要带出去给界面做提示
func TestToAssignmentViewUsesEffectiveStatus(t *testing.T) {
	deadline := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	manual := entity.StatusSubmitted
	markedAt := time.Date(2026, 10, 24, 8, 30, 0, 0, time.UTC)
	item := entity.Assignment{
		ID:               1,
		Platform:         entity.PlatformYuketang,
		Title:            "手动作业",
		Status:           entity.StatusUnknown,
		ManualStatus:     &manual,
		ManualStatusAt:   &markedAt,
		Deadline:         &deadline,
		DeadlineSource:   entity.DeadlinePlatformExact,
		FirstSeenAt:      markedAt,
		LastSeenAt:       markedAt,
		CourseExternalID: "1001",
	}

	view := toAssignmentView(item, markedAt)

	if view.Status != string(entity.StatusSubmitted) {
		t.Fatalf("有效状态 = %s, want submitted", view.Status)
	}
	if view.PlatformStatus != string(entity.StatusUnknown) {
		t.Fatalf("平台状态 = %s, want unknown", view.PlatformStatus)
	}
	if view.ManualStatus != string(entity.StatusSubmitted) {
		t.Fatalf("手动状态 = %s", view.ManualStatus)
	}
	if view.ManualStatusAt != "2026-10-24T08:30:00Z" {
		t.Fatalf("手动时间 = %s", view.ManualStatusAt)
	}
	if view.DeadlineLocal == "" {
		t.Fatal("截止时间未转换成本地时间")
	}
}

// 没有手动值时，有效状态必须等于平台状态
func TestToAssignmentViewWithoutManualStatus(t *testing.T) {
	item := entity.Assignment{
		Platform: entity.PlatformChaoxing,
		Status:   entity.StatusUnsubmitted,
	}
	view := toAssignmentView(item, time.Now())
	if view.Status != string(entity.StatusUnsubmitted) {
		t.Fatalf("有效状态 = %s", view.Status)
	}
	if view.PlatformStatus != string(entity.StatusUnsubmitted) {
		t.Fatalf("平台状态 = %s", view.PlatformStatus)
	}
	if view.ManualStatus != "" || view.ManualStatusAt != "" {
		t.Fatalf("不应带出手动字段: %#v", view)
	}
}
