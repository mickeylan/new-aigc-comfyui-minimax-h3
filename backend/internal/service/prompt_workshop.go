package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"comfyui-console/internal/models"
)

// PromptWorkshopService 提示词工作台服务：构建/优化/翻译/历史
type PromptWorkshopService struct {
	db           *gorm.DB
	textProvider TextProvider
}

// NewPromptWorkshopService 创建提示词工作台服务
func NewPromptWorkshopService(db *gorm.DB, textProvider TextProvider) *PromptWorkshopService {
	return &PromptWorkshopService{
		db:           db,
		textProvider: textProvider,
	}
}

func (s *PromptWorkshopService) validateEntityOwnership(projectID uint, entityType string, entityID uint) error {
	if projectID == 0 {
		return fmt.Errorf("project_id 不能为空")
	}
	if entityID == 0 {
		return nil // 未保存草稿可处理，但不得写历史
	}
	switch entityType {
	case "scene":
		var count int64
		if err := s.db.Model(&models.Scene{}).Where("id = ? AND project_id = ?", entityID, projectID).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return fmt.Errorf("场景不属于当前项目")
		}
	case "shot":
		var count int64
		if err := s.db.Model(&models.Shot{}).
			Joins("JOIN scenes ON scenes.id = shots.scene_id").
			Where("shots.id = ? AND scenes.project_id = ?", entityID, projectID).
			Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return fmt.Errorf("镜头不属于当前项目")
		}
	case "skill":
		var projectCount, skillCount int64
		if err := s.db.Model(&models.Project{}).Where("id = ?", projectID).Count(&projectCount).Error; err != nil {
			return err
		}
		if err := s.db.Model(&models.Skill{}).Where("id = ?", entityID).Count(&skillCount).Error; err != nil {
			return err
		}
		if projectCount == 0 || skillCount == 0 {
			return fmt.Errorf("项目或技能不存在")
		}
	default:
		return fmt.Errorf("不支持的 entity_type: %s", entityType)
	}
	return nil
}

// PromptAction 提示词操作类型
type PromptAction string

const (
	PromptActionBuild     PromptAction = "build"
	PromptActionOptimize  PromptAction = "optimize"
	PromptActionTranslate PromptAction = "translate"
	PromptActionManual    PromptAction = "manual"
	PromptActionRollback  PromptAction = "rollback"

	PromptVersionStateDraft   = "draft"
	PromptVersionStateApplied = "applied"
)

// BuildPrompt 构建提示词（基于模板和参数）
func (s *PromptWorkshopService) BuildPrompt(entityType string, entityID uint, template string, params map[string]string) (string, error) {
	return s.buildPrompt(0, entityType, entityID, template, params)
}

func (s *PromptWorkshopService) BuildPromptForProject(projectID uint, entityType string, entityID uint, template string, params map[string]string) (string, error) {
	if err := s.validateEntityOwnership(projectID, entityType, entityID); err != nil {
		return "", err
	}
	return s.buildPrompt(projectID, entityType, entityID, template, params)
}

func (s *PromptWorkshopService) buildPrompt(projectID uint, entityType string, entityID uint, template string, params map[string]string) (string, error) {
	result := template

	// 替换占位符
	for key, value := range params {
		placeholder := "{{" + key + "}}"
		result = strings.ReplaceAll(result, placeholder, value)
	}

	_, hasSubject := params["subject"]
	_, hasAction := params["action"]
	_, hasCamera := params["camera"]
	_, hasLighting := params["lighting"]
	_, hasStyle := params["style"]
	if entityType == "shot" && hasSubject && hasAction && hasCamera && hasLighting && hasStyle {
		if strings.Contains(result, "{{") || strings.Contains(result, "}}") {
			return "", fmt.Errorf("提示词仍包含未替换占位符")
		}
		lines := strings.Split(strings.ReplaceAll(result, "\r\n", "\n"), "\n")
		if len(lines) != 5 {
			return "", fmt.Errorf("Shot 提示词必须恰好包含五行")
		}
		for _, line := range lines {
			if strings.TrimSpace(line) == "" {
				return "", fmt.Errorf("Shot 五段提示词不能为空")
			}
		}
	}
	if err := s.recordVersion(projectID, entityType, entityID, result, PromptActionBuild, map[string]any{"template": template, "params": params}); err != nil {
		return "", err
	}
	return result, nil
}

