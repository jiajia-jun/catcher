package fiforal

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"catcher/internal/domain/entity"
)

func TestParseCallbackPage(t *testing.T) {
	raw := []byte(`var username = 'u1';
var password = "p1";
var nextPage = "https%3A%2F%2Foral.fifedu.com%2Fkyxl-web%2Fstudent%2Findex.do";
new EduSSO("https://sso.example/cas", "https://cas.example/cas-server");`)
	page, err := parseCallbackPage(raw)
	if err != nil {
		t.Fatalf("parseCallbackPage: %v", err)
	}
	if page.Username != "u1" || page.Password != "p1" {
		t.Fatalf("账号密码解析错误: %#v", page)
	}
	if page.NextPage != "https://oral.fifedu.com/kyxl-web/student/index.do" {
		t.Fatalf("nextPage 没有解码一次: %q", page.NextPage)
	}
	if page.SSOService != "https://sso.example/cas" || page.CASServer != "https://cas.example/cas-server" {
		t.Fatalf("EduSSO 地址解析错误: %#v", page)
	}
}

func TestParseCallbackPageDefaultsEduSSO(t *testing.T) {
	raw := []byte(`var username = "u1";
var password = "p1";
var nextPage = "https%3A%2F%2Foral.fifedu.com%2Fx";`)
	page, err := parseCallbackPage(raw)
	if err != nil {
		t.Fatalf("parseCallbackPage: %v", err)
	}
	if page.SSOService != DefaultSSOService || page.CASServer != DefaultCASServer {
		t.Fatalf("EduSSO 缺失时应使用默认值: %#v", page)
	}
}

func TestParseCallbackPageMissingFields(t *testing.T) {
	raw := []byte(`var username = "u1";
var nextPage = "https%3A%2F%2Foral.fifedu.com%2Fx";`)
	if _, err := parseCallbackPage(raw); !errors.Is(err, entity.ErrPlatformChanged) {
		t.Fatalf("缺少密码应报 ErrPlatformChanged，得到 %v", err)
	}
}

func TestParseCallbackPageDoubleQuotes(t *testing.T) {
	raw := []byte(`var username = "u1"; var password = "p1"; var nextPage = "https%3A%2F%2Fa";`)
	if _, err := parseCallbackPage(raw); err != nil {
		t.Fatalf("双引号不应解析失败: %v", err)
	}
}

func TestParseJSONP(t *testing.T) {
	var out struct {
		Login string `json:"login"`
		Token string `json:"token"`
	}
	if err := parseJSONP([]byte(`logincallback({"login":"success","token":"t"})`), &out); err != nil {
		t.Fatalf("parseJSONP: %v", err)
	}
	if out.Login != "success" || out.Token != "t" {
		t.Fatalf("JSONP 解析错误: %#v", out)
	}
	if err := parseJSONP([]byte(`not json`), &out); err == nil {
		t.Fatal("非 JSON 应报错")
	}
}

func TestParseH5Token(t *testing.T) {
	exp := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC).Unix()
	token, err := parseH5Token(jwtFor("user-1", exp), "")
	if err != nil {
		t.Fatalf("parseH5Token: %v", err)
	}
	if token.UserID != "user-1" || token.Source != defaultSource || token.ExpiresAt.Unix() != exp {
		t.Fatalf("token 解析错误: %#v", token)
	}
	if got, err := parseH5Token(jwtFor("user-1", exp), "10002"); err != nil || got.Source != "10002" {
		t.Fatalf("source 没有按 fragment 取值: %#v, %v", got, err)
	}

	if _, err := parseH5Token("only.two", ""); !errors.Is(err, entity.ErrPlatformChanged) {
		t.Fatalf("两段式 token 应报 ErrPlatformChanged，得到 %v", err)
	}
	badPayload := "eyJhbGciOiJIUzI1NiJ9.bm90LWpzb24.sig"
	if _, err := parseH5Token(badPayload, ""); !errors.Is(err, entity.ErrPlatformChanged) {
		t.Fatalf("载荷不是 JSON 应报 ErrPlatformChanged，得到 %v", err)
	}
}

func TestLandingFollows302AndReadsFragment(t *testing.T) {
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	jwt := jwtFor("user-1", now.Add(2*time.Hour).Unix())
	c, ft := newTestClient(t, now, func(r *http.Request) (*http.Response, error) {
		switch {
		case r.URL.Host == "www.fifedu.com" && r.URL.Path == "/iplat/ssoservice":
			return newResponse(302, "", map[string][]string{
				"Location": {"https://cycore.fifedu.com/cas-server/login?service=x"},
			}), nil
		case r.URL.Host == "cycore.fifedu.com" && r.URL.Path == "/cas-server/login":
			return newResponse(302, "", map[string][]string{
				"Location": {"https://static.fifedu.com/x/index.html#/?source=10003&token=" + jwt},
			}), nil
		}
		return nil, fmt.Errorf("unexpected %s", r.URL)
	})

	token, err := c.landingToToken(context.Background(), newCookieJar(nil, nil),
		"https://www.fifedu.com/iplat/ssoservice?nextPage=x")
	if err != nil {
		t.Fatalf("landingToToken: %v", err)
	}
	if token.UserID != "user-1" || token.Value != jwt || token.Source != "10003" {
		t.Fatalf("落地 token 错误: %#v", token)
	}
	if got := ft.Count("static.fifedu.com"); got != 0 {
		t.Fatalf("拿到 token 后不应再请求 static H5，实际 %d 次", got)
	}
}

