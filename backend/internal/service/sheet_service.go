package service

import (
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"comfyui-console/internal/models"
)

var errSheetActive = errors.New("角色设定图正在生成")

func characterFootwear(project *models.Project, ch *models.Character) string {
	context := strings.Join([]string{project.Genre, project.Style, project.Synopsis, ch.Role, ch.Appearance, ch.WardrobeDetail}, "，")
	wardrobe := strings.TrimSpace(ch.WardrobeDetail)
	footwear := "与完整服装和时代背景严格一致的包头鞋履"
	for _, word := range []string{"鞋", "靴", "履", "木屐", "凉鞋", "高跟"} {
		if strings.Contains(wardrobe, word) {
			return wardrobe
		}
	}
	for _, keyword := range []string{"修仙", "仙侠", "古典", "古代", "国风", "武侠", "汉服", "宗门", "朝代", "中国古"} {
		if strings.Contains(context, keyword) {
			if strings.Contains(ch.Appearance+ch.Role, "女性") || strings.Contains(ch.Appearance+ch.Role, "女") {
				return "与服装主色一致的中式软底云头绣鞋（包头、平底、非露趾）"
			}
			return "与服装主色一致的中式云纹皂靴（包头、平底）"
		}
	}
	return footwear
}

func characterAccessoryConstraint(ch *models.Character) string {
	return "角色固定佩饰档案：" + strings.TrimSpace(ch.Appearance) + "；" + strings.TrimSpace(ch.WardrobeDetail) + "。" +
		"所有明确佩饰（发簪、步摇、发冠、耳环、耳坠、项链、吊坠、腰佩、腰带、手镯、腕饰、戒指）必须严格固定其形状、材质、颜色、纹样、佩戴位置、数量、左右侧和层叠关系；不得丢失、增减、换款、左右互换或在不同视图中漂移。尤其女性角色的发饰、耳饰、颈饰、腰饰与腕饰必须逐项保留。"
}

func buildCharacterAnchorPrompt(project *models.Project, ch *models.Character) string {
	return "Use the supplied face portrait as the immutable identity reference. Generate exactly one front-facing full-body standing portrait of the same person, preserving face, exact visual age, hairstyle and art style. " +
		"角色完整服装档案：" + strings.TrimSpace(ch.WardrobeDetail) + "。" + characterAccessoryConstraint(ch) + "指定鞋履：" + characterFootwear(project, ch) + "。" +
		"Show the complete outfit from head to toe. Both feet and both shoes must be fully visible inside the frame; shoes must match the era, culture, outfit colors, material and identity exactly. " +
		"时代与文化必须服从项目设定「" + project.Genre + " / " + project.Style + "」。纯白背景，中性站姿，单人单视图，禁止改变脸、年龄、发型、服装，禁止赤脚、裸足、露趾、裁脚、遮脚、现代高跟鞋、运动鞋、现代皮鞋、日式木屐、多人、拼图、文字和水印。"
}

func buildCharacterSheetPrompt(project *models.Project, ch *models.Character) string {
	wardrobe := strings.TrimSpace(ch.WardrobeDetail)
	footwear := characterFootwear(project, ch)
	return "The supplied reference canvas contains two authoritative images of the same character: the face portrait on the LEFT controls identity, exact face, visual age and hairstyle; the full-body clothing anchor on the RIGHT controls body proportions, complete outfit and footwear. Use BOTH references. " +
		"Generate one clean character sheet with exactly four panels: face close-up, front full body, side full body, back full body. " +
		"Preserve the same identity, exact visual age, face, hairstyle, body proportions, complete outfit, colors and art style. " +
		"角色完整服装档案：" + wardrobe + "。" + characterAccessoryConstraint(ch) + "指定鞋履：" + footwear + "。" +
		"正面全身、侧面全身、背面全身三个视图必须穿完全相同的鞋：鞋型、鞋底高度、颜色、材质、纹样完全一致；每个视图都要清楚画出左右两只鞋，双脚必须完整穿鞋并全部位于画框内。" +
		"不得根据姿态、省略、长裙遮挡或背面视角把鞋变成赤脚，也不得让三个视图出现不同鞋款。" +
		"时代与文化必须服从项目设定「" + project.Genre + " / " + project.Style + "」，严禁赤脚、裸足、露趾、现代高跟鞋、细高跟鞋、运动鞋、现代皮鞋、日式木屐、和风鞋履及跨时代跨文化鞋款。" +
		" Neutral white background, no text, no extra people, no cropped feet."
}

