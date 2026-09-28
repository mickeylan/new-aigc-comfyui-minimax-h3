package service

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

func (s *Service) HandleListGeneratedMedia(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	rows, err := s.GeneratedMedia.List(p.ID, c.Query("entity_type"), c.Query("media_type"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": rows, "count": len(rows)})
}

func (s *Service) HandleSelectGeneratedMedia(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	id, err := strconv.ParseUint(c.Param("mid"), 10, 64)
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid media id"})
		return
	}
	if err := s.GeneratedMedia.Select(p.ID, c.Param("family"), uint(id)); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Service) HandleFavoriteGeneratedMedia(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	id, err := strconv.ParseUint(c.Param("mid"), 10, 64)
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid media id"})
		return
	}
	var req struct {
		Favorite bool `json:"favorite"`
	}
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "invalid request"})
		return
	}
	if err := s.GeneratedMedia.Favorite(p.ID, c.Param("family"), uint(id), req.Favorite); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Service) HandleDeleteGeneratedMedia(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	id, err := strconv.ParseUint(c.Param("mid"), 10, 64)
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid media id"})
		return
	}
	if err := s.GeneratedMedia.Delete(p.ID, c.Param("family"), uint(id)); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Service) HandleBulkDeleteGeneratedMedia(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	var req struct {
		Items []struct {
			Family string `json:"family"`
			ID     uint   `json:"id"`
		} `json:"items"`
	}
	if c.ShouldBindJSON(&req) != nil || len(req.Items) == 0 {
		c.JSON(400, gin.H{"error": "items are required"})
		return
	}
	if err := s.GeneratedMedia.BulkDelete(p.ID, req.Items); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "deleted": len(req.Items)})
}
