package tronclass

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"catcher/internal/domain/entity"
	"catcher/internal/infra/sender"
	"catcher/pkg/logger"
)

const testSession = "V2-1-uuid.MTIzNDU.1700000000.sig"

func newTestClient(t *testing.T, baseURL string, transport http.RoundTripper) *Client {
	t.Helper()
	cfg := sender.Config{Logger: logger.Discard()}
	if transport != nil {
		cfg.Transport = transport
	}
	return New(Options{
		BaseURL: baseURL,
		Sender:  sender.New(cfg),
		Logger:  logger.Discard(),
	})
}

func decodeTronclassSecret(t *testing.T, session *entity.Session) entity.TronclassSecret {
	t.Helper()
	var secret entity.TronclassSecret
	if err := json.Unmarshal(session.Secret, &secret); err != nil {
		t.Fatal(err)
	}
	return secret
}

func TestImportSessionAcceptsBareAndPrefixed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != todosPath {
			t.Errorf("验证路径 = %s", r.URL.Path)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("Accept = %q", got)
		}
		if got := r.Header.Get("Cookie"); got != "session="+testSession {
			t.Errorf("Cookie = %q", got)
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write([]byte(`{"todo_list":[]}`))
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, nil)
	for _, input := range []string{testSession, "  session=" + testSession + "  "} {
		session, err := client.ImportSession(context.Background(), input)
		if err != nil {
			t.Fatalf("导入 %q 失败: %v", input, err)
		}
		secret := decodeTronclassSecret(t, session)
		if secret.Session != testSession {
			t.Fatalf("session = %q, want %q", secret.Session, testSession)
		}
		if secret.UserID != "12345" {
			t.Fatalf("uid = %q, want 12345", secret.UserID)
		}
	}
}

func TestImportSessionUsesRotatedSession(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "V2-1-new.MTIzNDU.1700000100.sig"})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"todo_list":[]}`))
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, nil)
	session, err := client.ImportSession(context.Background(), testSession)
	if err != nil {
		t.Fatal(err)
	}
	if got := decodeTronclassSecret(t, session).Session; got != "V2-1-new.MTIzNDU.1700000100.sig" {
		t.Fatalf("session = %q, want 轮换后的值", got)
	}
}

func TestImportSessionExpired(t *testing.T) {
	cases := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{
			name: "401",
			handler: func(w http.ResponseWriter, r *http.Request) {
				// 401 下发的匿名 session 必须被忽略
				http.SetCookie(w, &http.Cookie{Name: "session", Value: "anonymous"})
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"message":""}`))
			},
		},
		{
			name: "302 到 CAS",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", "https://identity.tc.cqupt.edu.cn/auth/realms/cqupt/protocol/cas/login")
				w.WriteHeader(http.StatusFound)
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			server := httptest.NewServer(c.handler)
			defer server.Close()

			client := newTestClient(t, server.URL, nil)
			_, err := client.ImportSession(context.Background(), testSession)
			if !errors.Is(err, entity.ErrAuthExpired) {
				t.Fatalf("err = %v, want ErrAuthExpired", err)
			}
		})
	}
}

func TestImportSessionRejectsNonJSON(t *testing.T) {
	cases := []struct {
		name        string
		contentType string
		body        string
	}{
		{name: "HTML", contentType: "text/html", body: `<html><body>登录</body></html>`},
		{name: "伪 JSON", contentType: "application/json", body: `not-json`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", c.contentType)
				_, _ = w.Write([]byte(c.body))
			}))
			defer server.Close()

			client := newTestClient(t, server.URL, nil)
			_, err := client.ImportSession(context.Background(), testSession)
			if !errors.Is(err, entity.ErrPlatformChanged) {
				t.Fatalf("err = %v, want ErrPlatformChanged", err)
			}
		})
	}
}

// flakyTransport 第一次连接失败，之后转发给真实 Transport
type flakyTransport struct {
	failures int
	calls    atomic.Int32
	base     http.RoundTripper
}

func (f *flakyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if int(f.calls.Add(1)) <= f.failures {
		return nil, errors.New("连接被重置")
	}
	return f.base.RoundTrip(req)
}

func TestImportSessionRetriesNetworkOnce(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"todo_list":[]}`))
	}))
	defer server.Close()

	transport := &flakyTransport{failures: 1, base: http.DefaultTransport}
	client := newTestClient(t, server.URL, transport)
	if _, err := client.ImportSession(context.Background(), testSession); err != nil {
		t.Fatalf("重试后应成功: %v", err)
	}
	if got := transport.calls.Load(); got != 2 {
		t.Fatalf("请求次数 = %d, want 2", got)
	}
}

func TestImportSessionDoesNotRetry401(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":""}`))
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, nil)
	_, err := client.ImportSession(context.Background(), testSession)
	if !errors.Is(err, entity.ErrAuthExpired) {
		t.Fatalf("err = %v, want ErrAuthExpired", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("401 不应重试，请求次数 = %d", got)
	}
}

func TestImportSessionRejectsEmpty(t *testing.T) {
	client := New(Options{Sender: sender.New(sender.Config{Logger: logger.Discard()})})
	for _, input := range []string{"", "   ", "session="} {
		if _, err := client.ImportSession(context.Background(), input); err == nil {
			t.Fatalf("空输入 %q 应当报错", input)
		}
	}
}

func TestParseUID(t *testing.T) {
	cases := []struct {
		session string
		want    string
	}{
		{session: "V2-1-uuid.MTIzNDU.1700000000.sig", want: "12345"},
		{session: "V2-1-uuid..1700000000.sig", want: ""},
		{session: "V2-1-uuid.!!!.1700000000.sig", want: ""},
		{session: "no-dots", want: ""},
		// base64url 解出不可打印字节
		{session: "V2-1-uuid.Bw.1700000000.sig", want: ""},
	}
	for _, c := range cases {
		if got := parseUID(c.session); got != c.want {
			t.Errorf("parseUID(%q) = %q, want %q", c.session, got, c.want)
		}
	}
}