// StartCharacterSheet is the one-click entry point. It persists the final-sheet
// intent and creates the clothing anchor first when the character does not have one.
func (s *ProjectService) StartCharacterSheet(ch *models.Character) (string, error) {
	if strings.TrimSpace(ch.Portrait) == "" {
		return "", fmt.Errorf("请先为角色「%s」生成或上传标准像", ch.Name)
	}
	if s.tasks == nil {
		return "", fmt.Errorf("角色四视图生成依赖 ComfyUI 任务服务")
	}
	if strings.TrimSpace(ch.ClothingAnchor) == "" {
		if s.sheetTaskActive(ch.AnchorTaskID) {
			s.db.Model(ch).Update("sheet_after_anchor", true)
			return "", fmt.Errorf("%w：角色「%s」正在生成全身服装锚点", errSheetActive, ch.Name)
		}
		return "anchor", s.startCharacterAnchor(ch)
	}
	if s.sheetTaskActive(ch.SheetTaskID) {
		return "", fmt.Errorf("%w：角色「%s」正在生成四视图", errSheetActive, ch.Name)
	}
	return "sheet", s.startFinalCharacterSheet(ch)
}

func (s *ProjectService) startCharacterAnchor(ch *models.Character) error {
	var tpl models.Template
	if err := s.db.Where("code = ? AND enabled = ?", "krea2_character_clothing_anchor", true).First(&tpl).Error; err != nil {
		return fmt.Errorf("未找到已启用的 Krea2 角色全身服装锚点模板")
	}
	var project models.Project
	if err := s.db.First(&project, ch.ProjectID).Error; err != nil {
		return err
	}
	task, err := s.tasks.CreateTask(CreateTaskReq{
		TemplateID: tpl.ID,
		Prompt:     buildCharacterAnchorPrompt(&project, ch),
		Files:      map[string][]FileMeta{"source_image": {{TaskID: fmt.Sprint(ch.ProjectID), Name: ch.Portrait}}},
	})
	if err != nil {
		return fmt.Errorf("创建角色全身服装锚点任务失败: %w", err)
	}
	if err := s.db.Model(&models.Character{}).Where("id = ?", ch.ID).Updates(map[string]any{
		"anchor_task_id": task.TaskID, "anchor_error": "", "sheet_error": "", "sheet_after_anchor": true,
	}).Error; err != nil {
		_ = s.tasks.CancelTask(task.TaskID)
		return err
	}
	go func() {
		if err := s.tasks.Execute(task.TaskID); err != nil && !errors.Is(err, errNoFreeGPU) {
			log.Printf("[character %d] execute clothing anchor task %s failed: %v", ch.ID, task.TaskID, err)
		}
	}()
	s.pushProject(nil)
	return nil
}

