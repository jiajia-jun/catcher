package chaoxing

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
	"catcher/internal/infra/sender"
	"catcher/pkg/logger"
)

func TestFetchEndToEnd(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, shanghai)
	var detailHits atomic.Int32
	var listCookie, workCookie, listUA string

	mux := http.NewServeMux()
	mux.HandleFunc("/mycourse/backclazzdata", func(w http.ResponseWriter, r *http.Request) {
		listCookie = r.Header.Get("Cookie")
		listUA = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":1,"msg":"ok","channelList":[
			{"content":{"course":{"data":[
				{"id":214986481,"name":"大学物理ABC（上）"},
				{"id":266457278,"name":"概率论与数理统计"},
				{"id":264416084,"name":"高等数学A(下)"}
			]}}}
		]}`))
	})
	mux.HandleFunc("/work/stu-work", func(w http.ResponseWriter, r *http.Request) {
		workCookie = r.Header.Get("Cookie")
		base := "http://" + r.Host
		html := fmt.Sprintf(`<html><body><ul>
<li onclick="goTask(this);" data="%s/mooc-ans/android/mtaskmsgspecial?taskrefId=1001&amp;courseId=214986481&amp;type=work">
  <i class="task-work"></i>
  <span aria-label="作业名称第一次作业作业状态未提交所属课程大学物理AB...剩余时间312小时55分钟"></span>
</li>
<li onclick="goTask(this);" data="%s/mooc-ans/android/mtaskmsgspecial?taskrefId=1002&amp;courseId=266457278&amp;type=work">
  <i class="task-work-gray"></i>
  <span aria-label="作业名称作业补交 （前面有未及时提交的同学，今天内完成补交）20260622作业状态未提交所属课程概率论与数理..."></span>
</li>
<li onclick="goTask(this);" data="%s/mooc-ans/android/mtaskmsgspecial?taskrefId=1003&amp;courseId=264416084&amp;type=work">
  <i class="task-work-gray"></i>
  <span aria-label="作业名称期末补交作业状态已完成所属课程高等数学A(..."></span>
</li>
</ul><script>var haveMore = "0";</script></body></html>`, base, base, base)
		_, _ = w.Write([]byte(html))
	})
	mux.HandleFunc("/mooc-ans/android/mtaskmsgspecial", func(w http.ResponseWriter, r *http.Request) {
		detailHits.Add(1)
		switch r.URL.Query().Get("taskrefId") {
		case "1001":
			_, _ = w.Write([]byte(`<span aria-label="截止时间11月04日12时55分18秒"></span>`))
		case "1002":
			_, _ = w.Write([]byte(`<h4>截止时间：06-23 23:00</h4>`))
		default:
			// 已完成条目不应请求详情
			t.Errorf("不应请求已完成作业的详情: %s", r.URL)
			_, _ = w.Write([]byte(`<html></html>`))
		}
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := New(Options{
		BaseURL:      server.URL,
		LoginBaseURL: server.URL,
		Sender:       sender.New(sender.Config{Logger: logger.Discard()}),
		Logger:       logger.Discard(),
	})
	session := mustSession(t, "UID=u; JSESSIONID=sess-1")

	result, err := client.Fetch(context.Background(), entity.Account{}, session,
		entity.FetchOptions{Now: func() time.Time { return now }}, nil)
	if err != nil {
		t.Fatal(err)
	}

	if !result.Complete {
		t.Fatalf("haveMore=0 且全部解析时应完整: %#v", result.Warnings)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("不应有 warning: %#v", result.Warnings)
	}
	if len(result.Assignments) != 3 {
		t.Fatalf("作业数 = %d, want 3", len(result.Assignments))
	}
	if detailHits.Load() != 2 {
		t.Fatalf("详情请求数 = %d, want 2（已完成跳过）", detailHits.Load())
	}
	if !strings.Contains(listCookie, "JSESSIONID=sess-1") ||
		!strings.Contains(workCookie, "JSESSIONID=sess-1") {
		t.Fatalf("数据请求未携带 cookie: list=%q work=%q", listCookie, workCookie)
	}
	if !strings.Contains(listUA, "Mozilla/5.0") {
		t.Fatalf("数据请求缺少浏览器 UA: %q", listUA)
	}

	open, closed, submitted := result.Assignments[0], result.Assignments[1], result.Assignments[2]
	if open.Title != "第一次作业" || open.Status != entity.StatusUnsubmitted {
		t.Fatalf("第一条 = %#v", open)
	}
	if open.CourseName != "大学物理ABC（上）" {
		t.Fatalf("课程名未补全: %q", open.CourseName)
	}
	if open.CourseExternalID != "214986481" {
		t.Fatalf("courseId = %q（不能过 float64）", open.CourseExternalID)
	}
	if open.DedupKey != entity.ChaoxingDedupKey("214986481", "第一次作业") {
		t.Fatalf("去重键 = %q", open.DedupKey)
	}
	// 剩余时间只到分钟；偏差在容差内时采用详情页写死的精确时刻
	wantOpen := time.Date(2026, 11, 4, 12, 55, 18, 0, shanghai).UTC()
	if open.Deadline == nil || !open.Deadline.Equal(wantOpen) ||
		open.DeadlineSource != entity.DeadlineDerivedRemaining {
		t.Fatalf("进行中截止 = %v %s, want %v", open.Deadline, open.DeadlineSource, wantOpen)
	}

	if closed.CourseName != "概率论与数理统计" {
		t.Fatalf("课程名 = %q", closed.CourseName)
	}
	wantClosed := time.Date(2026, 6, 23, 23, 0, 0, 0, shanghai).UTC()
	if closed.Deadline == nil || !closed.Deadline.Equal(wantClosed) ||
		closed.DeadlineSource != entity.DeadlineInferredYear {
		t.Fatalf("已截止截止 = %v %s, want %v", closed.Deadline, closed.DeadlineSource, wantClosed)
	}

	if submitted.Status != entity.StatusSubmitted || submitted.Deadline != nil {
		t.Fatalf("已完成条目 = %#v（截止时间应为未知）", submitted)
	}
}

// 真实环境：作业列表给的详情链接是 Android 接口路径，服务器 302 到 H5 页面。
// 这是正常跳转，不能当登录失效
func TestFetchDetailFollowsRedirect(t *testing.T) {
	mux := http.NewServeMux()
	var base string
	mux.HandleFunc("/mycourse/backclazzdata", func(w http.ResponseWriter, r *http.Request) {
		base = "http://" + r.Host
		_, _ = w.Write([]byte(`{"result":1,"channelList":[
			{"content":{"course":{"data":[{"id":266457278,"name":"概率论与数理统计"}]}}}]}`))
	})
	mux.HandleFunc("/work/stu-work", func(w http.ResponseWriter, r *http.Request) {
		html := fmt.Sprintf(`<html><body><ul>
