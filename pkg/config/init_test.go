package config

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestSaveUISettingsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cfg, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}

	cfg.UI.EnabledPlatforms = []string{"chaoxing", "pigai"}
	cfg.UI.ChangelogSeenVersion = "1.3.0"
	path := filepath.Join(dir, FileName+"."+FileExt)
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}

	got, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.UI.EnabledPlatforms, []string{"chaoxing", "pigai"}) {
		t.Fatalf("勾选平台未持久化: %#v", got.UI.EnabledPlatforms)
	}
	if got.UI.ChangelogSeenVersion != "1.3.0" {
		t.Fatalf("已读版本未持久化: %q", got.UI.ChangelogSeenVersion)
	}
}