func (s *ProjectService) startFinalCharacterSheet(ch *models.Character) error {
	if strings.TrimSpace(ch.Portrait) == "" || strings.TrimSpace(ch.ClothingAnchor) == "" {
		return fmt.Errorf("角色「%s」缺少标准像或全身服装锚点", ch.Name)
	}
	var tpl models.Template
	if err := s.db.Where("code = ? AND enabled = ?", "krea2_character_sheet", true).First(&tpl).Error; err != nil {
		return fmt.Errorf("未找到已启用的 Krea2 角色四视图模板")
	}
	var project models.Project
	if err := s.db.First(&project, ch.ProjectID).Error; err != nil {
		return err
	}
	task, err := s.tasks.CreateTask(CreateTaskReq{
		TemplateID: tpl.ID,
		Prompt:     buildCharacterSheetPrompt(&project, ch),
		Files: map[string][]FileMeta{
			"face_portrait":   {{TaskID: fmt.Sprint(ch.ProjectID), Name: ch.Portrait}},
			"clothing_anchor": {{TaskID: fmt.Sprint(ch.ProjectID), Name: ch.ClothingAnchor}},
		},
	})
	if err != nil {
		return fmt.Errorf("创建角色四视图任务失败: %w", err)
	}
	if err := s.db.Model(&models.Character{}).Where("id = ?", ch.ID).Updates(map[string]any{
		"sheet_task_id": task.TaskID, "sheet_error": "", "sheet_after_anchor": false,
	}).Error; err != nil {
		_ = s.tasks.CancelTask(task.TaskID)
		return err
	}
	go func() {
		if err := s.tasks.Execute(task.TaskID); err != nil && !errors.Is(err, errNoFreeGPU) {
			log.Printf("[character %d] execute sheet task %s failed: %v", ch.ID, task.TaskID, err)
		}
	}()
	s.pushProject(nil)
	return nil
}

// StartPropSheet submits a Krea2 image-edit task using the prop reference image.
func (s *ProjectService) StartPropSheet(a *models.Asset) error {
	if a.Kind != AssetKindProp {
		return fmt.Errorf("场景资产不需要四视图")
	}
	if strings.TrimSpace(a.Image) == "" {
		return fmt.Errorf("请先为道具「%s」生成或上传参考图", a.Name)
	}
	if s.tasks == nil {
		return fmt.Errorf("道具四视图生成依赖 ComfyUI 任务服务")
	}
	if s.sheetTaskActive(a.SheetTaskID) {
		return fmt.Errorf("%w：道具「%s」", errSheetActive, a.Name)
	}
	var tpl models.Template
	if err := s.db.Where("code = ? AND enabled = ?", "krea2_prop_sheet", true).First(&tpl).Error; err != nil {
		return fmt.Errorf("未找到已启用的 Krea2 道具四视图模板")
	}
	prompt := "Convert the exact same single prop into a clean four-view object sheet: front, left side, back, and right side. Preserve shape, proportions, material, colors, markings and art style exactly. Neutral white background, no people, no text, no duplicate objects outside the four views."
	task, err := s.tasks.CreateTask(CreateTaskReq{
		TemplateID: tpl.ID,
		Prompt:     prompt,
		Files:      map[string][]FileMeta{"source_image": {{TaskID: fmt.Sprint(a.ProjectID), Name: a.Image}}},
	})
	if err != nil {
		return fmt.Errorf("创建道具四视图任务失败: %w", err)
	}
	if err := s.db.Model(&models.Asset{}).Where("id = ? AND kind = ?", a.ID, AssetKindProp).Updates(map[string]any{
		"sheet_task_id": task.TaskID, "sheet_error": "",
	}).Error; err != nil {
		_ = s.tasks.CancelTask(task.TaskID)
		return err
	}
	go func() {
		if err := s.tasks.Execute(task.TaskID); err != nil && !errors.Is(err, errNoFreeGPU) {
			log.Printf("[asset %d] execute sheet task %s failed: %v", a.ID, task.TaskID, err)
		}
	}()
	s.pushProject(nil)
	return nil
}

func (s *ProjectService) sheetTaskActive(taskID string) bool {
	if taskID == "" {
		return false
	}
	var active int64
	return s.db.Model(&models.Task{}).Where("task_id = ? AND status IN ?", taskID, []string{"pending", "queued", "running"}).Count(&active).Error == nil && active > 0
}

