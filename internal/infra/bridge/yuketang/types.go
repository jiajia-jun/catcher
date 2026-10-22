package yuketang

import "encoding/json"

// 所有 id 与时间戳都用 json.Number 承接，避免经 float64 丢精度
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

type courseListResp struct {
	Success bool         `json:"success"`
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
	Success bool   `json:"success"`
	Msg     string `json:"msg"`
	Op      string `json:"op"`
	Data    struct {
		NowTime    json.Number  `json:"now_time"`
		Activities [][]activity `json:"activities"`
	} `json:"data"`
}

// activity 的 ID / 时间也用 Number；content 只有部分 type 才有
type activity struct {
	ID         json.Number      `json:"id"`
	Type       int              `json:"type"`
	Title      string           `json:"title"`
	CreateTime json.Number      `json:"create_time"`
	Content    *activityContent `json:"content"`
}

type activityContent struct {
	ScoreD json.Number `json:"score_d"`
	LeafID json.Number `json:"leaf_id"`
}

// webLoginResp 是 ③ 成功时的 JSON；失败时返回 HTML，解析失败不算异常
type webLoginResp struct {
	Success  bool        `json:"success"`
	UserID   json.Number `json:"user_id"`
	UserRole int         `json:"user_role"`
}
