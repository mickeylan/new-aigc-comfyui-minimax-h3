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

var errSheetActive = errors.New("四视图正在生成")

func characterFootwear(project *models.Project, ch *models.Character) string {
	context := strings.Join([]string{project.Genre, project.Style, project.Synopsis, ch.Role, ch.Appearance, ch.WardrobeDetail}, "，")
	for _, word := range []string{"鞋", "靴", "履", "木屐", "凉鞋", "高跟"} {
		if strings.Contains(ch.WardrobeDetail, word) {
			return strings.TrimSpace(ch.WardrobeDetail)
		}
	}
	for _, keyword := range []string{"修仙", "仙侠", "古典", "古代", "国风", "武侠", "汉服", "宗门", "朝代", "中国古"} {
		if strings.Contains(context, keyword) {
			if strings.Contains(ch.Appearance+ch.Role, "女") {
				return "与服装主色一致的中式软底云头绣鞋（包头、平底、非露趾）"
			}
			return "与服装主色一致的中式云纹皂靴（包头、平底）"
		}
	}
	return "与完整服装和时代背景一致的包头鞋履"
}

// characterSheetFootwearLock 锁定三个全身视图的鞋履，防止赤脚、裁脚和现代鞋。
func characterSheetFootwearLock(project *models.Project, ch *models.Character) string {
	return "指定鞋履：" + characterFootwear(project, ch) + "。" +
		"正面全身、侧面全身、背面全身三个视图必须穿完全相同的鞋，并使用同一套服装；双脚必须完整穿鞋并位于画框内。禁止赤脚、露趾、裁脚、换装、丢失发饰和佩饰，禁止现代高跟鞋、运动鞋、现代皮鞋和日式木屐。"
}

// characterSheetHasHair 判断档案外貌是否已写明发型，已写明时不再重复注入时代发型约束。
func characterSheetHasHair(text string) bool {
	for _, word := range []string{"头发", "发型", "发丝", "发髻", "长发", "短发", "披发", "束发", "髻", "冠", "簪"} {
		if strings.Contains(text, word) {
			return true
		}
	}
	return false
}

// characterSheetDescription 按档案确定性编译四视图的人物描述段（不含版式），
// 用户未编辑四视图提示词或没有文本模型时使用。
func characterSheetDescription(project *models.Project, ch *models.Character) string {
	parts := make([]string, 0, 8)
	if subject := ageSubject(ch.Appearance, ch.Trait); subject != "" {
		parts = append(parts, subject)
	}
	appearance := strings.TrimSpace(ch.Appearance)
	if appearance == "" {
		appearance = strings.TrimSpace(ch.Trait)
	}
	if appearance != "" {
		parts = append(parts, appearance)
	}
	if !characterSheetHasHair(appearance) {
		if hair := historicalPortraitHairConstraint(ch, project); hair != "" {
			parts = append(parts, hair)
		}
	}
	if wardrobe := primaryWardrobe(ch); wardrobe != "" {
		parts = append(parts, "服饰："+wardrobe)
	}
	if palette := strings.TrimSpace(ch.ColorPalette); palette != "" {
		parts = append(parts, "主色调："+palette)
	}
	if style := strings.TrimSpace(project.Style); style != "" {
		parts = append(parts, style+"风格")
	}
	return normalizePortraitStyle(strings.Join(parts, "，"), project)
}

// characterSheetLayoutTail 固定追加的鞋履锁定与四格版式，保证用户改写人物描述后版式不丢。
func characterSheetLayoutTail(project *models.Project, ch *models.Character) string {
	return characterSheetFootwearLock(project, ch) +
		"四格为同一人物、同一张脸、同一发型与同一套服装配饰，全身格从头顶到鞋底完整入镜。" +
		"由左到右排列：正面面部特写、正面全身站立、侧面全身站立、背面全身站立。纯白色干净背景，影棚级柔和布光，角色设定集风格，画面中只有这一个角色且无文字标签，高分辨率，电影级质感，16:9画面比例。"
}

