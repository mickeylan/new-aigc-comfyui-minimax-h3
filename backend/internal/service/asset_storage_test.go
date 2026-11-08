package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
)

func TestGeneratedAssetsUseDurableAssetRootNotComfyInput(t *testing.T) {
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
	if _, _, err := upload.SaveFile("1", "image", "char_sheet_3_1.png", []byte("sheet")); err != nil {
		t.Fatal(err)
	}
	durable := filepath.Join(cfg.AssetsRoot(), "1", "char_sheet_3_1.png")
	if data, err := os.ReadFile(durable); err != nil || string(data) != "sheet" {
		t.Fatalf("asset not stored durably: %v %q", err, data)
	}
	if _, err := os.Stat(filepath.Join(cfg.Comfy.ComfyDir, "input", "1", "char_sheet_3_1.png")); !os.IsNotExist(err) {
		t.Fatal("generated asset must not be written into ComfyUI input")
	}
	// ComfyUI-style transient frames stay where they were created and are still readable.
	frames := filepath.Join(cfg.Comfy.ComfyDir, "input", "1")
	if err := os.MkdirAll(frames, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(frames, "scene_frames_s1_000001.png"), []byte("frame"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := upload.ResolveReadPath("1", "scene_frames_s1_000001.png"); got != filepath.Join(frames, "scene_frames_s1_000001.png") {
		t.Fatalf("transient frame path=%s", got)
	}
	if _, err := os.Stat(filepath.Join(cfg.AssetsRoot(), "1", "scene_frames_s1_000001.png")); !os.IsNotExist(err) {
		t.Fatal("transient frames must not be migrated into durable assets")
	}
}

func TestResolveReadPathMigratesLegacyRecordedAsset(t *testing.T) {
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
	legacyDir := filepath.Join(cfg.Comfy.ComfyDir, "input", "1")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "char_5_1.png"), []byte("legacy"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.UploadFile{TaskID: "1", Type: "image", Name: "char_5_1.png", Path: "1/char_5_1.png", Size: 6}).Error; err != nil {
		t.Fatal(err)
	}
	got := upload.ResolveReadPath("1", "char_5_1.png")
	if got != filepath.Join(cfg.AssetsRoot(), "1", "char_5_1.png") {
		t.Fatalf("legacy asset not migrated: %s", got)
	}
	if data, err := os.ReadFile(got); err != nil || string(data) != "legacy" {
		t.Fatalf("migrated bytes wrong: %v %q", err, data)
	}
}

func TestOutputSubfoldersUseRecordedFolderThenUserConfiguration(t *testing.T) {
	db := newTestDB(t)
	if err := db.AutoMigrate(&models.Setting{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Setting{Key: settingComfyOutputSubfolders, Value: "custom_out, extra/deep"}).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewTaskService(&config.Config{Comfy: config.ComfyConfig{OutputSubfolders: []string{"yaml_out"}}}, db, nil, nil, NewHub(), NewRemoteExec(config.RemoteConfig{}))
	got := svc.outputSubfolders("recorded_dir")
	want := []string{"recorded_dir", "yaml_out", "custom_out", "extra/deep", ""}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("subfolders=%v want=%v", got, want)
	}
	svc.cfg.Comfy.OutputSubfolders = nil
	got = svc.outputSubfolders("recorded_dir")
	if strings.Join(got, "|") != "recorded_dir|custom_out|extra/deep|" {
		t.Fatalf("settings not applied: %v", got)
	}
	for _, bad := range []string{"../outside", "C:/abs", `a\b`, "/abs"} {
		svc.cfg.Comfy.OutputSubfolders = []string{bad}
		if strings.Contains(strings.Join(svc.outputSubfolders(""), "|"), "..") || strings.Contains(strings.Join(svc.outputSubfolders(""), "|"), "C:") {
			t.Fatalf("unsafe folder accepted: %s => %v", bad, svc.outputSubfolders(""))
		}
	}
}
