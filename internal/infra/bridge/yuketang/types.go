package yuketang

import (
	"encoding/json"
	"fmt"
	"strings"
)

// 所有 id 与时间戳都用 json.Number / num 承接，避免经 float64 丢精度
// UserID 文档明确是数字；course_id / classroom_id / activity id 同理

type preInfoResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		QRContent string `json:"qrContent"`
		QRImage   string `json:"qrImage"`
		Token     string `json:"token"`
	} `json:"data"`
}

type pollResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		UserID json.Number `json:"UserID"`
		Auth   string      `json:"Auth"`
	} `json:"data"`
}

// errorResp 用于读取 400 时的业务 code
type errorResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

// 课程列表实测不返回 success 字段，只有 data；指针用于区分「缺失」与「显式 false」
type courseListResp struct {
	Success *bool        `json:"success"`
	Op      string       `json:"op"`
	Msg     string       `json:"msg"`
	Data    []courseItem `json:"data"`
}

type courseItem struct {
	CourseID      json.Number `json:"course_id"`
	ClassroomID   json.Number `json:"classroom_id"`
	CourseName    string      `json:"course_name"`
	ClassroomName string      `json:"classroom_name"`
	UniversityID  json.Number `json:"university_id"`
	Role          int         `json:"role"`
}

type classroomLogsResp struct {
	Success *bool  `json:"success"`
	Msg     string `json:"msg"`
	Op      string `json:"op"`
	Data    struct {
		NowTime    num          `json:"now_time"`
		Classroom  classroomRef `json:"classroom"`
		Activities [][]activity `json:"activities"`
	} `json:"data"`
}

// classroomRef 只需要待办列表要用的 sku 与 uv
type classroomRef struct {
	SkuID num `json:"sku_id"`
	UVId  num `json:"uv_id"`
}

// activity 的 ID / 时间也用 num；content 只有部分 type 才有
type activity struct {
	ID         num              `json:"id"`
	Type       int              `json:"type"`
	Title      string           `json:"title"`
	CreateTime num              `json:"create_time"`
	Content    *activityContent `json:"content"`
}

type activityContent struct {
	ScoreD num `json:"score_d"`
	LeafID num `json:"leaf_id"`
	SkuID  num `json:"sku_id"`
}

// todoListResp 是课堂待办列表，记录「未完成」的叶子
//
// 2026-10-25 用户账号实测：count.total 与课程页「未完成 (N)」一致，
// result 里同时有习题（type=6）、视频（0）、课件（7）与授课（8）
type todoListResp struct {
	Success *bool  `json:"success"`
	Msg     string `json:"msg"`
	Data    struct {
		Count struct {
			Total num `json:"total"`
		} `json:"count"`
		Result []todoItem `json:"result"`
	} `json:"data"`
}

type todoItem struct {
	LeafID num `json:"leaf_id"`
	Type   int `json:"type"`
}

// num 承接作业列表里的数字字段
//
// 平台会把数字字段写成空字符串或带引号的数字（2026-10-25 用户账号实测：
// score_d 为 "" 让整段响应解码失败，整门课的作业全丢）。空值统一解成零值，
// 交给逐条校验降级成 warning，不要用 json.Number 让整门课一起失败
type num string

func (n *num) UnmarshalJSON(data []byte) error {
	raw := strings.TrimSpace(string(data))
	if raw == "null" {
		*n = ""
		return nil
	}
	if strings.HasPrefix(raw, `"`) {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		raw = strings.TrimSpace(s)
		if raw == "" {
			*n = ""
			return nil
		}
	}
	var check json.Number
	if err := json.Unmarshal([]byte(raw), &check); err != nil {
		return fmt.Errorf("数字字段不是合法数字: %q", raw)
	}
	*n = num(check.String())
	return nil
}

func (n num) String() string { return string(n) }
