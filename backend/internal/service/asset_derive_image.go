package service

import (
	"errors"
	"fmt"
	"log"
	"strings"

	"comfyui-console/internal/models"
)

var errAssetDeriveBaseNotReady = errors.New("派生来源还没有参考图")

// startDerivedAssetImageLocked 派生资产只用 Qwen-Image-2.1 编辑：以来源资产当前参考图为 <image1>，
// 按记录的变化改出新图。不退回文生图，也不换用其他引擎；调用方须持有 assetMu。
func (s *ProjectService) startDerivedAssetImageLocked(p *models.Project, current *models.Asset) error {
	var base models.Asset
	if err := s.db.Where("id = ? AND project_id = ? AND kind = ?", current.DerivedFromID, current.ProjectID, current.Kind).First(&base).Error; err != nil {
		return fmt.Errorf("%s「%s」的派生来源已删除，无法基于原图编辑；请在编辑中更换派生来源或取消派生", AssetKindLabel(current.Kind), current.Name)
	}
	if strings.TrimSpace(base.Image) == "" {
		return fmt.Errorf("%w：请先给%s「%s」生成或上传参考图", errAssetDeriveBaseNotReady, AssetKindLabel(base.Kind), base.Name)
	}
	changes := ParseAssetDesignDirections(current.DeriveChanges)
	if len(changes) == 0 && strings.TrimSpace(current.Description) == "" {
		return fmt.Errorf("%s「%s」还没有确认相对来源的变化，请先在编辑弹窗里用 AI 设计并保存", AssetKindLabel(current.Kind), current.Name)
	}
	tpl, err := enabledTemplateByCode(s.db, TemplateQwen21MultiEdit)
	if err != nil {
		return err
	}
	projectDir := fmt.Sprint(current.ProjectID)
	refs := []FileMeta{{TaskID: projectDir, Name: base.Image}}
	hasSheet := current.Kind == AssetKindProp && strings.TrimSpace(base.Sheet) != ""
	if hasSheet {
		refs = append(refs, FileMeta{TaskID: projectDir, Name: base.Sheet})
	}
	params := qwenPortraitParams()
	params["aspect_ratio"] = qwenAspectRatio(p.AspectRatio)
	if current.Kind == AssetKindProp {
		params["aspect_ratio"] = qwenAspectRatio("1:1")
	}
	task, err := s.tasks.CreateTask(CreateTaskReq{TemplateID: tpl.ID, Prompt: buildDerivedAssetEditPrompt(p, &base, current, changes, hasSheet), Params: params,
		Files: map[string][]FileMeta{"ref_images": refs}})
	if err != nil {
		return fmt.Errorf("创建派生参考图任务失败: %w", err)
	}
	if err := s.db.Model(&models.Asset{}).Where("id = ?", current.ID).Updates(map[string]any{
		"image_task_id": task.TaskID, "image_error": "",
	}).Error; err != nil {
		_ = s.tasks.CancelTask(task.TaskID)
		return err
	}
	go func() {
		if err := s.tasks.Execute(task.TaskID); err != nil && !errors.Is(err, errNoFreeGPU) {
			log.Printf("[asset %d] execute derived Qwen edit task %s failed: %v", current.ID, task.TaskID, err)
		}
	}()
	s.pushProject(nil)
	return nil
}

// buildDerivedAssetEditPrompt 编辑指令 = 保持来源图 + 逐条变化 + 新设定全文作约束。
func buildDerivedAssetEditPrompt(p *models.Project, base, current *models.Asset, changes []string, hasSheet bool) string {
	label := AssetKindLabel(current.Kind)
	var b strings.Builder
	fmt.Fprintf(&b, "<image1>是%s「%s」的参考图。以<image1>为基础编辑出%s「%s」：保持<image1>的空间结构、视角构图、主体造型、材质与画风不变，只做下列变化。\n",
		label, strings.TrimSpace(base.Name), label, strings.TrimSpace(current.Name))
	if hasSheet {
		b.WriteString("<image2>是同一道具的四视图，仅用于辨认物件结构；输出单一物件特写，不要输出四视图或拼图布局。\n")
	}
	if len(changes) > 0 {
		b.WriteString("变化：\n")
		for i, change := range changes {
			fmt.Fprintf(&b, "%d. %s\n", i+1, change)
		}
	} else {
		b.WriteString("变化：按下面的新设定调整，其余保持<image1>原样。\n")
	}
	if d := strings.TrimSpace(current.Description); d != "" {
		b.WriteString("新设定（约束）：" + d + "\n")
	}
	if style := strings.TrimSpace(p.Style); style != "" {
		b.WriteString("画风保持项目画风「" + style + "」。\n")
	}
	if current.Kind == AssetKindProp {
		b.WriteString("这是无生命道具的产品参考图，画面只有一个物件，人物数量必须为零，禁止脸、人体、手、拟人化、文字、水印和拼图。")
	} else {
		ending := "这是无人物的环境场景空镜，人物数量必须为零，禁止人物、水印和拼图"
		if text := strings.TrimSpace(current.VisibleText); text != "" {
			ending += "；画面中必须逐字清晰呈现可读文字：\"" + strings.ReplaceAll(text, "\"", "\\\"") + "\"，不得改写或新增其他文字"
		} else {
			ending += "，禁止文字"
		}
		b.WriteString(ending + "。")
	}
	return b.String()
}
