package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"

	"catcher/internal/domain"
	"catcher/internal/domain/entity"
)

// CredentialRepo 把密码凭据经 DPAPI 加密后写入 accounts.credential_blob
//
// 学习通没有可长期复用的登录态 cookie，会话失效后要用保存的手机号+密码重登
// 一次。解密失败按「没有凭据」处理，绝不回退明文
type CredentialRepo struct {
	db     *DB
	cipher domain.SecretCipher
}

func NewCredentialRepo(db *DB, cipher domain.SecretCipher) *CredentialRepo {
	return &CredentialRepo{db: db, cipher: cipher}
}

// Get 读取并解密凭据。没有保存时返回 entity.ErrNotFound
func (r *CredentialRepo) Get(ctx context.Context, accountID int64) (*entity.PasswordCredential, error) {
	var blob []byte
	err := r.db.SQL().GetContext(ctx, &blob,
		`SELECT credential_blob FROM accounts WHERE id = ?`, accountID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, entity.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("查询登录凭据失败: %w", err)
	}
	if len(blob) == 0 {
		return nil, entity.ErrNotFound
	}

	plain, err := r.cipher.Decrypt(blob)
	if err != nil {
		return nil, fmt.Errorf("解密登录凭据失败（需要重新登录）: %w", err)
	}
	var creds entity.PasswordCredential
	if err := json.Unmarshal(plain, &creds); err != nil {
		return nil, fmt.Errorf("解析登录凭据失败: %w", err)
	}
	if creds.Phone == "" || creds.Password == "" {
		return nil, entity.ErrNotFound
	}
	return &creds, nil
}

// Save 加密并写入凭据；同一账号重复保存会覆盖旧值
func (r *CredentialRepo) Save(ctx context.Context, accountID int64, creds entity.PasswordCredential, at time.Time) error {
	if creds.Phone == "" || creds.Password == "" {
		return fmt.Errorf("登录凭据不能为空")
	}
	plain, err := json.Marshal(creds)
	if err != nil {
		return fmt.Errorf("序列化登录凭据失败: %w", err)
	}
	sealed, err := r.cipher.Encrypt(plain)
	if err != nil {
		return err
	}

	return r.db.WithTx(ctx, func(tx *sqlx.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE accounts SET credential_blob = ?, updated_at = ? WHERE id = ?`,
			sealed, formatTime(at), accountID)
		if err != nil {
			return fmt.Errorf("保存登录凭据失败: %w", err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			return entity.ErrNotFound
		}
		return nil
	})
}

// Delete 清除保存的密码；不存在时也视为成功
func (r *CredentialRepo) Delete(ctx context.Context, accountID int64) error {
	return r.db.WithTx(ctx, func(tx *sqlx.Tx) error {
		_, err := tx.ExecContext(ctx,
			`UPDATE accounts SET credential_blob = NULL, updated_at = ? WHERE id = ?`,
			formatTime(time.Now()), accountID)
		if err != nil {
			return fmt.Errorf("删除登录凭据失败: %w", err)
		}
		return nil
	})
}
