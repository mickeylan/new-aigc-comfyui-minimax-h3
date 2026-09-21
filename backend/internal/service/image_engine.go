package service

import (
	"fmt"
	"strings"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

const (
	ImageEngineKrea2        = "krea2"
	ImageEngineMiniMaxH3    = "minimax_h3"
	ImageEngineQwen21       = "qwen_image_2_1"
	TemplateQwen21T2I       = "qwen_image_2_1_t2i"
	TemplateQwen21MultiEdit = "qwen_image_2_1_multi_edit"
)

func normalizeAssetImageEngine(value string) (string, error) {
	switch strings.TrimSpace(value) {
	case "", ImageEngineKrea2:
		return ImageEngineKrea2, nil
	case ImageEngineQwen21:
		return ImageEngineQwen21, nil
	default:
		return "", fmt.Errorf("不支持的资产生图引擎")
	}
}
func normalizeSceneImageEngine(value string) (string, error) {
	switch strings.TrimSpace(value) {
	case "", ImageEngineMiniMaxH3:
		return ImageEngineMiniMaxH3, nil
	case ImageEngineQwen21:
		return ImageEngineQwen21, nil
	default:
		return "", fmt.Errorf("不支持的场景图引擎")
	}
}
func enabledTemplateByCode(db *gorm.DB, code string) (*models.Template, error) {
	var tpl models.Template
	if err := db.Where("code = ? AND enabled = ?", code, true).First(&tpl).Error; err != nil {
		return nil, fmt.Errorf("所选生图工作流模板 %s 未安装或未启用", code)
	}
	return &tpl, nil
}
func qwenTemplateCode(hasRefs bool) string {
	if hasRefs {
		return TemplateQwen21MultiEdit
	}
	return TemplateQwen21T2I
}

func qwenPortraitParams() map[string]any {
	return map[string]any{"aspect_ratio": "9:16 (Portrait Widescreen)", "megapixels": 2.5, "multiple": 8, "reference_resolution": 1024, "steps": 25, "cfg": 1, "negative_prompt": ""}
}

func qwenAspectRatio(value string) string {
	switch strings.TrimSpace(value) {
	case "9:16":
		return "9:16 (Portrait Widescreen)"
	case "1:1":
		return "1:1 (Square)"
	default:
		return "16:9 (Widescreen)"
	}
}
