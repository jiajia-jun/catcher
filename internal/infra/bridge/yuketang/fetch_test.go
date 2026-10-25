package yuketang

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"catcher/internal/domain/entity"
	"catcher/internal/infra/sender"
)

func testSession(t *testing.T, sessionID string) entity.Session {
	t.Helper()
	secret, err := json.Marshal(entity.YuketangSecret{SessionID: sessionID, Auth: "auth", UserID: "42"})
	if err != nil {
		t.Fatal(err)
	}
	return entity.Session{Platform: entity.PlatformYuketang, Secret: secret}
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// 2026-10 实测：课程列表响应只有 data，没有 success 字段
func TestLearningListWithoutSuccessField(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data":[{"course_id":1000001,"classroom_id":20000001,"course_name":"课程","classroom_name":"2026秋-课程-1班"}]}`)
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL, Sender: sender.New(sender.Config{})})
	items, err := c.learningList(context.Background(), "sid")
	if err != nil {
		t.Fatalf("缺少 success 字段不应报错: %v", err)
	}
	if len(items) != 1 || items[0].CourseID.String() != "1000001" {
		t.Fatalf("课程解析异常: %#v", items)
	}
}

func TestLearningListExplicitFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"success":false,"msg":"无权限","data":[]}`)
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL, Sender: sender.New(sender.Config{})})
	_, err := c.learningList(context.Background(), "sid")
	if !errors.Is(err, entity.ErrPlatformChanged) {
		t.Fatalf("显式 success=false 应报平台变化错误: %v", err)
	}
}

func TestClassroomLogsWithoutSuccessField(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data":{"now_time":1792563600,"activities":[]}}`)
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL, Sender: sender.New(sender.Config{})})
	item := courseItem{CourseID: json.Number("1"), ClassroomID: json.Number("2")}
	logs, err := c.classroomLogs(context.Background(), item, "sid")
	if err != nil {
		t.Fatalf("缺少 success 字段不应报错: %v", err)
	}
	if logs.Data.NowTime.String() != "1792563600" {
		t.Fatalf("now_time 解析异常: %s", logs.Data.NowTime.String())
	}
}

func TestFetchFlattensAndFiltersSemester(t *testing.T) {
	courseList := fixture(t, "learning_list.json")
	logs := fixture(t, "classroom_logs.json")

	var gotCookie string
	var logsQuery string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case learningListPath:
			gotCookie = r.Header.Get("Cookie")
			_, _ = w.Write(courseList)
		case classroomLogsPath:
			logsQuery = r.URL.RawQuery
			_, _ = w.Write(logs)
		default:
			http.NotFound(w, r)
		}
	}), 0)

	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	var progresses []entity.Progress
	result, err := client.Fetch(context.Background(), entity.Account{}, testSession(t, "sid-1"),
		entity.FetchOptions{Now: func() time.Time { return now }},
		func(p entity.Progress) { progresses = append(progresses, p) })
	if err != nil {
		t.Fatal(err)
	}

	if gotCookie != "sessionid=sid-1" {
		t.Fatalf("Cookie = %q", gotCookie)
	}
	if !strings.Contains(logsQuery, "course_id=1000001") ||
		!strings.Contains(logsQuery, "classroom_id=20000001") ||
		!strings.Contains(logsQuery, "activity_type=-1") {
		t.Fatalf("作业列表参数 = %q", logsQuery)
	}

	if len(result.Courses) != 1 {
		t.Fatalf("只应保留当前学期课程，实际 %d 门", len(result.Courses))
	}
	course := result.Courses[0]
	if course.ExternalID != "1000001" || course.ClassroomID != "20000001" {
		t.Fatalf("course = %#v", course)
	}
	if course.SemesterKey != "2026秋" {
		t.Fatalf("semester = %q", course.SemesterKey)
	}

	// 嵌套数组摊平后只取 type=20 的两条；9/14 不是作业
	if len(result.Assignments) != 2 {
		t.Fatalf("作业 = %d 条, want 2（未摊平会得到 0）", len(result.Assignments))
	}
	first := result.Assignments[0]
	if first.Title != "第一章练习1" {
		t.Fatalf("title = %q", first.Title)
	}
	if first.Status != entity.StatusUnknown {
		t.Fatalf("雨课堂提交状态必须是 unknown，实际 %s", first.Status)
	}
	if first.Deadline == nil || !first.Deadline.Equal(time.UnixMilli(1793001599000).UTC()) {
		t.Fatalf("deadline = %v", first.Deadline)
	}
	if first.DeadlineSource != entity.DeadlinePlatformExact {
		t.Fatalf("deadline source = %s", first.DeadlineSource)
	}
	if first.CourseName != "某门课程" || first.ClassroomID != "20000001" {
		t.Fatalf("课程关联 = %#v", first)
	}
	if first.DedupKey != entity.YuketangDedupKey("1000001", "20000001", "900000000003") {
		t.Fatalf("dedup = %s", first.DedupKey)
	}
	if !result.Complete {
		t.Fatal("全部课程抓取成功时 Complete 应为 true")
	}
	if result.ServerNow == nil || !result.ServerNow.Equal(time.Unix(1792563600, 0).UTC()) {
		t.Fatalf("server now = %v", result.ServerNow)
	}

	var sawAssignments bool
	for _, p := range progresses {
		if p.Phase == "assignments" && p.Total == 1 {
			sawAssignments = true
		}
	}
	if !sawAssignments {
		t.Fatalf("缺少作业阶段进度: %#v", progresses)
	}
}

