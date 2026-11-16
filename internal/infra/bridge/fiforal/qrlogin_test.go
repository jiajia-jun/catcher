package fiforal

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"catcher/internal/domain/entity"
)

// fullLoginHandler 模拟从取码到验证登录态的整条链路
func fullLoginHandler(t *testing.T, now time.Time) func(*http.Request) (*http.Response, error) {
	t.Helper()
	polls := 0
	// 二维码图片只要求文件头像 JPEG
	image := "\xff\xd8\xff\xe0fake-jpeg"
	jwt := jwtFor("user-1", now.Add(2*time.Hour).Unix())

	return func(r *http.Request) (*http.Response, error) {
		u := r.URL
		switch {
		case u.Host == "www.fifedu.com" && strings.HasPrefix(u.Path, "/iplat/fifLogin/"):
			return newResponse(200, "<html>login</html>", nil), nil

		case u.Host == "pass.fifedu.com" && u.Path == "/iplat-pass/web/getWebLoginUrl":
			body := `{"status":1,"msg":"","data":{"weixinUrl":"` +
				`https://open.weixin.qq.com/connect/qrconnect?appid=wx&state=iplat#wechat_redirect"}}`
			return newResponse(200, body, nil), nil

		case u.Host == "open.weixin.qq.com" && u.Path == "/connect/qrconnect":
			return newResponse(200, `<img src="https://open.weixin.qq.com/connect/qrcode/UUID-1">`, nil), nil

		case u.Host == "open.weixin.qq.com" && u.Path == "/connect/qrcode/UUID-1":
			return newResponse(200, image, map[string][]string{"Content-Type": {"image/jpeg"}}), nil

		case u.Host == "long.open.weixin.qq.com" && u.Path == "/connect/l/qrconnect":
			if u.Query().Get("last") == "404" {
				return newResponse(200, "window.wx_errcode=405;window.wx_code='WXCODE';", nil), nil
			}
			polls++
			if polls == 1 {
				return newResponse(200, "window.wx_errcode=408;window.wx_code='';", nil), nil
			}
			return newResponse(200, "window.wx_errcode=404;window.wx_code='';", nil), nil

		case u.Host == "pass.fifedu.com" && u.Path == "/iplat-pass/weixin/callback":
			if got := u.Query().Get("code"); got != "WXCODE" {
				t.Errorf("回调页 code = %q, want WXCODE", got)
			}
			if got := u.Query().Get("state"); got != "iplat" {
				t.Errorf("回调页 state = %q, want iplat", got)
			}
			html := `var username = "once-user";
var password = "once-pass";
var nextPage = "https%3A%2F%2Foral.fifedu.com%2Fkyxl-web%2Fstudent%2Findex.do";
new EduSSO("https://www.fifedu.com/iplat/ssoservice", "https://cycore.fifedu.com/cas-server");`
			return newResponse(200, html, nil), nil

		case u.Host == "cycore.fifedu.com" && u.Path == "/cas-server/login" && u.Query().Get("get-lt") == "true":
			return newResponse(200, `jsonpcallback({"lt":"LT-1","execution":"e2s1"})`,
				map[string][]string{"Set-Cookie": {"JSESSIONID=cas; Path=/cas-server"}}), nil

		case u.Host == "cycore.fifedu.com" && u.Path == "/cas-server/login" && u.Query().Get("callback") == "logincallback":
			if got := u.Query().Get("username"); got != "once-user" {
				t.Errorf("CAS username = %q", got)
			}
			if got := u.Query().Get("password"); got != base64.StdEncoding.EncodeToString([]byte("once-pass")) {
				t.Errorf("CAS password 不是 Base64 明文")
			}
			body := `logincallback({"login":"success","token":"prod-token-value","ticket":"ST-1"})`
			return newResponse(200, body, nil), nil

		case u.Host == "www.fifedu.com" && u.Path == "/iplat/ssoservice":
			location := "https://static.fifedu.com/static/fiforal/kyxl-web-static/student-h5/index.html" +
				"#/?type=1&source=10003&token=" + jwt
			return newResponse(302, "", map[string][]string{"Location": {location}}), nil

		case u.Host == "moral.fifedu.com" && u.Path == "/kyxl-app/stu/task/teaTaskList":
			return newResponse(200, taskListBody(), nil), nil
		}
		return nil, fmt.Errorf("unexpected request: %s %s", r.Method, u.String())
	}
}

