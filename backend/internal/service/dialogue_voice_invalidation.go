package service

import (
	"comfyui-console/internal/models"
	"gorm.io/gorm"
	"strings"
)

func invalidateCharacterDialogueAudio(db *gorm.DB, projectID uint, character, reason string) error {
	character = strings.TrimSpace(character)
	if character == "" {
		return nil
	}
	return db.Model(&models.Dialogue{}).Where("project_id = ? AND character = ?", projectID, character).Updates(map[string]any{
		"status": "pending", "audio_stale": true, "audio_stale_reason": reason, "audio_token": "",
	}).Error
}
