package service

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/gorm"

	"comfyui-console/internal/models"
)

// CharacterLookService 角色造型资产服务：独立于角色档案的造型变体管理
type CharacterLookService struct {
	db           *gorm.DB
	textProvider TextProvider
	skills       *SkillService
	tasks        *TaskService
	remote       *RemoteExec
	upload       *UploadManager
}

// ComfyConfig holds ComfyUI settings
type ComfyConfig struct {
	ComfyDir string `yaml:"comfy_dir"`
}

// NewCharacterLookService 创建角色造型服务
func NewCharacterLookService(db *gorm.DB, textProvider TextProvider) *CharacterLookService {
	return &CharacterLookService{
		db:           db,
		textProvider: textProvider,
	}
}

// SetDeps 设置依赖服务
func (s *CharacterLookService) SetDeps(tasks *TaskService, remote *RemoteExec, upload *UploadManager) {
	s.tasks = tasks
	s.remote = remote
	s.upload = upload
}

// ValidLookCategories 合法的造型分类
var ValidLookCategories = map[string]bool{
	"clothing":       true, // 服装
	"shoes":          true, // 鞋履
	"hair":           true, // 发型
	"hair_accessory": true, // 发饰
	"jewelry":        true, // 首饰
	"bag":            true, // 包
	"full":           true, // 兼容旧数据；新建不再使用
}

// LookCategoryLabel 造型分类中文名
func LookCategoryLabel(category string) string {
	labels := map[string]string{
		"clothing":       "服装",
		"shoes":          "鞋履",
		"hair":           "发型",
		"hair_accessory": "发饰",
		"jewelry":        "首饰",
		"bag":            "包",
		"full":           "旧版组合造型",
	}
	if l, ok := labels[category]; ok {
		return l
	}
	return category
}

// ListLooksByCharacter 获取角色的所有造型
func (s *CharacterLookService) ListByCharacter(characterID uint) ([]models.CharacterLook, error) {
	var looks []models.CharacterLook
	err := s.db.Where("character_id = ?", characterID).Order("priority DESC, id").Find(&looks).Error
	return looks, err
}

// ListLooksByProject 获取项目的所有造型（带角色信息）
func (s *CharacterLookService) ListByProject(projectID uint) ([]models.CharacterLook, error) {
	var looks []models.CharacterLook
	err := s.db.Preload("Character").Where("project_id = ?", projectID).Order("character_id, priority DESC, id").Find(&looks).Error
	return looks, err
}

// GetLook 获取单个造型
func (s *CharacterLookService) GetLook(id uint) (*models.CharacterLook, error) {
	var look models.CharacterLook
	err := s.db.Preload("Character").First(&look, id).Error
	return &look, err
}

// CreateLook 创建造型
func (s *CharacterLookService) CreateLook(look models.CharacterLook) (*models.CharacterLook, error) {
	look.Name = strings.TrimSpace(look.Name)
	if look.Name == "" {
		return nil, fmt.Errorf("造型名称不能为空")
	}
	if look.Category != "" && !ValidLookCategories[look.Category] {
		return nil, fmt.Errorf("无效的造型分类：%s", look.Category)
	}
	if look.Category == "full" {
		return nil, fmt.Errorf("请分别创建服装、鞋履、发型、发饰、首饰或包；不要把多种资产放进一个编辑框")
	}
	look.Source = "manual"
	look.IsDefault = false // 默认造型只能由标准像流程建立，客户端新增均为派生造型。
	look.AuditStatus = models.LookStatusDraft
	if look.Priority == 0 {
		look.Priority = 50 // 默认中等优先级
	}
	if err := s.db.Create(&look).Error; err != nil {
		return nil, err
	}
	return &look, nil
}

// EnsureDefaultLook 根据角色标准像所用服装与配饰，幂等建立一套待审核默认造型。
// 已被用户编辑的默认造型不会被后续角色档案更新覆盖。
func (s *CharacterLookService) EnsureDefaultLook(char *models.Character, project *models.Project) (*models.CharacterLook, error) {
	looks, err := s.EnsureDefaultLooks(char, project)
	if err != nil {
		return nil, err
	}
	if len(looks) == 0 {
		return nil, fmt.Errorf("未提取到默认造型资产")
	}
	return &looks[0], nil
}

