package tronclass

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"catcher/internal/domain/entity"
)

func fetchSession(t *testing.T) entity.Session {
	t.Helper()
	raw, err := json.Marshal(entity.TronclassSecret{Session: testSession, UserID: "12345"})
	if err != nil {
		t.Fatal(err)
	}
	return entity.Session{Platform: entity.PlatformTronclass, Secret: raw}
}

func newFetchClient(t *testing.T, handler http.Handler) (*Client, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return newTestClient(t, server.URL, nil), server
}

func hasIssue(warnings []entity.FetchIssue, kind entity.IssueKind, contains string) bool {
	for _, item := range warnings {
		if item.Kind == kind && strings.Contains(item.Message, contains) {
			return true
		}
	}
	return false
}

func TestFetchEndToEnd(t *testing.T) {
	var courseQueryBody map[string]any

	mux := http.NewServeMux()
	mux.HandleFunc(semestersPath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"semesters":[
			{"id":28,"name":"2025-2026第1学期","is_active":false},
			{"id":34,"name":"2026-2027第1学期","is_active":true}
		]}`)
	})
	mux.HandleFunc(coursesPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("课程列表方法 = %s", r.Method)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("Accept = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q", got)
		}
		var query map[string]any
		if err := json.NewDecoder(r.Body).Decode(&query); err != nil {
			t.Fatal(err)
		}
		if query["page"] == float64(1) {
			courseQueryBody = query
		}
		switch query["page"] {
		case float64(1):
			writeJSON(w, `{"courses":[
				{"id":101,"name":"离散数学B","semester":{"id":34,"code":"2026-1"}},
				{"id":999,"name":"无学期课","semester":null}
			],"page":1,"page_size":100,"pages":2,"total":2}`)
		default:
			writeJSON(w, `{"courses":[
				{"id":102,"name":"数据结构","semester":{"id":34,"code":"2026-1"}}
			],"page":2,"page_size":100,"pages":2,"total":2}`)
		}
	})
	mux.HandleFunc("/api/courses/101/homework-activities", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("page") {
		case "1":
			writeJSON(w, `{"page":1,"page_size":100,"pages":2,"total":3,"homework_activities":[
				{"id":549652,"title":"第一次作业","type":"homework",
				 "deadline":"2026-10-30T15:59:00Z","submitted":true},
				{"id":549653,"title":"随堂测验","type":"exam","deadline":"","submitted":false}
			]}`)
		default:
			writeJSON(w, `{"page":2,"page_size":100,"pages":2,"total":3,"homework_activities":[
				{"id":549654,"title":"无状态作业","deadline":"not-a-time"}
			]}`)
		}
	})
	mux.HandleFunc("/api/courses/102/homework-activities", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"page":0,"page_size":0,"pages":0,"total":0,"homework_activities":[]}`)
	})
	client, _ := newFetchClient(t, mux)

	result, err := client.Fetch(context.Background(), entity.Account{}, fetchSession(t),
		entity.FetchOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}

	// 显式传 page 与 semester_id，才能拿到完整字段和分页 envelope
	if courseQueryBody["page"] != float64(1) {
		t.Fatalf("课程查询缺少 page: %#v", courseQueryBody)
	}
	conditions, ok := courseQueryBody["conditions"].(map[string]any)
	if !ok || conditions["semester_id"] != float64(34) {
		t.Fatalf("课程过滤条件 = %#v", courseQueryBody["conditions"])
	}
	fields, _ := courseQueryBody["fields"].(string)
	if !strings.Contains(fields, "semester") {
		t.Fatalf("fields = %q", fields)
	}

	if !result.Complete || !result.CourseListComplete || len(result.FailedCourses) != 0 {
		t.Fatalf("完整性 = complete:%v course:%v failed:%v",
			result.Complete, result.CourseListComplete, result.FailedCourses)
	}
	if len(result.Courses) != 2 {
		t.Fatalf("课程数 = %d, want 2（semester=null 跳过）: %#v", len(result.Courses), result.Courses)
	}
	if result.Courses[0].ExternalID != "101" || result.Courses[0].SemesterKey != "2026-1" {
		t.Fatalf("课程 = %#v", result.Courses[0])
	}

	if len(result.Assignments) != 3 {
		t.Fatalf("作业数 = %d, want 3: %#v", len(result.Assignments), result.Assignments)
	}
	first, second, third := result.Assignments[0], result.Assignments[1], result.Assignments[2]
	wantDeadline := time.Date(2026, 10, 30, 15, 59, 0, 0, time.UTC)
	if first.Status != entity.StatusSubmitted || first.Deadline == nil || !first.Deadline.Equal(wantDeadline) {
		t.Fatalf("第一条 = %#v", first)
	}
	if first.DeadlineSource != entity.DeadlinePlatformExact {
		t.Fatalf("截止来源 = %s", first.DeadlineSource)
	}
	if first.DedupKey != entity.TronclassDedupKey("101", "549652") {
		t.Fatalf("去重键 = %q", first.DedupKey)
	}
	if first.CourseExternalID != "101" || first.CourseName != "离散数学B" {
		t.Fatalf("课程归属 = %q %q", first.CourseExternalID, first.CourseName)
	}

	// 非 homework 类型照常入库；空截止按未知
	if second.Title != "随堂测验" || second.Status != entity.StatusUnsubmitted ||
		second.Deadline != nil || second.DeadlineSource != entity.DeadlineUnknown {
		t.Fatalf("第二条 = %#v", second)
	}

	// submitted 缺失与截止时间非法都必须显式暴露
	if third.Status != entity.StatusUnknown || third.Deadline != nil {
		t.Fatalf("第三条 = %#v", third)
	}
	if !hasIssue(result.Warnings, entity.IssueParse, "缺少提交状态") {
		t.Fatalf("缺少 submitted warning: %#v", result.Warnings)
	}
	if !hasIssue(result.Warnings, entity.IssueParse, "截止时间无法解析") {
		t.Fatalf("缺少截止时间 warning: %#v", result.Warnings)
	}
}

