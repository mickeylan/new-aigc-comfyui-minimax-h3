package service

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
)

func (s *Service) HandleListAgentTools(c *gin.Context) {
	if s.AgentTools == nil || s.Cfg == nil || !s.Cfg.Extensibility.EnableAgentTools {
		c.JSON(http.StatusNotFound, gin.H{"error": "agent tools are disabled"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": s.AgentTools.List()})
}

func (s *Service) HandleInvokeAgentTool(c *gin.Context) {
	if s.AgentTools == nil || s.Cfg == nil || !s.Cfg.Extensibility.EnableAgentTools {
		c.JSON(http.StatusNotFound, gin.H{"error": "agent tools are disabled"})
		return
	}
	var body struct {
		ProjectID *uint           `json:"project_id"`
		Input     json.RawMessage `json:"input"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	if body.ProjectID != nil {
		var count int64
		if err := s.DB.Table("projects").Where("id = ?", *body.ProjectID).Count(&count).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if count != 1 {
			c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
			return
		}
	}
	if len(body.Input) == 0 {
		body.Input = json.RawMessage(`{}`)
	}
	result, err := s.AgentTools.Invoke(c.Request.Context(), c.Param("code"), AgentToolRequest{ProjectID: body.ProjectID, Input: body.Input})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"result": result})
}
