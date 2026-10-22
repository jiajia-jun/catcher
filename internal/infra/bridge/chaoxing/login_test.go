package chaoxing

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"catcher/internal/domain/entity"
	"catcher/internal/infra/sender"
	"catcher/pkg/logger"
)

func newTestClient(t *testing.T, serverURL string) *Client {
	t.Helper()
	return New(Options{
		BaseURL:      serverURL,
		LoginBaseURL: serverURL,
		Sender:       sender.New(sender.Config{Logger: logger.Discard()}),
		Logger:       logger.Discard(),
	})
}

func TestLoginFollowsRedirectsAndCollectsCookies(t *testing.T) {
	var gotUsername, gotUA string
	mux := http.NewServeMux()
	mux.HandleFunc("/fanyalogin", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("解析表单失败: %v", err)
		}
		gotUsername = r.PostForm.Get("uname")
		gotUA = r.Header.Get("User-Agent")
		http.SetCookie(w, &http.Cookie{Name: "UID", Value: "12345", Path: "/"})
		http.Redirect(w, r, "/passport/step2", http.StatusFound)
	})
	mux.HandleFunc("/passport/step2", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "JSESSIONID", Value: "sess-1", Path: "/", HttpOnly: true})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":true}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := newTestClient(t, server.URL)
	session, err := client.LoginWithPassword(context.Background(), entity.PasswordCredential{
		Phone:    "13800138000",
		Password: "secret-password",
	})
	if err != nil {
		t.Fatal(err)
	}

	// 学习通拦截 Go 默认 UA，必须带浏览器标识
	if !strings.Contains(gotUA, "Mozilla/5.0") {
		t.Fatalf("登录请求缺少浏览器 UA: %q", gotUA)
	}

	// uname 必须是 AES 加密后的值，不能是手机号明文
	if gotUsername == "13800138000" || gotUsername == "" {
		t.Fatalf("uname 未加密: %q", gotUsername)
	}
	if decrypted := decryptAES(t, gotUsername); decrypted != "13800138000" {
		t.Fatalf("uname 解密 = %q", decrypted)
	}

	var secret entity.ChaoxingSecret
	if err := json.Unmarshal(session.Secret, &secret); err != nil {
		t.Fatal(err)
	}
	cookies := map[string]string{}
	for _, cookie := range secret.Cookies {
		cookies[cookie.Name] = cookie.Value
	}
	if cookies["UID"] != "12345" || cookies["JSESSIONID"] != "sess-1" {
		t.Fatalf("cookie 袋 = %#v（逐跳的 Set-Cookie 都必须收到）", cookies)
	}
	if strings.Contains(string(session.Secret), "secret-password") {
		t.Fatal("会话秘密里不能出现密码明文")
	}
}

func TestLoginPreservesPostOn307(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/fanyalogin", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "route", Value: "a"})
		http.Redirect(w, r, "/retry", http.StatusTemporaryRedirect)
	})
	mux.HandleFunc("/retry", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("307 后方法 = %s, want POST", r.Method)
		}
		http.SetCookie(w, &http.Cookie{Name: "route", Value: "b"})
		_, _ = w.Write([]byte(`{"status":true}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	session, err := newTestClient(t, server.URL).LoginWithPassword(context.Background(),
		entity.PasswordCredential{Phone: "13800138000", Password: "x"})
	if err != nil {
		t.Fatal(err)
	}
	var secret entity.ChaoxingSecret
	if err := json.Unmarshal(session.Secret, &secret); err != nil {
		t.Fatal(err)
	}
	if len(secret.Cookies) != 1 || secret.Cookies[0].Value != "b" {
		t.Fatalf("同名 cookie 应以最后一跳为准: %#v", secret.Cookies)
	}
}

func TestLoginFailureReadsMsg2(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":false,"msg2":"用户名或密码错误"}`))
	}))
	defer server.Close()

	_, err := newTestClient(t, server.URL).LoginWithPassword(context.Background(),
		entity.PasswordCredential{Phone: "13800138000", Password: "wrong"})
	if !errors.Is(err, entity.ErrAuthFailed) {
		t.Fatalf("err = %v, want ErrAuthFailed", err)
	}
	if !strings.Contains(err.Error(), "用户名或密码错误") {
		t.Fatalf("err = %v，应包含 msg2", err)
	}
}

func TestLoginRejectsBadPhoneBeforeRequest(t *testing.T) {
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte(`{"status":true}`))
	}))
	defer server.Close()

	_, err := newTestClient(t, server.URL).LoginWithPassword(context.Background(),
		entity.PasswordCredential{Phone: "1380013", Password: "x"})
	if !errors.Is(err, entity.ErrAuthFailed) {
		t.Fatalf("err = %v", err)
	}
	if hits != 0 {
		t.Fatalf("本地校验失败时不应发请求，实际 %d 次", hits)
	}
}

func TestLoginTooManyRedirects(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/loop", http.StatusFound)
	}))
	defer server.Close()

	_, err := newTestClient(t, server.URL).LoginWithPassword(context.Background(),
		entity.PasswordCredential{Phone: "13800138000", Password: "x"})
	if !errors.Is(err, entity.ErrAuthFailed) {
		t.Fatalf("err = %v, want ErrAuthFailed", err)
	}
}

func TestLoginMissingLocation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusFound)
	}))
	defer server.Close()

	_, err := newTestClient(t, server.URL).LoginWithPassword(context.Background(),
		entity.PasswordCredential{Phone: "13800138000", Password: "x"})
	if !errors.Is(err, entity.ErrAuthFailed) {
		t.Fatalf("err = %v, want ErrAuthFailed", err)
	}
}
