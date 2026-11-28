package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"catcher/internal/domain/entity"
	"catcher/internal/service"
)

type fakeService struct {
	accounts    []entity.Account
	accountsErr error

	items      []entity.Assignment
	itemsErr   error
	lastFilter entity.AssignmentFilter

	setItem   entity.Assignment
	setErr    error
	gotID     int64
	gotStatus *entity.SubmissionStatus

	bootstrap *service.Bootstrap
	bootErr   error

	refresh          []service.PlatformResult
	blockUntilCancel bool
	gotPlatforms     []entity.Platform

	enabled []entity.Platform
	version string
	dataDir string
}

func (f *fakeService) Accounts(context.Context) ([]entity.Account, error) {
	return f.accounts, f.accountsErr
}

func (f *fakeService) ListAssignments(_ context.Context, filter entity.AssignmentFilter) ([]entity.Assignment, error) {
	f.lastFilter = filter
	return f.items, f.itemsErr
}

func (f *fakeService) SetAssignmentStatus(_ context.Context, id int64, status *entity.SubmissionStatus) (entity.Assignment, error) {
	f.gotID = id
	f.gotStatus = status
	return f.setItem, f.setErr
}

func (f *fakeService) Bootstrap(context.Context, bool) (*service.Bootstrap, error) {
	return f.bootstrap, f.bootErr
}

func (f *fakeService) RefreshSync(ctx context.Context, platforms []entity.Platform) []service.PlatformResult {
	f.gotPlatforms = platforms
	if f.blockUntilCancel {
		<-ctx.Done()
		return []service.PlatformResult{{
			Platform: platforms[0],
			Status:   entity.RunCanceled,
			Error:    &service.ErrorEvent{Code: "CANCELED", Message: "已取消"},
		}}
	}
	return f.refresh
}

func (f *fakeService) EnabledPlatforms() []entity.Platform { return f.enabled }
func (f *fakeService) Version() string                     { return f.version }
func (f *fakeService) DataDir() string                     { return f.dataDir }

type result struct {
	code   int
	stdout string
	stderr string
}

func execute(svc Service, args ...string) result {
	var stdout, stderr bytes.Buffer
	now := time.Date(2026, 11, 28, 19, 0, 0, 0, time.UTC)
	code := Execute(args, Deps{
		Service: svc,
		Version: "1.6.0",
		Stdout:  &stdout,
		Stderr:  &stderr,
		Now:     func() time.Time { return now },
	})
	return result{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

type envelope struct {
	OK    bool            `json:"ok"`
	Error *ErrorBody      `json:"error"`
	Data  json.RawMessage `json:"data"`
}

func decode(t *testing.T, text string) envelope {
	t.Helper()
	var env envelope
	if err := json.Unmarshal([]byte(text), &env); err != nil {
		t.Fatalf("解析输出失败: %v\n%s", err, text)
	}
	return env
}

func TestUnknownCommandIsUsageError(t *testing.T) {
	res := execute(&fakeService{}, "bogus")
	if res.code != ExitUsage {
		t.Fatalf("退出码 = %d, want 2", res.code)
	}
	if res.stdout != "" {
		t.Fatalf("用法错误不应写 stdout: %s", res.stdout)
	}
	if !strings.Contains(res.stderr, "未知命令") {
		t.Fatalf("stderr = %q", res.stderr)
	}
}

func TestVersionCommand(t *testing.T) {
	res := execute(&fakeService{}, "version")
	if res.code != ExitOK {
		t.Fatalf("退出码 = %d", res.code)
	}
	env := decode(t, res.stdout)
	if !env.OK {
		t.Fatal("ok 应为 true")
	}
	var data versionData
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatal(err)
	}
	if data.Version != "1.6.0" {
		t.Fatalf("版本 = %q", data.Version)
	}
}

func TestListDefaultFilter(t *testing.T) {
	deadline := time.Date(2026, 12, 1, 15, 59, 0, 0, time.UTC)
	svc := &fakeService{items: []entity.Assignment{{
		ID:       7,
		Platform: entity.PlatformChaoxing,
		Title:    "实验三",
		Status:   entity.StatusUnsubmitted,
		Deadline: &deadline,
	}}}

	res := execute(svc, "list")
	if res.code != ExitOK {
		t.Fatalf("退出码 = %d, stderr=%s", res.code, res.stderr)
	}
	if !svc.lastFilter.TermOnly || svc.lastFilter.ShowSubmitted {
		t.Fatalf("默认筛选 = %#v, want TermOnly 且隐藏已提交", svc.lastFilter)
	}
	if len(svc.lastFilter.Statuses) != 0 || len(svc.lastFilter.Platforms) != 0 {
		t.Fatalf("默认不应带状态或平台: %#v", svc.lastFilter)
	}

	env := decode(t, res.stdout)
	var data listData
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatal(err)
	}
	if data.Total != 1 || len(data.Items) != 1 || data.Items[0].ID != 7 {
		t.Fatalf("列表 = %#v", data)
	}
}

