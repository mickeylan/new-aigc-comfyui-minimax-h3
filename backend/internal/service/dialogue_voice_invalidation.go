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
	return db.Transaction(func(tx *gorm.DB) error {
		var dialogueIDs []uint
		if err := tx.Model(&models.Dialogue{}).Where("project_id = ? AND character = ?", projectID, character).Pluck("id", &dialogueIDs).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.Dialogue{}).Where("project_id = ? AND character = ?", projectID, character).Updates(map[string]any{
			"status": "pending", "audio_stale": true, "audio_stale_reason": reason, "audio_token": "",
		}).Error; err != nil {
			return err
		}
		if len(dialogueIDs) == 0 || !tx.Migrator().HasTable(&models.DialogueAudioCandidate{}) {
			return nil
		}
		return tx.Model(&models.DialogueAudioCandidate{}).Where("project_id = ? AND dialogue_id IN ? AND stale = ?", projectID, dialogueIDs, false).
			Updates(map[string]any{"stale": true, "stale_reason": reason, "is_current": false}).Error
	})
}
