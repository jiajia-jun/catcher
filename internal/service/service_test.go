package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"catcher/internal/domain"
	"catcher/internal/domain/entity"
	"catcher/pkg/logger"
)

type fakeAccounts struct {
	mu     sync.Mutex
	states map[entity.Platform]entity.LoginState
	names  map[entity.Platform]string
}

func newFakeAccounts() *fakeAccounts {
	return &fakeAccounts{
		states: make(map[entity.Platform]entity.LoginState),
		names:  make(map[entity.Platform]string),
	}
}

func (f *fakeAccounts) EnsurePlatforms(context.Context, time.Time) error { return nil }
func (f *fakeAccounts) ListAccounts(context.Context) ([]entity.Account, error) {
	out := make([]entity.Account, 0, len(entity.AllPlatforms()))
	for i, p := range entity.AllPlatforms() {
		out = append(out, entity.Account{ID: int64(i + 1), Platform: p})
	}
	return out, nil
}
func (f *fakeAccounts) GetAccount(_ context.Context, p entity.Platform) (*entity.Account, error) {
	for i, platform := range entity.AllPlatforms() {
		if platform == p {
			return &entity.Account{ID: int64(i + 1), Platform: p, LoginState: f.state(p)}, nil
		}
	}
	return nil, entity.ErrNotFound
}
func (f *fakeAccounts) SetLoginState(_ context.Context, p entity.Platform, s entity.LoginState, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.states[p] = s
	return nil
}
func (f *fakeAccounts) SetDisplayName(_ context.Context, p entity.Platform, name string, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.names[p] = name
	return nil
}
func (f *fakeAccounts) state(p entity.Platform) entity.LoginState {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.states[p]
}

type fakeSessions struct {
	mu      sync.Mutex
	secret  []byte
	getErr  error
	saves   int
	lastRaw []byte
}

func (f *fakeSessions) Get(context.Context, int64) (*entity.Session, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	if f.secret == nil {
		return nil, entity.ErrNotFound
	}
	return &entity.Session{Platform: entity.PlatformYuketang, Secret: f.secret}, nil
}
func (f *fakeSessions) Save(_ context.Context, _ int64, secret []byte, _ *time.Time, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saves++
	f.secret = secret
	f.lastRaw = append([]byte(nil), secret...)
	return nil
}
func (f *fakeSessions) Delete(context.Context, int64) error { return nil }

type fakeAssignments struct {
	mu         sync.Mutex
	snapshot   []entity.PersistInput
	list       []entity.Assignment
	err        error
	lastFilter entity.AssignmentFilter
}

func (f *fakeAssignments) ListAssignments(_ context.Context, filter entity.AssignmentFilter) ([]entity.Assignment, error) {
	f.mu.Lock()
	f.lastFilter = filter
	f.mu.Unlock()
	return f.list, nil
}

func (f *fakeAssignments) filter() entity.AssignmentFilter {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastFilter
}
func (f *fakeAssignments) SaveSnapshot(_ context.Context, in entity.PersistInput) (int, error) {
	if f.err != nil {
		return 0, f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.snapshot = append(f.snapshot, in)
	return len(in.Result.Assignments), nil
}

func (f *fakeAssignments) SetManualStatus(_ context.Context, id int64,
	status *entity.SubmissionStatus, _ time.Time) (entity.Assignment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, item := range f.list {
		if item.ID == id {
			item.ManualStatus = status
			return item, nil
		}
	}
	return entity.Assignment{}, entity.ErrNotFound
}
func (f *fakeAssignments) snapshots() []entity.PersistInput {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]entity.PersistInput(nil), f.snapshot...)
}

type fakeRuns struct {
	mu       sync.Mutex
	created  []*entity.FetchRun
	finished []entity.FetchRun
	issues   []entity.FetchIssue
	latest   map[entity.Platform]entity.FetchRun
}