<li onclick="goTask(this);" data="%s/mooc-ans/android/mtaskmsgspecial?taskrefId=1001&amp;courseId=266457278&amp;type=work">
  <i class="task-work-gray"></i>
  <span aria-label="作业名称第三章作业作业状态未提交所属课程概率论与数..."></span>
</li>
</ul><script>var haveMore = "0";</script></body></html>`, base)
		_, _ = w.Write([]byte(html))
	})
	mux.HandleFunc("/mooc-ans/android/mtaskmsgspecial", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/mooc-ans/work/phone/task-work?taskrefId=1001", http.StatusFound)
	})
	mux.HandleFunc("/mooc-ans/work/phone/task-work", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body><h4>截止时间：10-01 21:37</h4></body></html>`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := New(Options{
		BaseURL:      server.URL,
		LoginBaseURL: server.URL,
		Sender:       sender.New(sender.Config{Logger: logger.Discard()}),
		Logger:       logger.Discard(),
	})
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, shanghai)
	result, err := client.Fetch(context.Background(), entity.Account{}, mustSession(t, "UID=u"),
		entity.FetchOptions{Now: func() time.Time { return now }}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Assignments) != 1 {
		t.Fatalf("作业数 = %d", len(result.Assignments))
	}
	want := time.Date(2026, 10, 1, 21, 37, 0, 0, shanghai).UTC()
	if d := result.Assignments[0].Deadline; d == nil || !d.Equal(want) {
		t.Fatalf("截止 = %v, want %v（详情页在 302 之后）", d, want)
	}
}

// 跳去登录页才是会话失效
func TestFetchDetailLoginRedirectIsAuthExpired(t *testing.T) {
	mux := http.NewServeMux()
	var base string
	mux.HandleFunc("/mycourse/backclazzdata", func(w http.ResponseWriter, r *http.Request) {
		base = "http://" + r.Host
		_, _ = w.Write([]byte(`{"result":1,"channelList":[]}`))
	})
	mux.HandleFunc("/work/stu-work", func(w http.ResponseWriter, r *http.Request) {
		html := fmt.Sprintf(`<html><body><ul>
<li onclick="goTask(this);" data="%s/mooc-ans/android/mtaskmsgspecial?taskrefId=1001&amp;courseId=1&amp;type=work">
  <i class="task-work-gray"></i>
  <span aria-label="作业名称作业作业状态未提交所属课程课程"></span>
</li>
</ul><script>var haveMore = "0";</script></body></html>`, base)
		_, _ = w.Write([]byte(html))
	})
	mux.HandleFunc("/mooc-ans/android/mtaskmsgspecial", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://passport2.chaoxing.com/login?refer=x", http.StatusFound)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := New(Options{
		BaseURL:      server.URL,
		LoginBaseURL: server.URL,
		Sender:       sender.New(sender.Config{Logger: logger.Discard()}),
		Logger:       logger.Discard(),
	})
	_, err := client.Fetch(context.Background(), entity.Account{}, mustSession(t, "UID=u"),
		entity.FetchOptions{}, nil)
	if !errors.Is(err, entity.ErrAuthExpired) {
		t.Fatalf("err = %v, want ErrAuthExpired", err)
	}
}

