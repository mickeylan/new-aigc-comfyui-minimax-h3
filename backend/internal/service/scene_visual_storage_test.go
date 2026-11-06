package service

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
)

func TestStoryboardCheckUsesProductionUploadRootNotDatabaseDirectory(t *testing.T) {
	comfy, data := t.TempDir(), t.TempDir()
	cfg := &config.Config{}
	cfg.Storage.DataDir = data
	cfg.Comfy.ComfyDir = comfy
	svc := &Service{Cfg: cfg, Upload: NewUploadManager(cfg, nil, nil)}
	sc := &models.Scene{ID: 4, ProjectID: 7, ImageFile: "frame.png"}
	dir := filepath.Join(comfy, "input", fmt.Sprint(sc.ProjectID))
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	image := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 504)...)
	if err := os.WriteFile(filepath.Join(dir, sc.ImageFile), image, 0600); err != nil {
		t.Fatal(err)
	}
	_, hash, snapshot, err := svc.currentStoryboardImageSnapshot(sc)
	if err != nil || len(snapshot) != len(image) || hash == ([32]byte{}) {
		t.Fatalf("production image not found: len=%d err=%v", len(snapshot), err)
	}
	// A similarly named file under the application DB directory is not a fallback.
	if err := os.Remove(filepath.Join(dir, sc.ImageFile)); err != nil {
		t.Fatal(err)
	}
	wrongDir := filepath.Join(data, "input", "7")
	if err := os.MkdirAll(wrongDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wrongDir, sc.ImageFile), image, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := svc.currentStoryboardImageSnapshot(sc); err == nil {
		t.Fatal("silently used unrelated application-data image")
	}
}
