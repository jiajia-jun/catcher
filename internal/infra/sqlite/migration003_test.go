package sqlite

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 003 重建四张表：旧的 001+002 库升级后数据、索引、外键都要完好，
// 并且平台列不再有 CHECK 白名单
func TestMigrate003PreservesDataAndDropsPlatformCheck(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "catcher.db")
	db, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	applyLegacySchema(t, db, 1, 2)
	seedLegacyRows(t, db)

	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	assertLegacyRowsKept(t, db)
	assertIndexes(t, db)
	assertNoForeignKeyViolation(t, db)
	assertPlatformCheckGone(t, db)
	assertCascadeDelete(t, db)

	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Fatalf("迁移前应生成备份文件: %v", err)
	}
}

// applyLegacySchema 只执行到指定版本，模拟尚未升级的旧库
func applyLegacySchema(t *testing.T, db *DB, versions ...int) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.SQL().ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    INTEGER PRIMARY KEY,
			applied_at TEXT NOT NULL
		)`); err != nil {
		t.Fatal(err)
	}
	for _, v := range versions {
		name := map[int]string{
			1: "001_init.sql",
			2: "002_assignment_manual_status.sql",
		}[v]
		body, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.SQL().ExecContext(ctx, string(body)); err != nil {
			t.Fatalf("执行 %s 失败: %v", name, err)
		}
		if _, err := db.SQL().ExecContext(ctx,
			`INSERT INTO schema_migrations(version, applied_at) VALUES (?, ?)`, v, now); err != nil {
			t.Fatal(err)
		}
	}
}

func seedLegacyRows(t *testing.T, db *DB) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	ts := formatTime(now)
	deadline := formatTime(now.Add(24 * time.Hour))

	for _, platform := range []string{"chaoxing", "yuketang", "tronclass"} {
		if _, err := db.SQL().ExecContext(ctx, `
			INSERT INTO accounts (platform, display_name, login_state, created_at, updated_at)
			VALUES (?, ?, 'connected', ?, ?)`,
			platform, platform+"-账号", ts, ts); err != nil {
			t.Fatal(err)
		}
	}

	var accountID int64
	if err := db.SQL().GetContext(ctx, &accountID,
		`SELECT id FROM accounts WHERE platform = 'chaoxing'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx,
		`INSERT INTO sessions (account_id, secret_blob, updated_at) VALUES (?, ?, ?)`,
		accountID, []byte("sealed"), ts); err != nil {
		t.Fatal(err)
	}

	runRes, err := db.SQL().ExecContext(ctx, `
		INSERT INTO fetch_runs (account_id, platform, started_at, finished_at, status,
			courses_total, courses_ok, items_seen, items_upserted, warning_count, error_summary)
		VALUES (?, 'chaoxing', ?, ?, 'partial', 2, 1, 5, 3, 2, '部分课程失败')`,
		accountID, ts, ts)
	if err != nil {
		t.Fatal(err)
	}
	runID, err := runRes.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO fetch_issues (run_id, scope, ref, kind, message, raw_path, created_at)
		VALUES (?, 'course', '1001', 'parse', '解析失败', 'raw/x.json', ?)`,
		runID, ts); err != nil {
		t.Fatal(err)
	}

	courseRes, err := db.SQL().ExecContext(ctx, `
		INSERT INTO courses (account_id, platform, external_id, classroom_id, name,
			semester_key, active, first_seen_at, last_seen_at)
		VALUES (?, 'chaoxing', '1001', '', '高等数学', '2026秋', 1, ?, ?)`,
		accountID, ts, ts)
	if err != nil {
		t.Fatal(err)
	}
	courseID, err := courseRes.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}

	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO assignments (account_id, platform, course_id, course_external_id,
			course_name, external_id, dedup_key, title, status, manual_status, manual_status_at,
			deadline_at, deadline_source, deadline_raw, detail_url, description,
			first_seen_at, last_seen_at, active, last_run_id)
		VALUES (?, 'chaoxing', ?, '1001', '高等数学', '2001', 'chaoxing:1001:2001',
			'第一次作业', 'unsubmitted', 'submitted', ?, ?, 'platform_exact', '',
			'https://example.com/a', '说明', ?, ?, 1, ?)`,
		accountID, courseID, ts, deadline, ts, ts, runID); err != nil {
		t.Fatal(err)
	}
}