func (f *fakeRuns) Create(_ context.Context, run *entity.FetchRun) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	run.ID = int64(len(f.created) + 1)
	f.created = append(f.created, run)
	return nil
}
func (f *fakeRuns) Finish(_ context.Context, run entity.FetchRun, issues []entity.FetchIssue, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.finished = append(f.finished, run)
	f.issues = append(f.issues, issues...)
	return nil
}
func (f *fakeRuns) ListIssues(context.Context, int64) ([]entity.FetchIssue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]entity.FetchIssue(nil), f.issues...), nil
}
func (f *fakeRuns) LatestRuns(context.Context) (map[entity.Platform]entity.FetchRun, error) {
	if f.latest == nil {
		return map[entity.Platform]entity.FetchRun{}, nil
	}
	return f.latest, nil
}
func (f *fakeRuns) last() (entity.FetchRun, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.finished) == 0 {
		return entity.FetchRun{}, false
	}
	return f.finished[len(f.finished)-1], true
}

type fetchStep struct {
	result *entity.FetchResult
	err    error
}

type fakeFetcher struct {
	mu        sync.Mutex
	steps     []fetchStep
	calls     int
	blockOn   chan struct{} // 非 nil 时阻塞到 ctx 取消
	startedCh chan struct{}
}

func (f *fakeFetcher) Platform() entity.Platform { return entity.PlatformYuketang }

func (f *fakeFetcher) Fetch(ctx context.Context, _ entity.Account, _ entity.Session,
	opts entity.FetchOptions, progress entity.ProgressFunc) (*entity.FetchResult, error) {
	f.mu.Lock()
	call := f.calls
	f.calls++
	f.mu.Unlock()

	if progress != nil {
		progress(entity.Progress{Platform: entity.PlatformYuketang, Phase: "assignments", Current: 1, Total: 1})
	}
	if f.startedCh != nil {
		select {
		case f.startedCh <- struct{}{}:
		default:
		}
	}
	if f.blockOn != nil {
		<-ctx.Done()
		return nil, entity.ErrCanceled
	}
	if call < len(f.steps) {
		return f.steps[call].result, f.steps[call].err
	}
	if len(f.steps) > 0 {
		last := f.steps[len(f.steps)-1]
		return last.result, last.err
	}
	return &entity.FetchResult{Complete: true}, nil
}

func (f *fakeFetcher) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type fakeRenewer struct {
	session *entity.Session
	err     error
	calls   int
}

func (f *fakeRenewer) Renew(context.Context, entity.Session) (*entity.Session, error) {
	f.calls++
	return f.session, f.err
}

type qrStep struct {
	state   entity.QRState
	session *entity.Session
	err     error
}

type fakeQR struct {
	mu       sync.Mutex
	steps    []qrStep
	calls    int
	beginErr error
}

func (f *fakeQR) BeginQR(context.Context) (*entity.QRChallenge, error) {
	if f.beginErr != nil {
		return nil, f.beginErr
	}
	return &entity.QRChallenge{
		Content:   "qr-content",
		ExpiresAt: time.Date(2026, 10, 22, 12, 5, 0, 0, time.UTC),
	}, nil
}

func (f *fakeQR) PollQR(context.Context, *entity.QRChallenge) (entity.QRState, *entity.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	step := qrStep{state: entity.QRWaiting}
	if f.calls < len(f.steps) {
		step = f.steps[f.calls]
	}
	f.calls++
	return step.state, step.session, step.err
}

type fakeImporter struct {
	session *entity.Session
	err     error
	raw     string
	calls   int
}

func (f *fakeImporter) ImportSession(_ context.Context, raw string) (*entity.Session, error) {
	f.calls++
	f.raw = raw
	return f.session, f.err
}

