package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"catcher/internal/domain"
	"catcher/internal/domain/entity"

	"github.com/jmoiron/sqlx"
)

type AIRepo struct {
	db     *DB
	cipher domain.SecretCipher
}

func NewAIRepo(db *DB, cipher domain.SecretCipher) *AIRepo { return &AIRepo{db: db, cipher: cipher} }

func (r *AIRepo) Settings(ctx context.Context) (entity.AISettings, error) {
	s := entity.DefaultAISettings()
	var row struct {
		JSON string `db:"settings_json"`
		Key  []byte `db:"key_blob"`
	}
	err := r.db.SQL().GetContext(ctx, &row, "SELECT settings_json,key_blob FROM ai_settings WHERE id=1")
	if errors.Is(err, sql.ErrNoRows) {
		return s, nil
	}
	if err != nil {
		return s, fmt.Errorf("读取 AI 设置失败")
	}
	if err = json.Unmarshal([]byte(row.JSON), &s); err != nil {
		return s, fmt.Errorf("AI 设置损坏")
	}
	if len(row.Key) > 0 {
		plain, e := r.cipher.Decrypt(row.Key)
		if e != nil {
			return s, fmt.Errorf("AI Key 无法解密，请重新配置")
		}
		s.Key = string(plain)
	}
	return s, nil
}
func (r *AIRepo) SaveSettings(ctx context.Context, s entity.AISettings) error {
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	var sealed []byte
	if s.Key != "" {
		sealed, err = r.cipher.Encrypt([]byte(s.Key))
		if err != nil {
			return fmt.Errorf("加密 AI Key 失败")
		}
	}
	return r.db.WithTx(ctx, func(tx *sqlx.Tx) error {
		_, e := tx.ExecContext(ctx, "INSERT INTO ai_settings(id,settings_json,key_blob) VALUES(1,?,?) ON CONFLICT(id) DO UPDATE SET settings_json=excluded.settings_json,key_blob=excluded.key_blob", string(raw), sealed)
		return e
	})
}

