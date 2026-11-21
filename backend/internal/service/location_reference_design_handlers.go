package service

import (
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// loadLocationAsset 只接受场景资产，其余类别直接 400，避免把道具四视图当场景设计对象。
func (s *Service) loadLocationAsset(c *gin.Context) (*models.Asset, bool) {
	a, ok := s.loadAsset(c)
	if !ok {
		return nil, false
	}
	if a.Kind != AssetKindLocation {
		c.JSON(400, gin.H{"error": "场景参考设计只用于场景资产"})
		return nil, false
	}
	return a, true
}

func (s *Service) HandleLocationDesignOptions(c *gin.Context) {
	a, ok := s.loadLocationAsset(c)
	if !ok {
		return
	}
	var files []models.UploadFile
	if err := s.DB.Where("task_id=? AND type=?", fmt.Sprint(a.ProjectID), "image").Order("id DESC").Find(&files).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	names := []string{}
	for _, f := range files {
		if _, err := safeFileSegment(f.Name, "reference file"); err == nil {
			names = append(names, f.Name)
		}
	}
	var designs []models.LocationReferenceDesign
	if err := s.DB.Preload("References", func(db *gorm.DB) *gorm.DB { return db.Order("position ASC") }).Where("project_id=? AND asset_id=?", a.ProjectID, a.ID).Order("id DESC").Limit(30).Find(&designs).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"engines": s.Projects.locationDesignEngines(), "files": names, "designs": designs})
}

func (s *Service) HandleUploadLocationDesignReference(c *gin.Context) {
	a, ok := s.loadLocationAsset(c)
	if !ok {
		return
	}
	if s.Upload == nil {
		c.JSON(503, gin.H{"error": "上传服务不可用"})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 26<<20)
	file, _, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(400, gin.H{"error": "请选择小于25MB的图片"})
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 25<<20+1))
	if err != nil || len(data) == 0 || len(data) > 25<<20 {
		c.JSON(400, gin.H{"error": "图片为空、读取失败或超过25MB"})
		return
	}
	ext := ""
	switch http.DetectContentType(data) {
	case "image/png":
		ext = ".png"
	case "image/jpeg":
		ext = ".jpg"
	case "image/webp":
		ext = ".webp"
	}
	if ext == "" {
		c.JSON(400, gin.H{"error": "仅支持PNG、JPEG、WebP图片"})
		return
	}
	name := fmt.Sprintf("loc_design_ref_%d_%d%s", a.ID, time.Now().UnixNano(), ext)
	path, _, err := s.Upload.SaveFile(fmt.Sprint(a.ProjectID), "image", name, data)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"file": filepath.Base(path)})
}

func (s *Service) HandlePreviewLocationDesign(c *gin.Context) {
	a, ok := s.loadLocationAsset(c)
	if !ok {
		return
	}
	var in locationDesignInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	row, err := s.Projects.PreviewLocationDesign(a.ProjectID, a.ID, in)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, row)
}

func (s *Service) HandleStartLocationDesign(c *gin.Context) {
	a, ok := s.loadLocationAsset(c)
	if !ok {
		return
	}
	var in struct {
		DesignID uint   `json:"design_id"`
		Prompt   string `json:"prompt"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	id, err := s.Projects.StartLocationDesign(a.ProjectID, a.ID, in.DesignID, strings.TrimSpace(in.Prompt))
	if err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"task_id": id})
}