func TestLandingRejectsForeignHost(t *testing.T) {
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	c, _ := newTestClient(t, now, func(*http.Request) (*http.Response, error) {
		return newResponse(302, "", map[string][]string{
			"Location": {"https://evil.example.com/steal"},
		}), nil
	})
	_, err := c.landingToToken(context.Background(), newCookieJar(nil, nil),
		"https://www.fifedu.com/iplat/ssoservice")
	if !errors.Is(err, entity.ErrPlatformChanged) {
		t.Fatalf("跨域跳转应报 ErrPlatformChanged，得到 %v", err)
	}
}

func TestLandingLoginPageMeansExpired(t *testing.T) {
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	c, _ := newTestClient(t, now, func(*http.Request) (*http.Response, error) {
		return newResponse(200, "<html>Loading...</html>", nil), nil
	})
	_, err := c.landingToToken(context.Background(), newCookieJar(nil, nil),
		"https://cycore.fifedu.com/cas-server/login?service=x")
	if !errors.Is(err, entity.ErrAuthExpired) {
		t.Fatalf("落在 CAS 登录页应报 ErrAuthExpired，得到 %v", err)
	}
}

func TestLandingFollowsTransitionPageScript(t *testing.T) {
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	jwt := jwtFor("user-1", now.Add(time.Hour).Unix())
	c, ft := newTestClient(t, now, func(r *http.Request) (*http.Response, error) {
		switch r.URL.Host {
		case "pass.fifedu.com":
			body := `<script>location.href="https://static.fifedu.com/x#/?token=` + jwt + `"</script>`
			return newResponse(200, body, nil), nil
		}
		return nil, fmt.Errorf("unexpected %s", r.URL)
	})
	token, err := c.landingToToken(context.Background(), newCookieJar(nil, nil),
		"https://pass.fifedu.com/iplat-pass/go")
	if err != nil {
		t.Fatalf("过渡页脚本跳转没有被跟随: %v", err)
	}
	if token.Value != jwt {
		t.Fatalf("token 错误: %#v", token)
	}
	if got := ft.Count("static.fifedu.com"); got != 0 {
		t.Fatalf("不应真的请求 static：%d 次", got)
	}
}

func TestLandingDoesNotFollowBusinessScriptWithoutToken(t *testing.T) {
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	c, ft := newTestClient(t, now, func(r *http.Request) (*http.Response, error) {
		body := `<script>location.href="https://oral.fifedu.com/kyxl-web/student/index.do"</script>`
		return newResponse(200, body, nil), nil
	})
	_, err := c.landingToToken(context.Background(), newCookieJar(nil, nil),
		"https://oral.fifedu.com/kyxl-web/student/index.do")
	if !errors.Is(err, entity.ErrPlatformChanged) {
		t.Fatalf("业务页脚本跳转不应跟随，得到 %v", err)
	}
	if got := ft.Count("kyxl-web"); got != 1 {
		t.Fatalf("业务页脚本不应触发第二次请求：%d 次", got)
	}
}

func TestLandingReadsTokenFromBusinessScriptWithoutFollowing(t *testing.T) {
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	jwt := jwtFor("user-1", now.Add(time.Hour).Unix())
	c, ft := newTestClient(t, now, func(r *http.Request) (*http.Response, error) {
		body := `<script>location.href="https://static.fifedu.com/x#/?token=` + jwt + `"</script>`
		return newResponse(200, body, nil), nil
	})
	token, err := c.landingToToken(context.Background(), newCookieJar(nil, nil),
		"https://oral.fifedu.com/kyxl-web/student/index.do")
	if err != nil {
		t.Fatalf("业务页脚本里带 token 时应直接取用: %v", err)
	}
	if token.Value != jwt {
		t.Fatalf("token 错误: %#v", token)
	}
	if got := ft.Count("static.fifedu.com"); got != 0 {
		t.Fatalf("不应真的请求脚本目标：%d 次", got)
	}
}

func TestTokenFromURLNoFragment(t *testing.T) {
	token, err := tokenFromURL("https://static.fifedu.com/x/index.html")
	if err != nil || token != nil {
		t.Fatalf("没有 fragment 时应返回 (nil,nil)，得到 (%#v,%v)", token, err)
	}
}

func TestCASLoginFailure(t *testing.T) {
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	c, _ := newTestClient(t, now, func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("get-lt") == "true" {
			return newResponse(200, `jsonpcallback({"lt":"LT-1","execution":"e2s1"})`, nil), nil
		}
		return newResponse(200, `logincallback({"login":"fail","token":"","msg":"bad"})`, nil), nil
	})
	page := &callbackPage{
		Username:   "secret-user",
		Password:   "secret-pass",
		NextPage:   "https://oral.fifedu.com/x",
		SSOService: "https://www.fifedu.com/iplat/ssoservice",
		CASServer:  "https://cycore.fifedu.com/cas-server",
	}
	_, err := c.casLogin(context.Background(), newCookieJar(nil, nil), page)
	if !errors.Is(err, entity.ErrAuthFailed) {
		t.Fatalf("CAS 登录失败应报 ErrAuthFailed，得到 %v", err)
	}
	if err != nil && (strings.Contains(err.Error(), "secret-user") || strings.Contains(err.Error(), "secret-pass")) {
		t.Fatalf("错误串泄露凭据: %v", err)
	}
}
