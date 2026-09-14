package service

import (
	"fmt"
	"math"
	"strings"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

// ShotService manages the explicit director-shot layer below a Scene.
type ShotService struct{ db *gorm.DB }

func NewShotService(db *gorm.DB) *ShotService { return &ShotService{db: db} }

var shotUpdateFields = map[string]bool{
	"order": true, "act_type": true, "shot_type": true, "camera_angle": true, "camera_movement": true,
	"duration": true, "description": true, "dialogue": true, "emotion": true,
	"prompt_subject": true, "prompt_action": true, "prompt_camera": true,
	"prompt_lighting": true, "prompt_style": true,
}

func validateShot(shot *models.Shot) error {
	shot.ShotType = strings.TrimSpace(shot.ShotType)
	if shot.ActType == "" {
		shot.ActType = models.ShotActSetup
	}
	validAct := map[models.ShotActType]bool{models.ShotActSetup: true, models.ShotActRising: true, models.ShotActMidpoint: true, models.ShotActFalling: true, models.ShotActResolution: true}
	if !validAct[shot.ActType] {
		return fmt.Errorf("act_type 必须是 setup/rising/midpoint/falling/resolution")
	}
	shot.Description = strings.TrimSpace(shot.Description)
	if shot.ShotType == "" {
		return fmt.Errorf("shot_type 不能为空")
	}
	if shot.Duration <= 0 || shot.Duration > 120 || math.IsNaN(shot.Duration) || math.IsInf(shot.Duration, 0) {
		return fmt.Errorf("duration 必须在 0 到 120 秒之间")
	}
	return nil
}

func (s *ShotService) CreateShot(sceneID uint, shot models.Shot) (*models.Shot, error) {
	shot.ID, shot.SceneID = 0, sceneID
	if shot.Order <= 0 {
		var maxOrder int
		if err := s.db.Model(&models.Shot{}).Where("scene_id = ?", sceneID).Select("COALESCE(MAX(order_num), 0)").Scan(&maxOrder).Error; err != nil {
			return nil, err
		}
		shot.Order = maxOrder + 1
	}
	if err := validateShot(&shot); err != nil {
		return nil, err
	}
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&shot).Error; err != nil {
			return err
		}
		return updateSceneShotCount(tx, sceneID)
	}); err != nil {
		return nil, err
	}
	return &shot, nil
}

// ReplaceShots stores the five director stages supplied by the user as-is. It deliberately
// does not split prose by character count, because that destroys action and dialogue semantics.
func (s *ShotService) ReplaceShots(sceneID uint, shots []models.Shot) ([]models.Shot, error) {
	if len(shots) == 0 {
		return nil, fmt.Errorf("至少需要一个镜头")
	}
	for i := range shots {
		shots[i].ID, shots[i].SceneID, shots[i].Order = 0, sceneID, i+1
		if err := validateShot(&shots[i]); err != nil {
			return nil, fmt.Errorf("镜头 %d: %w", i+1, err)
		}
	}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("scene_id = ?", sceneID).Delete(&models.Shot{}).Error; err != nil {
			return err
		}
		if err := tx.Create(&shots).Error; err != nil {
			return err
		}
		return aggregateShotsIntoScene(tx, sceneID, shots)
	})
	return shots, err
}

func (s *ShotService) GetSceneShots(sceneID uint) ([]models.Shot, error) {
	var shots []models.Shot
	err := s.db.Where("scene_id = ?", sceneID).Order("order_num, id").Find(&shots).Error
	return shots, err
}