type harness struct {
	svc         *Service
	accounts    *fakeAccounts
	sessions    *fakeSessions
	assignments *fakeAssignments
	runs        *fakeRuns
	fetcher     *fakeFetcher
	renewer     *fakeRenewer
	qr          *fakeQR
	importer    *fakeImporter
	events      chan string
	eventBody   chan any
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{
		accounts:    newFakeAccounts(),
		sessions:    &fakeSessions{secret: mustSecret(t, "sid-old")},
		assignments: &fakeAssignments{},
		runs:        &fakeRuns{},
		fetcher:     &fakeFetcher{},
		renewer:     &fakeRenewer{},
		qr:          &fakeQR{},
		importer:    &fakeImporter{},
		events:      make(chan string, 64),
		eventBody:   make(chan any, 64),
	}
	h.svc = New(Deps{
		Accounts:    h.accounts,
		Sessions:    h.sessions,
		Assignments: h.assignments,
		Runs:        h.runs,
		Fetchers:    map[entity.Platform]domainFetcher{entity.PlatformYuketang: h.fetcher},
		Renewers:    map[entity.Platform]domainRenewer{entity.PlatformYuketang: h.renewer},
		QRLogin:     h.qr,
		Importer:    h.importer,
		Logger:      logger.Discard(),
		Now:         func() time.Time { return time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC) },
	})
	h.svc.SetEmitter(func(name string, payload any) {
		select {
		case h.events <- name:
		default:
		}
		select {
		case h.eventBody <- payload:
		default:
		}
	})
	return h
}

// 类型别名避免在结构体字面量里写很长的泛型 map
type domainFetcher = domain.Fetcher
type domainRenewer = domain.SessionRenewer

func mustSecret(t *testing.T, sessionID string) []byte {
	t.Helper()
	raw, err := json.Marshal(entity.YuketangSecret{SessionID: sessionID, Auth: "auth", UserID: "42"})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func (h *harness) waitFinished(t *testing.T) {
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

// waitQRState 等待指定扫码状态，返回该状态的消息
func (h *harness) waitQRState(t *testing.T, want entity.QRState) string {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case name := <-h.events:
			var payload any
			select {
			case payload = <-h.eventBody:
			case <-deadline:
				t.Fatalf("等待扫码状态 %s 超时", want)
			}
			if name != "auth.yuketang.state" {
				continue
			}
			m, ok := payload.(map[string]any)
			if !ok {
				t.Fatalf("扫码状态事件类型异常: %T", payload)
			}
			if m["state"] == string(want) {
				msg, _ := m["message"].(string)
				return msg
			}
		case <-deadline:
			t.Fatalf("等待扫码状态 %s 超时", want)
		}
	}
}

