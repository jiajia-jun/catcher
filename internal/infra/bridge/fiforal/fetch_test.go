package fiforal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"catcher/internal/domain/entity"
)

func newFetchClient(t *testing.T, now time.Time, handler func(*http.Request) (*http.Response, error)) (*Client, *fakeTransport) {
	t.Helper()
	c, ft := newTestClient(t, now, handler)
	c.cacheToken(&h5Token{
		Value:     "old-h5-token",
		UserID:    "user-1",
		Source:    defaultSource,
		ExpiresAt: now.Add(time.Hour),
	})
	return c, ft
}

// taskHandler 按 status-page 返回预置响应，未预置的组合按 500 处理
func taskHandler(pages map[string]string) func(*http.Request) (*http.Response, error) {
	return func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "moral.fifedu.com" || r.URL.Path != taskListPath {
			return nil, fmt.Errorf("unexpected request: %s", r.URL)
		}
		if err := r.ParseForm(); err != nil {
			return nil, err
		}
		key := r.Form.Get("status") + "-" + r.Form.Get("page")
		body, ok := pages[key]
		if !ok {
			return newResponse(500, "", nil), nil
		}
		return newResponse(200, body, nil), nil
	}
}

func runFetch(t *testing.T, c *Client) (*entity.FetchResult, error) {
	t.Helper()
	return c.Fetch(context.Background(), entity.Account{Platform: entity.PlatformFiforal},
		testSession(), entity.FetchOptions{}, nil)
}

func TestFetchPagingAndMapping(t *testing.T) {
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	c, _ := newFetchClient(t, now, taskHandler(map[string]string{
		"1-1": taskListBody(
			`{"taskId":"a","taskName":"Task A","teaName":"T1","taskInfo":"Info","endTime":"2026-11-20 23:59:59","delayTime":"2026-11-20 23:59:59","isDelay":0,"complete":100,"taskModelType":2}`),
		"1-2": taskListBody(),
		"2-1": taskListBody(
			`{"taskId":"b","taskName":"Task B","endTime":"2026-11-19 08:00:00","isDelay":0,"complete":0}`,
			`{"taskId":"c","taskName":"Task C","endTime":"2026-11-18 08:00:00","isDelay":0,"complete":null}`),
		"2-2": taskListBody(),
	}))

	result, err := runFetch(t, c)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !result.Complete || !result.CourseListComplete {
		t.Fatalf("两个列表都翻到空页时 Complete 应为 true: %#v", result)
	}
	if len(result.Assignments) != 3 {
		t.Fatalf("作业数 = %d, want 3", len(result.Assignments))
	}
	if len(result.Courses) != 1 || result.Courses[0].ExternalID != fiforalCourseID ||
		result.Courses[0].Name != fiforalCourseName {
		t.Fatalf("合成课程错误: %#v", result.Courses)
	}
	if result.ServerNow == nil {
		t.Fatal("应记录 sysTime")
	}

	byID := map[string]entity.Assignment{}
	for _, item := range result.Assignments {
		byID[item.ExternalID] = item
	}

	a := byID["a"]
	if a.Status != entity.StatusSubmitted {
		t.Fatalf("complete=100 应为已提交: %v", a.Status)
	}
	wantDeadline := time.Date(2026, 11, 20, 15, 59, 59, 0, time.UTC)
	if a.Deadline == nil || !a.Deadline.Equal(wantDeadline) {
		t.Fatalf("截止时间 = %v, want %v", a.Deadline, wantDeadline)
	}
	if a.DeadlineSource != entity.DeadlinePlatformExact {
		t.Fatalf("截止来源 = %v", a.DeadlineSource)
	}
	if a.Description != "教师：T1；Info" {
		t.Fatalf("描述 = %q", a.Description)
	}
	if !strings.Contains(a.DetailURL, "taskId=a") || !strings.Contains(a.DetailURL, "taskModelType=2") {
		t.Fatalf("详情链接 = %q", a.DetailURL)
	}
	if strings.Contains(a.DetailURL, "token") {
		t.Fatalf("详情链接不应带 token: %q", a.DetailURL)
	}
	if a.CourseExternalID != fiforalCourseID || a.CourseName != fiforalCourseName {
		t.Fatalf("作业课程错误: %#v", a)
	}
	if byID["b"].Status != entity.StatusUnsubmitted {
		t.Fatalf("complete=0 应为未提交: %v", byID["b"].Status)
	}
	if byID["c"].Status != entity.StatusUnknown {
		t.Fatalf("complete=null 应为未知: %v", byID["c"].Status)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("不应有告警: %#v", result.Warnings)
	}
}