func (s *ShotService) UpdateShot(shotID uint, updates map[string]any) (*models.Shot, error) {
	var shot models.Shot
	if err := s.db.First(&shot, shotID).Error; err != nil {
		return nil, err
	}
	filtered := map[string]any{}
	for key, value := range updates {
		if !shotUpdateFields[key] {
			return nil, fmt.Errorf("不允许修改字段: %s", key)
		}
		column := key
		if key == "order" {
			column = "order_num"
		}
		filtered[column] = value
	}
	// Decode into a copy to apply the same validation before persistence.
	candidate := shot
	if v, ok := updates["act_type"].(string); ok {
		candidate.ActType = models.ShotActType(v)
	}
	if v, ok := updates["shot_type"].(string); ok {
		candidate.ShotType = v
	}
	if v, ok := updates["description"].(string); ok {
		candidate.Description = v
	}
	if v, ok := updates["duration"].(float64); ok {
		candidate.Duration = v
	}
	if err := validateShot(&candidate); err != nil {
		return nil, err
	}
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&shot).Updates(filtered).Error; err != nil {
			return err
		}
		var shots []models.Shot
		if err := tx.Where("scene_id = ?", shot.SceneID).Order("order_num, id").Find(&shots).Error; err != nil {
			return err
		}
		return aggregateShotsIntoScene(tx, shot.SceneID, shots)
	}); err != nil {
		return nil, err
	}
	if err := s.db.First(&shot, shotID).Error; err != nil {
		return nil, err
	}
	return &shot, nil
}

func (s *ShotService) DeleteShot(shotID uint) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		var shot models.Shot
		if err := tx.First(&shot, shotID).Error; err != nil {
			return err
		}
		if err := tx.Delete(&shot).Error; err != nil {
			return err
		}
		var shots []models.Shot
		if err := tx.Where("scene_id = ?", shot.SceneID).Order("order_num, id").Find(&shots).Error; err != nil {
			return err
		}
		if len(shots) == 0 {
			return updateSceneShotCount(tx, shot.SceneID)
		}
		return aggregateShotsIntoScene(tx, shot.SceneID, shots)
	})
}

func aggregateShotsIntoScene(db *gorm.DB, sceneID uint, shots []models.Shot) error {
	actLabels := map[models.ShotActType]string{
		models.ShotActSetup: "建置", models.ShotActRising: "发展", models.ShotActMidpoint: "转折",
		models.ShotActFalling: "回落", models.ShotActResolution: "收束",
	}
	contents := make([]string, 0, len(shots))
	prompts := make([]string, 0, len(shots))
	var duration float64
	for i, shot := range shots {
		label := actLabels[shot.ActType]
		if label == "" {
			label = actLabels[models.ShotActSetup]
		}
		contentParts := []string{fmt.Sprintf("镜头%d·%s（%.1f秒，%s，%s，%s）", i+1, label, shot.Duration, shot.ShotType, shot.CameraAngle, shot.CameraMovement), strings.TrimSpace(shot.Description)}
		if shot.Emotion != "" {
			contentParts = append(contentParts, "情绪："+strings.TrimSpace(shot.Emotion))
		}
		if shot.Dialogue != "" {
			contentParts = append(contentParts, "对白："+strings.TrimSpace(shot.Dialogue))
		}
		contents = append(contents, strings.Join(nonEmptyStrings(contentParts), "："))
		promptParts := nonEmptyStrings([]string{shot.PromptSubject, shot.PromptAction, shot.PromptCamera, shot.PromptLighting, shot.PromptStyle})
		if len(promptParts) > 0 {
			prompts = append(prompts, fmt.Sprintf("镜头%d（%s）：%s", i+1, label, strings.Join(promptParts, ", ")))
		}
		duration += shot.Duration
	}
	return db.Model(&models.Scene{}).Where("id = ?", sceneID).Updates(map[string]any{
		"shot_count": len(shots), "content": strings.Join(contents, "\n"), "image_prompt": strings.Join(prompts, "；"),
		"duration": math.Round(duration*10) / 10, "image_file": "", "image_token": "", "video_task_id": "", "video_file": "", "video_gpu": nil,
		"status": "pending", "error": "",
	}).Error
}

func nonEmptyStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	return result
}

func updateSceneShotCount(db *gorm.DB, sceneID uint) error {
	var count int64
	if err := db.Model(&models.Shot{}).Where("scene_id = ?", sceneID).Count(&count).Error; err != nil {
		return err
	}
	return db.Model(&models.Scene{}).Where("id = ?", sceneID).Update("shot_count", count).Error
}
