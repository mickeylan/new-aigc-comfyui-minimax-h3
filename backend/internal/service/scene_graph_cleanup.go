package service

import (
	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

func deleteIfTable(tx *gorm.DB, model any, query string, args ...any) error {
	if !tx.Migrator().HasTable(model) {
		return nil
	}
	return tx.Where(query, args...).Delete(model).Error
}

// deleteSceneDependents removes every row whose identity depends on scenes that are about
// to be destructively replaced. Prompt/script revisions remain as audit history.
func deleteSceneDependents(tx *gorm.DB, projectID uint, sceneIDs []uint) error {
	if len(sceneIDs) == 0 {
		return nil
	}
	var shotIDs []uint
	if tx.Migrator().HasTable(&models.Shot{}) {
		if err := tx.Model(&models.Shot{}).Where("scene_id IN ?", sceneIDs).Pluck("id", &shotIDs).Error; err != nil {
			return err
		}
	}
	if len(shotIDs) > 0 {
		for _, model := range []any{&models.ShotCharacterLook{}, &models.ShotCharacterOutfit{}} {
			if err := deleteIfTable(tx, model, "shot_id IN ?", shotIDs); err != nil {
				return err
			}
		}
		if err := deleteIfTable(tx, &models.PromptPolicyOverride{}, "project_id = ? AND shot_id IN ?", projectID, shotIDs); err != nil {
			return err
		}
		if err := deleteIfTable(tx, &models.SharedAssetReference{}, "project_id = ? AND shot_id IN ?", projectID, shotIDs); err != nil {
			return err
		}
	}
	for _, model := range []any{&models.SceneCharacterLook{}, &models.SceneCharacterOutfit{}, &models.FrameCandidate{}, &models.AudioLayer{}, &models.SceneContinuity{}} {
		if err := deleteIfTable(tx, model, "scene_id IN ?", sceneIDs); err != nil {
			return err
		}
	}
	if err := deleteIfTable(tx, &models.SceneContinuity{}, "source_scene_id IN ?", sceneIDs); err != nil {
		return err
	}
	if err := deleteIfTable(tx, &models.GenerationCandidate{}, "project_id = ? AND entity_type = ? AND entity_id IN ?", projectID, "scene", sceneIDs); err != nil {
		return err
	}
	if err := deleteIfTable(tx, &models.PromptPolicyOverride{}, "project_id = ? AND scene_id IN ?", projectID, sceneIDs); err != nil {
		return err
	}
	if err := deleteIfTable(tx, &models.SharedAssetReference{}, "project_id = ? AND scene_id IN ?", projectID, sceneIDs); err != nil {
		return err
	}
	if err := deleteIfTable(tx, &models.Dialogue{}, "project_id = ? AND scene_id IN ?", projectID, sceneIDs); err != nil {
		return err
	}
	if len(shotIDs) > 0 {
		if err := deleteIfTable(tx, &models.Shot{}, "id IN ?", shotIDs); err != nil {
			return err
		}
	}
	return nil
}
