package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"catcher/internal/domain"
	"catcher/internal/domain/entity"
	"catcher/pkg/logger"
)

type fakeAuthenticator struct {
	mu        sync.Mutex
	session   *entity.Session
	err       error
	calls     int
	lastCreds entity.PasswordCredential
}

func (f *fakeAuthenticator) LoginWithPassword(_ context.Context, creds entity.PasswordCredential) (*entity.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.lastCreds = creds
	return f.session, f.err
}

func (f *fakeAuthenticator) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type fakeCredentials struct {
	mu      sync.Mutex
	creds   *entity.PasswordCredential
	getErr  error
	saveErr error
	gets    int
	saves   int
	deletes int
	savedAt entity.PasswordCredential
}

func (f *fakeCredentials) Get(context.Context, int64) (*entity.PasswordCredential, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gets++
	if f.getErr != nil {
		return nil, f.getErr
	}
	if f.creds == nil {
		return nil, entity.ErrNotFound
	}
	creds := *f.creds
	return &creds, nil
}

func (f *fakeCredentials) Save(_ context.Context, _ int64, creds entity.PasswordCredential, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.saveErr != nil {
		return f.saveErr
	}
	f.saves++
	f.savedAt = creds
	f.creds = &creds
	return nil
}

func (f *fakeCredentials) Delete(context.Context, int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deletes++
	f.creds = nil
	return nil
}

type cxHarness struct {
	svc         *Service
	accounts    *fakeAccounts
	sessions    *fakeSessions
	credentials *fakeCredentials
	assignments *fakeAssignments
	runs        *fakeRuns
	fetcher     *fakeFetcher
	auth        *fakeAuthenticator
	events      chan string
}

func newCXHarness(t *testing.T) *cxHarness {
	t.Helper()
	h := &cxHarness{
		accounts:    newFakeAccounts(),
		sessions:    &fakeSessions{secret: []byte(`{"cookies":[{"name":"JSESSIONID","value":"sess-old"}]}`)},
		credentials: &fakeCredentials{},
		assignments: &fakeAssignments{},
		runs:        &fakeRuns{},
		fetcher:     &fakeFetcher{},
		auth:        &fakeAuthenticator{},
		events:      make(chan string, 64),
	}
	h.svc = New(Deps{
		Accounts:    h.accounts,
		Sessions:    h.sessions,
		Credentials: h.credentials,
		Assignments: h.assignments,
		Runs:        h.runs,
		Fetchers:    map[entity.Platform]domainFetcher{entity.PlatformChaoxing: h.fetcher},
		Authenticators: map[entity.Platform]domain.PasswordAuthenticator{
			entity.PlatformChaoxing: h.auth,
		},
		Logger: logger.Discard(),
		Now:    func() time.Time { return time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC) },
	})
	h.svc.SetEmitter(func(name string, _ any) {
		select {
		case h.events <- name:
		default:
		}
	})
	return h
}

func (h *cxHarness) waitFinished(t *testing.T) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case name := <-h.events:
			if name == "refresh.all-finished" {
				return
			}
		case <-deadline:
			t.Fatal("等待刷新结束超时")
		}
	}
}

func TestChaoxingLoginRemembersPassword(t *testing.T) {
	h := newCXHarness(t)
	secret := []byte(`{"cookies":[{"name":"JSESSIONID","value":"sess-1"}]}`)
	h.auth.session = &entity.Session{Platform: entity.PlatformChaoxing, Secret: secret}

	creds := entity.PasswordCredential{Phone: " 13800138000 ", Password: "pw"}
	if err := h.svc.LoginChaoxing(context.Background(), creds, true); err != nil {
		t.Fatal(err)
	}

	if h.auth.callCount() != 1 {
		t.Fatalf("登录调用 = %d", h.auth.callCount())
	}
	if h.auth.lastCreds.Phone != "13800138000" {
		t.Fatalf("手机号未去空白: %q", h.auth.lastCreds.Phone)
	}
	if string(h.sessions.secret) != string(secret) {
		t.Fatal("登录成功后的会话未持久化")
	}
	if h.credentials.saves != 1 || h.credentials.savedAt.Password != "pw" {
		t.Fatalf("凭据未保存: %#v", h.credentials)
	}
	if got := h.accounts.names[entity.PlatformChaoxing]; got != "138****8000" {
		t.Fatalf("显示名 = %q（必须打码）", got)
	}
	if h.accounts.state(entity.PlatformChaoxing) != entity.LoginConnected {
		t.Fatalf("登录态 = %s", h.accounts.state(entity.PlatformChaoxing))
	}
}

