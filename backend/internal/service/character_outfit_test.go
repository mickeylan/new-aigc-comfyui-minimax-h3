package service

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

type sequenceOutfitProvider struct {
	responses []string
	calls     int
}

func (p *sequenceOutfitProvider) Name() string                      { return "sequence" }
func (p *sequenceOutfitProvider) HealthCheck(context.Context) error { return nil }
func (p *sequenceOutfitProvider) Chat(_, _ string) (string, error) {
	index := p.calls
	p.calls++
	if index >= len(p.responses) {
		return "", fmt.Errorf("unexpected call")
	}
	return p.responses[index], nil
}

func setupOutfitTestDB(t *testing.T) (*CharacterLookService, models.Project, models.Character) {
	db := setupTestDB(t)
	if err := db.AutoMigrate(&models.CharacterOutfit{}, &models.CharacterOutfitLook{}, &models.SceneCharacterOutfit{}, &models.ShotCharacterOutfit{}, &models.GenerationCandidate{}); err != nil {
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

func TestSaveOutfitRegistersLookDependencies(t *testing.T) {
	svc, project, character := setupOutfitTestDB(t)
	if err := svc.db.AutoMigrate(&models.ArtifactDependency{}, &models.ArtifactInvalidationEvent{}); err != nil {
		t.Fatal(err)
	}
	svc.dependencies = NewArtifactDependencyService(svc.db, true)
	look := models.CharacterLook{ProjectID: project.ID, CharacterID: character.ID, Name: "衣服", Category: "clothing", Version: 2}
	svc.db.Create(&look)
	outfit, err := svc.SaveOutfit(project.ID, character.ID, 0, OutfitInput{Name: "套装", LookIDs: []uint{look.ID}})
	if err != nil {
		t.Fatal(err)
	}
	var edge models.ArtifactDependency
	if err := svc.db.Where("upstream_kind = ? AND upstream_id = ? AND downstream_kind = ? AND downstream_id = ?", ArtifactKindCharacterLook, look.ID, ArtifactKindCharacterOutfit, outfit.ID).First(&edge).Error; err != nil {
		t.Fatal(err)
	}
	if edge.UpstreamVersion != "2" {
		t.Fatalf("unexpected edge: %+v", edge)
	}
	if err := svc.DeleteOutfit(project.ID, character.ID, outfit.ID); err != nil {
		t.Fatal(err)
	}
	var count int64
	svc.db.Model(&models.ArtifactDependency{}).Where("downstream_kind = ? AND downstream_id = ?", ArtifactKindCharacterOutfit, outfit.ID).Count(&count)
	if count != 0 {
		t.Fatal("outfit delete left dependency edges")
	}
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

func TestDesignOutfitRepairsEmptyModelResultOnce(t *testing.T) {
	svc, project, character := setupOutfitTestDB(t)
	provider := &sequenceOutfitProvider{responses: []string{
		`{"name":"","description":"","assets":[]}`,
		`{"name":"玄灰长老造型","description":"沉稳古典","assets":[{"name":"玄灰长老袍","category":"clothing","description":"玄灰色蚕丝云锦交领长袍，银灰云鹤暗纹"},{"name":"黑色皂靴","category":"shoes","description":"黑色布面中式皂靴，低跟防滑软底"}]}`,
	}}
	svc.textProvider = provider
	outfit, err := svc.DesignOutfit(project.ID, character.ID, OutfitDesignInput{Concept: "古典修仙长老造型", ImageEngine: ImageEngineQwen21})
	if err != nil {
		t.Fatal(err)
	}
	if provider.calls != 2 || outfit.Name != "玄灰长老造型" || outfit.ImageEngine != ImageEngineQwen21 || len(outfit.Items) != 2 {
		t.Fatalf("repair result=%+v calls=%d", outfit, provider.calls)
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

func TestSaveOutfitEngineOnlyChangePreservesApproval(t *testing.T) {
	svc, project, character := setupOutfitTestDB(t)
	look := models.CharacterLook{ProjectID: project.ID, CharacterID: character.ID, Name: "战袍", Category: "clothing", Description: "玄灰战袍", AuditStatus: models.LookStatusApproved}
	if err := svc.db.Create(&look).Error; err != nil {
		t.Fatal(err)
	}
	outfit := models.CharacterOutfit{ProjectID: project.ID, CharacterID: character.ID, Name: "护甲战衣", Description: "同一套设计", ImageEngine: ImageEngineKrea2, AuditStatus: models.LookStatusApproved, Image: "old.png", Sheet: "old-sheet.png", Version: 1}
	if err := svc.db.Create(&outfit).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.db.Create(&models.CharacterOutfitLook{OutfitID: outfit.ID, LookID: look.ID, Order: 0}).Error; err != nil {
		t.Fatal(err)
	}
	saved, err := svc.SaveOutfit(project.ID, character.ID, outfit.ID, OutfitInput{Name: outfit.Name, Description: outfit.Description, ImageEngine: ImageEngineQwen21, LookIDs: []uint{look.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if saved.ImageEngine != ImageEngineQwen21 || saved.AuditStatus != models.LookStatusApproved {
		t.Fatalf("engine-only change lost approval: %+v", saved)
	}
	if saved.Image != "" || saved.Sheet != "" {
		t.Fatalf("engine-only change retained stale media: %+v", saved)
	}
}

func TestSaveOutfitDesignChangeReturnsToDraft(t *testing.T) {
	svc, project, character := setupOutfitTestDB(t)
	look := models.CharacterLook{ProjectID: project.ID, CharacterID: character.ID, Name: "战袍", Category: "clothing", Description: "玄灰战袍", AuditStatus: models.LookStatusApproved}
	svc.db.Create(&look)
	outfit := models.CharacterOutfit{ProjectID: project.ID, CharacterID: character.ID, Name: "护甲战衣", Description: "旧设计", ImageEngine: ImageEngineKrea2, AuditStatus: models.LookStatusApproved, Version: 1}
	svc.db.Create(&outfit)
	svc.db.Create(&models.CharacterOutfitLook{OutfitID: outfit.ID, LookID: look.ID, Order: 0})
	saved, err := svc.SaveOutfit(project.ID, character.ID, outfit.ID, OutfitInput{Name: outfit.Name, Description: "设计已修改", ImageEngine: ImageEngineQwen21, LookIDs: []uint{look.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if saved.AuditStatus != models.LookStatusDraft {
		t.Fatalf("changed design must return to draft: %+v", saved)
	}
}

func TestOutfitResultFilenameSeparatesRegenerations(t *testing.T) {
	first := outfitResultFilename("outfit_sheet", 7, "task-a", ".png")
	second := outfitResultFilename("outfit_sheet", 7, "task-b", ".png")
	if first == second || !strings.Contains(first, "task_a") || !strings.Contains(second, "task_b") {
		t.Fatalf("result filenames are not task-specific: %q %q", first, second)
	}
}

func TestOutfitImageAndSheetUseSelectedTemplates(t *testing.T) {
	if got := outfitTemplateCode(false, ImageEngineKrea2); got != "krea2_storyboard_reference" {
		t.Fatalf("krea2 outfit image template=%s", got)
	}
	if got := outfitTemplateCode(false, ImageEngineQwen21); got != TemplateQwen21MultiEdit {
		t.Fatalf("qwen outfit image template=%s", got)
	}
	if got := outfitTemplateCode(true, ImageEngineQwen21); got != "krea2_character_sheet" {
		t.Fatalf("outfit sheet template=%s", got)
	}
}

func TestOutfitQwenPromptUsesOrderedImageBindings(t *testing.T) {
	outfit := &models.CharacterOutfit{Description: "玄灰长老造型", Items: []models.CharacterOutfitLook{{Order: 1, Look: &models.CharacterLook{Category: "clothing", Description: "玄灰云锦长袍", Image: "coat.png"}}, {Order: 2, Look: &models.CharacterLook{Category: "shoes", Description: "黑色皂靴", Image: "shoes.png"}}}}
	prompt := outfitQwenPrompt(outfit, true)
	for _, expected := range []string{"<image1>", "<image2>", "<image3>", "<image4>", "9:16竖版"} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("qwen prompt missing %q: %s", expected, prompt)
		}
	}
	if strings.Contains(prompt, "<Picture") {
		t.Fatalf("qwen prompt leaked H3 bindings: %s", prompt)
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

func TestExplicitOutfitReferenceRemovesSameCharacterBaseSheet(t *testing.T) {
	svc, project, character := setupOutfitTestDB(t)
	character.Sheet = "old-character-sheet.png"
	if err := svc.db.Save(&character).Error; err != nil {
		t.Fatal(err)
	}
	outfit := models.CharacterOutfit{ProjectID: project.ID, CharacterID: character.ID, Name: "现代衣着造型", AuditStatus: models.LookStatusApproved, Sheet: "modern-sheet.png"}
	if err := svc.db.Create(&outfit).Error; err != nil {
		t.Fatal(err)
	}
	scene := models.Scene{ProjectID: project.ID, Characters: character.Name, ReferenceImagesJSON: fmt.Sprintf(`[{"source_type":"outfit","source_id":%d,"variant":"sheet","use_krea2":true,"use_h3":true},{"source_type":"character","source_id":%d,"variant":"sheet","use_krea2":true,"use_h3":true}]`, outfit.ID, character.ID)}
	ps := &ProjectService{db: svc.db}
	refs, lines, explicit := ps.selectedSceneReferenceFiles(&scene, "krea2")
	if !explicit || len(refs) != 1 || refs[0].Name != outfit.Sheet || strings.Contains(strings.Join(lines, "\n"), "角色「"+character.Name+"」四视图") {
		t.Fatalf("base Subject was not removed: refs=%#v lines=%#v", refs, lines)
	}
	normalized := ps.normalizeSceneReferences(project.ID, []SceneReferenceSelection{{SourceType: "outfit", SourceID: outfit.ID, Variant: "sheet"}, {SourceType: "character", SourceID: character.ID, Variant: "sheet"}})
	if len(normalized) != 1 || normalized[0].SourceType != "outfit" {
		t.Fatalf("saved refs were not normalized: %#v", normalized)
	}
}

func TestAssignedOutfitSheetIsAvailableAndForcedIntoExplicitSceneReferences(t *testing.T) {
	svc, project, character := setupOutfitTestDB(t)
	outfit := models.CharacterOutfit{ProjectID: project.ID, CharacterID: character.ID, Name: "新形象", AuditStatus: models.LookStatusApproved, Image: "new-look.png", Sheet: "new-look-sheet.png"}
	if err := svc.db.Create(&outfit).Error; err != nil {
		t.Fatal(err)
	}
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1, Generation: 1, Title: "场景", Characters: character.Name, ReferenceImagesJSON: fmt.Sprintf(`[{"source_type":"character","source_id":%d,"variant":"portrait","use_krea2":true,"use_h3":true}]`, character.ID)}
	if err := svc.db.Create(&scene).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.AssignSceneOutfits(project.ID, scene.ID, []OutfitAssignment{{CharacterID: character.ID, OutfitID: outfit.ID}}); err != nil {
		t.Fatal(err)
	}
	ps := &ProjectService{db: svc.db}
	candidates := ps.sceneReferenceCandidates(&scene)
	found := false
	for _, candidate := range candidates {
		if candidate.SourceType == "outfit" && candidate.SourceID == outfit.ID && candidate.Variant == "sheet" && candidate.Image == outfit.Sheet {
			found = true
		}
	}
	if !found {
		t.Fatalf("new outfit sheet missing from candidates: %#v", candidates)
	}
	refs, lines, explicit := ps.selectedSceneReferenceFiles(&scene, "h3")
	if !explicit || len(refs) != 1 || refs[0].Name != outfit.Sheet || !strings.Contains(lines[0], "新形象") {
		t.Fatalf("assigned outfit did not replace old character reference: refs=%#v lines=%#v explicit=%v", refs, lines, explicit)
	}
	if missing := ps.missingSceneCharacterReferences(&scene, refs); len(missing) != 0 {
		t.Fatalf("complete selected outfit should satisfy character reference: %v", missing)
	}
	scene.ReferenceImagesJSON = ""
	autoRefs, autoLines := ps.sceneImageReferenceFiles(&scene)
	if len(autoRefs) != 1 || autoRefs[0].Name != outfit.Sheet || strings.Contains(strings.Join(autoLines, "\n"), "角色「"+character.Name+"」四视图") {
		t.Fatalf("automatic scene refs should use only selected outfit: refs=%#v lines=%#v", autoRefs, autoLines)
	}
}

func TestAssignSceneOutfitsNoOpPreservesGeneratedMedia(t *testing.T) {
	svc, project, character := setupOutfitTestDB(t)
	outfit := models.CharacterOutfit{ProjectID: project.ID, CharacterID: character.ID, Name: "常服", AuditStatus: models.LookStatusApproved, Image: "look.png"}
	if err := svc.db.Create(&outfit).Error; err != nil {
		t.Fatal(err)
	}
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1, Generation: 1, Title: "场景", ImageFile: "storyboard.png", VideoFile: "video.mp4", Status: "video_ready"}
	if err := svc.db.Create(&scene).Error; err != nil {
		t.Fatal(err)
	}
	assignment := []OutfitAssignment{{CharacterID: character.ID, OutfitID: outfit.ID}}
	if err := svc.AssignSceneOutfits(project.ID, scene.ID, assignment); err != nil {
		t.Fatal(err)
	}
	if err := svc.db.Model(&scene).Updates(map[string]any{"image_file": "storyboard.png", "video_file": "video.mp4", "status": "video_ready"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.AssignSceneOutfits(project.ID, scene.ID, assignment); err != nil {
		t.Fatal(err)
	}
	var got models.Scene
	if err := svc.db.First(&got, scene.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.ImageFile != "storyboard.png" || got.VideoFile != "video.mp4" || got.Status != "video_ready" {
		t.Fatalf("no-op assignment invalidated media: %+v", got)
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
