package service

import (
	"testing"

	"comfyui-console/internal/models"
)

func setupOutfitTestDB(t *testing.T) (*CharacterLookService, models.Project, models.Character) {
	db := setupTestDB(t)
	if err := db.AutoMigrate(&models.CharacterOutfit{}, &models.CharacterOutfitLook{}, &models.SceneCharacterOutfit{}, &models.ShotCharacterOutfit{}); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "多造型测试"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	character := models.Character{ProjectID: project.ID, Name: "林采微", Portrait: "face.png"}
	if err := db.Create(&character).Error; err != nil {
		t.Fatal(err)
	}
	return NewCharacterLookService(db, nil), project, character
}

func TestOutfitCombinesIndependentAssetsAndAssignsPerScene(t *testing.T) {
	svc, project, character := setupOutfitTestDB(t)
	assets := []models.CharacterLook{
		{ProjectID: project.ID, CharacterID: character.ID, Name: "工作服", Category: "clothing", AuditStatus: models.LookStatusApproved},
		{ProjectID: project.ID, CharacterID: character.ID, Name: "低马尾", Category: "hair", AuditStatus: models.LookStatusApproved},
		{ProjectID: project.ID, CharacterID: character.ID, Name: "黑色短靴", Category: "shoes", AuditStatus: models.LookStatusApproved},
	}
	for i := range assets {
		if err := svc.db.Create(&assets[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	outfit, err := svc.SaveOutfit(project.ID, character.ID, 0, OutfitInput{Name: "日常工作造型", LookIDs: []uint{assets[0].ID, assets[1].ID, assets[2].ID}})
	if err != nil {
		t.Fatal(err)
	}
	if len(outfit.Items) != 3 {
		t.Fatalf("expected 3 items, got %#v", outfit.Items)
	}
	if err := svc.ApproveOutfit(project.ID, character.ID, outfit.ID); err != nil {
		t.Fatal(err)
	}
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1, Generation: 1, Title: "办公室"}
	if err := svc.db.Create(&scene).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.AssignSceneOutfits(project.ID, scene.ID, []OutfitAssignment{{CharacterID: character.ID, OutfitID: outfit.ID}}); err != nil {
		t.Fatal(err)
	}
	rows, err := svc.ListSceneOutfits(project.ID, scene.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].OutfitID != outfit.ID {
		t.Fatalf("unexpected scene outfits: %#v", rows)
	}
}

func TestOutfitRejectsDuplicateExclusiveCategoryAndCrossCharacter(t *testing.T) {
	svc, project, character := setupOutfitTestDB(t)
	other := models.Character{ProjectID: project.ID, Name: "他人"}
	svc.db.Create(&other)
	looks := []models.CharacterLook{
		{ProjectID: project.ID, CharacterID: character.ID, Name: "服装A", Category: "clothing"},
		{ProjectID: project.ID, CharacterID: character.ID, Name: "服装B", Category: "clothing"},
		{ProjectID: project.ID, CharacterID: other.ID, Name: "他人发型", Category: "hair"},
	}
	for i := range looks {
		svc.db.Create(&looks[i])
	}
	if _, err := svc.SaveOutfit(project.ID, character.ID, 0, OutfitInput{Name: "冲突", LookIDs: []uint{looks[0].ID, looks[1].ID}}); err == nil {
		t.Fatal("expected duplicate clothing rejection")
	}
	if _, err := svc.SaveOutfit(project.ID, character.ID, 0, OutfitInput{Name: "越权", LookIDs: []uint{looks[0].ID, looks[2].ID}}); err == nil {
		t.Fatal("expected cross-character rejection")
	}
}
