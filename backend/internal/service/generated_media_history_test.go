package service

import (
	"os"
	"path/filepath"
	"testing"

	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
)

func TestGeneratedMediaHistoryListsBothFamilies(t *testing.T) {
	db := newTestProjectService(t).db
	if err := db.AutoMigrate(&models.AssetVariant{}, &models.GenerationCandidate{}); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "history"}
	db.Create(&project)
	db.Create(&models.AssetVariant{ProjectID: project.ID, EntityType: VariantCharacterPortrait, EntityID: 1, File: "portrait.png"})
	db.Create(&models.GenerationCandidate{ProjectID: project.ID, EntityType: "scene", EntityID: 2, MediaType: "video", TaskID: "video-1", File: "worker.mp4", VideoInputFile: "video.mp4"})
	rows, err := NewGeneratedMediaHistoryService(db, nil).List(project.ID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows=%+v", rows)
	}
	if rows[0].Family == rows[1].Family {
		t.Fatalf("families not unified: %+v", rows)
	}
}

func TestGeneratedMediaDeleteRemovesOnlyUnreferencedProjectFile(t *testing.T) {
	db := newTestProjectService(t).db
	if err := db.AutoMigrate(&models.AssetVariant{}, &models.UploadFile{}, &models.Character{}, &models.CharacterLook{}, &models.Asset{}, &models.Scene{}, &models.FrameCandidate{}, &models.GenerationCandidate{}); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "cleanup"}
	db.Create(&project)
	root := t.TempDir()
	cfg := config.Default()
	cfg.Comfy.ComfyDir = filepath.Join(root, "comfy")
	remote := NewRemoteExec(config.RemoteConfig{})
	if err := os.MkdirAll(filepath.Join(cfg.Comfy.ComfyDir, "input", "1"), 0o755); err != nil {
		t.Fatal(err)
	}
	remote.SetLocalRoots([]string{cfg.Comfy.ComfyDir})
	upload := NewUploadManager(cfg, db, remote)
	file := "old.png"
	path := filepath.Join(cfg.Comfy.ComfyDir, "input", "1", file)
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	row := models.AssetVariant{ProjectID: project.ID, EntityType: VariantCharacterPortrait, EntityID: 1, File: file}
	db.Create(&row)
	db.Create(&models.UploadFile{TaskID: "1", Type: "image", Name: file, Path: "1/" + file, Size: 1})
	service := NewGeneratedMediaHistoryService(db, upload)
	if err := service.Delete(project.ID, "asset_variant", row.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("file still exists: %v", err)
	}
}

func TestGeneratedMediaDeleteProtectsCurrent(t *testing.T) {
	db := newTestProjectService(t).db
	if err := db.AutoMigrate(&models.GenerationCandidate{}); err != nil {
		t.Fatal(err)
	}
	row := models.GenerationCandidate{ProjectID: 1, EntityType: "scene", EntityID: 1, MediaType: "image", TaskID: "x", File: "x.png", IsCurrent: true}
	db.Create(&row)
	if err := NewGeneratedMediaHistoryService(db, nil).Delete(1, "scene_candidate", row.ID); err == nil {
		t.Fatal("current candidate deletion accepted")
	}
}
