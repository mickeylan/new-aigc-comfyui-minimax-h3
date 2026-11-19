package service

import (
	"encoding/json"
	"strings"
	"testing"

	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
)

func setupAgeStageTestDB(t *testing.T) (*CharacterLookService, models.Project, models.Character) {
	t.Helper()
	svc, project, character := setupOutfitTestDB(t)
	pool, err := svc.db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(1)
	if err := svc.db.AutoMigrate(&models.CharacterAgeStage{}, &models.CharacterOutfit{}, &models.Template{}, &models.Task{}, &models.UploadFile{}, &models.Instance{}); err != nil {
		t.Fatal(err)
	}
	if err := svc.db.Model(&character).Updates(map[string]any{"sheet": "adult_sheet.png", "appearance": "28岁女性，高挑"}).Error; err != nil {
		t.Fatal(err)
	}
	character.Sheet = "adult_sheet.png"
	return svc, project, character
}

func attachAgeStageTaskService(t *testing.T, svc *CharacterLookService, templates ...string) {
	t.Helper()
	for _, name := range templates {
		tpl := loadTemplateForTest(t, name)
		if err := svc.db.Create(&tpl).Error; err != nil {
			t.Fatal(err)
		}
	}
	cfg := &config.Config{}
	svc.tasks = NewTaskService(cfg, svc.db, NewInstanceManager(cfg, svc.db, nil), nil, NewHub(), nil)
}

func registerAgeStageUploads(t *testing.T, svc *CharacterLookService, projectID uint, names ...string) {
	t.Helper()
	for _, name := range names {
		if err := svc.db.Create(&models.UploadFile{TaskID: jsonUint(projectID), Type: "image", Name: name, Path: jsonUint(projectID) + "/" + name}).Error; err != nil {
			t.Fatal(err)
		}
	}
}

func jsonUint(v uint) string {
	data, _ := json.Marshal(v)
	return string(data)
}

func intPtr(v int) *int { return &v }

func onlyTaskInputs(t *testing.T, svc *CharacterLookService) (models.Task, map[string][]FileMeta) {
	t.Helper()
	var tasks []models.Task
	if err := svc.db.Find(&tasks).Error; err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected exactly one task, got %d", len(tasks))
	}
	files := map[string][]FileMeta{}
	if strings.TrimSpace(tasks[0].InputsJSON) != "" && tasks[0].InputsJSON != "null" {
		if err := json.Unmarshal([]byte(tasks[0].InputsJSON), &files); err != nil {
			t.Fatal(err)
		}
	}
	return tasks[0], files
}

func TestSaveAgeStageValidatesAndResetsReviewOnAppearanceChange(t *testing.T) {
	svc, project, character := setupAgeStageTestDB(t)
	if _, err := svc.SaveAgeStage(project.ID, character.ID, 0, AgeStageInput{Name: "童年", Age: intPtr(6)}); err == nil {
		t.Fatal("stage without appearance was accepted")
	}
	if _, err := svc.SaveAgeStage(project.ID, character.ID, 0, AgeStageInput{Name: "童年", Age: intPtr(0), Appearance: "6岁女孩"}); err == nil {
		t.Fatal("non-positive age was accepted")
	}
	stage, err := svc.SaveAgeStage(project.ID, character.ID, 0, AgeStageInput{Name: "童年", Age: intPtr(6), Appearance: "6岁女孩，圆脸，齐刘海短发，约115厘米"})
	if err != nil {
		t.Fatal(err)
	}
	if stage.PortraitEngine != AgeStagePortraitQwenEdit || stage.AuditStatus != models.LookStatusDraft {
		t.Fatalf("unexpected defaults: engine=%s status=%s", stage.PortraitEngine, stage.AuditStatus)
	}
	if _, err := svc.SaveAgeStage(project.ID, character.ID, 0, AgeStageInput{Name: "童年", Appearance: "重复"}); err == nil {
		t.Fatal("duplicate stage name for one character was accepted")
	}
	if err := svc.db.Model(stage).Updates(map[string]any{"portrait": "child.png", "audit_status": models.LookStatusApproved, "portrait_prompt": "旧稿"}).Error; err != nil {
		t.Fatal(err)
	}
	renamed, err := svc.SaveAgeStage(project.ID, character.ID, stage.ID, AgeStageInput{Name: "幼年", Age: intPtr(6), Appearance: stage.Appearance})
	if err != nil {
		t.Fatal(err)
	}
	if renamed.AuditStatus != models.LookStatusApproved || renamed.PortraitPrompt != "旧稿" {
		t.Fatalf("rename alone must keep review and prompt: %+v", renamed)
	}
	changed, err := svc.SaveAgeStage(project.ID, character.ID, stage.ID, AgeStageInput{Name: "幼年", Age: intPtr(7), Appearance: stage.Appearance})
	if err != nil {
		t.Fatal(err)
	}
	if changed.AuditStatus != models.LookStatusDraft || changed.PortraitPrompt != "" || changed.Portrait != "child.png" {
		t.Fatalf("age change must return to draft, clear prompt and keep portrait: %+v", changed)
	}
}

