package service

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"comfyui-console/internal/models"
)

// ---------- 小说项目与章节管理 ----------

// HandleCreateNovelProject 创建小说改编项目
func (s *Service) HandleCreateNovelProject(c *gin.Context) {
	var req struct {
		Title string `json:"title"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	if req.Title == "" {
		req.Title = "未命名小说项目"
	}

	p := models.Project{
		Title:        req.Title,
		SourceType:   models.ProjectSourceNovel,
		Status:       "draft",
		AspectRatio:  "16:9",
		ImportStatus: models.NovelImportPending,
	}
	if err := s.DB.Create(&p).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, p)
}

const maxNovelUploadBytes int64 = 20 << 20

// HandleUploadNovel 上传小说文件（TXT/Markdown）
func (s *Service) HandleUploadNovel(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}

	if p.SourceType != models.ProjectSourceNovel {
		c.JSON(400, gin.H{"error": "项目不是小说改编类型"})
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxNovelUploadBytes+uploadMultipartOverhead)
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(400, gin.H{"error": "file required: " + err.Error()})
		return
	}
	defer file.Close()

	data, err := readBoundedUpload(file, maxNovelUploadBytes)
	if err != nil {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": err.Error()})
		return
	}
	if _, _, err := s.Novel.ValidateNovel(header.Filename, data); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ext := strings.ToLower(filepath.Ext(header.Filename))
	storedPath := ""
	fileHash := s.Novel.ComputeFileHash(data)
	filename := fmt.Sprintf("novel_%d_%s%s", p.ID, fileHash[:12], ext)
	if s.Upload != nil {
		path, _, err := s.Upload.SaveFile(fmt.Sprintf("%d", p.ID), "novel", filename, data)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		storedPath = filepath.Base(path)
	}

	// Chapters and authoritative source path are published in the same transaction.
	updated, err := s.Novel.ImportNovelWithPath(p.ID, header.Filename, data, storedPath)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	// 获取章节列表
	chapters, _ := s.Novel.ListChapters(p.ID)

	c.JSON(200, gin.H{
		"project":    updated,
		"chapters":   chapters,
		"word_count": updated.NovelWordCount,
		"file_hash":  updated.NovelFileHash,
		"message":    fmt.Sprintf("小说已导入，共识别 %d 个章节", len(chapters)),
	})
}

// HandleGetNovelImportStatus 获取小说导入状态
func (s *Service) HandleGetNovelImportStatus(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}

	if p.SourceType != models.ProjectSourceNovel {
		c.JSON(400, gin.H{"error": "项目不是小说改编类型"})
		return
	}

	chapters, _ := s.Novel.ListChapters(p.ID)
	c.JSON(200, gin.H{
		"project":       p,
		"import_status": p.ImportStatus,
		"import_error":  p.ImportError,
		"word_count":    p.NovelWordCount,
		"chapter_count": len(chapters),
	})
}

// HandleListChapters 获取章节列表
func (s *Service) HandleListChapters(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}

	if p.SourceType != models.ProjectSourceNovel {
		c.JSON(400, gin.H{"error": "项目不是小说改编类型"})
		return
	}

	chapters, err := s.Novel.ListChapters(p.ID)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{
		"chapters": chapters,
		"total":    len(chapters),
	})
}

// HandleGetChapter 获取章节详情
func (s *Service) HandleGetChapter(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}

	if p.SourceType != models.ProjectSourceNovel {
		c.JSON(400, gin.H{"error": "项目不是小说改编类型"})
		return
	}

	cid, err := strconv.Atoi(c.Param("cid"))
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid chapter id"})
		return
	}

	chapter, err := s.Novel.GetChapter(p.ID, uint(cid))
	if err != nil {
		c.JSON(404, gin.H{"error": "chapter not found"})
		return
	}

	c.JSON(200, chapter)
}

// HandleUpdateChapter 更新章节（标题/顺序）
func (s *Service) HandleUpdateChapter(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}

	if p.SourceType != models.ProjectSourceNovel {
		c.JSON(400, gin.H{"error": "项目不是小说改编类型"})
		return
	}

	cid, err := strconv.Atoi(c.Param("cid"))
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid chapter id"})
		return
	}

	chapter, err := s.Novel.GetChapter(p.ID, uint(cid))
	if err != nil {
		c.JSON(404, gin.H{"error": "chapter not found"})
		return
	}

	var req struct {
		Title string `json:"title"`
		Order int    `json:"order"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}

	if req.Title != "" && req.Title != chapter.Title {
		if err := s.Novel.UpdateChapterTitle(chapter, req.Title); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
	}

	if req.Order > 0 && req.Order != chapter.Order {
		if err := s.Novel.UpdateChapterOrder(chapter, req.Order); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
	}

	// 刷新章节数据
	updated, _ := s.Novel.GetChapter(p.ID, uint(cid))
	c.JSON(200, updated)
}