func TestChaoxingLoginWithoutRememberDeletesOldPassword(t *testing.T) {
	h := newCXHarness(t)
	h.credentials.creds = &entity.PasswordCredential{Phone: "13800138000", Password: "old"}
	h.auth.session = &entity.Session{Platform: entity.PlatformChaoxing, Secret: []byte(`{}`)}

	if err := h.svc.LoginChaoxing(context.Background(),
		entity.PasswordCredential{Phone: "13800138000", Password: "new"}, false); err != nil {
		t.Fatal(err)
	}

	if h.credentials.deletes != 1 {
		t.Fatalf("取消记住密码时必须删除旧密码，deletes = %d", h.credentials.deletes)
	}
	if h.credentials.saves != 0 {
		t.Fatalf("不应保存密码，saves = %d", h.credentials.saves)
	}
}

func TestChaoxingLoginFailureKeepsState(t *testing.T) {
	h := newCXHarness(t)
	h.auth.err = entity.ErrAuthFailed

	err := h.svc.LoginChaoxing(context.Background(),
		entity.PasswordCredential{Phone: "13800138000", Password: "wrong"}, true)
	if !errors.Is(err, entity.ErrAuthFailed) {
		t.Fatalf("err = %v", err)
	}
	if h.sessions.saves != 0 || h.credentials.saves != 0 {
		t.Fatal("登录失败不应写入会话或密码")
	}
	if h.accounts.state(entity.PlatformChaoxing) == entity.LoginConnected {
		t.Fatal("登录失败不能标记为已连接")
	}
}

func TestRefreshChaoxingReloginWithSavedPassword(t *testing.T) {
	h := newCXHarness(t)
	h.fetcher.steps = []fetchStep{
		{err: entity.ErrAuthExpired},
		{result: &entity.FetchResult{Complete: true}},
	}
	h.credentials.creds = &entity.PasswordCredential{Phone: "13800138000", Password: "pw"}
	renewed := []byte(`{"cookies":[{"name":"JSESSIONID","value":"sess-2"}]}`)
	h.auth.session = &entity.Session{Platform: entity.PlatformChaoxing, Secret: renewed}

	if _, err := h.svc.StartRefresh(context.Background(), []entity.Platform{entity.PlatformChaoxing}); err != nil {
		t.Fatal(err)
	}
	h.waitFinished(t)

	if h.credentials.gets != 1 || h.auth.callCount() != 1 {
		t.Fatalf("凭据读取 = %d, 重登 = %d（应各一次）", h.credentials.gets, h.auth.callCount())
	}
	if h.fetcher.callCount() != 2 {
		t.Fatalf("抓取调用 = %d, want 2（失效后重试一次）", h.fetcher.callCount())
	}
	if string(h.sessions.secret) != string(renewed) {
		t.Fatal("重登后的会话未持久化")
	}
	run, ok := h.runs.last()
	if !ok || run.Status != entity.RunSuccess {
		t.Fatalf("刷新终态 = %#v", run)
	}
}

func TestRefreshChaoxingWithoutSavedPasswordFails(t *testing.T) {
	h := newCXHarness(t)
	h.fetcher.steps = []fetchStep{{err: entity.ErrAuthExpired}}
	h.credentials.getErr = entity.ErrNotFound

	if _, err := h.svc.StartRefresh(context.Background(), []entity.Platform{entity.PlatformChaoxing}); err != nil {
		t.Fatal(err)
	}
	h.waitFinished(t)

	if h.auth.callCount() != 0 {
		t.Fatal("没有密码时不应尝试重登")
	}
	if snaps := h.assignments.snapshots(); len(snaps) != 0 {
		t.Fatalf("重登失败不得写入/软删除: %#v", snaps)
	}
	run, ok := h.runs.last()
	if !ok || run.Status != entity.RunFailed {
		t.Fatalf("刷新终态 = %#v", run)
	}
	if h.accounts.state(entity.PlatformChaoxing) != entity.LoginNeedsLogin {
		t.Fatalf("登录态 = %s", h.accounts.state(entity.PlatformChaoxing))
	}
}
