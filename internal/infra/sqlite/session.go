package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"

	"catcher/internal/domain"
	"catcher/internal/domain/entity"
)

// SessionRepo 把 entity.Session 的秘密 JSON 经 DPAPI 加密后存入 sessions 表
//
// 明文只在进程内存在：读出时解密，写入时加密。解密失败按「登录态不可用」处理，
// 由上层要求重新登录，绝不回退明文或返回旧值
type SessionRepo struct {
	db     *DB
	cipher domain.SecretCipher
}

func NewSessionRepo(db *DB, cipher domain.SecretCipher) *SessionRepo {
	return &SessionRepo{db: db, cipher: cipher}
}

// Get 读取并解密会话。没有会话时返回 entity.ErrNotFound
func (r *SessionRepo) Get(ctx context.Context, accountID int64) (*entity.Session, error) {
	var row sessionRow
	err := r.db.SQL().GetContext(ctx, &row, `
		SELECT s.account_id, a.platform, s.secret_blob, s.updated_at
		FROM sessions s
		JOIN accounts a ON a.id = s.account_id
		WHERE s.account_id = ?`, accountID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, entity.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("查询会话失败: %w", err)
	}

	plain, err := r.cipher.Decrypt(row.SecretBlob)
	if err != nil {
		return nil, fmt.Errorf("解密会话失败（需要重新登录）: %w", err)
	}
	updated, err := parseTime(row.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("解析会话 updated_at 失败: %w", err)
	}
	return &entity.Session{
		AccountID: row.AccountID,
		Platform:  entity.Platform(row.Platform),
		Secret:    plain,
		UpdatedAt: updated,
	}, nil
}

// Save 加密并写入会话；同一账号重复保存会覆盖旧值
func (r *SessionRepo) Save(ctx context.Context, accountID int64, secret []byte, expiresAt *time.Time, at time.Time) error {
	if len(secret) == 0 {
		return fmt.Errorf("会话秘密不能为空")
	}
	sealed, err := r.cipher.Encrypt(secret)
	if err != nil {
		return err
	}

	var exp any
	if expiresAt != nil {
		exp = formatTime(*expiresAt)
	}
	return r.db.WithTx(ctx, func(tx *sqlx.Tx) error {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO sessions (account_id, secret_blob, expires_at, updated_at)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(account_id) DO UPDATE SET
				secret_blob = excluded.secret_blob,
				expires_at  = excluded.expires_at,
				updated_at  = excluded.updated_at`,
			accountID, sealed, exp, formatTime(at))
		if err != nil {
			return fmt.Errorf("保存会话失败: %w", err)
		}
		return nil
	})
}

// Delete 删除会话；不存在时也视为成功
func (r *SessionRepo) Delete(ctx context.Context, accountID int64) error {
	return r.db.WithTx(ctx, func(tx *sqlx.Tx) error {
		_, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE account_id = ?`, accountID)
		if err != nil {
			return fmt.Errorf("删除会话失败: %w", err)
		}
		return nil
	})
}

type sessionRow struct {
	AccountID  int64  `db:"account_id"`
	Platform   string `db:"platform"`
	SecretBlob []byte `db:"secret_blob"`
	UpdatedAt  string `db:"updated_at"`
}