func TestFetchRequiresUniqueActiveSemester(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{name: "没有当前学期", body: `{"semesters":[{"id":28,"is_active":false}]}`},
		{name: "多个当前学期", body: `{"semesters":[{"id":34,"is_active":true},{"id":35,"is_active":true}]}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc(semestersPath, func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, c.body)
			})
			client, _ := newFetchClient(t, mux)

			_, err := client.Fetch(context.Background(), entity.Account{}, fetchSession(t),
				entity.FetchOptions{}, nil)
			if !errors.Is(err, entity.ErrPlatformChanged) {
				t.Fatalf("err = %v, want ErrPlatformChanged", err)
			}
		})
	}
}

func TestFetchAuthExpiredAborts(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc(semestersPath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"semesters":[{"id":34,"is_active":true}]}`)
	})
	mux.HandleFunc(coursesPath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"courses":[{"id":101,"name":"课程","semester":{"id":34,"code":"2026-1"}}],
			"page":1,"page_size":100,"pages":1,"total":1}`)
	})
	mux.HandleFunc("/api/courses/101/homework-activities", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":""}`))
	})
	client, _ := newFetchClient(t, mux)

	result, err := client.Fetch(context.Background(), entity.Account{}, fetchSession(t),
		entity.FetchOptions{}, nil)
	if !errors.Is(err, entity.ErrAuthExpired) {
		t.Fatalf("err = %v, want ErrAuthExpired", err)
	}
	if result != nil {
		t.Fatal("认证失效时不得返回部分结果（会被当作成功的空列表）")
	}
}

