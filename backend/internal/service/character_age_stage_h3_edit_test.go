package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

// 新模板按官方 MiniMax H3-Image 多图编辑图裁剪：去掉提示词扩写器（会把用户审阅过的中文稿改写成英文），
// 出图尺寸改为固定宽高参数而不是跟随参考图，其余保持 UNet/CLIP/VAE → 条件化（含有序参考图）→ 零化负向 → 6 步采样。
func TestH3ImageEditTemplateRendersOrderedReferencesAndFixedCanvas(t *testing.T) {
	tpl := loadTemplateForTest(t, "minimax_h3_image_edit.json")
	params := map[string]any{"prompt": "把主标准像改成童年四视图", "width": ageStageSheetWidth, "height": ageStageSheetHeight, "_task_id": "edit-task"}
	files := map[string][]FileMeta{"ref_images": {{TaskID: "1", Name: "face.png"}}}
	if err := normalizeTemplateFiles(&tpl, params, files); err != nil {
		t.Fatal(err)
	}
	workflow, err := (&TaskService{}).RenderWorkflow(&tpl, params)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(workflow)
	if strings.Contains(string(raw), "{{") {
		t.Fatalf("unrendered placeholder: %s", raw)
	}
	inputs := func(id string) map[string]any { return workflow[id].(map[string]any)["inputs"].(map[string]any) }
	cond := inputs("10")
	if workflow["10"].(map[string]any)["class_type"] != "MiniMaxH3ImageConditioning" || fmt.Sprint(cond["width"]) != "2048" || fmt.Sprint(cond["height"]) != "1152" {
		t.Fatalf("conditioning=%v", workflow["10"])
	}
	if ref, _ := cond["images.image_1"].([]any); len(ref) != 2 || fmt.Sprint(ref[0]) != "4" {
		t.Fatalf("first reference must feed images.image_1: %v", cond)
	}
	// 只给一张参考图时，另外两个 LoadImage 节点连同指向它们的输入一并移除。
	if _, ok := workflow["5"]; ok {
		t.Fatalf("unused reference node kept: %v", workflow["5"])
	}
	if _, ok := cond["images.image_2"]; ok {
		t.Fatalf("dangling images.image_2: %v", cond)
	}
	if inputs("4")["image"] != "1/face.png" {
		t.Fatalf("reference not rendered: %v", inputs("4"))
	}
	if fmt.Sprint(inputs("12")["steps"]) != "6" || fmt.Sprint(inputs("12")["cfg"]) != "1" || inputs("12")["sampler_name"] != "euler" {
		t.Fatalf("sampler=%v", inputs("12"))
	}
	if inputs("14")["filename_prefix"] != "minimax_h3_image/edit_edit-task" {
		t.Fatalf("save=%v", inputs("14"))
	}

	// 三张参考图时三个 LoadImage 都在，并按顺序接到 images.image_1..3。
	params = map[string]any{"prompt": "p", "width": ageStageSheetWidth, "height": ageStageSheetHeight, "_task_id": "edit-task"}
	files = map[string][]FileMeta{"ref_images": {{TaskID: "1", Name: "a.png"}, {TaskID: "1", Name: "b.png"}, {TaskID: "1", Name: "c.png"}}}
	if err := normalizeTemplateFiles(&tpl, params, files); err != nil {
		t.Fatal(err)
	}
	workflow, err = (&TaskService{}).RenderWorkflow(&tpl, params)
	if err != nil {
		t.Fatal(err)
	}
	cond = workflow["10"].(map[string]any)["inputs"].(map[string]any)
	for _, key := range []string{"images.image_1", "images.image_2", "images.image_3"} {
		if _, ok := cond[key]; !ok {
			t.Fatalf("three references must fill %s: %v", key, cond)
		}
	}
	// 模板不带提示词扩写器：工作流里不应出现文本生成节点与第二个 CLIP。
	if _, ok := workflow["qwen_text"]; ok || strings.Contains(string(raw), "TextGenerateV2") {
		t.Fatal("prompt expander must not be part of this template")
	}
}

func TestNormalizeAgeStagePortraitEngineKeepsQwenDefaultAndAcceptsH3Edit(t *testing.T) {
	for _, legacy := range []string{"", AgeStagePortraitQwenEdit, ImageEngineQwen21, ImageEngineMoss, ImageEngineKrea2} {
		if got, err := normalizeAgeStagePortraitEngine(legacy); err != nil || got != AgeStagePortraitQwenEdit {
			t.Fatalf("%q must fall back to qwen_edit, got %q %v", legacy, got, err)
		}
	}
	if got, err := normalizeAgeStagePortraitEngine(AgeStagePortraitH3Edit); err != nil || got != AgeStagePortraitH3Edit {
		t.Fatalf("h3 edit must be accepted, got %q %v", got, err)
	}
	if _, err := normalizeAgeStagePortraitEngine("nano_banana"); err == nil {
		t.Fatal("unknown engine accepted")
	}
	// 绑定写法随引擎不同：H3-Image 用 <Picture 1>，Qwen 用 <image1>。
	if err := checkAgeStageReferenceBindingFor("<Picture 1>是角色「A」的主标准像", AgeStagePortraitH3Edit); err != nil {
		t.Fatal(err)
	}
	if err := checkAgeStageReferenceBindingFor("<image1>是角色「A」的主标准像", AgeStagePortraitH3Edit); err == nil || !strings.Contains(err.Error(), "<Picture 1>") {
		t.Fatalf("a Qwen prompt must be refused for the H3 engine, got %v", err)
	}
	if err := checkAgeStageReferenceBindingFor("<Picture 1>是角色「A」的主标准像", AgeStagePortraitQwenEdit); err == nil {
		t.Fatal("an H3 prompt must be refused for the Qwen engine")
	}
}

