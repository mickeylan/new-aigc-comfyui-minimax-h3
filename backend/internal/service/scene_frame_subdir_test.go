package service

import (
	"os"
	"path/filepath"
	"testing"

	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
)

func writeFramedFixture(t *testing.T, assetRoot, name, content string) string {
	t.Helper()
	dir := filepath.Join(assetRoot, "1", sceneFrameSubdir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	full := filepath.Join(dir, name)
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return full
}

func TestResolveReadPathFindsBareFrameNameInFramesSubdir(t *testing.T) {
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

	framed := writeFramedFixture(t, cfg.AssetsRoot(), "continuity_s3_task_1_005.png", "frame")
	if got := upload.ResolveReadPath("1", "continuity_s3_task_1_005.png"); got != framed {
		t.Fatalf("framed frame path=%s want %s", got, framed)
	}
	// 项目目录里的同名文件（旧版本提取的帧）优先，旧数据不受影响。
	legacyBare := filepath.Join(cfg.AssetsRoot(), "1", "continuity_s3_task_1_005.png")
	if err := os.WriteFile(legacyBare, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := upload.ResolveReadPath("1", "continuity_s3_task_1_005.png"); got != legacyBare {
		t.Fatalf("project-dir file must win: %s", got)
	}
	// 不存在的文件仍返回项目目录路径，调用方据此报「不存在」。
	if got := upload.ResolveReadPath("1", "missing.png"); got != filepath.Join(cfg.AssetsRoot(), "1", "missing.png") {
		t.Fatalf("missing path=%s", got)
	}
}

func TestEnsureProjectInputUsesFramesSubdirWithoutCopyingBack(t *testing.T) {
	f := newInputRecoveryFixture(t)
	name := "continuity_s9_task_1_last.png"
	framed := writeFramedFixture(t, f.svc.cfg.AssetsRoot(), name, "tail-frame")
	got, err := f.svc.ensureProjectInputFile(1, name)
	if err != nil {
		t.Fatal(err)
	}
	if got != framed {
		t.Fatalf("source path=%s want %s", got, framed)
	}
	if _, err := os.Stat(filepath.Join(f.svc.cfg.AssetsRoot(), "1", name)); !os.IsNotExist(err) {
		t.Fatal("frame was copied back into the project asset directory")
	}
}
