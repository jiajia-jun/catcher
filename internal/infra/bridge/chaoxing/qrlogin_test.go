package chaoxing

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"catcher/internal/domain/entity"
)

const (
	qrTestUUID = "0123456789abcdef0123456789abcdef"
	qrTestEnc  = "fedcba9876543210fedcba9876543210"
)

// qrTestPNG 只需要文件头，用例不解码图片
var qrTestPNG = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x01\x0e")

// qrTestServer 同时扮演 passport2 与 mooc1-api。
// mode 是 /getauthstatus 原样返回的正文，各用例按需切换
type qrTestServer struct {
	*httptest.Server
	mode       string
	statusHits int
}

func newQRTestServer(t *testing.T) *qrTestServer {
	t.Helper()
	s := &qrTestServer{mode: `{"mes":"未登录","type":"3","status":false}`}
	mux := http.NewServeMux()

	mux.HandleFunc(qrLoginPath, func(w http.ResponseWriter, r *http.Request) {
		// 学习通的扫码页只认 GET，POST 会 302 到 passport404
		if r.Method != http.MethodGet {
			t.Errorf("扫码页方法 = %s, want GET", r.Method)
		}
		q := r.URL.Query()
		if q.Get("mobiletip") == "" || q.Get("time") == "" || q.Get("pcrefer") == "" {
			t.Errorf("扫码页查询参数不全: %s", r.URL.RawQuery)
		}
		http.SetCookie(w, &http.Cookie{Name: "JSESSIONID", Value: "qr-sess", Path: "/", HttpOnly: true})
		// 故意把 enc 的 value 写在 id 前面，覆盖两种属性顺序
		_, _ = io.WriteString(w, `<html><body>
<input type="hidden" id="uuid" value="`+qrTestUUID+`"/>
<input type="hidden" value="`+qrTestEnc+`" id="enc"/>
<input id="pcrefer" value="http://`+r.Host+`/landing" type="hidden"/>
</body></html>`)
	})

	mux.HandleFunc(qrImagePath, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("uuid"); got != qrTestUUID {
			t.Errorf("取图 uuid = %q", got)
		}
		if got := r.Header.Get("Cookie"); !strings.Contains(got, "JSESSIONID=qr-sess") {
			t.Errorf("取图没有带扫码页的 cookie: %q", got)
		}
		// 真实接口声明 image/jpeg，实际返回 PNG
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(qrTestPNG)
	})

	mux.HandleFunc(qrStatusPath, func(w http.ResponseWriter, r *http.Request) {
		s.statusHits++
		if r.Method != http.MethodPost {
			t.Errorf("轮询方法 = %s, want POST", r.Method)
		}
		if got := r.Header.Get("X-Requested-With"); got != "XMLHttpRequest" {
			t.Errorf("轮询缺少 X-Requested-With: %q", got)
		}
		if got := r.Header.Get("Cookie"); !strings.Contains(got, "JSESSIONID=qr-sess") {
			t.Errorf("轮询没有带扫码页的 cookie: %q", got)
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("解析轮询表单失败: %v", err)
		}
		if r.PostForm.Get("uuid") != qrTestUUID || r.PostForm.Get("enc") != qrTestEnc {
			t.Errorf("轮询表单 = %v", r.PostForm)
		}
		w.Header().Set("Content-Type", "text/html;charset=utf-8")
		_, _ = io.WriteString(w, s.mode)
	})

	// 扫码成功后跟随 pcrefer 才能拿到 UID 等 cookie
	mux.HandleFunc("/landing", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "UID", Value: "12345", Path: "/"})
		http.Redirect(w, r, "/home", http.StatusFound)
	})
	mux.HandleFunc("/home", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "_uid", Value: "12345", Path: "/"})
		_, _ = io.WriteString(w, "<html>个人空间</html>")
	})

	mux.HandleFunc("/mycourse/backclazzdata", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"result":1,"channelList":[]}`)
	})

	s.Server = httptest.NewServer(mux)
	return s
}

func (s *qrTestServer) close() {
	s.Server.Close()
}

func mustBeginQR(t *testing.T, client *Client) *entity.QRChallenge {
	t.Helper()
	challenge, err := client.BeginQR(context.Background())
	if err != nil {
		t.Fatalf("BeginQR: %v", err)
	}
	return challenge
}

func TestBeginQRReturnsImageDataURL(t *testing.T) {
	srv := newQRTestServer(t)
	defer srv.close()

	challenge := mustBeginQR(t, newTestClient(t, srv.URL))

	const prefix = "data:image/png;base64,"
	if !strings.HasPrefix(challenge.Content, prefix) {
		t.Fatalf("Content = %.40q，应为 PNG data URL", challenge.Content)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(challenge.Content, prefix))
	if err != nil {
		t.Fatalf("解码二维码失败: %v", err)
	}
	if string(raw) != string(qrTestPNG) {
		t.Fatalf("二维码内容 = %q", raw)
	}
	if challenge.ID == "" {
		t.Fatal("二维码 id 为空")
	}
	if challenge.ExpiresAt.IsZero() {
		t.Fatal("二维码没有过期时间")
	}
}

func TestBeginQRFailsWhenPageLacksUUID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `<html><body><input type="hidden" id="uuid" value=""/></body></html>`)
	}))
	defer server.Close()

	_, err := newTestClient(t, server.URL).BeginQR(context.Background())
	if !errors.Is(err, entity.ErrPlatformChanged) {
		t.Fatalf("err = %v, want ErrPlatformChanged", err)
	}
}

func TestBeginQRFailsWhenImageIsNotImage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, qrImagePath) {
			_, _ = io.WriteString(w, `{"error":"uuid 无效"}`)
			return
		}
		_, _ = io.WriteString(w, `<input id="uuid" value="`+qrTestUUID+`"/>
