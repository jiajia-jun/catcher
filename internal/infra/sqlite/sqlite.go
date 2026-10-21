// Package sqlite 提供 sqlite 连接、迁移与仓储实现。
//
// 所有写操作经过 DB.WithTx 的单一写锁串行化；读可以直接使用 SQL()，
// 在 WAL 模式下读写互不阻塞。
package sqlite

import (
	"context"
	"embed"
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
	return &DB{db: db}, nil
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

	for _, m := range list {
		if m.version <= current {
			continue
		}
		body, err := migrationsFS.ReadFile("migrations/" + m.name)
		if err != nil {
			return fmt.Errorf("读取迁移 %s 失败: %w", m.name, err)
		}
		if err := d.WithTx(ctx, func(tx *sqlx.Tx) error {
			if _, err := tx.ExecContext(ctx, string(body)); err != nil {
				return fmt.Errorf("执行迁移 %s 失败: %w", m.name, err)
			}
			_, err := tx.ExecContext(ctx,
				`INSERT INTO schema_migrations(version, applied_at) VALUES (?, ?)`,
				m.version, time.Now().UTC().Format(time.RFC3339))
			return err
		}); err != nil {
			return err
		}
	}
	return nil
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
