package service

import (
	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSequenceIsolationAndImmutableResults(t *testing.T) {
	db := newTestDB(t)
	if err := db.AutoMigrate(&models.Material{}, &models.UploadFile{}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	cfg := &config.Config{Comfy: config.ComfyConfig{ComfyDir: filepath.Join(root, "ComfyUI")}, Storage: config.StorageConfig{DataDir: filepath.Join(root, "data")}}
	remote := NewRemoteExec(config.RemoteConfig{})
	remote.SetLocalRoots([]string{cfg.Comfy.ComfyDir, cfg.Storage.DataDir, cfg.AssetsRoot()})
	upload := NewUploadManager(cfg, db, remote)
	service := NewMaterialService(cfg, db, remote, upload)
	a, b := uint(11), uint(22)
	const stamp = "20261122-174038-0123456789abcdef0123456789abcdef"
	ids := map[uint][]uint{}
	for _, project := range []uint{a, b} {
		for index := 0; index < 2; index++ {
			row := models.Material{ProjectID: &project, Type: "image", Name: fmt.Sprintf("clay-seq-%s-%03d.png", stamp, index), Path: fmt.Sprintf("missing-%d-%d.png", project, index)}
			if err := db.Create(&row).Error; err != nil {
				t.Fatal(err)
			}
			ids[project] = append(ids[project], row.ID)
		}
	}
	req := SequenceVideoRequest{Pass: "clay", Stamp: stamp, FPS: 8, FrameIDs: ids[b]}
	if _, err := service.ComposeSequenceVideo(&a, req); err == nil || !strings.Contains(err.Error(), "不属于") {
		t.Fatalf("foreign IDs accepted: %v", err)
	}
	req.FrameIDs = ids[a]
	// Correct IDs must reach file loading, not be mixed with identical names in project B.
	if _, err := service.ComposeSequenceVideo(&a, req); err == nil || !strings.Contains(err.Error(), "不在本机磁盘") {
		t.Fatalf("scoped selection failed: %v", err)
	}
	req.FrameIDs = nil
	if _, err := service.ComposeSequenceVideo(nil, req); err == nil || !strings.Contains(err.Error(), "只有 0 帧") {
		t.Fatalf("unassigned scope saw project frames: %v", err)
	}
	req.FrameIDs = []uint{ids[a][0], ids[a][0]}
	if _, err := service.ComposeSequenceVideo(&a, req); err == nil || !strings.Contains(err.Error(), "重复") {
		t.Fatalf("duplicate IDs accepted: %v", err)
	}
	if !sequenceStampPattern.MatchString(stamp) {
		t.Fatal("new batch stamp rejected")
	}
	if _, err := orderSequenceFrames([]string{fmt.Sprintf("clay-seq-%s-000.png", stamp), fmt.Sprintf("clay-seq-%s-001.png", stamp)}, "clay", stamp); err != nil {
		t.Fatal(err)
	}
	first, err := service.SaveStudioSequenceVideo(&a, "clay-seq-20261122-174038.mp4", []byte("first"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.SaveStudioSequenceVideo(&a, "clay-seq-20261122-174038.mp4", []byte("second"))
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID || first.Path == second.Path {
		t.Fatal("results overwrite one another")
	}
	var count int64
	db.Model(&models.Material{}).Where("type = ?", "video").Count(&count)
	if count != 2 {
		t.Fatalf("prior video deleted: %d", count)
	}
	data, err := os.ReadFile(filepath.Join(upload.InputDir(), filepath.FromSlash(first.Path)))
	if err != nil || string(data) != "first" {
		t.Fatalf("first output changed: %q %v", data, err)
	}
}