func TestFetchCourseFailureKeepsOtherCourses(t *testing.T) {
	var failedCalls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc(semestersPath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"semesters":[{"id":34,"is_active":true}]}`)
	})
	mux.HandleFunc(coursesPath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"courses":[
			{"id":101,"name":"课程一","semester":{"id":34,"code":"2026-1"}},
			{"id":102,"name":"课程二","semester":{"id":34,"code":"2026-1"}}
		],"page":1,"page_size":100,"pages":1,"total":2}`)
	})
	mux.HandleFunc("/api/courses/101/homework-activities", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"page":1,"page_size":100,"pages":1,"total":1,"homework_activities":[
			{"id":1,"title":"作业一","type":"homework","deadline":"","submitted":false}
		]}`)
	})
	mux.HandleFunc("/api/courses/102/homework-activities", func(w http.ResponseWriter, r *http.Request) {
		failedCalls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`<html>参数形态错误</html>`))
	})
	client, _ := newFetchClient(t, mux)

	result, err := client.Fetch(context.Background(), entity.Account{}, fetchSession(t),
		entity.FetchOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.CourseListComplete || result.Complete {
		t.Fatalf("完整性 = course:%v complete:%v", result.CourseListComplete, result.Complete)
	}
	if len(result.FailedCourses) != 1 || result.FailedCourses[0] != "102" {
		t.Fatalf("FailedCourses = %#v", result.FailedCourses)
	}
	if len(result.Assignments) != 1 {
		t.Fatalf("作业数 = %d, want 1", len(result.Assignments))
	}
	if !hasIssue(result.Warnings, entity.IssueParse, "课程二") {
		t.Fatalf("缺少课程失败 warning: %#v", result.Warnings)
	}
	if failedCalls.Load() != 1 {
		t.Fatalf("500 不应重试，请求次数 = %d", failedCalls.Load())
	}
}

func TestFetchPersistsRotatedSessionOnEveryOK(t *testing.T) {
	var seq atomic.Int32
	var cookies []string
	rotate := func(w http.ResponseWriter) {
		n := seq.Add(1)
		http.SetCookie(w, &http.Cookie{
			Name:  "session",
			Value: fmt.Sprintf("V2-1-r%d.MTIzNDU.1700000000.sig", n),
		})
	}
	mux := http.NewServeMux()
	mux.HandleFunc(semestersPath, func(w http.ResponseWriter, r *http.Request) {
		cookies = append(cookies, r.Header.Get("Cookie"))
		rotate(w)
		writeJSON(w, `{"semesters":[{"id":34,"is_active":true}]}`)
	})
	mux.HandleFunc(coursesPath, func(w http.ResponseWriter, r *http.Request) {
		cookies = append(cookies, r.Header.Get("Cookie"))
		rotate(w)
		writeJSON(w, `{"courses":[{"id":101,"name":"课程","semester":{"id":34,"code":"2026-1"}}],
			"page":1,"page_size":100,"pages":1,"total":1}`)
	})
	mux.HandleFunc("/api/courses/101/homework-activities", func(w http.ResponseWriter, r *http.Request) {
		cookies = append(cookies, r.Header.Get("Cookie"))
		rotate(w)
		writeJSON(w, `{"page":0,"pages":0,"total":0,"homework_activities":[]}`)
	})
	client, _ := newFetchClient(t, mux)

	var saved []entity.TronclassSecret
	_, err := client.Fetch(context.Background(), entity.Account{}, fetchSession(t),
		entity.FetchOptions{OnSessionUpdated: func(session entity.Session) error {
			var secret entity.TronclassSecret
			if err := json.Unmarshal(session.Secret, &secret); err != nil {
				t.Fatal(err)
			}
			saved = append(saved, secret)
			return nil
		}}, nil)
	if err != nil {
		t.Fatal(err)
	}

	// 三个 200 各保存一次，后一个请求带的是前一个响应轮换后的值
	if len(saved) != 3 {
		t.Fatalf("保存次数 = %d, want 3", len(saved))
	}
	if saved[0].Session != "V2-1-r1.MTIzNDU.1700000000.sig" {
		t.Fatalf("首次保存 = %q", saved[0].Session)
	}
	if len(cookies) != 3 || !strings.Contains(cookies[1], "r1.") || !strings.Contains(cookies[2], "r2.") {
		t.Fatalf("后续请求未使用轮换后的 session: %#v", cookies)
	}
}

func TestFetchIgnoresAnonymousSessionOn401(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc(semestersPath, func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "V2-1-good.MTIzNDU.1700000000.sig"})
		writeJSON(w, `{"semesters":[{"id":34,"is_active":true}]}`)
	})
	mux.HandleFunc(coursesPath, func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "V2-1-good2.MTIzNDU.1700000000.sig"})
		writeJSON(w, `{"courses":[{"id":101,"name":"课程","semester":{"id":34,"code":"2026-1"}}],
			"page":1,"page_size":100,"pages":1,"total":1}`)
	})
	mux.HandleFunc("/api/courses/101/homework-activities", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "anonymous"})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":""}`))
	})
	client, _ := newFetchClient(t, mux)

	var saved []string
	_, err := client.Fetch(context.Background(), entity.Account{}, fetchSession(t),
		entity.FetchOptions{OnSessionUpdated: func(session entity.Session) error {
			var secret entity.TronclassSecret
			if err := json.Unmarshal(session.Secret, &secret); err != nil {
				t.Fatal(err)
			}
			saved = append(saved, secret.Session)
			return nil
		}}, nil)
	if !errors.Is(err, entity.ErrAuthExpired) {
		t.Fatalf("err = %v, want ErrAuthExpired", err)
	}
	if len(saved) != 2 {
		t.Fatalf("保存次数 = %d, want 2（401 不保存）", len(saved))
	}
	for _, value := range saved {
		if strings.Contains(value, "anonymous") {
			t.Fatalf("401 的匿名 session 被保存: %#v", saved)
		}
	}
}

