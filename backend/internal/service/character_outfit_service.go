package service

import (
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

type OutfitInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	IsDefault   bool   `json:"is_default"`
	LookIDs     []uint `json:"look_ids"`
}

type OutfitAssignment struct {
	CharacterID uint `json:"character_id"`
	OutfitID    uint `json:"outfit_id"`
}

type OutfitDesignInput struct {
	Concept   string `json:"concept"`
	Name      string `json:"name"`
	IsDefault bool   `json:"is_default"`
}

type outfitDesignAsset struct {
	Name        string `json:"name"`
	Category    string `json:"category"`
	Description string `json:"description"`
}

type outfitDesignResult struct {
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Assets      []outfitDesignAsset `json:"assets"`
}

// DesignOutfit 把一个完整形象概念拆成独立、可审核的造型资产，并组合为套装草稿。
func (s *CharacterLookService) DesignOutfit(projectID, characterID uint, req OutfitDesignInput) (*models.CharacterOutfit, error) {
	concept := strings.TrimSpace(req.Concept)
	if concept == "" {
		return nil, fmt.Errorf("请描述想设计的新形象")
	}
	if s.textProvider == nil {
		return nil, fmt.Errorf("文本生成服务未配置")
	}
	var project models.Project
	var character models.Character
	if err := s.db.First(&project, projectID).Error; err != nil {
		return nil, err
	}
	if err := s.db.Where("id = ? AND project_id = ?", characterID, projectID).First(&character).Error; err != nil {
		return nil, err
	}
	if strings.TrimSpace(character.Portrait) == "" {
		return nil, fmt.Errorf("请先为角色「%s」生成或上传标准像；新形象必须绑定标准像设计", character.Name)
	}
	system := `你是影视角色造型设计师。根据用户的完整形象概念，设计一套统一的新造型，并拆成独立资产。
只输出严格JSON：{"name":"套装名","description":"整套形象说明","assets":[{"name":"资产名","category":"clothing|shoes|hair|hair_accessory|jewelry|bag","description":"材质、颜色、款式、结构细节"}]}。
必须包含clothing和shoes。hair仅在用户明确要求改变发型时加入；用户未提发型时必须沿用角色标准像发型，不得自行设计新发型。hair_accessory、jewelry、bag仅在用户明确要求时加入。每类最多一个。资产描述只写该单件资产，不写人物姓名、脸、身体、姿势或构图。不得加入武器、法宝、剧情道具，不得混入多个互斥方案。`
	user := fmt.Sprintf("项目题材：%s\n项目画风：%s\n角色：%s（仅用于理解身份，不得写入资产描述）\n用户命名：%s\n新形象概念：%s", project.Genre, project.Style, character.Name, strings.TrimSpace(req.Name), concept)
	raw, err := s.textProvider.Chat(system, user)
	if err != nil {
		return nil, fmt.Errorf("AI设计新形象失败: %w", err)
	}
	var design outfitDesignResult
	if err := parseJSONObject(raw, &design); err != nil {
		return nil, fmt.Errorf("AI新形象结果解析失败: %w", err)
	}
	if strings.TrimSpace(req.Name) != "" {
		design.Name = strings.TrimSpace(req.Name)
	}
	design.Name, design.Description = strings.TrimSpace(design.Name), strings.TrimSpace(design.Description)
	// 用户原始概念是权威约束，必须随套装保存；不能让 LLM 摘要丢掉“不要手套/不要包”等要求。
	design.Description = strings.TrimSpace(design.Description + "\n用户原始造型要求：" + concept)
	if design.Name == "" || len(design.Assets) == 0 || len(design.Assets) > 6 {
		return nil, fmt.Errorf("AI新形象缺少有效套装名或资产")
	}
	excludeBag, excludeGloves := outfitConceptExclusions(concept)
	changeHair := outfitConceptChangesHair(concept)
	filtered := design.Assets[:0]
	for _, asset := range design.Assets {
		text := asset.Name + " " + asset.Description
		if excludeBag && asset.Category == "bag" {
			continue
		}
		if excludeGloves && isGloveText(text) {
			continue
		}
		if asset.Category == "hair" && !changeHair {
			continue
		}
		filtered = append(filtered, asset)
	}
	design.Assets = filtered
	seen := map[string]bool{}
	for i := range design.Assets {
		a := &design.Assets[i]
		a.Name, a.Category, a.Description = strings.TrimSpace(a.Name), strings.TrimSpace(a.Category), strings.TrimSpace(a.Description)
		if a.Name == "" || a.Description == "" || !ValidLookCategories[a.Category] || a.Category == "full" {
			return nil, fmt.Errorf("AI返回了无效造型资产")
		}
		if seen[a.Category] {
			return nil, fmt.Errorf("AI返回了重复的%s资产", LookCategoryLabel(a.Category))
		}
		seen[a.Category] = true
	}
	for _, required := range []string{"clothing", "shoes"} {
		if !seen[required] {
			return nil, fmt.Errorf("AI新形象缺少%s设计", LookCategoryLabel(required))
		}
	}
	var outfit models.CharacterOutfit
	err = s.db.Transaction(func(tx *gorm.DB) error {
		outfit = models.CharacterOutfit{ProjectID: projectID, CharacterID: characterID, Name: design.Name, Description: design.Description, IsDefault: req.IsDefault, AuditStatus: models.LookStatusDraft, Version: 1}
		if err := tx.Create(&outfit).Error; err != nil {
			return err
		}
		if req.IsDefault {
			if err := tx.Model(&models.CharacterOutfit{}).Where("character_id = ? AND id <> ?", characterID, outfit.ID).Update("is_default", false).Error; err != nil {
				return err
			}
		}
		for i, asset := range design.Assets {
			look := models.CharacterLook{ProjectID: projectID, CharacterID: characterID, Name: asset.Name, Category: asset.Category, Description: asset.Description, Prompt: buildLookAssetPrompt(asset.Category, asset.Name, asset.Description, &project), Source: "auto", Priority: 50, AuditStatus: models.LookStatusDraft, Version: 1}
			if err := tx.Create(&look).Error; err != nil {
				return err
			}
			if err := tx.Create(&models.CharacterOutfitLook{OutfitID: outfit.ID, LookID: look.ID, Order: i}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	var saved models.CharacterOutfit
	if err := s.db.Preload("Items.Look").First(&saved, outfit.ID).Error; err != nil {
		return nil, err
	}
	return &saved, nil
}

func (s *CharacterLookService) ListOutfits(projectID, characterID uint) ([]models.CharacterOutfit, error) {
	var outfits []models.CharacterOutfit
	err := s.db.Preload("Items", func(db *gorm.DB) *gorm.DB { return db.Order("`order`, id") }).Preload("Items.Look").
		Where("project_id = ? AND character_id = ?", projectID, characterID).Order("is_default DESC, id").Find(&outfits).Error
	return outfits, err
}

func (s *CharacterLookService) SaveOutfit(projectID, characterID, outfitID uint, req OutfitInput) (*models.CharacterOutfit, error) {
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return nil, fmt.Errorf("套装名称不能为空")
	}
	looks, err := s.validOutfitLooks(projectID, characterID, req.LookIDs)
	if err != nil {
		return nil, err
	}
	if len(looks) == 0 {
		return nil, fmt.Errorf("套装至少需要一个造型资产")
	}
	var outfit models.CharacterOutfit
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if outfitID == 0 {
			outfit = models.CharacterOutfit{ProjectID: projectID, CharacterID: characterID, Name: req.Name, Description: strings.TrimSpace(req.Description), IsDefault: req.IsDefault, AuditStatus: models.LookStatusDraft, Version: 1}
			if err := tx.Create(&outfit).Error; err != nil {
				return err
			}
		} else {
			if err := tx.Where("id = ? AND project_id = ? AND character_id = ?", outfitID, projectID, characterID).First(&outfit).Error; err != nil {
				return err
			}
			if err := tx.Model(&outfit).Updates(map[string]any{"name": req.Name, "description": strings.TrimSpace(req.Description), "is_default": req.IsDefault, "audit_status": models.LookStatusDraft, "audit_note": "", "image": "", "image_task_id": "", "image_error": "", "sheet": "", "sheet_task_id": "", "sheet_error": "", "version": gorm.Expr("version + 1")}).Error; err != nil {
				return err
			}
			if err := tx.Where("outfit_id = ?", outfit.ID).Delete(&models.CharacterOutfitLook{}).Error; err != nil {
				return err
			}
		}
		if req.IsDefault {
			if err := tx.Model(&models.CharacterOutfit{}).Where("character_id = ? AND id <> ?", characterID, outfit.ID).Update("is_default", false).Error; err != nil {
				return err
			}
		}
		for i, look := range looks {
			if err := tx.Create(&models.CharacterOutfitLook{OutfitID: outfit.ID, LookID: look.ID, Order: i}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	var saved models.CharacterOutfit
	if err := s.db.Preload("Items.Look").First(&saved, outfit.ID).Error; err != nil {
		return nil, err
	}
	return &saved, nil
}

func (s *CharacterLookService) validOutfitLooks(projectID, characterID uint, ids []uint) ([]models.CharacterLook, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var looks []models.CharacterLook
	if err := s.db.Where("id IN ?", ids).Find(&looks).Error; err != nil {
		return nil, err
	}
	if len(looks) != len(ids) {
		return nil, fmt.Errorf("部分造型资产不存在")
	}
	byID := map[uint]models.CharacterLook{}
	categories := map[string]bool{}
	for _, look := range looks {
		if look.ProjectID != projectID || look.CharacterID != characterID {
			return nil, fmt.Errorf("造型资产不属于当前角色")
		}
		if look.Category == "full" || !ValidLookCategories[look.Category] {
			return nil, fmt.Errorf("套装不能包含旧版组合造型")
		}
		if categories[look.Category] && look.Category != "jewelry" && look.Category != "hair_accessory" {
			return nil, fmt.Errorf("套装内%s只能选择一个", LookCategoryLabel(look.Category))
		}
		categories[look.Category] = true
		byID[look.ID] = look
	}
	ordered := make([]models.CharacterLook, 0, len(ids))
	for _, id := range ids {
		ordered = append(ordered, byID[id])
	}
	return ordered, nil
}

func (s *CharacterLookService) DeleteOutfit(projectID, characterID, outfitID uint) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		var outfit models.CharacterOutfit
		if err := tx.Where("id = ? AND project_id = ? AND character_id = ?", outfitID, projectID, characterID).First(&outfit).Error; err != nil {
			return err
		}
		if err := tx.Where("outfit_id = ?", outfitID).Delete(&models.CharacterOutfitLook{}).Error; err != nil {
			return err
		}
		if err := tx.Where("outfit_id = ?", outfitID).Delete(&models.SceneCharacterOutfit{}).Error; err != nil {
			return err
		}
		if err := tx.Where("outfit_id = ?", outfitID).Delete(&models.ShotCharacterOutfit{}).Error; err != nil {
			return err
		}
		return tx.Delete(&outfit).Error
	})
}

func (s *CharacterLookService) ApproveOutfit(projectID, characterID, outfitID uint) error {
	return s.db.Model(&models.CharacterOutfit{}).Where("id = ? AND project_id = ? AND character_id = ?", outfitID, projectID, characterID).
		Updates(map[string]any{"audit_status": models.LookStatusApproved, "audit_note": ""}).Error
}

func (s *CharacterLookService) StartOutfitImage(projectID, characterID, outfitID uint, sheet bool) error {
	if s.tasks == nil {
		return fmt.Errorf("套装参考图生成依赖 ComfyUI 任务服务")
	}
	var outfit models.CharacterOutfit
	if err := s.db.Preload("Items.Look").Where("id = ? AND project_id = ? AND character_id = ?", outfitID, projectID, characterID).First(&outfit).Error; err != nil {
		return err
	}
	if outfit.AuditStatus != models.LookStatusApproved && outfit.AuditStatus != models.LookStatusPublished {
		return fmt.Errorf("请先审核通过套装")
	}
	if !sheet {
		excludeBag, excludeGloves := outfitConceptExclusions(outfit.Description)
		for _, item := range outfit.Items {
			if item.Look == nil {
				continue
			}
			if excludeBag && item.Look.Category == "bag" {
				return fmt.Errorf("套装要求不携带包，但当前组合仍选择了包「%s」；请先编辑组合移除该资产", item.Look.Name)
			}
			if excludeGloves && isGloveText(item.Look.Name+" "+item.Look.Description) {
				return fmt.Errorf("套装要求裸手，但当前组合仍选择了手套「%s」；请先编辑组合移除该资产", item.Look.Name)
			}
		}
	}
	if sheet && strings.TrimSpace(outfit.Image) == "" {
		return fmt.Errorf("请先生成或上传竖版套装参考图")
	}
	if (!sheet && outfit.ImageTaskID != "") || (sheet && outfit.SheetTaskID != "") {
		return fmt.Errorf("套装图片正在生成，请勿重复提交")
	}
	var ch models.Character
	if err := s.db.Where("id = ? AND project_id = ?", characterID, projectID).First(&ch).Error; err != nil {
		return err
	}
	if strings.TrimSpace(ch.Portrait) == "" {
		return fmt.Errorf("请先为角色「%s」生成或上传定妆照", ch.Name)
	}
	templateCode := outfitTemplateCode(sheet)
	var tpl models.Template
	if err := s.db.Where("code = ? AND enabled = ?", templateCode, true).First(&tpl).Error; err != nil {
		return fmt.Errorf("未找到已启用的模板 %s", templateCode)
	}
	refs := []FileMeta{{TaskID: fmt.Sprint(projectID), Name: ch.Portrait}}
	hasCharacterSheet := strings.TrimSpace(ch.Sheet) != ""
	if !sheet && hasCharacterSheet {
		refs = append(refs, FileMeta{TaskID: fmt.Sprint(projectID), Name: ch.Sheet})
	}
	prompt := outfitPrompt(&outfit, hasCharacterSheet)
	if !sheet && (len(refs) == 0 || strings.TrimSpace(refs[0].Name) == "") {
		return fmt.Errorf("角色标准像未进入换装参考图列表，已阻止生成")
	}
	params := map[string]any{"width": 928, "height": 1664, "ref_image_size": "max"}
	if sheet {
		refs = []FileMeta{{TaskID: fmt.Sprint(projectID), Name: outfit.Image}}
		prompt = "<Picture 1>=已经审核的新形象正面全身定妆照。保持同一个角色、同一张脸、同一发型、同一套服装鞋履配饰，生成四视图设定图：脸部特写、正面全身、侧面全身、背面全身，纯净背景，四个视图彼此一致。"
		params = map[string]any{"width": 1664, "height": 928}
	} else {
		for _, item := range outfit.Items {
			if item.Look != nil && item.Look.Image != "" && len(refs) < 9 {
				refs = append(refs, FileMeta{TaskID: fmt.Sprint(projectID), Name: item.Look.Image})
			}
		}
	}
	if !sheet && !strings.Contains(prompt, "<Subject 1> 是 <Picture 1> 中的当前角色标准像") {
		return fmt.Errorf("换装提示词未绑定当前角色标准像 Picture 1，已阻止生成")
	}
	log.Printf("[outfit-submit] project=%d character=%d(%s) outfit=%d portrait=%s refs=%v prompt=%q", projectID, ch.ID, ch.Name, outfit.ID, ch.Portrait, refs, prompt)
	files := map[string][]FileMeta{"ref_images": refs}
	if sheet {
		files = map[string][]FileMeta{"source_image": refs}
	}
	log.Printf("[outfit-submit] template=%s files=%v", templateCode, files)
	task, err := s.tasks.CreateTask(CreateTaskReq{TemplateID: tpl.ID, Prompt: prompt, Params: params, Files: files})
	if err != nil {
		return err
	}
	if !sheet && outfit.SheetTaskID != "" {
		_ = s.tasks.CancelTask(outfit.SheetTaskID)
	}
	field := "image_task_id"
	errorField := "image_error"
	if sheet {
		field, errorField = "sheet_task_id", "sheet_error"
	}
	updates := map[string]any{field: task.TaskID, errorField: ""}
	// 新任务开始时立即移除旧展示结果，避免页面继续显示此前 H3 产物。
	if sheet {
		updates["sheet"] = ""
	} else {
		updates["image"] = ""
		updates["sheet"] = ""
		updates["sheet_task_id"] = ""
		updates["sheet_error"] = "换装定妆照已更新，请重新生成 Krea2 四视图"
	}
	res := s.db.Model(&models.CharacterOutfit{}).Where("id = ? AND "+field+" = ''", outfit.ID).Updates(updates)
	if res.Error != nil || res.RowsAffected == 0 {
		_ = s.tasks.CancelTask(task.TaskID)
		if res.Error != nil {
			return res.Error
		}
		return fmt.Errorf("套装图片任务已存在")
	}
	go func() {
		if err := s.tasks.Execute(task.TaskID); err != nil && !errors.Is(err, errNoFreeGPU) {
			log.Printf("[outfit %d] task %s failed: %v", outfit.ID, task.TaskID, err)
		}
	}()
	return nil
}

func outfitTemplateCode(sheet bool) string {
	if sheet {
		return "krea2_character_sheet"
	}
	return "minimax_h3_look_reference"
}

func outfitConceptExclusions(text string) (excludeBag, excludeGloves bool) {
	compact := strings.NewReplacer(" ", "", "，", ",", "。", ".").Replace(strings.ToLower(text))
	for _, phrase := range []string{"不要包", "不带包", "不携带包", "无包", "不要包袋", "不带包袋"} {
		excludeBag = excludeBag || strings.Contains(compact, phrase)
	}
	for _, phrase := range []string{"不要手套", "不戴手套", "无手套", "裸手", "露出双手"} {
		excludeGloves = excludeGloves || strings.Contains(compact, phrase)
	}
	return
}

func outfitConceptChangesHair(text string) bool {
	compact := strings.NewReplacer(" ", "", "，", ",", "。", ".").Replace(strings.ToLower(text))
	for _, term := range []string{"发型", "头发", "长发", "短发", "卷发", "直发", "刘海", "马尾", "发髻", "盘发", "束发", "编发", "染发", "发色"} {
		if strings.Contains(compact, term) {
			return true
		}
	}
	return false
}

func isGloveText(text string) bool {
	return strings.Contains(text, "手套") || strings.Contains(text, "手甲") || strings.Contains(text, "护手")
}

func outfitPrompt(outfit *models.CharacterOutfit, hasCharacterSheet bool) string {
	definitions := []string{"<Subject 1> 是 <Picture 1> 中的当前角色标准像，仅作为脸部身份与画风基准；原图服装、手部遮挡物和随身包袋不作为本次造型参考。"}
	retention := []string{"<Subject 1>: identity_preserved - 只保持同一人物的脸部身份、五官、年龄感与画风，造型完全以本次设计为准。"}
	design := []string{strings.TrimSpace(outfit.Description)}
	pic, subject := 2, 2
	if hasCharacterSheet {
		definitions = append(definitions, "<Subject 2> 是 <Picture 2> 中的当前角色四视图，仅作为头身比例、体态与身体结构基准；四视图中的旧服装和配饰不沿用。")
		retention = append(retention, "<Subject 2>: structure_preserved - 只保持同一人物的头身比例、体态与完整身体结构。")
		pic, subject = 3, 3
	}
	items := append([]models.CharacterOutfitLook(nil), outfit.Items...)
	sort.SliceStable(items, func(i, j int) bool { return items[i].Order < items[j].Order })
	hasBag, hasGloves, hasHair := false, false, false
	for _, item := range items {
		if item.Look != nil {
			hasBag = hasBag || item.Look.Category == "bag"
			hasHair = hasHair || item.Look.Category == "hair"
			text := item.Look.Name + " " + item.Look.Description
			hasGloves = hasGloves || isGloveText(text)
		}
	}
	for _, item := range items {
		if item.Look == nil {
			continue
		}
		label := LookCategoryLabel(item.Look.Category)
		description := strings.TrimSpace(item.Look.Description)
		if item.Look.Image != "" {
			definitions = append(definitions, fmt.Sprintf("<Subject %d> 是 <Picture %d> 中的%s参考，只约束该资产的材质、颜色与结构。", subject, pic, label))
			retention = append(retention, fmt.Sprintf("<Subject %d>: fully_preserved - 保持%s参考的材质、颜色与结构。", subject, label))
			pic++
			subject++
		}
		design = append(design, fmt.Sprintf("%s：%s", label, description))
	}
	if !hasHair {
		design = append(design, "发型完整沿用当前角色标准像，不改变发长、发色、刘海、分缝、卷直状态与束发方式。")
	}
	if !hasGloves {
		design = append(design, "双手自然裸露且完整可见，手掌与五指结构清晰。")
	}
	if !hasBag {
		design = append(design, "双肩、双手和腰侧保持空置，不携带包袋。")
	}
	return strings.Join([]string{
		"subject_definitions:\n" + strings.Join(definitions, "\n"),
		"summary:\n[reference generation] 以当前角色身份参考和本次造型设计生成一张新的角色换装定妆照。",
		"retention_analysis:\n" + strings.Join(retention, "\n"),
		"detailed_description:\n" + strings.Join(design, "\n") + "\n视觉媒介、渲染方式、材质表现和色彩风格严格沿用当前角色标准像。9:16竖版，单人正面全身定妆照，完整头部与清晰正脸自然可见，从头发顶部到鞋底完整入镜，正面自然站立。",
		"overall_soundscape:\nN/A",
		"non_diegetic_music:\nN/A",
	}, "\n\n")
}

func (s *CharacterLookService) SaveOutfitUploadedImage(projectID, characterID, outfitID uint, filename string, data []byte, sheet bool) error {
	if s.upload == nil {
		return fmt.Errorf("上传服务未配置")
	}
	ext := strings.ToLower(filepath.Ext(filename))
	if ext != ".png" && ext != ".jpg" && ext != ".jpeg" && ext != ".webp" {
		return fmt.Errorf("仅支持 PNG/JPG/WebP")
	}
	var outfit models.CharacterOutfit
	if err := s.db.Where("id = ? AND project_id = ? AND character_id = ?", outfitID, projectID, characterID).First(&outfit).Error; err != nil {
		return err
	}
	prefix, field, taskField, errorField := "outfit", "image", "image_task_id", "image_error"
	if sheet {
		prefix, field, taskField, errorField = "outfit_sheet", "sheet", "sheet_task_id", "sheet_error"
	}
	path, _, err := s.upload.SaveFile(fmt.Sprint(projectID), "image", fmt.Sprintf("%s_%d_%d%s", prefix, outfitID, time.Now().UnixNano(), ext), data)
	if err != nil {
		return err
	}
	taskID := outfit.ImageTaskID
	if sheet {
		taskID = outfit.SheetTaskID
	}
	if taskID != "" && s.tasks != nil {
		_ = s.tasks.CancelTask(taskID)
	}
	updates := map[string]any{field: filepath.Base(path), taskField: "", errorField: ""}
	if !sheet {
		if outfit.SheetTaskID != "" && s.tasks != nil {
			_ = s.tasks.CancelTask(outfit.SheetTaskID)
		}
		updates["sheet"] = ""
		updates["sheet_task_id"] = ""
		updates["sheet_error"] = "换装定妆照已替换，请重新生成 Krea2 四视图"
	}
	return s.db.Model(&outfit).Updates(updates).Error
}

func outfitResultFilename(prefix string, outfitID uint, taskID, ext string) string {
	safeTask := strings.NewReplacer("-", "_", "/", "_", "\\", "_").Replace(strings.TrimSpace(taskID))
	return fmt.Sprintf("%s_%d_%s%s", prefix, outfitID, safeTask, ext)
}

func (s *CharacterLookService) SyncOutfitImages() {
	var outfits []models.CharacterOutfit
	if s.db.Where("image_task_id != '' OR sheet_task_id != ''").Find(&outfits).Error != nil {
		return
	}
	for i := range outfits {
		outfit := &outfits[i]
		for _, state := range []struct{ taskID, taskField, fileField, errorField, prefix string }{{outfit.ImageTaskID, "image_task_id", "image", "image_error", "outfit"}, {outfit.SheetTaskID, "sheet_task_id", "sheet", "sheet_error", "outfit_sheet"}} {
			if state.taskID == "" {
				continue
			}
			var task models.Task
			if s.db.Where("task_id = ?", state.taskID).First(&task).Error != nil {
				s.db.Model(outfit).Where(state.taskField+" = ?", state.taskID).Updates(map[string]any{state.taskField: "", state.errorField: "任务不存在"})
				continue
			}
			if task.Status == "failed" || task.Status == "cancelled" {
				s.db.Model(outfit).Where(state.taskField+" = ?", state.taskID).Updates(map[string]any{state.taskField: "", state.errorField: task.Error})
				continue
			}
			if task.Status != "success" {
				continue
			}
			file, _ := resultImageOf(&task)
			if file == "" || task.Port == nil || s.upload == nil {
				s.db.Model(outfit).Where(state.taskField+" = ?", state.taskID).Updates(map[string]any{state.taskField: "", state.errorField: "任务成功但未返回图片"})
				continue
			}
			sub, name := filepath.ToSlash(filepath.Dir(file)), filepath.Base(file)
			if sub == "." {
				sub = ""
			}
			data, err := NewComfyClient(s.tasks.comfyHostForPort(*task.Port), *task.Port).DownloadOutput(name, sub, "output")
			if err != nil {
				s.db.Model(outfit).Where(state.taskField+" = ?", state.taskID).Updates(map[string]any{state.taskField: "", state.errorField: err.Error()})
				continue
			}
			ext := filepath.Ext(file)
			if ext == "" {
				ext = ".png"
			}
			path, _, err := s.upload.SaveFile(fmt.Sprint(outfit.ProjectID), "image", outfitResultFilename(state.prefix, outfit.ID, state.taskID, ext), data)
			if err != nil {
				s.db.Model(outfit).Where(state.taskField+" = ?", state.taskID).Updates(map[string]any{state.taskField: "", state.errorField: err.Error()})
				continue
			}
			s.db.Model(outfit).Where(state.taskField+" = ?", state.taskID).Updates(map[string]any{state.fileField: filepath.Base(path), state.taskField: "", state.errorField: ""})
		}
	}
}

func (s *CharacterLookService) AssignSceneOutfits(projectID, sceneID uint, assignments []OutfitAssignment) error {
	var scene models.Scene
	if err := s.db.Where("id = ? AND project_id = ?", sceneID, projectID).First(&scene).Error; err != nil {
		return err
	}
	if err := s.validateOutfitAssignments(projectID, assignments); err != nil {
		return err
	}
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("scene_id = ?", sceneID).Delete(&models.SceneCharacterOutfit{}).Error; err != nil {
			return err
		}
		for _, a := range assignments {
			if err := tx.Create(&models.SceneCharacterOutfit{SceneID: sceneID, CharacterID: a.CharacterID, OutfitID: a.OutfitID}).Error; err != nil {
				return err
			}
		}
		return tx.Model(&models.Scene{}).Where("id = ?", sceneID).Updates(map[string]any{
			"image_file": "", "image_task_id": "", "video_file": "", "video_input_file": "", "video_task_id": "", "video_gpu": nil,
			"prompt_stale": true, "status": "pending", "error": "",
		}).Error
	}); err != nil {
		return err
	}
	return MarkSceneCandidatesStale(s.db, projectID, sceneID, "", "场景角色新形象已修改")
}