func TestFetchDuplicatePageStops(t *testing.T) {
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	item := `{"taskId":"a","taskName":"A","endTime":"2026-11-20 23:59:59","complete":0}`
	c, _ := newFetchClient(t, now, taskHandler(map[string]string{
		"1-1": taskListBody(item),
		"1-2": taskListBody(item),
		"2-1": taskListBody(),
	}))
	result, err := runFetch(t, c)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if result.Complete {
		t.Fatal("整页重复时列表应标记不完整")
	}
	if len(result.Assignments) != 1 {
		t.Fatalf("作业数 = %d, want 1", len(result.Assignments))
	}
	if !hasWarning(result.Warnings, "重复") {
		t.Fatalf("缺少重复页告警: %#v", result.Warnings)
	}
}

func TestFetchMissingID(t *testing.T) {
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	c, _ := newFetchClient(t, now, taskHandler(map[string]string{
		"1-1": taskListBody(`{"taskName":"没有 id"}`),
		"2-1": taskListBody(),
	}))
	result, err := runFetch(t, c)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if result.Complete {
		t.Fatal("缺 id 时列表应标记不完整")
	}
	if !hasWarning(result.Warnings, "缺少任务 id") {
		t.Fatalf("缺少缺 id 告警: %#v", result.Warnings)
	}
}

func TestFetchInProgressFailureFailsWholeRun(t *testing.T) {
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	c, ft := newFetchClient(t, now, func(r *http.Request) (*http.Response, error) {
		_ = r.ParseForm()
		if r.Form.Get("status") == "1" {
			return newResponse(500, "", nil), nil
		}
		return newResponse(200, taskListBody(), nil), nil
	})
	result, err := runFetch(t, c)
	if !errors.Is(err, entity.ErrNetwork) || result != nil {
		t.Fatalf("进行中列表失败应整次失败，得到 (%v,%v)", result, err)
	}
	if ft.Count(taskListPath) != 1 {
		t.Fatalf("进行中列表失败后不应继续抓已结束列表，实际 %d 次请求", ft.Count(taskListPath))
	}
}

func TestFetchFinishedDegradesToWarning(t *testing.T) {
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	c, _ := newFetchClient(t, now, func(r *http.Request) (*http.Response, error) {
		_ = r.ParseForm()
		switch r.Form.Get("status") {
		case "1":
			if r.Form.Get("page") == "1" {
				return newResponse(200, taskListBody(`{"taskId":"a","taskName":"A","endTime":"2026-11-20 23:59:59","complete":0}`), nil), nil
			}
			return newResponse(200, taskListBody(), nil), nil
		default:
			return newResponse(500, "", nil), nil
		}
	})
	result, err := runFetch(t, c)
	if err != nil {
		t.Fatalf("已结束列表失败应降级，得到 %v", err)
	}
	if result.Complete {
		t.Fatal("已结束列表失败时 Complete 应为 false")
	}
	if len(result.Assignments) != 1 {
		t.Fatalf("应保留已抓到的进行中作业: %d", len(result.Assignments))
	}
	if !hasWarning(result.Warnings, "已结束任务列表读取失败") {
		t.Fatalf("缺少降级告警: %#v", result.Warnings)
	}
}

