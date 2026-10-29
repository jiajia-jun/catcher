package pigai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"catcher/internal/domain/entity"
	"catcher/internal/infra/sender"
)

const testUUID = "TESTUUID123"

// fakePigai 是一个可编排的批改网 + 微信扫码服务端
type fakePigai struct {
	mu sync.Mutex
	// errcodes 按顺序返回给长轮询；用完后一直返回最后一个
	errcodes []int
	wxCodes  []string
	// lasts 记录每次长轮询收到的 last 参数
	lasts []string
	// loginRedirect 覆盖换号首跳，空则走正常链路
	loginRedirect string
	// writeBody 是 c=write 返回的 JSON
	writeBody string
	image     []byte
}

func (f *fakePigai) next() (int, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.errcodes) == 0 {
		return wxErrcodeWaiting, ""
	}
	errcode := f.errcodes[0]
	if len(f.errcodes) > 1 {
		f.errcodes = f.errcodes[1:]
	}
	code := ""
	if len(f.wxCodes) > 0 {
		code = f.wxCodes[0]
		if len(f.wxCodes) > 1 {
			f.wxCodes = f.wxCodes[1:]
		}
	}
	return errcode, code
}

func (f *fakePigai) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/d3/weixin/wxlogin.html", func(w http.ResponseWriter, _ *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "PHPSESSID", Value: "site-session"})
		_, _ = w.Write([]byte("<html>扫码登录</html>"))
	})
	mux.HandleFunc("/connect/qrconnect", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<script>var img = "https://open.weixin.qq.com/connect/qrcode/` + testUUID + `?uuid=` + testUUID + `";</script>`))
	})
	mux.HandleFunc("/connect/qrcode/", func(w http.ResponseWriter, _ *http.Request) {
		f.mu.Lock()
		image := f.image
		f.mu.Unlock()
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(image)
	})
	mux.HandleFunc("/connect/l/qrconnect", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.lasts = append(f.lasts, r.URL.Query().Get("last"))
		f.mu.Unlock()
		errcode, code := f.next()
		w.Header().Set("Content-Type", "text/javascript")
		_, _ = w.Write([]byte("window.wx_errcode=" + itoa(errcode) + ";window.wx_code='" + code + "';"))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/d3/weixin/login.php":
			f.mu.Lock()
			override := f.loginRedirect
			f.mu.Unlock()
			if override != "" {
				w.Header().Set("Location", override)
				w.WriteHeader(http.StatusFound)
				return
			}
			http.SetCookie(w, &http.Cookie{Name: "wx_entry", Value: "1"})
			w.Header().Set("Location", "/?a=oauth")
			w.WriteHeader(http.StatusFound)
		case r.URL.Path == "/" && r.URL.Query().Get("a") == "oauth":
			http.SetCookie(w, &http.Cookie{Name: "_JUKU_USER", Value: "%7B%22i%22%3A1%7D"})
			w.Header().Set("Location", "/?f2=login")
			w.WriteHeader(http.StatusFound)
		case r.URL.Path == "/" && r.URL.Query().Get("f2") == "login":
			http.SetCookie(w, &http.Cookie{Name: "old", Value: "0"})
			_, _ = w.Write([]byte("<html>已登录</html>"))
		case r.URL.Path == "/index.php":
			f.mu.Lock()
			body := f.writeBody
			f.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		default:
			http.NotFound(w, r)
		}
	})
	return mux
}

func itoa(v int) string {
	switch v {
	case 0:
		return "0"
	}
	var buf [8]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

func newTestClient(t *testing.T, f *fakePigai) *Client {
	t.Helper()
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)
	return New(Options{
		BaseURL:         srv.URL,
		WeixinBaseURL:   srv.URL,
		LongPollBaseURL: srv.URL,
		Sender:          sender.New(sender.Config{}),
		Now:             func() time.Time { return time.Unix(1761700000, 0) },
	})
}

func newFake() *fakePigai {
	return &fakePigai{
		image:     []byte{0xff, 0xd8, 0xff, 0xe0, 'j', 'p', 'g'},
		writeBody: `{"error":0,"data":{"_u":{"user_id":"9001"}}}`,
	}
}

func TestBeginQR(t *testing.T) {
	c := newTestClient(t, newFake())

	challenge, err := c.BeginQR(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if challenge.ID != testUUID {
		t.Fatalf("ID = %q, want %q", challenge.ID, testUUID)
	}
	if !strings.HasPrefix(challenge.Content, "data:image/jpeg;base64,") {
		t.Fatalf("Content 前缀不对: %.40q", challenge.Content)
	}
	if want := time.Unix(1761700000, 0).Add(qrDefaultTTL); !challenge.ExpiresAt.Equal(want) {
		t.Fatalf("ExpiresAt = %v, want %v", challenge.ExpiresAt, want)
	}
}

// 微信域名下发的 cookie 不能进持久化会话，只有站点自己的 cookie 才保存
func TestFinishDropsWeixinCookies(t *testing.T) {
	c := newTestClient(t, newFake())

	challenge, err := c.BeginQR(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	qs, _ := c.lookupQR(challenge.ID)

	// 微信域名的 cookie 被写进临时袋，站点袋不应出现
	qs.wxJar.add([]string{"wx_cookie=1"})
	if qs.jar.get("wx_cookie") != "" {
		t.Fatal("微信 cookie 不应进入站点 cookie 袋")
	}
}

func TestBeginQRRejectsNonImage(t *testing.T) {
	f := newFake()
	f.image = []byte("<html>被拦截</html>")
	c := newTestClient(t, f)

	_, err := c.BeginQR(context.Background())
	if !errors.Is(err, entity.ErrPlatformChanged) {
		t.Fatalf("= %v, want ErrPlatformChanged", err)
	}
}

func TestPollQRStates(t *testing.T) {
	cases := []struct {
		name     string
		errcode  int
		wxCode   string
		want     entity.QRState
		wantErr  error
		wantLast string
	}{
		{name: "未扫码", errcode: wxErrcodeWaiting, want: entity.QRWaiting},
		{name: "已扫待确认", errcode: wxErrcodeScanned, want: entity.QRScanned, wantLast: "404"},
		{name: "二维码过期", errcode: wxErrcodeExpired, want: entity.QRExpired},
		{name: "手机上取消", errcode: wxErrcodeCanceled, want: entity.QRExpired},
		{name: "微信侧异常", errcode: wxErrcodeServer, want: entity.QRExpired},
		{name: "已确认但没有授权码", errcode: wxErrcodeConfirmed, want: entity.QRFailed, wantErr: entity.ErrAuthFailed},
		{name: "未知状态", errcode: 418, want: entity.QRFailed, wantErr: entity.ErrPlatformChanged},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFake()
			f.errcodes = []int{tc.errcode}
			f.wxCodes = []string{tc.wxCode}
			c := newTestClient(t, f)

			challenge, err := c.BeginQR(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			state, session, err := c.PollQR(context.Background(), challenge)
			if state != tc.want {
				t.Fatalf("state = %s, want %s", state, tc.want)
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr == nil && err != nil {
				t.Fatalf("不应报错: %v", err)
			}
			if session != nil {
				t.Fatal("未成功时不应返回会话")
			}
			if tc.wantLast != "" {
				if len(f.lasts) == 0 || f.lasts[0] != "" {
					t.Fatalf("第一次轮询不应带 last: %v", f.lasts)
				}
			}
		})
	}
}

// 404 之后必须带 last=404，否则微信侧会重复返回同一状态
func TestPollQRPassesLastAfterScanned(t *testing.T) {
	f := newFake()
	f.errcodes = []int{wxErrcodeScanned, wxErrcodeWaiting, wxErrcodeScanned}
	c := newTestClient(t, f)

	challenge, err := c.BeginQR(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, _, err := c.PollQR(context.Background(), challenge); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"", "404", ""}
	if len(f.lasts) != len(want) {
		t.Fatalf("轮询次数 = %d, want %d", len(f.lasts), len(want))
	}
	for i := range want {
		if f.lasts[i] != want[i] {
			t.Fatalf("第 %d 次 last = %q, want %q", i+1, f.lasts[i], want[i])
		}
	}
}

func TestPollQRSucceedsAndBuildsSession(t *testing.T) {
	f := newFake()
	f.errcodes = []int{wxErrcodeConfirmed}
	f.wxCodes = []string{"WXCODE"}
	c := newTestClient(t, f)

	challenge, err := c.BeginQR(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	state, session, err := c.PollQR(context.Background(), challenge)
	if err != nil {
		t.Fatal(err)
	}
	if state != entity.QRSucceeded || session == nil {
		t.Fatalf("state = %s, session = %v", state, session)
	}
	if session.Platform != entity.PlatformPigai {
		t.Fatalf("platform = %s", session.Platform)
	}

	var secret entity.PigaiSecret
	if err := json.Unmarshal(session.Secret, &secret); err != nil {
		t.Fatal(err)
	}
	names := map[string]string{}
	for _, cookie := range secret.Cookies {
		names[cookie.Name] = cookie.Value
	}
	// 换号链路每一跳的 Set-Cookie 都要收齐
	for _, name := range []string{"PHPSESSID", "wx_entry", "_JUKU_USER", "old"} {
		if names[name] == "" {
			t.Fatalf("会话缺少 cookie %s: %v", name, names)
		}
	}
	if _, ok := names["wx_cookie"]; ok {
		t.Fatal("不应带上微信域名的 cookie")
	}
}

// 换号链路跳到站外说明链路异常，不能把 cookie 带过去
func TestFinishRejectsForeignRedirect(t *testing.T) {
	f := newFake()
	f.errcodes = []int{wxErrcodeConfirmed}
	f.wxCodes = []string{"WXCODE"}
	f.loginRedirect = "https://evil.example.com/steal"
	c := newTestClient(t, f)

	challenge, err := c.BeginQR(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	state, _, err := c.PollQR(context.Background(), challenge)
	if state != entity.QRFailed {
		t.Fatalf("state = %s", state)
	}
	if !errors.Is(err, entity.ErrPlatformChanged) {
		t.Fatalf("= %v, want ErrPlatformChanged", err)
	}
}

// 换号后 c=write 仍返回 error=302，说明没有真正登录
func TestFinishFailsWhenVerifyNotLoggedIn(t *testing.T) {
	f := newFake()
	f.errcodes = []int{wxErrcodeConfirmed}
	f.wxCodes = []string{"WXCODE"}
	f.writeBody = `{"error":302,"error_des":"请先登录"}`
	c := newTestClient(t, f)

	challenge, err := c.BeginQR(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	state, _, err := c.PollQR(context.Background(), challenge)
	if state != entity.QRFailed || !errors.Is(err, entity.ErrAuthFailed) {
		t.Fatalf("state = %s, err = %v", state, err)
	}
}

func TestPollQRUnknownChallenge(t *testing.T) {
	c := newTestClient(t, newFake())
	state, _, err := c.PollQR(context.Background(), &entity.QRChallenge{ID: "missing"})
	if state != entity.QRExpired || err != nil {
		t.Fatalf("state = %s, err = %v", state, err)
	}
}

func TestParseQRResponse(t *testing.T) {
	cases := []struct {
		raw     string
		errcode int
		code    string
		ok      bool
	}{
		{raw: "window.wx_errcode=408;window.wx_code='';", errcode: 408, ok: true},
		{raw: "window.wx_errcode=405;window.wx_code='ABC123';", errcode: 405, code: "ABC123", ok: true},
		{raw: "<html>登录页</html>"},
	}
	for _, tc := range cases {
		errcode, code, ok := parseQRResponse([]byte(tc.raw))
		if ok != tc.ok || errcode != tc.errcode || code != tc.code {
			t.Fatalf("%q → (%d,%q,%v), want (%d,%q,%v)",
				tc.raw, errcode, code, ok, tc.errcode, tc.code, tc.ok)
		}
	}
}

func TestExtractUUID(t *testing.T) {
	cases := map[string]string{
		`<img src="https://open.weixin.qq.com/connect/qrcode/ABC123">`: "ABC123",
		`<script>var u = "?uuid=XYZ789";</script>`:                    "XYZ789",
		`<html>nothing</html>`:                                        "",
	}
	for html, want := range cases {
		if got := extractUUID(html); got != want {
			t.Fatalf("extractUUID(%q) = %q, want %q", html, got, want)
		}
	}
}