// EnsureDefaultLooks 将角色档案中的造型目录拆成独立资产；一条记录只代表一件资产或一套明确服装。
func (s *CharacterLookService) EnsureDefaultLooks(char *models.Character, project *models.Project) ([]models.CharacterLook, error) {
	if char == nil || project == nil {
		return nil, fmt.Errorf("角色和项目不能为空")
	}
	source := strings.TrimSpace(char.WardrobeDetail)
	if source == "" {
		source = strings.TrimSpace(char.Style)
	}
	if source == "" {
		return nil, fmt.Errorf("角色缺少服装与配饰描述")
	}
	assets, err := s.extractLookAssets(source, project)
	if err != nil {
		return nil, err
	}
	if len(assets) == 0 {
		return nil, fmt.Errorf("未从角色档案提取到造型资产")
	}
	err = s.db.Transaction(func(tx *gorm.DB) error {
		// 仅替换系统自动建立且尚未被用户接管的默认资产。
		if err := tx.Where("project_id = ? AND character_id = ? AND is_default = ? AND source = ?", project.ID, char.ID, true, "auto_default").Delete(&models.CharacterLook{}).Error; err != nil {
			return err
		}
		for i := range assets {
			assets[i].ProjectID, assets[i].CharacterID = project.ID, char.ID
			assets[i].IsDefault, assets[i].Source = true, "auto_default"
			assets[i].AuditStatus, assets[i].Priority, assets[i].Version = models.LookStatusDraft, 100-i, 1
			assets[i].Prompt = buildLookAssetPrompt(assets[i].Category, assets[i].Name, assets[i].Description, project)
			var existing models.CharacterLook
			if tx.Where("project_id = ? AND character_id = ? AND name = ?", project.ID, char.ID, assets[i].Name).First(&existing).Error == nil {
				continue
			}
			if err := tx.Create(&assets[i]).Error; err != nil {
				return err
			}
		}
		return nil
	})
	return assets, err
}

func (s *CharacterLookService) extractLookAssets(source string, project *models.Project) ([]models.CharacterLook, error) {
	if s.textProvider == nil {
		return []models.CharacterLook{{Name: "默认服装", Category: "clothing", Description: source}}, nil
	}
	system := `把角色造型目录拆成独立资产。只输出严格 JSON：{"assets":[{"name":"名称","category":"clothing|shoes|hair|hair_accessory|jewelry|bag","description":"只描述这一件资产"}]}。不同颜色或场合的服装必须分别列出；每件鞋、发型、发饰、首饰、包分别列出；不得输出人物外貌、身份、动作或剧情说明；兵器、法宝、玉佩剧情道具和绣帕不属于造型资产，不输出。`
	user := fmt.Sprintf("项目题材：%s\n画风：%s\n待拆分造型目录：\n%s", project.Genre, project.Style, source)
	raw, err := s.textProvider.Chat(system, user)
	if err != nil {
		return nil, fmt.Errorf("拆分默认造型资产失败: %w", err)
	}
	var result struct {
		Assets []models.CharacterLook `json:"assets"`
	}
	if err := parseJSONObject(raw, &result); err != nil {
		return nil, fmt.Errorf("解析默认造型资产失败: %w", err)
	}
	out := make([]models.CharacterLook, 0, len(result.Assets))
	for _, asset := range result.Assets {
		asset.Name, asset.Description = strings.TrimSpace(asset.Name), strings.TrimSpace(asset.Description)
		if asset.Name == "" || asset.Description == "" || asset.Category == "full" || !ValidLookCategories[asset.Category] {
			continue
		}
		out = append(out, asset)
	}
	return out, nil
}

func defaultLookPrompt(_ *models.Character, project *models.Project, description string) string {
	return buildLookAssetPrompt("full", "默认造型", description, project)
}

// buildLookAssetPrompt 只描述当前造型资产本身；人物身份由独立定妆照负责。
func buildLookAssetPrompt(category, name, description string, project *models.Project) string {
	subject := map[string]string{
		"clothing":       "一套服装",
		"shoes":          "一双鞋履",
		"hair":           "一个发型设计",
		"hair_accessory": "一件发饰",
		"jewelry":        "一件首饰",
		"bag":            "一个包",
		"full":           "一套完整穿搭组合",
	}[category]
	if subject == "" {
		subject = "一件造型资产"
	}
	parts := []string{subject + "「" + strings.TrimSpace(name) + "」", strings.TrimSpace(description)}
	if project != nil && strings.TrimSpace(project.Style) != "" {
		parts = append(parts, "视觉风格："+strings.TrimSpace(project.Style))
	}
	parts = append(parts, "独立造型资产参考图，干净中性背景，完整展示材质、颜色、结构和细节")
	return strings.Join(parts, "，")
}

