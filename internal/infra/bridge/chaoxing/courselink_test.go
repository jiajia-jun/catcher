package chaoxing

import (
	"context"
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

func TestCourseHomeURL(t *testing.T) {
	client := New(Options{DesktopBaseURL: "https://mooc1.example.com"})

	got := client.courseHomeURL(courseEntry{ID: "266504745", ClassID: "153621536", CPI: "488789618"})
	want := "https://mooc1.example.com/visit/stucoursemiddle?courseid=266504745&clazzid=153621536&cpi=488789618&ismooc2=1&pageHeader=8&v=2"
	if got != want {
		t.Fatalf("课程主页 = %q, want %q", got, want)
	}

	// 缺 clazzId 或 cpi 都组不出有效地址
	for _, entry := range []courseEntry{
		{ID: "1", ClassID: "2"},
		{ID: "1", CPI: "3"},
		{ClassID: "2", CPI: "3"},
	} {
		if got := client.courseHomeURL(entry); got != "" {
			t.Fatalf("%#v 应返回空地址，得到 %q", entry, got)
		}
	}
}

// 有 clazzId/cpi 的课程跳到课程主页；缺 cpi 的课程保留手机版详情链接。
// 两种情况下截止时间都仍然从手机版详情页取
func TestFetchUsesCourseHomeURL(t *testing.T) {
	var detailHits atomic.Int32

	mux := http.NewServeMux()
	mux.HandleFunc("/mycourse/backclazzdata", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"result":1,"channelList":[
			{"content":{"id":153621536,"cpi":488789618,"course":{"data":[
				{"id":266504745,"name":"离散数学"}
			]}}},
			{"content":{"id":154000001,"course":{"data":[
				{"id":266504746,"name":"线性代数"}
			]}}}
		]}`))
	})
	mux.HandleFunc("/work/stu-work", func(w http.ResponseWriter, r *http.Request) {
		base := "http://" + r.Host
		_, _ = fmt.Fprintf(w, `<html><body><ul>
<li onclick="goTask(this);" data="%s/mooc-ans/android/mtaskmsgspecial?taskrefId=57939574&amp;courseId=266504745&amp;type=work">
  <span aria-label="作业名称离散数学课堂作业6作业状态未提交所属课程离散数学..."></span>
</li>
<li onclick="goTask(this);" data="%s/mooc-ans/android/mtaskmsgspecial?taskrefId=57939575&amp;courseId=266504746&amp;type=work">
  <span aria-label="作业名称线性代数课堂作业1作业状态未提交所属课程线性代数..."></span>
</li>
</ul><script>var haveMore = "0";</script></body></html>`, base, base)
	})
	mux.HandleFunc("/mooc-ans/android/mtaskmsgspecial", func(w http.ResponseWriter, r *http.Request) {
		detailHits.Add(1)
		_, _ = w.Write([]byte(`<span aria-label="截止时间10月01日21时37分00秒"></span>`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := New(Options{
		BaseURL:        server.URL,
		DesktopBaseURL: server.URL,
		LoginBaseURL:   server.URL,
		Sender:         sender.New(sender.Config{Logger: logger.Discard()}),
		Logger:         logger.Discard(),
	})
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, shanghai)
	result, err := client.Fetch(context.Background(), entity.Account{}, mustSession(t, "UID=u"),
		entity.FetchOptions{Now: func() time.Time { return now }}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Assignments) != 2 {
		t.Fatalf("作业数 = %d", len(result.Assignments))
	}

	home := server.URL + "/visit/stucoursemiddle?courseid=266504745&clazzid=153621536&cpi=488789618&ismooc2=1&pageHeader=8&v=2"
	if got := result.Assignments[0].DetailURL; got != home {
		t.Fatalf("detail_url = %q, want 课程主页 %q", got, home)
	}
	// 缺 cpi 的课程组不出课程主页，退回 ③ 给的详情链接
	if got := result.Assignments[1].DetailURL; !strings.Contains(got, "mtaskmsgspecial?taskrefId=57939575") {
		t.Fatalf("detail_url = %q, want 手机版详情链接", got)
	}
	if detailHits.Load() != 2 {
		t.Fatalf("详情请求数 = %d, want 2（截止时间仍走手机版详情页）", detailHits.Load())
	}
	want := time.Date(2026, 10, 1, 21, 37, 0, 0, shanghai).UTC()
	if d := result.Assignments[0].Deadline; d == nil || !d.Equal(want) {
		t.Fatalf("截止 = %v, want %v", d, want)
	}
}
