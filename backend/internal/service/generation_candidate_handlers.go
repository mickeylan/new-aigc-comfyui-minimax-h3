package service

import (
	"net/http"
	"strconv"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func (s *Service) HandleListSceneCandidates(c *gin.Context) {
	scene, ok := s.loadScene(c)
	if !ok {
		return
	}
	rows, err := NewGenerationCandidateService(s.DB).ListScene(scene.ProjectID, scene.ID, c.Query("media_type"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, rows)
}

func (s *Service) HandleReviewGenerationCandidate(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	id, err := strconv.ParseUint(c.Param("cid"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid candidate id"})
		return
	}
	var req struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	row, err := NewGenerationCandidateService(s.DB).Review(p.ID, uint(id), req.Status, req.Reason)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, row)
}

func (s *Service) HandleBranchGenerationCandidate(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	id, err := strconv.ParseUint(c.Param("cid"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid candidate id"})
		return
	}
	var candidate models.GenerationCandidate
	if err := s.DB.Where("id = ? AND project_id = ? AND entity_type = ?", id, p.ID, "scene").First(&candidate).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "candidate not found"})
		return
	}
	if candidate.Stale {
		c.JSON(http.StatusConflict, gin.H{"error": "过期候选不能创建分支"})
		return
	}
	if s.Tasks == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "task service unavailable"})
		return
	}
	// A branch clones the successful task's immutable prompt/params/input snapshot. It must
	// not rebuild from the current Scene, which may have changed since this take was made.
	branchTask, err := s.Tasks.CloneTaskSnapshot(candidate.TaskID, &candidate.ID)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "candidate task snapshot unavailable: " + err.Error()})
		return
	}
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		var scene models.Scene
		if err := tx.Where("id = ? AND project_id = ?", candidate.EntityID, p.ID).First(&scene).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.GenerationCandidate{}).Where("project_id = ? AND entity_type = ? AND entity_id = ? AND media_type = ?", p.ID, "scene", scene.ID, candidate.MediaType).Update("is_current", false).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.GenerationCandidate{}).Where("id = ? AND stale = ?", candidate.ID, false).Updates(map[string]any{"is_current": true, "review_status": "accepted"}).Error; err != nil {
			return err
		}
		updates := map[string]any{"error": ""}
		if candidate.MediaType == "image" {
			updates["image_file"], updates["image_task_id"], updates["image_token"], updates["status"] = candidate.File, branchTask.TaskID, branchTask.TaskID, "image_pending"
			updates["video_task_id"], updates["video_file"], updates["video_input_file"], updates["video_gpu"] = "", "", "", nil
			if err := tx.Model(&models.GenerationCandidate{}).Where("project_id = ? AND entity_type = ? AND entity_id = ? AND media_type = ?", p.ID, "scene", scene.ID, "video").Updates(map[string]any{"stale": true, "stale_reason": "分镜画面候选已切换", "is_current": false}).Error; err != nil {
				return err
			}
		} else {
			updates["video_task_id"], updates["video_file"], updates["video_input_file"], updates["video_gpu"], updates["status"] = branchTask.TaskID, "", "", nil, "video_pending"
		}
		return tx.Model(&models.Scene{}).Where("id = ? AND project_id = ?", scene.ID, p.ID).Updates(updates).Error
	})
	if err != nil {
		_ = s.DB.Delete(branchTask).Error
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	go func() { _ = s.Tasks.Execute(branchTask.TaskID) }()
	c.JSON(http.StatusAccepted, gin.H{"ok": true, "parent_candidate_id": candidate.ID, "task_id": branchTask.TaskID})
}

func (s *Service) HandleSelectGenerationCandidate(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	id, err := strconv.ParseUint(c.Param("cid"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid candidate id"})
		return
	}
	row, err := NewGenerationCandidateService(s.DB).SelectCurrent(p.ID, uint(id))
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, row)
}
