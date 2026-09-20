package service

import (
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

func TestClaimSceneImageRefusesLockedScene(t *testing.T) {
	ps := newTestProjectService(t)
	scene := models.Scene{ProjectID: 1, EpisodeN: 1, Order: 1, Generation: 1, Status: "image_ready", ImageLocked: true}
	if err := ps.db.Create(&scene).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := ps.claimSceneImage(&scene); err == nil || !strings.Contains(err.Error(), "已锁定") {
		t.Fatalf("expected explicit locked error, got %v", err)
	}
	var got models.Scene
	if err := ps.db.First(&got, scene.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.Status != "image_ready" || got.ImageToken != "" {
		t.Fatalf("locked scene was changed: %+v", got)
	}
}
