package pigai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"catcher/internal/domain/entity"
	"catcher/internal/infra/sender"
)

// 东八区 2026-11-05 23:59:00，属于 2026 秋季学期
const currentTermDeadline = "1793894340"

// 2023 年的截止时间，不属于本学期
const oldTermDeadline = "1699000000"

type fetchFake struct {
	mu sync.Mutex

	pending      string
	pendingErr   int
	completed    string
	completedErr int
	// details 按 "write:R1" / "view:E1" 取详情响应
	details map[string]string
	// requests 记录收到的接口调用，用于断言没有多调详情
	requests []string
}

func (f *fetchFake) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		f.mu.Lock()
		defer f.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch {
		case q.Get("a") == "waitComplete":
			f.requests = append(f.requests, "pending")
			if f.pendingErr != 0 {
				writeJSON(w, envelopeJSON(f.pendingErr))
				return
			}
			_, _ = w.Write([]byte(f.pending))
		case q.Get("c") == "write":
			f.requests = append(f.requests, "completed")
			if f.completedErr != 0 {
				writeJSON(w, envelopeJSON(f.completedErr))
				return
			}
			_, _ = w.Write([]byte(f.completed))
		case q.Get("c") == "v2" && q.Get("a") == "write":
			key := "write:" + q.Get("rid")
			f.requests = append(f.requests, key)
			_, _ = w.Write([]byte(f.details[key]))
		case q.Get("c") == "v2" && q.Get("a") == "view":
			key := "view:" + q.Get("eid")
			f.requests = append(f.requests, key)
			_, _ = w.Write([]byte(f.details[key]))
		default:
			http.NotFound(w, r)
		}
	})
}

func writeJSON(w http.ResponseWriter, body string) {
	_, _ = w.Write([]byte(body))
}

func envelopeJSON(code int) string {
	return `{"error":` + itoa(code) + `,"error_des":"失败"}`
}

func (f *fetchFake) calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.requests...)
}

func (f *fetchFake) contains(call string) bool {
	for _, c := range f.calls() {
		if c == call {
			return true
		}
	}
	return false
}

func newFetchClient(t *testing.T, f *fetchFake) *Client {
	t.Helper()
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)
	return New(Options{
		BaseURL:         srv.URL,
		WeixinBaseURL:   srv.URL,
		LongPollBaseURL: srv.URL,
		Sender:          sender.New(sender.Config{}),
		Now:             func() time.Time { return time.Date(2026, 10, 29, 12, 0, 0, 0, shanghai) },
	})
}

