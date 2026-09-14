package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"comfyui-console/internal/models"
)

// StylePresetService 风格预设服务：预设管理与推荐
type StylePresetService struct {
	db *gorm.DB
}

// NewStylePresetService 创建风格预设服务
func NewStylePresetService(db *gorm.DB) *StylePresetService {
	return &StylePresetService{db: db}
}

// InitSystemPresets 初始化系统风格预设
func (s *StylePresetService) InitSystemPresets() error {
	systemPresets := []models.StylePreset{
		// 电影感分类
		{
			Name:           "电影感冷色调",
			Category:       models.StylePresetCinematic,
			Subcategory:    "color_grading",
			PromptTail:     "cinematic color grading, desaturated blue tones, film grain, anamorphic lens flare, moody lighting",
			NegativeTail:   "oversaturated, cartoon, anime, bright colors, flat lighting",
			Reason:         "适合复仇/悬疑类题材，增强压抑氛围，电影级质感",
			UseCases:       "复仇爽剧、悬疑推理、暗黑风格",
			SceneTypes:     "夜景、情感冲突、内心独白",
			Tags:           "电影感,冷色调,压抑,悬疑",
			IsRecommended:  true,
			RecommendedFor: "复仇,悬疑,暗黑",
			IsSystem:       true,
		},
		{
			Name:           "电影感暖色调",
			Category:       models.StylePresetCinematic,
			Subcategory:    "color_grading",
			PromptTail:     "cinematic color grading, warm golden tones, soft bokeh, vintage film look, romantic lighting",
			NegativeTail:   "oversaturated, cartoon, anime, cold colors, harsh lighting",
			Reason:         "适合甜宠/温情类题材，营造浪漫氛围",
			UseCases:       "甜宠剧、爱情剧、温情场景",
			SceneTypes:     "室内、约会、情感表达",
			Tags:           "电影感,暖色调,浪漫,甜蜜",
			IsRecommended:  true,
			RecommendedFor: "甜宠,爱情,温情",
			IsSystem:       true,
		},
		{
			Name:           "好莱坞大片感",
			Category:       models.StylePresetCinematic,
			Subcategory:    "action",
			PromptTail:     "Hollywood blockbuster style, epic composition, dramatic lighting, IMAX quality, 35mm film grain, epic scale",
			NegativeTail:   "low budget, amateur, blurry, distorted",
			Reason:         "适合高潮/决战场景，大场面震撼效果",
			UseCases:       "高潮对决、大场面战斗、史诗场景",
			SceneTypes:     "决战、爆炸、史诗战斗",
			Tags:           "好莱坞,大片,震撼,史诗",
			IsRecommended:  true,
			RecommendedFor: "战斗,高潮,决战",
			IsSystem:       true,
		},
		// 动画风分类
		{
			Name:           "二次元动漫风",
			Category:       models.StylePresetAnimated,
			Subcategory:    "anime",
			PromptTail:     "anime style, cel shading, vibrant colors, clean lineart, Japanese animation aesthetic",
			NegativeTail:   "realistic, photorealistic, 3D render, Western cartoon",
			Reason:         "适合二次元/动漫向题材，经典日漫风格",
			UseCases:       "动漫改编、二次元甜宠、校园题材",
			SceneTypes:     "日常、校园、可爱互动",
			Tags:           "二次元,动漫,日漫,可爱",
			IsRecommended:  true,
			RecommendedFor: "二次元,校园,可爱",
			IsSystem:       true,
		},
		{
			Name:           "国风水墨",
			Category:       models.StylePresetAnimated,
			Subcategory:    "chinese",
			PromptTail:     "Chinese ink painting style, traditional Chinese art, xuan paper texture, delicate brushwork, classical beauty",
			NegativeTail:   "modern, Western, CG, 3D render",
			Reason:         "适合古风/仙侠题材，传承国风美学",
			UseCases:       "古风剧、仙侠剧、武侠剧",
			SceneTypes:     "山水、庭院、古建筑",
			Tags:           "国风,水墨,古风,仙侠",
			IsRecommended:  true,
			RecommendedFor: "古风,仙侠,武侠",
			IsSystem:       true,
		},
		// 写实风分类
		{
			Name:           "写实自然光",
			Category:       models.StylePresetRealistic,
			Subcategory:    "natural",
			PromptTail:     "photorealistic, natural lighting, soft shadows, high detail, 8K quality, professional photography",
			NegativeTail:   "anime, cartoon, painting, illustration, artificial",
			Reason:         "适合现实题材，自然真实质感",
			UseCases:       "都市剧、生活剧、写实风格",
			SceneTypes:     "日常、外景、自然光",
			Tags:           "写实,自然,真实,摄影",
			IsRecommended:  true,
			RecommendedFor: "都市,生活,现实",
			IsSystem:       true,
		},
		{
			Name:           "商业广告质感",
			Category:       models.StylePresetCommercial,
			Subcategory:    "advertising",
			PromptTail:     "commercial photography style, high-end advertising, clean background, professional lighting, luxury product photography",
			NegativeTail:   "amateur, low quality, cluttered, messy background",
			Reason:         "适合展示/转场场景，高端商业质感",
			UseCases:       "产品展示、转场过渡、高端场景",
			SceneTypes:     "展示、转场、奢侈品",
			Tags:           "商业,广告,高端,质感",
			IsRecommended:  false,
			RecommendedFor: "展示,转场",
			IsSystem:       true,
		},
		// 艺术风分类
		{
			Name:           "油画质感",
			Category:       models.StylePresetArtistic,
			Subcategory:    "painting",
			PromptTail:     "oil painting style, classical art, impasto texture, rich colors, Renaissance aesthetics",
			NegativeTail:   "photorealistic, digital art, 3D render",
			Reason:         "适合艺术/复古场景，经典油画风格",
			UseCases:       "艺术场景、复古风格、博物馆",
			SceneTypes:     "艺术馆、复古、怀旧",
			Tags:           "油画,艺术,复古,经典",
			IsRecommended:  true,
			RecommendedFor: "艺术,复古,怀旧",
			IsSystem:       true,
		},
		{
			Name:           "水彩梦幻",
			Category:       models.StylePresetArtistic,
			Subcategory:    "watercolor",
			PromptTail:     "watercolor painting style, soft edges, dreamy atmosphere, ethereal colors, hand-painted texture",
			NegativeTail:   "harsh lines, photorealistic, digital sharp edges",
			Reason:         "适合梦境/回忆场景，梦幻艺术风格",
			UseCases:       "梦境、回忆、幻想场景",
			SceneTypes:     "梦境、回忆、幻觉",
			Tags:           "水彩,梦幻,柔软,艺术",
			IsRecommended:  true,
			RecommendedFor: "梦境,回忆,幻想",
			IsSystem:       true,
		},
	}

	for _, preset := range systemPresets {
		// 检查是否已存在同名系统预设
		var existing models.StylePreset
		err := s.db.Where("name = ? AND is_system = ?", preset.Name, true).First(&existing).Error
		if err == gorm.ErrRecordNotFound {
			if err := s.db.Create(&preset).Error; err != nil {
				return fmt.Errorf("init preset %s failed: %w", preset.Name, err)
			}
		} else if err != nil {
			return fmt.Errorf("query preset %s failed: %w", preset.Name, err)
		}
	}
	return nil
}

