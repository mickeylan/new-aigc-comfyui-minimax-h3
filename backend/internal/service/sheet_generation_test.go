package service

import (
	"encoding/json"
	"strings"
	"testing"

	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
)

func newSheetGenerationService(t *testing.T) *ProjectService {
	t.Helper()
	ps := newTestProjectService(t)
	if err := ps.db.AutoMigrate(&models.Template{}, &models.Setting{}, &models.Instance{}, &models.UploadFile{}); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"moss_character_sheet.json", "krea2_character_portrait.json", "qwen_image_2_1_t2i.json", "krea2_character_sheet.json"} {
		tpl := loadTemplateForTest(t, file)
		if err := ps.db.Create(&tpl).Error; err != nil {
			t.Fatal(err)
		}
	}
	cfg := &config.Config{}
	ps.tasks = NewTaskService(cfg, ps.db, NewInstanceManager(cfg, ps.db, nil), nil, NewHub(), nil)
	return ps
}

func createSheetCharacter(t *testing.T, ps *ProjectService, engine, portrait string) *models.Character {
	t.Helper()
	p := models.Project{Title: "问仙", Genre: "古典修仙", Style: "国风写实"}
	ps.db.Create(&p)
	ch := models.Character{ProjectID: p.ID, Name: "林舒", SheetEngine: engine, Portrait: portrait, ProfileStatus: models.ProfileStatusApproved,
		Appearance: "22岁女性，乌黑长发梳成垂鬟分肖髻", WardrobeDetail: "白色交领仙裙，银色腰封，腰间垂青玉禁步，月白云头绣鞋", ColorPalette: "白、银、青"}
	if err := ps.db.Create(&ch).Error; err != nil {
		t.Fatal(err)
	}
	return &ch
}

func startedSheetTask(t *testing.T, ps *ProjectService, ch *models.Character) (models.Task, map[string]any) {
	t.Helper()
	var saved models.Character
	ps.db.First(&saved, ch.ID)
	var task models.Task
	if err := ps.db.Where("task_id = ?", saved.SheetTaskID).First(&task).Error; err != nil {
		t.Fatalf("sheet task not recorded: %v", err)
	}
	var params map[string]any
	if err := json.Unmarshal([]byte(task.ParamsJSON), &params); err != nil {
		t.Fatal(err)
	}
	return task, params
}

func TestStartCharacterSheetDefaultsToMossWithoutPortrait(t *testing.T) {
	ps := newSheetGenerationService(t)
	ch := createSheetCharacter(t, ps, "", "")
	if err := ps.StartCharacterSheet(ch, false); err != nil {
		t.Fatalf("direct sheet generation must not require a portrait: %v", err)
	}
	task, params := startedSheetTask(t, ps, ch)
	if task.TemplateName != loadTemplateForTest(t, "moss_character_sheet.json").Name {
		t.Fatalf("sheet must default to the Moss template, got %s", task.TemplateName)
	}
	if strings.Contains(task.InputsJSON, "source_image") {
		t.Fatalf("direct sheet must not upload a source image: %s", task.InputsJSON)
	}
	if params["aspect_ratio"] != "16:9 (Widescreen)" {
		t.Fatalf("Moss sheet must be 16:9, got %v", params["aspect_ratio"])
	}
	for _, want := range []string{"由左到右排列：正面面部特写、正面全身站立、侧面全身站立、背面全身站立", "银色腰封", "青玉禁步", "月白云头绣鞋", "从头顶到鞋底完整入镜", "纯白色干净背景"} {
		if !strings.Contains(task.Prompt, want) {
			t.Fatalf("sheet prompt missing %q: %s", want, task.Prompt)
		}
	}
	if strings.Contains(task.Prompt, "supplied face portrait") {
		t.Fatalf("text-to-image sheet prompt must not reference a portrait: %s", task.Prompt)
	}
	// 外貌已写明发髻时不得再叠加默认时代发型，避免与档案发型冲突。
	if strings.Contains(task.Prompt, "具体发型：") {
		t.Fatalf("declared hairstyle must not be duplicated: %s", task.Prompt)
	}
	tpl := loadTemplateForTest(t, "moss_character_sheet.json")
	workflow, err := ps.tasks.RenderWorkflow(&tpl, params)
	if err != nil {
		t.Fatalf("Moss workflow must render: %v", err)
	}
	encoded, _ := json.Marshal(workflow)
	if strings.Contains(string(encoded), "{{") || !strings.Contains(string(encoded), "moss_image") {
		t.Fatalf("Moss workflow left placeholders or lost its CLIP type: %s", encoded)
	}
}