func TestFetchAuthExpiredByResponseBody(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// HTTP 200，但响应体表明会话失效
		_, _ = io.WriteString(w, `{"url":"/web?next=/v/course_meta/learning_list/","msg":"页面正在跳转","op":"web_redirect"}`)
	}), 0)

	_, err := client.Fetch(context.Background(), entity.Account{}, testSession(t, "sid"),
		entity.FetchOptions{}, nil)
	if !errors.Is(err, entity.ErrAuthExpired) {
		t.Fatalf("err = %v, want ErrAuthExpired", err)
	}
}

func TestFetchCourseFailureIsPartial(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case learningListPath:
			_, _ = w.Write(fixture(t, "learning_list.json"))
		case classroomLogsPath:
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, "boom")
		}
	}), 0)

	result, err := client.Fetch(context.Background(), entity.Account{}, testSession(t, "sid"),
		entity.FetchOptions{Now: func() time.Time { return time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC) }}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Complete {
		t.Fatal("单门课程失败时 Complete 必须为 false")
	}
	if len(result.Warnings) == 0 || result.Warnings[0].Scope != entity.ScopeCourse {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
	if len(result.Assignments) != 0 {
		t.Fatalf("失败课程不应产生作业: %d", len(result.Assignments))
	}
}

func TestFetchScoreDMustBe13Digits(t *testing.T) {
	logs := `{"success":true,"data":{"now_time":1792563600,"activities":[[
		{"id":1,"type":20,"title":"坏时间作业","create_time":1790000000,
		 "content":{"score_d":1793001599}}]]}}`
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case learningListPath:
			_, _ = w.Write(fixture(t, "learning_list.json"))
		case classroomLogsPath:
			_, _ = io.WriteString(w, logs)
		}
	}), 0)

	result, err := client.Fetch(context.Background(), entity.Account{}, testSession(t, "sid"),
		entity.FetchOptions{Now: func() time.Time { return time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC) }}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Assignments) != 1 {
		t.Fatalf("作业仍应保留，实际 %d", len(result.Assignments))
	}
	if result.Assignments[0].Deadline != nil {
		t.Fatalf("位数不对时不能落 1970: %v", result.Assignments[0].Deadline)
	}
	if result.Assignments[0].DeadlineSource != entity.DeadlineUnknown {
		t.Fatalf("source = %s", result.Assignments[0].DeadlineSource)
	}
	if result.Complete {
		t.Fatal("解析异常时应标记 partial，避免软删除")
	}
}

