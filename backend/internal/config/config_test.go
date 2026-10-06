package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadResolvesStoragePathsRelativeToExplicitConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "runtime.yaml")
	if err := os.WriteFile(configPath, []byte("storage:\n  db_path: data/console.db\n  data_dir: data\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COMFYUI_CONSOLE_CONFIG", configPath)
	cfg := Load()
	if cfg.Storage.DBPath != filepath.Join(dir, "data", "console.db") {
		t.Fatalf("db path = %q", cfg.Storage.DBPath)
	}
	if cfg.Storage.DataDir != filepath.Join(dir, "data") {
		t.Fatalf("data dir = %q", cfg.Storage.DataDir)
	}
}

func TestResolveConfiguredPathPreservesAbsolutePath(t *testing.T) {
	absolute := filepath.Join(t.TempDir(), "console.db")
	if got := resolveConfiguredPath(absolute, `C:\ignored`); got != filepath.Clean(absolute) {
		t.Fatalf("absolute path changed: %q", got)
	}
}
