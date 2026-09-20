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

func TestDesignOutfitRequiresCharacterPortraitBinding(t *testing.T) {
	svc, project, character := setupOutfitTestDB(t)
	svc.textProvider = &stubTextProvider{response: `{}`}
	if err := svc.db.Model(&character).Update("portrait", "").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.DesignOutfit(project.ID, character.ID, OutfitDesignInput{Concept: "新造型"}); err == nil || !strings.Contains(err.Error(), "必须绑定标准像") {
		t.Fatalf("expected portrait binding error, got %v", err)
	}
}

func TestDesignOutfitCreatesReviewableAssetsAndDraft(t *testing.T) {
	svc, project, character := setupOutfitTestDB(t)
	svc.textProvider = &stubTextProvider{response: `{"name":"雨夜调查造型","description":"克制专业的雨夜行动形象","assets":[{"name":"深灰防水风衣","category":"clothing","description":"深灰色防水斜纹面料，中长款收腰风衣"},{"name":"防滑短靴","category":"shoes","description":"黑色低跟防滑皮质短靴"},{"name":"湿发低束","category":"hair","description":"黑色短发向后收束，发束利落"},{"name":"银色耳钉","category":"jewelry","description":"小尺寸哑光银色圆形耳钉"}]}`}
	outfit, err := svc.DesignOutfit(project.ID, character.ID, OutfitDesignInput{Concept: "雨夜调查记者形象"})
	if err != nil {
		t.Fatal(err)
	}
	if outfit.AuditStatus != models.LookStatusDraft || len(outfit.Items) != 3 {
		t.Fatalf("unexpected outfit: %#v", outfit)
	}
	if !strings.Contains(outfit.Description, "用户原始造型要求：雨夜调查记者形象") {
		t.Fatalf("original concept not retained: %q", outfit.Description)
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

func TestListOutfitsDoesNotLeakAcrossCharacters(t *testing.T) {
	svc, project, character := setupOutfitTestDB(t)
	other := models.Character{ProjectID: project.ID, Name: "另一角色", Portrait: "other.png"}
	if err := svc.db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.db.Create(&models.CharacterOutfit{ProjectID: project.ID, CharacterID: character.ID, Name: "角色一套装"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.db.Create(&models.CharacterOutfit{ProjectID: project.ID, CharacterID: other.ID, Name: "角色二套装"}).Error; err != nil {
		t.Fatal(err)
	}
	rows, err := svc.ListOutfits(project.ID, character.ID)
	if err != nil || len(rows) != 1 || rows[0].CharacterID != character.ID {
		t.Fatalf("outfit binding leaked: rows=%#v err=%v", rows, err)
	}
}

func TestOutfitResultFilenameSeparatesRegenerations(t *testing.T) {
	first := outfitResultFilename("outfit_sheet", 7, "task-a", ".png")
	second := outfitResultFilename("outfit_sheet", 7, "task-b", ".png")
	if first == second || !strings.Contains(first, "task_a") || !strings.Contains(second, "task_b") {
		t.Fatalf("result filenames are not task-specific: %q %q", first, second)
	}
}

func TestOutfitImageAndSheetUseDifferentTemplates(t *testing.T) {
	if got := outfitTemplateCode(false); got != "minimax_h3_look_reference" {
		t.Fatalf("outfit image template=%s", got)
	}
	if got := outfitTemplateCode(true); got != "krea2_character_sheet" {
		t.Fatalf("outfit sheet template=%s", got)
	}
}

func TestOutfitPromptUsesPortraitAsIdentityAnchorForRedressing(t *testing.T) {
	prompt := outfitPrompt(&models.CharacterOutfit{Description: "雨夜调查造型"}, false)
	for _, expected := range []string{"<Subject 1> 是 <Picture 1> 中的当前角色标准像", "identity_preserved", "9:16竖版", "完整头部与清晰正脸自然可见"} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("prompt missing %q: %s", expected, prompt)
		}
	}
}

func TestDesignOutfitDoesNotChangeHairUnlessExplicitlyRequested(t *testing.T) {
	svc, project, character := setupOutfitTestDB(t)
	svc.textProvider = &stubTextProvider{response: `{"name":"通勤装","description":"通勤装","assets":[{"name":"外套","category":"clothing","description":"短外套"},{"name":"短靴","category":"shoes","description":"短靴"},{"name":"AI臆造短发","category":"hair","description":"短发"}]}`}
	outfit, err := svc.DesignOutfit(project.ID, character.ID, OutfitDesignInput{Concept: "换一套通勤服装"})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range outfit.Items {
		if item.Look.Category == "hair" {
			t.Fatalf("unrequested hairstyle retained: %#v", item.Look)
		}
	}
	prompt := outfitPrompt(outfit, false)
	if !strings.Contains(prompt, "发型完整沿用当前角色标准像") {
		t.Fatalf("hair preservation missing: %s", prompt)
	}
}

func TestDesignOutfitFiltersExplicitlyExcludedBagAndGloves(t *testing.T) {
	svc, project, character := setupOutfitTestDB(t)
	svc.textProvider = &stubTextProvider{response: `{"name":"轻装","description":"轻装","assets":[{"name":"外套","category":"clothing","description":"短外套"},{"name":"短靴","category":"shoes","description":"短靴"},{"name":"短发","category":"hair","description":"短发"},{"name":"皮手套","category":"jewelry","description":"黑色手套"},{"name":"手提包","category":"bag","description":"黑色包"}]}`}
	outfit, err := svc.DesignOutfit(project.ID, character.ID, OutfitDesignInput{Concept: "轻装，不要手套，不要包"})
	if err != nil {
		t.Fatal(err)
	}
	if len(outfit.Items) != 2 {
		t.Fatalf("excluded or unrequested assets remained: %#v", outfit.Items)
	}
	for _, item := range outfit.Items {
		if item.Look.Category == "bag" || isGloveText(item.Look.Name+item.Look.Description) {
			t.Fatalf("excluded asset remained: %#v", item.Look)
		}
	}
}

func TestOutfitPromptMakesAbsentGlovesAndBagPositiveVisibleConstraints(t *testing.T) {
	prompt := outfitPrompt(&models.CharacterOutfit{Description: "轻便工作造型；不要手套、不要包"}, false)
	for _, expected := range []string{"原图服装、手部遮挡物和随身包袋不作为本次造型参考", "双手自然裸露且完整可见", "手掌与五指结构清晰", "双肩、双手和腰侧保持空置"} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("prompt missing %q: %s", expected, prompt)
		}
	}
}