func assertLegacyRowsKept(t *testing.T, db *DB) {
	t.Helper()
	ctx := context.Background()

	var accounts, sessions, runs, issues, courses, assignments int
	for query, dst := range map[string]*int{
		`SELECT COUNT(*) FROM accounts`:     &accounts,
		`SELECT COUNT(*) FROM sessions`:     &sessions,
		`SELECT COUNT(*) FROM fetch_runs`:   &runs,
		`SELECT COUNT(*) FROM fetch_issues`: &issues,
		`SELECT COUNT(*) FROM courses`:      &courses,
		`SELECT COUNT(*) FROM assignments`:  &assignments,
	} {
		if err := db.SQL().GetContext(ctx, dst, query); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
	}
	if accounts != 3 || sessions != 1 || runs != 1 || issues != 1 || courses != 1 || assignments != 1 {
		t.Fatalf("行数不符 accounts=%d sessions=%d runs=%d issues=%d courses=%d assignments=%d",
			accounts, sessions, runs, issues, courses, assignments)
	}

	var title, status, manual string
	var courseID int64
	if err := db.SQL().QueryRowContext(ctx, `
		SELECT title, status, manual_status, course_id FROM assignments`).
		Scan(&title, &status, &manual, &courseID); err != nil {
		t.Fatal(err)
	}
	if title != "第一次作业" || status != "unsubmitted" || manual != "submitted" || courseID == 0 {
		t.Fatalf("作业字段丢失: title=%q status=%q manual=%q courseID=%d",
			title, status, manual, courseID)
	}
}

func assertIndexes(t *testing.T, db *DB) {
	t.Helper()
	want := []string{
		"idx_courses_account_active",
		"idx_assignments_active_deadline",
		"idx_assignments_account_platform",
		"idx_assignments_status_deadline",
		"idx_assignments_course",
		"idx_fetch_runs_account_started",
		"idx_fetch_issues_run",
	}
	for _, name := range want {
		var count int
		if err := db.SQL().GetContext(context.Background(), &count,
			`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?`, name); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("索引 %s 丢失", name)
		}
	}
}

func assertNoForeignKeyViolation(t *testing.T, db *DB) {
	t.Helper()
	rows, err := db.SQL().QueryContext(context.Background(), `PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	if rows.Next() {
		t.Fatal("迁移后存在外键违规")
	}
}

// 平台白名单已经交给 Go 侧校验，库里不再拦未知取值
func assertPlatformCheckGone(t *testing.T, db *DB) {
	t.Helper()
	ctx := context.Background()
	ts := formatTime(time.Now())
	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO accounts (platform, display_name, login_state, created_at, updated_at)
		VALUES ('pigai', '', 'disconnected', ?, ?)`, ts, ts); err != nil {
		t.Fatalf("应允许写入新平台: %v", err)
	}

	var keep int
	if err := db.SQL().GetContext(ctx, &keep,
		`SELECT COUNT(*) FROM pragma_table_info('assignments') WHERE name = 'manual_status'`); err != nil {
		t.Fatal(err)
	}
	if keep != 1 {
		t.Fatal("assignments 缺少 manual_status 列")
	}
}

// 重建后外键动作仍然有效：删账号会级联删会话、刷新记录、课程、作业
func assertCascadeDelete(t *testing.T, db *DB) {
	t.Helper()
	ctx := context.Background()
	if _, err := db.SQL().ExecContext(ctx,
		`DELETE FROM accounts WHERE platform = 'chaoxing'`); err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for label, query := range map[string]string{
		"sessions":     `SELECT COUNT(*) FROM sessions`,
		"fetch_runs":   `SELECT COUNT(*) FROM fetch_runs`,
		"fetch_issues": `SELECT COUNT(*) FROM fetch_issues`,
		"courses":      `SELECT COUNT(*) FROM courses`,
		"assignments":  `SELECT COUNT(*) FROM assignments`,
	} {
		var n int
		if err := db.SQL().GetContext(ctx, &n, query); err != nil {
			t.Fatal(err)
		}
		counts[label] = n
	}
	for label, n := range counts {
		if n != 0 {
			t.Fatalf("级联删除后 %s 仍有 %d 行", label, n)
		}
	}
}
