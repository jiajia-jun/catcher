package fiforal

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
)

// flexInt 同时接受数字与数字字符串，并记录是否解析成功。
// complete / isDelay 实测是 int，但平台字段类型不稳定；
// 类型漂移不能让整页解析失败，也不能把认不出的值当成 0
type flexInt struct {
	value int64
	ok    bool
}

func (f *flexInt) UnmarshalJSON(raw []byte) error {
	s := strings.Trim(string(bytes.TrimSpace(raw)), `"`)
	if s == "" || s == "null" {
		*f = flexInt{}
		return nil
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		*f = flexInt{}
		return nil
	}
	*f = flexInt{value: v, ok: true}
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

// isDelay 返回延期标记；缺失或无法解析时 ok 为 false
func (t taskItem) isDelay() (int64, bool) {
	if t.IsDelay == nil || !t.IsDelay.ok {
		return 0, false
	}
	return t.IsDelay.value, true
}

// complete 返回提交状态；缺失、null 或无法解析时 ok 为 false，
// 调用方必须按未知处理，不能当成 0（0 表示明确未提交）
func (t taskItem) complete() (int64, bool) {
	if t.Complete == nil || !t.Complete.ok {
		return 0, false
	}
	return t.Complete.value, true
}