func TestQRLoginFullFlow(t *testing.T) {
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	c, ft := newTestClient(t, now, fullLoginHandler(t, now))
	ctx := context.Background()

	challenge, err := c.BeginQR(ctx)
	if err != nil {
		t.Fatalf("BeginQR: %v", err)
	}
	if !strings.HasPrefix(challenge.Content, "data:image/jpeg;base64,") {
		t.Fatalf("二维码不是 JPEG data URL: %q", challenge.Content[:40])
	}

	state, session, err := c.PollQR(ctx, challenge)
	if err != nil || state != entity.QRWaiting || session != nil {
		t.Fatalf("第一次轮询 = (%v,%v,%v), want waiting", state, session, err)
	}
	state, session, err = c.PollQR(ctx, challenge)
	if err != nil || state != entity.QRScanned || session != nil {
		t.Fatalf("第二次轮询 = (%v,%v,%v), want scanned", state, session, err)
	}
	state, session, err = c.PollQR(ctx, challenge)
	if err != nil || state != entity.QRSucceeded || session == nil {
		t.Fatalf("第三次轮询 = (%v,%v,%v), want succeeded", state, session, err)
	}

	var secret entity.FiforalSecret
	if err := json.Unmarshal(session.Secret, &secret); err != nil {
		t.Fatalf("解析会话: %v", err)
	}
	if secret.UserID != "user-1" {
		t.Fatalf("会话 user_id = %q", secret.UserID)
	}
	foundProd := false
	for _, cookie := range secret.Cookies {
		if cookie.Name == "prod-token" && cookie.Value == "prod-token-value" &&
			cookie.Domain == "fifedu.com" && !cookie.HostOnly {
			foundProd = true
		}
	}
	if !foundProd {
		t.Fatalf("会话里没有新的 prod-token: %#v", secret.Cookies)
	}

	if c.token == nil || c.token.Value != jwtFor("user-1", now.Add(2*time.Hour).Unix()) {
		t.Fatal("扫码完成后没有缓存 H5 token")
	}
	if got := ft.Count("/connect/l/qrconnect"); got != 3 {
		t.Fatalf("轮询次数 = %d, want 3", got)
	}
}

func TestQRCodesExpire(t *testing.T) {
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	for _, code := range []int{402, 403, 500} {
		code := code
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			c, _ := newTestClient(t, now, func(*http.Request) (*http.Response, error) {
				return newResponse(200, fmt.Sprintf("window.wx_errcode=%d;window.wx_code='';", code), nil), nil
			})
			c.qrSessions["u"] = &qrSession{
				uuid:      "u",
				expiresAt: now.Add(time.Minute),
				jar:       newCookieJar(nil, nil),
				wxJar:     newCookieJar(nil, nil),
			}
			state, session, err := c.PollQR(context.Background(), &entity.QRChallenge{ID: "u"})
			if err != nil || state != entity.QRExpired || session != nil {
				t.Fatalf("%d = (%v,%v,%v), want expired", code, state, session, err)
			}
			if _, ok := c.qrSessions["u"]; ok {
				t.Fatal("失效的二维码应从表里删掉")
			}
		})
	}
}

type fakeTimeoutError struct{}

func (fakeTimeoutError) Error() string   { return "i/o timeout" }
func (fakeTimeoutError) Timeout() bool   { return true }
func (fakeTimeoutError) Temporary() bool { return true }

var _ net.Error = fakeTimeoutError{}

func TestQRPollTreatsTimeoutAnd5xxAsWaiting(t *testing.T) {
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	cases := map[string]func(*http.Request) (*http.Response, error){
		"timeout": func(*http.Request) (*http.Response, error) { return nil, fakeTimeoutError{} },
		"5xx":     func(*http.Request) (*http.Response, error) { return newResponse(502, "", nil), nil },
	}
	for name, handler := range cases {
		handler := handler
		t.Run(name, func(t *testing.T) {
			c, _ := newTestClient(t, now, handler)
			c.qrSessions["u"] = &qrSession{
				uuid:      "u",
				expiresAt: now.Add(time.Minute),
				jar:       newCookieJar(nil, nil),
				wxJar:     newCookieJar(nil, nil),
			}
			state, session, err := c.PollQR(context.Background(), &entity.QRChallenge{ID: "u"})
			if err != nil || state != entity.QRWaiting || session != nil {
				t.Fatalf("transient = (%v,%v,%v), want waiting", state, session, err)
			}
		})
	}
}

func TestQRUnknownErrcodeFails(t *testing.T) {
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	c, _ := newTestClient(t, now, func(*http.Request) (*http.Response, error) {
		return newResponse(200, "window.wx_errcode=999;window.wx_code='';", nil), nil
	})
	c.qrSessions["u"] = &qrSession{
		uuid:      "u",
		expiresAt: now.Add(time.Minute),
		jar:       newCookieJar(nil, nil),
		wxJar:     newCookieJar(nil, nil),
	}
	state, _, err := c.PollQR(context.Background(), &entity.QRChallenge{ID: "u"})
	if state != entity.QRFailed || err == nil || !errorsIsPlatformChanged(err) {
		t.Fatalf("未知状态 = (%v,%v), want failed + ErrPlatformChanged", state, err)
	}
}

func errorsIsPlatformChanged(err error) bool {
	return errors.Is(err, entity.ErrPlatformChanged)
}
