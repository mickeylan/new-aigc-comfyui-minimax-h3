package service

import (
	"strings"
	"testing"

	"comfyui-console/internal/models"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newTestDBWithNewModels(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Project{}, &models.Scene{}, &models.Shot{}, &models.PromptVersion{}, &models.StylePreset{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestShotServiceStoresManualDirectorStructure(t *testing.T) {
	db := newTestDBWithNewModels(t)
	project := models.Project{Title: "test"}
	db.Create(&project)
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1}
	db.Create(&scene)
	svc := NewShotService(db)
	input := []models.Shot{
		{ActType: models.ShotActSetup, ShotType: "wide", Duration: 1.5, Description: "雨夜建立旧宅", PromptSubject: "雨中的旧宅", PromptCamera: "广角固定", PromptLighting: "冷色月光"},
		{ActType: models.ShotActMidpoint, ShotType: "close_up", CameraAngle: "eye_level", CameraMovement: "dolly_in", Duration: 2.5, Description: "角色发现线索", Dialogue: "原来如此", Emotion: "震惊", PromptSubject: "侦探与信件", PromptAction: "拆开信封", PromptCamera: "近景缓慢推进", PromptLighting: "冷色侧光", PromptStyle: "电影写实"},
	}
	shots, err := svc.ReplaceShots(scene.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	if len(shots) != 2 || shots[1].ActType != models.ShotActMidpoint || shots[1].PromptLighting != "冷色侧光" {
		t.Fatalf("structure lost: %+v", shots)
	}
	db.First(&scene, scene.ID)
	if scene.ShotCount != 2 || scene.Duration != 4 {
		t.Fatalf("scene aggregate count=%d duration=%v", scene.ShotCount, scene.Duration)
	}
	for _, want := range []string{"镜头1·建置", "雨夜建立旧宅", "镜头2·转折", "对白：原来如此"} {
		if !strings.Contains(scene.Content, want) {
			t.Fatalf("scene content missing %q: %s", want, scene.Content)
		}
	}
	if !strings.Contains(scene.ImagePrompt, "侦探与信件") || scene.Status != "pending" {
		t.Fatalf("scene prompt/status not aggregated: %+v", scene)
	}
}

func TestShotServiceReplaceIsTransactionalAndValidates(t *testing.T) {
	db := newTestDBWithNewModels(t)
	scene := models.Scene{ProjectID: 1, EpisodeN: 1, Order: 1}
	db.Create(&scene)
	svc := NewShotService(db)
	_, err := svc.ReplaceShots(scene.ID, []models.Shot{{ShotType: "wide", Duration: 1}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.ReplaceShots(scene.ID, []models.Shot{{ShotType: "", Duration: 1}})
	if err == nil {
		t.Fatal("expected validation error")
	}
	shots, _ := svc.GetSceneShots(scene.ID)
	if len(shots) != 1 || shots[0].ShotType != "wide" {
		t.Fatalf("existing shots changed: %+v", shots)
	}
}

func TestShotServiceDeleteUpdatesCount(t *testing.T) {
	db := newTestDBWithNewModels(t)
	scene := models.Scene{ProjectID: 1, EpisodeN: 1, Order: 1}
	db.Create(&scene)
	svc := NewShotService(db)
	shots, _ := svc.ReplaceShots(scene.ID, []models.Shot{{ShotType: "medium", Duration: 2}})
	if err := svc.DeleteShot(shots[0].ID); err != nil {
		t.Fatal(err)
	}
	db.First(&scene, scene.ID)
	if scene.ShotCount != 0 {
		t.Fatalf("shot count=%d", scene.ShotCount)
	}
}
