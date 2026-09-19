package service

import (
	"net/http"
	"strconv"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
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
	if _, err := NewGenerationCandidateService(s.DB).SelectCurrent(p.ID, candidate.ID); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	var scene models.Scene
	if err := s.DB.Where("id = ? AND project_id = ?", candidate.EntityID, p.ID).First(&scene).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "scene not found"})
		return
	}
	if candidate.MediaType == "image" {
		if err := s.DB.Model(&scene).Update("image_candidate_parent_id", candidate.ID).Error; err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		if err := s.Projects.StartSceneImage(&scene); err != nil {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
	} else {
		if err := s.DB.Model(&scene).Update("video_candidate_parent_id", candidate.ID).Error; err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		if err := s.Projects.GenerateSceneVideo(p, &scene); err != nil {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
	}
	c.JSON(http.StatusAccepted, gin.H{"ok": true, "parent_candidate_id": candidate.ID})
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