// ListPresets 列出预设（支持分类/标签筛选）
func (s *StylePresetService) ListPresets(category string, tags []string, recommendedOnly bool) ([]models.StylePreset, error) {
	query := s.db.Model(&models.StylePreset{})

	if category != "" {
		query = query.Where("category = ?", category)
	}

	if recommendedOnly {
		query = query.Where("is_recommended = ?", true)
	}

	if len(tags) > 0 {
		for _, tag := range tags {
			query = query.Where("tags LIKE ?", "%"+tag+"%")
		}
	}

	var presets []models.StylePreset
	if err := query.Order("is_recommended DESC, usage_count DESC, id").Find(&presets).Error; err != nil {
		return nil, err
	}

	return presets, nil
}

// GetPreset 获取单个预设
func (s *StylePresetService) GetPreset(presetID uint) (*models.StylePreset, error) {
	var preset models.StylePreset
	if err := s.db.First(&preset, presetID).Error; err != nil {
		return nil, err
	}
	return &preset, nil
}

// GetPresetByName 按名称获取预设
func (s *StylePresetService) GetPresetByName(name string) (*models.StylePreset, error) {
	var preset models.StylePreset
	if err := s.db.Where("name = ?", name).First(&preset).Error; err != nil {
		return nil, err
	}
	return &preset, nil
}

// CreatePreset 创建自定义预设
func (s *StylePresetService) CreatePreset(preset models.StylePreset) (*models.StylePreset, error) {
	if preset.Name == "" {
		return nil, fmt.Errorf("预设名称不能为空")
	}
	preset.IsSystem = false
	if err := s.db.Create(&preset).Error; err != nil {
		return nil, err
	}
	return &preset, nil
}