func (s *CharacterLookService) validateOutfitAssignments(projectID uint, assignments []OutfitAssignment) error {
	seen := map[uint]bool{}
	for _, a := range assignments {
		if seen[a.CharacterID] {
			return fmt.Errorf("同一角色只能选择一套造型")
		}
		seen[a.CharacterID] = true
		var outfit models.CharacterOutfit
		if err := s.db.Where("id = ? AND project_id = ? AND character_id = ?", a.OutfitID, projectID, a.CharacterID).First(&outfit).Error; err != nil {
			return fmt.Errorf("套装不属于当前项目或角色")
		}
		if outfit.AuditStatus != models.LookStatusApproved && outfit.AuditStatus != models.LookStatusPublished {
			return fmt.Errorf("套装「%s」尚未审核通过", outfit.Name)
		}
	}
	return nil
}

func (s *CharacterLookService) ListSceneOutfits(projectID, sceneID uint) ([]models.SceneCharacterOutfit, error) {
	var rows []models.SceneCharacterOutfit
	err := s.db.Preload("Outfit.Items.Look").Joins("JOIN scenes ON scenes.id = scene_character_outfits.scene_id").Where("scene_character_outfits.scene_id = ? AND scenes.project_id = ?", sceneID, projectID).Find(&rows).Error
	return rows, err
}

