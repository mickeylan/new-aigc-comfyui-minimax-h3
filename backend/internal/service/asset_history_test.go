package service

import (
	"fmt"
	"testing"

	"comfyui-console/internal/models"
)

func setupAssetHistoryDB(t *testing.T) (*CharacterLookService, models.Project, models.Character) {
	t.Helper()
	svc, project, character := setupAgeStageTestDB(t)
	if err := svc.db.AutoMigrate(&models.AssetVariant{}, &models.SceneContinuity{}, &models.SceneCharacterLook{}, &models.ShotCharacterLook{}, &models.CharacterLook{}); err != nil {
		t.Fatal(err)
	}
	return svc, project, character
}

func variantFiles(t *testing.T, svc *CharacterLookService, projectID uint, entityType string, entityID uint) map[string]bool {
	t.Helper()
	var rows []models.AssetVariant
	if err := svc.db.Where("project_id = ? AND entity_type = ? AND entity_id = ?", projectID, entityType, entityID).Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, row := range rows {
		out[row.File] = row.Selected
	}
	return out
}

// 回忆/闪回需要旧标准像：每次替换都必须保留，且不受普通版本数量上限清理。
func TestMainPortraitReplacementsAreAllKeptAndRestorable(t *testing.T) {
	svc, project, character := setupAssetHistoryDB(t)
	names := []string{"face.png"}
	for i := 1; i <= MaxNonFavoriteAssetVariants+3; i++ {
		names = append(names, fmt.Sprintf("face_v%d.png", i))
	}
	registerAgeStageUploads(t, svc, project.ID, names...)
	for _, name := range names[1:] {
		if _, _, err := updateSelectedAsset(svc.db, project.ID, VariantCharacterPortrait, character.ID, name, "", "test", nil, "", nil); err != nil {
			t.Fatal(err)
		}
	}
	got := variantFiles(t, svc, project.ID, VariantCharacterPortrait, character.ID)
	for _, name := range names {
		if _, ok := got[name]; !ok {
			t.Fatalf("portrait %s was dropped from history; have %v", name, got)
		}
	}
	// 最早那张从未登记为版本的 face.png 也必须能恢复。
	var original models.AssetVariant
	svc.db.Where("entity_type = ? AND entity_id = ? AND file = ?", VariantCharacterPortrait, character.ID, "face.png").First(&original)
	if _, err := NewAssetVariantService(svc.db).Select(project.ID, original.ID); err != nil {
		t.Fatal(err)
	}
	var ch models.Character
	svc.db.First(&ch, character.ID)
	if ch.Portrait != "face.png" {
		t.Fatalf("original portrait not restored: %q", ch.Portrait)
	}
}

func TestRegeneratingReferencePromptKeepsOldPortraitAndSheet(t *testing.T) {
	svc, project, character := setupAssetHistoryDB(t)
	var p models.Project
	svc.db.First(&p, project.ID)
	var ch models.Character
	svc.db.First(&ch, character.ID)
	if _, err := NewCharacterProfileService(svc.db, nil).GenerateReferencePrompt(&ch, &p); err != nil {
		t.Fatal(err)
	}
	svc.db.First(&ch, character.ID)
	if ch.Portrait != "" || ch.Sheet != "" {
		t.Fatalf("precondition: reference prompt regeneration clears current visuals, got %+v", ch)
	}
	if got := variantFiles(t, svc, project.ID, VariantCharacterPortrait, character.ID); len(got) != 1 || got["face.png"] {
		t.Fatalf("old portrait must be kept as an unselected version: %v", got)
	}
	if got := variantFiles(t, svc, project.ID, VariantCharacterSheet, character.ID); len(got) != 1 {
		t.Fatalf("old sheet must be kept as a version: %v", got)
	}
}

func TestOutfitEditKeepsOldImagesAndHistoryCanBeRestored(t *testing.T) {
	svc, project, character := setupAssetHistoryDB(t)
	outfit := models.CharacterOutfit{ProjectID: project.ID, CharacterID: character.ID, Name: "校服", Description: "白衬衫", AuditStatus: models.LookStatusApproved, Image: "uniform.png", Sheet: "uniform_sheet.png"}
	if err := svc.db.Create(&outfit).Error; err != nil {
		t.Fatal(err)
	}
	registerAgeStageUploads(t, svc, project.ID, "uniform.png", "uniform_sheet.png")
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1, Title: "回忆", ImageFile: "s.png", VideoFile: "s.mp4"}
	if err := svc.db.Create(&scene).Error; err != nil {
		t.Fatal(err)
	}
	svc.db.Create(&models.SceneCharacterOutfit{SceneID: scene.ID, CharacterID: character.ID, OutfitID: outfit.ID})

	look := models.CharacterLook{ProjectID: project.ID, CharacterID: character.ID, Name: "水手服", Category: "clothing"}
	if err := svc.db.Create(&look).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveOutfit(project.ID, character.ID, outfit.ID, OutfitInput{Name: "校服", Description: "蓝色水手服", LookIDs: []uint{look.ID}}); err != nil {
		t.Fatal(err)
	}
	var saved models.CharacterOutfit
	svc.db.First(&saved, outfit.ID)
	if saved.Image != "" || saved.Sheet != "" {
		t.Fatalf("precondition: design change clears current outfit images, got %+v", saved)
	}
	images := variantFiles(t, svc, project.ID, VariantOutfitImage, outfit.ID)
	sheets := variantFiles(t, svc, project.ID, VariantOutfitSheet, outfit.ID)
	if _, ok := images["uniform.png"]; !ok {
		t.Fatalf("old outfit image not kept: %v", images)
	}
	if _, ok := sheets["uniform_sheet.png"]; !ok {
		t.Fatalf("old outfit sheet not kept: %v", sheets)
	}

	var old models.AssetVariant
	svc.db.Where("entity_type = ? AND entity_id = ? AND file = ?", VariantOutfitImage, outfit.ID, "uniform.png").First(&old)
	if _, err := NewAssetVariantService(svc.db).Select(project.ID, old.ID); err != nil {
		t.Fatal(err)
	}
	svc.db.First(&saved, outfit.ID)
	if saved.Image != "uniform.png" {
		t.Fatalf("outfit image not restored: %+v", saved)
	}
	var sc models.Scene
	svc.db.First(&sc, scene.ID)
	if !sc.PromptStale || sc.VideoFile != "s.mp4" {
		t.Fatalf("scene using the outfit must be marked stale and keep media: %+v", sc)
	}

	if err := svc.DeleteOutfit(project.ID, character.ID, outfit.ID); err != nil {
		t.Fatal(err)
	}
	var left int64
	svc.db.Model(&models.AssetVariant{}).Where("entity_type IN ? AND entity_id = ?", []string{VariantOutfitImage, VariantOutfitSheet}, outfit.ID).Count(&left)
	if left != 0 {
		t.Fatalf("deleted outfit left %d history rows", left)
	}
}