<input id="enc" value="`+qrTestEnc+`"/>`)
	}))
	defer server.Close()

	_, err := newTestClient(t, server.URL).BeginQR(context.Background())
	if !errors.Is(err, entity.ErrPlatformChanged) {
		t.Fatalf("err = %v, want ErrPlatformChanged", err)
	}
}

func TestPollQRStateMapping(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		want    entity.QRState
		wantErr error
	}{
		{"等待扫码", `{"mes":"未登录","type":"3","status":false}`, entity.QRWaiting, nil},
		{"已扫待确认", `{"mes":"","type":"4","status":false,"nickname":"同学","uid":"123"}`, entity.QRScanned, nil},
		{"二维码失效", `{"mes":"会话已失效，请刷新页面重试","type":"2","status":false}`, entity.QRExpired, nil},
		{"用户取消", `{"mes":"","type":"6","status":false}`, entity.QRFailed, entity.ErrAuthFailed},
		{"参数校验失败", `{"mes":"验证失败","type":"1","status":false}`, entity.QRFailed, entity.ErrAuthFailed},
		{"未知类型", `{"mes":"","type":"9","status":false}`, entity.QRFailed, entity.ErrPlatformChanged},
		{"不是 JSON", `<html>系统繁忙</html>`, entity.QRFailed, entity.ErrPlatformChanged},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newQRTestServer(t)
			defer srv.close()
			srv.mode = tc.body

			client := newTestClient(t, srv.URL)
			challenge := mustBeginQR(t, client)
			state, session, err := client.PollQR(context.Background(), challenge)

			if state != tc.want {
				t.Fatalf("state = %s, want %s", state, tc.want)
			}
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
			} else if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if session != nil {
				t.Fatal("未成功时不应返回会话")
			}
			// 服务端 mes 或本地提示要透传给用户
			switch tc.name {
			case "参数校验失败":
				if !strings.Contains(err.Error(), "验证失败") {
					t.Fatalf("err = %v，应包含服务端 mes", err)
				}
			case "用户取消":
				if !strings.Contains(err.Error(), "已在手机上取消登录") {
					t.Fatalf("err = %v，应说明已取消", err)
				}
			}
		})
	}
}

func TestPollQRDropsChallengeAfterTerminalState(t *testing.T) {
	srv := newQRTestServer(t)
	defer srv.close()
	srv.mode = `{"mes":"","type":"6","status":false}`

	client := newTestClient(t, srv.URL)
	challenge := mustBeginQR(t, client)

	if state, _, err := client.PollQR(context.Background(), challenge); state != entity.QRFailed || !errors.Is(err, entity.ErrAuthFailed) {
		t.Fatalf("首次轮询 = %s, %v", state, err)
	}
	hits := srv.statusHits
	state, _, err := client.PollQR(context.Background(), challenge)
	if state != entity.QRExpired || err != nil {
		t.Fatalf("再次轮询 = %s, %v，want expired", state, err)
	}
	if srv.statusHits != hits {
		t.Fatalf("已丢弃的码不应再请求服务端，实际 %d 次", srv.statusHits-hits)
	}
}

func TestPollQRExpiresAfterLimit(t *testing.T) {
	srv := newQRTestServer(t)
	defer srv.close()

	client := newTestClient(t, srv.URL)
	challenge := mustBeginQR(t, client)

	for i := 0; i < qrPollLimit; i++ {
		state, _, err := client.PollQR(context.Background(), challenge)
		if state != entity.QRWaiting || err != nil {
			t.Fatalf("第 %d 次轮询 = %s, %v", i+1, state, err)
		}
	}
	if srv.statusHits != qrPollLimit {
		t.Fatalf("服务端收到 %d 次轮询, want %d", srv.statusHits, qrPollLimit)
	}

	state, _, err := client.PollQR(context.Background(), challenge)
	if state != entity.QRExpired || err != nil {
		t.Fatalf("超过上限后 = %s, %v，want expired", state, err)
	}
	if srv.statusHits != qrPollLimit {
		t.Fatalf("超过上限后不应再请求服务端，实际 %d 次", srv.statusHits)
	}
}

func TestPollQRTreatsServerErrorAsWaiting(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, qrStatusPath) {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if strings.HasPrefix(r.URL.Path, qrImagePath) {
			_, _ = w.Write(qrTestPNG)
			return
		}
		_, _ = io.WriteString(w, `<input id="uuid" value="`+qrTestUUID+`"/>
