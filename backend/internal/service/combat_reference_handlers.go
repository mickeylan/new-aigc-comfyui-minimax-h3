package service

import (
	"net/http"
	"os"
	"path/filepath"
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

func (s *Service) HandleGetCombatShowcase(c *gin.Context) {
	if _, ok := s.combatReferenceProject(c); !ok {
		return
	}
	if s.CombatReferences == nil || s.Cfg == nil || strings.TrimSpace(s.Cfg.CombatReferenceDir) == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "战斗展示样本目录未配置"})
		return
	}
	relative, err := s.CombatReferences.Showcase(strings.TrimSpace(c.Param("scope")), strings.TrimSpace(c.Param("rid")))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	root, err := filepath.Abs(s.Cfg.CombatReferenceDir)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "战斗展示样本目录无效"})
		return
	}
	path := filepath.Join(root, filepath.FromSlash(relative))
	resolved, err := filepath.Abs(path)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "展示样本路径无效"})
		return
	}
	inside, err := filepath.Rel(root, resolved)
	if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "展示样本路径越界"})
		return
	}
	info, err := os.Stat(resolved)
	if err != nil || info.IsDir() {
		c.JSON(http.StatusNotFound, gin.H{"error": "展示样本不存在"})
		return
	}
	c.Header("Cache-Control", "public, max-age=3600")
	c.File(resolved)
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