func TestListExplicitStatusAndFlags(t *testing.T) {
	svc := &fakeService{}
	res := execute(svc, "list", "--status", "submitted,unsubmitted", "--all-terms", "--keyword", " 实验 ")
	if res.code != ExitOK {
		t.Fatalf("退出码 = %d, stderr=%s", res.code, res.stderr)
	}
	if svc.lastFilter.TermOnly {
		t.Fatal("--all-terms 应关闭学期过滤")
	}
	if svc.lastFilter.Keyword != "实验" {
		t.Fatalf("关键字 = %q", svc.lastFilter.Keyword)
	}
	if len(svc.lastFilter.Statuses) != 2 {
		t.Fatalf("状态 = %#v", svc.lastFilter.Statuses)
	}
}

func TestListInvalidStatus(t *testing.T) {
	res := execute(&fakeService{}, "list", "--status", "done")
	if res.code != ExitFailure {
		t.Fatalf("退出码 = %d, want 1", res.code)
	}
	env := decode(t, res.stdout)
	if env.Error == nil || env.Error.Code != "INVALID_ARGUMENT" {
		t.Fatalf("错误 = %#v", env.Error)
	}
}

func TestMarkWritesManualStatus(t *testing.T) {
	submitted := entity.StatusSubmitted
	svc := &fakeService{setItem: entity.Assignment{
		ID:           123,
		Platform:     entity.PlatformChaoxing,
		Title:        "实验三",
		Status:       entity.StatusUnknown,
		ManualStatus: &submitted,
	}}
	res := execute(svc, "mark", "123", "--status", "submitted")
	if res.code != ExitOK {
		t.Fatalf("退出码 = %d, stderr=%s", res.code, res.stderr)
	}
	if svc.gotID != 123 || svc.gotStatus == nil || *svc.gotStatus != submitted {
		t.Fatalf("调用参数 = id %d status %v", svc.gotID, svc.gotStatus)
	}
	var data markData
	env := decode(t, res.stdout)
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatal(err)
	}
	if data.Item.Status != "submitted" || data.Item.ManualStatus != "submitted" {
		t.Fatalf("返回作业 = %#v", data.Item)
	}
}

func TestMarkClearRestoresPlatformValue(t *testing.T) {
	svc := &fakeService{setItem: entity.Assignment{ID: 5, Status: entity.StatusUnsubmitted}}
	res := execute(svc, "mark", "5", "--status", "clear")
	if res.code != ExitOK {
		t.Fatalf("退出码 = %d", res.code)
	}
	if svc.gotStatus != nil {
		t.Fatalf("clear 应传 nil，收到 %v", *svc.gotStatus)
	}
}

func TestMarkNotFound(t *testing.T) {
	svc := &fakeService{setErr: entity.ErrNotFound}
	res := execute(svc, "mark", "9", "--status", "submitted")
	if res.code != ExitFailure {
		t.Fatalf("退出码 = %d", res.code)
	}
	env := decode(t, res.stdout)
	if env.Error == nil || env.Error.Code != "NOT_FOUND" {
		t.Fatalf("错误 = %#v", env.Error)
	}
}