<input id="enc" value="`+qrTestEnc+`"/>`)
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	challenge := mustBeginQR(t, client)

	state, session, err := client.PollQR(context.Background(), challenge)
	if state != entity.QRWaiting || err != nil || session != nil {
		t.Fatalf("504/5xx 应继续等: state=%s err=%v", state, err)
	}
}

func TestPollQRSucceedsAndCollectsCookies(t *testing.T) {
	srv := newQRTestServer(t)
	defer srv.close()
	srv.mode = `{"mes":"","type":"4","status":true,"nickname":"同学"}`

	client := newTestClient(t, srv.URL)
	challenge := mustBeginQR(t, client)

	state, session, err := client.PollQR(context.Background(), challenge)
	if err != nil {
		t.Fatalf("PollQR: %v", err)
	}
	if state != entity.QRSucceeded {
		t.Fatalf("state = %s, want succeeded", state)
	}
	if session == nil {
		t.Fatal("成功时必须返回会话")
	}
	if session.Platform != entity.PlatformChaoxing {
		t.Fatalf("session.Platform = %s", session.Platform)
	}

	var secret entity.ChaoxingSecret
	if err := json.Unmarshal(session.Secret, &secret); err != nil {
		t.Fatalf("解析会话失败: %v", err)
	}
	cookies := map[string]string{}
	for _, cookie := range secret.Cookies {
		cookies[cookie.Name] = cookie.Value
	}
	// JSESSIONID 来自扫码页，UID 来自 pcrefer 落地页，两者都要留下
	if cookies["JSESSIONID"] != "qr-sess" || cookies["UID"] != "12345" || cookies["_uid"] != "12345" {
		t.Fatalf("cookie 袋 = %#v", cookies)
	}
}

func TestPollQRRejectsEmptyChallenge(t *testing.T) {
	state, _, err := newTestClient(t, "http://127.0.0.1:0").PollQR(context.Background(), nil)
	if state != entity.QRFailed || !errors.Is(err, entity.ErrAuthFailed) {
		t.Fatalf("state=%s err=%v", state, err)
	}
}
