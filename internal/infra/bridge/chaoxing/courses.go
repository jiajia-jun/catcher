package chaoxing

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"catcher/internal/domain/entity"
)

const courseListPath = "/mycourse/backclazzdata?view=json&rss=1"

// courseEntry 的 id 用 json.Number 保留原始字面量。
// 一旦过 float64，214986481 会变成 "2.14986481e+08"，课程名补全静默失效
type courseEntry struct {
	ID   json.Number `json:"id"`
	Name string      `json:"name"`
	// ClassID 与 CPI 挂在 channel 的 content 上，不在 course.data 里，
	// 解析后由 fetchCourseList 补到每条课程上，用于拼课程主页地址
	ClassID string `json:"-"`
	CPI     string `json:"-"`
}

type courseListResp struct {
	Result      json.Number `json:"result"`
	Msg         string      `json:"msg"`
	ChannelList []struct {
		Content struct {
			ID     json.Number `json:"id"`
			CPI    json.Number `json:"cpi"`
			Course struct {
				Data []courseEntry `json:"data"`
			} `json:"course"`
		} `json:"content"`
	} `json:"channelList"`
}

// fetchCourseList 读取课程列表。它同时承担登录态判定：
// result=1 已登录，result=0 会话失效；绝不能拿 HTTP 200 或课程数当判据
func (c *Client) fetchCourseList(ctx context.Context, jar *cookieJar) ([]courseEntry, error) {
	resp, err := c.get(ctx, c.baseURL+courseListPath, jar)
	if err != nil {
		return nil, err
	}

	trimmed := strings.TrimSpace(string(resp.Raw))
	if strings.HasPrefix(trimmed, "<") {
		// 会话失效的真实形态之一：200 + 登录页 HTML
		return nil, fmt.Errorf("%w: 课程列表返回登录页", entity.ErrAuthExpired)
	}

	var out courseListResp
	if err := json.Unmarshal(resp.Raw, &out); err != nil {
		return nil, fmt.Errorf("%w: 课程列表不是 JSON: %v", entity.ErrPlatformChanged, err)
	}
	switch out.Result.String() {
	case "1":
	case "0":
		msg := strings.TrimSpace(out.Msg)
		if msg == "" {
			msg = "请重新登录"
		}
		return nil, fmt.Errorf("%w: %s", entity.ErrAuthExpired, msg)
	case "":
		return nil, fmt.Errorf("%w: 课程列表缺少 result 字段", entity.ErrPlatformChanged)
	default:
		return nil, fmt.Errorf("%w: 课程列表 result=%s", entity.ErrPlatformChanged, out.Result.String())
	}

	var entries []courseEntry
	for _, channel := range out.ChannelList {
		// 一个 channel 就是一个班，classId 与 cpi 对它下面的所有课程相同
		classID := numberLiteral(channel.Content.ID)
		cpi := numberLiteral(channel.Content.CPI)
		for _, entry := range channel.Content.Course.Data {
			entry.ClassID = classID
			entry.CPI = cpi
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

// numberLiteral 取 JSON 数字的原始字面量，顺带把服务端的「空」统一成空串。
// 这些 id 都不能过 float64
func numberLiteral(n json.Number) string {
	raw := strings.TrimSpace(n.String())
	if raw == "0" {
		return ""
	}
	return raw
}

// courseNameMap 建立 courseId 字面量 → 完整课程名。
// 列表页的课程名被截断，这里补全
func courseNameMap(entries []courseEntry) map[string]string {
	out := make(map[string]string, len(entries))
	for _, entry := range entries {
		id := entry.ID.String()
		if id == "" || strings.TrimSpace(entry.Name) == "" {
			continue
		}
		out[id] = entry.Name
	}
	return out
}

// toCourses 把课程列表转成领域课程；学习通没有学期概念，semester_key 留空
func toCourses(entries []courseEntry) []entity.Course {
	seen := make(map[string]struct{}, len(entries))
	out := make([]entity.Course, 0, len(entries))
	for _, entry := range entries {
		id := entry.ID.String()
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, entity.Course{
			Platform:   entity.PlatformChaoxing,
			ExternalID: id,
			Name:       strings.TrimSpace(entry.Name),
			Active:     true,
		})
	}
	return out
}