// UpdateLook 更新造型
func (s *CharacterLookService) UpdateLook(id uint, req models.CharacterLook) (*models.CharacterLook, error) {
	var look models.CharacterLook
	if err := s.db.First(&look, id).Error; err != nil {
		return nil, err
	}

	updates := map[string]any{}

	if req.Name != "" && req.Name != look.Name {
		// 检查名称唯一性
		var cnt int64
		s.db.Model(&models.CharacterLook{}).Where("character_id = ? AND name = ? AND id != ?", look.CharacterID, req.Name, id).Count(&cnt)
		if cnt > 0 {
			return nil, fmt.Errorf("该角色下已存在同名造型")
		}
		updates["name"] = strings.TrimSpace(req.Name)
	}

	if req.Category != "" && req.Category != look.Category {
		if !ValidLookCategories[req.Category] || req.Category == "full" {
			return nil, fmt.Errorf("无效的造型分类：%s", req.Category)
		}
		updates["category"] = req.Category
	}
	if value := strings.TrimSpace(req.Description); value != "" && value != look.Description {
		updates["description"] = value
	}
	if value := strings.TrimSpace(req.Prompt); value != "" && value != look.Prompt {
		updates["prompt"] = value
	}
	if req.Priority != look.Priority {
		updates["priority"] = req.Priority
	}
	if req.IsShotRelated != look.IsShotRelated {
		updates["is_shot_related"] = req.IsShotRelated
	}

	// 任何修改都需要重新审核
	if len(updates) > 0 {
		updates["audit_status"] = models.LookStatusDraft
		updates["audit_note"] = ""
		updates["audit_version"] = look.AuditVersion + 1
		updates["version"] = look.Version + 1
		updates["source"] = "manual"
		updates["image"] = ""
		updates["image_task_id"] = ""
		updates["image_error"] = ""
		if look.ImageTaskID != "" && s.tasks != nil {
			_ = s.tasks.CancelTask(look.ImageTaskID)
		}
	}

	if err := s.db.Model(&look).Updates(updates).Error; err != nil {
		return nil, err
	}

	s.db.First(&look, id)
	return &look, nil
}

// DeleteLook 删除造型
func (s *CharacterLookService) DeleteLook(id uint) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		var look models.CharacterLook
		if err := tx.First(&look, id).Error; err != nil {
			return err
		}
		if err := tx.Where("project_id = ? AND entity_type = ? AND entity_id = ?", look.ProjectID, VariantCharacterLook, look.ID).Delete(&models.AssetVariant{}).Error; err != nil {
			return err
		}
		// 删除造型
		if err := tx.Delete(&look).Error; err != nil {
			return err
		}
		// 清理关联
		tx.Where("look_id = ?", id).Delete(&models.SceneCharacterLook{})
		tx.Where("look_id = ?", id).Delete(&models.ShotCharacterLook{})
		return nil
	})
}

type CharacterLookExpansion struct {
	Description string `json:"description"`
	Prompt      string `json:"prompt"`
}

