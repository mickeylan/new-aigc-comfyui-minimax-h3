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

func (s *Service) HandleFaceDesignOptions(c *gin.Context) {
	ch, ok := s.loadCharacter(c)
	if !ok {
		return
	}
	var files []models.UploadFile
	if err := s.DB.Where("task_id=? AND type=?", fmt.Sprint(ch.ProjectID), "image").Order("id DESC").Find(&files).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	// Only registered safe project images are offered, including existing character
	// portraits. Selection stores the concrete file, never a mutable character ID.
	names := []string{}
	for _, f := range files {
		if _, err := safeFileSegment(f.Name, "reference file"); err == nil {
			names = append(names, f.Name)
		}
	}
	var designs []models.CharacterFaceDesign
	if err := s.DB.Preload("References", func(db *gorm.DB) *gorm.DB { return db.Order("position ASC") }).Where("project_id=? AND character_id=?", ch.ProjectID, ch.ID).Order("id DESC").Limit(30).Find(&designs).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"engines": s.Projects.faceDesignEngines(), "files": names, "designs": designs})
}

func (s *Service) HandleUploadFaceDesignReference(c *gin.Context) {
	ch, ok := s.loadCharacter(c)
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
	name := fmt.Sprintf("face_ref_%d_%d%s", ch.ID, time.Now().UnixNano(), ext)
	path, _, err := s.Upload.SaveFile(fmt.Sprint(ch.ProjectID), "image", name, data)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"file": filepath.Base(path)})
}

func (s *Service) HandlePreviewFaceDesign(c *gin.Context) {
	ch, ok := s.loadCharacter(c)
	if !ok {
		return
	}
	var in faceDesignInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	row, err := s.Projects.PreviewFaceDesign(ch.ProjectID, ch.ID, in)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, row)
}

func (s *Service) HandleStartFaceDesign(c *gin.Context) {
	ch, ok := s.loadCharacter(c)
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
	id, err := s.Projects.StartFaceDesign(ch.ProjectID, ch.ID, in.DesignID, strings.TrimSpace(in.Prompt))
	if err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"task_id": id})
}