// OptimizePrompt 优化提示词（使用 AI 增强）
func (s *PromptWorkshopService) OptimizePrompt(entityType string, entityID uint, prompt string, context string) (string, error) {
	return s.optimizePrompt(0, entityType, entityID, prompt, context)
}

func (s *PromptWorkshopService) OptimizePromptForProject(projectID uint, entityType string, entityID uint, prompt string, context string) (string, error) {
	if err := s.validateEntityOwnership(projectID, entityType, entityID); err != nil {
		return prompt, err
	}
	return s.optimizePrompt(projectID, entityType, entityID, prompt, context)
}

func (s *PromptWorkshopService) optimizePrompt(projectID uint, entityType string, entityID uint, prompt string, context string) (string, error) {
	if s.textProvider == nil {
		return prompt, fmt.Errorf("文本生成服务未配置")
	}

	systemPrompt := `你是一位专业的 AI 生图/视频提示词工程师。你的任务是优化用户提供的提示词，使其：
1. 更适合 AI 生成（具体、清晰、可执行）
2. 保持原意不变
3. 增加质量修饰词（如有需要）
4. 添加风格一致性描述

只输出优化后的提示词，不要其他解释。`
	if entityType == "shot" {
		systemPrompt = `优化单镜头的五段导演提示词。仅输出 JSON 对象，不要 Markdown 或解释。对象必须包含五个非空字符串字段：subject（主体与五官）、action（姿态与动作）、camera（摄影机与构图）、lighting（光线与氛围）、style（视觉风格与质感）。不得合并或遗漏字段。`
	}

	userPrompt := fmt.Sprintf("原始提示词：\n%s\n\n上下文信息：\n%s", prompt, context)

	output, err := s.textProvider.Chat(systemPrompt, userPrompt)
	if err != nil {
		return prompt, fmt.Errorf("优化失败: %w", err)
	}

	// 清理并验证输出。镜头工作台只接受可无歧义还原到五个字段的结果。
	output = strings.TrimSpace(output)
	if entityType == "shot" {
		output, err = normalizeShotPromptOutput(output)
		if err != nil {
			return prompt, fmt.Errorf("优化失败: %w", err)
		}
	}

	// 记录历史
	if err := s.recordVersion(projectID, entityType, entityID, output, PromptActionOptimize, map[string]any{
		"original": prompt,
		"context":  context,
	}); err != nil {
		// 历史记录失败不阻塞主流程
	}

	return output, nil
}

// TranslatePrompt 翻译提示词（支持中↔英）
func (s *PromptWorkshopService) TranslatePrompt(entityType string, entityID uint, prompt string, targetLang string) (string, error) {
	return s.translatePrompt(0, entityType, entityID, prompt, targetLang)
}

func (s *PromptWorkshopService) TranslatePromptForProject(projectID uint, entityType string, entityID uint, prompt string, targetLang string) (string, error) {
	if err := s.validateEntityOwnership(projectID, entityType, entityID); err != nil {
		return prompt, err
	}
	return s.translatePrompt(projectID, entityType, entityID, prompt, targetLang)
}

