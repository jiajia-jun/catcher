package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveDataDirPrecedence(t *testing.T) {
	root := t.TempDir()
	exeDir := filepath.Join(root, "exe")
	cwd := filepath.Join(root, "cwd")
	if err := os.MkdirAll(exeDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cwd, 0o750); err != nil {
		t.Fatal(err)
	}

	base := DataDirOptions{
		Getenv:     func(string) string { return "" },
		Executable: func() (string, error) { return filepath.Join(exeDir, "Catcher.exe"), nil },
		WorkingDir: func() (string, error) { return cwd, nil },
	}

	t.Run("flag wins", func(t *testing.T) {
		opts := base
		opts.Args = []string{"--data-dir", filepath.Join(root, "flag")}
		opts.Getenv = func(string) string { return filepath.Join(root, "env") }
		got, err := ResolveDataDir(opts)
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(root, "flag")
		if got != want {
			t.Fatalf("got %s, want %s", got, want)
		}
	})

	t.Run("env wins over exe", func(t *testing.T) {
		opts := base
		opts.Getenv = func(string) string { return filepath.Join(root, "env") }
		got, err := ResolveDataDir(opts)
		if err != nil {
			t.Fatal(err)
		}
		if got != filepath.Join(root, "env") {
			t.Fatalf("got %s", got)
		}
	})

	t.Run("dev uses cwd", func(t *testing.T) {
		opts := base
		opts.DevMode = true
		got, err := ResolveDataDir(opts)
		if err != nil {
			t.Fatal(err)
		}
		if got != filepath.Join(cwd, "data") {
			t.Fatalf("got %s", got)
		}
	})

	t.Run("release uses exe dir", func(t *testing.T) {
		opts := base
		got, err := ResolveDataDir(opts)
		if err != nil {
			t.Fatal(err)
		}
		if got != filepath.Join(exeDir, "data") {
			t.Fatalf("got %s", got)
		}
	})
}

func TestResolveDataDirRejectsUnknownArgs(t *testing.T) {
	_, err := ResolveDataDir(DataDirOptions{
		Args:       []string{"--nope"},
		Getenv:     func(string) string { return "" },
		Executable: func() (string, error) { return filepath.Join(t.TempDir(), "Catcher.exe"), nil },
		WorkingDir: func() (string, error) { return t.TempDir(), nil },
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestEnsureWritableDirRejectsFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "not-a-dir")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ensureWritableDir(path); err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadOrCreateWritesDefaults(t *testing.T) {
	dir := t.TempDir()
	cfg, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Log.Level != "info" || !cfg.Raw.Enabled || cfg.UI.Theme != "auto" {
		t.Fatalf("默认值不正确: %+v", cfg)
	}
	path := filepath.Join(dir, FileName+"."+FileExt)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("配置文件未落盘: %v", err)
	}

	cfg.Log.Level = "debug"
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Log.Level != "debug" {
		t.Fatalf("配置未持久化: %+v", got)
	}
}