func TestFetchAuthExpiredRemintsOnce(t *testing.T) {
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	newJWT := jwtFor("user-1", now.Add(2*time.Hour).Unix())
	taskCalls := 0
	var authHeaders []string
	handler := func(r *http.Request) (*http.Response, error) {
		switch {
		case r.URL.Host == "oral.fifedu.com":
			return newResponse(302, "", map[string][]string{
				"Location": {"https://static.fifedu.com/x#/?token=" + newJWT},
			}), nil
		case r.URL.Host == "moral.fifedu.com":
			taskCalls++
			authHeaders = append(authHeaders, r.Header.Get("Authorization"))
			if taskCalls == 1 {
				return newResponse(200, `{"status":-1,"msg":"未登录","data":{}}`, nil), nil
			}
			return newResponse(200, taskListBody(), nil), nil
		}
		return nil, fmt.Errorf("unexpected %s", r.URL)
	}
	c, _ := newFetchClient(t, now, handler)

	result, err := runFetch(t, c)
	if err != nil {
		t.Fatalf("换 token 后应成功: %v", err)
	}
	if !result.Complete {
		t.Fatal("重试成功后列表应完整")
	}
	if taskCalls != 3 {
		t.Fatalf("任务接口调用 %d 次, want 3（一次 -1、一次空页、一次已结束空页）", taskCalls)
	}
	if len(authHeaders) < 2 || authHeaders[0] == authHeaders[1] {
		t.Fatalf("重试应使用换到的新 token: %#v", authHeaders)
	}
	if authHeaders[1] != "Bearer "+newJWT {
		t.Fatalf("Authorization = %q, want Bearer 新 token", authHeaders[1])
	}
	if c.token == nil || c.token.Value != newJWT {
		t.Fatal("新 token 没有被缓存")
	}
}

func TestFetchStopsAtMaxPages(t *testing.T) {
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	c, ft := newFetchClient(t, now, func(r *http.Request) (*http.Response, error) {
		_ = r.ParseForm()
		if r.Form.Get("status") == "2" {
			return newResponse(200, taskListBody(), nil), nil
		}
		page, _ := strconv.Atoi(r.Form.Get("page"))
		item := fmt.Sprintf(`{"taskId":"t-%d","taskName":"T","complete":0}`, page)
		return newResponse(200, taskListBody(item), nil), nil
	})

	result, err := runFetch(t, c)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if result.Complete {
		t.Fatal("超过页数上限时列表应标记不完整")
	}
	if len(result.Assignments) != maxPages {
		t.Fatalf("作业数 = %d, want %d", len(result.Assignments), maxPages)
	}
	if !hasWarning(result.Warnings, "超过 100 页") {
		t.Fatalf("缺少页数上限告警: %#v", result.Warnings)
	}
	if got := ft.Count(taskListPath); got != maxPages+1 {
		t.Fatalf("请求次数 = %d, want %d", got, maxPages+1)
	}
}

func TestFetchNonJSONIsPlatformChanged(t *testing.T) {
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	c, _ := newFetchClient(t, now, func(*http.Request) (*http.Response, error) {
		return newResponse(200, "<html>login</html>", nil), nil
	})
	if _, err := runFetch(t, c); !errors.Is(err, entity.ErrPlatformChanged) {
		t.Fatalf("非 JSON 应报 ErrPlatformChanged，得到 %v", err)
	}
}

func TestFetchRequiresCookies(t *testing.T) {
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	c, _ := newFetchClient(t, now, nil)
	empty := entity.Session{Platform: entity.PlatformFiforal, Secret: []byte(`{"cookies":[],"user_id":""}`)}
	_, err := c.Fetch(context.Background(), entity.Account{}, empty, entity.FetchOptions{}, nil)
	if !errors.Is(err, entity.ErrAuthRequired) {
		t.Fatalf("缺少 cookie 应报 ErrAuthRequired，得到 %v", err)
	}
}

