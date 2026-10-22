package service

import (
	"net/http"
	"strconv"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
)

func (s *Service) dependencyProjectID(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid project id"})
		return 0, false
	}
	var count int64
	if err := s.DB.Model(&models.Project{}).Where("id = ?", id).Count(&count).Error; err != nil || count != 1 {
		c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
		return 0, false
	}
	return uint(id), true
}

func (s *Service) HandleListArtifactDependencies(c *gin.Context) {
	projectID, ok := s.dependencyProjectID(c)
	if !ok {
		return
	}
	if s.ArtifactDependencies == nil || s.Cfg == nil || !s.Cfg.Extensibility.EnableDependencyGraph {
		c.JSON(http.StatusOK, gin.H{"enabled": false, "items": []models.ArtifactDependency{}})
		return
	}
	query := s.DB.Where("project_id = ?", projectID).Order("upstream_kind, upstream_id, downstream_kind, downstream_id")
	if kind := c.Query("kind"); kind != "" {
		query = query.Where("upstream_kind = ? OR downstream_kind = ?", kind, kind)
	}
	var rows []models.ArtifactDependency
	if err := query.Find(&rows).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"enabled": true, "items": rows})
}

func (s *Service) HandleAffectedArtifacts(c *gin.Context) {
	projectID, ok := s.dependencyProjectID(c)
	if !ok {
		return
	}
	kind := c.Query("kind")
	id, err := strconv.ParseUint(c.Query("artifact_id"), 10, 32)
	if err != nil || kind == "" {
		c.JSON(400, gin.H{"error": "kind and artifact_id are required"})
		return
	}
	items, err := s.ArtifactDependencies.Affected(projectID, ArtifactRef{Kind: kind, ID: uint(id)})
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"enabled": s.Cfg != nil && s.Cfg.Extensibility.EnableDependencyGraph, "items": items})
}

func (s *Service) HandleListArtifactInvalidations(c *gin.Context) {
	projectID, ok := s.dependencyProjectID(c)
	if !ok {
		return
	}
	var rows []models.ArtifactInvalidationEvent
	if err := s.DB.Where("project_id = ?", projectID).Order("created_at DESC, id DESC").Limit(100).Find(&rows).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"items": rows})
}
