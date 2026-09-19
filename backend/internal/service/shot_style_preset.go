package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

type ShotPresetApplication struct {
	Shot        models.Shot        `json:"shot"`
	Preset      models.StylePreset `json:"preset"`
	PromptStyle string             `json:"prompt_style"`
	Prompt      string             `json:"prompt"`
	Changed     bool               `json:"changed"`
	Preview     bool               `json:"preview"`
	Metadata    map[string]any     `json:"metadata"`
}

func (s *StylePresetService) RecommendationsForShot(projectID, shotID uint, limit int) ([]StyleRecommendation, error) {
	var shot models.Shot
	if err := s.db.Joins("JOIN scenes ON scenes.id = shots.scene_id").
		Where("shots.id = ? AND scenes.project_id = ?", shotID, projectID).
		Select("shots.*").First(&shot).Error; err != nil {
		return nil, err
	}
	camera := strings.TrimSpace(strings.Join([]string{shot.CameraAngle, shot.CameraMovement, shot.PromptCamera}, " "))
	return s.GetRecommendationsWithReasons(SceneContext{
		ActType: string(shot.ActType), ShotType: shot.ShotType, Emotion: shot.Emotion,
		Lighting: shot.PromptLighting, Camera: camera, SceneType: shot.Description,
	}, limit)
}

func appendPresetTail(style, tail string) (string, bool) {
	style, tail = strings.TrimSpace(style), strings.TrimSpace(tail)
	if tail == "" || strings.Contains(strings.ToLower(style), strings.ToLower(tail)) {
		return style, false
	}
	if style == "" {
		return tail, true
	}
	return style + ", " + tail, true
}

func (s *StylePresetService) ApplyPresetToShot(projectID, shotID, presetID uint, preview bool) (*ShotPresetApplication, error) {
	var shot models.Shot
	if err := s.db.Joins("JOIN scenes ON scenes.id = shots.scene_id").
		Where("shots.id = ? AND scenes.project_id = ?", shotID, projectID).
		Select("shots.*").First(&shot).Error; err != nil {
		return nil, err
	}
	preset, err := s.GetPreset(presetID)
	if err != nil {
		return nil, err
	}
	style, changed := appendPresetTail(shot.PromptStyle, preset.PromptTail)
	previewShot := shot
	previewShot.PromptStyle = style
	metadata := map[string]any{"preset_id": preset.ID, "preset_name": preset.Name, "category": preset.Category, "preview": preview}
	result := &ShotPresetApplication{Shot: previewShot, Preset: *preset, PromptStyle: style, Prompt: canonicalShotPrompt(previewShot), Changed: changed, Preview: preview, Metadata: metadata}
	if preview || !changed {
		return result, nil
	}
	metaJSON, _ := json.Marshal(metadata)
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.Shot{}).Where("id = ?", shot.ID).Update("prompt_style", style).Error; err != nil {
			return err
		}
		if err := tx.Create(&models.PromptVersion{ProjectID: projectID, EntityType: "shot", EntityID: shot.ID, Content: result.Prompt, Action: "preset_apply", Metadata: string(metaJSON)}).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.StylePreset{}).Where("id = ?", preset.ID).UpdateColumn("usage_count", gorm.Expr("usage_count + 1")).Error; err != nil {
			return err
		}
		var shots []models.Shot
		if err := tx.Where("scene_id = ?", shot.SceneID).Order("order_num, id").Find(&shots).Error; err != nil {
			return err
		}
		return aggregateShotsIntoScene(tx, shot.SceneID, shots)
	})
	if err != nil {
		return nil, fmt.Errorf("apply shot preset: %w", err)
	}
	return result, nil
}
