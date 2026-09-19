package service

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type assetReconciliationRequest struct {
	Characters []string `json:"characters"`
	Locations  []string `json:"locations"`
	Props      []string `json:"props"`
}

func (s *Service) HandleSuggestAssetReconciliation(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	var req assetReconciliationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	rows, err := s.AssetReconciliation.Suggest(p.ID, map[string][]string{"character": req.Characters, "location": req.Locations, "prop": req.Props})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"suggestions": rows})
}

func (s *Service) HandleApplyAssetReconciliation(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	var req struct {
		Decisions []AssetReconciliationDecision `json:"decisions" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	if err := s.AssetReconciliation.Apply(p.ID, req.Decisions); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
