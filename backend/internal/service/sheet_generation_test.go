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

func TestGenerateSheetPromptAppendsFixedLayoutToLLMDescription(t *testing.T) {
	ps := newSheetGenerationService(t)
	ch := createSheetCharacter(t, ps, "", "")
	ch.Sheet = "sheet.png"
	ps.db.Model(ch).Update("sheet", ch.Sheet)
	var project models.Project
	ps.db.First(&project, ch.ProjectID)
	// 模型自带的版式句必须被截掉，由系统统一追加，避免重复或互相矛盾。
	provider := &captureTextProvider{response: "女性，22岁，乌黑长发梳成垂鬟分肖髻，白色交领仙裙、银色腰封与青玉禁步。由左到右排列：正面、背面"}
	prompt, err := NewCharacterProfileService(ps.db, provider).GenerateSheetPrompt(ch, &project)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"银色腰封与青玉禁步", "月白云头绣鞋", "由左到右排列：正面面部特写、正面全身站立、侧面全身站立、背面全身站立", "16:9画面比例"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("sheet prompt missing %q: %s", want, prompt)
		}
	}
	if strings.Count(prompt, "由左到右") != 1 {
		t.Fatalf("layout must appear exactly once: %s", prompt)
	}
	if !strings.Contains(provider.user, "银色腰封") || !strings.Contains(provider.system, "下装、腰部配饰和鞋履必须完整写出") {
		t.Fatalf("LLM request lost wardrobe facts or contract: system=%s user=%s", provider.system, provider.user)
	}
	var saved models.Character
	ps.db.First(&saved, ch.ID)
	if saved.SheetPrompt != prompt || saved.Sheet != "sheet.png" || saved.ProfileStatus != models.ProfileStatusApproved {
		t.Fatalf("sheet prompt must persist without touching sheet or approval: %+v", saved)
	}
}

func TestStartCharacterSheetUsesEditedSheetPrompt(t *testing.T) {
	ps := newSheetGenerationService(t)
	ch := createSheetCharacter(t, ps, "", "")
	ch.SheetPrompt = "女性，22岁，腰间改系朱红丝绦"
	ps.db.Model(ch).Update("sheet_prompt", ch.SheetPrompt)
	if err := ps.StartCharacterSheet(ch, false); err != nil {
		t.Fatal(err)
	}
	task, _ := startedSheetTask(t, ps, ch)
	if !strings.HasPrefix(task.Prompt, "女性，22岁，腰间改系朱红丝绦。") || !strings.Contains(task.Prompt, "由左到右排列：正面面部特写") {
		t.Fatalf("edited sheet prompt must be used with the fixed layout appended: %s", task.Prompt)
	}
	if strings.Contains(task.Prompt, "垂鬟分肖髻") {
		t.Fatalf("edited sheet prompt must replace the compiled description: %s", task.Prompt)
	}
}

func TestSaveSheetPromptKeepsApprovalAndAssets(t *testing.T) {
	ps := newSheetGenerationService(t)
	ch := createSheetCharacter(t, ps, "", "portrait.png")
	ps.db.Model(ch).Update("sheet", "sheet.png")
	if err := NewCharacterProfileService(ps.db, nil).SaveSheetPrompt(ch, "  女性，22岁，腰间改系朱红丝绦  "); err != nil {
		t.Fatal(err)
	}
	var saved models.Character
	ps.db.First(&saved, ch.ID)
	if saved.SheetPrompt != "女性，22岁，腰间改系朱红丝绦" || saved.ProfileStatus != models.ProfileStatusApproved || saved.Sheet != "sheet.png" || saved.Portrait != "portrait.png" {
		t.Fatalf("saving the sheet prompt must not reset approval or discard assets: %+v", saved)
	}
}

func TestUpdateProfileClearsSheetPromptUnlessProvided(t *testing.T) {
	ps := newSheetGenerationService(t)
	ch := createSheetCharacter(t, ps, "", "")
	ps.db.Model(ch).Update("sheet_prompt", "旧四视图提示词")
	service := NewCharacterProfileService(ps.db, nil)
	if err := service.UpdateProfile(ch, models.Character{Appearance: "新外貌", SheetPrompt: "用户修改后的四视图提示词"}); err != nil {
		t.Fatal(err)
	}
	var saved models.Character
	ps.db.First(&saved, ch.ID)
	if saved.SheetPrompt != "用户修改后的四视图提示词" {
		t.Fatalf("edited sheet prompt not saved: %q", saved.SheetPrompt)
	}
	if err := service.UpdateProfile(&saved, models.Character{Appearance: "再改外貌"}); err != nil {
		t.Fatal(err)
	}
	ps.db.First(&saved, ch.ID)
	if saved.SheetPrompt != "" {
		t.Fatalf("profile replacement without sheet prompt must clear it: %q", saved.SheetPrompt)
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