func TestFetchCoursePageLimitStops(t *testing.T) {
	var calls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc(semestersPath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"semesters":[{"id":34,"is_active":true}]}`)
	})
	mux.HandleFunc(coursesPath, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		writeJSON(w, `{"courses":[],"page":1,"page_size":100,"pages":999,"total":0}`)
	})
	client, _ := newFetchClient(t, mux)

	result, err := client.Fetch(context.Background(), entity.Account{}, fetchSession(t),
		entity.FetchOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.CourseListComplete || result.Complete {
		t.Fatal("课程页数超限时必须标记不完整")
	}
	if got := calls.Load(); got != maxCoursePages {
		t.Fatalf("课程请求次数 = %d, want %d", got, maxCoursePages)
	}
	if !hasIssue(result.Warnings, entity.IssueParse, "课程列表超过") {
		t.Fatalf("缺少页数超限 warning: %#v", result.Warnings)
	}
}

func TestFetchHomeworkPageLimitMarksCourseFailed(t *testing.T) {
	var calls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc(semestersPath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"semesters":[{"id":34,"is_active":true}]}`)
	})
	mux.HandleFunc(coursesPath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"courses":[{"id":101,"name":"课程","semester":{"id":34,"code":"2026-1"}}],
			"page":1,"page_size":100,"pages":1,"total":1}`)
	})
	mux.HandleFunc("/api/courses/101/homework-activities", func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		writeJSON(w, `{"page":1,"page_size":100,"pages":999,"total":0,"homework_activities":[]}`)
	})
	client, _ := newFetchClient(t, mux)

	result, err := client.Fetch(context.Background(), entity.Account{}, fetchSession(t),
		entity.FetchOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.FailedCourses) != 1 || result.FailedCourses[0] != "101" {
		t.Fatalf("FailedCourses = %#v", result.FailedCourses)
	}
	if got := calls.Load(); got != maxHomeworkPages {
		t.Fatalf("作业请求次数 = %d, want %d", got, maxHomeworkPages)
	}
}

func writeJSON(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(body))
}
