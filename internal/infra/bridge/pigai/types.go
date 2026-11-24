package pigai

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
)

// flexInt 同时接受数字与数字字符串
//
// 平台字段类型不稳定，cnt 与 now_time 都可能以字符串出现
// 解析失败的值按 0 处理，与平台未提供时间等价
type flexInt int64

func (f *flexInt) UnmarshalJSON(raw []byte) error {
	s := strings.Trim(string(bytes.TrimSpace(raw)), `"`)
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		*f = 0
		return nil
	}
	*f = flexInt(v)
	return nil
}

// 以下结构只包含白名单字段
//
// 详情响应里的 data.re_user（老师的密码摘要、邮箱、QQ 凭据、IP）、
// data._u（当前用户的邮箱、手机号、IP）与 data._CSRF 不建模
// 这些字段在解析阶段丢弃，结构体、日志与错误信息中只保留白名单内容

// pendingData 是未完成列表的 data
// 班级表只在这个响应里出现，抓取顺序为先未完成、后已完成
type pendingData struct {
	RequestInfo []requestInfo `json:"requestInfo"`
	StduntClass []stduntClass `json:"stduntClass"`
	NowTime     flexInt       `json:"now_time"`
}

// requestInfo 是未完成作业列表的一条
type requestInfo struct {
	RequestID    string `json:"request_id"`
	EssayTitle   string `json:"essay_title"`
	TeacherName  string `json:"teacher_name"`
	CreateTime   string `json:"create_time"`
	ValidEndTime string `json:"valid_end_time"`
}

// stduntClass 是学生所在班级，tClass 使用同一结构，isdel 字段仅在班级表中出现
type stduntClass struct {
	ClassID string `json:"class_id"`
	Class   string `json:"class"`
	IsDel   string `json:"isdel"`
}

// completedData 是已完成作文列表的 data
// 分页字段的语义未证实，类型也不稳定，这里只保留原始值，不参与解析
type completedData struct {
	EssayList []essayItem     `json:"essayList"`
	AllNum    json.RawMessage `json:"allNum"`
	Cnt       json.RawMessage `json:"cnt"`
}

// essayItem 是已完成列表的一条
// title 是「第N号 作文」的通用标题，真实标题来自详情
type essayItem struct {
	EssayID      string `json:"essay_id"`
	RequestID    string `json:"request_id"`
	Title        string `json:"title"`
	Ctime        string `json:"ctime"`
	Score        string `json:"score"`
	StuClass     string `json:"stu_class"`
	ValidEndTime string `json:"valid_end_time"`
	CreateTime   string `json:"create_time"`
	Version      string `json:"version"`
}

// detailData 是作业详情的 data，未完成与已完成共用
type detailData struct {
	Request detailRequest `json:"request"`
	TClass  []stduntClass `json:"tClass"`
	Essay   essayDetail   `json:"essay"`
	NowTime flexInt       `json:"now_time"`
}

type detailRequest struct {
	RequestID    string `json:"request_id"`
	EssayTitle   string `json:"essay_title"`
	EssayTopic   string `json:"essay_topic"`
	TeacherName  string `json:"teacher_name"`
	CreateTime   string `json:"create_time"`
	ValidEndTime string `json:"valid_end_time"`
}

type essayDetail struct {
	EssayID   string `json:"essay_id"`
	RequestID string `json:"request_id"`
	Title     string `json:"title"`
	Tm        string `json:"tm"`
	Score     string `json:"score"`
	StuClass  string `json:"stu_class"`
}