func (s *PromptWorkshopService) translatePrompt(projectID uint, entityType string, entityID uint, prompt string, targetLang string) (string, error) {
	if s.textProvider == nil {
		return prompt, fmt.Errorf("文本生成服务未配置")
	}

	var systemPrompt, userPrompt string

	switch targetLang {
	case "en", "english":
		systemPrompt = "你是一位专业的翻译专家。将中文提示词翻译为英文，保持 AI 生图/视频提示词的专业格式和质量修饰词。不要添加额外解释，只输出翻译结果。"
		userPrompt = fmt.Sprintf("待翻译：\n%s", prompt)
	case "zh", "chinese":
		systemPrompt = "You are a professional translator. Translate the English prompt to Chinese while maintaining the professional AI image/video prompt format. Only output the translation, no explanations."
		userPrompt = fmt.Sprintf("To translate:\n%s", prompt)
	default:
		return prompt, fmt.Errorf("不支持的目标语言: %s", targetLang)
	}
	if entityType == "shot" {
		systemPrompt += " Return only a JSON object with exactly five non-empty string fields: subject, action, camera, lighting, style. Do not use Markdown or add explanations."
	}

	output, err := s.textProvider.Chat(systemPrompt, userPrompt)
	if err != nil {
		return prompt, fmt.Errorf("翻译失败: %w", err)
	}

	// 清理并验证输出。镜头工作台只接受可无歧义还原到五个字段的结果。
	output = strings.TrimSpace(output)
	if entityType == "shot" {
		output, err = normalizeShotPromptOutput(output)
		if err != nil {
			return prompt, fmt.Errorf("翻译失败: %w", err)
		}
	}

	// 记录历史
	if err := s.recordVersion(projectID, entityType, entityID, output, PromptActionTranslate, map[string]any{
		"original":    prompt,
		"target_lang": targetLang,
	}); err != nil {
		// 历史记录失败不阻塞主流程
	}

	return output, nil
}

func normalizeShotPromptOutput(output string) (string, error) {
	cleaned := strings.TrimSpace(output)
	if strings.HasPrefix(cleaned, "```") {
		lines := strings.Split(cleaned, "\n")
		if len(lines) >= 3 && strings.HasPrefix(strings.TrimSpace(lines[0]), "```") && strings.TrimSpace(lines[len(lines)-1]) == "```" {
			cleaned = strings.TrimSpace(strings.Join(lines[1:len(lines)-1], "\n"))
		}
	}

	parts := make([]string, 0, 5)
	if strings.HasPrefix(cleaned, "{") {
		var value map[string]any
		if err := json.Unmarshal([]byte(cleaned), &value); err != nil {
			return "", fmt.Errorf("镜头提示词 JSON 无效: %w", err)
		}
		for _, keys := range [][]string{{"subject", "prompt_subject"}, {"action", "prompt_action"}, {"camera", "prompt_camera"}, {"lighting", "prompt_lighting"}, {"style", "prompt_style"}} {
			part := ""
			for _, key := range keys {
				if text, ok := value[key].(string); ok {
					part = text
					break
				}
			}
			parts = append(parts, part)
		}
	} else {
		lines := strings.Split(strings.ReplaceAll(cleaned, "\r\n", "\n"), "\n")
		if len(lines) != 5 {
			return "", fmt.Errorf("镜头提示词必须是五行或包含五段字段的 JSON")
		}
		parts = append(parts, lines...)
	}

	for i := range parts {
		parts[i] = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(parts[i], "\r\n", " "), "\n", " "))
		if parts[i] == "" {
			return "", fmt.Errorf("镜头提示词第 %d 段不能为空", i+1)
		}
	}
	return strings.Join(parts, "\n"), nil
}

// recordVersion 记录提示词版本历史
func (s *PromptWorkshopService) recordVersion(projectID uint, entityType string, entityID uint, content string, action PromptAction, metadata map[string]any) error {
	if entityID == 0 {
		return nil
	}
	metadataJSON, _ := json.Marshal(metadata)

	createdAt := time.Now()
	var latest models.PromptVersion
	if s.db.Where("project_id = ? AND entity_type = ? AND entity_id = ?", projectID, entityType, entityID).Order("created_at DESC, id DESC").First(&latest).Error == nil && !createdAt.After(latest.CreatedAt) {
		createdAt = latest.CreatedAt.Add(time.Nanosecond)
	}
	version := models.PromptVersion{
		ProjectID:  projectID,
		EntityType: entityType,
		EntityID:   entityID,
		Content:    content,
		Action:     string(action),
		State:      PromptVersionStateDraft,
		Metadata:   string(metadataJSON),
		CreatedAt:  createdAt,
	}

	return s.db.Create(&version).Error
}

// GetHistory 获取提示词版本历史
func (s *PromptWorkshopService) GetHistory(entityType string, entityID uint, limit int) ([]models.PromptVersion, error) {
	return s.getHistory(0, entityType, entityID, limit)
}