// PreviewExpansion 根据尚未保存的用户简述生成可审核描述和 Krea2 提示词。
func (s *CharacterLookService) PreviewExpansion(look *models.CharacterLook, char *models.Character, project *models.Project) (*CharacterLookExpansion, error) {
	if s.textProvider == nil {
		return nil, fmt.Errorf("文本生成服务未配置")
	}
	_ = char // 人物身份由独立定妆照管理，禁止混入造型资产提示词。
	system := `你是专业的影视造型资产设计师。把用户对一个造型资产的简单描述扩写为详细设计，并生成一条可直接生图的资产提示词。
只输出严格 JSON：{"description":"中文详细资产描述","prompt":"Krea2自然语言资产提示词"}。
description 和 prompt 都只能描述当前这一件资产，明确材质、颜色、款式、结构和细节。不要描述人物姓名、年龄、身份、脸、身体、姿势或人物构图；不要混入角色档案、整套人物形象、兵器或法宝。服装只画服装，鞋履只画鞋履，发饰只画发饰，首饰只画首饰，包只画包。`
	user := fmt.Sprintf("项目题材：%s\n画风：%s\n资产分类：%s\n资产名称：%s\n用户简述：%s", project.Genre, project.Style, LookCategoryLabel(look.Category), look.Name, look.Description)
	output, err := s.textProvider.Chat(system, user)
	if err != nil {
		return nil, fmt.Errorf("AI 扩写造型失败: %w", err)
	}
	var result CharacterLookExpansion
	if err := parseJSONObject(output, &result); err != nil {
		return nil, fmt.Errorf("AI 造型结果解析失败: %w", err)
	}
	result.Description, result.Prompt = strings.TrimSpace(result.Description), strings.TrimSpace(result.Prompt)
	if len([]rune(result.Description)) < 20 || len([]rune(result.Prompt)) < 20 {
		return nil, fmt.Errorf("AI 返回的造型描述或提示词过短，请重试")
	}
	characterName := ""
	if char != nil {
		characterName = char.Name
	}
	for _, forbidden := range []string{characterName, "角色外貌", "人物形象", "单一角色", "全身站立", "头肩定妆照"} {
		if strings.TrimSpace(forbidden) != "" && strings.Contains(result.Prompt, forbidden) {
			return nil, fmt.Errorf("AI 把人物形象混入资产提示词，请重试")
		}
	}
	return &result, nil
}

// ExpandDescription 扩写已保存造型，并使旧审核和参考图失效。
func (s *CharacterLookService) ExpandDescription(look *models.CharacterLook, char *models.Character, project *models.Project) (string, error) {
	result, err := s.PreviewExpansion(look, char, project)
	if err != nil {
		return "", err
	}
	updates := map[string]any{"description": result.Description, "prompt": result.Prompt, "source": "auto", "audit_status": models.LookStatusDraft, "audit_note": "", "audit_version": gorm.Expr("audit_version + 1"), "version": gorm.Expr("version + 1"), "image": "", "image_task_id": "", "image_error": ""}
	if err := s.db.Model(look).Updates(updates).Error; err != nil {
		return "", err
	}
	return result.Description, nil
}

// GeneratePrompt 生成参考图提示词
func (s *CharacterLookService) GeneratePrompt(look *models.CharacterLook, char *models.Character, project *models.Project) (string, error) {
	if strings.TrimSpace(look.Description) == "" {
		return "", fmt.Errorf("请先填写或生成造型描述")
	}

	_ = char // 造型资产提示词不得混入人物形象提示词。
	prompt := buildLookAssetPrompt(look.Category, look.Name, look.Description, project)

	// 更新提示词
	updates := map[string]any{
		"prompt":        prompt,
		"audit_status":  models.LookStatusDraft,
		"audit_version": gorm.Expr("audit_version + 1"),
	}
	if err := s.db.Model(look).Updates(updates).Error; err != nil {
		return "", err
	}

	return prompt, nil
}

// ApproveLook 审核通过造型
func (s *CharacterLookService) ApproveLook(id uint, note string) error {
	var look models.CharacterLook
	if err := s.db.First(&look, id).Error; err != nil {
		return err
	}

	if look.Description == "" {
		return fmt.Errorf("造型描述不能为空")
	}

	return s.db.Model(&look).Updates(map[string]any{
		"audit_status":  models.LookStatusApproved,
		"audit_note":    strings.TrimSpace(note),
		"audit_version": look.AuditVersion + 1,
	}).Error
}

// RejectLook 驳回造型
func (s *CharacterLookService) RejectLook(id uint, reason string) error {
	return s.db.Model(&models.CharacterLook{}).Where("id = ?", id).Updates(map[string]any{
		"audit_status": models.LookStatusRejected,
		"audit_note":   strings.TrimSpace(reason),
	}).Error
}

// PublishLook 发布造型（审核通过后才能发布）
func (s *CharacterLookService) PublishLook(id uint) error {
	var look models.CharacterLook
	if err := s.db.First(&look, id).Error; err != nil {
		return err
	}

	if look.AuditStatus != models.LookStatusApproved {
		return fmt.Errorf("只有审核通过的造型才能发布")
	}

	return s.db.Model(&look).Update("audit_status", models.LookStatusPublished).Error
}