func TestAgeStageH3EditEngineBindsPictureOneAndSubmitsEditTemplate(t *testing.T) {
	svc, project, character := setupAgeStageTestDB(t)
	attachAgeStageTaskService(t, svc, "minimax_h3_image_edit.json")
	registerAgeStageUploads(t, svc, project.ID, "face.png")
	stage, err := svc.SaveAgeStage(project.ID, character.ID, 0, AgeStageInput{Name: "童年", Age: intPtr(6), Appearance: "6岁女孩，圆脸，齐刘海短发", PortraitEngine: AgeStagePortraitH3Edit})
	if err != nil {
		t.Fatal(err)
	}
	if stage.PortraitEngine != AgeStagePortraitH3Edit {
		t.Fatalf("engine not saved: %q", stage.PortraitEngine)
	}
	saved, draft, err := svc.AgeStagePortraitPrompt(project.ID, character.ID, stage.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved != "" || !strings.HasPrefix(draft, "<Picture 1>是角色「林采微」的主标准像") || strings.Contains(draft, "<image1>") {
		t.Fatalf("H3 draft must bind <Picture 1> only: saved=%q draft=%q", saved, draft)
	}
	if !strings.Contains(draft, "由左到右排列") || !strings.Contains(draft, "6岁女孩，圆脸，齐刘海短发") {
		t.Fatalf("four-view layout or stage appearance missing:\n%s", draft)
	}
	if err := svc.StartAgeStagePortrait(project.ID, character.ID, stage.ID, ""); err != nil {
		t.Fatal(err)
	}
	task, files := onlyTaskInputs(t, svc)
	if task.Prompt != draft {
		t.Fatalf("submitted prompt differs from the reviewed draft")
	}
	if refs := files["ref_images"]; len(files) != 1 || len(refs) != 1 || refs[0].Name != "face.png" {
		t.Fatalf("stage sheet must use only the main portrait as reference, got %+v", files)
	}
	var tpl models.Template
	if err := svc.db.First(&tpl, task.TemplateID).Error; err != nil || tpl.Code != TemplateH3ImageEdit {
		t.Fatalf("H3 stage sheet must use %s, got %q %v", TemplateH3ImageEdit, tpl.Code, err)
	}
	var params map[string]any
	if err := json.Unmarshal([]byte(task.ParamsJSON), &params); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(params["width"]) != "2048" || fmt.Sprint(params["height"]) != "1152" {
		t.Fatalf("stage sheet must ask for a fixed 16:9 canvas, got %+v", params)
	}
	if _, ok := params["use_aspect_ratio"]; ok {
		t.Fatalf("H3 template takes width/height, not Qwen aspect switches: %+v", params)
	}
	var saved2 models.CharacterAgeStage
	if err := svc.db.First(&saved2, stage.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved2.PortraitTaskID != task.TaskID || saved2.PortraitPrompt != draft {
		t.Fatalf("stage did not record task and prompt: %+v", saved2)
	}
}

// 换引擎后旧稿（另一个绑定写法）按未保存处理；直接把 Qwen 稿提交给 H3 引擎会被拒绝。
func TestAgeStageSavedPromptFollowsSelectedEngine(t *testing.T) {
	svc, project, character := setupAgeStageTestDB(t)
	attachAgeStageTaskService(t, svc, "minimax_h3_image_edit.json")
	registerAgeStageUploads(t, svc, project.ID, "face.png")
	stage, err := svc.SaveAgeStage(project.ID, character.ID, 0, AgeStageInput{Name: "童年", Age: intPtr(6), Appearance: "6岁女孩", PortraitEngine: AgeStagePortraitH3Edit})
	if err != nil {
		t.Fatal(err)
	}
	qwenPrompt := "<image1>是角色「林采微」的主标准像，仅用于保持同一人物的脸部特征与项目画风。" + ageStageSheetBody(&project, &character, stage)
	if err := svc.db.Model(stage).Update("portrait_prompt", qwenPrompt).Error; err != nil {
		t.Fatal(err)
	}
	if saved, _, err := svc.AgeStagePortraitPrompt(project.ID, character.ID, stage.ID); err != nil || saved != "" {
		t.Fatalf("a prompt written for the other engine must not be offered as saved: %q %v", saved, err)
	}
	if err := svc.StartAgeStagePortrait(project.ID, character.ID, stage.ID, qwenPrompt); err == nil || !strings.Contains(err.Error(), "<Picture 1>") {
		t.Fatalf("submitting a Qwen prompt to the H3 engine must be refused, got %v", err)
	}
	if err := svc.StartAgeStagePortrait(project.ID, character.ID, stage.ID, ""); err != nil {
		t.Fatal(err)
	}
	task, _ := onlyTaskInputs(t, svc)
	if !strings.HasPrefix(task.Prompt, "<Picture 1>") {
		t.Fatalf("empty prompt must fall back to the H3 draft: %s", task.Prompt)
	}
}