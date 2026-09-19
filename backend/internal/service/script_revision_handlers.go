package service

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func revisionIDParam(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("rid"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid revision id"})
		return 0, false
	}
	return uint(id), true
}

func (s *Service) HandleCreateScriptRevision(c *gin.Context) {
	project, ok := s.loadProject(c)
	if !ok {
		return
	}
	var req struct {
		EpisodeN int    `json:"episode_n"`
		Reason   string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	revision, err := s.ScriptRevisions.Create(project.ID, req.EpisodeN, req.Reason)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, revision)
}

func (s *Service) HandleListScriptRevisions(c *gin.Context) {
	project, ok := s.loadProject(c)
	if !ok {
		return
	}
	episodeN, _ := strconv.Atoi(c.Query("episode_n"))
	revisions, err := s.ScriptRevisions.List(project.ID, episodeN)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, revisions)
}

func (s *Service) HandleGetScriptRevision(c *gin.Context) {
	project, ok := s.loadProject(c)
	if !ok {
		return
	}
	revisionID, ok := revisionIDParam(c)
	if !ok {
		return
	}
	detail, err := s.ScriptRevisions.Get(project.ID, revisionID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "revision not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, detail)
}

func (s *Service) HandleRestoreScriptRevision(c *gin.Context) {
	project, ok := s.loadProject(c)
	if !ok {
		return
	}
	revisionID, ok := revisionIDParam(c)
	if !ok {
		return
	}
	safetyRevision, err := s.ScriptRevisions.Restore(project.ID, revisionID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "revision not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "safety_revision": safetyRevision})
}