// HandleReorderChapters 批量更新章节顺序
func (s *Service) HandleReorderChapters(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}

	if p.SourceType != models.ProjectSourceNovel {
		c.JSON(400, gin.H{"error": "项目不是小说改编类型"})
		return
	}

	var req struct {
		Orders map[uint]int `json:"orders"` // chapter_id -> new_order
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}

	if len(req.Orders) == 0 {
		c.JSON(400, gin.H{"error": "orders 不能为空"})
		return
	}

	if err := s.Novel.ReorderChapters(p.ID, req.Orders); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	chapters, _ := s.Novel.ListChapters(p.ID)
	c.JSON(200, gin.H{
		"ok":       true,
		"chapters": chapters,
		"message":  fmt.Sprintf("已更新 %d 个章节的顺序", len(req.Orders)),
	})
}

// HandleSplitChapter 拆分章节
func (s *Service) HandleSplitChapter(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}

	if p.SourceType != models.ProjectSourceNovel {
		c.JSON(400, gin.H{"error": "项目不是小说改编类型"})
		return
	}

	cid, err := strconv.Atoi(c.Param("cid"))
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid chapter id"})
		return
	}

	chapter, err := s.Novel.GetChapter(p.ID, uint(cid))
	if err != nil {
		c.JSON(404, gin.H{"error": "chapter not found"})
		return
	}

	var req struct {
		SplitPoints []int `json:"split_points"` // 拆分点位置（字符索引）
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}

	if len(req.SplitPoints) < 1 {
		c.JSON(400, gin.H{"error": "至少需要一个拆分点"})
		return
	}

	task, newChapters, err := s.Novel.SplitChapter(chapter, req.SplitPoints)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{
		"task":         task,
		"new_chapters": newChapters,
		"message":      fmt.Sprintf("已将章节「%s」拆分为 %d 个新章节", chapter.Title, len(newChapters)),
	})
}

// HandleMergeChapters 合并章节
func (s *Service) HandleMergeChapters(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}

	if p.SourceType != models.ProjectSourceNovel {
		c.JSON(400, gin.H{"error": "项目不是小说改编类型"})
		return
	}

	var req struct {
		ChapterIDs []uint `json:"chapter_ids"`
		NewTitle   string `json:"new_title"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}

	if len(req.ChapterIDs) < 2 {
		c.JSON(400, gin.H{"error": "至少需要选择 2 个章节进行合并"})
		return
	}

	task, mergedChapter, err := s.Novel.MergeChapters(p.ID, req.ChapterIDs, req.NewTitle)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{
		"task":    task,
		"chapter": mergedChapter,
		"message": fmt.Sprintf("已将 %d 个章节合并为「%s」", len(req.ChapterIDs), mergedChapter.Title),
	})
}

// HandleListChapterTasks 获取章节任务列表
func (s *Service) HandleListChapterTasks(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}

	if p.SourceType != models.ProjectSourceNovel {
		c.JSON(400, gin.H{"error": "项目不是小说改编类型"})
		return
	}

	taskType := c.Query("type")
	tasks, err := s.Novel.ListChapterTasks(p.ID, taskType)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{
		"tasks": tasks,
		"total": len(tasks),
	})
}

// HandleChangeProjectSourceType 转换项目来源类型
func (s *Service) HandleChangeProjectSourceType(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}

	var req struct {
		SourceType string `json:"source_type"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}

	if req.SourceType != "outline" && req.SourceType != "novel" {
		c.JSON(400, gin.H{"error": "source_type 必须是 outline 或 novel"})
		return
	}

	// 检查是否有章节数据
	if req.SourceType == "outline" && p.SourceType == models.ProjectSourceNovel {
		var chapterCount int64
		s.DB.Model(&models.Chapter{}).Where("project_id = ?", p.ID).Count(&chapterCount)
		if chapterCount > 0 {
			c.JSON(400, gin.H{"error": "小说项目有章节数据，无法转换为梗概项目"})
			return
		}
	}

	updates := map[string]interface{}{
		"source_type": req.SourceType,
	}
	if req.SourceType == "novel" {
		updates["import_status"] = models.NovelImportPending
	}

	if err := s.DB.Model(p).Updates(updates).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	var updated models.Project
	s.DB.First(&updated, p.ID)
	c.JSON(200, updated)
}
