package service

import (
	"math"
	"net/http"
	"strings"
	"time"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
)

// HandleUpdateSceneStoryboardVideoRole changes only how the saved storyboard image participates in video generation.
// Existing prompts, manually selected frames, and generated media are deliberately preserved.
func (s *Service) HandleUpdateSceneStoryboardVideoRole(c *gin.Context) {
	sc, ok := s.loadScene(c)
	if !ok {
		return
	}
	var req struct {
		Role              string     `json:"storyboard_video_role"`
		Time              *float64   `json:"storyboard_video_time"`
		ExpectedUpdatedAt *time.Time `json:"expected_updated_at"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "invalid request"})
		return
	}
	role := strings.TrimSpace(req.Role)
	if role != "start" && role != "keyframe" && role != "end" && role != "none" {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "storyboard_video_role must be start, keyframe, end, or none"})
		return
	}
	storyboardTime := req.Time
	if role != "keyframe" {
		storyboardTime = nil
	} else if storyboardTime != nil && (math.IsNaN(*storyboardTime) || math.IsInf(*storyboardTime, 0) || *storyboardTime < 0 || *storyboardTime > sc.Duration) {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "storyboard_video_time must be finite and within scene duration"})
		return
	}
	changed := storyboardVideoRole(sc) != role || !optionalFloatEqual(sc.StoryboardVideoTime, storyboardTime)
	updates := map[string]any{"storyboard_video_role": role, "storyboard_video_time": storyboardTime}
	if changed {
		updates["prompt_stale"] = true
		updates["video_exec_source_hash"] = ""
		updates["video_exec_confirmed_hash"] = ""
		updates["video_exec_confirmed_at"] = nil
	}
	query := s.DB.Model(&models.Scene{}).Where("id = ? AND project_id = ?", sc.ID, sc.ProjectID)
	if req.ExpectedUpdatedAt != nil {
		query = query.Where("updated_at = ?", *req.ExpectedUpdatedAt)
	}
	result := query.Updates(updates)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": result.Error.Error()})
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "scene was modified; reload and retry"})
		return
	}
	var saved models.Scene
	if err := s.DB.Where("id = ? AND project_id = ?", sc.ID, sc.ProjectID).First(&saved).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, saved)
}

func optionalFloatEqual(a, b *float64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