// 2026-10-25 用户反馈：某门课的数字字段返回 ""，整段响应解码失败，作业一条都没入库
// 空字符串要降级成单条 warning，不能连带整门课失败
func TestFetchToleratesEmptyNumericFields(t *testing.T) {
	logs := `{"success":true,"data":{"now_time":1792563600,"activities":[[
		{"id":11,"type":20,"title":"正常作业","create_time":1790000000,
		 "content":{"score_d":1793001599000,"leaf_id":""}},
		{"id":12,"type":20,"title":"没有截止时间的作业","create_time":1790000000,
		 "content":{"score_d":""}},
		{"id":13,"type":20,"title":"没有创建时间的作业","create_time":"",
		 "content":{"score_d":1793001599000}}]]}}`
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case learningListPath:
			_, _ = w.Write(fixture(t, "learning_list.json"))
		case classroomLogsPath:
			_, _ = io.WriteString(w, logs)
		}
	}), 0)

	result, err := client.Fetch(context.Background(), entity.Account{}, testSession(t, "sid"),
		entity.FetchOptions{Now: func() time.Time { return time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC) }}, nil)
	if err != nil {
		t.Fatalf("空字符串数字字段不应让整门课失败: %v", err)
	}
	if len(result.Courses) != 1 {
		t.Fatalf("课程应照常入库，实际 %d 门", len(result.Courses))
	}
	if len(result.Assignments) != 3 {
		t.Fatalf("三条作业都该保留，实际 %d", len(result.Assignments))
	}
	normal := result.Assignments[0]
	if normal.Deadline == nil || !normal.Deadline.Equal(time.UnixMilli(1793001599000).UTC()) {
		t.Fatalf("leaf_id 为空不应影响截止时间: %v", normal.Deadline)
	}
	empty := result.Assignments[1]
	if empty.Deadline != nil || empty.DeadlineSource != entity.DeadlineUnknown {
		t.Fatalf("score_d 为空时截止时间应为未知: %#v", empty)
	}

	var sawScoreD, sawCreateTime bool
	for _, w := range result.Warnings {
		if strings.Contains(w.Message, "缺少 score_d") {
			sawScoreD = true
		}
		if strings.Contains(w.Message, "缺少 create_time") {
			sawCreateTime = true
		}
	}
	if !sawScoreD || !sawCreateTime {
		t.Fatalf("空字段应各留一条 warning: %#v", result.Warnings)
	}
	// 有解析降级时要标记不完整，避免软删除把正常数据一起清掉
	if result.Complete {
		t.Fatal("解析降级时 Complete 应为 false")
	}
}

func TestNumAcceptsQuotedAndEmptyValues(t *testing.T) {
	var v struct {
		A num `json:"a"`
		B num `json:"b"`
		C num `json:"c"`
	}
	if err := json.Unmarshal([]byte(`{"a":"123","b":"","c":456}`), &v); err != nil {
		t.Fatal(err)
	}
	if v.A.String() != "123" || v.B.String() != "" || v.C.String() != "456" {
		t.Fatalf("a=%q b=%q c=%q", v.A.String(), v.B.String(), v.C.String())
	}

	// 非数字文本仍然要失败，不能把脏数据当 0
	var bad struct {
		A num `json:"a"`
	}
	if err := json.Unmarshal([]byte(`{"a":"abc"}`), &bad); err == nil {
		t.Fatal("非数字字符串应报错")
	}
}

func TestFetchUnknownTypeWithContentWarns(t *testing.T) {
	logs := `{"success":true,"data":{"now_time":1792563600,"activities":[[
		{"id":7,"type":99,"title":"未知活动","create_time":1790000000,
		 "content":{"score_d":1793001599000}}]]}}`
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case learningListPath:
			_, _ = w.Write(fixture(t, "learning_list.json"))
		case classroomLogsPath:
			_, _ = io.WriteString(w, logs)
		}
	}), 0)

	result, err := client.Fetch(context.Background(), entity.Account{}, testSession(t, "sid"),
		entity.FetchOptions{Now: func() time.Time { return time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC) }}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Warnings) == 0 || !strings.Contains(result.Warnings[0].Message, "type=99") {
		t.Fatalf("带 content 的未知类型必须记 warning: %#v", result.Warnings)
	}
}