func TestYuketangLoginPersistsSession(t *testing.T) {
	h := newHarness(t)
	secret := mustSecret(t, "sid-login")
	h.qr.steps = []qrStep{
		{state: entity.QRWaiting},
		{state: entity.QRSucceeded, session: &entity.Session{Platform: entity.PlatformYuketang, Secret: secret}},
	}

	if _, err := h.svc.StartYuketangLogin(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.waitQRState(t, entity.QRSucceeded)

	if h.qr.calls != 2 {
		t.Fatalf("轮询次数 = %d, want 2", h.qr.calls)
	}
	if string(h.sessions.secret) != string(secret) {
		t.Fatal("登录成功后的会话未持久化")
	}
	if got := h.accounts.names[entity.PlatformYuketang]; got != "42" {
		t.Fatalf("显示名 = %q, want 42", got)
	}
	if h.accounts.state(entity.PlatformYuketang) != entity.LoginConnected {
		t.Fatalf("登录态 = %s", h.accounts.state(entity.PlatformYuketang))
	}
}

func TestYuketangLoginAutoRefreshesExpiredQR(t *testing.T) {
	h := newHarness(t)
	secret := mustSecret(t, "sid-second")
	h.qr.steps = []qrStep{
		{state: entity.QRExpired},
		{state: entity.QRSucceeded, session: &entity.Session{Platform: entity.PlatformYuketang, Secret: secret}},
	}

	if _, err := h.svc.StartYuketangLogin(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.waitQRState(t, entity.QRSucceeded)

	if h.qr.calls != 2 {
		t.Fatalf("轮询次数 = %d, want 2", h.qr.calls)
	}
	if string(h.sessions.secret) != string(secret) {
		t.Fatal("换码登录后会话未持久化")
	}
}

func TestYuketangLoginBeginQRFailure(t *testing.T) {
	h := newHarness(t)
	h.qr.beginErr = errors.New("网络不可用")

	if _, err := h.svc.StartYuketangLogin(context.Background()); err != nil {
		t.Fatal(err)
	}
	msg := h.waitQRState(t, entity.QRFailed)
	if msg != "网络不可用" {
		t.Fatalf("失败消息 = %q", msg)
	}
	if h.sessions.saves != 0 {
		t.Fatal("取码失败不应写入会话")
	}
}

func TestRefreshRenewsOnceThenSucceeds(t *testing.T) {
	h := newHarness(t)
	h.fetcher.steps = []fetchStep{
		{err: entity.ErrAuthExpired},
		{result: &entity.FetchResult{
			Complete:    true,
			Courses:     []entity.Course{{ExternalID: "1", ClassroomID: "2", Name: "课程"}},
			Assignments: []entity.Assignment{{DedupKey: "yuketang:1:2:3", Title: "作业"}},
		}},
	}
	renewed := mustSecret(t, "sid-new")
	h.renewer.session = &entity.Session{Platform: entity.PlatformYuketang, Secret: renewed}

	if _, err := h.svc.StartRefresh(context.Background(), []entity.Platform{entity.PlatformYuketang}); err != nil {
		t.Fatal(err)
	}
	h.waitFinished(t)

	if h.renewer.calls != 1 {
		t.Fatalf("续期调用 = %d, want 1", h.renewer.calls)
	}
	if h.fetcher.callCount() != 2 {
		t.Fatalf("抓取调用 = %d, want 2（失效后重试一次）", h.fetcher.callCount())
	}
	if got := h.sessions.secret; string(got) != string(renewed) {
		t.Fatalf("续期后的会话未持久化: %s", got)
	}
	if snaps := h.assignments.snapshots(); len(snaps) != 1 || len(snaps[0].Result.Assignments) != 1 {
		t.Fatalf("成功结果未落库: %#v", snaps)
	}
	run, ok := h.runs.last()
	if !ok || run.Status != entity.RunSuccess {
		t.Fatalf("刷新终态 = %#v", run)
	}
	if h.accounts.state(entity.PlatformYuketang) != entity.LoginConnected {
		t.Fatalf("登录态 = %s", h.accounts.state(entity.PlatformYuketang))
	}
}

func TestRefreshRenewFailureKeepsOldData(t *testing.T) {
	h := newHarness(t)
	h.fetcher.steps = []fetchStep{{err: entity.ErrAuthExpired}}

	if _, err := h.svc.StartRefresh(context.Background(), []entity.Platform{entity.PlatformYuketang}); err != nil {
		t.Fatal(err)
	}
	h.waitFinished(t)

	if snaps := h.assignments.snapshots(); len(snaps) != 0 {
		t.Fatalf("续期失败不得写入/软删除: %#v", snaps)
	}
	run, ok := h.runs.last()
	if !ok || run.Status != entity.RunFailed {
		t.Fatalf("刷新终态 = %#v", run)
	}
	if h.accounts.state(entity.PlatformYuketang) != entity.LoginNeedsLogin {
		t.Fatalf("登录态 = %s", h.accounts.state(entity.PlatformYuketang))
	}
}

func TestRefreshPartialKeepsOldDataAndRecordsIssues(t *testing.T) {
	h := newHarness(t)
	warning := entity.FetchIssue{Scope: entity.ScopeCourse, Ref: "1", Kind: entity.IssueNetwork, Message: "课程超时"}
	h.fetcher.steps = []fetchStep{{result: &entity.FetchResult{
		Complete:    false,
		Courses:     []entity.Course{{ExternalID: "1", ClassroomID: "2", Name: "课程"}},
		Assignments: []entity.Assignment{{DedupKey: "yuketang:1:2:3", Title: "作业"}},
		Warnings:    []entity.FetchIssue{warning},
	}}}

	if _, err := h.svc.StartRefresh(context.Background(), []entity.Platform{entity.PlatformYuketang}); err != nil {
		t.Fatal(err)
	}
	h.waitFinished(t)

	run, ok := h.runs.last()
	if !ok || run.Status != entity.RunPartial {
		t.Fatalf("刷新终态 = %#v", run)
	}
	if run.WarningCount != 1 {
		t.Fatalf("warning count = %d", run.WarningCount)
	}
	if run.CoursesOK != 0 || run.CoursesTotal != 1 {
		t.Fatalf("课程计数 = %d/%d, want 0/1", run.CoursesOK, run.CoursesTotal)
	}
	h.runs.mu.Lock()
	defer h.runs.mu.Unlock()
	if len(h.runs.issues) != 1 || h.runs.issues[0].Message != "课程超时" {
		t.Fatalf("问题明细 = %#v", h.runs.issues)
	}
}

func TestRefreshCanceled(t *testing.T) {
	h := newHarness(t)
	h.fetcher.blockOn = make(chan struct{})
	h.fetcher.startedCh = make(chan struct{}, 1)

	start, err := h.svc.StartRefresh(context.Background(), []entity.Platform{entity.PlatformYuketang})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-h.fetcher.startedCh:
	case <-time.After(2 * time.Second):
		t.Fatal("抓取未启动")
	}

	// 同一平台重复启动应返回已在刷新中
	again, err := h.svc.StartRefresh(context.Background(), []entity.Platform{entity.PlatformYuketang})
	if err != nil {
		t.Fatal(err)
	}
	if !again.AlreadyRunning {
		t.Fatal("重复刷新应返回 AlreadyRunning")
	}

	if err := h.svc.CancelRefresh(start.JobID); err != nil {
		t.Fatal(err)
	}
	h.waitFinished(t)

	run, ok := h.runs.last()
	if !ok || run.Status != entity.RunCanceled {
		t.Fatalf("取消后的终态 = %#v", run)
	}
	if snaps := h.assignments.snapshots(); len(snaps) != 0 {
		t.Fatalf("取消不应落库: %#v", snaps)
	}
}

func TestRefreshAllSkipsUnimplementedPlatforms(t *testing.T) {
	h := newHarness(t)

	start, err := h.svc.StartRefresh(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	h.waitFinished(t)

	if len(start.Platforms) != 1 || start.Platforms[0] != entity.PlatformYuketang {
		t.Fatalf("全部刷新平台 = %v, want 仅雨课堂", start.Platforms)
	}
	h.runs.mu.Lock()
	defer h.runs.mu.Unlock()
	if len(h.runs.created) != 1 {
		t.Fatalf("刷新记录数 = %d, want 1", len(h.runs.created))
	}
}

func TestRefreshWithoutSessionRequiresLogin(t *testing.T) {
	h := newHarness(t)
	h.sessions.secret = nil

	if _, err := h.svc.StartRefresh(context.Background(), []entity.Platform{entity.PlatformYuketang}); err != nil {
		t.Fatal(err)
	}
	h.waitFinished(t)

	if h.fetcher.callCount() != 0 {
		t.Fatal("没有会话时不应发起抓取")
	}
	if h.accounts.state(entity.PlatformYuketang) != entity.LoginDisconnected {
		t.Fatalf("登录态 = %s", h.accounts.state(entity.PlatformYuketang))
	}
}

func TestBootstrapAggregatesCounts(t *testing.T) {
	h := newHarness(t)
	future := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	past := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	manualSubmitted := entity.StatusSubmitted
	manualUnsubmitted := entity.StatusUnsubmitted
	h.assignments.list = []entity.Assignment{
		{Platform: entity.PlatformYuketang, Status: entity.StatusUnsubmitted, Deadline: &future},
		{Platform: entity.PlatformYuketang, Status: entity.StatusUnsubmitted, Deadline: &past},
		{Platform: entity.PlatformYuketang, Status: entity.StatusUnknown, Deadline: &past},
		{Platform: entity.PlatformYuketang, Status: entity.StatusSubmitted},
		// 手动值优先于平台值：unknown 被手改成已提交、平台已提交被手改成未提交
		{Platform: entity.PlatformYuketang, Status: entity.StatusUnknown,
			ManualStatus: &manualSubmitted, Deadline: &future},
		{Platform: entity.PlatformYuketang, Status: entity.StatusSubmitted,
			ManualStatus: &manualUnsubmitted, Deadline: &past},
	}

	b, err := h.svc.Bootstrap(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	got := b.Counts[entity.PlatformYuketang]
	if got.OpenUnsubmitted != 1 || got.OverdueUnsubmitted != 2 || got.Unknown != 1 || got.Submitted != 2 {
		t.Fatalf("统计 = %#v", got)
	}
	if b.Stats != got {
		t.Fatalf("汇总统计 = %#v", b.Stats)
	}
}

func TestSetAssignmentStatusValidatesAndDelegates(t *testing.T) {
	h := newHarness(t)
	h.assignments.list = []entity.Assignment{
		{ID: 7, Platform: entity.PlatformYuketang, Status: entity.StatusUnknown},
	}

	if _, err := h.svc.SetAssignmentStatus(context.Background(), 0, nil); err == nil {
		t.Fatal("缺少 id 时应当报错")
	}
	invalid := entity.StatusUnknown
	if _, err := h.svc.SetAssignmentStatus(context.Background(), 7, &invalid); err == nil {
		t.Fatal("手动状态不能是 unknown")
	}

	submitted := entity.StatusSubmitted
	item, err := h.svc.SetAssignmentStatus(context.Background(), 7, &submitted)
	if err != nil {
		t.Fatal(err)
	}
	if item.EffectiveStatus() != entity.StatusSubmitted {
		t.Fatalf("有效状态 = %s, want submitted", item.EffectiveStatus())
	}
	if item.Status != entity.StatusUnknown {
		t.Fatalf("平台状态不应被改写: %s", item.Status)
	}

	if _, err := h.svc.SetAssignmentStatus(context.Background(), 99, nil); !errors.Is(err, entity.ErrNotFound) {
		t.Fatalf("不存在的作业应返回 ErrNotFound: %v", err)
	}
}

func TestErrorCodeMapping(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{entity.ErrAuthRequired, "AUTH_REQUIRED"},
		{entity.ErrAuthExpired, "AUTH_EXPIRED"},
		{entity.ErrAuthFailed, "AUTH_FAILED"},
		{entity.ErrNetwork, "NETWORK"},
		{entity.ErrBusiness, "BUSINESS"},
		{entity.ErrPlatformChanged, "PLATFORM_CHANGED"},
		{entity.ErrCanceled, "CANCELED"},
		{ErrRefreshRunning, "REFRESH_RUNNING"},
		{errors.New("其他"), "INTERNAL"},
	}
	for _, c := range cases {
		if got := ErrorCode(c.err); got != c.want {
			t.Errorf("%v -> %s, want %s", c.err, got, c.want)
		}
	}
}

func tronclassSession(t *testing.T, uid string) *entity.Session {
	t.Helper()
	raw, err := json.Marshal(entity.TronclassSecret{Session: "V2-1-uuid.MTIzNDU.1700000000.sig", UserID: uid})
	if err != nil {
		t.Fatal(err)
	}
	return &entity.Session{Platform: entity.PlatformTronclass, Secret: raw}
}

func TestImportTronclassSessionSavesAndMarksConnected(t *testing.T) {
	h := newHarness(t)
	h.importer.session = tronclassSession(t, "2024001")

	name, err := h.svc.ImportTronclassSession(context.Background(), "  V2-1-x ")
	if err != nil {
		t.Fatal(err)
	}
	if name != "2024001" {
		t.Fatalf("显示名 = %q, want 2024001", name)
	}
	if h.importer.raw != "  V2-1-x " {
		t.Fatalf("适配器收到 %q（归一化由适配器负责）", h.importer.raw)
	}
	if h.sessions.saves != 1 {
		t.Fatalf("保存次数 = %d, want 1", h.sessions.saves)
	}
	if got := h.accounts.names[entity.PlatformTronclass]; got != "2024001" {
		t.Fatalf("账号显示名 = %q", got)
	}
	if h.accounts.state(entity.PlatformTronclass) != entity.LoginConnected {
		t.Fatalf("登录态 = %s", h.accounts.state(entity.PlatformTronclass))
	}
}

func TestImportTronclassSessionFallsBackToImportedName(t *testing.T) {
	h := newHarness(t)
	h.importer.session = tronclassSession(t, "")

	name, err := h.svc.ImportTronclassSession(context.Background(), "V2-1-x")
	if err != nil {
		t.Fatal(err)
	}
	if name != "已导入" {
		t.Fatalf("显示名 = %q, want 已导入", name)
	}
}

func TestImportTronclassSessionFailureKeepsOldSession(t *testing.T) {
	h := newHarness(t)
	before := h.sessions.secret
	h.importer.err = entity.ErrAuthExpired

	if _, err := h.svc.ImportTronclassSession(context.Background(), "V2-1-x"); !errors.Is(err, entity.ErrAuthExpired) {
		t.Fatalf("err = %v, want ErrAuthExpired", err)
	}
	if h.sessions.saves != 0 {
		t.Fatal("验证失败不应写入会话")
	}
	if string(h.sessions.secret) != string(before) {
		t.Fatal("验证失败不应改动已保存的会话")
	}
	if h.accounts.state(entity.PlatformTronclass) != "" {
		t.Fatalf("验证失败不应改动登录态: %s", h.accounts.state(entity.PlatformTronclass))
	}
}

func TestImportTronclassSessionRejectsWhileRefreshing(t *testing.T) {
	h := newHarness(t)
	h.importer.session = tronclassSession(t, "2024001")
	h.svc.jobsMu.Lock()
	h.svc.running[entity.PlatformTronclass] = "refresh-1"
	h.svc.jobsMu.Unlock()

	_, err := h.svc.ImportTronclassSession(context.Background(), "V2-1-x")
	if !errors.Is(err, ErrRefreshRunning) {
		t.Fatalf("err = %v, want ErrRefreshRunning", err)
	}
	if h.importer.calls != 0 {
		t.Fatal("刷新进行中不应验证或保存会话")
	}
}

func TestTermOnlyResolvesSemesterStart(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	if _, err := h.svc.ListAssignments(ctx, entity.AssignmentFilter{TermOnly: true}); err != nil {
		t.Fatal(err)
	}
	f := h.assignments.filter()
	if f.DeadlineFrom == nil {
		t.Fatal("TermOnly 必须换算成 DeadlineFrom")
	}
	want := entity.SemesterStart(time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC))
	if !f.DeadlineFrom.Equal(want) {
		t.Fatalf("DeadlineFrom = %s, want %s", f.DeadlineFrom, want)
	}

	// 平台卡统计与列表必须同口径
	if _, err := h.svc.Bootstrap(ctx, true); err != nil {
		t.Fatal(err)
	}
	if b := h.assignments.filter(); b.DeadlineFrom == nil || !b.ShowSubmitted {
		t.Fatalf("Bootstrap 未按学期过滤: %#v", b)
	}

	if _, err := h.svc.ListAssignments(ctx, entity.AssignmentFilter{}); err != nil {
		t.Fatal(err)
	}
	if f := h.assignments.filter(); f.DeadlineFrom != nil {
		t.Fatalf("关闭 TermOnly 后不应过滤: %#v", f)
	}
}
