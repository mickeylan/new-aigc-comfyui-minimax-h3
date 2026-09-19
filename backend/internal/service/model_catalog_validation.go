package service

import (
	"fmt"
	"strings"

	"comfyui-console/internal/models"
)

// ValidateVideoSelection validates an executable template against the same capability metadata
// exposed by the Model Catalog. This keeps generation and catalog claims on one source of truth.
func (s *ModelCatalogService) ValidateVideoSelection(code string, files map[string][]FileMeta, duration float64) (*models.Template, error) {
	var template models.Template
	if err := s.db.Where("code = ? AND enabled = ?", strings.TrimSpace(code), true).First(&template).Error; err != nil {
		return nil, fmt.Errorf("视频模板 %s 不可用", code)
	}
	entry, err := catalogEntryFromTemplate(template)
	if err != nil {
		return nil, fmt.Errorf("视频模板 %s 能力元数据无效: %w", code, err)
	}
	if entry.Duration == nil {
		return nil, fmt.Errorf("模板 %s 不是视频模板", code)
	}
	if entry.Duration.Min != nil && duration < *entry.Duration.Min || entry.Duration.Max != nil && duration > *entry.Duration.Max {
		return nil, fmt.Errorf("模板 %s 不支持 %.3g 秒时长", code, duration)
	}
	refs, first, last := len(files["ref_images"]), len(files["first_frame"]), len(files["last_frame"])
	if refs > 0 && (entry.MaxReferences <= 0 || refs > entry.MaxReferences) {
		return nil, fmt.Errorf("模板 %s 不支持 %d 张参考图", code, refs)
	}
	if first > 0 && !entry.SupportsFirstFrame {
		return nil, fmt.Errorf("模板 %s 不支持首帧输入", code)
	}
	if last > 0 && !entry.SupportsLastFrame {
		return nil, fmt.Errorf("模板 %s 不支持尾帧输入", code)
	}
	switch entry.Mode {
	case "reference-to-video":
		if refs == 0 {
			return nil, fmt.Errorf("模板 %s 需要参考图", code)
		}
	case "image-to-video":
		if first == 0 {
			return nil, fmt.Errorf("模板 %s 需要首帧", code)
		}
	case "first-last-frame-to-video":
		if first == 0 || last == 0 {
			return nil, fmt.Errorf("模板 %s 需要首帧和尾帧", code)
		}
	case "text-to-video":
		if refs+first+last != 0 {
			return nil, fmt.Errorf("模板 %s 不接受图片输入", code)
		}
	default:
		return nil, fmt.Errorf("模板 %s 不具备受支持的视频能力", code)
	}
	return &template, nil
}