func TestMarkInvalidStatusAndMissingID(t *testing.T) {
	if res := execute(&fakeService{}, "mark", "9", "--status", "done"); res.code != ExitFailure {
		t.Fatalf("状态非法退出码 = %d", res.code)
	}
	res := execute(&fakeService{}, "mark")
	if res.code != ExitFailure {
		t.Fatalf("缺少 id 退出码 = %d, want 1", res.code)
	}
	if env := decode(t, res.stdout); env.Error == nil || env.Error.Code != "INVALID_ARGUMENT" {
		t.Fatalf("错误 = %#v", env.Error)
	}
}

func TestRefreshSkippedIsSuccess(t *testing.T) {
	svc := &fakeService{
		enabled: []entity.Platform{entity.PlatformPigai},
		refresh: []service.PlatformResult{{
			Platform:   entity.PlatformPigai,
			Status:     entity.RunSkipped,
			SkipReason: "not_logged_in",
		}},
	}
	res := execute(svc, "refresh")
	if res.code != ExitOK {
		t.Fatalf("退出码 = %d, stderr=%s", res.code, res.stderr)
	}
	env := decode(t, res.stdout)
	if !env.OK {
		t.Fatal("skipped 应算成功")
	}
	var data refreshData
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatal(err)
	}
	if len(data.Platforms) != 1 || data.Platforms[0].Status != "skipped" {
		t.Fatalf("平台 = %#v", data.Platforms)
	}
	if data.Platforms[0].SkipReason != "not_logged_in" {
		t.Fatalf("skipReason = %q", data.Platforms[0].SkipReason)
	}
	if len(data.Platforms[0].Issues) != 0 {
		t.Fatalf("issues 应为空数组: %#v", data.Platforms[0].Issues)
	}
}

func TestRefreshPartialIsSuccess(t *testing.T) {
	svc := &fakeService{refresh: []service.PlatformResult{{
		Platform: entity.PlatformChaoxing,
		Status:   entity.RunPartial,
		Run:      entity.FetchRun{ID: 3, ItemsSeen: 2, ItemsUpserted: 2, WarningCount: 1},
		Issues:   []entity.FetchIssue{{Scope: entity.ScopeCourse, Ref: "1", Kind: entity.IssueNetwork, Message: "课程失败"}},
	}}}
	res := execute(svc, "refresh")
	if res.code != ExitOK {
		t.Fatalf("partial 退出码 = %d, stderr=%s", res.code, res.stderr)
	}
	if !decode(t, res.stdout).OK {
		t.Fatal("partial 应 ok=true")
	}
}

func TestRefreshFailureExitsTwoOrThree(t *testing.T) {
	network := &fakeService{refresh: []service.PlatformResult{{
		Platform: entity.PlatformChaoxing,
		Status:   entity.RunFailed,
		Error:    &service.ErrorEvent{Code: "NETWORK", Message: "连接失败"},
	}}}
	if res := execute(network, "refresh"); res.code != ExitFailure {
		t.Fatalf("网络失败退出码 = %d", res.code)
	}

	auth := &fakeService{refresh: []service.PlatformResult{{
		Platform: entity.PlatformChaoxing,
		Status:   entity.RunFailed,
		Error:    &service.ErrorEvent{Code: "AUTH_EXPIRED", Message: "登录已失效"},
	}}}
	res := execute(auth, "refresh")
	if res.code != ExitAuth {
		t.Fatalf("登录失效退出码 = %d, want 3", res.code)
	}
	env := decode(t, res.stdout)
	if env.OK || env.Error == nil || env.Error.Code != "AUTH_EXPIRED" {
		t.Fatalf("错误 = %#v", env.Error)
	}
	if len(env.Data) == 0 {
		t.Fatal("失败时仍应返回 data")
	}
}

func TestRefreshTimeout(t *testing.T) {
	svc := &fakeService{
		enabled:          []entity.Platform{entity.PlatformChaoxing},
		blockUntilCancel: true,
	}
	res := execute(svc, "refresh", "--timeout", "30ms")
	if res.code != ExitFailure {
		t.Fatalf("退出码 = %d", res.code)
	}
	env := decode(t, res.stdout)
	if env.Error == nil || env.Error.Code != "TIMEOUT" {
		t.Fatalf("错误 = %#v", env.Error)
	}
}

