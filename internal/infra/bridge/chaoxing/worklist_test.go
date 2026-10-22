package chaoxing

import (
	"strings"
	"testing"
	"time"

	"catcher/internal/domain/entity"
)

const worklistFixture = `<html><body>
<ul class="work">
<li onclick="goTask(this);" data="https://mooc1-api.chaoxing.com/mooc-ans/android/mtaskmsgspecial?taskrefId=1001&amp;msgId=0&amp;courseId=214986481&amp;userId=9&amp;clazzId=7&amp;type=work&amp;enc_task=abc">
  <i class="icon task-work"></i>
  <span aria-label="作业名称第一次作业作业状态未提交所属课程大学物理AB...剩余时间312小时55分钟"></span>
</li>
<li onclick="goTask(this);" data="https://mooc1-api.chaoxing.com/mooc-ans/android/mtaskmsgspecial?taskrefId=1002&amp;courseId=266457278&amp;type=work">
  <i class="redPoint"></i><i class="icon task-work-gray"></i>
  <span aria-label="作业名称作业补交 （前面有未及时提交的同学，今天内完成补交）20260622作业状态未提交所属课程概率论与数理..."></span>
</li>
<li onclick="goTask(this);" data="https://mooc1-api.chaoxing.com/mooc-ans/android/mtaskmsgspecial?taskrefId=1003&amp;courseId=264416084&amp;type=work">
  <i class="icon task-work-gray"></i>
  <span aria-label="作业名称期末补交作业状态已完成所属课程高等数学A(..."></span>
</li>
</ul>
<script>var haveMore = "0";</script>
</body></html>`

func TestParseWorkList(t *testing.T) {
	page := parseWorkList(worklistFixture)
	if !page.Complete {
		t.Fatalf("haveMore=0 时应完整: %#v", page.Warnings)
	}
	if len(page.Items) != 3 {
		t.Fatalf("条目数 = %d, want 3", len(page.Items))
	}

	first := page.Items[0]
	if first.Title != "第一次作业" {
		t.Fatalf("标题 = %q", first.Title)
	}
	if first.CourseID != "214986481" {
		t.Fatalf("courseId = %q（不能过 float64）", first.CourseID)
	}
	if first.Status != entity.StatusUnsubmitted {
		t.Fatalf("状态 = %s", first.Status)
	}
	if !first.HasRemaining || first.Remaining != 312*time.Hour+55*time.Minute {
		t.Fatalf("剩余时间 = %v, has=%v", first.Remaining, first.HasRemaining)
	}
	if first.Ended {
		t.Fatal("task-work.png 应是进行中")
	}

	second := page.Items[1]
	if second.Title != "作业补交 （前面有未及时提交的同学，今天内完成补交）20260622" {
		t.Fatalf("含日期/括号的标题 = %q", second.Title)
	}
	if second.HasRemaining {
		t.Fatal("没有剩余时间段时不应有剩余时间")
	}
	if !second.Ended {
		t.Fatal("task-work-gray 应已结束")
	}

	third := page.Items[2]
	if third.Status != entity.StatusSubmitted {
		t.Fatalf("已完成状态 = %s", third.Status)
	}
}

func TestParseAriaLabelRightToLeft(t *testing.T) {
	// 标题里故意塞入两个定界符，必须从右往左锚到真正的定界符
	aria := "作业名称章节作业状态说明所属课程测试作业状态未提交所属课程高等数学剩余时间1天2小时3分钟"
	title, status, course, remaining, has, ok := parseAriaLabel(aria)
	if !ok {
		t.Fatal("解析失败")
	}
	if title != "章节作业状态说明所属课程测试" {
		t.Fatalf("标题 = %q", title)
	}
	if status != "未提交" {
		t.Fatalf("状态 = %q", status)
	}
	if course != "高等数学" {
		t.Fatalf("课程 = %q", course)
	}
	if !has || remaining != "1天2小时3分钟" {
		t.Fatalf("剩余 = %q, has=%v", remaining, has)
	}
}

func TestParseAriaLabelMissingStatus(t *testing.T) {
	if _, _, _, _, _, ok := parseAriaLabel("作业名称只有标题"); ok {
		t.Fatal("缺少定界符时必须显式失败")
	}
}