// snapshot 在一个读事务中读取事实和覆盖范围。任何已失效的选择都显式报错。
func snapshot(ctx context.Context, tx *sqlx.Tx, ids []int64) (entity.AISnapshot, error) {
	out := entity.AISnapshot{Sources: []entity.AISource{}, Coverage: []entity.AICoverage{}}
	if len(ids) == 0 || len(ids) > 40 {
		return out, fmt.Errorf("请选择 1—40 条作业")
	}
	seen := map[int64]bool{}
	for _, id := range ids {
		if id <= 0 || seen[id] {
			return out, fmt.Errorf("作业选择无效或重复")
		}
		seen[id] = true
	}
	query, args, err := sqlx.In(assignmentColumns+" WHERE active=1 AND id IN (?) ORDER BY id", ids)
	if err != nil {
		return out, err
	}
	var rows []assignmentRow
	if err = tx.SelectContext(ctx, &rows, query, args...); err != nil {
		return out, err
	}
	if len(rows) != len(ids) {
		return out, fmt.Errorf("选中的作业已变化，请重新选择")
	}
	platforms := map[string]bool{}
	hash := sha256.New()
	for i, row := range rows {
		a, e := row.toEntity()
		if e != nil {
			return out, e
		}
		var identity struct {
			Blob  []byte         `db:"secret_blob"`
			Login sql.NullString `db:"last_login_at"`
		}
		if e = tx.GetContext(ctx, &identity, "SELECT s.secret_blob,a.last_login_at FROM accounts a LEFT JOIN sessions s ON s.account_id=a.id WHERE a.id=?", a.AccountID); e != nil {
			return out, e
		}
		stamp := sha256.Sum256(append(append([]byte{}, identity.Blob...), []byte(identity.Login.String)...))
		source := entity.AISource{ID: a.ID, Ref: fmt.Sprintf("A%d", i+1), Platform: string(a.Platform), Course: a.CourseName, Title: a.Title, Status: string(a.EffectiveStatus()), PlatformStatus: string(a.Status), Manual: a.ManualStatus != nil, Deadline: a.Deadline, DeadlineSource: string(a.DeadlineSource), HasDescription: strings.TrimSpace(a.Description) != "", LastSeenAt: a.LastSeenAt, AccountStamp: hex.EncodeToString(stamp[:])}
		// last_seen_at 会随重复刷新变化，不作为作业内容变化；会话变化保守地要求重新确认。
		canonical := source
		canonical.LastSeenAt = time.Time{}
		raw, _ := json.Marshal(canonical)
		hash.Write(raw)
		hash.Write([]byte(source.AccountStamp))
		out.Sources = append(out.Sources, source)
		platforms[source.Platform] = true
	}
	names := []string{}
	for p := range platforms {
		names = append(names, p)
	}
	sort.Strings(names)
	for _, p := range names {
		var row struct {
			At       string `db:"seen_at"`
			Complete bool   `db:"complete"`
			Warnings int    `db:"warnings"`
		}
		e := tx.GetContext(ctx, &row, "SELECT seen_at,complete,warnings FROM ai_fetch_coverage WHERE platform=? ORDER BY seen_at DESC LIMIT 1", p)
		c := entity.AICoverage{Platform: p}
		if e == nil {
			at, e := parseTime(row.At)
			if e != nil {
				return out, e
			}
			c.SeenAt = &at
			c.Complete = row.Complete
			c.Warnings = row.Warnings
		} else if !errors.Is(e, sql.ErrNoRows) {
			return out, e
		}
		out.Coverage = append(out.Coverage, c)
		var latest string
		e = tx.GetContext(ctx, &latest, "SELECT status FROM fetch_runs WHERE platform=? ORDER BY id DESC LIMIT 1", p)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return out, e
		}
		out.Coverage[len(out.Coverage)-1].LatestState = latest
	}
	out.Fingerprint = hex.EncodeToString(hash.Sum(nil))
	return out, nil
}
func (r *AIRepo) Snapshot(ctx context.Context, ids []int64) (entity.AISnapshot, error) {
	tx, err := r.db.SQL().BeginTxx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return entity.AISnapshot{}, err
	}
	defer tx.Rollback()
	s, err := snapshot(ctx, tx, ids)
	if err != nil {
		return s, err
	}
	return s, tx.Commit()
}
func (r *AIRepo) Reserve(ctx context.Context, id, month string, tokens, limit int, at time.Time) error {
	return r.db.WithTx(ctx, func(tx *sqlx.Tx) error {
		var used int
		if err := tx.GetContext(ctx, &used, "SELECT COALESCE(SUM(charged_tokens+reserved_tokens),0) FROM ai_runs WHERE month=?", month); err != nil {
			return err
		}
		if tokens <= 0 || used+tokens > limit {
			return fmt.Errorf("本月 AI Token 额度不足（含在途及未确认用量的保守预留）")
		}
		_, err := tx.ExecContext(ctx, "INSERT INTO ai_runs(id,month,state,reserved_tokens,created_at) VALUES(?,?,'running',?,?)", id, month, tokens, formatTime(at))
		return err
	})
}
func (r *AIRepo) Finish(ctx context.Context, id, state string, result entity.AIModelResult, at time.Time) error {
	return r.db.WithTx(ctx, func(tx *sqlx.Tx) error {
		// 无 usage 的失败/取消按预留计费，避免网络错误导致实际费用无上限。
		_, err := tx.ExecContext(ctx, `UPDATE ai_runs SET state=?,finished_at=?,charged_tokens=CASE WHEN ? THEN ? ELSE reserved_tokens END,reserved_tokens=0 WHERE id=? AND state='running'`, state, formatTime(at), result.UsageKnown, result.InputTokens+result.OutputTokens, id)
		return err
	})
}
func (r *AIRepo) Usage(ctx context.Context, month string) (entity.AIUsage, error) {
	out := entity.AIUsage{Month: month}
	err := r.db.SQL().QueryRowxContext(ctx, "SELECT COALESCE(SUM(charged_tokens),0),COALESCE(SUM(reserved_tokens),0),COUNT(*) FROM ai_runs WHERE month=?", month).Scan(&out.ChargedTokens, &out.ReservedTokens, &out.Runs)
	return out, err
}
func (r *AIRepo) ConfirmPlan(ctx context.Context, plan entity.StudyPlan) (entity.StudyPlan, error) {
	var out entity.StudyPlan
	err := r.db.WithTx(ctx, func(tx *sqlx.Tx) error {
		var existing string
		e := tx.GetContext(ctx, &existing, "SELECT plan_json FROM study_plans WHERE id=?", plan.ID)
		if e == nil {
			return json.Unmarshal([]byte(existing), &out)
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		current, e := snapshot(ctx, tx, plan.SourceIDs)
		if e != nil {
			return e
		}
		if current.Fingerprint != plan.Fingerprint {
			return fmt.Errorf("作业或登录会话已变化，请重新生成并确认计划")
		}
		raw, e := json.Marshal(plan)
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, "INSERT INTO study_plans(id,plan_json,created_at) VALUES(?,?,?)", plan.ID, string(raw), formatTime(plan.CreatedAt))
		out = plan
		return e
	})
	return out, err
}
func (r *AIRepo) Plans(ctx context.Context) ([]entity.StudyPlan, error) {
	var rows []string
	if err := r.db.SQL().SelectContext(ctx, &rows, "SELECT plan_json FROM study_plans ORDER BY created_at DESC LIMIT 50"); err != nil {
		return nil, err
	}
	out := []entity.StudyPlan{}
	for _, raw := range rows {
		var p entity.StudyPlan
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}
func (r *AIRepo) SetTaskDone(ctx context.Context, planID, taskID string, done bool) error {
	return r.db.WithTx(ctx, func(tx *sqlx.Tx) error {
		var raw string
		if err := tx.GetContext(ctx, &raw, "SELECT plan_json FROM study_plans WHERE id=?", planID); err != nil {
			return fmt.Errorf("学习计划不存在")
		}
		var p entity.StudyPlan
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			return err
		}
		found := false
		for i := range p.Tasks {
			if p.Tasks[i].ID == taskID {
				p.Tasks[i].Done = done
				found = true
			}
		}
		if !found {
			return fmt.Errorf("学习子任务不存在")
		}
		body, err := json.Marshal(p)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "UPDATE study_plans SET plan_json=? WHERE id=?", string(body), planID)
		return err
	})
}