// StartImageGeneration 生成单件独立造型资产图；完整人物换装由 CharacterOutfit 负责。
func (s *CharacterLookService) StartImageGeneration(look *models.CharacterLook) error {
	if s.tasks == nil {
		return fmt.Errorf("造型资产图生成依赖 ComfyUI 任务服务")
	}
	if look.AuditStatus != models.LookStatusApproved && look.AuditStatus != models.LookStatusPublished {
		return fmt.Errorf("请先审核通过造型")
	}
	if strings.TrimSpace(look.Prompt) == "" {
		return fmt.Errorf("请先生成或填写参考图提示词")
	}
	if strings.TrimSpace(look.ImageTaskID) != "" {
		return fmt.Errorf("造型资产图正在生成，请勿重复提交")
	}
	var tpl models.Template
	if err := s.db.Where("code = ? AND enabled = ?", "krea2_asset_reference", true).First(&tpl).Error; err != nil {
		return fmt.Errorf("未找到已启用的独立造型资产模板 krea2_asset_reference")
	}
	task, err := s.tasks.CreateTask(CreateTaskReq{TemplateID: tpl.ID, Prompt: look.Prompt, Params: map[string]any{"width": 1024, "height": 1024}})
	if err != nil {
		return err
	}
	if err := s.db.Model(look).Updates(map[string]any{"image_task_id": task.TaskID, "image_error": ""}).Error; err != nil {
		_ = s.tasks.CancelTask(task.TaskID)
		return err
	}
	go func() {
		if err := s.tasks.Execute(task.TaskID); err != nil && !errors.Is(err, errNoFreeGPU) {
			log.Printf("[character_look %d] execute task %s failed: %v", look.ID, task.TaskID, err)
		}
	}()
	return nil
}

// findLookTemplate 只允许使用支持参考图身份锚定的 H3 造型模板。
func (s *CharacterLookService) findLookTemplate() (*models.Template, error) {
	var tpl models.Template
	if err := s.db.Where("code = ? AND enabled = ?", "minimax_h3_look_reference", true).First(&tpl).Error; err != nil {
		return nil, fmt.Errorf("未找到已启用的 H3 造型参考图模板 minimax_h3_look_reference")
	}
	return &tpl, nil
}

func (s *CharacterLookService) SyncImages() {
	var looks []models.CharacterLook
	if s.db.Where("image_task_id != ''").Find(&looks).Error != nil {
		return
	}
	for i := range looks {
		look := &looks[i]
		var task models.Task
		if s.db.Where("task_id = ?", look.ImageTaskID).First(&task).Error != nil {
			s.db.Model(look).Where("image_task_id = ?", look.ImageTaskID).Updates(map[string]any{"image_task_id": "", "image_error": "任务不存在，请重新生成"})
			continue
		}
		if task.Status == "failed" || task.Status == "cancelled" {
			s.db.Model(look).Where("image_task_id = ?", task.TaskID).Updates(map[string]any{"image_task_id": "", "image_error": task.Error})
			continue
		}
		if task.Status != "success" {
			continue
		}
		file, _ := resultImageOf(&task)
		if file == "" || task.Port == nil || s.upload == nil {
			s.db.Model(look).Where("image_task_id = ?", task.TaskID).Updates(map[string]any{"image_task_id": "", "image_error": "任务成功但未返回图片"})
			continue
		}
		sub, name := filepath.ToSlash(filepath.Dir(file)), filepath.Base(file)
		if sub == "." {
			sub = ""
		}
		data, err := NewComfyClient(s.tasks.comfyHostForPort(*task.Port), *task.Port).DownloadOutput(name, sub, "output")
		if err != nil {
			s.db.Model(look).Where("image_task_id = ?", task.TaskID).Updates(map[string]any{"image_task_id": "", "image_error": err.Error()})
			continue
		}
		ext := filepath.Ext(file)
		if ext == "" {
			ext = ".png"
		}
		path, _, err := s.upload.SaveFile(fmt.Sprint(look.ProjectID), "image", fmt.Sprintf("look_%d_%d%s", look.ID, time.Now().UnixNano(), ext), data)
		if err != nil {
			s.db.Model(look).Where("image_task_id = ?", task.TaskID).Updates(map[string]any{"image_task_id": "", "image_error": err.Error()})
			continue
		}
		_, _, _ = updateSelectedAsset(s.db, look.ProjectID, VariantCharacterLook, look.ID, filepath.Base(path), task.Prompt, "krea2_task:"+task.TaskID, map[string]any{"image_task_id": "", "image_error": ""}, "image_task_id", task.TaskID)
	}
}

