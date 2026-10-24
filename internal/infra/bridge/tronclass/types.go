package tronclass

import "encoding/json"

// semesterList 对应 GET /api/my-semesters 的响应
type semesterList struct {
	Semesters []semester `json:"semesters"`
}

type semester struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	IsActive bool   `json:"is_active"`
}

// semesterRef 是课程对象里的学期引用，id 与学年 id 不是同一实体
type semesterRef struct {
	ID   int64  `json:"id"`
	Code string `json:"code"`
	Name string `json:"name"`
}

// coursePage 对应 POST /api/my-courses 带 page 时的扁平分页对象
type coursePage struct {
	Courses  []course `json:"courses"`
	Page     int      `json:"page"`
	PageSize int      `json:"page_size"`
	Pages    int      `json:"pages"`
	Total    int      `json:"total"`
}

type course struct {
	ID       int64        `json:"id"`
	Name     string       `json:"name"`
	Semester *semesterRef `json:"semester"`
}

type courseQuery struct {
	Page       int              `json:"page"`
	PageSize   int              `json:"page_size"`
	Fields     string           `json:"fields"`
	Conditions courseConditions `json:"conditions"`
}

type courseConditions struct {
	SemesterID int64 `json:"semester_id"`
}

// homeworkPage 对应作业列表的扁平 7 键对象，没有 data 包装
type homeworkPage struct {
	Page       int                `json:"page"`
	PageSize   int                `json:"page_size"`
	Pages      int                `json:"pages"`
	Total      int                `json:"total"`
	Activities []homeworkActivity `json:"homework_activities"`
}

type homeworkActivity struct {
	ID        int64         `json:"id"`
	Title     string        `json:"title"`
	Type      string        `json:"type"`
	Deadline  string        `json:"deadline"`
	Submitted submittedFlag `json:"submitted"`
}

// submittedFlag 区分「明确给了布尔值」与「缺失或类型错误」
//
// 后者必须落到 unknown 并写 warning，绝不能默认成未提交
type submittedFlag struct {
	Set   bool
	Value bool
}

func (f *submittedFlag) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return nil
	}
	var value bool
	if err := json.Unmarshal(data, &value); err != nil {
		// 类型异常按缺失处理，由调用方写 warning
		return nil
	}
	f.Set = true
	f.Value = value
	return nil
}
