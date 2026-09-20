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
	"transition_type": true, "transition_note": true, "start_state": true, "end_state": true,
	"duration": true, "description": true, "dialogue": true, "emotion": true,
	"prompt_subject": true, "prompt_action": true, "prompt_camera": true,
	"prompt_lighting": true, "prompt_style": true, "negative_prompt": true,
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
	shot.TransitionType = models.ShotTransitionType(strings.TrimSpace(string(shot.TransitionType)))
	shot.TransitionNote = strings.TrimSpace(shot.TransitionNote)
	shot.StartState = strings.TrimSpace(shot.StartState)
	shot.EndState = strings.TrimSpace(shot.EndState)
	validTransition := map[models.ShotTransitionType]bool{
		"": true, models.ShotTransitionCut: true, models.ShotTransitionDissolve: true,
		models.ShotTransitionFade: true, models.ShotTransitionWipe: true, models.ShotTransitionMatchCut: true,
	}
	if !validTransition[shot.TransitionType] {
		return fmt.Errorf("transition_type 必须为空或 cut/dissolve/fade/wipe/match_cut")
	}
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
		projectID, err := projectIDForScene(tx, sceneID)
		if err != nil {
			return err
		}
		if err := tx.Create(&shot).Error; err != nil {
			return err
		}
		if err := recordManualShotPrompt(tx, projectID, shot); err != nil {
			return err
		}
		if err := updateSceneShotCount(tx, sceneID); err != nil {
			return err
		}
		return markEpisodeEditorialStaleByScene(tx, sceneID)
	}); err != nil {
		return nil, err
	}
	return &shot, nil
}

// ReplaceShots stores the supplied director shots as-is while preserving IDs for existing
// shots. Stable IDs keep prompt history and shot-level asset associations attached across saves.
func (s *ShotService) ReplaceShots(sceneID uint, shots []models.Shot) ([]models.Shot, error) {
	for i := range shots {
		shots[i].SceneID, shots[i].Order = sceneID, i+1
		if err := validateShot(&shots[i]); err != nil {
			return nil, fmt.Errorf("镜头 %d: %w", i+1, err)
		}
	}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		projectID, err := projectIDForScene(tx, sceneID)
		if err != nil {
			return err
		}
		var existing []models.Shot
		if err := tx.Where("scene_id = ?", sceneID).Order("order_num, id").Find(&existing).Error; err != nil {
			return err
		}
		existingByID := make(map[uint]models.Shot, len(existing))
		for _, shot := range existing {
			existingByID[shot.ID] = shot
		}
		keep := make(map[uint]bool, len(shots))
		// Move existing rows out of the positive order range first to avoid the unique
		// (scene_id, order_num) constraint while the requested order is applied.
		if len(existing) > 0 {
			if err := tx.Model(&models.Shot{}).Where("scene_id = ?", sceneID).
				Update("order_num", gorm.Expr("-id")).Error; err != nil {
				return err
			}
		}
		for i := range shots {
			if shots[i].ID == 0 {
				if err := tx.Create(&shots[i]).Error; err != nil {
					return err
				}
				keep[shots[i].ID] = true
				continue
			}
			if _, ok := existingByID[shots[i].ID]; !ok {
				return fmt.Errorf("镜头 %d 不属于当前场景", shots[i].ID)
			}
			keep[shots[i].ID] = true
			updates := map[string]any{
				"order_num": shots[i].Order, "act_type": shots[i].ActType, "shot_type": shots[i].ShotType,
				"camera_angle": shots[i].CameraAngle, "camera_movement": shots[i].CameraMovement,
				"transition_type": shots[i].TransitionType, "transition_note": shots[i].TransitionNote, "start_state": shots[i].StartState, "end_state": shots[i].EndState,
				"duration": shots[i].Duration, "description": shots[i].Description, "dialogue": shots[i].Dialogue,
				"emotion": shots[i].Emotion, "prompt_subject": shots[i].PromptSubject,
				"prompt_action": shots[i].PromptAction, "prompt_camera": shots[i].PromptCamera,
				"prompt_lighting": shots[i].PromptLighting, "prompt_style": shots[i].PromptStyle,
				"negative_prompt": shots[i].NegativePrompt,
			}
			if err := tx.Model(&models.Shot{}).Where("id = ? AND scene_id = ?", shots[i].ID, sceneID).Updates(updates).Error; err != nil {
				return err
			}
		}
		for _, shot := range existing {
			if !keep[shot.ID] {
				if err := deleteShotAssociations(tx, shot.ID); err != nil {
					return err
				}
				if err := tx.Delete(&shot).Error; err != nil {
					return err
				}
			}
		}
		var saved []models.Shot
		if err := tx.Where("scene_id = ?", sceneID).Order("order_num, id").Find(&saved).Error; err != nil {
			return err
		}
		shots = saved
		for _, shot := range shots {
			if err := recordManualShotPrompt(tx, projectID, shot); err != nil {
				return err
			}
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
	if v, ok := updates["transition_type"].(string); ok {
		candidate.TransitionType = models.ShotTransitionType(v)
	}
	if v, ok := updates["transition_note"].(string); ok {
		candidate.TransitionNote = v
	}
	if v, ok := updates["start_state"].(string); ok {
		candidate.StartState = v
	}
	if v, ok := updates["end_state"].(string); ok {
		candidate.EndState = v
	}
	if v, ok := updates["order"].(float64); ok && v < 1 {
		return nil, fmt.Errorf("order 必须大于0")
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
		projectID, err := projectIDForScene(tx, shot.SceneID)
		if err != nil {
			return err
		}
		var saved models.Shot
		if err := tx.First(&saved, shot.ID).Error; err != nil {
			return err
		}
		if err := recordManualShotPrompt(tx, projectID, saved); err != nil {
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
		if err := deleteShotAssociations(tx, shot.ID); err != nil {
			return err
		}
		if err := tx.Delete(&shot).Error; err != nil {
			return err
		}
		var shots []models.Shot
		if err := tx.Where("scene_id = ?", shot.SceneID).Order("order_num, id").Find(&shots).Error; err != nil {
			return err
		}
		return aggregateShotsIntoScene(tx, shot.SceneID, shots)
	})
}

func canonicalShotPrompt(shot models.Shot) string {
	parts := []string{shot.PromptSubject, shot.PromptAction, shot.PromptCamera, shot.PromptLighting, shot.PromptStyle}
	for i := range parts {
		parts[i] = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(parts[i], "\r\n", " "), "\n", " "))
	}
	return strings.Join(parts, "\n")
}