// 2026-10-23 实测：type=15 是慕课视频学习任务，content.score_d 等于课堂 class_end
// 它不是作业，但保留为一条带期限的任务，且不应再报「未知活动类型」
func TestFetchType15IsKnownTask(t *testing.T) {
	logs := `{"success":true,"data":{"now_time":1792563600,"activities":[[
		{"id":8,"type":15,"title":"衔接课视频学习","create_time":1787889149,
		 "content":{"score_d":1801411199000,"sku_id":12915963,"c_n":1}}]]}}`
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case learningListPath:
			_, _ = w.Write(fixture(t, "learning_list.json"))
		case classroomLogsPath:
			_, _ = io.WriteString(w, logs)
		}
	}), 0)

	result, err := client.Fetch(context.Background(), entity.Account{}, testSession(t, "sid"),
		entity.FetchOptions{Now: func() time.Time { return time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC) }}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Assignments) != 1 {
		t.Fatalf("type=15 应保留为一条任务，实际 %d", len(result.Assignments))
	}
	if result.Assignments[0].Status != entity.StatusUnknown {
		t.Fatalf("状态必须是 unknown，实际 %s", result.Assignments[0].Status)
	}
	for _, w := range result.Warnings {
		if strings.Contains(w.Message, "type=15") {
			t.Fatalf("type=15 是已知类型，不应再告警: %#v", result.Warnings)
		}
	}
	if !result.Complete {
		t.Fatal("已知类型不应把结果标记为 partial")
	}
}

// 2026-10-25 用户账号「计算机组成原理A」实测：新课用 19（习题/练习/作业）
// 与 17（视频），整门课没有 20/15，不能当未知类型刷 warning
func TestFetchType17And19AreKnownTasks(t *testing.T) {
	logs := `{"success":true,"data":{"now_time":1792904417,"activities":[[
		{"id":101,"type":19,"title":"线下课程9课前预习习题","create_time":1792888844,
		 "content":{"score_d":1792943999000,"leaf_id":48013198}},
		{"id":102,"type":17,"title":"线下课程9课前预习视频","create_time":1792888796,
		 "content":{"score_d":1792943999000,"leaf_id":48013194}}]]}}`
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case learningListPath:
			_, _ = w.Write(fixture(t, "learning_list.json"))
		case classroomLogsPath:
			_, _ = io.WriteString(w, logs)
		}
	}), 0)

	result, err := client.Fetch(context.Background(), entity.Account{}, testSession(t, "sid"),
		entity.FetchOptions{Now: func() time.Time { return time.Date(2026, 10, 25, 13, 0, 0, 0, time.UTC) }}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Assignments) != 2 {
		t.Fatalf("19/17 都应落库，实际 %d 条", len(result.Assignments))
	}
	for _, w := range result.Warnings {
		if strings.Contains(w.Message, "未知活动类型") {
			t.Fatalf("19/17 是已知类型，不该告警: %#v", result.Warnings)
		}
	}
	if !result.Complete {
		t.Fatal("已知类型不应把结果标记为 partial")
	}
}

// type=19 的练习没有截止时间时也要保留：它是作业类，不能按「非作业类型」丢掉
func TestFetchType19WithoutDeadlineIsKept(t *testing.T) {
	logs := `{"success":true,"data":{"now_time":1792904417,"activities":[[
		{"id":201,"type":19,"title":"线下课程3课堂练习","create_time":1789959009,
		 "content":{"score_d":"","leaf_id":3772429}}]]}}`
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case learningListPath:
			_, _ = w.Write(fixture(t, "learning_list.json"))
		case classroomLogsPath:
			_, _ = io.WriteString(w, logs)
		}
	}), 0)

	result, err := client.Fetch(context.Background(), entity.Account{}, testSession(t, "sid"),
		entity.FetchOptions{Now: func() time.Time { return time.Date(2026, 10, 25, 13, 0, 0, 0, time.UTC) }}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Assignments) != 1 {
		t.Fatalf("无截止时间的课堂练习也应保留，实际 %d 条", len(result.Assignments))
	}
	got := result.Assignments[0]
	if got.Title != "线下课程3课堂练习" || got.Deadline != nil ||
		got.DeadlineSource != entity.DeadlineUnknown {
		t.Fatalf("assignment = %#v", got)
	}
	var sawWarn bool
	for _, w := range result.Warnings {
		if strings.Contains(w.Message, "缺少 score_d") {
			sawWarn = true
		}
	}
	if !sawWarn {
		t.Fatalf("空 score_d 应留 warning: %#v", result.Warnings)
	}
	if result.Complete {
		t.Fatal("有解析降级时 Complete 应为 false")
	}
}