// UpdatePreset 更新预设
func (s *StylePresetService) UpdatePreset(presetID uint, updates map[string]any) (*models.StylePreset, error) {
	var preset models.StylePreset
	if err := s.db.First(&preset, presetID).Error; err != nil {
		return nil, err
	}

	// 系统预设只允许更新 usage_count
	if preset.IsSystem {
		allowed := map[string]bool{"usage_count": true}
		filtered := make(map[string]any)
		for key, value := range updates {
			if allowed[key] {
				filtered[key] = value
			}
		}
		if err := s.db.Model(&preset).Updates(filtered).Error; err != nil {
			return nil, err
		}
	} else {
		// 自定义预设允许更新更多字段
		if err := s.db.Model(&preset).Updates(updates).Error; err != nil {
			return nil, err
		}
	}

	s.db.First(&preset, presetID)
	return &preset, nil
}

// DeletePreset 删除自定义预设
func (s *StylePresetService) DeletePreset(presetID uint) error {
	var preset models.StylePreset
	if err := s.db.First(&preset, presetID).Error; err != nil {
		return err
	}

	if preset.IsSystem {
		return fmt.Errorf("系统预设不可删除")
	}

	return s.db.Delete(&preset).Error
}

// IncrementUsageCount 增加使用计数
func (s *StylePresetService) IncrementUsageCount(presetID uint) error {
	return s.db.Model(&models.StylePreset{}).Where("id = ?", presetID).
		UpdateColumn("usage_count", gorm.Expr("usage_count + 1")).Error
}

// ApplyPreset 应用预设到提示词
func (s *StylePresetService) ApplyPreset(presetID uint, basePrompt string) (string, string, error) {
	preset, err := s.GetPreset(presetID)
	if err != nil {
		return "", "", err
	}

	optimizedPrompt := basePrompt
	if preset.PromptTail != "" {
		optimizedPrompt += ", " + preset.PromptTail
	}

	negativePrompt := preset.NegativeTail
	if negativePrompt == "" {
		negativePrompt = "low quality, blurry, worst quality, bad anatomy, distorted"
	}

	// 增加使用计数
	_ = s.IncrementUsageCount(presetID)

	return optimizedPrompt, negativePrompt, nil
}

// SceneContext 场景上下文（用于推荐匹配）
type SceneContext struct {
	Genre      string   `json:"genre"`       // 题材
	Tone       string   `json:"tone"`        // 基调
	SceneType  string   `json:"scene_type"`  // 场景类型
	Characters []string `json:"characters"`  // 角色
	Tags       []string `json:"tags"`        // 标签
	Lighting   string   `json:"lighting"`    // 光照
	TimeOfDay  string   `json:"time_of_day"` // 时段
}

// GetRecommendations 获取推荐预设
func (s *StylePresetService) GetRecommendations(ctx SceneContext, limit int) ([]models.StylePreset, error) {
	if limit <= 0 {
		limit = 5
	}

	query := s.db.Model(&models.StylePreset{})

	// 基于场景上下文构建筛选条件
	var conditions []string
	var args []interface{}

	if ctx.Genre != "" {
		conditions = append(conditions, "(use_cases LIKE ? OR recommended_for LIKE ?)")
		pattern := "%" + ctx.Genre + "%"
		args = append(args, pattern, pattern)
	}

	if ctx.Tone != "" {
		conditions = append(conditions, "(use_cases LIKE ? OR recommended_for LIKE ?)")
		pattern := "%" + ctx.Tone + "%"
		args = append(args, pattern, pattern)
	}

	if ctx.SceneType != "" {
		conditions = append(conditions, "scene_types LIKE ?")
		args = append(args, "%"+ctx.SceneType+"%")
	}

	if len(ctx.Tags) > 0 {
		for _, tag := range ctx.Tags {
			conditions = append(conditions, "tags LIKE ?")
			args = append(args, "%"+tag+"%")
		}
	}

	// 优先推荐标记的预设
	if len(conditions) > 0 {
		query = query.Where(strings.Join(conditions, " OR "), args...)
	}

	// 优先返回推荐标记的、使用次数高的
	var presets []models.StylePreset
	if err := query.
		Order("is_recommended DESC, usage_count DESC, id").
		Limit(limit).
		Find(&presets).Error; err != nil {
		return nil, err
	}

	// 如果没有匹配结果，返回使用次数最高的几个
	if len(presets) == 0 {
		if err := s.db.Model(&models.StylePreset{}).
			Order("usage_count DESC, id").
			Limit(limit).
			Find(&presets).Error; err != nil {
			return nil, err
		}
	}

	return presets, nil
}