func testSession(t *testing.T) entity.Session {
	t.Helper()
	raw, err := json.Marshal(entity.PigaiSecret{
		Cookies: []entity.Cookie{{Name: "PHPSESSID", Value: "x"}, {Name: "_JUKU_USER", Value: "y"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return entity.Session{Platform: entity.PlatformPigai, Secret: raw}
}

func fetch(t *testing.T, f *fetchFake) *entity.FetchResult {
	t.Helper()
	c := newFetchClient(t, f)
	result, err := c.Fetch(context.Background(), entity.Account{}, testSession(t), entity.FetchOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func assignmentByKey(t *testing.T, result *entity.FetchResult, requestID string) entity.Assignment {
	t.Helper()
	key := entity.PigaiDedupKey(requestID)
	for _, a := range result.Assignments {
		if a.DedupKey == key {
			return a
		}
	}
	t.Fatalf("没有找到作业 %s: %#v", requestID, result.Assignments)
	return entity.Assignment{}
}

func TestFetchMergesListsAndMapsFields(t *testing.T) {
	f := &fetchFake{
		pending: `{"error":0,"data":{
			"requestInfo":[
				{"request_id":"R1","essay_title":"列表里的旧标题","valid_end_time":"` + currentTermDeadline + `","create_time":"1792645751"},
				{"request_id":"R3","essay_title":"没详情的作业","valid_end_time":"0"}
			],
			"stduntClass":[
				{"class_id":"C1","class":"2026秋 示例课程","isdel":"0"},
				{"class_id":"C9","class":"已删除班级","isdel":"1"}
			]
		}}`,
		completed: `{"error":0,"data":{"essayList":[
			{"essay_id":"E2","request_id":"R2","title":"第R2号 作文","stu_class":"2026秋 示例课程",
			 "valid_end_time":"` + currentTermDeadline + `","ctime":"1792587182","version":"1"},
			{"essay_id":"E1","request_id":"R1","title":"第R1号 作文","stu_class":"2026秋 示例课程",
			 "valid_end_time":"` + currentTermDeadline + `","ctime":"1792587182","version":"1"}
		]}}`,
		details: map[string]string{
			"write:R1": `{"error":0,"data":{
				"request":{"request_id":"R1","essay_title":"第一次作业","essay_topic":"<p>请写一篇&nbsp;短文</p>",
				           "valid_end_time":"` + currentTermDeadline + `","teacher_name":"示例老师"},
				"tClass":[{"class_id":"C1","class":"2026秋 示例课程"}],
				"now_time":1793199195}}`,
			"view:E2": `{"error":0,"data":{
				"request":{"request_id":"R2","essay_title":"第二次作业","essay_topic":"<div>题目</div>"},
				"essay":{"essay_id":"E2","request_id":"R2","stu_class":"2026秋 示例课程"}}}`,
		},
	}

	result := fetch(t, f)

	if result.Complete || result.CourseListComplete {
		t.Fatal("分页未验证，Complete 与 CourseListComplete 必须为 false")
	}
	if len(result.Assignments) != 3 {
		t.Fatalf("作业 = %d 条, want 3", len(result.Assignments))
	}

	// 两边都有的作业按未提交处理，标题取详情里的真实标题
	r1 := assignmentByKey(t, result, "R1")
	if r1.Status != entity.StatusUnsubmitted {
		t.Fatalf("R1 状态 = %s, want unsubmitted", r1.Status)
	}
	if r1.Title != "第一次作业" {
		t.Fatalf("R1 标题 = %q", r1.Title)
	}
	if r1.CourseExternalID != "C1" || r1.CourseName != "2026秋 示例课程" {
		t.Fatalf("R1 课程 = %s/%s", r1.CourseExternalID, r1.CourseName)
	}
	if r1.Description != "请写一篇 短文" {
		t.Fatalf("R1 题干 = %q", r1.Description)
	}
	if r1.Deadline == nil || r1.DeadlineSource != entity.DeadlinePlatformExact {
		t.Fatalf("R1 截止 = %v / %s", r1.Deadline, r1.DeadlineSource)
	}

	// 已完成作业的标题来自详情，不是列表里的「第N号 作文」
	r2 := assignmentByKey(t, result, "R2")
	if r2.Status != entity.StatusSubmitted || r2.Title != "第二次作业" {
		t.Fatalf("R2 = %s / %q", r2.Status, r2.Title)
	}
	if !strings.Contains(r2.DetailURL, "a=view") || !strings.Contains(r2.DetailURL, "eid=E2") {
		t.Fatalf("R2 详情链接 = %q", r2.DetailURL)
	}

	// 详情失败时用列表数据兜底，截止时间 "0" 按未知处理
	r3 := assignmentByKey(t, result, "R3")
	if r3.Status != entity.StatusUnsubmitted || r3.Title != "没详情的作业" {
		t.Fatalf("R3 = %s / %q", r3.Status, r3.Title)
	}
	if r3.Deadline != nil || r3.DeadlineSource != entity.DeadlineUnknown {
		t.Fatalf("R3 截止 = %v / %s", r3.Deadline, r3.DeadlineSource)
	}
	if r3.CourseExternalID != unknownClassID {
		t.Fatalf("R3 课程 = %s, want 兜底课程", r3.CourseExternalID)
	}

	// isdel=1 的班级不建课程
	for _, course := range result.Courses {
		if course.ExternalID == "C9" {
			t.Fatal("已删除的班级不应建课")
		}
	}
	if result.ServerNow == nil {
		t.Fatal("应记录平台当前时间")
	}
}

func TestFetchMatchesClassByNameAndWarns(t *testing.T) {
	f := &fetchFake{
		pending:   `{"error":0,"data":{"requestInfo":[],"stduntClass":[{"class_id":"C1","class":"2026秋 示例课程"}]}}`,
		completed: `{"error":0,"data":{"essayList":[
			{"essay_id":"E2","request_id":"R2","title":"第R2号 作文","stu_class":"2026秋 另一个课程",
			 "valid_end_time":"` + currentTermDeadline + `","ctime":"1792587182","version":"1"}
		]}}`,
		details: map[string]string{
			"view:E2": `{"error":0,"data":{"request":{"essay_title":"第二次作业"},"essay":{"stu_class":"2026秋 另一个课程"}}}`,
		},
	}

	result := fetch(t, f)

	r2 := assignmentByKey(t, result, "R2")
	if r2.CourseExternalID != classKeyPrefix+"2026秋 另一个课程" {
		t.Fatalf("课程 = %q, want 按名字造课", r2.CourseExternalID)
	}
	if r2.CourseName != "2026秋 另一个课程" {
		t.Fatalf("课程名 = %q", r2.CourseName)
	}
	assertWarning(t, result, entity.ScopeCourse)
}

func TestFetchSkipsDetailForOldSemester(t *testing.T) {
	f := &fetchFake{
		pending:   `{"error":0,"data":{"requestInfo":[],"stduntClass":[{"class_id":"C1","class":"2026秋 示例课程"}]}}`,
		completed: `{"error":0,"data":{"essayList":[
			{"essay_id":"E9","request_id":"R9","title":"第R9号 作文","stu_class":"2026秋 示例课程",
			 "valid_end_time":"` + oldTermDeadline + `","ctime":"1699000000","version":"1"}
		]}}`,
	}

	result := fetch(t, f)

	if f.contains("view:E9") {
		t.Fatal("往期作文不应调详情")
	}
	r9 := assignmentByKey(t, result, "R9")
	if r9.Title != "第R9号 作文" {
		t.Fatalf("往期作业应保留列表通用标题，实际 %q", r9.Title)
	}
	if r9.CourseExternalID != "C1" {
		t.Fatalf("课程 = %q", r9.CourseExternalID)
	}
}

func TestFetchDetailFailureKeepsAssignment(t *testing.T) {
	f := &fetchFake{
		pending: `{"error":0,"data":{"requestInfo":[
			{"request_id":"R1","essay_title":"第一次作业","valid_end_time":"` + currentTermDeadline + `"}
		],"stduntClass":[{"class_id":"C1","class":"2026秋 示例课程"}]}}`,
		completed: `{"error":0,"data":{"essayList":[]}}`,
		details: map[string]string{
			"write:R1": envelopeJSON(9),
		},
	}

	result := fetch(t, f)

	if len(result.Assignments) != 1 {
		t.Fatalf("作业 = %d 条", len(result.Assignments))
	}
	r1 := result.Assignments[0]
	if r1.CourseExternalID != unknownClassID {
		t.Fatalf("课程 = %q, want 兜底课程", r1.CourseExternalID)
	}
	if r1.Deadline == nil {
		t.Fatal("详情失败时应保留列表里的截止时间")
	}
	assertWarning(t, result, entity.ScopeAssignment)
}

// 未完成列表是主数据源，它失败就整次失败，不能返回空列表
func TestFetchPendingFailureAborts(t *testing.T) {
	f := &fetchFake{pendingErr: 302}
	c := newFetchClient(t, f)

	_, err := c.Fetch(context.Background(), entity.Account{}, testSession(t), entity.FetchOptions{}, nil)
	if !errors.Is(err, entity.ErrAuthExpired) {
		t.Fatalf("= %v, want ErrAuthExpired", err)
	}
	if f.contains("completed") {
		t.Fatal("未完成列表失败后不应继续请求已完成列表")
	}
}

func TestFetchCompletedFailureIsWarning(t *testing.T) {
	f := &fetchFake{
		pending: `{"error":0,"data":{"requestInfo":[
			{"request_id":"R1","essay_title":"第一次作业","valid_end_time":"` + currentTermDeadline + `"}
		],"stduntClass":[{"class_id":"C1","class":"2026秋 示例课程"}]}}`,
		completedErr: 9,
		details: map[string]string{
			"write:R1": `{"error":0,"data":{"request":{"essay_title":"第一次作业"},"tClass":[{"class_id":"C1","class":"2026秋 示例课程"}]}}`,
		},
	}

	result := fetch(t, f)

	if len(result.Assignments) != 1 {
		t.Fatalf("作业 = %d 条, want 1", len(result.Assignments))
	}
	assertWarning(t, result, entity.ScopePlatform)
}

func TestMergeItemsPrefersLatestVersion(t *testing.T) {
	c := New(Options{BaseURL: "https://www.pigai.org"})
	items := c.mergeItems(nil, []essayItem{
		{EssayID: "E4", RequestID: "R1", Title: "旧版", Ctime: "100", Version: "1"},
		{EssayID: "E5", RequestID: "R1", Title: "新版", Ctime: "200", Version: "2"},
		{EssayID: "E6", RequestID: "R1", Title: "同时间的低版本", Ctime: "200", Version: "1"},
	})
	if len(items) != 1 {
		t.Fatalf("items = %d", len(items))
	}
	if items[0].essayID != "E5" || items[0].title != "新版" {
		t.Fatalf("应取最新一版: %#v", items[0])
	}
}

func TestStripHTML(t *testing.T) {
	cases := map[string]string{
		"<p>请写一篇&nbsp;短文</p>": "请写一篇 短文",
		"<div><b>加粗</b><br/>换行</div>": "加粗 换行",
		"": "",
	}
	for in, want := range cases {
		if got := stripHTML(in); got != want {
			t.Fatalf("stripHTML(%q) = %q, want %q", in, got, want)
		}
	}
}

// 分页字段的类型在不同账号下不一致：文档样本里 cnt 是数字，
// 真机返回的是字符串，两种都不能让整份已完成列表解析失败
func TestCompletedDataAcceptsMixedPagingTypes(t *testing.T) {
	cases := []string{
		`{"essayList":[{"request_id":"R1"}],"allNum":1,"cnt":5}`,
		`{"essayList":[{"request_id":"R1"}],"allNum":"1","cnt":"5"}`,
	}
	for _, body := range cases {
		var data completedData
		env := &envelope{Data: json.RawMessage(body)}
		if err := decodeEnvelopeData(env, &data); err != nil {
			t.Fatalf("%s → %v", body, err)
		}
		if len(data.EssayList) != 1 || data.EssayList[0].RequestID != "R1" {
			t.Fatalf("%s → %#v", body, data.EssayList)
		}
	}
}

func assertWarning(t *testing.T, result *entity.FetchResult, scope entity.IssueScope) {
	t.Helper()
	for _, w := range result.Warnings {
		if w.Scope == scope {
			return
		}
	}
	t.Fatalf("缺少 %s 范围的告警: %#v", scope, result.Warnings)
}
