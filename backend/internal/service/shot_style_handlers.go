package service

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

func (s *Service) HandleShotStyleRecommendations(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	shotID, err := strconv.ParseUint(c.Param("shid"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid shot id"})
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "3"))
	rows, err := s.StylePresets.RecommendationsForShot(p.ID, uint(shotID), limit)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, rows)
}

func (s *Service) HandleApplyShotStylePreset(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	shotID, err := strconv.ParseUint(c.Param("shid"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid shot id"})
		return
	}
	var req struct {
		PresetID uint `json:"preset_id"`
		Preview  bool `json:"preview"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.PresetID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "preset_id is required"})
		return
	}
	result, err := s.StylePresets.ApplyPresetToShot(p.ID, uint(shotID), req.PresetID, req.Preview)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}
