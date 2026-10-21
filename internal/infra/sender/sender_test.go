package sender

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDoRedirectNotFollowed(t *testing.T) {
	var hits int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		if r.URL.Path == "/next" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "sessionid", Value: "abc"})
		w.Header().Set("Location", "/next")
		w.WriteHeader(http.StatusFound)
	}))
	defer server.Close()

	s := New(Config{})
	resp, err := s.Do(context.Background(), &Request{Method: http.MethodGet, URL: server.URL})

	var statusErr *StatusError
	if !errors.As(err, &statusErr) || statusErr.Code != http.StatusFound {
		t.Fatalf("期望 StatusError{302}，实际 %v", err)
	}
	if resp == nil {
		t.Fatal("302 必须保留 Response")
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("不应跟随重定向，请求次数 = %d", got)
	}
	if resp.Location != server.URL+"/next" {
		t.Fatalf("Location 未解析为绝对地址: %q", resp.Location)
	}
	if len(resp.SetCookies) != 1 || !strings.Contains(resp.SetCookies[0], "sessionid=abc") {
		t.Fatalf("Set-Cookie 未保留: %#v", resp.SetCookies)
	}
}

func TestDoNon2xxReturnsResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer server.Close()

	s := New(Config{})
	resp, err := s.Do(context.Background(), &Request{Method: http.MethodGet, URL: server.URL})

	var statusErr *StatusError
	if !errors.As(err, &statusErr) || statusErr.Code != http.StatusInternalServerError {
		t.Fatalf("期望 StatusError{500}，实际 %v", err)
	}
	if resp == nil || string(resp.Raw) != "boom" {
		t.Fatalf("非 2xx 的 body 必须可读，实际 %#v", resp)
	}
}

func TestDoTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(500 * time.Millisecond):
		case <-r.Context().Done():
		}
	}))
	defer server.Close()

	s := New(Config{})
	_, err := s.Do(context.Background(), &Request{
		Method:  http.MethodGet,
		URL:     server.URL,
		Timeout: 20 * time.Millisecond,
	})

	var timeoutErr *TimeoutError
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("期望 TimeoutError，实际 %v", err)
	}
}

func TestDoCanceled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s := New(Config{})
	_, err := s.Do(ctx, &Request{Method: http.MethodGet, URL: server.URL})

	var canceledErr *CanceledError
	if !errors.As(err, &canceledErr) {
		t.Fatalf("期望 CanceledError，实际 %v", err)
	}
}

func TestDoTooLarge(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("0123456789"))
	}))
	defer server.Close()

	s := New(Config{MaxBodyBytes: 4})
	resp, err := s.Do(context.Background(), &Request{Method: http.MethodGet, URL: server.URL})

	var tooLarge *TooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("期望 TooLargeError，实际 %v", err)
	}
	if resp != nil {
		t.Fatal("超限时不应返回被截断的 Response")
	}
	if tooLarge.Limit != 4 || tooLarge.Got != 5 {
		t.Fatalf("超限信息不正确: %+v", tooLarge)
	}
}

func TestDoSendsHeaderAndBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		if r.Header.Get("X-Test") != "1" {
			t.Errorf("header 未发送")
		}
		buf := make([]byte, 4)
		n, _ := r.Body.Read(buf)
		if string(buf[:n]) != "ping" {
			t.Errorf("body = %q", buf[:n])
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	s := New(Config{})
	resp, err := s.Do(context.Background(), &Request{
		Method: http.MethodPost,
		URL:    server.URL,
		Header: map[string]string{"X-Test": "1"},
		Body:   []byte("ping"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != http.StatusNoContent {
		t.Fatalf("status = %d", resp.Status)
	}
}
