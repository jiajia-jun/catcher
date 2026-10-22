package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"

	"catcher/internal/domain/entity"
)

// Repo 是 domain 端口在 sqlite 上的实现。
type Repo struct {
	db *DB
}

func NewRepo(db *DB) *Repo {
	return &Repo{db: db}
}

// EnsurePlatforms 保证每个平台恰好有一行账号，重复调用无副作用。
func (r *Repo) EnsurePlatforms(ctx context.Context, now time.Time) error {
	return r.db.WithTx(ctx, func(tx *sqlx.Tx) error {
		for _, p := range entity.AllPlatforms() {
			_, err := tx.ExecContext(ctx, `
				INSERT INTO accounts (platform, display_name, login_state, created_at, updated_at)
				VALUES (?, '', 'disconnected', ?, ?)
				ON CONFLICT(platform) DO NOTHING`,
				string(p), formatTime(now), formatTime(now))
			if err != nil {
				return fmt.Errorf("初始化平台账号 %s 失败: %w", p, err)
			}
		}
		return nil
	})
}

func (r *Repo) ListAccounts(ctx context.Context) ([]entity.Account, error) {
	var rows []accountRow
	err := r.db.SQL().SelectContext(ctx, &rows, `
		SELECT id, platform, display_name, login_state, last_login_at, created_at, updated_at
		FROM accounts
		ORDER BY CASE platform
			WHEN 'chaoxing' THEN 1
			WHEN 'yuketang' THEN 2
			WHEN 'tronclass' THEN 3
			ELSE 9 END`)
	if err != nil {
		return nil, fmt.Errorf("查询账号失败: %w", err)
	}

	out := make([]entity.Account, 0, len(rows))
	for _, row := range rows {
		account, err := row.toEntity()
		if err != nil {
			return nil, err
		}
		out = append(out, account)
	}
	return out, nil
}

func (r *Repo) GetAccount(ctx context.Context, platform entity.Platform) (*entity.Account, error) {
	var row accountRow
	err := r.db.SQL().GetContext(ctx, &row, `
		SELECT id, platform, display_name, login_state, last_login_at, created_at, updated_at
		FROM accounts WHERE platform = ?`, string(platform))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, entity.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("查询账号失败: %w", err)
	}
	account, err := row.toEntity()
	if err != nil {
		return nil, err
	}
	return &account, nil
}

func (r *Repo) SetLoginState(ctx context.Context, platform entity.Platform, state entity.LoginState, at time.Time) error {
	var affected int64
	err := r.db.WithTx(ctx, func(tx *sqlx.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE accounts SET login_state = ?, updated_at = ? WHERE platform = ?`,
			string(state), formatTime(at), string(platform))
		if err != nil {
			return err
		}
		affected, err = res.RowsAffected()
		return err
	})
	if err != nil {
		return fmt.Errorf("更新登录态失败: %w", err)
	}
	if affected == 0 {
		return entity.ErrNotFound
	}
	return nil
}

// SetDisplayName 写入脱敏后的账号标识，并更新最近登录时间
func (r *Repo) SetDisplayName(ctx context.Context, platform entity.Platform, name string, at time.Time) error {
	var affected int64
	err := r.db.WithTx(ctx, func(tx *sqlx.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE accounts SET display_name = ?, last_login_at = ?, updated_at = ? WHERE platform = ?`,
			name, formatTime(at), formatTime(at), string(platform))
		if err != nil {
			return err
		}
		affected, err = res.RowsAffected()
		return err
	})
	if err != nil {
		return fmt.Errorf("更新账号显示名失败: %w", err)
	}
	if affected == 0 {
		return entity.ErrNotFound
	}
	return nil
}

