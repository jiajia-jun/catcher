package chaoxing

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"catcher/internal/domain/entity"
)

var (
	// 详情页 aria-label 带秒，精确到分钟也能用
	detailAriaPattern = regexp.MustCompile(`aria-label="截止时间(\d{1,2})月(\d{1,2})日(\d{1,2})时(\d{2})分(?:(\d{2})秒)?"`)
	// <h4>截止时间：09-25 22:55</h4>
	detailH4Pattern = regexp.MustCompile(`截止时间[：:]\s*(\d{1,2})-(\d{1,2})\s+(\d{1,2}):(\d{2})`)
)

// deadlineTolerance 是「剩余时间推算」与「详情页截止时刻」允许的偏差。
// 剩余时间只到分钟，且以页面渲染时刻为基准，秒级到分钟级误差是正常的
const deadlineTolerance = 10 * time.Minute

var shanghai = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("CST", 8*3600)
	}
	return loc
}()

// detailDeadline 是详情页的月-日时分；没有年份，需要结合列表页剩余时间补全
type detailDeadline struct {
	Raw    string
	Month  int
	Day    int
	Hour   int
	Minute int
	Second int
}

// fetchDetail 取一条作业的详情页。data URL 已经是完整地址，参数全都在里面
func (c *Client) fetchDetail(ctx context.Context, item workItem, jar *cookieJar) (string, error) {
	rawURL := item.DetailURL
	if u, err := url.Parse(rawURL); err == nil && !u.IsAbs() {
		base, _ := url.Parse(c.baseURL)
		rawURL = base.ResolveReference(u).String()
	}
	// Android 接口路径会 302 到 H5 详情页，必须跟过去再解析
	resp, err := c.getFollowing(ctx, rawURL, jar)
	if err != nil {
		return "", err
	}
	body := string(resp.Raw)
	if strings.Contains(body, "请重新登录") || strings.Contains(body, "fanyalogin") {
		return "", fmt.Errorf("%w: 作业详情返回登录页", entity.ErrAuthExpired)
	}
	return body, nil
}

// parseDetailDeadline 优先读 aria-label（带秒），退化到 h4
func parseDetailDeadline(body string) *detailDeadline {
	if match := detailAriaPattern.FindStringSubmatch(body); match != nil {
		second := 0
		if match[5] != "" {
			second, _ = strconv.Atoi(match[5])
		}
		return &detailDeadline{
			Raw:    match[0],
			Month:  atoi(match[1]),
			Day:    atoi(match[2]),
			Hour:   atoi(match[3]),
			Minute: atoi(match[4]),
			Second: second,
		}
	}
	if match := detailH4Pattern.FindStringSubmatch(body); match != nil {
		return &detailDeadline{
			Raw:    match[0],
			Month:  atoi(match[1]),
			Day:    atoi(match[2]),
			Hour:   atoi(match[3]),
			Minute: atoi(match[4]),
		}
	}
	return nil
}

// resolveDeadline 按接口文档的年份补全规则算出绝对时刻。
//
// 进行中：now + 剩余时间（唯一带绝对信息的源），再用详情页月-日交叉校验；
// 已截止：详情页月-日，月-日 > 今天归去年，否则归今年
func resolveDeadline(item workItem, detail *detailDeadline, now time.Time) (*time.Time, entity.DeadlineSource, string, []entity.FetchIssue) {
	now = now.In(shanghai)

	if item.HasRemaining {
		derived := now.Add(item.Remaining)
		raw := item.RemainingRaw
		var warnings []entity.FetchIssue
		utc := derived.UTC()
		if detail != nil {
			raw = detail.Raw
			d := derived.In(shanghai)
			// 详情页的月日时分秒是服务端写死的截止时刻，年份从剩余时间推算；
			// 列表的剩余时间只到分钟且以页面渲染时刻为基准，差几十秒是正常的，
			// 落在容差内就用详情页的精确值
			exact := time.Date(d.Year(), time.Month(detail.Month), detail.Day,
				detail.Hour, detail.Minute, detail.Second, 0, shanghai)
			if diff := exact.Sub(d); diff <= deadlineTolerance && diff >= -deadlineTolerance {
				utc = exact.UTC()
			} else {
				warnings = append(warnings, entity.FetchIssue{
					Scope: entity.ScopeAssignment,
					Ref:   item.CourseID,
					Kind:  entity.IssueBusiness,
					Message: fmt.Sprintf("作业 %q 的剩余时间推算为 %s，详情页为 %s，以剩余时间为准",
						item.Title, d.Format("2006-01-02 15:04"), detail.Raw),
				})
			}
		}
		return &utc, entity.DeadlineDerivedRemaining, raw, warnings
	}

	if detail == nil {
		return nil, entity.DeadlineUnknown, "", []entity.FetchIssue{{
			Scope:   entity.ScopeAssignment,
			Ref:     item.CourseID,
			Kind:    entity.IssueParse,
			Message: fmt.Sprintf("作业 %q 已截止但没有拿到详情页截止时间", item.Title),
		}}
	}

	year := now.Year()
	if detail.Month > int(now.Month()) || (detail.Month == int(now.Month()) && detail.Day > now.Day()) {
		year--
	}
	deadline := time.Date(year, time.Month(detail.Month), detail.Day,
		detail.Hour, detail.Minute, detail.Second, 0, shanghai).UTC()
	return &deadline, entity.DeadlineInferredYear, detail.Raw, nil
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