func TestParseWorkListHaveMoreNonZero(t *testing.T) {
	page := parseWorkList(strings.Replace(worklistFixture, `haveMore = "0"`, `haveMore = "1"`, 1))
	if page.Complete {
		t.Fatal("haveMore 非 0 时必须不完整")
	}
	if !hasIssueKind(page.Warnings, entity.IssueBusiness) {
		t.Fatalf("缺少 business 警告: %#v", page.Warnings)
	}
}

func TestParseWorkListHaveMoreMissing(t *testing.T) {
	body := strings.Replace(worklistFixture, `<script>var haveMore = "0";</script>`, "", 1)
	page := parseWorkList(body)
	if page.Complete {
		t.Fatal("缺少 haveMore 时必须不完整")
	}
	if !hasIssueKind(page.Warnings, entity.IssueParse) {
		t.Fatalf("缺少 parse 警告: %#v", page.Warnings)
	}
}

func TestParseWorkListUnknownStatusIsNotSubmitted(t *testing.T) {
	body := strings.Replace(worklistFixture, "作业状态已完成", "作业状态已过期", 1)
	page := parseWorkList(body)
	if len(page.Items) != 3 {
		t.Fatalf("条目数 = %d", len(page.Items))
	}
	if page.Items[2].Status != entity.StatusUnknown {
		t.Fatalf("认不出的状态 = %s, want unknown", page.Items[2].Status)
	}
	if !hasIssueKind(page.Warnings, entity.IssueParse) {
		t.Fatalf("认不出的状态必须记 warning: %#v", page.Warnings)
	}
}

// 真实环境里「待批阅」大量出现：它是已提交，不能当未知、更不能当未提交
func TestParseWorkListAwaitingGradeIsSubmitted(t *testing.T) {
	body := strings.Replace(worklistFixture, "作业状态已完成", "作业状态待批阅", 1)
	page := parseWorkList(body)
	if len(page.Items) != 3 {
		t.Fatalf("条目数 = %d", len(page.Items))
	}
	if page.Items[2].Status != entity.StatusSubmitted {
		t.Fatalf("待批阅 = %s, want submitted", page.Items[2].Status)
	}
	if len(page.Warnings) != 0 {
		t.Fatalf("待批阅不应产生 warning: %#v", page.Warnings)
	}
}

func TestParseWorkListStructureDrift(t *testing.T) {
	body := strings.ReplaceAll(worklistFixture, `<li onclick="goTask(this);"`, `<li class="changed"`)
	// 页面结构变了但脚本里仍会提到 goTask，此时必须靠「匹配数为 0」判定不完整
	body = strings.Replace(body, "</body>", `<script>function goTask(el){}</script></body>`, 1)
	page := parseWorkList(body)
	if page.Complete {
		t.Fatal("goTask 还在但正则匹配不到时必须不完整")
	}
	if len(page.Items) != 0 {
		t.Fatalf("不应解析出条目: %#v", page.Items)
	}
}

func TestParseRemaining(t *testing.T) {
	cases := []struct {
		raw  string
		want time.Duration
		ok   bool
	}{
		{"312小时55分钟", 312*time.Hour + 55*time.Minute, true},
		{"3天", 72 * time.Hour, true},
		{"2 天 3 小时", 51 * time.Hour, true},
		{"", 0, false},
		{"即将截止", 0, false},
	}
	for _, c := range cases {
		got, ok := parseRemaining(c.raw)
		if ok != c.ok || got != c.want {
			t.Errorf("%q = %v,%v want %v,%v", c.raw, got, ok, c.want, c.ok)
		}
	}
}

func TestParseStatusIcon(t *testing.T) {
	if ended, known := parseStatusIcon(`<img src="/a/task-work.png">`); ended || !known {
		t.Fatalf("task-work = %v,%v", ended, known)
	}
	if ended, known := parseStatusIcon(`<img src="/a/task-work-gray.png">`); !ended || !known {
		t.Fatalf("task-work-gray = %v,%v", ended, known)
	}
	if _, known := parseStatusIcon(`<i class="redPoint"></i>`); known {
		t.Fatal("redPoint 不能当状态图标")
	}
}

func hasIssueKind(issues []entity.FetchIssue, kind entity.IssueKind) bool {
	for _, issue := range issues {
		if issue.Kind == kind {
			return true
		}
	}
	return false
}
