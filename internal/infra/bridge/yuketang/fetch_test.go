package yuketang

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"catcher/internal/domain/entity"
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