func TestFetchFallsBackToLatestSemester(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case learningListPath:
			_, _ = w.Write(fixture(t, "learning_list.json"))
		case classroomLogsPath:
			_, _ = w.Write(fixture(t, "classroom_logs.json"))
		}
	}), 0)

	// 2027 秋没有课，应退化为列表里最新的 2026秋，并记 warning
	now := time.Date(2027, 10, 22, 12, 0, 0, 0, time.UTC)
	result, err := client.Fetch(context.Background(), entity.Account{}, testSession(t, "sid"),
		entity.FetchOptions{Now: func() time.Time { return now }}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Courses) != 1 || result.Courses[0].SemesterKey != "2026秋" {
		t.Fatalf("退化结果 = %#v", result.Courses)
	}
	var found bool
	for _, w := range result.Warnings {
		if strings.Contains(w.Message, "退化") {
			found = true
		}
	}
	if !found {
		t.Fatalf("学期退化必须记 warning: %#v", result.Warnings)
	}
}

func TestCurrentSemesterBoundaries(t *testing.T) {
	cases := []struct {
		at   time.Time
		want string
	}{
		{time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), "2026秋"},
		{time.Date(2027, 1, 15, 0, 0, 0, 0, time.UTC), "2026秋"},
		{time.Date(2027, 2, 1, 0, 0, 0, 0, time.UTC), "2027春"},
		{time.Date(2027, 8, 31, 0, 0, 0, 0, time.UTC), "2027春"},
	}
	for _, c := range cases {
		if got := currentSemester(c.at).Key(); got != c.want {
			t.Errorf("%s -> %s, want %s", c.at.Format("2006-01-02"), got, c.want)
		}
	}
}

// 2026-10-25 用户账号实测：课堂 todo_list 精确对应课程页「未完成」，
// 待办外的习题都已答完。用它换算提交状态，视频只认「待办内」这一侧
func TestFetchMapsTodoListStatus(t *testing.T) {
	logs := `{"success":true,"data":{"now_time":1792904417,
		"classroom":{"sku_id":13062048,"uv_id":3042},
		"activities":[[
		{"id":101,"type":19,"title":"已交作业","create_time":1792888844,
		 "content":{"score_d":1792943999000,"leaf_id":48013198}},
		{"id":102,"type":17,"title":"看完视频","create_time":1792888796,
		 "content":{"score_d":1792943999000,"leaf_id":48013194}},
		{"id":103,"type":19,"title":"未交作业","create_time":1792888844,
		 "content":{"score_d":1792943999000,"leaf_id":48013199}},
		{"id":104,"type":17,"title":"未看完视频","create_time":1792888796,
		 "content":{"score_d":1792943999000,"leaf_id":48013195}}]]}}`
	todo := `{"success":true,"data":{"count":{"total":2},"result":[
		{"leaf_id":48013199,"type":6},
		{"leaf_id":48013195,"type":0}]}}`

	var todoPath, todoQuery string
	var todoHeader http.Header
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == learningListPath:
			_, _ = w.Write(fixture(t, "learning_list.json"))
		case r.URL.Path == classroomLogsPath:
			_, _ = io.WriteString(w, logs)
		case strings.Contains(r.URL.Path, "/todo_list/"):
			todoPath, todoQuery, todoHeader = r.URL.Path, r.URL.RawQuery, r.Header.Clone()
			_, _ = io.WriteString(w, todo)
		default:
			http.NotFound(w, r)
		}
	}), 0)

	result, err := client.Fetch(context.Background(), entity.Account{}, testSession(t, "sid"),
		entity.FetchOptions{Now: func() time.Time { return time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC) }}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if todoPath != "/c27/online_courseware/course/classroom/20000001/13062048/todo_list/" {
		t.Fatalf("待办路径 = %q", todoPath)
	}
	if todoQuery != "client_type=h5" {
		t.Fatalf("待办参数 = %q", todoQuery)
	}
	for k, want := range map[string]string{
		"xtbz": "ykt", "Xt-Agent": "web", "X-Client": "web",
		"University-Id": "1001", "Uv-Id": "1001", "Classroom-Id": "20000001",
		"Cookie": "sessionid=sid",
	} {
		if got := todoHeader.Get(k); got != want {
			t.Errorf("请求头 %s = %q, want %q", k, got, want)
		}
	}

	wantStatus := map[string]entity.SubmissionStatus{
		"已交作业":  entity.StatusSubmitted,
		"看完视频":  entity.StatusUnknown,
		"未交作业":  entity.StatusUnsubmitted,
		"未看完视频": entity.StatusUnsubmitted,
	}
	if len(result.Assignments) != len(wantStatus) {
		t.Fatalf("作业 = %d 条, want %d", len(result.Assignments), len(wantStatus))
	}
	for _, a := range result.Assignments {
		if a.Status != wantStatus[a.Title] {
			t.Errorf("%s 状态 = %s, want %s", a.Title, a.Status, wantStatus[a.Title])
		}
	}
	if !result.Complete {
		t.Fatal("待办查询成功不应影响 Complete")
	}
}

