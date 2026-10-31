package service

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"catcher/internal/domain/entity"
	"catcher/internal/infra/sqlite"
)

type aiTestCipher struct{}

func (aiTestCipher) Encrypt(b []byte) ([]byte, error) { return append([]byte("sealed:"), b...), nil }
func (aiTestCipher) Decrypt(b []byte) ([]byte, error) {
	if !strings.HasPrefix(string(b), "sealed:") {
		return nil, fmt.Errorf("bad cipher")
	}
	return b[7:], nil
}

type aiTestModel struct {
	calls  atomic.Int32
	output string
	block  bool
	err    error
}

func (m *aiTestModel) Generate(ctx context.Context, in entity.AIModelRequest) (entity.AIModelResult, error) {
	m.calls.Add(1)
	if m.block {
		<-ctx.Done()
		return entity.AIModelResult{}, ctx.Err()
	}
	return entity.AIModelResult{Content: m.output, InputTokens: 100, OutputTokens: 50, UsageKnown: true}, m.err
}

type aiFixture struct {
	svc   *AIService
	repo  *sqlite.AIRepo
	data  *sqlite.Repo
	db    *sqlite.DB
	model *aiTestModel
	now   time.Time
	ids   []int64
}

func newAIFixture(t *testing.T) *aiFixture {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, 10, 31, 10, 0, 0, 0, time.UTC)
	db, e := sqlite.Open(ctx, filepath.Join(t.TempDir(), "ai.db"))
	if e != nil {
		t.Fatal(e)
	}
	if e = db.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	repo := sqlite.NewRepo(db)
	if e = repo.EnsurePlatforms(ctx, now); e != nil {
		t.Fatal(e)
	}
	account, e := repo.GetAccount(ctx, entity.PlatformChaoxing)
	if e != nil {
		t.Fatal(e)
	}
	deadline := now.Add(48 * time.Hour)
	_, e = repo.SaveSnapshot(ctx, entity.PersistInput{AccountID: account.ID, Platform: entity.PlatformChaoxing, At: now, Result: entity.FetchResult{Complete: true, Assignments: []entity.Assignment{
		{DedupKey: "one", Title: "数学作业", CourseName: "数学", Status: entity.StatusUnsubmitted, Deadline: &deadline, DeadlineSource: entity.DeadlinePlatformExact, Description: "PRIVATE_BODY_NOT_SENT", DetailURL: "https://example.com/?token=PRIVATE_TOKEN", ExternalID: "PRIVATE_EXTERNAL_ID"},
		{DedupKey: "two", Title: "英语作业", CourseName: "英语", Status: entity.StatusUnknown, DeadlineSource: entity.DeadlineUnknown},
		{DedupKey: "three", Title: "已交作业", CourseName: "数学", Status: entity.StatusSubmitted, Deadline: &deadline, DeadlineSource: entity.DeadlinePlatformExact},
	}}})
	if e != nil {
		t.Fatal(e)
	}
	items, e := repo.ListAssignments(ctx, entity.AssignmentFilter{ShowSubmitted: true})
	if e != nil {
		t.Fatal(e)
	}
	ids := []int64{}
	for _, a := range items {
		ids = append(ids, a.ID)
	}
	ar := sqlite.NewAIRepo(db, aiTestCipher{})
	model := &aiTestModel{output: `{"keyword":"数学","platform":"","status":"not_submitted","range":"all"}`}
	svc := NewAI(ar, model, func() time.Time { return now })
	t.Cleanup(func() { svc.Shutdown(); db.Close() })
	return &aiFixture{svc, ar, repo, db, model, now, ids}
}
func (f *aiFixture) enable(t *testing.T) {
	t.Helper()
	cfg := entity.DefaultAISettings()
	cfg.Enabled = true
	if e := f.svc.SaveSettings(context.Background(), cfg, "FAKE_API_KEY", false); e != nil {
		t.Fatal(e)
	}
}
func (f *aiFixture) input(kind string) entity.AIInput {
	return entity.AIInput{Kind: kind, Question: "数学优先", AssignmentIDs: f.ids, StartAt: f.now.Add(time.Hour).Format(time.RFC3339), EndAt: f.now.Add(3 * time.Hour).Format(time.RFC3339)}
}
func (f *aiFixture) start(t *testing.T, in entity.AIInput) string {
	t.Helper()
	p, e := f.svc.Preview(context.Background(), in)
	if e != nil {
		t.Fatal(e)
	}
	in.Snapshot = p["snapshot"].(string)
	id, e := f.svc.Start(context.Background(), in)
	if e != nil {
		t.Fatal(e)
	}
	return id
}
func (f *aiFixture) wait(t *testing.T, id string) entity.AIJob {
	t.Helper()
	until := time.Now().Add(3 * time.Second)
	for time.Now().Before(until) {
		j, e := f.svc.Job(id)
		if e != nil {
			t.Fatal(e)
		}
		if j.State != "running" {
			return j
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("AI 任务未结束")
	return entity.AIJob{}
}

func TestAIDefaultOffAndPrivatePreview(t *testing.T) {
	f := newAIFixture(t)
	in := f.input("query")
	if _, e := f.svc.Preview(context.Background(), in); e == nil || f.model.calls.Load() != 0 {
		t.Fatal("默认不能调用模型")
	}
	f.enable(t)
	p, e := f.svc.Preview(context.Background(), in)
	if e != nil {
		t.Fatal(e)
	}
	payload := p["payload"].(string)
	publicPreview, _ := json.Marshal(p)
	if strings.Contains(string(publicPreview), "PRIVATE_BODY_NOT_SENT") {
		t.Fatal("预览与计划事实卡片不应复制完整题干")
	}
	for _, private := range []string{"FAKE_API_KEY", "PRIVATE_BODY_NOT_SENT", "PRIVATE_TOKEN", "PRIVATE_EXTERNAL_ID", "accountStamp", "assignmentId"} {
		if strings.Contains(payload, private) {
			t.Fatalf("上下文包含不应出站的数据 %s", private)
		}
	}
	state, e := f.svc.Settings(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(state)
	if strings.Contains(string(raw), "FAKE_API_KEY") {
		t.Fatal("设置返回了密钥")
	}
}
func TestAIRequiresMatchingPreview(t *testing.T) {
	for _, change := range []string{"missing", "question", "selection", "model"} {
		t.Run(change, func(t *testing.T) {
			f := newAIFixture(t)
			f.enable(t)
			in := f.input("query")
			p, e := f.svc.Preview(context.Background(), in)
			if e != nil {
				t.Fatal(e)
			}
			in.Snapshot = p["snapshot"].(string)
			switch change {
			case "missing":
				in.Snapshot = ""
			case "question":
				in.Question = "其他问题"
			case "selection":
				in.AssignmentIDs = in.AssignmentIDs[:1]
			case "model":
				cfg, _ := f.repo.Settings(context.Background())
				cfg.Model = "changed"
				if e = f.svc.SaveSettings(context.Background(), cfg, "", false); e != nil {
					t.Fatal(e)
				}
			}
			if _, e = f.svc.Start(context.Background(), in); e == nil || f.model.calls.Load() != 0 {
				t.Fatal("必须重新预览", e)
			}
		})
	}
}
func TestAIQueryUsesCanonicalFacts(t *testing.T) {
	f := newAIFixture(t)
	f.enable(t)
	id := f.start(t, f.input("query"))
	j := f.wait(t, id)
	if j.State != "succeeded" || len(j.Sources) != 1 || j.Sources[0].Title != "数学作业" || !j.UsageKnown {
		t.Fatal(j)
	}
	usage, e := f.repo.Usage(context.Background(), "2026-10")
	if e != nil || usage.ChargedTokens != 150 || usage.ReservedTokens != 0 {
		t.Fatal(usage, e)
	}
}
func TestAIQuotaCancellationAndExclusion(t *testing.T) {
	t.Run("quota", func(t *testing.T) {
		f := newAIFixture(t)
		f.enable(t)
		cfg, _ := f.repo.Settings(context.Background())
		cfg.MonthlyTokenLimit = 1000
		if e := f.svc.SaveSettings(context.Background(), cfg, "", false); e != nil {
			t.Fatal(e)
		}
		in := f.input("query")
		p, _ := f.svc.Preview(context.Background(), in)
		in.Snapshot = p["snapshot"].(string)
		if _, e := f.svc.Start(context.Background(), in); e == nil || f.model.calls.Load() != 0 {
			t.Fatal("额度不足不能调用模型")
		}
	})
	t.Run("cancel", func(t *testing.T) {
		f := newAIFixture(t)
		f.enable(t)
		f.model.block = true
		id := f.start(t, f.input("query"))
		if _, e := f.svc.Start(context.Background(), f.input("query")); e == nil {
			t.Fatal("不应并发启动第二个模型请求")
		}
		if e := f.svc.Cancel(id); e != nil {
			t.Fatal(e)
		}
		j := f.wait(t, id)
		if j.State != "failed" {
			t.Fatal(j)
		}
		u, _ := f.repo.Usage(context.Background(), "2026-10")
		if u.ReservedTokens != 0 || u.ChargedTokens == 0 {
			t.Fatal("取消必须可靠收尾并保守保留费用", u)
		}
	})
}
func TestAISettingsClearAndEndpointChange(t *testing.T) {
	f := newAIFixture(t)
	f.enable(t)
	cfg, _ := f.repo.Settings(context.Background())
	cfg.BaseURL = "https://other.example.com"
	if e := f.svc.SaveSettings(context.Background(), cfg, "", false); e == nil {
		t.Fatal("更换服务不能继承旧 Key")
	}
	cfg, _ = f.repo.Settings(context.Background())
	cfg.Enabled = false
	if e := f.svc.SaveSettings(context.Background(), cfg, "", true); e != nil {
		t.Fatal(e)
	}
	cfg, _ = f.repo.Settings(context.Background())
	if cfg.Key != "" || cfg.Enabled {
		t.Fatal("清除 Key 失败")
	}
}
func TestAIRejectsInvalidModelResults(t *testing.T) {
	outputs := []string{
		`not json`, `{} {}`, `{"sql":"DROP TABLE accounts"}`, `{"keyword":"","platform":"evil","status":"all","range":"all"}`,
		`{"keyword":"","platform":"","status":"false","range":"all"}`, `{"keyword":"","platform":"","status":"all","range":"yesterday"}`,
	}
	for i, raw := range outputs {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			f := newAIFixture(t)
			f.enable(t)
			f.model.output = raw
			j := f.wait(t, f.start(t, f.input("query")))
			if j.State != "failed" {
				t.Fatal(j)
			}
		})
	}
}

func TestAIPlanConfirmationAndLocalCompletion(t *testing.T) {
	f := newAIFixture(t)
	f.enable(t)
	f.model.output = `{"title":"今晚计划","tasks":[{"ref":"A1","title":"整理数学要求","minutes":30,"steps":["核对题目"],"reason":"仅有标题，时长估算"}],"notes":[]}`
	in := f.input("plan")
	in.Blocked = []entity.AITimeRange{{StartAt: f.now.Add(time.Hour).Format(time.RFC3339), EndAt: f.now.Add(90 * time.Minute).Format(time.RFC3339)}}
	j := f.wait(t, f.start(t, in))
	if j.State != "succeeded" || j.Plan == nil {
		t.Fatal(j)
	}
	tasks := j.Plan.Tasks
	tasks[0].Minutes = 20
	plan, e := f.svc.Confirm(context.Background(), j.ID, tasks)
	if e != nil {
		t.Fatal(e)
	}
	if plan.Tasks[0].StartAt != f.now.Add(90*time.Minute).Format(time.RFC3339) {
		t.Fatal("必须跳过固定安排", plan.Tasks)
	}
	if e = f.svc.SetTaskDone(context.Background(), plan.ID, plan.Tasks[0].ID, true); e != nil {
		t.Fatal(e)
	}
	if _, e = f.svc.Confirm(context.Background(), j.ID, tasks); e != nil {
		t.Fatal(e)
	}
	plans, e := f.svc.Plans(context.Background())
	if e != nil || len(plans) != 1 || !plans[0].Tasks[0].Done {
		t.Fatal("确认必须幂等且保留本地完成状态", plans, e)
	}
	assignments, _ := f.data.ListAssignments(context.Background(), entity.AssignmentFilter{ShowSubmitted: true})
	for _, a := range assignments {
		if a.ID == tasks[0].AssignmentID && (a.Status != entity.StatusUnsubmitted || a.ManualStatus != nil) {
			t.Fatal("学习完成不能修改平台提交状态")
		}
	}
}
func TestAIPlanRejectsChangedFactsAndSession(t *testing.T) {
	for _, change := range []string{"deadline", "manual_status", "session"} {
		t.Run(change, func(t *testing.T) {
			f := newAIFixture(t)
			f.enable(t)
			f.model.output = `{"title":"计划","tasks":[{"ref":"A1","title":"准备","minutes":20,"steps":[],"reason":"估算"}],"notes":[]}`
			j := f.wait(t, f.start(t, f.input("plan")))
			if j.Plan == nil {
				t.Fatal(j)
			}
			switch change {
			case "deadline":
				_, _ = f.db.SQL().Exec("UPDATE assignments SET deadline_at=? WHERE id=?", f.now.Add(time.Hour).Format(time.RFC3339), j.Plan.Tasks[0].AssignmentID)
			case "manual_status":
				status := entity.StatusSubmitted
				_, _ = f.data.SetManualStatus(context.Background(), j.Plan.Tasks[0].AssignmentID, &status, f.now)
			case "session":
				a, _ := f.data.GetAccount(context.Background(), entity.PlatformChaoxing)
				_ = sqlite.NewSessionRepo(f.db, aiTestCipher{}).Save(context.Background(), a.ID, []byte("changed identity"), nil, f.now)
			}
			if _, e := f.svc.Confirm(context.Background(), j.ID, j.Plan.Tasks); e == nil {
				t.Fatal("变化后的作业或登录会话必须重新确认")
			}
		})
	}
}
func TestAISnapshotDuplicateRefreshDoesNotInvalidate(t *testing.T) {
	f := newAIFixture(t)
	before, e := f.repo.Snapshot(context.Background(), f.ids)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.db.SQL().Exec("UPDATE assignments SET last_seen_at=?", f.now.Add(time.Hour).Format(time.RFC3339)); e != nil {
		t.Fatal(e)
	}
	after, e := f.repo.Snapshot(context.Background(), f.ids)
	if e != nil || before.Fingerprint != after.Fingerprint {
		t.Fatal("重复读取不应改变事实指纹", e)
	}
}

func TestAIPlanRejectsUntrustedModelShape(t *testing.T) {
	for _, output := range []string{
		`{"title":"计划","tasks":[{"ref":"A999","title":"越界引用","minutes":20,"steps":[],"reason":""}],"notes":[]}`,
		`{"title":"计划","tasks":[{"ref":"A3","title":"已交作业","minutes":20,"steps":[],"reason":""}],"notes":[]}`,
		`{"title":"计划","tasks":[{"ref":"A1","title":"无效时长","minutes":-5,"steps":[],"reason":""}],"notes":[]}`,
		`{"title":"计划","tasks":[{"ref":"A1","title":"超出窗口","minutes":180,"steps":[],"reason":""}],"notes":[]}`,
		`{"title":"计划","tasks":[{"ref":"A1","title":"伪造截止","minutes":20,"deadline":"2099-01-01","steps":[],"reason":""}],"notes":[]}`,
	} {
		t.Run(output, func(t *testing.T) {
			f := newAIFixture(t)
			f.enable(t)
			f.model.output = output
			job := f.wait(t, f.start(t, f.input("plan")))
			if job.State != "failed" || job.Plan != nil {
				t.Fatal("无效或越权草案不能进入确认流程", job)
			}
			plans, err := f.repo.Plans(context.Background())
			if err != nil || len(plans) != 0 {
				t.Fatal("模型不能直接保存计划", plans, err)
			}
		})
	}
}

func TestAIQueryDateRangesAndUnknown(t *testing.T) {
	now := time.Date(2026, 10, 31, 10, 0, 0, 0, time.UTC)
	tomorrow := time.Date(2026, 11, 1, 2, 0, 0, 0, time.UTC)
	nextWeek := time.Date(2026, 11, 2, 2, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour)
	sources := []entity.AISource{{ID: 1, Title: "数学", Status: "unsubmitted", Deadline: &tomorrow}, {ID: 2, Title: "数学", Status: "unknown"}, {ID: 3, Title: "数学", Status: "submitted", Deadline: &past}, {ID: 4, Title: "数学", Status: "unknown", Deadline: &nextWeek}}
	cases := []struct {
		r, status string
		want      int
	}{{"today", "all", 1}, {"tomorrow", "all", 1}, {"this_week", "all", 2}, {"next_week", "all", 1}, {"overdue", "all", 1}, {"all", "not_submitted", 3}, {"all", "unknown", 2}, {"all", "submitted", 1}, {"all", "unsubmitted", 1}}
	for _, tc := range cases {
		t.Run(tc.r+tc.status, func(t *testing.T) {
			raw := fmt.Sprintf(`{"keyword":"数学","platform":"","status":%q,"range":%q}`, tc.status, tc.r)
			items, notes, e := queryAI(raw, sources, now)
			if e != nil || len(items) != tc.want {
				t.Fatal(items, notes, e)
			}
		})
	}
}
func TestAIPlanningConstraints(t *testing.T) {
	now := time.Date(2026, 10, 31, 10, 0, 0, 0, time.UTC)
	deadline := now.Add(90 * time.Minute)
	snap := entity.AISnapshot{Sources: []entity.AISource{{ID: 1, Status: "unsubmitted", Deadline: &deadline}, {ID: 2, Status: "submitted"}}}
	cases := []string{"zero", "negative", "oversized", "unselected", "submitted", "deadline", "duplicate", "overlap", "outside", "past"}
	for _, tc := range cases {
		t.Run(tc, func(t *testing.T) {
			p := entity.StudyPlan{StartAt: now.Add(time.Hour).Format(time.RFC3339), EndAt: now.Add(3 * time.Hour).Format(time.RFC3339), Tasks: []entity.StudyTask{{ID: "t", AssignmentID: 1, Title: "准备", Minutes: 20}}}
			switch tc {
			case "zero":
				p.Tasks[0].Minutes = 0
			case "negative":
				p.Tasks[0].Minutes = -1
			case "oversized":
				p.Tasks[0].Minutes = 481
			case "unselected":
				p.Tasks[0].AssignmentID = 99
			case "submitted":
				p.Tasks[0].AssignmentID = 2
			case "deadline":
				p.Tasks[0].Minutes = 40
			case "duplicate":
				p.Tasks = append(p.Tasks, p.Tasks[0])
			case "overlap":
				p.Blocked = []entity.AITimeRange{{StartAt: p.StartAt, EndAt: now.Add(2 * time.Hour).Format(time.RFC3339)}, {StartAt: p.StartAt, EndAt: now.Add(2 * time.Hour).Format(time.RFC3339)}}
			case "outside":
				p.Blocked = []entity.AITimeRange{{StartAt: now.Format(time.RFC3339), EndAt: p.EndAt}}
			case "past":
				p.StartAt = now.Add(-time.Hour).Format(time.RFC3339)
			}
			if e := schedulePlan(&p, snap, now); e == nil {
				t.Fatal("必须拒绝无效安排", tc)
			}
		})
	}
}
