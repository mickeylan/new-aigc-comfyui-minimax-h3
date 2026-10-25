package service

import (
	"comfyui-console/internal/models"
	"gorm.io/gorm"
	"strings"
)

func invalidateDialogueAudioByIDs(tx *gorm.DB, projectID uint, dialogueIDs []uint, reason string) error {
	if len(dialogueIDs) == 0 {
		return nil
	}
	if err := tx.Model(&models.Dialogue{}).Where("project_id = ? AND id IN ?", projectID, dialogueIDs).Updates(map[string]any{
		"status": "pending", "audio_stale": true, "audio_stale_reason": reason,
		"audio_token": "", "audio_task_id": "", "audio_task_hash": "", "error": "",
	}).Error; err != nil {
		return err
	}
	if !tx.Migrator().HasTable(&models.DialogueAudioCandidate{}) {
		return nil
	}
	return tx.Model(&models.DialogueAudioCandidate{}).Where("project_id = ? AND dialogue_id IN ? AND stale = ?", projectID, dialogueIDs, false).
		Updates(map[string]any{"stale": true, "stale_reason": reason, "is_current": false}).Error
}

func invalidateNarratorDialogueAudio(db *gorm.DB, projectID uint, reason string) error {
	return db.Transaction(func(tx *gorm.DB) error {
		var dialogues []models.Dialogue
		if err := tx.Select("id", "character", "speech_type").Where("project_id = ?", projectID).Find(&dialogues).Error; err != nil {
			return err
		}
		ids := make([]uint, 0)
		for _, dialogue := range dialogues {
			speechType, _ := normalizeScriptSpeech(dialogue.SpeechType, dialogue.Character)
			if speechType == "narration" {
				ids = append(ids, dialogue.ID)
			}
		}
		return invalidateDialogueAudioByIDs(tx, projectID, ids, reason)
	})
}

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
		return invalidateDialogueAudioByIDs(tx, projectID, dialogueIDs, reason)
	})
}
