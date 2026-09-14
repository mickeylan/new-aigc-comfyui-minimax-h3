package service

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func parseUintParam(c *gin.Context, name string) (uint, bool) {
	n, err := strconv.ParseUint(c.Param(name), 10, 64)
	if err != nil || n == 0 {
		c.JSON(400, gin.H{"error": "invalid " + name})
		return 0, false
	}
	return uint(n), true
}

func (s *Service) HandleAnalyzeNovelChapters(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	var req struct {
		ChapterIDs []uint `json:"chapter_ids"`
	}
	if c.Request.ContentLength > 0 && c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "invalid request"})
		return
	}
	job, err := s.NovelAnalysis.AnalyzeChapters(p.ID, req.ChapterIDs)
	if err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, job)
}

func (s *Service) HandleRetryNovelChapter(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	cid, ok := parseUintParam(c, "cid")
	if !ok {
		return
	}
	if _, err := s.Novel.GetChapter(p.ID, cid); err != nil {
		c.JSON(404, gin.H{"error": "chapter not found"})
		return
	}
	job, err := s.NovelAnalysis.AnalyzeChapters(p.ID, []uint{cid})
	if err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, job)
}

func (s *Service) HandleGenerateNovelArcs(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	var req struct {
		GroupSize int `json:"group_size"`
	}
	if c.Request.ContentLength > 0 {
		_ = c.ShouldBindJSON(&req)
	}
	arcs, job, err := s.NovelAnalysis.GenerateArcs(p.ID, req.GroupSize)
	if err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"arcs": arcs, "job": job})
}
func (s *Service) HandleListNovelArcs(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	rows, err := s.NovelAnalysis.ListArcs(p.ID)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"arcs": rows})
}
func (s *Service) HandleListNovelAliases(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	rows, err := s.NovelAnalysis.ListAliases(p.ID)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"aliases": rows})
}
func (s *Service) HandleUpdateNovelAlias(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	id, ok := parseUintParam(c, "aid")
	if !ok {
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "invalid request"})
		return
	}
	row, err := s.NovelAnalysis.SetAliasStatus(p.ID, id, req.Status)
	if err != nil {
		c.JSON(404, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, row)
}

func (s *Service) HandleGetStoryBible(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	b, err := s.NovelAnalysis.GetBible(p.ID)
	if err == gorm.ErrRecordNotFound {
		c.JSON(404, gin.H{"error": "story bible not found"})
		return
	}
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, b)
}
func (s *Service) HandleGenerateStoryBible(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	b, j, err := s.NovelAnalysis.GenerateBible(p.ID)
	if err != nil {
		c.JSON(409, gin.H{"error": err.Error(), "job": j})
		return
	}
	c.JSON(200, gin.H{"story_bible": b, "job": j})
}
func (s *Service) HandleUpdateStoryBible(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	var req map[string]any
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "invalid request"})
		return
	}
	b, err := s.NovelAnalysis.UpdateBible(p.ID, req)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, b)
}
func (s *Service) HandleApproveStoryBible(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	b, err := s.NovelAnalysis.ApproveBible(p.ID)
	if err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, b)
}
func (s *Service) HandleListNovelJobs(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	j, err := s.NovelAnalysis.ListJobs(p.ID)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"jobs": j})
}
func (s *Service) HandleRetryNovelJob(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	id, ok := parseUintParam(c, "jid")
	if !ok {
		return
	}
	j, err := s.NovelAnalysis.RetryJob(p.ID, id)
	if err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, j)
}
func (s *Service) HandleCancelNovelJob(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	id, ok := parseUintParam(c, "jid")
	if !ok {
		return
	}
	if err := s.NovelAnalysis.CancelJob(p.ID, id); err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}
