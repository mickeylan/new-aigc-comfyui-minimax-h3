package service

import (
	"net/http"
	"strconv"

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
