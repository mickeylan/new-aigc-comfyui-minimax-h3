package service

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

// 用户要求：阶段形象直接生成四视图（自带正面特写），不再先生成标准像；提示词沿用 Moss 四视图结构，
// 且必须带主标准像参考（Moss 只支持文生图，默认改用 Qwen 多图编辑）。
func TestAgeStageDefaultsToQwenEditSheetWithMainPortrait(t *testing.T) {
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
	if !strings.HasPrefix(draft, "<image1>是角色「林采微」的主标准像") {
		t.Fatalf("draft must bind the main portrait first:\n%s", draft)
	}
	for _, want := range []string{"不沿用<image1>中的年龄", "6岁女孩，圆脸，齐刘海短发，年龄感、身高头身比例与面部成熟度严格符合该年龄", "由左到右排列：第1格正面面部特写", "第2至第4格从头顶到鞋底完整入镜", "指定鞋履："} {
		if !strings.Contains(draft, want) {
			t.Fatalf("draft missing %q:\n%s", want, draft)
		}
	}
	// 用户要求：阶段外貌已写明年龄时不再前置“6岁人物”，同一年龄只出现一次，也不出现“。，”。
	if strings.Count(draft, "6岁") != 1 || strings.Contains(draft, "。，") {
		t.Fatalf("age must appear once without broken punctuation:\n%s", draft)
	}
	for _, banned := range []string{"28岁", "高挑", "标准参考像"} {
		if strings.Contains(draft, banned) {
			t.Fatalf("draft must not carry %q:\n%s", banned, draft)
		}
	}
	if err := svc.StartAgeStagePortrait(project.ID, character.ID, stage.ID, "6岁女孩。由左到右排列四视图"); err == nil {
		t.Fatal("a prompt that drops the <image1> binding was submitted")
	}
	if err := svc.StartAgeStagePortrait(project.ID, character.ID, stage.ID, ""); err != nil {
		t.Fatal(err)
	}
	task, files := onlyTaskInputs(t, svc)
	if task.Prompt != draft {
		t.Fatalf("submitted prompt differs from reviewed draft")
	}
	if refs := files["ref_images"]; len(files) != 1 || len(refs) != 1 || refs[0].Name != "face.png" {
		t.Fatalf("stage sheet must use only the main portrait as reference, got %+v", files)
	}
	var tpl models.Template
	if err := svc.db.First(&tpl, task.TemplateID).Error; err != nil || tpl.Code != TemplateQwen21MultiEdit {
		t.Fatalf("stage sheet must use the Qwen multi-edit template, got %q %v", tpl.Code, err)
	}
	var params map[string]any
	if err := json.Unmarshal([]byte(task.ParamsJSON), &params); err != nil {
		t.Fatal(err)
	}
	if params["aspect_ratio"] != "16:9 (Widescreen)" || params["use_aspect_ratio"] != true {
		t.Fatalf("stage sheet must switch on the fixed 16:9 canvas, got %+v", params)
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

// 用户确认 Moss 与 Krea2 都只支持文生图：选过 krea2/moss 的阶段也只走 Qwen 多图编辑并带主标准像。
func TestAgeStageTextOnlyEnginesFallBackToQwenEdit(t *testing.T) {
	for _, engine := range []string{ImageEngineKrea2, ImageEngineMoss} {
		svc, project, character := setupAgeStageTestDB(t)
		attachAgeStageTaskService(t, svc, "qwen_image_2_1_multi_edit.json")
		registerAgeStageUploads(t, svc, project.ID, "face.png")
		stage, err := svc.SaveAgeStage(project.ID, character.ID, 0, AgeStageInput{Name: "少年", Age: intPtr(15), Appearance: "15岁少女，马尾", PortraitEngine: engine})
		if err != nil {
			t.Fatal(err)
		}
		if stage.PortraitEngine != AgeStagePortraitQwenEdit {
			t.Fatalf("%s must be saved as qwen_edit, got %q", engine, stage.PortraitEngine)
		}
		if err := svc.StartAgeStagePortrait(project.ID, character.ID, stage.ID, ""); err != nil {
			t.Fatal(err)
		}
		task, files := onlyTaskInputs(t, svc)
		if refs := files["ref_images"]; len(files) != 1 || len(refs) != 1 || refs[0].Name != "face.png" {
			t.Fatalf("%s stage sheet must reference the main portrait, got %+v", engine, files)
		}
		var tpl models.Template
		if err := svc.db.First(&tpl, task.TemplateID).Error; err != nil || tpl.Code != TemplateQwen21MultiEdit {
			t.Fatalf("%s stage sheet must use the Qwen multi-edit template, got %q %v", engine, tpl.Code, err)
		}
		if !strings.HasPrefix(task.Prompt, "<image1>") || !strings.Contains(task.Prompt, "15岁少女，马尾") || strings.Contains(task.Prompt, "28岁") {
			t.Fatalf("%s stage sheet prompt must bind <image1> and keep the stage appearance: %s", engine, task.Prompt)
		}
	}
}

func TestAgeStageSheetRequiresMainPortrait(t *testing.T) {
	svc, project, character := setupAgeStageTestDB(t)
	attachAgeStageTaskService(t, svc, "qwen_image_2_1_multi_edit.json")
	if err := svc.db.Model(&character).Update("portrait", "").Error; err != nil {
		t.Fatal(err)
	}
	stage, err := svc.SaveAgeStage(project.ID, character.ID, 0, AgeStageInput{Name: "童年", Age: intPtr(6), Appearance: "6岁女孩"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.StartAgeStagePortrait(project.ID, character.ID, stage.ID, ""); err == nil || !strings.Contains(err.Error(), "主标准像") {
		t.Fatalf("stage sheet without a main portrait reference must be refused, got %v", err)
	}
	var count int64
	svc.db.Model(&models.Task{}).Count(&count)
	if count != 0 {
		t.Fatal("no task may be created without a reference image")
	}
}

// 用户实例：柳乐儿 19 岁阶段。年龄写在主体最前，阶段外貌里的衣裙与短靴原样进入提示词并锁定鞋履。
func TestAgeStageSheetPromptLeadsWithAgeAndKeepsDeclaredOutfit(t *testing.T) {
	project := models.Project{Style: "国漫", Genre: "古风仙侠"}
	ch := models.Character{Name: "柳乐儿", Gender: "女", Appearance: "6岁女孩，圆脸"}
	stage := models.CharacterAgeStage{Name: "青年", Age: intPtr(19), Appearance: "身着白色衫裙的20岁模样俏丽女子，黑发及腰，脚穿藕色布制中国古典短靴"}
	prompt := ageStageSheetPrompt(&project, &ch, &stage)
	if !strings.HasPrefix(prompt, "<image1>是角色「柳乐儿」的主标准像") {
		t.Fatalf("prompt must bind the main portrait first:\n%s", prompt)
	}
	if !strings.Contains(prompt, "与服装。19岁女性，年龄感、身高头身比例与面部成熟度严格符合该年龄，身着白色衫裙") {
		t.Fatalf("age must lead the subject right after the reference binding:\n%s", prompt)
	}
	if strings.Count(prompt, "19岁") != 1 || strings.Contains(prompt, "。，") {
		t.Fatalf("stage age must be written once without broken punctuation:\n%s", prompt)
	}
	for _, want := range []string{"指定鞋履：脚穿藕色布制中国古典短靴", "由左到右排列"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
	for _, banned := range []string{"6岁", "圆脸", "朴素日常基础服装", "云头绣鞋"} {
		if strings.Contains(prompt, banned) {
			t.Fatalf("prompt must not carry %q:\n%s", banned, prompt)
		}
	}
}

// 旧数据：文生图来源（qwen_image_2_1）改为 Qwen 多图编辑；旧的 9:16 阶段标准像提示词不再复用。
func TestLegacyStageMovesToReferencedSheet(t *testing.T) {
	svc, project, character := setupAgeStageTestDB(t)
	attachAgeStageTaskService(t, svc, "qwen_image_2_1_multi_edit.json")
	registerAgeStageUploads(t, svc, project.ID, "face.png")
	stage := models.CharacterAgeStage{ProjectID: project.ID, CharacterID: character.ID, Name: "童年", Age: intPtr(6), Appearance: "6岁女孩", PortraitEngine: ImageEngineQwen21, PortraitPrompt: "6岁人物，正面肩部以上头像，9:16竖版标准参考像", AuditStatus: models.LookStatusDraft}
	if err := svc.db.Create(&stage).Error; err != nil {
		t.Fatal(err)
	}
	saved, draft, err := svc.AgeStagePortraitPrompt(project.ID, character.ID, stage.ID)
	if err != nil || saved != "" || !strings.HasPrefix(draft, "<image1>") || !strings.Contains(draft, "由左到右") {
		t.Fatalf("legacy portrait prompt must not be offered for a sheet: saved=%q draft=%q err=%v", saved, draft, err)
	}
	if err := svc.StartAgeStagePortrait(project.ID, character.ID, stage.ID, ""); err != nil {
		t.Fatal(err)
	}
	task, files := onlyTaskInputs(t, svc)
	if refs := files["ref_images"]; len(refs) != 1 || refs[0].Name != "face.png" || task.Prompt != draft {
		t.Fatalf("legacy stage must submit a referenced sheet: files=%+v prompt=%s", files, task.Prompt)
	}
}

func TestExpandAgeStageAppearanceKeepsUserTextAndDoesNotSave(t *testing.T) {
	svc, project, character := setupAgeStageTestDB(t)
	attachAgeStageSkills(t, svc)
	if _, _, err := svc.ExpandAgeStageAppearance(project.ID, character.ID, AgeStageExpandInput{Name: "童年", Age: intPtr(6), Brief: "齐刘海"}); err == nil {
		t.Fatal("expand without a text provider must fail")
	}
	provider := &captureTextProvider{response: "<think>草稿</think>“6岁女孩，圆脸，齐刘海短发，约115厘米。基础服饰：浅蓝棉布童装，布鞋。”"}
	svc.textProvider = provider
	if _, _, err := svc.ExpandAgeStageAppearance(project.ID, character.ID, AgeStageExpandInput{Name: "童年", Age: intPtr(6)}); err == nil {
		t.Fatal("empty brief must be rejected before calling the model")
	}
	text, skill, err := svc.ExpandAgeStageAppearance(project.ID, character.ID, AgeStageExpandInput{Name: "童年", Age: intPtr(6), Brief: "齐刘海短发，圆脸"})
	if err != nil {
		t.Fatal(err)
	}
	if text != "6岁女孩，圆脸，齐刘海短发，约115厘米。基础服饰：浅蓝棉布童装，布鞋。" {
		t.Fatalf("think block and quotes must be stripped, got %q", text)
	}
	// 非成年女性阶段走角色设定阶段的项目 Skill（默认“角色设定标准”），且只取其设计规则。
	if !strings.HasPrefix(skill, "角色设定标准 v") || !strings.Contains(provider.system, "【当前项目 Skill】") || !strings.Contains(provider.system, "东方幻想漫剧角色设计师") {
		t.Fatalf("expansion must use the project character skill, skill=%q system:\n%s", skill, provider.system)
	}
	if strings.Contains(provider.user, "请为以下角色生成完整的结构化档案") {
		t.Fatalf("the profile skill's full-profile request must not be sent for a stage expansion:\n%s", provider.user)
	}
	var audits int64
	svc.db.Model(&models.SkillAuditLog{}).Where("project_id = ? AND skill_code = ?", project.ID, "character-standard").Count(&audits)
	if audits != 1 {
		t.Fatalf("skill use must be audited once, got %d", audits)
	}
	for _, want := range []string{"用户描述：齐刘海短发，圆脸", "该阶段年龄：6岁", "28岁女性，高挑"} {
		if !strings.Contains(provider.user, want) {
			t.Fatalf("model input missing %q:\n%s", want, provider.user)
		}
	}
	if !strings.Contains(provider.system, "不得沿用主档案的年龄") || !strings.Contains(provider.system, "用户写明的内容是权威") {
		t.Fatalf("system prompt lost its age/authority rules:\n%s", provider.system)
	}
	var count int64
	svc.db.Model(&models.CharacterAgeStage{}).Count(&count)
	if count != 0 {
		t.Fatal("expanding must not create or save a stage")
	}
}

func attachAgeStageSkills(t *testing.T, svc *CharacterLookService) {
	t.Helper()
	if err := svc.db.AutoMigrate(&models.Skill{}, &models.ProjectSkillConfig{}, &models.SkillAuditLog{}); err != nil {
		t.Fatal(err)
	}
	svc.skills = NewSkillService(svc.db)
	if err := svc.skills.InitSystemSkills(); err != nil {
		t.Fatal(err)
	}
}

// 成年女性阶段与角色档案一致，走“成年女性角色美术增强”Skill。
func TestExpandAdultFemaleAgeStageUsesBeautySkill(t *testing.T) {
	svc, project, character := setupAgeStageTestDB(t)
	attachAgeStageSkills(t, svc)
	if err := svc.db.Model(&character).Update("gender", "女性").Error; err != nil {
		t.Fatal(err)
	}
	provider := &captureTextProvider{response: "20岁女性，鹅蛋脸，杏眼。"}
	svc.textProvider = provider
	_, skill, err := svc.ExpandAgeStageAppearance(project.ID, character.ID, AgeStageExpandInput{Name: "成年", Age: intPtr(20), Brief: "杏眼"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(skill, "成年女性角色美术增强 v") || !strings.Contains(provider.system, "女性角色美术增强契约") {
		t.Fatalf("adult female stage must use the beauty skill, skill=%q system:\n%s", skill, provider.system)
	}
	// 女性优先：没填阶段年龄也走美术增强；用户描述写明未成年时改走通用角色 Skill。
	if _, skill, err = svc.ExpandAgeStageAppearance(project.ID, character.ID, AgeStageExpandInput{Name: "宗主", Brief: "杏眼"}); err != nil || !strings.HasPrefix(skill, "成年女性角色美术增强 v") {
		t.Fatalf("female stage without age must prefer the beauty skill, skill=%q err=%v", skill, err)
	}
	if _, skill, err = svc.ExpandAgeStageAppearance(project.ID, character.ID, AgeStageExpandInput{Name: "少女", Brief: "15岁，杏眼"}); err != nil || !strings.HasPrefix(skill, "角色设定标准 v") {
		t.Fatalf("minor female stage must use the general character skill, skill=%q err=%v", skill, err)
	}
}

// 古典修仙题材：扩写约束中国古代形制与平底鞋履；模型写出短裙、高跟鞋时丢弃结果；四视图提示词与负面词同样约束。
func TestClassicalAgeStageForbidsModernDress(t *testing.T) {
	svc, project, character := setupAgeStageTestDB(t)
	attachAgeStageSkills(t, svc)
	attachAgeStageTaskService(t, svc, "qwen_image_2_1_multi_edit.json")
	registerAgeStageUploads(t, svc, project.ID, "face.png")
	if err := svc.db.Model(&project).Update("genre", "古典修仙").Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.db.Model(&character).Update("gender", "女性").Error; err != nil {
		t.Fatal(err)
	}
	provider := &captureTextProvider{response: "20岁女性，杏眼。基础服饰：粉色短裙，白色高跟鞋。"}
	svc.textProvider = provider
	if _, _, err := svc.ExpandAgeStageAppearance(project.ID, character.ID, AgeStageExpandInput{Name: "成年", Age: intPtr(20), Brief: "杏眼"}); err == nil || !strings.Contains(err.Error(), "古典题材") {
		t.Fatalf("modern dress in a classical project must be rejected, got %v", err)
	}
	for _, want := range []string{"中国古代形制", "禁止短裙", "高跟鞋"} {
		if !strings.Contains(provider.system, want) {
			t.Fatalf("classical rule missing %q:\n%s", want, provider.system)
		}
	}
	provider.response = "20岁女性，杏眼。基础服饰：藕色交领襦裙，长及脚踝，藕色绣鞋。"
	if text, _, err := svc.ExpandAgeStageAppearance(project.ID, character.ID, AgeStageExpandInput{Name: "成年", Age: intPtr(20), Brief: "杏眼"}); err != nil || !strings.Contains(text, "交领襦裙") {
		t.Fatalf("classical dress must pass, text=%q err=%v", text, err)
	}
	stage, err := svc.SaveAgeStage(project.ID, character.ID, 0, AgeStageInput{Name: "成年", Age: intPtr(20), Appearance: "20岁女性，杏眼"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.StartAgeStagePortrait(project.ID, character.ID, stage.ID, ""); err != nil {
		t.Fatal(err)
	}
	task, _ := onlyTaskInputs(t, svc)
	for _, want := range []string{"中国古代形制朴素日常服装", "不出现短裙、丝袜、高跟鞋", "中式软底云头绣鞋", "16:9画面比例"} {
		if !strings.Contains(task.Prompt, want) {
			t.Fatalf("classical sheet prompt missing %q:\n%s", want, task.Prompt)
		}
	}
	var params map[string]any
	if err := json.Unmarshal([]byte(task.ParamsJSON), &params); err != nil {
		t.Fatal(err)
	}
	if neg, _ := params["negative_prompt"].(string); !strings.Contains(neg, "短裙") || !strings.Contains(neg, "高跟鞋") {
		t.Fatalf("classical sheet negative prompt must forbid modern dress, got %q", neg)
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
	if gotStage.Image != "child_outfit.png" || gotStage.Sheet != "child_sheet.png" || !strings.Contains(gotStage.ImageError, "年龄阶段四视图已更换") {
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

func TestDeleteAgeStagePortraitHistoryRemovesEntryAndFileOnly(t *testing.T) {
	svc, project, character := setupAgeStageTestDB(t)
	root := t.TempDir()
	cfg := &config.Config{
		Comfy:   config.ComfyConfig{ComfyDir: filepath.Join(root, "ComfyUI")},
		Storage: config.StorageConfig{DataDir: filepath.Join(root, "data")},
	}
	remote := NewRemoteExec(config.RemoteConfig{})
	remote.SetLocalRoots([]string{cfg.Comfy.ComfyDir, cfg.Storage.DataDir, cfg.AssetsRoot()})
	upload := NewUploadManager(cfg, svc.db, remote)
	svc.SetDeps(nil, remote, upload)

	stage, err := svc.SaveAgeStage(project.ID, character.ID, 0, AgeStageInput{Name: "童年", Age: intPtr(6), Appearance: "6岁女孩"})
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"stage_v1.png", "stage_v2.png", "stage_v3.png"} {
		if _, _, err := upload.SaveFile(fmt.Sprint(project.ID), "image", file, []byte("png")); err != nil {
			t.Fatal(err)
		}
		if err := svc.applyAgeStagePortrait(stage.ID, "", file); err != nil {
			t.Fatal(err)
		}
	}
	var got models.CharacterAgeStage
	svc.db.First(&got, stage.ID)
	if got.Portrait != "stage_v3.png" || strings.Join(ageStagePortraitHistory(got.PortraitHistory), ",") != "stage_v2.png,stage_v1.png" {
		t.Fatalf("setup history=%q portrait=%q", got.PortraitHistory, got.Portrait)
	}
	assetPath := func(file string) string { return filepath.Join(cfg.AssetsRoot(), fmt.Sprint(project.ID), file) }

	if _, err := svc.DeleteAgeStagePortraitHistory(project.ID, character.ID, stage.ID, ""); err == nil {
		t.Fatal("an empty file name must be rejected")
	}
	if _, err := svc.DeleteAgeStagePortraitHistory(project.ID, character.ID, stage.ID, "stage_v3.png"); err == nil {
		t.Fatal("the portrait currently in use must not be deleted")
	}
	if _, err := svc.DeleteAgeStagePortraitHistory(project.ID, character.ID, stage.ID, "not_in_history.png"); err == nil {
		t.Fatal("a file that is not in history must be rejected")
	}

	remaining, err := svc.DeleteAgeStagePortraitHistory(project.ID, character.ID, stage.ID, "stage_v1.png")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(remaining, ",") != "stage_v2.png" {
		t.Fatalf("remaining history=%q", remaining)
	}
	svc.db.First(&got, stage.ID)
	if got.Portrait != "stage_v3.png" || strings.Join(ageStagePortraitHistory(got.PortraitHistory), ",") != "stage_v2.png" {
		t.Fatalf("deleting history must not touch the current portrait: %+v", got)
	}
	if _, err := os.Stat(assetPath("stage_v1.png")); !os.IsNotExist(err) {
		t.Fatalf("the deleted history image must be removed from disk: %v", err)
	}
	var records int64
	svc.db.Model(&models.UploadFile{}).Where("task_id = ? AND name = ?", fmt.Sprint(project.ID), "stage_v1.png").Count(&records)
	if records != 0 {
		t.Fatalf("the upload record of the deleted history image must be removed: %d", records)
	}
	for _, keep := range []string{"stage_v2.png", "stage_v3.png"} {
		if _, err := os.Stat(assetPath(keep)); err != nil {
			t.Fatalf("%s must be kept: %v", keep, err)
		}
	}
	var ch models.Character
	svc.db.First(&ch, character.ID)
	if ch.Portrait != "face.png" || ch.Sheet != "adult_sheet.png" {
		t.Fatalf("deleting age-stage history must never touch the main portrait/sheet: %+v", ch)
	}

	if remaining, err = svc.DeleteAgeStagePortraitHistory(project.ID, character.ID, stage.ID, "stage_v2.png"); err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 0 {
		t.Fatalf("remaining history=%q", remaining)
	}
	svc.db.First(&got, stage.ID)
	if got.PortraitHistory != "" {
		t.Fatalf("an empty history must be stored as an empty string: %q", got.PortraitHistory)
	}
	if _, err := os.Stat(assetPath("stage_v2.png")); !os.IsNotExist(err) {
		t.Fatalf("the deleted history image must be removed from disk: %v", err)
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
	if err := svc.SaveAgeStagePortraitPrompt(project.ID, character.ID, stage.ID, "6岁女孩，齐刘海。由左到右排列四视图。"); err == nil {
		t.Fatal("a sheet prompt without the <image1> reference binding must be rejected")
	}
	prompt := "<image1>是角色主标准像，只参考脸部特征。6岁女孩，齐刘海。由左到右排列：正面面部特写、正面全身站立、侧面全身站立、背面全身站立。"
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
