// Package sqlite 提供 sqlite 连接、迁移与仓储实现。
//
// 所有写操作经过 DB.WithTx 的单一写锁串行化；读可以直接使用 SQL()，
// 在 WAL 模式下读写互不阻塞。
package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// DB 包装 sqlx.DB，并持有一把写锁。
type DB struct {
	db      *sqlx.DB
	path    string
	writeMu sync.Mutex
}

// Open 打开数据库并验证连接。调用方负责 Close。
func Open(ctx context.Context, path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("创建数据库目录失败: %w", err)
	}

	db, err := sqlx.Open("sqlite", dsn(path))
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(0)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("连接数据库失败: %w", err)
	}
	return &DB{db: db, path: path}, nil
}

// SQL 暴露底层连接，供仓储读取使用。
func (d *DB) SQL() *sqlx.DB {
	return d.db
}

// WithTx 在写锁内执行一个事务。任何错误都会回滚。
func (d *DB) WithTx(ctx context.Context, fn func(*sqlx.Tx) error) error {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()

	tx, err := d.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开启事务失败: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交事务失败: %w", err)
	}
	return nil
}

// Close 关闭数据库连接。
func (d *DB) Close() error {
	if d == nil || d.db == nil {
		return nil
	}
	return d.db.Close()
}

// Migrate 按 schema_migrations.version 顺序执行嵌入的迁移，只前进不回滚。
func (d *DB) Migrate(ctx context.Context) error {
	if _, err := d.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    INTEGER PRIMARY KEY,
			applied_at TEXT NOT NULL
		)`); err != nil {
		return fmt.Errorf("创建迁移表失败: %w", err)
	}

	var current int
	if err := d.db.GetContext(ctx, &current,
		`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`); err != nil {
		return fmt.Errorf("读取迁移版本失败: %w", err)
	}

	type migration struct {
		version int
		name    string
	}
	var list []migration
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("读取迁移目录失败: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		version, err := migrationVersion(entry.Name())
		if err != nil {
			return err
		}
		list = append(list, migration{version: version, name: entry.Name()})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].version < list[j].version })

	var pending []migration
	for _, m := range list {
		if m.version > current {
			pending = append(pending, m)
		}
	}
	if len(pending) == 0 {
		return nil
	}
	// 只在已有结构上升级时留底；全新库第一次建表没有可回滚的数据
	if current > 0 {
		if err := d.backup(ctx); err != nil {
			return err
		}
	}

	for _, m := range pending {
		body, err := migrationsFS.ReadFile("migrations/" + m.name)
		if err != nil {
			return fmt.Errorf("读取迁移 %s 失败: %w", m.name, err)
		}
		if err := d.applyMigration(ctx, m.version, m.name, string(body)); err != nil {
			return err
		}
	}
	return nil
}

// backup 把当前数据库复制为 catcher.db.bak。
// 先把 WAL 落进主库，否则复制出来的文件可能缺少未提交的页。
func (d *DB) backup(ctx context.Context) error {
	if d.path == "" {
		return nil
	}
	if _, err := d.db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		return fmt.Errorf("迁移前检查点失败: %w", err)
	}
	data, err := os.ReadFile(d.path)
	if err != nil {
		return fmt.Errorf("迁移前读取数据库失败: %w", err)
	}
	dst := d.path + ".bak"
	if err := os.WriteFile(dst, data, 0o600); err != nil {
		return fmt.Errorf("迁移前备份数据库失败: %w", err)
	}
	return nil
}

// applyMigration 执行单个迁移并记录版本。
//
// 迁移文件首行写成 `-- catcher:foreign_keys=off` 时，改用独立连接先关闭外键再执行，
// 因为重建表需要对被引用表执行 DROP TABLE。WithTx 里改不了外键：
// PRAGMA foreign_keys 在事务内是空操作，且连接池中的其他连接仍处于开启状态。
func (d *DB) applyMigration(ctx context.Context, version int, name, body string) error {
	if !needsForeignKeysOff(body) {
		return d.WithTx(ctx, func(tx *sqlx.Tx) error {
			if _, err := tx.ExecContext(ctx, body); err != nil {
				return fmt.Errorf("执行迁移 %s 失败: %w", name, err)
			}
			_, err := tx.ExecContext(ctx,
				`INSERT INTO schema_migrations(version, applied_at) VALUES (?, ?)`,
				version, time.Now().UTC().Format(time.RFC3339))
			return err
		})
	}

	d.writeMu.Lock()
	defer d.writeMu.Unlock()

	conn, err := d.db.Connx(ctx)
	if err != nil {
		return fmt.Errorf("迁移 %s 获取连接失败: %w", name, err)
	}
	defer func() { _ = conn.Close() }()

	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		return fmt.Errorf("迁移 %s 关闭外键失败: %w", name, err)
	}
	// 恢复外键必须无条件执行，且不能受 ctx 取消影响，
	// 否则这条连接带着关闭状态回到连接池，后续写入全部失去外键保护
	defer func() {
		_, _ = conn.ExecContext(context.Background(), `PRAGMA foreign_keys = ON`)
	}()

	tx, err := conn.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("迁移 %s 开启事务失败: %w", name, err)
	}
	if _, err := tx.ExecContext(ctx, body); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("执行迁移 %s 失败: %w", name, err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations(version, applied_at) VALUES (?, ?)`,
		version, time.Now().UTC().Format(time.RFC3339)); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("记录迁移 %s 失败: %w", name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交迁移 %s 失败: %w", name, err)
	}

	// 外键关闭期间写坏的关系不会报错，提交后必须主动查一遍
	var table string
	err = conn.GetContext(ctx, &table, `SELECT "table" FROM pragma_foreign_key_check LIMIT 1`)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("迁移 %s 外键自检失败: %w", name, err)
	}
	if table != "" {
		return fmt.Errorf("迁移 %s 后存在外键违规: 表 %s", name, table)
	}
	return nil
}

// needsForeignKeysOff 识别迁移文件首行的执行指令
func needsForeignKeysOff(body string) bool {
	line, _, _ := strings.Cut(body, "\n")
	return strings.Contains(line, "catcher:foreign_keys=off")
}

func migrationVersion(name string) (int, error) {
	prefix, _, ok := strings.Cut(name, "_")
	if !ok {
		return 0, fmt.Errorf("迁移文件名缺少版本前缀: %s", name)
	}
	v, err := strconv.Atoi(prefix)
	if err != nil || v <= 0 {
		return 0, fmt.Errorf("迁移文件名版本无效: %s", name)
	}
	return v, nil
}

// dsn 把 Windows / POSIX 路径转成 modernc sqlite 的 file: URL，
// 并通过 _pragma 让每个新连接都带上必要设置。
func dsn(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	p := filepath.ToSlash(abs)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}

	u := &url.URL{Scheme: "file", Path: p}
	q := u.Query()
	for _, pragma := range []string{
		"journal_mode(WAL)",
		"foreign_keys(1)",
		"busy_timeout(5000)",
		"synchronous(NORMAL)",
	} {
		q.Add("_pragma", pragma)
	}
	u.RawQuery = q.Encode()
	return u.String()
}
