package pigai

// 以下结构只包含白名单字段。
//
// 详情响应里的 data.re_user（老师的密码摘要、邮箱、QQ 凭据、IP）、
// data._u（当前用户的邮箱、手机号、IP）与 data._CSRF 一律不建模：
// 解析完立刻丢弃，不进结构体、不进日志、不进错误信息。

// pendingData 是未完成列表的 data。班级表只在这个响应里出现，
// 所以必须先拉它再拉已完成列表
type pendingData struct {
	RequestInfo []requestInfo `json:"requestInfo"`
	StduntClass []stduntClass `json:"stduntClass"`
	NowTime     int64         `json:"now_time"`
}

// requestInfo 是未完成作业列表的一条
type requestInfo struct {
	RequestID    string `json:"request_id"`
	EssayTitle   string `json:"essay_title"`
	TeacherName  string `json:"teacher_name"`
	CreateTime   string `json:"create_time"`
	ValidEndTime string `json:"valid_end_time"`
}

// stduntClass 是学生所在班级。tClass 也用这个结构，只是不带 isdel
type stduntClass struct {
	ClassID string `json:"class_id"`
	Class   string `json:"class"`
	IsDel   string `json:"isdel"`
}

// completedData 是已完成作文列表的 data。
// 分页字段的语义未证实，这里只用于判断是否只拿到一页
type completedData struct {
	EssayList []essayItem `json:"essayList"`
	AllNum    int         `json:"allNum"`
	Cnt       int         `json:"cnt"`
}

// essayItem 是已完成列表的一条。title 是「第N号 作文」的通用标题，
// 不是作业标题；真实标题要调详情
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
	NowTime int64         `json:"now_time"`
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
