package service

import (
	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

// Shared by explicit deletes and garbage collection. A history row may be removed,
// but an input referenced by a reviewed design must remain readable and registered.
func faceDesignUsesFile(db *gorm.DB, projectID uint, file string) (bool, error) {
	if file == "" || !db.Migrator().HasTable(&models.CharacterFaceReference{}) {
		return false, nil
	}
	var count int64
	err := db.Model(&models.CharacterFaceReference{}).Where("project_id=? AND file=?", projectID, file).Count(&count).Error
	return count > 0, err
}

func faceDesignTask(db *gorm.DB, projectID, characterID uint, taskID string) bool {
	if taskID == "" || !db.Migrator().HasTable(&models.CharacterFaceDesign{}) {
		return false
	}
	var count int64
	return db.Model(&models.CharacterFaceDesign{}).Where("project_id=? AND character_id=? AND task_id=?", projectID, characterID, taskID).Count(&count).Error == nil && count > 0
}
