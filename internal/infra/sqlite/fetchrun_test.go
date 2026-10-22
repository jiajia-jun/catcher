package sqlite

import (
	"context"
	"testing"
	"time"

	"catcher/internal/domain/entity"
)

func TestFetchRunLifecycle(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewRepo(db)
	account := mustYuketangAccount(t, repo)
	runs := NewFetchRunRepo(db)

	started := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	run := &entity.FetchRun{
		AccountID: account.ID,
		Platform:  entity.PlatformYuketang,
		StartedAt: started,
	}
	if err := runs.Create(ctx, run); err != nil {
		t.Fatal(err)
	}
	if run.ID == 0 {
		t.Fatal("Create 未回填 id")
	}
	if run.Status != entity.RunRunning {
		t.Fatalf("初始状态 = %s", run.Status)
	}

	latest, err := runs.LatestRuns(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := latest[entity.PlatformYuketang]; !ok || got.ID != run.ID {
		t.Fatalf("running 记录应出现在 LatestRuns: %#v", latest)
	}

	issues := []entity.FetchIssue{
		{Scope: entity.ScopeCourse, Ref: "1001", Kind: entity.IssueNetwork, Message: "超时"},
		{Scope: entity.ScopePlatform, Kind: entity.IssueParse, Message: "结构变化", RawPath: "raw/2026-10-21/x.http"},
	}
	run.Status = entity.RunPartial
	run.CoursesTotal = 3
	run.CoursesOK = 2
	run.ItemsSeen = 7
	run.ItemsUpserted = 6
	run.WarningCount = 2
	run.ErrorSummary = "部分课程失败"
	finished := started.Add(5 * time.Second)
	if err := runs.Finish(ctx, *run, issues, finished); err != nil {
		t.Fatal(err)
	}

	got, err := runs.ListIssues(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("issues = %d, want 2", len(got))
	}
	if got[0].Kind != entity.IssueNetwork || got[0].Ref != "1001" {
		t.Fatalf("issue[0] = %#v", got[0])
	}

	latest, err = runs.LatestRuns(ctx)
	if err != nil {
		t.Fatal(err)
	}
	stored := latest[entity.PlatformYuketang]
	if stored.Status != entity.RunPartial || stored.FinishedAt == nil || !stored.FinishedAt.Equal(finished) {
		t.Fatalf("刷新终态未写入: %#v", stored)
	}
	if stored.ItemsUpserted != 6 || stored.WarningCount != 2 {
		t.Fatalf("计数未写入: %#v", stored)
	}

	// 再跑一次，LatestRuns 取最新的
	next := &entity.FetchRun{AccountID: account.ID, Platform: entity.PlatformYuketang, StartedAt: finished}
	if err := runs.Create(ctx, next); err != nil {
		t.Fatal(err)
	}
	latest, err = runs.LatestRuns(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if latest[entity.PlatformYuketang].ID != next.ID {
		t.Fatal("LatestRuns 没有返回最近一条")
	}
}

func TestFetchRunFinishRejectsRunning(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewRepo(db)
	account := mustYuketangAccount(t, repo)
	runs := NewFetchRunRepo(db)

	run := &entity.FetchRun{AccountID: account.ID, Platform: entity.PlatformYuketang, StartedAt: time.Now().UTC()}
	if err := runs.Create(ctx, run); err != nil {
		t.Fatal(err)
	}
	if err := runs.Finish(ctx, *run, nil, time.Now().UTC()); err == nil {
		t.Fatal("running 不能作为终态写入")
	}
}
