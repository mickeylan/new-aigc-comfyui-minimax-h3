package service

import (
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

func TestCharacterRegistrationPhotoSkillIsSeeded(t *testing.T) {
	db := newTestDB(t)
	if err := NewSkillService(db).InitSystemSkills(); err != nil {
		t.Fatal(err)
	}
	var skill models.Skill
	if err := db.Where("code = ?", characterRegistrationSkillCode).First(&skill).Error; err != nil {
		t.Fatal(err)
	}
	if skill.Version != 1 || skill.Stage != models.SkillStageQwenImageEdit || !strings.Contains(skill.SystemPrompt, "单人正面胸像登记照") {
		t.Fatalf("registration skill=%+v", skill)
	}
}

func TestStartCharacterRegistrationPhotoUsesSheetAsOnlyReference(t *testing.T) {
	ps := newSheetGenerationService(t)
	tpl := loadTemplateForTest(t, "qwen_image_2_1_multi_edit.json")
	if err := ps.db.Create(&tpl).Error; err != nil {
		t.Fatal(err)
	}
	ch := createSheetCharacter(t, ps, "", "")
	if err := ps.StartCharacterRegistrationPhoto(ch); err == nil || !strings.Contains(err.Error(), "四视图") {
		t.Fatalf("registration photo must require a sheet, err=%v", err)
	}
	ch.Sheet = "sheet.png"
	ps.db.Model(ch).Update("sheet", ch.Sheet)
	ps.db.Create(&models.UploadFile{TaskID: "1", Type: "image", Name: "sheet.png", Path: "1/sheet.png"})
	if err := ps.StartCharacterRegistrationPhoto(ch); err != nil {
		t.Fatal(err)
	}
	var saved models.Character
	ps.db.First(&saved, ch.ID)
	var task models.Task
	if err := ps.db.Where("task_id = ?", saved.RegistrationTaskID).First(&task).Error; err != nil {
		t.Fatalf("registration task not recorded: %v", err)
	}
	if task.TemplateName != tpl.Name || !strings.Contains(task.InputsJSON, "sheet.png") || strings.Contains(task.InputsJSON, "face") {
		t.Fatalf("registration task must use Qwen Edit with only the sheet: %s %s", task.TemplateName, task.InputsJSON)
	}
	if !strings.Contains(task.Prompt, "参考图是林舒的四视图人物设定") || strings.Contains(task.Prompt, "{{") {
		t.Fatalf("registration prompt=%q", task.Prompt)
	}
	if err := ps.StartCharacterRegistrationPhoto(&saved); err == nil || !strings.Contains(err.Error(), "正在生成") {
		t.Fatalf("an active registration task must block a second submission, err=%v", err)
	}
}

func TestRegistrationPhotoIsAnExplicitSceneReferenceCandidate(t *testing.T) {
	ps := newSheetGenerationService(t)
	ch := createSheetCharacter(t, ps, "", "")
	ps.db.Model(ch).Update("registration_photo", "reg.png")
	scene := models.Scene{ProjectID: ch.ProjectID, Characters: ch.Name}
	ps.db.Create(&scene)
	found := false
	for _, c := range ps.sceneReferenceCandidates(&scene) {
		if c.SourceType == "character" && c.SourceID == ch.ID && c.Variant == "registration" && c.Image == "reg.png" {
			found = true
		}
	}
	if !found {
		t.Fatal("registration photo missing from scene reference candidates")
	}
}
