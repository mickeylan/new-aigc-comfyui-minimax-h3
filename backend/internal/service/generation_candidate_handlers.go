package service

import (
	"errors"
	"fmt"
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

// HandleRetryGenerationCandidate retries the immutable candidate snapshot without claiming it,
// changing review state, or replacing the Scene's current output. The cloned result is promoted
// only by the candidate retry synchronizer after it succeeds. Stale snapshots are rejected:
// promoting their result would reintroduce inputs explicitly invalidated by later editorial work.
func (s *Service) HandleRetryGenerationCandidate(c *gin.Context) {
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
		c.JSON(http.StatusConflict, gin.H{"error": "过期候选不能重试；其输入已被后续编辑失效"})
		return
	}
	if s.Tasks == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "task service unavailable"})
		return
	}
	retryTask, err := s.Tasks.CloneTaskSnapshot(candidate.TaskID, &candidate.ID)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "candidate task snapshot unavailable: " + err.Error()})
		return
	}
	go func() { _ = s.Tasks.Execute(retryTask.TaskID) }()
	c.JSON(http.StatusAccepted, gin.H{"ok": true, "parent_candidate_id": candidate.ID, "task_id": retryTask.TaskID})
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
		claimed := tx.Model(&models.GenerationCandidate{}).Where("id = ? AND project_id = ? AND stale = ?", candidate.ID, p.ID, false).Updates(map[string]any{"is_current": true, "review_status": "accepted"})
		if claimed.Error != nil {
			return claimed.Error
		}
		if claimed.RowsAffected != 1 {
			return fmt.Errorf("候选已过期或状态已变化")
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

// HandleDeleteGenerationCandidate deletes a non-current candidate with no running task and no child branches.
// Returns 409 Conflict with specific reason when deletion is blocked by business rules.
func (s *Service) HandleDeleteGenerationCandidate(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	id, err := strconv.ParseUint(c.Param("cid"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid candidate id"})
		return
	}
	err = NewGenerationCandidateService(s.DB).Delete(p.ID, uint(id))
	if err != nil {
		var delErr *CandidateDeleteError
		if errors.As(err, &delErr) {
			c.JSON(http.StatusConflict, gin.H{"error": delErr.Reason})
			return
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "candidate not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