func (s *PromptWorkshopService) GetHistoryForProject(projectID uint, entityType string, entityID uint, limit int) ([]models.PromptVersion, error) {
	if err := s.validateEntityOwnership(projectID, entityType, entityID); err != nil {
		return nil, err
	}
	return s.getHistory(projectID, entityType, entityID, limit)
}

func (s *PromptWorkshopService) getHistory(projectID uint, entityType string, entityID uint, limit int) ([]models.PromptVersion, error) {
	if limit <= 0 {
		limit = 50
	}

	var versions []models.PromptVersion
	if err := s.db.Where("project_id = ? AND entity_type = ? AND entity_id = ?", projectID, entityType, entityID).
		Order("created_at DESC, id DESC").
		Limit(limit).
		Find(&versions).Error; err != nil {
		return nil, err
	}

	return versions, nil
}

// GetVersion 获取特定版本
func (s *PromptWorkshopService) GetVersion(versionID uint) (*models.PromptVersion, error) {
	var version models.PromptVersion
	if err := s.db.First(&version, versionID).Error; err != nil {
		return nil, err
	}
	return &version, nil
}

// Rollback 回滚到指定版本
func (s *PromptWorkshopService) Rollback(entityType string, entityID uint, versionID uint) (string, error) {
	return s.rollback(0, entityType, entityID, versionID)
}

func (s *PromptWorkshopService) RollbackForProject(projectID uint, entityType string, entityID uint, versionID uint) (string, error) {
	if err := s.validateEntityOwnership(projectID, entityType, entityID); err != nil {
		return "", err
	}
	return s.rollback(projectID, entityType, entityID, versionID)
}

func (s *PromptWorkshopService) rollback(projectID uint, entityType string, entityID uint, versionID uint) (string, error) {
	var version models.PromptVersion
	if err := s.db.Where("id = ? AND project_id = ?", versionID, projectID).First(&version).Error; err != nil {
		return "", fmt.Errorf("获取版本失败: %w", err)
	}
	if version.EntityType != entityType || version.EntityID != entityID {
		return "", fmt.Errorf("版本不匹配")
	}
	if err := s.recordVersion(projectID, entityType, entityID, version.Content, PromptActionRollback, map[string]any{
		"rollback_from_version": versionID,
		"action":                "rollback",
	}); err != nil {
		// 历史记录失败不阻塞主流程
	}
	return version.Content, nil
}

// CompareVersions 比较两个版本
func (s *PromptWorkshopService) CompareVersions(versionID1, versionID2 uint) (map[string]any, error) {
	v1, err := s.GetVersion(versionID1)
	if err != nil {
		return nil, fmt.Errorf("获取版本1失败: %w", err)
	}

	v2, err := s.GetVersion(versionID2)
	if err != nil {
		return nil, fmt.Errorf("获取版本2失败: %w", err)
	}

	// 计算基本统计
	hash1 := hashContent(v1.Content)
	hash2 := hashContent(v2.Content)

	return map[string]any{
		"version1": map[string]any{
			"id":         v1.ID,
			"content":    v1.Content,
			"action":     v1.Action,
			"created_at": v1.CreatedAt,
			"length":     len(v1.Content),
			"hash":       hash1,
		},
		"version2": map[string]any{
			"id":         v2.ID,
			"content":    v2.Content,
			"action":     v2.Action,
			"created_at": v2.CreatedAt,
			"length":     len(v2.Content),
			"hash":       hash2,
		},
		"is_same":       hash1 == hash2,
		"length_change": len(v2.Content) - len(v1.Content),
	}, nil
}

// hashContent 计算内容哈希
func hashContent(content string) string {
	h := sha256.Sum256([]byte(content))
	return hex.EncodeToString(h[:])
}

// DeleteOldHistory 删除旧版本历史
func (s *PromptWorkshopService) DeleteOldHistory(entityType string, entityID uint, keepCount int) (int64, error) {
	if keepCount < 1 {
		keepCount = 10
	}

	// 先获取需要保留的版本 ID
	var keepIDs []uint
	if err := s.db.Model(&models.PromptVersion{}).
		Where("entity_type = ? AND entity_id = ?", entityType, entityID).
		Order("created_at DESC, id DESC").
		Limit(keepCount).
		Pluck("id", &keepIDs).Error; err != nil {
		return 0, err
	}

	if len(keepIDs) == 0 {
		return 0, nil
	}

	// 删除不在保留列表中的版本
	result := s.db.Where("entity_type = ? AND entity_id = ? AND id NOT IN ?", entityType, entityID, keepIDs).
		Delete(&models.PromptVersion{})

	return result.RowsAffected, result.Error
}

