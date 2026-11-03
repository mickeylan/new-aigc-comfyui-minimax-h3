package service

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func (s *Service) HandleGetSceneReferences(c *gin.Context) {
	sc, ok := s.loadScene(c)
	if !ok {
		return
	}
	selected, explicit := parseSceneReferences(sc)
	selected = s.Projects.normalizeSceneReferences(sc.ProjectID, selected)
	candidates := s.Projects.sceneReferenceCandidates(sc)
	valid, invalid := partitionSceneReferences(selected, candidates)
	c.JSON(http.StatusOK, gin.H{"candidates": candidates, "references": valid, "invalid_references": invalid, "explicit": explicit, "max_krea2": maxSceneReferenceImages, "max_h3": maxSceneReferenceImages - 1})
}

func (s *Service) HandleUpdateSceneReferences(c *gin.Context) {
	sc, ok := s.loadScene(c)
	if !ok {
		return
	}
	var req struct {
		References []SceneReferenceSelection `json:"references"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	if err := s.Projects.SaveSceneReferences(sc, req.References); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
