package service

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"comfyui-console/internal/models"
)

func (s *Service) combatReferenceProject(c *gin.Context) (*models.Project, bool) {
	projectID, ok := parseUintParam(c, "id")
	if !ok {
		return nil, false
	}
	var project models.Project
	if err := s.DB.First(&project, projectID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "项目不存在"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return nil, false
	}
	return &project, true
}

func (s *Service) HandleSearchCombatReferences(c *gin.Context) {
	if _, ok := s.combatReferenceProject(c); !ok {
		return
	}
	if s.CombatReferences == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "战斗资料库未加载"})
		return
	}
	result, err := s.CombatReferences.Search(c.Query("scope"), c.Query("query"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (s *Service) HandleGetCombatReference(c *gin.Context) {
	if _, ok := s.combatReferenceProject(c); !ok {
		return
	}
	if s.CombatReferences == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "战斗资料库未加载"})
		return
	}
	document, err := s.CombatReferences.Read(strings.TrimSpace(c.Param("scope")), strings.TrimSpace(c.Param("rid")))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, document)
}