func (s *ProjectService) syncSheets() {
	changed := false
	var anchoring []models.Character
	if s.db.Where("anchor_task_id != ''").Find(&anchoring).Error == nil {
		for i := range anchoring {
			ch := &anchoring[i]
			updates, ok := s.generatedImageTaskUpdates(ch.AnchorTaskID, fmt.Sprintf("char_anchor_%d_%d", ch.ID, time.Now().UnixNano()), ch.ProjectID,
				"clothing_anchor", "anchor_task_id", "anchor_error", "全身服装锚点")
			if ok {
				if errText, failed := updates["anchor_error"].(string); failed && errText != "" {
					updates["sheet_after_anchor"] = false
				}
				res := s.db.Model(ch).Where("anchor_task_id = ?", ch.AnchorTaskID).Updates(updates)
				changed = changed || res.RowsAffected > 0
			}
		}
	}
	var chars []models.Character
	if s.db.Where("sheet_task_id != ''").Find(&chars).Error == nil {
		for i := range chars {
			ch := &chars[i]
			updates, ok := s.generatedImageTaskUpdates(ch.SheetTaskID, fmt.Sprintf("char_sheet_%d_%d", ch.ID, time.Now().UnixNano()), ch.ProjectID,
				"sheet", "sheet_task_id", "sheet_error", "四视图")
			if ok {
				res := s.db.Model(ch).Where("sheet_task_id = ?", ch.SheetTaskID).Updates(updates)
				changed = changed || res.RowsAffected > 0
			}
		}
	}
	var assets []models.Asset
	if s.db.Where("kind = ? AND sheet_task_id != ''", AssetKindProp).Find(&assets).Error == nil {
		for i := range assets {
			a := &assets[i]
			updates, ok := s.generatedImageTaskUpdates(a.SheetTaskID, fmt.Sprintf("prop_sheet_%d_%d", a.ID, time.Now().UnixNano()), a.ProjectID,
				"sheet", "sheet_task_id", "sheet_error", "四视图")
			if ok {
				res := s.db.Model(a).Where("sheet_task_id = ?", a.SheetTaskID).Updates(updates)
				changed = changed || res.RowsAffected > 0
			}
		}
	}
	var pending []models.Character
	if s.db.Where("sheet_after_anchor = ? AND clothing_anchor != '' AND anchor_task_id = '' AND sheet_task_id = ''", true).Find(&pending).Error == nil {
		for i := range pending {
			if err := s.startFinalCharacterSheet(&pending[i]); err != nil {
				s.db.Model(&pending[i]).Update("sheet_error", "自动续跑四视图失败: "+err.Error())
				changed = true
			}
		}
	}
	if changed {
		s.pushProject(nil)
	}
}

func (s *ProjectService) generatedImageTaskUpdates(taskID, baseName string, projectID uint, outputField, taskField, errorField, label string) (map[string]any, bool) {
	failure := func(message string) (map[string]any, bool) {
		return map[string]any{taskField: "", errorField: message}, true
	}
	var task models.Task
	if err := s.db.Where("task_id = ?", taskID).First(&task).Error; err != nil {
		return failure(label + "任务不存在，请重新生成")
	}
	switch task.Status {
	case "failed", "cancelled":
		msg := strings.TrimSpace(task.Error)
		if msg == "" {
			msg = label + "任务已" + map[string]string{"failed": "失败", "cancelled": "取消"}[task.Status]
		}
		return failure(msg)
	case "success":
		file, _ := resultImageOf(&task)
		if file == "" || task.Port == nil || s.upload == nil || s.tasks == nil {
			return failure(label + "任务成功但未返回可用图片")
		}
		subfolder, filename := filepath.ToSlash(filepath.Dir(file)), filepath.Base(file)
		if subfolder == "." {
			subfolder = ""
		}
		data, err := NewComfyClient(s.tasks.comfyHostForPort(*task.Port), *task.Port).DownloadOutput(filename, subfolder, "output")
		if err != nil {
			return failure("读取" + label + "失败: " + err.Error())
		}
		ext := filepath.Ext(file)
		if ext == "" {
			ext = detectImageExt(data)
		}
		path, _, err := s.upload.SaveFile(fmt.Sprint(projectID), "image", baseName+ext, data)
		if err != nil {
			return failure("保存" + label + "失败: " + err.Error())
		}
		return map[string]any{outputField: filepath.Base(path), taskField: "", errorField: ""}, true
	}
	return nil, false
}