// GetPresetWithReasons 获取带推荐理由的预设
func (s *StylePresetService) GetPresetWithReasons(presetID uint) (map[string]any, error) {
	preset, err := s.GetPreset(presetID)
	if err != nil {
		return nil, err
	}

	similar, _ := s.findSimilarPresets(preset)
	return map[string]any{
		"preset":  preset,
		"reasons": s.generateReasons(preset),
		"similar": similar,
	}, nil
}

// generateReasons 生成推荐理由
func (s *StylePresetService) generateReasons(preset *models.StylePreset) []string {
	var reasons []string

	if preset.IsRecommended {
		reasons = append(reasons, "系统推荐：经过验证，适合以下场景")
	}

	if preset.Reason != "" {
		reasons = append(reasons, preset.Reason)
	}

	if preset.UseCases != "" {
		reasons = append(reasons, "适用场景："+preset.UseCases)
	}

	if preset.SceneTypes != "" {
		reasons = append(reasons, "适合镜头类型："+preset.SceneTypes)
	}

	if preset.UsageCount > 100 {
		reasons = append(reasons, fmt.Sprintf("热门选择：已被使用 %d 次", preset.UsageCount))
	}

	return reasons
}

// findSimilarPresets 查找相似预设
func (s *StylePresetService) findSimilarPresets(preset *models.StylePreset) ([]models.StylePreset, error) {
	var similar []models.StylePreset

	// 同分类的预设
	err := s.db.Where("category = ? AND id != ?", preset.Category, preset.ID).
		Order("usage_count DESC").
		Limit(3).
		Find(&similar).Error

	return similar, err
}

// GetCategories 获取所有预设分类
func (s *StylePresetService) GetCategories() ([]map[string]any, error) {
	type CategoryStat struct {
		Category string
		Count    int64
	}

	var stats []CategoryStat
	if err := s.db.Model(&models.StylePreset{}).
		Select("category, count(*) as count").
		Group("category").
		Find(&stats).Error; err != nil {
		return nil, err
	}

	categoryInfo := map[string]map[string]string{
		string(models.StylePresetCinematic):  {"label": "电影感", "description": "电影级质感的色调与光影"},
		string(models.StylePresetAnimated):   {"label": "动画风", "description": "动漫、二次元、国风动画风格"},
		string(models.StylePresetRealistic):  {"label": "写实风", "description": "真实自然的摄影质感"},
		string(models.StylePresetArtistic):   {"label": "艺术风", "description": "油画、水彩等艺术风格"},
		string(models.StylePresetCommercial): {"label": "商业风", "description": "广告、商业摄影风格"},
	}

	result := make([]map[string]any, 0)
	for _, stat := range stats {
		info := categoryInfo[stat.Category]
		if info == nil {
			info = map[string]string{"label": stat.Category, "description": ""}
		}
		result = append(result, map[string]any{
			"value":       stat.Category,
			"label":       info["label"],
			"description": info["description"],
			"count":       stat.Count,
		})
	}

	return result, nil
}

// ExportPresets 导出预设为 JSON
func (s *StylePresetService) ExportPresets(category string) (string, error) {
	var presets []models.StylePreset
	query := s.db.Model(&models.StylePreset{})
	if category != "" {
		query = query.Where("category = ?", category)
	}

	if err := query.Find(&presets).Error; err != nil {
		return "", err
	}

	data, err := json.MarshalIndent(presets, "", "  ")
	if err != nil {
		return "", err
	}

	return string(data), nil
}

// ImportPresets 从 JSON 导入预设
func (s *StylePresetService) ImportPresets(jsonData string) (int, error) {
	var presets []models.StylePreset
	if err := json.Unmarshal([]byte(jsonData), &presets); err != nil {
		return 0, fmt.Errorf("解析 JSON 失败: %w", err)
	}

	imported := 0
	for _, preset := range presets {
		// 检查是否已存在同名预设
		var existing models.StylePreset
		err := s.db.Where("name = ?", preset.Name).First(&existing).Error
		if err == gorm.ErrRecordNotFound {
			preset.ID = 0
			preset.IsSystem = false
			if err := s.db.Create(&preset).Error; err == nil {
				imported++
			}
		}
	}

	return imported, nil
}

// SearchPresets 搜索预设
func (s *StylePresetService) SearchPresets(keyword string, limit int) ([]models.StylePreset, error) {
	if limit <= 0 {
		limit = 20
	}

	var presets []models.StylePreset
	searchPattern := "%" + keyword + "%"

	err := s.db.Where(
		"name LIKE ? OR tags LIKE ? OR use_cases LIKE ? OR reason LIKE ?",
		searchPattern, searchPattern, searchPattern, searchPattern,
	).
		Order("is_recommended DESC, usage_count DESC").
		Limit(limit).
		Find(&presets).Error

	return presets, err
}
