package service

import (
	"os"
	"path/filepath"
	"testing"

	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
)

func TestMigrateLegacyAssetsCopiesRecordedFilesOnly(t *testing.T) {
	db := newTestDB(t)
	if err := db.AutoMigrate(&models.UploadFile{}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	cfg := &config.Config{
		Comfy:   config.ComfyConfig{ComfyDir: filepath.Join(root, "ComfyUI")},
		Storage: config.StorageConfig{DataDir: filepath.Join(root, "data")},
	}
	remote := NewRemoteExec(config.RemoteConfig{})
	remote.SetLocalRoots([]string{cfg.Comfy.ComfyDir, cfg.Storage.DataDir, cfg.AssetsRoot()})
	upload := NewUploadManager(cfg, db, remote)
	legacy := filepath.Join(cfg.Comfy.ComfyDir, "input", "1")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"char_1_1.png", "char_sheet_1_1.png"} {
		if err := os.WriteFile(filepath.Join(legacy, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&models.UploadFile{TaskID: "1", Type: "image", Name: name, Path: "1/" + name}).Error; err != nil {
			t.Fatal(err)
		}
	}
	// Unrecorded transient files must not be promoted into durable assets.
	if err := os.WriteFile(filepath.Join(legacy, "scene_frames_s1_000001.png"), []byte("frame"), 0o644); err != nil {
		t.Fatal(err)
	}
	upload.MigrateLegacyAssets()
	for _, name := range []string{"char_1_1.png", "char_sheet_1_1.png"} {
		if data, err := os.ReadFile(filepath.Join(cfg.AssetsRoot(), "1", name)); err != nil || string(data) != name {
			t.Fatalf("recorded asset %s not migrated: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(cfg.AssetsRoot(), "1", "scene_frames_s1_000001.png")); !os.IsNotExist(err) {
		t.Fatal("transient frame was promoted into durable assets")
	}
	if _, err := os.Stat(filepath.Join(legacy, "char_1_1.png")); err != nil {
		t.Fatal("legacy file must not be deleted by migration")
	}
	// Idempotent second run.
	upload.MigrateLegacyAssets()
	if data, err := os.ReadFile(filepath.Join(cfg.AssetsRoot(), "1", "char_1_1.png")); err != nil || string(data) != "char_1_1.png" {
		t.Fatal("second migration changed content")
	}
}
