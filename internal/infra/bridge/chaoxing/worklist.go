package chaoxing

import (
	"context"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"catcher/internal/domain/entity"
)

const workListPath = "/work/stu-work?ut=s"

// 文档给出的条目正则：data 是完整详情 URL，后面是该条目的 HTML 片段
var workItemPattern = regexp.MustCompile(`(?s)<li onclick="goTask\(this\);" data="([^"]+)">(.*?)</li>`)

var (
	haveMorePattern  = regexp.MustCompile(`(?s)var\s+haveMore\s*=\s*["']?(\d+)`)
	ariaLabelPattern = regexp.MustCompile(`aria-label="([^"]*)"`)
	remainingPattern = regexp.MustCompile(`(\d+)\s*(天|小时|分钟)`)
)

// workItem 是作业列表里的一条。死线时间要结合详情页再补
type workItem struct {
	DetailURL    string
	Title        string
	Status       entity.SubmissionStatus
	CourseID     string
	CourseName   string
	Remaining    time.Duration
	HasRemaining bool
	RemainingRaw string
	Ended        bool // 图标 task-work-gray.png
	IconKnown    bool
}

// worklistPage 是一次作业列表页的解析结果
type worklistPage struct {
	Items    []workItem
	Warnings []entity.FetchIssue
	Complete bool
}

// fetchWorkList 请求并解析作业列表。失败必须显式返回，绝不返回空列表冒充成功
func (c *Client) fetchWorkList(ctx context.Context, jar *cookieJar) (*worklistPage, error) {
	resp, err := c.get(ctx, c.baseURL+workListPath, jar)
	if err != nil {
		return nil, err
	}
	body := string(resp.Raw)
	if strings.Contains(body, "请重新登录") || strings.Contains(body, "fanyalogin") {
		return nil, fmt.Errorf("%w: 作业列表返回登录页", entity.ErrAuthExpired)
	}
	return parseWorkList(body), nil
}

// parseWorkList 是纯函数，方便用 fixture 做回归
func parseWorkList(body string) *worklistPage {
	page := &worklistPage{Complete: true}

	haveMore, found := parseHaveMore(body)
	if !found {
		page.Complete = false
		page.Warnings = append(page.Warnings, entity.FetchIssue{
			Scope:   entity.ScopePlatform,
			Kind:    entity.IssueParse,
			Message: "作业列表未找到 haveMore，本次不做软删除",
		})
	} else if haveMore != "0" {
		// 翻页参数未知，v1 不猜；非 0 只标 partial，不软删除
		page.Complete = false
		page.Warnings = append(page.Warnings, entity.FetchIssue{
			Scope:   entity.ScopePlatform,
			Kind:    entity.IssueBusiness,
			Message: fmt.Sprintf("作业列表 haveMore=%s，可能还有下一页，本次不做软删除", haveMore),
		})
	}

	matches := workItemPattern.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 && strings.Contains(body, "goTask") {
		// 页面结构变了，但 haveMore 还在；不能因此把旧作业全部软删除
		page.Complete = false
		page.Warnings = append(page.Warnings, entity.FetchIssue{
			Scope:   entity.ScopePlatform,
			Kind:    entity.IssueParse,
			Message: "作业列表含有 goTask 但条目正则没有匹配到，页面结构可能已变化",
		})
	}
	for _, match := range matches {
		item, warnings, ok := parseWorkItem(match[1], match[2])
		page.Warnings = append(page.Warnings, warnings...)
		if !ok {
			// 有一条解析不了就无法断言完整枚举，保守地不软删除
			page.Complete = false
			continue
		}
		page.Items = append(page.Items, item)
	}
	return page
}

func parseHaveMore(body string) (string, bool) {
	match := haveMorePattern.FindStringSubmatch(body)
	if match == nil {
		return "", false
	}
	return match[1], true
}