// AssignToScene 将造型关联到场景
func (s *CharacterLookService) AssignToScene(sceneID, lookID uint, order int, isFeatured bool) error {
	// 检查是否已存在关联
	var existing models.SceneCharacterLook
	err := s.db.Where("scene_id = ? AND look_id = ?", sceneID, lookID).First(&existing).Error
	if err == nil {
		// 已存在，更新
		return s.db.Model(&existing).Updates(map[string]any{
			"order":       order,
			"is_featured": isFeatured,
		}).Error
	}

	scl := models.SceneCharacterLook{
		SceneID:    sceneID,
		LookID:     lookID,
		Order:      order,
		IsFeatured: isFeatured,
	}
	return s.db.Create(&scl).Error
}

// UnassignFromScene 取消场景关联
func (s *CharacterLookService) UnassignFromScene(sceneID, lookID uint) error {
	return s.db.Where("scene_id = ? AND look_id = ?", sceneID, lookID).Delete(&models.SceneCharacterLook{}).Error
}

// AssignToShot 将造型关联到镜头
func (s *CharacterLookService) AssignToShot(shotID, lookID uint, order int, isFeatured bool) error {
	// Validate look exists and get its project
	var look models.CharacterLook
	if err := s.db.First(&look, lookID).Error; err != nil {
		return fmt.Errorf("造型不存在")
	}

	// Validate shot exists and belongs to the same project
	var shot models.Shot
	if err := s.db.First(&shot, shotID).Error; err != nil {
		return fmt.Errorf("镜头不存在")
	}
	var scene models.Scene
	if err := s.db.First(&scene, shot.SceneID).Error; err != nil {
		return fmt.Errorf("场景不存在")
	}
	if scene.ProjectID != look.ProjectID {
		return fmt.Errorf("造型与镜头不属于同一项目，不能关联")
	}

	var existing models.ShotCharacterLook
	err := s.db.Where("shot_id = ? AND look_id = ?", shotID, lookID).First(&existing).Error
	if err == nil {
		return s.db.Model(&existing).Updates(map[string]any{
			"order":       order,
			"is_featured": isFeatured,
		}).Error
	}

	shcl := models.ShotCharacterLook{
		ShotID:     shotID,
		LookID:     lookID,
		Order:      order,
		IsFeatured: isFeatured,
	}
	return s.db.Create(&shcl).Error
}

// UnassignFromShot 取消镜头关联
func (s *CharacterLookService) UnassignFromShot(shotID, lookID uint) error {
	return s.db.Where("shot_id = ? AND look_id = ?", shotID, lookID).Delete(&models.ShotCharacterLook{}).Error
}

// GetSceneLooks 获取场景的所有造型关联
func (s *CharacterLookService) GetSceneLooks(sceneID uint) ([]models.SceneCharacterLook, error) {
	var scl []models.SceneCharacterLook
	err := s.db.Preload("Look").Preload("Look.Character").Where("scene_id = ?", sceneID).Order("`order`, id").Find(&scl).Error
	return scl, err
}

// GetShotLooks 获取镜头的所有造型关联
func (s *CharacterLookService) GetShotLooks(shotID uint) ([]models.ShotCharacterLook, error) {
	var shcl []models.ShotCharacterLook
	err := s.db.Preload("Look").Preload("Look.Character").Where("shot_id = ?", shotID).Order("`order`, id").Find(&shcl).Error
	return shcl, err
}