func TestCallTaskListErrors(t *testing.T) {
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	token := &h5Token{Value: "h5", UserID: "u", Source: defaultSource, ExpiresAt: now.Add(time.Hour)}

	cases := map[string]struct {
		handler func(*http.Request) (*http.Response, error)
		want    error
	}{
		"3xx": {
			handler: func(*http.Request) (*http.Response, error) {
				return newResponse(302, "", map[string][]string{"Location": {"https://x"}}), nil
			},
			want: entity.ErrAuthExpired,
		},
		"401": {
			handler: func(*http.Request) (*http.Response, error) { return newResponse(401, "", nil), nil },
			want:    entity.ErrAuthExpired,
		},
		"5xx": {
			handler: func(*http.Request) (*http.Response, error) { return newResponse(500, "", nil), nil },
			want:    entity.ErrNetwork,
		},
		"status -1": {
			handler: func(*http.Request) (*http.Response, error) {
				return newResponse(200, `{"status":-1,"msg":"未登录","data":{}}`, nil), nil
			},
			want: entity.ErrAuthExpired,
		},
		"status 5": {
			handler: func(*http.Request) (*http.Response, error) {
				return newResponse(200, `{"status":5,"msg":"参数错误","data":{}}`, nil), nil
			},
			want: entity.ErrBusiness,
		},
	}
	for name, tc := range cases {
		tc := tc
		t.Run(name, func(t *testing.T) {
			c, _ := newTestClient(t, now, tc.handler)
			_, _, err := c.callTaskList(context.Background(), newCookieJar(nil, nil), token, statusInProgress, 1)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestStatusOf(t *testing.T) {
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	c, _ := newTestClient(t, now, nil)
	run := &fetchRun{client: c, result: &entity.FetchResult{}}

	cases := []struct {
		name  string
		value *int64
		want  entity.SubmissionStatus
	}{
		{"缺失", nil, entity.StatusUnknown},
		{"已提交", int64Ptr(100), entity.StatusSubmitted},
		{"未提交", int64Ptr(0), entity.StatusUnsubmitted},
		{"其他", int64Ptr(50), entity.StatusUnknown},
	}
	for _, tc := range cases {
		item := taskItem{}
		if tc.value != nil {
			item.Complete = flexPtr(*tc.value)
		}
		if got := run.statusOf(item, "a"); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
	if !hasWarning(run.result.Warnings, "无法识别") {
		t.Fatalf("complete=50 应写告警: %#v", run.result.Warnings)
	}
}

func TestDeadlineOf(t *testing.T) {
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	c, _ := newTestClient(t, now, nil)

	cases := []struct {
		name      string
		item      taskItem
		wantRaw   string
		wantNil   bool
		wantWarn  bool
		wantLocal string
	}{
		{
			name:      "isDelay=0 即使 delayTime 有值也用 endTime",
			item:      taskItem{EndTime: "2026-11-20 23:59:59", DelayTime: "2026-11-25 23:59:59", IsDelay: flexPtr(0)},
			wantRaw:   "2026-11-20 23:59:59",
			wantLocal: "2026-11-20 23:59:59",
		},
		{
			name:      "isDelay=1 用 delayTime",
			item:      taskItem{EndTime: "2026-11-20 23:59:59", DelayTime: "2026-11-25 23:59:59", IsDelay: flexPtr(1)},
			wantRaw:   "2026-11-25 23:59:59",
			wantLocal: "2026-11-25 23:59:59",
		},
		{
			name:      "isDelay=1 但 delayTime 为空退回 endTime",
			item:      taskItem{EndTime: "2026-11-20 23:59:59", IsDelay: flexPtr(1)},
			wantRaw:   "2026-11-20 23:59:59",
			wantLocal: "2026-11-20 23:59:59",
			wantWarn:  true,
		},
		{
			name:    "没有截止时间",
			item:    taskItem{},
			wantNil: true,
		},
		{
			name:     "格式错误保留原文",
			item:     taskItem{EndTime: "2026/11/20 23:59"},
			wantRaw:  "2026/11/20 23:59",
			wantNil:  true,
			wantWarn: true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			run := &fetchRun{client: c, result: &entity.FetchResult{}}
			raw, deadline := run.deadlineOf(tc.item, "a")
			if raw != tc.wantRaw {
				t.Fatalf("raw = %q, want %q", raw, tc.wantRaw)
			}
			if tc.wantNil && deadline != nil {
				t.Fatalf("deadline = %v, want nil", deadline)
			}
			if !tc.wantNil && deadline == nil {
				t.Fatal("deadline 不应为 nil")
			}
			if tc.wantLocal != "" {
				if got := deadline.In(shanghai).Format("2006-01-02 15:04:05"); got != tc.wantLocal {
					t.Fatalf("本地时间 = %q, want %q", got, tc.wantLocal)
				}
			}
			if got := hasWarning(run.result.Warnings, ""); got != tc.wantWarn {
				t.Fatalf("告警 = %v, want %v: %#v", got, tc.wantWarn, run.result.Warnings)
			}
		})
	}
}

func TestDetailURL(t *testing.T) {
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	c, _ := newTestClient(t, now, nil)

	got := c.detailURL(taskItem{TaskID: "task 1", TaskModelType: "2"})
	want := DefaultH5BaseURL + "#/pages/teaching/details/details?taskId=task+1&taskModelType=2"
	if got != want {
		t.Fatalf("detailURL = %q, want %q", got, want)
	}
	if got := c.detailURL(taskItem{TaskID: "a"}); !strings.HasSuffix(got, "?taskId=a") {
		t.Fatalf("缺少 taskModelType 时只带 taskId: %q", got)
	}
}

func TestFlexTypes(t *testing.T) {
	body := `{"taskId":"a","complete":"100","isDelay":"1","taskModelType":"2"}`
	var item taskItem
	if err := json.Unmarshal([]byte(body), &item); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if complete, ok := item.complete(); !ok || complete != 100 {
		t.Fatalf("字符串 complete 没转成整数: %#v", item.Complete)
	}
	if delayed, ok := item.isDelay(); !ok || delayed != 1 {
		t.Fatalf("字符串 isDelay 没转成整数: %#v", item.IsDelay)
	}
	if string(item.TaskModelType) != "2" {
		t.Fatalf("taskModelType 没转成字符串: %q", item.TaskModelType)
	}
}

func TestFlexIntUnparseableIsNotZero(t *testing.T) {
	var item taskItem
	if err := json.Unmarshal([]byte(`{"complete":"many"}`), &item); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := item.complete(); ok {
		t.Fatal("无法解析的 complete 不能当成 0 或任何已知值")
	}
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	c, _ := newTestClient(t, now, nil)
	run := &fetchRun{client: c, result: &entity.FetchResult{}}
	if got := run.statusOf(item, "a"); got != entity.StatusUnknown {
		t.Fatalf("无法解析的 complete 应为未知，得到 %v", got)
	}
}

func TestMergeTasksCrossListDuplicate(t *testing.T) {
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	c, _ := newTestClient(t, now, nil)
	run := &fetchRun{client: c, result: &entity.FetchResult{}}

	inProgress := []taskItem{{TaskID: "a"}}
	finished := []taskItem{{TaskID: "a"}, {TaskID: "b"}}
	got := mergeTasks(inProgress, finished, run)
	if len(got) != 2 || got[0].taskID() != "a" || got[1].taskID() != "b" {
		t.Fatalf("跨列表去重错误: %#v", got)
	}
	if !hasWarning(run.result.Warnings, "同时出现") {
		t.Fatalf("缺少跨列表重复告警: %#v", run.result.Warnings)
	}
}

func hasWarning(issues []entity.FetchIssue, substr string) bool {
	for _, issue := range issues {
		if strings.Contains(issue.Message, substr) {
			return true
		}
	}
	return false
}

func int64Ptr(v int64) *int64 { return &v }