// 待办接口失败只降级状态，作业本身照常入库
func TestFetchTodoFailureKeepsUnknown(t *testing.T) {
	logs := `{"success":true,"data":{"now_time":1792904417,
		"classroom":{"sku_id":13062048,"uv_id":3042},
		"activities":[[{"id":101,"type":19,"title":"作业","create_time":1792888844,
		 "content":{"score_d":1792943999000,"leaf_id":48013198}}]]}}`
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == learningListPath:
			_, _ = w.Write(fixture(t, "learning_list.json"))
		case r.URL.Path == classroomLogsPath:
			_, _ = io.WriteString(w, logs)
		case strings.Contains(r.URL.Path, "/todo_list/"):
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, "boom")
		default:
			http.NotFound(w, r)
		}
	}), 0)

	result, err := client.Fetch(context.Background(), entity.Account{}, testSession(t, "sid"),
		entity.FetchOptions{Now: func() time.Time { return time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC) }}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Assignments) != 1 || result.Assignments[0].Status != entity.StatusUnknown {
		t.Fatalf("待办失败时状态必须 unknown: %#v", result.Assignments)
	}
	var sawWarn bool
	for _, w := range result.Warnings {
		if strings.Contains(w.Message, "待办列表获取失败") {
			sawWarn = true
		}
	}
	if !sawWarn {
		t.Fatalf("待办失败要记 warning: %#v", result.Warnings)
	}
	if !result.Complete {
		t.Fatal("待办失败不能让作业参与软删除")
	}
}

// 课堂级 sku 缺失时无法查待办，状态保持 unknown 并显式告警
func TestFetchMissingSkuKeepsUnknown(t *testing.T) {
	logs := `{"success":true,"data":{"now_time":1792904417,
		"activities":[[{"id":101,"type":19,"title":"作业","create_time":1792888844,
		 "content":{"score_d":1792943999000,"leaf_id":48013198}}]]}}`
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case learningListPath:
			_, _ = w.Write(fixture(t, "learning_list.json"))
		case classroomLogsPath:
			_, _ = io.WriteString(w, logs)
		default:
			http.NotFound(w, r)
		}
	}), 0)

	result, err := client.Fetch(context.Background(), entity.Account{}, testSession(t, "sid"),
		entity.FetchOptions{Now: func() time.Time { return time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC) }}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Assignments) != 1 || result.Assignments[0].Status != entity.StatusUnknown {
		t.Fatalf("缺 sku 时状态必须 unknown: %#v", result.Assignments)
	}
	var sawWarn bool
	for _, w := range result.Warnings {
		if strings.Contains(w.Message, "缺少 sku_id") {
			sawWarn = true
		}
	}
	if !sawWarn {
		t.Fatalf("缺 sku 要记 warning: %#v", result.Warnings)
	}
}
