package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"catcher/internal/domain/entity"
)

type aiCipher struct{}

func (aiCipher) Encrypt(v []byte) ([]byte, error) {
	out := []byte("sealed:")
	for _, b := range v {
		out = append(out, b^0x55)
	}
	return out, nil
}
func (aiCipher) Decrypt(v []byte) ([]byte, error) {
	out := []byte{}
	for _, b := range v[7:] {
		out = append(out, b^0x55)
	}
	return out, nil
}
func TestAIMigrationUpgradesLegacyWithoutChangingData(t *testing.T) {
	ctx := context.Background()
	db, e := Open(ctx, filepath.Join(t.TempDir(), "legacy.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	applyLegacySchema(t, db, 1, 2)
	seedLegacyRows(t, db)
	body, e := migrationsFS.ReadFile("migrations/003_rebuild_without_platform_check.sql")
	if e != nil {
		t.Fatal(e)
	}
	if e = db.applyMigration(ctx, 3, "003", string(body)); e != nil {
		t.Fatal(e)
	}
	if e = db.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	assertLegacyRowsKept(t, db)
	assertNoForeignKeyViolation(t, db)
	var version int
	if e = db.SQL().GetContext(ctx, &version, "SELECT MAX(version) FROM schema_migrations"); e != nil || version != 4 {
		t.Fatal(version, e)
	}
	if e = db.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	cfg, e := NewAIRepo(db, aiCipher{}).Settings(ctx)
	if e != nil || cfg.Enabled || cfg.Key != "" {
		t.Fatal("旧库升级后 AI 必须默认关闭", cfg, e)
	}
}
func TestAISettingsEncryptedAndBudgetAtomic(t *testing.T) {
	db := openTestDB(t)
	repo := NewAIRepo(db, aiCipher{})
	ctx := context.Background()
	cfg := entity.DefaultAISettings()
	cfg.Key = "FAKE_SECRET_NOT_PLAINTEXT"
	if e := repo.SaveSettings(ctx, cfg); e != nil {
		t.Fatal(e)
	}
	var raw string
	var blob []byte
	if e := db.SQL().QueryRowxContext(ctx, "SELECT settings_json,key_blob FROM ai_settings").Scan(&raw, &blob); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(raw, cfg.Key) || strings.Contains(string(blob), cfg.Key) {
		t.Fatal("Key 不应明文落库")
	}
	loaded, e := repo.Settings(ctx)
	if e != nil || loaded.Key != cfg.Key {
		t.Fatal(e)
	}
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if repo.Reserve(ctx, string(rune('a'+i)), "2026-10", 100, 500, time.Now()) == nil {
				accepted.Add(1)
			}
		}(i)
	}
	wg.Wait()
	usage, e := repo.Usage(ctx, "2026-10")
	if e != nil || accepted.Load() != 5 || usage.ReservedTokens != 500 {
		t.Fatal("并发预算必须原子预留", accepted.Load(), usage, e)
	}
}
