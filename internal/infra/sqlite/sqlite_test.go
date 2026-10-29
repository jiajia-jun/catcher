package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"catcher/internal/domain/entity"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "catcher.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return db
}

// 只有 001 的旧库升级到 002 时，原有作业行和状态必须保留
func TestMigrateUpgradesExistingAssignments(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "catcher.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	body, err := migrationsFS.ReadFile("migrations/001_init.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, string(body)); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
	if _, err := db.SQL().ExecContext(ctx,
		`INSERT INTO schema_migrations(version, applied_at) VALUES (1, ?)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx,
		`INSERT INTO accounts (platform, created_at, updated_at) VALUES ('yuketang', ?, ?)`,
		now, now); err != nil {
		t.Fatal(err)
	}
	var accountID int64
	if err := db.SQL().GetContext(ctx, &accountID,
		`SELECT id FROM accounts WHERE platform = 'yuketang'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO assignments (account_id, platform, dedup_key, title, status,
			first_seen_at, last_seen_at)
		VALUES (?, 'yuketang', 'yuketang:1:2:3', '旧作业', 'unknown', ?, ?)`,
		accountID, now, now); err != nil {
		t.Fatal(err)
	}

	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	var count, columns int
	if err := db.SQL().GetContext(ctx, &count, `SELECT COUNT(*) FROM assignments`); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().GetContext(ctx, &columns,
		`SELECT COUNT(*) FROM pragma_table_info('assignments') WHERE name IN ('manual_status', 'manual_status_at')`); err != nil {
		t.Fatal(err)
	}
	if count != 1 || columns != 2 {
		t.Fatalf("升级后作业 = %d 条，手动列 = %d 个, want 1/2", count, columns)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("迁移不可重复执行: %v", err)
	}
}

func TestMigrateAndSeed(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	var foreignKeys int
	if err := db.SQL().GetContext(ctx, &foreignKeys, "PRAGMA foreign_keys"); err != nil {
		t.Fatal(err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign_keys = %d, want 1（DSN pragma 未生效）", foreignKeys)
	}

	// 迁移可重复执行。
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	repo := NewRepo(db)
	now := time.Now().UTC().Truncate(time.Second)
	if err := repo.EnsurePlatforms(ctx, now); err != nil {
		t.Fatal(err)
	}
	if err := repo.EnsurePlatforms(ctx, now); err != nil {
		t.Fatal(err)
	}

	accounts, err := repo.ListAccounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if want := len(entity.AllPlatforms()); len(accounts) != want {
		t.Fatalf("accounts = %d, want %d", len(accounts), want)
	}
	for i, want := range entity.AllPlatforms() {
		if accounts[i].Platform != want {
			t.Fatalf("accounts[%d] = %s, want %s", i, accounts[i].Platform, want)
		}
		if accounts[i].LoginState != entity.LoginDisconnected {
			t.Fatalf("初始登录态 = %s", accounts[i].LoginState)
		}
	}

	items, err := repo.ListAssignments(ctx, entity.AssignmentFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("空库应返回 0 条，实际 %d", len(items))
	}
}

func TestListAssignmentsFilters(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewRepo(db)
	now := time.Now().UTC().Truncate(time.Second)
	if err := repo.EnsurePlatforms(ctx, now); err != nil {
		t.Fatal(err)
	}

	account, err := repo.GetAccount(ctx, entity.PlatformChaoxing)
	if err != nil {
		t.Fatal(err)
	}

	var courseID int64
	// 直接使用 SQL 插入，避免为 M0 引入写仓储。
	courseRes, err := db.SQL().ExecContext(ctx, `
		INSERT INTO courses (account_id, platform, external_id, classroom_id, name,
			semester_key, active, first_seen_at, last_seen_at)
		VALUES (?, 'chaoxing', '1001', '', '高等数学', '', 1, ?, ?)`,
		account.ID, formatTime(now), formatTime(now))
	if err != nil {
		t.Fatal(err)
	}
	courseID, err = courseRes.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}

	deadline := now.Add(24 * time.Hour)
	rows := []struct {
		platform string
		dedup    string
		title    string
		status   string
		deadline any
	}{
		{"chaoxing", "chaoxing:1001:第一次作业", "第一次作业", "unsubmitted", formatTime(deadline)},
		{"chaoxing", "chaoxing:1001:第二次作业", "第二次作业", "submitted", formatTime(deadline)},
		{"yuketang", "yuketang:2001:3001:4001", "雨课堂作业", "unknown", nil},
	}
	for _, row := range rows {
		_, err := db.SQL().ExecContext(ctx, `
			INSERT INTO assignments (account_id, platform, course_id, course_external_id,
				course_name, external_id, dedup_key, title, status, deadline_at,
				deadline_source, deadline_raw, detail_url, description,
				first_seen_at, last_seen_at, active)
			VALUES (?, ?, ?, '1001', '高等数学', '', ?, ?, ?, ?, 'platform_exact', '', '', '', ?, ?, 1)`,
			account.ID, row.platform, courseID, row.dedup, row.title, row.status, row.deadline,
			formatTime(now), formatTime(now))
		if err != nil {
			t.Fatal(err)
		}
	}

	// 默认隐藏已提交。
	items, err := repo.ListAssignments(ctx, entity.AssignmentFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("默认应返回 2 条，实际 %d", len(items))
	}

	// 显示已提交。
	items, err = repo.ListAssignments(ctx, entity.AssignmentFilter{ShowSubmitted: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("显示已提交应返回 3 条，实际 %d", len(items))
	}

	// 平台筛选。
	items, err = repo.ListAssignments(ctx, entity.AssignmentFilter{
		Platforms:     []entity.Platform{entity.PlatformYuketang},
		ShowSubmitted: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Platform != entity.PlatformYuketang {
		t.Fatalf("平台筛选失败: %#v", items)
	}

	// 状态筛选。
	items, err = repo.ListAssignments(ctx, entity.AssignmentFilter{
		Statuses:      []entity.SubmissionStatus{entity.StatusSubmitted},
		ShowSubmitted: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Status != entity.StatusSubmitted {
		t.Fatalf("状态筛选失败: %#v", items)
	}

	// 关键字。
	items, err = repo.ListAssignments(ctx, entity.AssignmentFilter{Keyword: "第一次"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("关键字筛选失败: %#v", items)
	}

	// 截止时间未知的排在最后。
	items, err = repo.ListAssignments(ctx, entity.AssignmentFilter{ShowSubmitted: true})
	if err != nil {
		t.Fatal(err)
	}
	if items[len(items)-1].Deadline != nil {
		t.Fatalf("未知截止时间应排在最后: %#v", items)
	}
}

// 只看本学期：截止时间早于学期起点的隐藏，截止时间未知的保留
func TestListAssignmentsDeadlineFrom(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewRepo(db)
	now := time.Now().UTC().Truncate(time.Second)
	if err := repo.EnsurePlatforms(ctx, now); err != nil {
		t.Fatal(err)
	}
	account, err := repo.GetAccount(ctx, entity.PlatformChaoxing)
	if err != nil {
		t.Fatal(err)
	}

	insert := func(title string, deadline any) {
		t.Helper()
		_, err := db.SQL().ExecContext(ctx, `
			INSERT INTO assignments (account_id, platform, course_id, course_external_id,
				course_name, external_id, dedup_key, title, status, deadline_at,
				deadline_source, deadline_raw, detail_url, description,
				first_seen_at, last_seen_at, active)
			VALUES (?, 'chaoxing', NULL, '1001', '高等数学', '', ?, ?, 'unsubmitted', ?,
				'platform_exact', '', '', '', ?, ?, 1)`,
			account.ID, "chaoxing:1001:"+title, title, deadline, formatTime(now), formatTime(now))
		if err != nil {
			t.Fatal(err)
		}
	}

	insert("上学期作业", formatTime(now.AddDate(0, -3, 0)))
	insert("本学期作业", formatTime(now.AddDate(0, 0, 3)))
	insert("截止时间未知", nil)

	from := now
	items, err := repo.ListAssignments(ctx, entity.AssignmentFilter{
		ShowSubmitted: true,
		DeadlineFrom:  &from,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("应保留 2 条（本学期 + 未知），实际 %d: %#v", len(items), items)
	}
	for _, item := range items {
		if item.Title == "上学期作业" {
			t.Fatalf("上学期作业不应出现: %#v", item)
		}
	}
}