// GetLookReferenceImages 获取造型参考图（用于 Krea2 场景图注入）
// 返回按优先级排序的参考图列表
func (s *CharacterLookService) GetLookReferenceImages(sceneID uint, maxImages int) ([]ImageRef, []string, error) {
	looks, err := s.GetSceneLooks(sceneID)
	if err != nil {
		return nil, nil, err
	}

	// 过滤已发布的造型
	var publishedLooks []models.SceneCharacterLook
	for _, scl := range looks {
		if scl.Look != nil && (scl.Look.AuditStatus == models.LookStatusApproved || scl.Look.AuditStatus == models.LookStatusPublished) {
			publishedLooks = append(publishedLooks, scl)
		}
	}

	// 按主推优先、优先级降序排序
	// （这里简化处理，实际可能需要更复杂的排序逻辑）
	type lookWithScore struct {
		scl    models.SceneCharacterLook
		score  int
		isFull bool
	}

	var scored []lookWithScore
	for _, scl := range publishedLooks {
		score := scl.Look.Priority
		if scl.IsFeatured {
			score += 100 // 主推造型优先
		}
		isFull := scl.Look.Category == "full"
		scored = append(scored, lookWithScore{scl, score, isFull})
	}

	// 按分数降序、完整造型优先排序
	// （完整造型放在前面作为主要参考）
	for i := 0; i < len(scored); i++ {
		for j := i + 1; j < len(scored); j++ {
			if scored[j].score > scored[i].score ||
				(scored[j].score == scored[i].score && scored[j].isFull && !scored[i].isFull) {
				scored[i], scored[j] = scored[j], scored[i]
			}
		}
	}

	refs := make([]ImageRef, 0, maxImages)
	lines := make([]string, 0, maxImages)
	for i, item := range scored {
		if len(refs) >= maxImages {
			break
		}
		look := item.scl.Look
		if look.Image == "" {
			continue
		}

		// 读取图片文件
		imgPath := filepath.Join("input", fmt.Sprintf("%d", look.ProjectID), look.Image)
		data, err := s.readFile(imgPath)
		if err != nil {
			log.Printf("[character_look] read image %s failed: %v", imgPath, err)
			continue
		}

		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(look.Image)), ".")
		if ext == "jpg" {
			ext = "jpeg"
		}
		if ext != "jpeg" && ext != "png" && ext != "webp" {
			ext = "jpeg"
		}

		refs = append(refs, ImageRef{URL: fmt.Sprintf("data:image/%s;base64,%s", ext, data)})
		featured := ""
		if item.scl.IsFeatured {
			featured = " [主推]"
		}
		charName := ""
		if look.Character != nil {
			charName = look.Character.Name
		}
		lines = append(lines, fmt.Sprintf("- 图%d：角色「%s」%s%s（%s）", i+1, charName, LookCategoryLabel(look.Category), featured, strings.TrimSpace(look.Description)))
	}

	return refs, lines, nil
}

// GetShotRelatedLooks 获取镜头相关的造型（用于 MiniMax H3）
// 仅返回 is_shot_related=true 的造型，按优先级排序
func (s *CharacterLookService) GetShotRelatedLooks(shotID uint) ([]models.CharacterLook, error) {
	var shcl []models.ShotCharacterLook
	if err := s.db.Preload("Look").Where("shot_id = ?", shotID).Find(&shcl).Error; err != nil {
		return nil, err
	}

	var looks []models.CharacterLook
	for _, item := range shcl {
		if item.Look != nil && item.Look.IsShotRelated &&
			(item.Look.AuditStatus == models.LookStatusApproved || item.Look.AuditStatus == models.LookStatusPublished) {
			looks = append(looks, *item.Look)
		}
	}

	// 按优先级降序排序
	for i := 0; i < len(looks); i++ {
		for j := i + 1; j < len(looks); j++ {
			if looks[j].Priority > looks[i].Priority {
				looks[i], looks[j] = looks[j], looks[i]
			}
		}
	}

	return looks, nil
}

// BuildLookContextForShot 构建镜头造型上下文（用于 MiniMax H3 提示词）
func (s *CharacterLookService) BuildLookContextForShot(shotID uint, char *models.Character) (string, error) {
	looks, err := s.GetShotRelatedLooks(shotID)
	if err != nil {
		return "", err
	}

	if len(looks) == 0 {
		return "", nil
	}

	lines := make([]string, 0, len(looks))
	for _, look := range looks {
		desc := strings.TrimSpace(look.Description)
		if desc == "" {
			desc = "外观保持与参考图完全一致"
		}
		lines = append(lines, fmt.Sprintf("- %s「%s」%s：%s",
			LookCategoryLabel(look.Category), look.Name,
			fmt.Sprintf("(优先级%d)", look.Priority), desc))
	}

	charName := "角色"
	if char != nil {
		charName = char.Name
	}

	return fmt.Sprintf("【镜头中%s的造型（必须严格遵守，保证%s外观一致）】\n%s",
		charName, charName, strings.Join(lines, "\n")), nil
}

