package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"

	"catcher/internal/domain/entity"
)

// FetchRunRepo 记录每次平台刷新的结果与问题明细
type FetchRunRepo struct {
	db *DB
}

func NewFetchRunRepo(db *DB) *FetchRunRepo {
	return &FetchRunRepo{db: db}
}

// Create 插入一条 running 状态的刷新记录，并回填 run.ID
func (r *FetchRunRepo) Create(ctx context.Context, run *entity.FetchRun) error {
	if run == nil {
		return fmt.Errorf("刷新记录不能为空")
	}
	if !run.Platform.Valid() {
		return fmt.Errorf("刷新平台无效: %q", run.Platform)
	}
	status := run.Status
	if status == "" {
		status = entity.RunRunning
	}

	var id int64
	err := r.db.WithTx(ctx, func(tx *sqlx.Tx) error {
		res, err := tx.ExecContext(ctx, `
			INSERT INTO fetch_runs (account_id, platform, started_at, status,
				courses_total, courses_ok, items_seen, items_upserted,
				warning_count, error_summary)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			run.AccountID, string(run.Platform), formatTime(run.StartedAt), string(status),
			run.CoursesTotal, run.CoursesOK, run.ItemsSeen, run.ItemsUpserted,
			run.WarningCount, run.ErrorSummary)
		if err != nil {
			return fmt.Errorf("创建刷新记录失败: %w", err)
		}
		id, err = res.LastInsertId()
		return err
	})
	if err != nil {
		return err
	}
	run.ID = id
	run.Status = status
	return nil
}

// Finish 更新刷新终态并写入问题明细，两步在同一事务内完成
func (r *FetchRunRepo) Finish(ctx context.Context, run entity.FetchRun, issues []entity.FetchIssue, finishedAt time.Time) error {
	if run.ID <= 0 {
		return fmt.Errorf("刷新记录缺少 id")
	}
	status := run.Status
	if status == "" || status == entity.RunRunning {
		return fmt.Errorf("刷新记录终态无效: %q", run.Status)
	}

	return r.db.WithTx(ctx, func(tx *sqlx.Tx) error {
		_, err := tx.ExecContext(ctx, `
			UPDATE fetch_runs SET
				finished_at    = ?,
				status         = ?,
				courses_total  = ?,
				courses_ok     = ?,
				items_seen     = ?,
				items_upserted = ?,
				warning_count  = ?,
				error_summary  = ?
			WHERE id = ?`,
			formatTime(finishedAt), string(status),
			run.CoursesTotal, run.CoursesOK, run.ItemsSeen, run.ItemsUpserted,
			run.WarningCount, run.ErrorSummary, run.ID)
		if err != nil {
			return fmt.Errorf("更新刷新记录失败: %w", err)
		}

		for _, issue := range issues {
			_, err := tx.ExecContext(ctx, `
				INSERT INTO fetch_issues (run_id, scope, ref, kind, message, raw_path, created_at)
				VALUES (?, ?, ?, ?, ?, ?, ?)`,
				run.ID, string(issue.Scope), issue.Ref, string(issue.Kind),
				issue.Message, issue.RawPath, formatTime(finishedAt))
			if err != nil {
				return fmt.Errorf("写入刷新问题失败: %w", err)
			}
		}
		return nil
	})
}

// ListIssues 按写入顺序返回某次刷新的问题明细
func (r *FetchRunRepo) ListIssues(ctx context.Context, runID int64) ([]entity.FetchIssue, error) {
	var rows []issueRow
	if err := r.db.SQL().SelectContext(ctx, &rows, `
		SELECT scope, ref, kind, message, raw_path
		FROM fetch_issues WHERE run_id = ? ORDER BY id`, runID); err != nil {
		return nil, fmt.Errorf("查询刷新问题失败: %w", err)
	}

	out := make([]entity.FetchIssue, 0, len(rows))
	for _, row := range rows {
		out = append(out, entity.FetchIssue{
			Scope:   entity.IssueScope(row.Scope),
			Ref:     row.Ref,
			Kind:    entity.IssueKind(row.Kind),
			Message: row.Message,
			RawPath: row.RawPath,
		})
	}
	return out, nil
}

// LatestRuns 返回每个平台最近一次刷新记录
func (r *FetchRunRepo) LatestRuns(ctx context.Context) (map[entity.Platform]entity.FetchRun, error) {
	out := make(map[entity.Platform]entity.FetchRun, len(entity.AllPlatforms()))
	for _, platform := range entity.AllPlatforms() {
		var row fetchRunRow
		err := r.db.SQL().GetContext(ctx, &row, `
			SELECT id, account_id, platform, started_at, finished_at, status,
			       courses_total, courses_ok, items_seen, items_upserted,
			       warning_count, error_summary
			FROM fetch_runs WHERE platform = ? ORDER BY id DESC LIMIT 1`,
			string(platform))
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			return nil, fmt.Errorf("查询最近刷新记录失败: %w", err)
		}
		run, err := row.toEntity()
		if err != nil {
			return nil, err
		}
		out[platform] = run
	}
	return out, nil
}

type issueRow struct {
	Scope   string `db:"scope"`
	Ref     string `db:"ref"`
	Kind    string `db:"kind"`
	Message string `db:"message"`
	RawPath string `db:"raw_path"`
}

type fetchRunRow struct {
	ID            int64          `db:"id"`
	AccountID     int64          `db:"account_id"`
	Platform      string         `db:"platform"`
	StartedAt     string         `db:"started_at"`
	FinishedAt    sql.NullString `db:"finished_at"`
	Status        string         `db:"status"`
	CoursesTotal  int            `db:"courses_total"`
	CoursesOK     int            `db:"courses_ok"`
	ItemsSeen     int            `db:"items_seen"`
	ItemsUpserted int            `db:"items_upserted"`
	WarningCount  int            `db:"warning_count"`
	ErrorSummary  string         `db:"error_summary"`
}

func (r fetchRunRow) toEntity() (entity.FetchRun, error) {
	started, err := parseTime(r.StartedAt)
	if err != nil {
		return entity.FetchRun{}, fmt.Errorf("解析刷新 started_at 失败: %w", err)
	}
	finished, err := parseNullableTime(r.FinishedAt)
	if err != nil {
		return entity.FetchRun{}, fmt.Errorf("解析刷新 finished_at 失败: %w", err)
	}
	return entity.FetchRun{
		ID:            r.ID,
		AccountID:     r.AccountID,
		Platform:      entity.Platform(r.Platform),
		StartedAt:     started,
		FinishedAt:    finished,
		Status:        entity.RunStatus(r.Status),
		CoursesTotal:  r.CoursesTotal,
		CoursesOK:     r.CoursesOK,
		ItemsSeen:     r.ItemsSeen,
		ItemsUpserted: r.ItemsUpserted,
		WarningCount:  r.WarningCount,
		ErrorSummary:  r.ErrorSummary,
	}, nil
}
