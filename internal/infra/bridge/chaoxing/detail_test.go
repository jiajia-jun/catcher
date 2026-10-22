package chaoxing

import (
	"testing"
	"time"

	"catcher/internal/domain/entity"
)

func TestParseDetailDeadlineAria(t *testing.T) {
	body := `<h4>截止时间：09-25 22:55</h4>
	<span aria-label="截止时间09月25日22时55分18秒"></span>`
	d := parseDetailDeadline(body)
	if d == nil {
		t.Fatal("未解析到截止时间")
	}
	if d.Month != 9 || d.Day != 25 || d.Hour != 22 || d.Minute != 55 || d.Second != 18 {
		t.Fatalf("解析结果 = %#v", d)
	}
}

func TestParseDetailDeadlineH4Fallback(t *testing.T) {
	d := parseDetailDeadline(`<h4>截止时间：06-23 23:00</h4>`)
	if d == nil {
		t.Fatal("未解析到 h4 截止时间")
	}
	if d.Month != 6 || d.Day != 23 || d.Hour != 23 || d.Minute != 0 {
		t.Fatalf("解析结果 = %#v", d)
	}
}

func TestResolveDeadlineFromRemaining(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, shanghai)
	item := workItem{
		Title:        "第一次作业",
		CourseID:     "1",
		Remaining:    312*time.Hour + 55*time.Minute,
		HasRemaining: true,
		RemainingRaw: "312小时55分钟",
	}
	detail := &detailDeadline{Raw: "截止时间11月04日12时55分00秒", Month: 11, Day: 4, Hour: 12, Minute: 55}

	deadline, source, _, warnings := resolveDeadline(item, detail, now)
	if source != entity.DeadlineDerivedRemaining {
		t.Fatalf("来源 = %s", source)
	}
	want := now.Add(item.Remaining).UTC()
	if deadline == nil || !deadline.Equal(want) {
		t.Fatalf("截止 = %v, want %v", deadline, want)
	}
	if len(warnings) != 0 {
		t.Fatalf("交叉校验吻合时不应有 warning: %#v", warnings)
	}
}

func TestResolveDeadlineRemainingMismatchWarns(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, shanghai)
	item := workItem{
		Title:        "第一次作业",
		Remaining:    2 * time.Hour,
		HasRemaining: true,
		RemainingRaw: "2小时",
	}
	detail := &detailDeadline{Raw: "截止时间10月20日08时00分00秒", Month: 10, Day: 20, Hour: 8}

	_, source, _, warnings := resolveDeadline(item, detail, now)
	if source != entity.DeadlineDerivedRemaining {
		t.Fatalf("来源 = %s", source)
	}
	if len(warnings) == 0 {
		t.Fatal("剩余时间与详情页不一致时必须 warning")
	}
}

func TestResolveDeadlineClosedThisYear(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, shanghai)
	item := workItem{Title: "作业补交", CourseID: "1"}
	detail := &detailDeadline{Raw: "截止时间06月23日23时00分18秒", Month: 6, Day: 23, Hour: 23, Minute: 0, Second: 18}

	deadline, source, _, warnings := resolveDeadline(item, detail, now)
	if source != entity.DeadlineInferredYear {
		t.Fatalf("来源 = %s", source)
	}
	want := time.Date(2026, 6, 23, 23, 0, 18, 0, shanghai).UTC()
	if deadline == nil || !deadline.Equal(want) {
		t.Fatalf("截止 = %v, want %v", deadline, want)
	}
	if len(warnings) != 0 {
		t.Fatalf("不应有 warning: %#v", warnings)
	}
}

func TestResolveDeadlineClosedPreviousYear(t *testing.T) {
	now := time.Date(2026, 1, 5, 12, 0, 0, 0, shanghai)
	item := workItem{Title: "期末补交", CourseID: "1"}
	detail := &detailDeadline{Raw: "截止时间12月30日23时59分00秒", Month: 12, Day: 30, Hour: 23, Minute: 59}

	deadline, _, _, _ := resolveDeadline(item, detail, now)
	want := time.Date(2025, 12, 30, 23, 59, 0, 0, shanghai).UTC()
	if deadline == nil || !deadline.Equal(want) {
		t.Fatalf("截止 = %v, want %v", deadline, want)
	}
}

func TestResolveDeadlineMissingDetail(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, shanghai)
	item := workItem{Title: "作业补交", CourseID: "1"}

	deadline, source, _, warnings := resolveDeadline(item, nil, now)
	if deadline != nil || source != entity.DeadlineUnknown {
		t.Fatalf("截止 = %v, 来源 = %s", deadline, source)
	}
	if len(warnings) == 0 {
		t.Fatal("没有详情页截止时间时必须 warning")
	}
}
