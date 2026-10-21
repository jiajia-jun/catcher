package logger

import (
	"net/http"
	"strings"
	"testing"
)

func TestIsSensitiveKey(t *testing.T) {
	cases := map[string]bool{
		"Cookie":        true,
		"Set-Cookie":    true,
		"Authorization": true,
		"access_token":  true,
		"refresh_token": true,
		"sessionid":     true,
		"qr_content":    true,
		"author":        false,
		"course_name":   false,
		"content-type":  false,
		"max_size_mb":   false,
		"user_id":       false,
	}
	for key, want := range cases {
		if got := IsSensitiveKey(key); got != want {
			t.Errorf("IsSensitiveKey(%q) = %v, want %v", key, got, want)
		}
	}
}

func TestRedactHeaders(t *testing.T) {
	h := http.Header{
		"Cookie":        {"sessionid=abc"},
		"Set-Cookie":    {"a=b", "c=d"},
		"Authorization": {"Bearer secret"},
		"Content-Type":  {"application/json"},
	}
	got := RedactHeaders(h)

	for _, key := range []string{"Cookie", "Set-Cookie", "Authorization"} {
		for _, v := range got[key] {
			if v != "***" {
				t.Fatalf("%s 未打码: %q", key, v)
			}
		}
	}
	if got["Content-Type"][0] != "application/json" {
		t.Fatalf("Content-Type 不应被打码: %q", got["Content-Type"][0])
	}
	// 原 header 不能被修改。
	if h["Cookie"][0] != "sessionid=abc" {
		t.Fatalf("RedactHeaders 修改了原 header")
	}
}

func TestRedactURL(t *testing.T) {
	in := "https://example.com/api?sessionid=abc&course=1&auth=xyz"
	got := RedactURL(in)
	for _, leaked := range []string{"abc", "xyz"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("URL 未打码: %s", got)
		}
	}
	if !strings.Contains(got, "course=1") {
		t.Fatalf("普通参数被误伤: %s", got)
	}
}
