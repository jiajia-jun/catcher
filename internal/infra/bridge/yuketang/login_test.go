package yuketang

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"catcher/internal/domain/entity"
	"catcher/internal/infra/sender"
	"catcher/pkg/logger"
)

func newTestClient(t *testing.T, handler http.Handler, pollTimeout time.Duration) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return New(Options{
		BaseURL: srv.URL,
		Sender: sender.New(sender.Config{
			Transport: srv.Client().Transport,
			Logger:    logger.Discard(),
		}),
		Logger:      logger.Discard(),
		PollTimeout: pollTimeout,
	})
}

func TestBeginQR(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != preInfoPath {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, `{"code":0,"msg":"","data":{"qrContent":"https://example/qr?u=abc","qrImage":"","token":"tok-1"}}`)
	}), 0)

	challenge, err := client.BeginQR(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if challenge.ID != "tok-1" {
		t.Fatalf("challenge.ID = %q，应为 token", challenge.ID)
	}
	if !strings.Contains(challenge.Content, "https://example/qr") {
		t.Fatalf("qrContent = %q", challenge.Content)
	}
	if challenge.ExpiresAt.Before(time.Now().Add(4 * time.Minute)) {
		t.Fatalf("二维码有效期异常: %v", challenge.ExpiresAt)
	}
}

func TestPollQRWaitingThenSucceeded(t *testing.T) {
	var polls int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case pollPath:
			n := atomic.AddInt32(&polls, 1)
			if n == 1 {
				// 未扫码：nginx 504 HTML，不是错误
				w.WriteHeader(http.StatusGatewayTimeout)
				_, _ = io.WriteString(w, "<html><title>504 Gateway Time-out</title></html>")
				return
			}
			_, _ = io.WriteString(w, `{"code":0,"msg":"","data":{"UserID":123456789,"Auth":"auth-uuid"}}`)
		case webLoginPath:
			if ct := r.Header.Get("Content-Type"); ct != contentFormURLEncoded {
				t.Errorf("③ Content-Type = %q", ct)
			}
			body, _ := io.ReadAll(r.Body)
			var payload struct {
				UserID json.RawMessage `json:"UserID"`
				Auth   string          `json:"Auth"`
			}
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Errorf("③ body 不是 JSON: %v (%s)", err, body)
			}
			if string(payload.UserID) != "123456789" {
				t.Errorf("③ UserID 字面量 = %s", payload.UserID)
			}
			http.SetCookie(w, &http.Cookie{Name: "sessionid", Value: "sid-1", Path: "/"})
			http.SetCookie(w, &http.Cookie{Name: "csrftoken", Value: "csrf", Path: "/"})
			_, _ = io.WriteString(w, `{"data":{},"success":true,"user_id":123456789,"user_role":-1}`)
		default:
			http.NotFound(w, r)
		}
	})
	client := newTestClient(t, handler, 0)
	challenge := &entity.QRChallenge{ID: "tok-1", ExpiresAt: time.Now().Add(5 * time.Minute)}

	state, session, err := client.PollQR(context.Background(), challenge)
	if err != nil {
		t.Fatal(err)
	}
	if state != entity.QRWaiting || session != nil {
		t.Fatalf("首次轮询 = %v, %v", state, session)
	}

	state, session, err = client.PollQR(context.Background(), challenge)
	if err != nil {
		t.Fatal(err)
	}
	if state != entity.QRSucceeded || session == nil {
		t.Fatalf("第二次轮询 = %v, %v", state, session)
	}

	var secret entity.YuketangSecret
	if err := json.Unmarshal(session.Secret, &secret); err != nil {
		t.Fatal(err)
	}
	if secret.SessionID != "sid-1" || secret.Auth != "auth-uuid" || secret.UserID != "123456789" {
		t.Fatalf("secret = %#v", secret)
	}
	if session.Platform != entity.PlatformYuketang {
		t.Fatalf("platform = %s", session.Platform)
	}
}

func TestPollQRTimeoutKeepsWaiting(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(120 * time.Millisecond)
		_, _ = io.WriteString(w, `{"code":0}`)
	}), 40*time.Millisecond)

	state, _, err := client.PollQR(context.Background(), &entity.QRChallenge{ID: "tok"})
	if err != nil {
		t.Fatalf("超时应当继续等，不应报错: %v", err)
	}
	if state != entity.QRWaiting {
		t.Fatalf("state = %v, want waiting", state)
	}
}

func TestPollQRTokenExpired(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"code":50400,"msg":"BAD_REQUEST","data":null}`)
	}), 0)

	state, _, err := client.PollQR(context.Background(), &entity.QRChallenge{ID: "tok"})
	if err != nil {
		t.Fatalf("token 作废应返回 expired 而非错误: %v", err)
	}
	if state != entity.QRExpired {
		t.Fatalf("state = %v, want expired", state)
	}
}

func TestExchangeFailsWithoutSessionCookie(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// ③ 失败返回登录页 HTML，不下发 sessionid
		_, _ = io.WriteString(w, `<html data-status_code="401">login</html>`)
	}), 0)

	_, err := client.exchange(context.Background(), json.Number("1"), "auth")
	if !errors.Is(err, entity.ErrAuthFailed) {
		t.Fatalf("err = %v, want ErrAuthFailed", err)
	}
}

func TestRenewReplaysWebLogin(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != webLoginPath {
			http.NotFound(w, r)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "sessionid", Value: "sid-new", Path: "/"})
		_, _ = io.WriteString(w, `{"success":true}`)
	}), 0)

	secret, _ := json.Marshal(entity.YuketangSecret{SessionID: "old", Auth: "auth", UserID: "42"})
	session, err := client.Renew(context.Background(), entity.Session{Secret: secret})
	if err != nil {
		t.Fatal(err)
	}
	var got entity.YuketangSecret
	if err := json.Unmarshal(session.Secret, &got); err != nil {
		t.Fatal(err)
	}
	if got.SessionID != "sid-new" || got.Auth != "auth" {
		t.Fatalf("续期后的 secret = %#v", got)
	}
}

func TestRenewWithoutAuthRequiresLogin(t *testing.T) {
	client := New(Options{Logger: logger.Discard()})
	secret, _ := json.Marshal(entity.YuketangSecret{SessionID: "old"})
	if _, err := client.Renew(context.Background(), entity.Session{Secret: secret}); !errors.Is(err, entity.ErrAuthRequired) {
		t.Fatalf("err = %v, want ErrAuthRequired", err)
	}
}
