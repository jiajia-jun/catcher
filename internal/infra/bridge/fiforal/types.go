package fiforal

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
)

// flexInt 同时接受数字与数字字符串。
// complete / isDelay 实测是 int，但平台字段类型不稳定；
// 类型漂移不能让整页解析失败
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

// flexString 同时接受字符串与数字，统一成字符串。
// taskModelType 实测是 int，生成详情链接时要转字符串
type flexString string

func (f *flexString) UnmarshalJSON(raw []byte) error {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		*f = ""
		return nil
	}
	if strings.HasPrefix(s, `"`) {
		var v string
		if err := json.Unmarshal(raw, &v); err == nil {
			*f = flexString(v)
			return nil
		}
	}
	*f = flexString(strings.Trim(s, `"`))
	return nil
}

// loginURLEnvelope 是 getWebLoginUrl 的响应。status 是数字 1，不是字符串
type loginURLEnvelope struct {
	Status int    `json:"status"`
	Msg    string `json:"msg"`
	Data   struct {
		WeixinURL string `json:"weixinUrl"`
	} `json:"data"`
}

// taskListEnvelope 是任务列表的信封
type taskListEnvelope struct {
	Status int    `json:"status"`
	Msg    string `json:"msg"`
	Data   struct {
		TtiList []taskItem `json:"ttiList"`
		SysTime string     `json:"sysTime"`
	} `json:"data"`
}

// taskItem 只保留抓取需要的白名单字段
type taskItem struct {
	ID            string     `json:"id"`
	TaskID        string     `json:"taskId"`
	TaskName      string     `json:"taskName"`
	TaskInfo      string     `json:"taskInfo"`
	TeaName       string     `json:"teaName"`
	StartTime     string     `json:"startTime"`
	EndTime       string     `json:"endTime"`
	DelayTime     string     `json:"delayTime"`
	IsDelay       *flexInt   `json:"isDelay"`
	Complete      *flexInt   `json:"complete"`
	TaskModelType flexString `json:"taskModelType"`
	Score         flexInt    `json:"score"`
	// Status 是生命周期（"1" 进行中 / "2" 已结束），不是提交状态
	Status flexString `json:"status"`
}

func (t taskItem) taskID() string {
	if id := strings.TrimSpace(t.TaskID); id != "" {
		return id
	}
	return strings.TrimSpace(t.ID)
}

func (t taskItem) isDelay() int64 {
	if t.IsDelay == nil {
		return 0
	}
	return int64(*t.IsDelay)
}

func (t taskItem) complete() *int64 {
	if t.Complete == nil {
		return nil
	}
	v := int64(*t.Complete)
	return &v
}
