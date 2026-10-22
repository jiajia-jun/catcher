package sqlite

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"catcher/internal/domain/entity"
)

// testCipher 是可逆的测试加密，避免单测依赖当前用户的 DPAPI 密钥
type testCipher struct{}

func (testCipher) Encrypt(plain []byte) ([]byte, error) {
	out := make([]byte, len(plain))
	for i := range plain {
		out[i] = plain[i] ^ 0x5a
	}
	return out, nil
}

func (testCipher) Decrypt(sealed []byte) ([]byte, error) {
	if len(sealed) == 0 {
		return nil, errors.New("空密文")
	}
	return testCipher{}.Encrypt(sealed)
}

func TestSessionRepoRoundTrip(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewRepo(db)
	now := time.Now().UTC().Truncate(time.Second)
	if err := repo.EnsurePlatforms(ctx, now); err != nil {
		t.Fatal(err)
	}
	account, err := repo.GetAccount(ctx, entity.PlatformYuketang)
	if err != nil {
		t.Fatal(err)
	}

	sessions := NewSessionRepo(db, testCipher{})
	if _, err := sessions.Get(ctx, account.ID); !errors.Is(err, entity.ErrNotFound) {
		t.Fatalf("空库应返回 ErrNotFound，实际 %v", err)
	}

	secret, _ := json.Marshal(entity.YuketangSecret{SessionID: "sid", Auth: "auth", UserID: "42"})
	if err := sessions.Save(ctx, account.ID, secret, nil, now); err != nil {
		t.Fatal(err)
	}

	got, err := sessions.Get(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Platform != entity.PlatformYuketang {
		t.Fatalf("platform = %s", got.Platform)
	}
	if !bytes.Equal(got.Secret, secret) {
		t.Fatalf("secret 不一致: %s", got.Secret)
	}

	// 覆盖保存不产生第二行
	next := []byte(`{"sessionid":"sid2"}`)
	if err := sessions.Save(ctx, account.ID, next, nil, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.SQL().GetContext(ctx, &count, `SELECT COUNT(*) FROM sessions`); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("sessions 行数 = %d, want 1", count)
	}
	got, err = sessions.Get(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Secret, next) {
		t.Fatalf("覆盖后 secret = %s", got.Secret)
	}
}

func TestSessionRepoStoresCiphertext(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewRepo(db)
	now := time.Now().UTC()
	if err := repo.EnsurePlatforms(ctx, now); err != nil {
		t.Fatal(err)
	}
	account, err := repo.GetAccount(ctx, entity.PlatformYuketang)
	if err != nil {
		t.Fatal(err)
	}

	plain := []byte(`{"sessionid":"明文凭据"}`)
	sessions := NewSessionRepo(db, testCipher{})
	if err := sessions.Save(ctx, account.ID, plain, nil, now); err != nil {
		t.Fatal(err)
	}

	var blob []byte
	if err := db.SQL().GetContext(ctx, &blob, `SELECT secret_blob FROM sessions WHERE account_id = ?`, account.ID); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(blob, []byte("明文凭据")) {
		t.Fatal("数据库里不应出现明文凭据")
	}

	if err := sessions.Delete(ctx, account.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := sessions.Get(ctx, account.ID); !errors.Is(err, entity.ErrNotFound) {
		t.Fatalf("删除后应返回 ErrNotFound，实际 %v", err)
	}
}
