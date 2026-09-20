package service

import (
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"sort"
	"strings"

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
必须包含clothing、shoes、hair；其余类别只在概念确实需要时加入。每类最多一个，hair_accessory和jewelry可各一个。资产描述只写该单件资产，不写人物姓名、脸、身体、姿势或构图。不得加入武器、法宝、剧情道具，不得混入多个互斥方案。`
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
	if design.Name == "" || len(design.Assets) == 0 || len(design.Assets) > 6 {
		return nil, fmt.Errorf("AI新形象缺少有效套装名或资产")
	}
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
	for _, required := range []string{"clothing", "shoes", "hair"} {
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
	tpl, err := s.findLookTemplate()
	if err != nil {
		return err
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
	params := map[string]any{"width": 928, "height": 1664}
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
	if !sheet && !strings.Contains(prompt, "<Picture 1>=角色原始标准像") {
		return fmt.Errorf("换装提示词未绑定角色标准像 Picture 1，已阻止生成")
	}
	log.Printf("[outfit-submit] project=%d character=%d(%s) outfit=%d portrait=%s refs=%v prompt=%q", projectID, ch.ID, ch.Name, outfit.ID, ch.Portrait, refs, prompt)
	task, err := s.tasks.CreateTask(CreateTaskReq{TemplateID: tpl.ID, Prompt: prompt, Params: params, Files: map[string][]FileMeta{"ref_images": refs}})
	if err != nil {
		return err
	}
	field := "image_task_id"
	errorField := "image_error"
	if sheet {
		field, errorField = "sheet_task_id", "sheet_error"
	}
	res := s.db.Model(&models.CharacterOutfit{}).Where("id = ? AND "+field+" = ''", outfit.ID).Updates(map[string]any{field: task.TaskID, errorField: ""})
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

func outfitPrompt(outfit *models.CharacterOutfit, hasCharacterSheet bool) string {
	lines := []string{"<Picture 1>=当前角色标准像，保持同一张脸、五官、年龄感和项目画风。生成9:16竖版正面全身新形象定妆照，完整头部和清晰正脸自然可见，人物从头发顶部到鞋底完整入镜，正面自然站立。", strings.TrimSpace(outfit.Description)}
	pic := 2
	if hasCharacterSheet {
		lines = append(lines, "<Picture 2>=当前角色四视图，保持同一人物的头身比例、体态和完整身体结构；服装造型以本次新设计为准。")
		pic = 3
	}
	items := append([]models.CharacterOutfitLook(nil), outfit.Items...)
	sort.SliceStable(items, func(i, j int) bool { return items[i].Order < items[j].Order })
	for _, item := range items {
		if item.Look == nil {
			continue
		}
		line := fmt.Sprintf("%s：%s", LookCategoryLabel(item.Look.Category), strings.TrimSpace(item.Look.Description))
		if item.Look.Image != "" {
			line = fmt.Sprintf("<Picture %d>仅参考%s的材质、颜色与结构；%s", pic, LookCategoryLabel(item.Look.Category), line)
			pic++
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
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
	path, _, err := s.upload.SaveFile(fmt.Sprint(projectID), "image", fmt.Sprintf("%s_%d%s", prefix, outfitID, ext), data)
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
	return s.db.Model(&outfit).Updates(map[string]any{field: filepath.Base(path), taskField: "", errorField: ""}).Error
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
			path, _, err := s.upload.SaveFile(fmt.Sprint(outfit.ProjectID), "image", fmt.Sprintf("%s_%d%s", state.prefix, outfit.ID, ext), data)
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
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("scene_id = ?", sceneID).Delete(&models.SceneCharacterOutfit{}).Error; err != nil {
			return err
		}
		for _, a := range assignments {
			if err := tx.Create(&models.SceneCharacterOutfit{SceneID: sceneID, CharacterID: a.CharacterID, OutfitID: a.OutfitID}).Error; err != nil {
				return err
			}
		}
		return nil
	})
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
