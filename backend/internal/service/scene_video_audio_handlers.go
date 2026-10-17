package service

import (
	"net/http"
	"strconv"
	"strings"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
)

func (s *Service) HandleListSceneAudioCandidates(c *gin.Context) {
	sc, ok := s.loadScene(c)
	if !ok {
		return
	}
	var rows []models.SceneAudioCandidate
	if err := s.DB.Where("project_id = ? AND scene_id = ?", sc.ProjectID, sc.ID).Order("created_at DESC, id DESC").Find(&rows).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"candidates": rows, "selection": s.Projects.sceneReviewedAudioSelection(sc)})
}

func (s *Service) HandleCreateSceneAudioCandidate(c *gin.Context) {
	sc, ok := s.loadScene(c)
	if !ok {
		return
	}
	row, err := s.Projects.CreateSceneAudioCandidate(sc)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, row)
}

func (s *Service) HandleReviewSceneAudioCandidate(c *gin.Context) {
	sc, ok := s.loadScene(c)
	if !ok {
		return
	}
	candidateID, err := strconv.ParseUint(c.Param("cid"), 10, 64)
	if err != nil || candidateID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "候选ID无效"})
		return
	}
	var req struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	row, err := s.Projects.ReviewSceneAudioCandidate(sc, uint(candidateID), strings.TrimSpace(req.Status), req.Reason)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, row)
}