// parseWorkItem 解析一条条目；第三个返回值表示这条是否可用于落库
func parseWorkItem(rawData, inner string) (workItem, []entity.FetchIssue, bool) {
	dataURL := html.UnescapeString(strings.TrimSpace(rawData))
	item := workItem{
		DetailURL: dataURL,
		CourseID:  courseIDFromURL(dataURL),
	}

	aria := pickAriaLabel(inner, "作业名称")
	if aria == "" {
		return item, []entity.FetchIssue{{
			Scope:   entity.ScopePlatform,
			Kind:    entity.IssueParse,
			Message: fmt.Sprintf("作业条目缺少 aria-label：%s", truncate(dataURL, 80)),
		}}, false
	}

	title, statusText, courseName, remainingRaw, hasRemaining, ok := parseAriaLabel(aria)
	if !ok {
		return item, []entity.FetchIssue{{
			Scope:   entity.ScopePlatform,
			Kind:    entity.IssueParse,
			Message: fmt.Sprintf("aria-label 无法解析：%s", truncate(aria, 120)),
		}}, false
	}
	item.Title = entity.NormalizeTitle(title)
	item.CourseName = courseName
	item.RemainingRaw = strings.TrimSpace(remainingRaw)
	item.HasRemaining = hasRemaining
	if item.CourseID == "" {
		return item, []entity.FetchIssue{{
			Scope:   entity.ScopePlatform,
			Kind:    entity.IssueParse,
			Message: fmt.Sprintf("作业 %q 的详情 URL 缺少 courseId", item.Title),
		}}, false
	}
	if item.Title == "" {
		return item, []entity.FetchIssue{{
			Scope:   entity.ScopePlatform,
			Kind:    entity.IssueParse,
			Message: "aria-label 解析出的作业标题为空",
		}}, false
	}

	var warnings []entity.FetchIssue
	warn := func(msg string) {
		warnings = append(warnings, entity.FetchIssue{
			Scope:   entity.ScopeAssignment,
			Ref:     item.CourseID,
			Kind:    entity.IssueParse,
			Message: msg,
		})
	}

	switch statusText {
	case "未提交":
		item.Status = entity.StatusUnsubmitted
	case "已完成":
		item.Status = entity.StatusSubmitted
	case "待批阅":
		// 待批阅 = 已交、老师还没批改；真实环境里大量出现，接口文档没写。
		// 它是已提交，不能当未提交（会误报），也不能当未知（会漏掉详情跳过）
		item.Status = entity.StatusSubmitted
	default:
		// 认不出的状态显式暴露，绝不默认成已完成
		item.Status = entity.StatusUnknown
		warn(fmt.Sprintf("作业 %q 的状态无法识别：%s", item.Title, truncate(statusText, 40)))
	}

	if hasRemaining {
		if duration, ok := parseRemaining(remainingRaw); ok {
			item.Remaining = duration
		} else {
			// 有「剩余时间」段但读不出来，按没有处理；年份补全不能再依赖它
			item.HasRemaining = false
			warn(fmt.Sprintf("作业 %q 的剩余时间无法解析：%s", item.Title, truncate(remainingRaw, 40)))
		}
	}

	item.Ended, item.IconKnown = parseStatusIcon(inner)
	if item.IconKnown && item.Ended && item.HasRemaining {
		warn(fmt.Sprintf("作业 %q 图标显示已结束，但仍有剩余时间 %s", item.Title, truncate(remainingRaw, 40)))
	}

	return item, warnings, true
}

// pickAriaLabel 取包含指定前缀的 aria-label，避免误取列表里的其它标注
func pickAriaLabel(inner, prefix string) string {
	for _, match := range ariaLabelPattern.FindAllStringSubmatch(inner, -1) {
		value := html.UnescapeString(match[1])
		if strings.HasPrefix(strings.TrimSpace(value), prefix) {
			return value
		}
	}
	return ""
}

// parseAriaLabel 按「作业名称<标题>作业状态<状态>所属课程<课程>剩余时间<剩余>」拆解。
//
// 三个定界符都从右往左找：标题是自由文本，可能自带日期、括号、空格，
// 理论上也可能自带同名字样；真正的定界符一定在标题之后
func parseAriaLabel(aria string) (title, statusText, courseName, remainingRaw string, hasRemaining, ok bool) {
	end := len(aria)

	idxRemaining := strings.LastIndex(aria, "剩余时间")
	if idxRemaining >= 0 {
		remainingRaw = aria[idxRemaining+len("剩余时间"):]
		hasRemaining = strings.TrimSpace(remainingRaw) != ""
		end = idxRemaining
	}

	idxCourse := strings.LastIndex(aria[:end], "所属课程")
	if idxCourse >= 0 {
		courseName = strings.TrimSpace(aria[idxCourse+len("所属课程") : end])
		end = idxCourse
	}

	idxStatus := strings.LastIndex(aria[:end], "作业状态")
	if idxStatus < 0 {
		return "", "", "", "", false, false
	}
	statusText = strings.TrimSpace(aria[idxStatus+len("作业状态") : end])

	idxTitle := strings.Index(aria[:idxStatus], "作业名称")
	if idxTitle < 0 {
		return "", "", "", "", false, false
	}
	title = strings.TrimSpace(aria[idxTitle+len("作业名称") : idxStatus])
	return title, statusText, courseName, strings.TrimSpace(remainingRaw), hasRemaining, true
}

// parseRemaining 把「312小时55分钟」这类相对时间累加。
// 剩余时间是相对值，必须在抓取当场换算成绝对时刻
func parseRemaining(raw string) (time.Duration, bool) {
	matches := remainingPattern.FindAllStringSubmatch(raw, -1)
	if len(matches) == 0 {
		return 0, false
	}
	var total time.Duration
	for _, match := range matches {
		n, err := strconv.Atoi(match[1])
		if err != nil {
			return 0, false
		}
		switch match[2] {
		case "天":
			total += time.Duration(n) * 24 * time.Hour
		case "小时":
			total += time.Duration(n) * time.Hour
		case "分钟":
			total += time.Duration(n) * time.Minute
		}
	}
	return total, true
}

// parseStatusIcon 读图标文件名判断作业是否还在进行。
// 不能用 redPoint：那是未读标记，会随阅读清除
func parseStatusIcon(inner string) (ended, known bool) {
	switch {
	case strings.Contains(inner, "task-work-gray"):
		return true, true
	case strings.Contains(inner, "task-work"):
		return false, true
	default:
		return false, false
	}
}

// courseIDFromURL 用 net/url 取查询参数，不做字符串切割
func courseIDFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(u.Query().Get("courseId"))
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}
