package sqlite

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"catcher/internal/domain/entity"
)

func TestCredentialRepoRoundTrip(t *testing.T) {
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

	credentials := NewCredentialRepo(db, testCipher{})
	if _, err := credentials.Get(ctx, account.ID); !errors.Is(err, entity.ErrNotFound) {
		t.Fatalf("空库应返回 ErrNotFound，实际 %v", err)
	}

	creds := entity.PasswordCredential{Phone: "13800138000", Password: "s3cret"}
	if err := credentials.Save(ctx, account.ID, creds, now); err != nil {
		t.Fatal(err)
	}

	got, err := credentials.Get(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if *got != creds {
		t.Fatalf("读回 = %#v, want %#v", *got, creds)
	}

	var blob []byte
	if err := db.SQL().GetContext(ctx, &blob,
		`SELECT credential_blob FROM accounts WHERE id = ?`, account.ID); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(blob, []byte("s3cret")) || bytes.Contains(blob, []byte("13800138000")) {
		t.Fatal("凭据落库必须是密文")
	}
}

func TestCredentialRepoDelete(t *testing.T) {
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

	credentials := NewCredentialRepo(db, testCipher{})
	if err := credentials.Save(ctx, account.ID,
		entity.PasswordCredential{Phone: "13800138000", Password: "pw"}, now); err != nil {
		t.Fatal(err)
	}
	if err := credentials.Delete(ctx, account.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := credentials.Get(ctx, account.ID); !errors.Is(err, entity.ErrNotFound) {
		t.Fatalf("删除后应返回 ErrNotFound，实际 %v", err)
	}
	// 幂等：重复删除不是错误
	if err := credentials.Delete(ctx, account.ID); err != nil {
		t.Fatal(err)
	}
}

func TestCredentialRepoCorruptBlobDoesNotFallBack(t *testing.T) {
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

	if _, err := db.SQL().ExecContext(ctx,
		`UPDATE accounts SET credential_blob = ? WHERE id = ?`, []byte{0x01, 0x02}, account.ID); err != nil {
		t.Fatal(err)
	}

	credentials := NewCredentialRepo(db, testCipher{})
	_, err = credentials.Get(ctx, account.ID)
	if err == nil || errors.Is(err, entity.ErrNotFound) {
		t.Fatalf("解密失败必须返回错误而不是当作没有凭据: %v", err)
	}
}

func TestCredentialRepoRejectsEmpty(t *testing.T) {
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

	credentials := NewCredentialRepo(db, testCipher{})
	if err := credentials.Save(ctx, account.ID, entity.PasswordCredential{}, now); err == nil {
		t.Fatal("空凭据不应写入")
	}
}