func TestFetchAuthExpiredOnResultZero(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"result":0,"msg":"请重新登录"}`))
	}))
	defer server.Close()

	client := New(Options{
		BaseURL:      server.URL,
		LoginBaseURL: server.URL,
		Sender:       sender.New(sender.Config{Logger: logger.Discard()}),
		Logger:       logger.Discard(),
	})
	_, err := client.Fetch(context.Background(), entity.Account{}, mustSession(t, "UID=u"), entity.FetchOptions{}, nil)
	if !errors.Is(err, entity.ErrAuthExpired) {
		t.Fatalf("err = %v, want ErrAuthExpired（失败不能静默成空列表）", err)
	}
}

func TestFetchAuthExpiredOnLoginPageHTML(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<!DOCTYPE html><html><body>请重新登录</body></html>`))
	}))
	defer server.Close()

	client := New(Options{
		BaseURL:      server.URL,
		LoginBaseURL: server.URL,
		Sender:       sender.New(sender.Config{Logger: logger.Discard()}),
		Logger:       logger.Discard(),
	})
	_, err := client.Fetch(context.Background(), entity.Account{}, mustSession(t, "UID=u"), entity.FetchOptions{}, nil)
	if !errors.Is(err, entity.ErrAuthExpired) {
		t.Fatalf("err = %v, want ErrAuthExpired", err)
	}
}

func TestFetchHaveMoreNonZeroIsPartial(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/mycourse/backclazzdata", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"result":1,"channelList":[]}`))
	})
	mux.HandleFunc("/work/stu-work", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><script>var haveMore = "1";</script></html>`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := New(Options{
		BaseURL:      server.URL,
		LoginBaseURL: server.URL,
		Sender:       sender.New(sender.Config{Logger: logger.Discard()}),
		Logger:       logger.Discard(),
	})
	result, err := client.Fetch(context.Background(), entity.Account{}, mustSession(t, "UID=u"), entity.FetchOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Complete {
		t.Fatal("haveMore 非 0 时必须 Complete=false")
	}
	if !hasIssueKind(result.Warnings, entity.IssueBusiness) {
		t.Fatalf("缺少 business warning: %#v", result.Warnings)
	}
}

func TestPersistCookiesOnRotation(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/mycourse/backclazzdata", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "route", Value: "new"})
		_, _ = w.Write([]byte(`{"result":1,"channelList":[]}`))
	})
	mux.HandleFunc("/work/stu-work", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><script>var haveMore = "0";</script></html>`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := New(Options{
		BaseURL:      server.URL,
		LoginBaseURL: server.URL,
		Sender:       sender.New(sender.Config{Logger: logger.Discard()}),
		Logger:       logger.Discard(),
	})

	var saved []byte
	result, err := client.Fetch(context.Background(), entity.Account{}, mustSession(t, "route=old"),
		entity.FetchOptions{OnSessionUpdated: func(s entity.Session) error {
			saved = append([]byte(nil), s.Secret...)
			return nil
		}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("result 为空")
	}
	if saved == nil {
		t.Fatal("cookie 轮换后必须通过 OnSessionUpdated 立即落盘")
	}
	var secret entity.ChaoxingSecret
	if err := json.Unmarshal(saved, &secret); err != nil {
		t.Fatal(err)
	}
	if len(secret.Cookies) != 1 || secret.Cookies[0].Value != "new" {
		t.Fatalf("落盘 cookie = %#v", secret.Cookies)
	}
}

func mustSession(t *testing.T, cookieHeader string) entity.Session {
	t.Helper()
	var cookies []entity.Cookie
	for _, part := range strings.Split(cookieHeader, ";") {
		name, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		cookies = append(cookies, entity.Cookie{Name: name, Value: value})
	}
	raw, err := json.Marshal(entity.ChaoxingSecret{Cookies: cookies})
	if err != nil {
		t.Fatal(err)
	}
	return entity.Session{Platform: entity.PlatformChaoxing, Secret: raw}
}