// readFile 读取文件并返回 base64 编码
func (s *CharacterLookService) SaveUploadedImage(look *models.CharacterLook, filename string, data []byte) error {
	if s.upload == nil {
		return fmt.Errorf("上传服务未配置")
	}
	ext := strings.ToLower(filepath.Ext(filename))
	if ext != ".png" && ext != ".jpg" && ext != ".jpeg" && ext != ".webp" {
		return fmt.Errorf("仅支持 PNG/JPG/WebP")
	}
	path, _, err := s.upload.SaveFile(fmt.Sprint(look.ProjectID), "image", fmt.Sprintf("look_%d_%d%s", look.ID, time.Now().UnixNano(), ext), data)
	if err != nil {
		return err
	}
	if look.ImageTaskID != "" && s.tasks != nil {
		_ = s.tasks.CancelTask(look.ImageTaskID)
	}
	_, _, err = updateSelectedAsset(s.db, look.ProjectID, VariantCharacterLook, look.ID, filepath.Base(path), look.Prompt, "manual_upload", map[string]any{"image_task_id": "", "image_error": ""}, "", nil)
	return err
}

func (s *CharacterLookService) readFile(path string) (string, error) {
	if s.remote == nil {
		return "", errors.New("remote service not configured")
	}
	f, err := s.remote.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

// GetLooksByScene 获取场景关联的造型列表（简化版）
func (s *CharacterLookService) GetLooksByScene(sceneID uint) ([]models.CharacterLook, error) {
	var scl []models.SceneCharacterLook
	if err := s.db.Preload("Look").Preload("Look.Character").Where("scene_id = ?", sceneID).Find(&scl).Error; err != nil {
		return nil, err
	}
	looks := make([]models.CharacterLook, 0, len(scl))
	for _, item := range scl {
		if item.Look != nil {
			looks = append(looks, *item.Look)
		}
	}
	return looks, nil
}

// GetLooksByShot 获取镜头关联的造型列表（简化版）
func (s *CharacterLookService) GetLooksByShot(shotID uint) ([]models.CharacterLook, error) {
	var shcl []models.ShotCharacterLook
	if err := s.db.Preload("Look").Preload("Look.Character").Where("shot_id = ?", shotID).Find(&shcl).Error; err != nil {
		return nil, err
	}
	looks := make([]models.CharacterLook, 0, len(shcl))
	for _, item := range shcl {
		if item.Look != nil {
			looks = append(looks, *item.Look)
		}
	}
	return looks, nil
}

// SceneLookAssignmentRequest 场景造型关联请求
type SceneLookAssignmentRequest struct {
	LookID     uint `json:"look_id"`
	Order      int  `json:"order"`
	IsFeatured bool `json:"is_featured"`
}

// AssignLooksToScene 批量将造型关联到场景
func (s *CharacterLookService) AssignLooksToScene(sceneID uint, assignments []SceneLookAssignmentRequest) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		// 删除现有关联
		if err := tx.Where("scene_id = ?", sceneID).Delete(&models.SceneCharacterLook{}).Error; err != nil {
			return err
		}
		// 创建新关联
		for _, a := range assignments {
			scl := models.SceneCharacterLook{
				SceneID:    sceneID,
				LookID:     a.LookID,
				Order:      a.Order,
				IsFeatured: a.IsFeatured,
			}
			if err := tx.Create(&scl).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// ShotLookAssignmentRequest 镜头造型关联请求
type ShotLookAssignmentRequest struct {
	LookID     uint `json:"look_id"`
	Order      int  `json:"order"`
	IsFeatured bool `json:"is_featured"`
}

// AssignLooksToShot 批量将造型关联到镜头
func (s *CharacterLookService) AssignLooksToShot(shotID uint, assignments []ShotLookAssignmentRequest) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		// 删除现有关联
		if err := tx.Where("shot_id = ?", shotID).Delete(&models.ShotCharacterLook{}).Error; err != nil {
			return err
		}
		// 创建新关联
		for _, a := range assignments {
			shcl := models.ShotCharacterLook{
				ShotID:     shotID,
				LookID:     a.LookID,
				Order:      a.Order,
				IsFeatured: a.IsFeatured,
			}
			if err := tx.Create(&shcl).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
