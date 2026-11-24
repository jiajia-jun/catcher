package fiforal

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"catcher/internal/domain/entity"
	"catcher/internal/infra/sender"
	"catcher/pkg/logger"
)

// fakeTransport 按请求返回预置响应，测试全程离线，主机范围不受 fifedu 限制
type fakeTransport struct {
	mu      sync.Mutex
	handler func(*http.Request) (*http.Response, error)
	calls   []string
}

func (f *fakeTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	f.mu.Lock()
	f.calls = append(f.calls, r.Method+" "+r.URL.String())
	f.mu.Unlock()
	if f.handler == nil {
		return nil, fmt.Errorf("没有预置响应: %s %s", r.Method, r.URL.String())
	}
	return f.handler(r)
}

func (f *fakeTransport) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeTransport) Count(substr string) int {
	n := 0
	for _, call := range f.Calls() {
		if strings.Contains(call, substr) {
			n++
		}
	}
	return n
}

func newResponse(status int, body string, headers map[string][]string) *http.Response {
	h := http.Header{}
	for key, values := range headers {
		for _, value := range values {
			h.Add(key, value)
		}
	}
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:     h,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func newTestClient(t *testing.T, now time.Time, handler func(*http.Request) (*http.Response, error)) (*Client, *fakeTransport) {
	t.Helper()
	ft := &fakeTransport{handler: handler}
	c := New(Options{
		Sender: sender.New(sender.Config{Transport: ft, Logger: logger.Discard()}),
		Logger: logger.Discard(),
		Now:    func() time.Time { return now },
	})
	return c, ft
}

func jwtFor(userID string, exp int64) string {
	payload := fmt.Sprintf(`{"login_name":"x","user_id":%q,"user_key":"k","iat":%d,"exp":%d}`,
		userID, exp-7200, exp)
	return "eyJhbGciOiJIUzI1NiJ9." +
		base64.RawURLEncoding.EncodeToString([]byte(payload)) + ".sig"
}

func testSession(cookies ...entity.FiforalCookie) entity.Session {
	if len(cookies) == 0 {
		cookies = []entity.FiforalCookie{{
			Name: "prod-token", Value: "v", Domain: "fifedu.com", Path: "/", Secure: true,
		}}
	}
	raw, _ := json.Marshal(entity.FiforalSecret{Cookies: cookies})
	return entity.Session{Platform: entity.PlatformFiforal, Secret: raw}
}

func flexPtr(v int64) *flexInt {
	return &flexInt{value: v, ok: true}
}

func taskListBody(items ...string) string {
	list := strings.Join(items, ",")
	return `{"status":1,"msg":"","data":{"ttiList":[` + list + `],"sysTime":"2026-11-16 10:00:00"}}`
}
