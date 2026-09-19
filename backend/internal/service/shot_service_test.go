package service

import (
	"fmt"
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
	if err := db.AutoMigrate(
		&models.Project{}, &models.Episode{}, &models.Scene{}, &models.Shot{}, &models.PromptVersion{}, &models.StylePreset{},
		&models.Character{}, &models.CharacterLook{}, &models.ShotCharacterLook{},
		&models.CharacterOutfit{}, &models.ShotCharacterOutfit{},
	); err != nil {
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

func TestShotServiceReplacePreservesIDsAndRejectsForeignShot(t *testing.T) {
	db := newTestDBWithNewModels(t)
	project := models.Project{Title: "test"}
	db.Create(&project)
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1}
	otherScene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 2}
	db.Create(&scene)
	db.Create(&otherScene)
	svc := NewShotService(db)
	initial, err := svc.ReplaceShots(scene.ID, []models.Shot{
		{ShotType: "wide", Duration: 1, Description: "first"},
		{ShotType: "close", Duration: 2, Description: "second"},
	})
	if err != nil {
		t.Fatal(err)
	}
	firstID, secondID := initial[0].ID, initial[1].ID
	updated, err := svc.ReplaceShots(scene.ID, []models.Shot{
		{ID: secondID, ShotType: "close", Duration: 2, Description: "second updated"},
		{ID: firstID, ShotType: "wide", Duration: 1, Description: "first updated"},
		{ShotType: "medium", Duration: 1.5, Description: "new"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated) != 3 || updated[0].ID != secondID || updated[1].ID != firstID || updated[2].ID == 0 {
		t.Fatalf("shot IDs/order were not preserved: %+v", updated)
	}
	foreign, err := svc.ReplaceShots(otherScene.ID, []models.Shot{{ShotType: "wide", Duration: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ReplaceShots(scene.ID, []models.Shot{{ID: foreign[0].ID, ShotType: "wide", Duration: 1}}); err == nil || !strings.Contains(err.Error(), "不属于当前场景") {
		t.Fatalf("foreign shot ID accepted: %v", err)
	}
	remaining, _ := svc.GetSceneShots(scene.ID)
	if len(remaining) != 3 || remaining[0].ID != secondID {
		t.Fatalf("failed replacement mutated existing shots: %+v", remaining)
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

func TestShotServiceSaveRecordsCanonicalDeduplicatedManualPrompt(t *testing.T) {
	db := newTestDBWithNewModels(t)
	project := models.Project{Title: "prompt history"}
	db.Create(&project)
	scene := models.Scene{ProjectID: project.ID, Order: 1}
	db.Create(&scene)
	svc := NewShotService(db)

	shots, err := svc.ReplaceShots(scene.ID, []models.Shot{{
		ShotType: "close", Duration: 2, PromptSubject: "  face  ", PromptAction: "turns\nslowly",
		PromptCamera: "close-up", PromptLighting: "moonlight", PromptStyle: "film",
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := "face\nturns slowly\nclose-up\nmoonlight\nfilm"
	var versions []models.PromptVersion
	db.Where("project_id = ? AND entity_type = ? AND entity_id = ?", project.ID, "shot", shots[0].ID).Find(&versions)
	if len(versions) != 1 || versions[0].Action != "manual" || versions[0].Content != want {
		t.Fatalf("manual prompt versions = %+v, want content %q", versions, want)
	}

	if _, err := svc.ReplaceShots(scene.ID, shots); err != nil {
		t.Fatal(err)
	}
	var count int64
	db.Model(&models.PromptVersion{}).Where("project_id = ? AND entity_type = ? AND entity_id = ?", project.ID, "shot", shots[0].ID).Count(&count)
	if count != 1 {
		t.Fatalf("same prompt produced %d versions", count)
	}

	if _, err := svc.UpdateShot(shots[0].ID, map[string]any{"prompt_style": "noir"}); err != nil {
		t.Fatal(err)
	}
	db.Model(&models.PromptVersion{}).Where("project_id = ? AND entity_type = ? AND entity_id = ?", project.ID, "shot", shots[0].ID).Count(&count)
	if count != 2 {
		t.Fatalf("changed prompt produced %d versions", count)
	}
}

func TestShotServiceRemovalCleansAssociationsAndRetainsPromptHistory(t *testing.T) {
	db := newTestDBWithNewModels(t)
	project := models.Project{Title: "cleanup"}
	db.Create(&project)
	scene := models.Scene{ProjectID: project.ID, Order: 1}
	db.Create(&scene)
	svc := NewShotService(db)
	shots, err := svc.ReplaceShots(scene.ID, []models.Shot{
		{ShotType: "wide", Duration: 1, PromptSubject: "first"},
		{ShotType: "close", Duration: 1, PromptSubject: "second"},
	})
	if err != nil {
		t.Fatal(err)
	}
	character := models.Character{ProjectID: project.ID, Name: "actor"}
	if err := db.Create(&character).Error; err != nil {
		t.Fatal(err)
	}
	for i, shot := range shots {
		look := models.CharacterLook{ProjectID: project.ID, CharacterID: character.ID, Name: fmt.Sprintf("look-%d", i)}
		outfit := models.CharacterOutfit{ProjectID: project.ID, CharacterID: character.ID, Name: fmt.Sprintf("outfit-%d", i)}
		if err := db.Create(&look).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&outfit).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&models.ShotCharacterLook{ShotID: shot.ID, LookID: look.ID}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&models.ShotCharacterOutfit{ShotID: shot.ID, CharacterID: character.ID, OutfitID: outfit.ID}).Error; err != nil {
			t.Fatal(err)
		}
	}

	if _, err := svc.ReplaceShots(scene.ID, []models.Shot{shots[0]}); err != nil {
		t.Fatal(err)
	}
	assertShotReferences := func(shotID uint, wantAssociations, wantVersions int64) {
		t.Helper()
		var looks, outfits, versions int64
		db.Model(&models.ShotCharacterLook{}).Where("shot_id = ?", shotID).Count(&looks)
		db.Model(&models.ShotCharacterOutfit{}).Where("shot_id = ?", shotID).Count(&outfits)
		db.Model(&models.PromptVersion{}).Where("project_id = ? AND entity_type = 'shot' AND entity_id = ?", project.ID, shotID).Count(&versions)
		if looks != wantAssociations || outfits != wantAssociations || versions != wantVersions {
			t.Fatalf("shot %d refs: looks=%d outfits=%d versions=%d", shotID, looks, outfits, versions)
		}
	}
	assertShotReferences(shots[1].ID, 0, 1)
	assertShotReferences(shots[0].ID, 1, 1)

	if err := svc.DeleteShot(shots[0].ID); err != nil {
		t.Fatal(err)
	}
	assertShotReferences(shots[0].ID, 0, 1)
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