func TestStartCharacterSheetKrea2UsesLandscapeTextToImage(t *testing.T) {
	ps := newSheetGenerationService(t)
	ch := createSheetCharacter(t, ps, ImageEngineKrea2, "")
	if err := ps.StartCharacterSheet(ch, false); err != nil {
		t.Fatal(err)
	}
	task, params := startedSheetTask(t, ps, ch)
	if task.TemplateName != loadTemplateForTest(t, "krea2_character_portrait.json").Name {
		t.Fatalf("Krea2 sheet must reuse the text-to-image template, got %s", task.TemplateName)
	}
	if params["width"] != float64(1344) || params["height"] != float64(768) {
		t.Fatalf("Krea2 sheet must be landscape: %v x %v", params["width"], params["height"])
	}
}

func TestStartCharacterSheetQwenUsesWidescreenTextToImage(t *testing.T) {
	ps := newSheetGenerationService(t)
	ch := createSheetCharacter(t, ps, ImageEngineQwen21, "")
	if err := ps.StartCharacterSheet(ch, false); err != nil {
		t.Fatal(err)
	}
	task, params := startedSheetTask(t, ps, ch)
	if task.TemplateName != loadTemplateForTest(t, "qwen_image_2_1_t2i.json").Name {
		t.Fatalf("Qwen sheet must reuse qwen_image_2_1_t2i, got %s", task.TemplateName)
	}
	if params["aspect_ratio"] != "16:9 (Widescreen)" {
		t.Fatalf("Qwen sheet aspect ratio = %v", params["aspect_ratio"])
	}
}

func TestStartCharacterSheetFromPortraitKeepsEditPath(t *testing.T) {
	ps := newSheetGenerationService(t)
	ch := createSheetCharacter(t, ps, ImageEngineQwen21, "")
	if err := ps.StartCharacterSheet(ch, true); err == nil || !strings.Contains(err.Error(), "标准像") {
		t.Fatalf("portrait edit path must still require a portrait, err=%v", err)
	}
	ch.Portrait = "face.png"
	ps.db.Model(ch).Update("portrait", ch.Portrait)
	ps.db.Create(&models.UploadFile{TaskID: "1", Type: "image", Name: "face.png", Path: "1/face.png"})
	if err := ps.StartCharacterSheet(ch, true); err != nil {
		t.Fatal(err)
	}
	task, _ := startedSheetTask(t, ps, ch)
	if task.TemplateName != loadTemplateForTest(t, "krea2_character_sheet.json").Name || !strings.Contains(task.InputsJSON, "face.png") {
		t.Fatalf("portrait path must use the Krea2 edit template with the portrait: %s %s", task.TemplateName, task.InputsJSON)
	}
}

func TestStartCharacterSheetDirectRequiresApprovedProfile(t *testing.T) {
	ps := newSheetGenerationService(t)
	ch := createSheetCharacter(t, ps, ImageEngineKrea2, "")
	ch.ProfileStatus = models.ProfileStatusDraft
	if err := ps.StartCharacterSheet(ch, false); err == nil || !strings.Contains(err.Error(), "审核通过") {
		t.Fatalf("draft profile must not generate a sheet, err=%v", err)
	}
}

func TestUpdateCharacterSheetEngineKeepsExistingSheet(t *testing.T) {
	ps := newSheetGenerationService(t)
	ch := createSheetCharacter(t, ps, "", "")
	ps.db.Model(ch).Update("sheet", "sheet.png")
	if err := ps.UpdateCharacter(ch, models.Character{Name: ch.Name, SheetEngine: ImageEngineQwen21}); err != nil {
		t.Fatal(err)
	}
	var saved models.Character
	ps.db.First(&saved, ch.ID)
	if saved.SheetEngine != ImageEngineQwen21 || saved.Sheet != "sheet.png" {
		t.Fatalf("sheet engine switch must persist without discarding the sheet: %+v", saved)
	}
	if err := ps.UpdateCharacter(&saved, models.Character{Name: ch.Name, SheetEngine: "midjourney"}); err == nil {
		t.Fatal("unknown sheet engine accepted")
	}
}

func TestStartCharacterPortraitKeepsExistingSheet(t *testing.T) {
	ps := newSheetGenerationService(t)
	ch := createSheetCharacter(t, ps, ImageEngineKrea2, "")
	ch.ReferencePrompt = "22岁女性，正面肩部以上"
	ch.Sheet = "sheet.png"
	ps.db.Model(ch).Updates(map[string]any{"reference_prompt": ch.ReferencePrompt, "sheet": ch.Sheet})
	if err := ps.StartCharacterPortrait(ch); err != nil {
		t.Fatal(err)
	}
	var saved models.Character
	ps.db.First(&saved, ch.ID)
	if saved.Sheet != "sheet.png" || saved.PortraitTaskID == "" {
		t.Fatalf("regenerating the optional portrait must not clear the sheet: %+v", saved)
	}
}
