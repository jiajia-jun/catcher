package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"catcher/internal/domain/entity"
)

func TestEndpointValidation(t *testing.T) {
	for _, base := range []string{"http://example.com", "https://user:pw@example.com", "https://example.com?token=secret", "https://example.com/#key", "file:///tmp/key", "https://"} {
		t.Run(base, func(t *testing.T) {
			if _, e := Endpoint(base); e == nil {
				t.Fatal("应拒绝不安全的模型地址")
			}
		})
	}
	got, e := Endpoint("https://example.com/v1/")
	if e != nil || got != "https://example.com/v1/chat/completions" {
		t.Fatal(got, e)
	}
}
func TestGenerateJSONAndCredentials(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer FAKE_KEY" || r.URL.Path != "/v1/chat/completions" {
			t.Error("请求配置错误")
		}
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Fatal("JSON body")
		}
		if body["stream"] != false || body["response_format"].(map[string]any)["type"] != "json_object" {
			t.Error("应请求 JSON 输出")
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"ok\":true}"},"finish_reason":"stop"}],"usage":{"prompt_tokens":12,"completion_tokens":5}}`)
	}))
	defer server.Close()
	c := Client{Transport: server.Client().Transport}
	cfg := entity.DefaultAISettings()
	cfg.BaseURL = server.URL + "/v1"
	cfg.Key = "FAKE_KEY"
	out, err := c.Generate(context.Background(), entity.AIModelRequest{Settings: cfg, System: "json", User: "test"})
	if err != nil || !out.UsageKnown || out.InputTokens != 12 || out.OutputTokens != 5 {
		t.Fatal(out, err)
	}
}
func TestGenerateRejectsInvalidResponses(t *testing.T) {
	tests := []struct {
		name, body string
		status     int
	}{
		{"unauthorized", "FAKE_KEY token=secret", 401}, {"rate_limit", "secret", 429}, {"server_error", "secret", 500},
		{"redirect", "secret", 302}, {"not_json", "secret", 200}, {"empty_choices", `{"choices":[]}`, 200},
		{"empty_content", `{"choices":[{"message":{"content":"  "},"finish_reason":"stop"}]}`, 200},
		{"truncated", `{"choices":[{"message":{"content":"{}"},"finish_reason":"length"}]}`, 200},
		{"invalid_content", `{"choices":[{"message":{"content":"not json"},"finish_reason":"stop"}]}`, 200},
		{"oversized", strings.Repeat("x", (1<<20)+1), 200},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); fmt.Fprint(w, tc.body) }))
			defer srv.Close()
			cfg := entity.DefaultAISettings()
			cfg.BaseURL = srv.URL
			cfg.Key = "FAKE_KEY"
			_, e := (&Client{Transport: srv.Client().Transport}).Generate(context.Background(), entity.AIModelRequest{Settings: cfg})
			if e == nil || strings.Contains(e.Error(), "FAKE_KEY") || strings.Contains(e.Error(), "secret") {
				t.Fatal("必须拒绝无效响应且不回显服务端正文", e)
			}
		})
	}
}
func TestRedirectDoesNotForwardKey(t *testing.T) {
	hits := 0
	dst := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++ }))
	defer dst.Close()
	src := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Header().Set("Location", dst.URL); w.WriteHeader(307) }))
	defer src.Close()
	cfg := entity.DefaultAISettings()
	cfg.BaseURL = src.URL
	cfg.Key = "FAKE_KEY"
	_, e := (&Client{Transport: src.Client().Transport}).Generate(context.Background(), entity.AIModelRequest{Settings: cfg})
	if e == nil || hits != 0 {
		t.Fatal("不应跟随携带密钥的跳转", e, hits)
	}
}
func TestGenerateCancellation(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
	}))
	defer srv.Close()
	cfg := entity.DefaultAISettings()
	cfg.BaseURL = srv.URL
	cfg.Key = "FAKE_KEY"
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, e := (&Client{Transport: srv.Client().Transport}).Generate(ctx, entity.AIModelRequest{Settings: cfg})
	if e == nil || strings.Contains(e.Error(), "FAKE_KEY") {
		t.Fatal(e)
	}
}