func TestOutfitPromptKeepsExplicitGlovesAndBag(t *testing.T) {
	outfit := &models.CharacterOutfit{Items: []models.CharacterOutfitLook{
		{Look: &models.CharacterLook{Name: "皮手套", Category: "clothing", Description: "黑色皮手套"}},
		{Look: &models.CharacterLook{Name: "邮差包", Category: "bag", Description: "棕色邮差包"}},
	}}
	prompt := outfitPrompt(outfit, false)
	for _, absent := range []string{"双手自然裸露且完整可见", "双肩、双手和腰侧保持空置"} {
		if strings.Contains(prompt, absent) {
			t.Fatalf("explicit accessory contradicted by %q: %s", absent, prompt)
		}
	}
}

func TestOutfitPromptUsesCharacterSheetAsSecondIdentityReference(t *testing.T) {
	outfit := &models.CharacterOutfit{Items: []models.CharacterOutfitLook{{Look: &models.CharacterLook{Category: "clothing", Description: "深灰风衣", Image: "coat.png"}}}}
	prompt := outfitPrompt(outfit, true)
	for _, expected := range []string{"<Subject 1> 是 <Picture 1> 中的当前角色标准像", "旧服装和配饰不沿用", "<Subject 2> 是 <Picture 2> 中的当前角色四视图", "<Subject 3> 是 <Picture 3> 中的服装参考"} {
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
