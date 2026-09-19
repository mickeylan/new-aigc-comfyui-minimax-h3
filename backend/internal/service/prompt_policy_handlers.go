package service

import (
	"net/http"
	"strconv"
	"strings"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func parseOptionalUintQuery(c *gin.Context, name string) (*uint, error) {
	value := strings.TrimSpace(c.Query(name))
	if value == "" {
		return nil, nil
	}
	parsed, err := strconv.ParseUint(value, 10, 32)
	if err != nil || parsed == 0 {
		return nil, strconv.ErrSyntax
	}
	id := uint(parsed)
	return &id, nil
}

func (s *Service) HandleListPromptPolicyOverrides(c *gin.Context) {
	project, ok := s.loadProject(c)
	if !ok {
		return
	}
	rows, err := NewPromptPolicyService(s.DB).List(project.ID, c.Query("policy_key"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, rows)
}

func (s *Service) HandleResolvePromptPolicy(c *gin.Context) {
	project, ok := s.loadProject(c)
	if !ok {
		return
	}
	episodeID, err1 := parseOptionalUintQuery(c, "episode_id")
	sceneID, err2 := parseOptionalUintQuery(c, "scene_id")
	shotID, err3 := parseOptionalUintQuery(c, "shot_id")
	if err1 != nil || err2 != nil || err3 != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid scope id"})
		return
	}
	policy, err := NewPromptPolicyService(s.DB).Resolve(PromptPolicyContext{ProjectID: project.ID, EpisodeID: episodeID, SceneID: sceneID, ShotID: shotID}, c.Query("policy_key"), c.Query("system_default"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, policy)
}

func (s *Service) HandleCreatePromptPolicyOverride(c *gin.Context) {
	project, ok := s.loadProject(c)
	if !ok {
		return
	}
	var req struct {
		EpisodeID *uint  `json:"episode_id"`
		SceneID   *uint  `json:"scene_id"`
		ShotID    *uint  `json:"shot_id"`
		PolicyKey string `json:"policy_key"`
		Content   string `json:"content"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	row, err := NewPromptPolicyService(s.DB).Create(&models.PromptPolicyOverride{ProjectID: project.ID, EpisodeID: req.EpisodeID, SceneID: req.SceneID, ShotID: req.ShotID, PolicyKey: req.PolicyKey, Content: req.Content})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, row)
}

func (s *Service) HandleUpdatePromptPolicyOverride(c *gin.Context) {
	project, ok := s.loadProject(c)
	if !ok {
		return
	}
	id, err := strconv.ParseUint(c.Param("poid"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid override id"})
		return
	}
	var req struct {
		Content string `json:"content"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	row, err := NewPromptPolicyService(s.DB).Update(project.ID, uint(id), req.Content)
	if err != nil {
		status := http.StatusBadRequest
		if err == gorm.ErrRecordNotFound {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, row)
}

func (s *Service) HandleDeletePromptPolicyOverride(c *gin.Context) {
	project, ok := s.loadProject(c)
	if !ok {
		return
	}
	id, err := strconv.ParseUint(c.Param("poid"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid override id"})
		return
	}
	if err := NewPromptPolicyService(s.DB).Delete(project.ID, uint(id)); err != nil {
		status := http.StatusInternalServerError
		if err == gorm.ErrRecordNotFound {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
