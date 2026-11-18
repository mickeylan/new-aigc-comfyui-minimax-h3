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
	byKey := map[string]SceneReferenceCandidate{}
	for _, candidate := range candidates {
		byKey[candidate.Key] = candidate
	}
	kept := make([]SceneReferenceSelection, 0, len(valid))
	for _, ref := range valid {
		if issue := s.Projects.referenceBindingIssue(sc.ProjectID, ref, byKey[referenceKey(ref)], true); issue != "" {
			ref.InvalidReason = issue
			invalid = append(invalid, ref)
		} else {
			kept = append(kept, ref)
		}
	}
	valid = kept
	c.JSON(http.StatusOK, gin.H{"candidates": candidates, "references": valid, "invalid_references": invalid, "explicit": explicit, "max_krea2": maxSceneReferenceImages, "max_h3": maxSceneReferenceImages - 1,
		"storyboard_bindings": sceneStoryboardReferenceBindings(s.Projects.sceneStoryboardReferences(sc))})
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
	bound, _ := parseSceneReferences(sc)
	c.JSON(http.StatusOK, gin.H{"ok": true, "references": bound})
}