// ListAssignments 支持平台、课程、状态、关键字筛选；默认隐藏已提交作业。
func (r *Repo) ListAssignments(ctx context.Context, filter entity.AssignmentFilter) ([]entity.Assignment, error) {
	query := `
		SELECT id, account_id, platform, COALESCE(course_id, 0) AS course_id,
		       course_external_id, course_name, external_id, dedup_key, title,
		       status, deadline_at, deadline_source, deadline_raw, detail_url,
		       description, first_seen_at, last_seen_at, active,
		       COALESCE(last_run_id, 0) AS last_run_id
		FROM assignments
		WHERE active = 1`
	var args []any

	if len(filter.Platforms) > 0 {
		query += ` AND platform IN (?)`
		args = append(args, platformStrings(filter.Platforms))
	}
	if len(filter.CourseIDs) > 0 {
		query += ` AND course_id IN (?)`
		args = append(args, filter.CourseIDs)
	}
	if len(filter.Statuses) > 0 {
		query += ` AND status IN (?)`
		args = append(args, statusStrings(filter.Statuses))
	} else if !filter.ShowSubmitted {
		query += ` AND status <> 'submitted'`
	}
	if filter.DeadlineFrom != nil {
		// 截止时间未知的作业要保留：读不到时间不等于不是本学期的作业
		query += ` AND (deadline_at IS NULL OR deadline_at >= ?)`
		args = append(args, formatTime(*filter.DeadlineFrom))
	}
	if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
		query += ` AND (title LIKE ? OR course_name LIKE ?)`
		like := "%" + keyword + "%"
		args = append(args, like, like)
	}
	query += ` ORDER BY CASE WHEN deadline_at IS NULL THEN 1 ELSE 0 END, deadline_at ASC, id ASC`

	var err error
	if len(args) > 0 {
		query, args, err = sqlx.In(query, args...)
		if err != nil {
			return nil, fmt.Errorf("构造查询失败: %w", err)
		}
	}
	query = r.db.SQL().Rebind(query)

	var rows []assignmentRow
	if err := r.db.SQL().SelectContext(ctx, &rows, query, args...); err != nil {
		return nil, fmt.Errorf("查询作业失败: %w", err)
	}

	out := make([]entity.Assignment, 0, len(rows))
	for _, row := range rows {
		a, err := row.toEntity()
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

type accountRow struct {
	ID          int64          `db:"id"`
	Platform    string         `db:"platform"`
	DisplayName string         `db:"display_name"`
	LoginState  string         `db:"login_state"`
	LastLoginAt sql.NullString `db:"last_login_at"`
	CreatedAt   string         `db:"created_at"`
	UpdatedAt   string         `db:"updated_at"`
}

func (r accountRow) toEntity() (entity.Account, error) {
	lastLogin, err := parseNullableTime(r.LastLoginAt)
	if err != nil {
		return entity.Account{}, fmt.Errorf("解析账号 last_login_at 失败: %w", err)
	}
	created, err := parseTime(r.CreatedAt)
	if err != nil {
		return entity.Account{}, fmt.Errorf("解析账号 created_at 失败: %w", err)
	}
	updated, err := parseTime(r.UpdatedAt)
	if err != nil {
		return entity.Account{}, fmt.Errorf("解析账号 updated_at 失败: %w", err)
	}
	return entity.Account{
		ID:          r.ID,
		Platform:    entity.Platform(r.Platform),
		DisplayName: r.DisplayName,
		LoginState:  entity.LoginState(r.LoginState),
		LastLoginAt: lastLogin,
		CreatedAt:   created,
		UpdatedAt:   updated,
	}, nil
}

type assignmentRow struct {
	ID               int64          `db:"id"`
	AccountID        int64          `db:"account_id"`
	Platform         string         `db:"platform"`
	CourseID         int64          `db:"course_id"`
	CourseExternalID string         `db:"course_external_id"`
	CourseName       string         `db:"course_name"`
	ExternalID       string         `db:"external_id"`
	DedupKey         string         `db:"dedup_key"`
	Title            string         `db:"title"`
	Status           string         `db:"status"`
	DeadlineAt       sql.NullString `db:"deadline_at"`
	DeadlineSource   string         `db:"deadline_source"`
	DeadlineRaw      string         `db:"deadline_raw"`
	DetailURL        string         `db:"detail_url"`
	Description      string         `db:"description"`
	FirstSeenAt      string         `db:"first_seen_at"`
	LastSeenAt       string         `db:"last_seen_at"`
	Active           int            `db:"active"`
	LastRunID        int64          `db:"last_run_id"`
}

func (r assignmentRow) toEntity() (entity.Assignment, error) {
	deadline, err := parseNullableTime(r.DeadlineAt)
	if err != nil {
		return entity.Assignment{}, fmt.Errorf("解析作业 deadline_at 失败: %w", err)
	}
	firstSeen, err := parseTime(r.FirstSeenAt)
	if err != nil {
		return entity.Assignment{}, fmt.Errorf("解析作业 first_seen_at 失败: %w", err)
	}
	lastSeen, err := parseTime(r.LastSeenAt)
	if err != nil {
		return entity.Assignment{}, fmt.Errorf("解析作业 last_seen_at 失败: %w", err)
	}
	return entity.Assignment{
		ID:               r.ID,
		AccountID:        r.AccountID,
		Platform:         entity.Platform(r.Platform),
		CourseID:         r.CourseID,
		CourseExternalID: r.CourseExternalID,
		CourseName:       r.CourseName,
		ExternalID:       r.ExternalID,
		DedupKey:         r.DedupKey,
		Title:            r.Title,
		Status:           entity.SubmissionStatus(r.Status),
		Deadline:         deadline,
		DeadlineSource:   entity.DeadlineSource(r.DeadlineSource),
		DeadlineRaw:      r.DeadlineRaw,
		DetailURL:        r.DetailURL,
		Description:      r.Description,
		FirstSeenAt:      firstSeen,
		LastSeenAt:       lastSeen,
		Active:           r.Active == 1,
		LastRunID:        r.LastRunID,
	}, nil
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

func parseTime(s string) (time.Time, error) {
	return time.Parse(time.RFC3339, s)
}

func parseNullableTime(v sql.NullString) (*time.Time, error) {
	if !v.Valid || v.String == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, v.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func platformStrings(platforms []entity.Platform) []string {
	out := make([]string, len(platforms))
	for i, p := range platforms {
		out[i] = string(p)
	}
	return out
}

func statusStrings(statuses []entity.SubmissionStatus) []string {
	out := make([]string, len(statuses))
	for i, s := range statuses {
		out[i] = string(s)
	}
	return out
}