// GetHistoryStatistics 获取历史统计信息
func (s *PromptWorkshopService) GetHistoryStatistics(entityType string, entityID uint) (map[string]any, error) {
	var total int64
	if err := s.db.Model(&models.PromptVersion{}).
		Where("entity_type = ? AND entity_id = ?", entityType, entityID).
		Count(&total).Error; err != nil {
		return nil, err
	}

	type ActionCount struct {
		Action string
		Count  int64
	}
	var actionCounts []ActionCount
	if err := s.db.Model(&models.PromptVersion{}).
		Select("action, count(*) as count").
		Where("entity_type = ? AND entity_id = ?", entityType, entityID).
		Group("action").
		Find(&actionCounts).Error; err != nil {
		return nil, err
	}

	byAction := make(map[string]int64)
	for _, ac := range actionCounts {
		byAction[ac.Action] = ac.Count
	}

	// 获取最新版本
	var latest models.PromptVersion
	latestErr := s.db.Where("entity_type = ? AND entity_id = ?", entityType, entityID).
		Order("created_at DESC, id DESC").First(&latest).Error

	return map[string]any{
		"total_versions": total,
		"by_action":      byAction,
		"latest_version": latestErr == nil && latest.ID > 0,
		"latest_at":      latest.CreatedAt,
	}, nil
}

// BatchOptimize 批量优化多个提示词
func (s *PromptWorkshopService) BatchOptimize(prompts []string, context string) ([]string, error) {
	if s.textProvider == nil {
		return prompts, fmt.Errorf("文本生成服务未配置")
	}

	results := make([]string, len(prompts))
	var errors []string

	for i, prompt := range prompts {
		optimized, err := s.OptimizePrompt("batch", 0, prompt, context)
		if err != nil {
			errors = append(errors, fmt.Sprintf("第%d个: %v", i+1, err))
			results[i] = prompt // 失败时保留原内容
		} else {
			results[i] = optimized
		}
	}

	if len(errors) > 0 && len(errors) == len(prompts) {
		return results, fmt.Errorf("全部优化失败: %s", strings.Join(errors, "; "))
	}

	return results, nil
}

// ParsePromptTemplate 解析提示词模板，提取占位符
func (s *PromptWorkshopService) ParsePromptTemplate(template string) ([]string, error) {
	var placeholders []string
	start := 0

	for {
		idx := strings.Index(template[start:], "{{")
		if idx == -1 {
			break
		}
		idx += start

		endIdx := strings.Index(template[idx:], "}}")
		if endIdx == -1 {
			break
		}
		endIdx += idx

		placeholder := template[idx+2 : endIdx]
		placeholders = append(placeholders, placeholder)
		start = endIdx + 2
	}

	return placeholders, nil
}

// ValidatePromptTemplate 验证提示词模板
func (s *PromptWorkshopService) ValidatePromptTemplate(template string) (bool, string) {
	if template == "" {
		return false, "模板不能为空"
	}

	// 检查未闭合的占位符
	openCount := strings.Count(template, "{{")
	closeCount := strings.Count(template, "}}")

	if openCount != closeCount {
		return false, fmt.Sprintf("占位符未匹配: %d 个 {{, %d 个 }}", openCount, closeCount)
	}

	return true, ""
}

// GeneratePromptFromPreset 使用预设生成提示词
func (s *PromptWorkshopService) GeneratePromptFromPreset(basePrompt string, presetTail string, negativeTail string) (string, string) {
	optimized := basePrompt
	if presetTail != "" {
		optimized += ", " + presetTail
	}

	negative := negativeTail
	if negative == "" {
		negative = "low quality, blurry, distorted, watermark, text, logo"
	}

	return optimized, negative
}
