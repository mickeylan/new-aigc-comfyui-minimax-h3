package service

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"comfyui-console/internal/models"
)

type sceneStoryDialogueSyncResult struct {
	EpisodeN     int `json:"episode_n"`
	CountChanged int `json:"count_changed"`
}

// syncEpisodeStoryDialogues only repairs active Scene.Content from the dialogue
// rows already saved for those scenes. It deliberately does not rewrite Project.Script.
func (s *Service) syncEpisodeStoryDialogues(projectID uint, episodeN int) (sceneStoryDialogueSyncResult, error) {
	result := sceneStoryDialogueSyncResult{EpisodeN: episodeN}
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		var project models.Project
		if err := tx.First(&project, projectID).Error; err != nil {
			return err
		}
		var scenes []models.Scene
		if err := tx.Where("project_id = ? AND episode_n = ? AND archived = ?", projectID, episodeN, false).Order("`order`, id").Find(&scenes).Error; err != nil {
			return err
		}
		if len(scenes) == 0 {
			return nil
		}
		if tx.Migrator().HasTable(&models.ScriptRevision{}) {
			if _, err := createScriptRevisionTx(tx, projectID, episodeN, "before_story_dialogue_sync", nil); err != nil {
				return err
			}
		}
		for _, scene := range scenes {
			var saved []models.Dialogue
			if err := tx.Where("project_id = ? AND scene_id = ?", projectID, scene.ID).Order("`order`, id").Find(&saved).Error; err != nil {
				return err
			}
			dialogues := make([]scriptDialogue, 0, len(saved))
			for _, dialogue := range saved {
				dialogues = append(dialogues, scriptDialogue{Character: dialogue.Character, SpeechType: dialogue.SpeechType, Text: dialogue.Text})
			}
			content := sceneStoryContentWithDialogues(scene.Content, dialogues)
			if content == scene.Content {
				continue
			}
			if err := tx.Model(&models.Scene{}).Where("id = ? AND project_id = ? AND archived = ?", scene.ID, projectID, false).
				Updates(map[string]any{"content": content, "prompt_stale": true}).Error; err != nil {
				return err
			}
			result.CountChanged++
		}
		return nil
	})
	return result, err
}

func (s *Service) HandleSyncEpisodeStoryDialogues(c *gin.Context) {
	project64, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || project64 == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid project id"})
		return
	}
	episode64, err := strconv.ParseInt(c.Param("number"), 10, 32)
	if err != nil || episode64 <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid episode number"})
		return
	}
	result, err := s.syncEpisodeStoryDialogues(uint(project64), int(episode64))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"episode_n": result.EpisodeN, "count_changed": result.CountChanged, "scope": "scene_content_only"})
}
