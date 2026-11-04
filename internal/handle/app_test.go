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

// detail_url 有一部分来自平台 HTML，非 http/https 一律置空
func TestToAssignmentViewSanitizesDetailURL(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{name: "https 原样保留", raw: "https://example.com/course/1", want: "https://example.com/course/1"},
		{name: "http 原样保留", raw: "http://example.com/a", want: "http://example.com/a"},
		{name: "大写 scheme 原样保留", raw: "HTTPS://EXAMPLE.COM/a", want: "HTTPS://EXAMPLE.COM/a"},
		{name: "javascript 置空", raw: "javascript:alert(1)", want: ""},
		{name: "file 置空", raw: "file:///C:/secret.txt", want: ""},
		{name: "相对路径置空", raw: "/relative/path", want: ""},
		{name: "无 host 置空", raw: "https://", want: ""},
		{name: "残缺 scheme 置空", raw: "://bad", want: ""},
		{name: "空串保持空", raw: "", want: ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			item := entity.Assignment{DetailURL: c.raw}
			if got := toAssignmentView(item, time.Now()).DetailURL; got != c.want {
				t.Fatalf("DetailURL = %q, want %q", got, c.want)
			}
		})
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
