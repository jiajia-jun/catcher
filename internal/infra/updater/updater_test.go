package updater

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"catcher/internal/domain/entity"
)

func newTestChecker(t *testing.T, handler http.Handler) *Checker {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return New(Options{
		BaseURL: srv.URL,
		Owner:   "o",
		Repo:    "r",
		Current: "1.0.0",
	})
}

func TestLatestReleaseSuccess(t *testing.T) {
	var gotPath, gotAccept, gotUA string
	checker := newTestChecker(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAccept = r.Header.Get("Accept")
		gotUA = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"tag_name": "v1.1.0",
			"name": "Catcher 1.1.0",
			"html_url": "https://github.com/o/r/releases/tag/v1.1.0",
			"published_at": "2026-11-01T08:00:00Z"
		}`))
	}))

	info, err := checker.LatestRelease(context.Background())
	if err != nil {
		t.Fatalf("LatestRelease 返回错误: %v", err)
	}
	if info.Version != "1.1.0" || info.Tag != "v1.1.0" {
		t.Errorf("版本解析不对: %+v", info)
	}
	if info.Name != "Catcher 1.1.0" {
		t.Errorf("Name = %q", info.Name)
	}
	if info.URL != "https://github.com/o/r/releases/tag/v1.1.0" {
		t.Errorf("URL = %q", info.URL)
	}
	if info.PublishedAt.IsZero() {
		t.Error("PublishedAt 未解析")
	}
	if gotPath != "/repos/o/r/releases/latest" {
		t.Errorf("请求路径 = %q", gotPath)
	}
	if gotAccept != "application/vnd.github+json" {
		t.Errorf("Accept = %q", gotAccept)
	}
	if gotUA != "Catcher/1.0.0" {
		t.Errorf("User-Agent = %q", gotUA)
	}
}

func TestLatestReleaseFallsBackURL(t *testing.T) {
	for _, raw := range []string{"", "javascript:alert(1)", "not a url"} {
		checker := newTestChecker(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"tag_name":"v1.1.0","html_url":"` + raw + `"}`))
		}))
		info, err := checker.LatestRelease(context.Background())
		if err != nil {
			t.Fatalf("html_url=%q 返回错误: %v", raw, err)
		}
		want := "https://github.com/o/r/releases"
		if info.URL != want {
			t.Errorf("html_url=%q 时 URL = %q, want %q", raw, info.URL, want)
		}
	}
}

func TestLatestReleaseStatusErrors(t *testing.T) {
	cases := []struct {
		status int
		want   error
	}{
		{status: http.StatusNotFound, want: entity.ErrReleaseUnavailable},
		{status: http.StatusGone, want: entity.ErrReleaseUnavailable},
		{status: http.StatusUnauthorized, want: entity.ErrReleaseUnavailable},
		{status: http.StatusForbidden, want: entity.ErrReleaseRateLimited},
		{status: http.StatusTooManyRequests, want: entity.ErrReleaseRateLimited},
		{status: http.StatusInternalServerError, want: entity.ErrNetwork},
		{status: http.StatusBadGateway, want: entity.ErrNetwork},
	}
	for _, c := range cases {
		checker := newTestChecker(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(c.status)
		}))
		_, err := checker.LatestRelease(context.Background())
		if !errors.Is(err, c.want) {
			t.Errorf("HTTP %d 错误 = %v, want %v", c.status, err, c.want)
		}
	}
}

func TestLatestReleaseInvalidPayload(t *testing.T) {
	cases := []string{
		`not json`,
		`{}`,
		`{"tag_name":""}`,
		`{"tag_name":"release-1.1.0"}`,
		`{"tag_name":"1.1"}`,
	}
	for _, body := range cases {
		checker := newTestChecker(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(body))
		}))
		_, err := checker.LatestRelease(context.Background())
		if !errors.Is(err, entity.ErrReleaseInvalid) {
			t.Errorf("响应 %q 错误 = %v, want ErrReleaseInvalid", body, err)
		}
	}
}

func TestLatestReleaseTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte(`{"tag_name":"v1.1.0"}`))
	}))
	defer srv.Close()

	checker := New(Options{
		BaseURL: srv.URL,
		Owner:   "o",
		Repo:    "r",
		Current: "1.0.0",
		Client:  &http.Client{Timeout: 30 * time.Millisecond},
	})
	_, err := checker.LatestRelease(context.Background())
	if !errors.Is(err, entity.ErrNetwork) {
		t.Errorf("超时错误 = %v, want ErrNetwork", err)
	}
}

func TestLatestReleaseCanceled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte(`{"tag_name":"v1.1.0"}`))
	}))
	defer srv.Close()

	checker := New(Options{BaseURL: srv.URL, Owner: "o", Repo: "r"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := checker.LatestRelease(ctx)
	if !errors.Is(err, entity.ErrCanceled) {
		t.Errorf("取消错误 = %v, want ErrCanceled", err)
	}
}