func (s *CharacterLookService) AssignShotOutfits(projectID, shotID uint, assignments []OutfitAssignment) error {
	var shot models.Shot
	if err := s.db.Joins("JOIN scenes ON scenes.id = shots.scene_id").Where("shots.id = ? AND scenes.project_id = ?", shotID, projectID).First(&shot).Error; err != nil {
		return err
	}
	if err := s.validateOutfitAssignments(projectID, assignments); err != nil {
		return err
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("shot_id = ?", shotID).Delete(&models.ShotCharacterOutfit{}).Error; err != nil {
			return err
		}
		for _, a := range assignments {
			if err := tx.Create(&models.ShotCharacterOutfit{ShotID: shotID, CharacterID: a.CharacterID, OutfitID: a.OutfitID}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *CharacterLookService) ListShotOutfits(projectID, shotID uint) ([]models.ShotCharacterOutfit, error) {
	var rows []models.ShotCharacterOutfit
	err := s.db.Preload("Outfit.Items.Look").Joins("JOIN shots ON shots.id = shot_character_outfits.shot_id").Joins("JOIN scenes ON scenes.id = shots.scene_id").Where("shot_character_outfits.shot_id = ? AND scenes.project_id = ?", shotID, projectID).Find(&rows).Error
	return rows, err
}
