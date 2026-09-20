package service

import (
	"strings"
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

func TestDesignOutfitCreatesReviewableAssetsAndDraft(t *testing.T) {
	svc, project, character := setupOutfitTestDB(t)
	svc.textProvider = &stubTextProvider{response: `{"name":"雨夜调查造型","description":"克制专业的雨夜行动形象","assets":[{"name":"深灰防水风衣","category":"clothing","description":"深灰色防水斜纹面料，中长款收腰风衣"},{"name":"防滑短靴","category":"shoes","description":"黑色低跟防滑皮质短靴"},{"name":"湿发低束","category":"hair","description":"黑色短发向后收束，发束利落"},{"name":"银色耳钉","category":"jewelry","description":"小尺寸哑光银色圆形耳钉"}]}`}
	outfit, err := svc.DesignOutfit(project.ID, character.ID, OutfitDesignInput{Concept: "雨夜调查记者形象"})
	if err != nil {
		t.Fatal(err)
	}
	if outfit.AuditStatus != models.LookStatusDraft || len(outfit.Items) != 4 {
		t.Fatalf("unexpected outfit: %#v", outfit)
	}
	for _, item := range outfit.Items {
		if item.Look == nil || item.Look.AuditStatus != models.LookStatusDraft || item.Look.Prompt == "" {
			t.Fatalf("asset not reviewable: %#v", item.Look)
		}
		if item.Look.CharacterID != character.ID || item.Look.ProjectID != project.ID {
			t.Fatalf("asset ownership mismatch: %#v", item.Look)
		}
	}
}

func TestOutfitPromptUsesPortraitAsIdentityAnchorForRedressing(t *testing.T) {
	prompt := outfitPrompt(&models.CharacterOutfit{Description: "雨夜调查造型"})
	for _, expected := range []string{"<Picture 1>=角色原始标准像", "基于该标准像为角色换装", "9:16竖版正面全身新形象定妆照", "不得覆盖原标准像"} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("prompt missing %q: %s", expected, prompt)
		}
	}
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