func TestRefreshPlatformSelection(t *testing.T) {
	svc := &fakeService{
		enabled: []entity.Platform{entity.PlatformChaoxing},
		refresh: []service.PlatformResult{},
	}
	if res := execute(svc, "refresh"); res.code != ExitOK {
		t.Fatalf("退出码 = %d", res.code)
	}
	if len(svc.gotPlatforms) != 1 || svc.gotPlatforms[0] != entity.PlatformChaoxing {
		t.Fatalf("默认平台 = %v", svc.gotPlatforms)
	}

	svc.refresh = []service.PlatformResult{}
	res := execute(svc, "refresh", "--platform", "chaoxing", "--platform", "yuketang,chaoxing")
	if res.code != ExitOK {
		t.Fatalf("退出码 = %d", res.code)
	}
	want := []entity.Platform{entity.PlatformChaoxing, entity.PlatformYuketang}
	if len(svc.gotPlatforms) != len(want) {
		t.Fatalf("平台 = %v", svc.gotPlatforms)
	}
	for i := range want {
		if svc.gotPlatforms[i] != want[i] {
			t.Fatalf("平台 = %v, want %v", svc.gotPlatforms, want)
		}
	}
}

func TestRefreshRedactsURLInMessage(t *testing.T) {
	svc := &fakeService{refresh: []service.PlatformResult{{
		Platform: entity.PlatformChaoxing,
		Status:   entity.RunFailed,
		Error: &service.ErrorEvent{
			Code:    "NETWORK",
			Message: `Get "https://example.com/login?code=SECRET&state=1": EOF`,
		},
	}}}
	res := execute(svc, "refresh")
	if strings.Contains(res.stdout, "SECRET") {
		t.Fatalf("输出未打码: %s", res.stdout)
	}
	if res.code != ExitFailure {
		t.Fatalf("退出码 = %d", res.code)
	}
}

func TestStatusReportsCountsAndLoginState(t *testing.T) {
	started := time.Date(2026, 11, 28, 11, 0, 0, 0, time.UTC)
	finished := started.Add(42 * time.Second)
	svc := &fakeService{
		version: "1.6.0",
		dataDir: `D:\data`,
		bootstrap: &service.Bootstrap{
			Accounts: []entity.Account{{
				Platform:   entity.PlatformChaoxing,
				LoginState: entity.LoginConnected,
			}},
			Counts: map[entity.Platform]service.Counts{
				entity.PlatformChaoxing: {OpenUnsubmitted: 2, Unknown: 1, Submitted: 5},
			},
			LastRuns: map[entity.Platform]entity.FetchRun{
				entity.PlatformChaoxing: {
					ID: 42, Status: entity.RunSuccess, StartedAt: started, FinishedAt: &finished,
				},
			},
		},
	}
	res := execute(svc, "status")
	if res.code != ExitOK {
		t.Fatalf("退出码 = %d, stderr=%s", res.code, res.stderr)
	}
	var data statusData
	env := decode(t, res.stdout)
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatal(err)
	}
	if len(data.Platforms) != len(entity.AllPlatforms()) {
		t.Fatalf("平台数 = %d", len(data.Platforms))
	}
	got := data.Platforms[0]
	if got.Platform != "chaoxing" || got.LoginState != "connected" {
		t.Fatalf("平台 = %#v", got)
	}
	if got.Counts.OpenUnsubmitted != 2 || got.Counts.Submitted != 5 {
		t.Fatalf("统计 = %#v", got.Counts)
	}
	if got.LastRun == nil || got.LastRun.ID != 42 {
		t.Fatalf("lastRun = %#v", got.LastRun)
	}
	if !strings.HasSuffix(got.LastRun.StartedAt, "+08:00") {
		t.Fatalf("时间未转东八区: %s", got.LastRun.StartedAt)
	}
}

func TestHelpWritesPlainTextToStdout(t *testing.T) {
	res := execute(&fakeService{}, "help")
	if res.code != ExitOK {
		t.Fatalf("退出码 = %d", res.code)
	}
	if !strings.Contains(res.stdout, "用法") || res.stderr != "" {
		t.Fatalf("stdout=%q stderr=%q", res.stdout, res.stderr)
	}
}