func projectIDForScene(db *gorm.DB, sceneID uint) (uint, error) {
	var scene models.Scene
	if err := db.Select("project_id").First(&scene, sceneID).Error; err != nil {
		return 0, err
	}
	return scene.ProjectID, nil
}

func recordManualShotPrompt(db *gorm.DB, projectID uint, shot models.Shot) error {
	content := canonicalShotPrompt(shot)
	var count int64
	if err := db.Model(&models.PromptVersion{}).Where(
		"project_id = ? AND entity_type = ? AND entity_id = ? AND content = ? AND state = ?",
		projectID, "shot", shot.ID, content, PromptVersionStateApplied,
	).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	var draft models.PromptVersion
	err := db.Where("project_id = ? AND entity_type = ? AND entity_id = ? AND content = ? AND state = ?", projectID, "shot", shot.ID, content, PromptVersionStateDraft).
		Order("id DESC").First(&draft).Error
	if err == nil {
		return db.Model(&draft).Update("state", PromptVersionStateApplied).Error
	}
	if err != gorm.ErrRecordNotFound {
		return err
	}
	return db.Create(&models.PromptVersion{
		ProjectID: projectID, EntityType: "shot", EntityID: shot.ID,
		Content: content, Action: string(PromptActionManual), State: PromptVersionStateApplied, Metadata: "{}",
	}).Error
}

func deleteShotAssociations(db *gorm.DB, shotID uint) error {
	if err := db.Where("shot_id = ?", shotID).Delete(&models.ShotCharacterLook{}).Error; err != nil {
		return err
	}
	return db.Where("shot_id = ?", shotID).Delete(&models.ShotCharacterOutfit{}).Error
}

func aggregateShotsIntoScene(db *gorm.DB, sceneID uint, shots []models.Shot) error {
	negativePrompts := make([]string, 0, len(shots)+1)
	seenNegative := map[string]bool{}
	var scene models.Scene
	if err := db.Where("id = ?", sceneID).First(&scene).Error; err != nil {
		return err
	}
	// Scene 级负向约束可能来自人工编辑，Shot 保存不得将其静默清空。
	if negative := strings.TrimSpace(scene.NegativePrompt); negative != "" {
		seenNegative[negative] = true
		negativePrompts = append(negativePrompts, negative)
	}
	for _, shot := range shots {
		if negative := strings.TrimSpace(shot.NegativePrompt); negative != "" && !seenNegative[negative] {
			seenNegative[negative] = true
			negativePrompts = append(negativePrompts, negative)
		}
	}
	if err := db.Model(&scene).Updates(map[string]any{
		"shot_count": len(shots), "negative_prompt": strings.Join(negativePrompts, ", "), "prompt_stale": true,
		"image_file": "", "image_token": "", "image_task_id": "",
		"video_task_id": "", "video_file": "", "video_input_file": "", "video_gpu": nil, "video_full_prompt": "",
		"image_candidate_parent_id": nil, "video_candidate_parent_id": nil, "status": "pending", "error": "",
	}).Error; err != nil {
		return err
	}
	if err := MarkSceneCandidatesStale(db, scene.ProjectID, sceneID, "", "导演镜头已修改"); err != nil {
		return err
	}
	if err := db.Where("scene_id = ?", sceneID).Delete(&models.FrameCandidate{}).Error; err != nil {
		return err
	}
	if err := invalidateContinuityDependentsTx(db, scene.ProjectID, []uint{sceneID}, "上游导演镜头已修改"); err != nil {
		return err
	}
	return markEpisodeEditorialStale(db, scene.ProjectID, scene.EpisodeN)
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
