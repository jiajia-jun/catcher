package fiforal

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"catcher/internal/domain/entity"
)

func TestEnsureTokenUsesCache(t *testing.T) {
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	c, ft := newTestClient(t, now, func(*http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("不应发起请求")
	})
	cached := &h5Token{Value: "cached", UserID: "u", Source: defaultSource, ExpiresAt: now.Add(time.Hour)}

	if _, err := c.ensureToken(context.Background(), newCookieJar(nil, nil)); err == nil {
		t.Fatal("没有缓存时应发请求")
	}
	before := len(ft.Calls())
	c.cacheToken(cached)
	got, err := c.ensureToken(context.Background(), newCookieJar(nil, nil))
	if err != nil || got.Value != "cached" {
		t.Fatalf("应直接使用缓存: %#v, %v", got, err)
	}
	if after := len(ft.Calls()); after != before {
		t.Fatalf("使用缓存时不应请求: %#v", ft.Calls()[before:])
	}
}

func TestEnsureTokenRefreshesNearExpiry(t *testing.T) {
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	newJWT := jwtFor("user-1", now.Add(2*time.Hour).Unix())
	c, ft := newTestClient(t, now, func(r *http.Request) (*http.Response, error) {
		return newResponse(302, "", map[string][]string{
			"Location": {"https://static.fifedu.com/x#/?token=" + newJWT},
		}), nil
	})
	// 距过期不足 5 分钟，应当重新换
	c.cacheToken(&h5Token{
		Value:     "old",
		UserID:    "user-1",
		Source:    defaultSource,
		ExpiresAt: now.Add(time.Minute),
	})

	got, err := c.ensureToken(context.Background(), newCookieJar(nil, nil))
	if err != nil {
		t.Fatalf("ensureToken: %v", err)
	}
	if got.Value != newJWT {
		t.Fatalf("快过期时应换新 token: %#v", got)
	}
	if ft.Count("/kyxl-web/student/index.do") != 1 {
		t.Fatalf("应重走一次 oral 入口: %#v", ft.Calls())
	}
}

func TestEnsureTokenRemintFailureIsAuthExpired(t *testing.T) {
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	c, _ := newTestClient(t, now, func(r *http.Request) (*http.Response, error) {
		switch r.URL.Host {
		case "oral.fifedu.com":
			return newResponse(302, "", map[string][]string{
				"Location": {"https://cycore.fifedu.com/cas-server/login?service=x"},
			}), nil
		case "cycore.fifedu.com":
			// cookie 失效后落回 CAS 登录页
			return newResponse(200, "<html>Loading...</html>", nil), nil
		}
		return nil, fmt.Errorf("unexpected %s", r.URL)
	})
	c.cacheToken(&h5Token{
		Value:     "old",
		UserID:    "user-1",
		Source:    defaultSource,
		ExpiresAt: now.Add(-time.Minute),
	})

	_, err := c.ensureToken(context.Background(), newCookieJar(nil, nil))
	if !errors.Is(err, entity.ErrAuthExpired) {
		t.Fatalf("换 token 失败应报 ErrAuthExpired，得到 %v", err)
	}
}

func TestRemintTokenForcesNetwork(t *testing.T) {
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	newJWT := jwtFor("user-1", now.Add(2*time.Hour).Unix())
	c, ft := newTestClient(t, now, func(r *http.Request) (*http.Response, error) {
		return newResponse(302, "", map[string][]string{
			"Location": {"https://static.fifedu.com/x#/?token=" + newJWT},
		}), nil
	})
	// 缓存仍有效，remint 强制忽略缓存
	c.cacheToken(&h5Token{
		Value:     "old",
		UserID:    "user-1",
		Source:    defaultSource,
		ExpiresAt: now.Add(time.Hour),
	})

	got, err := c.remintToken(context.Background(), newCookieJar(nil, nil))
	if err != nil || got.Value != newJWT {
		t.Fatalf("remintToken = (%#v,%v)", got, err)
	}
	if ft.Count("/kyxl-web/student/index.do") != 1 {
		t.Fatalf("remint 应强制请求一次: %#v", ft.Calls())
	}
	if c.token == nil || c.token.Value != newJWT {
		t.Fatal("remint 后应更新缓存")
	}
}