// buildCharacterSheetPrompt 直接文生四视图。结构与用户验证过的 Moss 样例一致：先写人物外貌、
// 完整服装与画风，再写“由左到右排列”的四格版式与设定集画质要求。不经过标准像，
// 避免大头照缺失的下半身、腰饰被编辑模型臆造。用户编辑过的四视图提示词优先；
// 其中已自带“由左到右”版式时原样提交，不再重复追加。
func buildCharacterSheetPrompt(project *models.Project, ch *models.Character) string {
	if custom := strings.TrimSpace(ch.SheetPrompt); custom != "" {
		if strings.Contains(custom, "由左到右") {
			return custom
		}
		return strings.TrimRight(custom, "。，,. ") + "。" + characterSheetLayoutTail(project, ch)
	}
	return characterSheetDescription(project, ch) + "。" + characterSheetLayoutTail(project, ch)
}

// buildCharacterSheetEditPrompt 保留“基于标准像”的编辑路径，供已定脸的角色沿用。
func buildCharacterSheetEditPrompt(project *models.Project, ch *models.Character) string {
	return "Convert the exact same single character from the supplied face portrait into a clean four-view character sheet: face close-up, front full body, side full body, and back full body. Preserve identity, exact visual age, face, hairstyle, body proportions, complete outfit, colors, accessories and art style. " +
		"角色完整服装档案：" + strings.TrimSpace(ch.WardrobeDetail) + "。" + characterSheetFootwearLock(project, ch) + "Neutral white background, no text, no extra people."
}

// characterSheetTemplate 返回四视图直接文生所用模板与参数：Moss 使用专用四视图模板默认值，
// Krea2/Qwen 复用各自文生图模板并改为横幅。
func characterSheetTemplate(engine string) (string, map[string]any) {
	switch engine {
	case ImageEngineQwen21:
		params := qwenPortraitParams()
		params["aspect_ratio"] = qwenAspectRatio("16:9")
		return TemplateQwen21T2I, params
	case ImageEngineKrea2:
		// Krea2 模板第二阶段放大 1.5 倍，实际输出约 2016x1152。
		return "krea2_character_portrait", map[string]any{"width": 1344, "height": 768}
	default:
		return TemplateMossSheet, nil
	}
}