func TestAgeStageBelongsToCharacter(t *testing.T) {
	svc, project, character := setupAgeStageTestDB(t)
	other := models.Character{ProjectID: project.ID, Name: "另一人", Portrait: "other.png"}
	if err := svc.db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	stage, err := svc.SaveAgeStage(project.ID, other.ID, 0, AgeStageInput{Name: "童年", Appearance: "6岁男孩"})
	if err != nil {
		t.Fatal(err)
	}
	look := models.CharacterLook{ProjectID: project.ID, CharacterID: character.ID, Name: "布衣", Category: "clothing", Description: "棉布短衫"}
	if err := svc.db.Create(&look).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveOutfit(project.ID, character.ID, 0, OutfitInput{Name: "越权阶段", LookIDs: []uint{look.ID}, AgeStageID: stage.ID}); err == nil {
		t.Fatal("outfit accepted another character's age stage")
	}
	if _, _, err := svc.AgeStagePortraitPrompt(project.ID, character.ID, stage.ID); err == nil {
		t.Fatal("prompt preview accepted another character's age stage")
	}
}

func TestAgeStageQwenEditPortraitUsesMainPortraitAsOnlyReference(t *testing.T) {
	svc, project, character := setupAgeStageTestDB(t)
	attachAgeStageTaskService(t, svc, "qwen_image_2_1_multi_edit.json")
	registerAgeStageUploads(t, svc, project.ID, "face.png")
	stage, err := svc.SaveAgeStage(project.ID, character.ID, 0, AgeStageInput{Name: "童年", Age: intPtr(6), Appearance: "6岁女孩，圆脸，齐刘海短发"})
	if err != nil {
		t.Fatal(err)
	}
	_, draft, err := svc.AgeStagePortraitPrompt(project.ID, character.ID, stage.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<image1>", "童年（6岁）", "6岁女孩，圆脸，齐刘海短发", "不沿用<image1>中的年龄"} {
		if !strings.Contains(draft, want) {
			t.Fatalf("draft missing %q:\n%s", want, draft)
		}
	}
	if err := svc.StartAgeStagePortrait(project.ID, character.ID, stage.ID, "没有图片绑定的提示词"); err == nil {
		t.Fatal("qwen edit prompt without <image1> was submitted")
	}
	if err := svc.StartAgeStagePortrait(project.ID, character.ID, stage.ID, ""); err != nil {
		t.Fatal(err)
	}
	task, files := onlyTaskInputs(t, svc)
	if task.Prompt != draft {
		t.Fatalf("submitted prompt differs from reviewed draft")
	}
	refs := files["ref_images"]
	if len(refs) != 1 || refs[0].Name != "face.png" {
		t.Fatalf("qwen edit must reference only the main portrait, got %+v", refs)
	}
	var saved models.CharacterAgeStage
	if err := svc.db.First(&saved, stage.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.PortraitTaskID != task.TaskID || saved.PortraitPrompt != draft {
		t.Fatalf("stage did not record task and prompt: %+v", saved)
	}
	if err := svc.StartAgeStagePortrait(project.ID, character.ID, stage.ID, ""); err == nil {
		t.Fatal("duplicate in-flight portrait task accepted")
	}
}

func TestAgeStageTextToImagePortraitHasNoAdultReference(t *testing.T) {
	svc, project, character := setupAgeStageTestDB(t)
	attachAgeStageTaskService(t, svc, "krea2_character_portrait.json")
	stage, err := svc.SaveAgeStage(project.ID, character.ID, 0, AgeStageInput{Name: "少年", Age: intPtr(15), Appearance: "15岁少女，马尾", PortraitEngine: ImageEngineKrea2})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.StartAgeStagePortrait(project.ID, character.ID, stage.ID, ""); err != nil {
		t.Fatal(err)
	}
	task, files := onlyTaskInputs(t, svc)
	if len(files) != 0 {
		t.Fatalf("text-to-image stage portrait must not carry references, got %+v", files)
	}
	if !strings.Contains(task.Prompt, "15岁人物") || !strings.Contains(task.Prompt, "15岁少女，马尾") || strings.Contains(task.Prompt, "28岁") {
		t.Fatalf("unexpected text-to-image prompt: %s", task.Prompt)
	}
}

func TestAgeStageOutfitImageUsesStagePortraitWithoutAdultSheet(t *testing.T) {
	svc, project, character := setupAgeStageTestDB(t)
	attachAgeStageTaskService(t, svc, "krea2_storyboard_reference.json")
	registerAgeStageUploads(t, svc, project.ID, "child.png", "face.png", "adult_sheet.png")
	stage, err := svc.SaveAgeStage(project.ID, character.ID, 0, AgeStageInput{Name: "童年", Age: intPtr(6), Appearance: "6岁女孩"})
	if err != nil {
		t.Fatal(err)
	}
	outfit := models.CharacterOutfit{ProjectID: project.ID, CharacterID: character.ID, AgeStageID: stage.ID, Name: "童年日常", Description: "棉布童装", ImageEngine: ImageEngineKrea2, AuditStatus: models.LookStatusApproved}
	if err := svc.db.Create(&outfit).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.StartOutfitImage(project.ID, character.ID, outfit.ID, false); err == nil || !strings.Contains(err.Error(), "童年（6岁）") {
		t.Fatalf("outfit image must wait for an approved stage portrait, got %v", err)
	}
	if err := svc.db.Model(stage).Updates(map[string]any{"portrait": "child.png", "audit_status": models.LookStatusApproved}).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.StartOutfitImage(project.ID, character.ID, outfit.ID, false); err != nil {
		t.Fatal(err)
	}
	task, files := onlyTaskInputs(t, svc)
	refs := files["ref_images"]
	if len(refs) != 1 || refs[0].Name != "child.png" {
		t.Fatalf("stage outfit must use only the stage portrait, got %+v", refs)
	}
	if strings.Contains(task.Prompt, "四视图") {
		t.Fatalf("stage outfit prompt must not bind the adult sheet:\n%s", task.Prompt)
	}
}

func TestMainOutfitImageStillUsesCharacterPortraitAndSheet(t *testing.T) {
	svc, project, character := setupAgeStageTestDB(t)
	attachAgeStageTaskService(t, svc, "krea2_storyboard_reference.json")
	registerAgeStageUploads(t, svc, project.ID, "face.png", "adult_sheet.png")
	outfit := models.CharacterOutfit{ProjectID: project.ID, CharacterID: character.ID, Name: "成人日常", Description: "西装", ImageEngine: ImageEngineKrea2, AuditStatus: models.LookStatusApproved}
	if err := svc.db.Create(&outfit).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.StartOutfitImage(project.ID, character.ID, outfit.ID, false); err != nil {
		t.Fatal(err)
	}
	task, files := onlyTaskInputs(t, svc)
	refs := files["ref_images"]
	if len(refs) != 2 || refs[0].Name != "face.png" || refs[1].Name != "adult_sheet.png" {
		t.Fatalf("main outfit references changed: %+v", refs)
	}
	if task.Prompt != outfitPrompt(&models.CharacterOutfit{Description: "西装"}, true) {
		t.Fatalf("main outfit prompt changed:\n%s", task.Prompt)
	}
}

func TestNewAgeStagePortraitInvalidatesItsOutfitImagesOnly(t *testing.T) {
	svc, project, character := setupAgeStageTestDB(t)
	stage, err := svc.SaveAgeStage(project.ID, character.ID, 0, AgeStageInput{Name: "童年", Appearance: "6岁女孩"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.db.Model(stage).Updates(map[string]any{"portrait": "old_child.png", "audit_status": models.LookStatusApproved, "portrait_task_id": "task-new"}).Error; err != nil {
		t.Fatal(err)
	}
	stageOutfit := models.CharacterOutfit{ProjectID: project.ID, CharacterID: character.ID, AgeStageID: stage.ID, Name: "童年日常", Image: "child_outfit.png", Sheet: "child_sheet.png", AuditStatus: models.LookStatusApproved}
	mainOutfit := models.CharacterOutfit{ProjectID: project.ID, CharacterID: character.ID, Name: "成人日常", Image: "adult_outfit.png", Sheet: "adult_outfit_sheet.png", AuditStatus: models.LookStatusApproved}
	for _, row := range []*models.CharacterOutfit{&stageOutfit, &mainOutfit} {
		if err := svc.db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.applyAgeStagePortrait(stage.ID, "task-stale", "stale.png"); err != nil {
		t.Fatal(err)
	}
	var unchanged models.CharacterAgeStage
	svc.db.First(&unchanged, stage.ID)
	if unchanged.Portrait != "old_child.png" {
		t.Fatal("a superseded task overwrote the stage portrait")
	}
	if err := svc.applyAgeStagePortrait(stage.ID, "task-new", "new_child.png"); err != nil {
		t.Fatal(err)
	}
	var saved models.CharacterAgeStage
	svc.db.First(&saved, stage.ID)
	if saved.Portrait != "new_child.png" || saved.AuditStatus != models.LookStatusDraft || saved.PortraitTaskID != "" {
		t.Fatalf("stage portrait not applied for review: %+v", saved)
	}
	if got := ageStagePortraitHistory(saved.PortraitHistory); len(got) != 1 || got[0] != "old_child.png" {
		t.Fatalf("replaced stage portrait must be kept in history: %q", saved.PortraitHistory)
	}
	// 用户要求旧资产不被覆盖：阶段套装已有的定妆照/四视图保留，只提示可重新生成。
	var gotStage, gotMain models.CharacterOutfit
	svc.db.First(&gotStage, stageOutfit.ID)
	svc.db.First(&gotMain, mainOutfit.ID)
	if gotStage.Image != "child_outfit.png" || gotStage.Sheet != "child_sheet.png" || !strings.Contains(gotStage.ImageError, "年龄阶段标准像已更换") {
		t.Fatalf("stage outfit images must be kept with an outdated hint: %+v", gotStage)
	}
	if gotMain.Image != "adult_outfit.png" || gotMain.Sheet != "adult_outfit_sheet.png" {
		t.Fatalf("main outfit was touched: %+v", gotMain)
	}
}

func TestDeleteAgeStageRefusesWhileOutfitsAttached(t *testing.T) {
	svc, project, character := setupAgeStageTestDB(t)
	stage, err := svc.SaveAgeStage(project.ID, character.ID, 0, AgeStageInput{Name: "童年", Appearance: "6岁女孩"})
	if err != nil {
		t.Fatal(err)
	}
	outfit := models.CharacterOutfit{ProjectID: project.ID, CharacterID: character.ID, AgeStageID: stage.ID, Name: "童年日常"}
	if err := svc.db.Create(&outfit).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteAgeStage(project.ID, character.ID, stage.ID); err == nil {
		t.Fatal("stage with attached outfits was deleted")
	}
	if err := svc.db.Delete(&outfit).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteAgeStage(project.ID, character.ID, stage.ID); err != nil {
		t.Fatal(err)
	}
}

func TestAgeStagesAreIndependentAndPortraitHistoryRestorable(t *testing.T) {
	svc, project, character := setupAgeStageTestDB(t)
	six, err := svc.SaveAgeStage(project.ID, character.ID, 0, AgeStageInput{Name: "童年", Age: intPtr(6), Appearance: "6岁女孩"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.applyAgeStagePortrait(six.ID, "", "six_v1.png"); err != nil {
		t.Fatal(err)
	}
	nine, err := svc.SaveAgeStage(project.ID, character.ID, 0, AgeStageInput{Name: "九岁", Age: intPtr(9), Appearance: "9岁女孩"})
	if err != nil {
		t.Fatal(err)
	}
	if nine.ID == six.ID {
		t.Fatal("a new age must create a new stage")
	}
	if err := svc.applyAgeStagePortrait(nine.ID, "", "nine_v1.png"); err != nil {
		t.Fatal(err)
	}
	var gotSix models.CharacterAgeStage
	svc.db.First(&gotSix, six.ID)
	if gotSix.Portrait != "six_v1.png" {
		t.Fatalf("generating the 9-year-old look replaced the 6-year-old look: %+v", gotSix)
	}
	var ch models.Character
	svc.db.First(&ch, character.ID)
	if ch.Portrait != "face.png" || ch.Sheet != "adult_sheet.png" {
		t.Fatalf("age stages must never touch the main portrait/sheet: %+v", ch)
	}

	if err := svc.applyAgeStagePortrait(six.ID, "", "six_v2.png"); err != nil {
		t.Fatal(err)
	}
	if err := svc.RestoreAgeStagePortrait(project.ID, character.ID, six.ID, "six_v1.png"); err != nil {
		t.Fatal(err)
	}
	svc.db.First(&gotSix, six.ID)
	if gotSix.Portrait != "six_v1.png" {
		t.Fatalf("restore did not bring back the old portrait: %+v", gotSix)
	}
	if got := ageStagePortraitHistory(gotSix.PortraitHistory); len(got) != 1 || got[0] != "six_v2.png" {
		t.Fatalf("restore must keep the replaced portrait in history: %q", gotSix.PortraitHistory)
	}
	if err := svc.RestoreAgeStagePortrait(project.ID, character.ID, six.ID, "nine_v1.png"); err == nil {
		t.Fatal("restoring another stage's portrait must be rejected")
	}
}

func TestSaveAgeStagePortraitPromptOnlySavesPrompt(t *testing.T) {
	svc, project, character := setupAgeStageTestDB(t)
	stage, err := svc.SaveAgeStage(project.ID, character.ID, 0, AgeStageInput{Name: "童年", Age: intPtr(6), Appearance: "6岁女孩"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.applyAgeStagePortrait(stage.ID, "", "six.png"); err != nil {
		t.Fatal(err)
	}
	if err := svc.ApproveAgeStage(project.ID, character.ID, stage.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.SaveAgeStagePortraitPrompt(project.ID, character.ID, stage.ID, "没有参考图标记"); err == nil {
		t.Fatal("qwen_edit prompt without <image1> must be rejected")
	}
	prompt := "<image1>是角色主标准像。6岁女孩，齐刘海。"
	if err := svc.SaveAgeStagePortraitPrompt(project.ID, character.ID, stage.ID, prompt); err != nil {
		t.Fatal(err)
	}
	var got models.CharacterAgeStage
	svc.db.First(&got, stage.ID)
	if got.PortraitPrompt != prompt || got.Portrait != "six.png" || got.AuditStatus != models.LookStatusApproved || got.PortraitTaskID != "" {
		t.Fatalf("saving the prompt must not touch portrait, review or tasks: %+v", got)
	}
	saved, _, err := svc.AgeStagePortraitPrompt(project.ID, character.ID, stage.ID)
	if err != nil || saved != prompt {
		t.Fatalf("saved prompt not returned for review: %q %v", saved, err)
	}
}
