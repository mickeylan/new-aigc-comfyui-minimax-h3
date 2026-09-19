package service

import (
	"errors"
	"net/http"
	"strconv"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func variantEntityParams(c *gin.Context) (string, uint, bool) {
	entityType := c.Query("entity_type")
	entityID, err := strconv.ParseUint(c.Query("entity_id"), 10, 64)
	if err != nil || entityID == 0 || entityType == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "entity_type and entity_id are required"})
		return "", 0, false
	}
	return entityType, uint(entityID), true
}

func variantError(c *gin.Context, err error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "variant or entity not found"})
		return
	}
	c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
}

func (s *Service) HandleListAssetVariants(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	entityType, entityID, ok := variantEntityParams(c)
	if !ok {
		return
	}
	rows, err := s.AssetVariants.List(p.ID, entityType, entityID)
	if err != nil {
		variantError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"variants": rows})
}

func (s *Service) HandleRegisterAssetVariant(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	var req struct {
		EntityType string `json:"entity_type" binding:"required"`
		EntityID   uint   `json:"entity_id" binding:"required"`
		File       string `json:"file" binding:"required"`
		Prompt     string `json:"prompt"`
		Provenance string `json:"provenance"`
		Selected   bool   `json:"selected"`
		Favorite   bool   `json:"favorite"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	row, err := s.AssetVariants.Register(models.AssetVariant{ProjectID: p.ID, EntityType: req.EntityType, EntityID: req.EntityID, File: req.File, Prompt: req.Prompt, Provenance: req.Provenance, Selected: req.Selected, Favorite: req.Favorite})
	if err != nil {
		variantError(c, err)
		return
	}
	c.JSON(http.StatusCreated, row)
}

func (s *Service) HandleSelectAssetVariant(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	id, err := strconv.ParseUint(c.Param("vid"), 10, 64)
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid variant id"})
		return
	}
	row, err := s.AssetVariants.Select(p.ID, uint(id))
	if err != nil {
		variantError(c, err)
		return
	}
	c.JSON(http.StatusOK, row)
}

func (s *Service) HandleFavoriteAssetVariant(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	id, err := strconv.ParseUint(c.Param("vid"), 10, 64)
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid variant id"})
		return
	}
	var req struct {
		Favorite bool `json:"favorite"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "invalid request"})
		return
	}
	row, err := s.AssetVariants.SetFavorite(p.ID, uint(id), req.Favorite)
	if err != nil {
		variantError(c, err)
		return
	}
	c.JSON(http.StatusOK, row)
}

func (s *Service) HandleDeleteAssetVariant(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	id, err := strconv.ParseUint(c.Param("vid"), 10, 64)
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid variant id"})
		return
	}
	if err := s.AssetVariants.Delete(p.ID, uint(id)); err != nil {
		variantError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