// StartCharacterSheet 默认按角色四视图引擎（默认 Moss）直接文生四视图；fromPortrait 时沿用 Krea2 标准像编辑路径。
func (s *ProjectService) StartCharacterSheet(ch *models.Character, fromPortrait bool) error {
	if fromPortrait && strings.TrimSpace(ch.Portrait) == "" {
		return fmt.Errorf("请先为角色「%s」生成或上传标准像", ch.Name)
	}
	if !fromPortrait && ch.ProfileStatus != models.ProfileStatusApproved {
		return fmt.Errorf("请先审核通过角色「%s」的详细档案", ch.Name)
	}
	if s.tasks == nil {
		return fmt.Errorf("角色四视图生成依赖 ComfyUI 任务服务")
	}
	if s.sheetTaskActive(ch.SheetTaskID) {
		return fmt.Errorf("%w：角色「%s」", errSheetActive, ch.Name)
	}
	var project models.Project
	if err := s.db.First(&project, ch.ProjectID).Error; err != nil {
		return err
	}
	req := CreateTaskReq{}
	if fromPortrait {
		tpl, err := enabledTemplateByCode(s.db, "krea2_character_sheet")
		if err != nil {
			return err
		}
		req = CreateTaskReq{TemplateID: tpl.ID, Prompt: buildCharacterSheetEditPrompt(&project, ch),
			Files: map[string][]FileMeta{"source_image": {{TaskID: fmt.Sprint(ch.ProjectID), Name: ch.Portrait}}}}
	} else {
		engine, err := normalizeSheetImageEngine(ch.SheetEngine)
		if err != nil {
			return err
		}
		templateCode, params := characterSheetTemplate(engine)
		tpl, err := enabledTemplateByCode(s.db, templateCode)
		if err != nil {
			return err
		}
		req = CreateTaskReq{TemplateID: tpl.ID, Prompt: buildCharacterSheetPrompt(&project, ch), Params: params}
	}
	task, err := s.tasks.CreateTask(req)
	if err != nil {
		return fmt.Errorf("创建角色四视图任务失败: %w", err)
	}
	if err := s.db.Model(&models.Character{}).Where("id = ?", ch.ID).Updates(map[string]any{
		"sheet_task_id": task.TaskID, "sheet_error": "",
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
	var chars []models.Character
	if s.db.Where("sheet_task_id != ''").Find(&chars).Error == nil {
		for i := range chars {
			ch := &chars[i]
			updates, ok := s.sheetTaskUpdates(ch.SheetTaskID, fmt.Sprintf("char_sheet_%d_%d", ch.ID, time.Now().UnixNano()), ch.ProjectID)
			if ok {
				taskID := ch.SheetTaskID
				if file, _ := updates["sheet"].(string); file != "" {
					var task models.Task
					_ = s.db.Where("task_id = ?", taskID).First(&task).Error
					_, applied, err := updateSelectedAsset(s.db, ch.ProjectID, VariantCharacterSheet, ch.ID, file, task.Prompt, "krea2_task:"+taskID, updates, "sheet_task_id", taskID)
					changed = changed || (err == nil && applied)
				} else {
					res := s.db.Model(ch).Where("sheet_task_id = ?", taskID).Updates(updates)
					changed = changed || res.RowsAffected > 0
				}
			}
		}
	}
	var assets []models.Asset
	if s.db.Where("kind = ? AND sheet_task_id != ''", AssetKindProp).Find(&assets).Error == nil {
		for i := range assets {
			a := &assets[i]
			updates, ok := s.sheetTaskUpdates(a.SheetTaskID, fmt.Sprintf("prop_sheet_%d_%d", a.ID, time.Now().UnixNano()), a.ProjectID)
			if ok {
				taskID := a.SheetTaskID
				if file, _ := updates["sheet"].(string); file != "" {
					var task models.Task
					_ = s.db.Where("task_id = ?", taskID).First(&task).Error
					_, applied, err := updateSelectedAsset(s.db, a.ProjectID, VariantAssetSheet, a.ID, file, task.Prompt, "krea2_task:"+taskID, updates, "sheet_task_id", taskID)
					changed = changed || (err == nil && applied)
				} else {
					res := s.db.Model(a).Where("sheet_task_id = ?", taskID).Updates(updates)
					changed = changed || res.RowsAffected > 0
				}
			}
		}
	}
	if changed {
		s.pushProject(nil)
	}
}

func (s *ProjectService) sheetTaskUpdates(taskID, baseName string, projectID uint) (map[string]any, bool) {
	var task models.Task
	if err := s.db.Where("task_id = ?", taskID).First(&task).Error; err != nil {
		return map[string]any{"sheet_task_id": "", "sheet_error": "四视图任务不存在，请重新生成"}, true
	}
	switch task.Status {
	case "failed", "cancelled":
		msg := strings.TrimSpace(task.Error)
		if msg == "" {
			msg = "四视图任务已" + map[string]string{"failed": "失败", "cancelled": "取消"}[task.Status]
		}
		return map[string]any{"sheet_task_id": "", "sheet_error": msg}, true
	case "success":
		file, _ := resultImageOf(&task)
		if file == "" || task.Port == nil || s.upload == nil || s.tasks == nil {
			return map[string]any{"sheet_task_id": "", "sheet_error": "四视图任务成功但未返回可用图片"}, true
		}
		subfolder, filename := filepath.ToSlash(filepath.Dir(file)), filepath.Base(file)
		if subfolder == "." {
			subfolder = ""
		}
		data, err := NewComfyClient(s.tasks.comfyHostForTask(&task), *task.Port).DownloadOutput(filename, subfolder, "output")
		if err != nil {
			return map[string]any{"sheet_task_id": "", "sheet_error": "读取四视图失败: " + err.Error()}, true
		}
		ext := filepath.Ext(file)
		if ext == "" {
			ext = detectImageExt(data)
		}
		path, _, err := s.upload.SaveFile(fmt.Sprint(projectID), "image", baseName+ext, data)
		if err != nil {
			return map[string]any{"sheet_task_id": "", "sheet_error": "保存四视图失败: " + err.Error()}, true
		}
		return map[string]any{"sheet": filepath.Base(path), "sheet_task_id": "", "sheet_error": ""}, true
	}
	return nil, false
}
