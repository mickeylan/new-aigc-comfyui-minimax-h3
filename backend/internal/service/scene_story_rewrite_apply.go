package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type sceneStoryApplyRequest struct {
	Content     string               `json:"content"`
	Dialogues   []sceneStoryDialogue `json:"dialogues"`
	SourceHash  string               `json:"source_hash"`
	BaseContent string               `json:"base_content"`
}

// sceneStorySourceHash binds a preview to all authoritative DB inputs, not to the editable draft.
func sceneStorySourceHash(scene *models.Scene, dialogues []models.Dialogue, chapters []models.Chapter) (string, error) {
	type dialogueSource struct {
		ID                                                    uint
		Order                                                 int
		Character, SpeechType, Text                           string
		AudioFile, AudioTaskID, AudioToken, AudioHash, Status string
	}
	type chapterSource struct {
		ID      uint
		Order   int
		Content string
	}
	payload := struct {
		SceneID    uint
		ProjectID  uint
		EpisodeN   int
		Content    string
		Generation uint
		Archived   bool
		Dialogues  []dialogueSource
		Chapters   []chapterSource
	}{scene.ID, scene.ProjectID, scene.EpisodeN, scene.Content, scene.Generation, scene.Archived, make([]dialogueSource, 0, len(dialogues)), make([]chapterSource, 0, len(chapters))}
	for _, d := range dialogues {
		payload.Dialogues = append(payload.Dialogues, dialogueSource{d.ID, d.Order, d.Character, d.SpeechType, d.Text, d.AudioFile, d.AudioTaskID, d.AudioToken, d.AudioHash, d.Status})
	}
	for _, ch := range chapters {
		payload.Chapters = append(payload.Chapters, chapterSource{ch.ID, ch.Order, ch.Content})
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func sceneStoryDialogueHasAudio(d models.Dialogue) bool {
	return d.AudioFile != "" || d.PreviousAudioFile != "" || d.AudioTaskID != "" || d.AudioToken != "" || d.Status == "ready" || d.Status == "synthesizing" || d.Status == "running" || d.Status == "queued"
}

func sceneStoryRewriteWarnings(r sceneStoryRewriteResult, ds []models.Dialogue) []string {
	warnings := []string{}
	for i, d := range ds {
		if r.Dialogues[i].SpeechType == "unknown" {
			warnings = append(warnings, fmt.Sprintf("对白%d类型仍为unknown，需要人工确认", d.ID))
		}
		if (r.Dialogues[i].Speaker != d.Character || r.Dialogues[i].SpeechType != d.SpeechType) && sceneStoryDialogueHasAudio(d) {
			warnings = append(warnings, fmt.Sprintf("对白%d已有音频；变更说话人前需先处理现有配音", d.ID))
		}
	}
	return warnings
}

// HandleApplySceneStory atomically applies an explicitly reviewed preview without touching media.
func (s *Service) HandleApplySceneStory(c *gin.Context) {
	scene, ok := s.loadScene(c)
	if !ok {
		return
	}
	var req sceneStoryApplyRequest
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2<<20)
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Content) == "" || req.SourceHash == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "content、dialogues和source_hash不能为空"})
		return
	}
	result := sceneStoryRewriteResult{Content: strings.TrimSpace(req.Content), Dialogues: req.Dialogues}
	var warnings []string
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		var locked models.Scene
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND project_id = ?", scene.ID, scene.ProjectID).First(&locked).Error; err != nil {
			return err
		}
		if locked.Archived {
			return errSceneStoryConflict
		}
		var ds []models.Dialogue
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("project_id = ? AND scene_id = ?", locked.ProjectID, locked.ID).Order("`order`, id").Find(&ds).Error; err != nil {
			return err
		}
		chapters, err := s.sceneStorySourceChaptersDB(tx, &locked)
		if err != nil {
			return err
		}
		currentHash, err := sceneStorySourceHash(&locked, ds, chapters)
		if err != nil {
			return err
		}
		if currentHash != req.SourceHash {
			return errSceneStoryConflict
		}
		if err := validateSceneStoryRewrite(result, ds, chapters); err != nil {
			return fmt.Errorf("结果无效: %w", err)
		}
		warnings = sceneStoryRewriteWarnings(result, ds)
		if err := validateSceneStoryNarrative(result.Content, req.BaseContent, ds, chapters); err != nil {
			// A human-reviewed stylistic edit is advisory, never a creative gate.
			warnings = append(warnings, err.Error())
		}
		for i, d := range ds {
			proposal := result.Dialogues[i]
			if (proposal.Speaker != d.Character || proposal.SpeechType != d.SpeechType) && sceneStoryDialogueHasAudio(d) {
				return errSceneStoryAudioConflict
			}
			if err := tx.Model(&models.Dialogue{}).Where("id = ? AND project_id = ? AND scene_id = ?", d.ID, locked.ProjectID, locked.ID).Updates(map[string]any{"character": proposal.Speaker, "speech_type": proposal.SpeechType, "source": "ai_reviewed"}).Error; err != nil {
				return err
			}
		}
		return tx.Model(&models.Scene{}).Where("id = ? AND project_id = ?", locked.ID, locked.ProjectID).Updates(map[string]any{"content": result.Content, "prompt_stale": true}).Error
	})
	if err != nil {
		status := http.StatusInternalServerError
		if err == errSceneStoryConflict || err == errSceneStoryAudioConflict {
			status = http.StatusConflict
		}
		if strings.HasPrefix(err.Error(), "结果无效:") {
			status = http.StatusUnprocessableEntity
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"content": result.Content, "dialogues": result.Dialogues, "warnings": warnings, "persisted": true})
}

var (
	errSceneStoryConflict      = fmt.Errorf("场景或原文来源已变化，请重新预览")
	errSceneStoryAudioConflict = fmt.Errorf("说话人变更涉及已有配音，请先处理现有配音后再应用")
)
