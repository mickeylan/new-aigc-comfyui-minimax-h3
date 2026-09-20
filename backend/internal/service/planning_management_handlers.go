package service

import (
	"fmt"
	"net/http"
	"strings"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func arcLocked(tx *gorm.DB, arc *models.StoryArc) bool {
	if arc.BatchID == nil {
		return false
	}
	var b models.PlanningBatch
	if tx.First(&b, *arc.BatchID).Error != nil {
		return false
	}
	return b.Status == BatchStatusApproved || b.Status == BatchStatusProduced
}
func staleArcDependents(tx *gorm.DB, projectID uint) error {
	if err := tx.Model(&models.StoryBible{}).Where("project_id = ?", projectID).Update("status", "stale").Error; err != nil {
		return err
	}
	return tx.Model(&models.PlanningBatch{}).Where("project_id = ? AND status IN ?", projectID, []string{BatchStatusDraft, BatchStatusReview}).Updates(map[string]any{"status": BatchStatusDraft, "summary": "", "generation_json": "", "review_status": ReviewStatusPending}).Error
}

func (s *Service) HandleCreateNovelArc(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	var req models.StoryArc
	if c.ShouldBindJSON(&req) != nil || req.ChapterStart < 1 || req.ChapterEnd < req.ChapterStart || strings.TrimSpace(req.Title) == "" {
		c.JSON(400, gin.H{"error": "invalid arc"})
		return
	}
	var max int
	s.DB.Model(&models.StoryArc{}).Where("project_id = ?", p.ID).Select("COALESCE(MAX(arc_no),0)").Scan(&max)
	req.ID = 0
	req.ProjectID = p.ID
	req.ArcNo = max + 1
	req.Status = "draft"
	req.ReviewStatus = ReviewStatusPending
	req.Version = 1
	if err := s.DB.Create(&req).Error; err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	c.JSON(201, req)
}
func (s *Service) HandleUpdateNovelArc(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	aid, ok := parseUintParam(c, "aid")
	if !ok {
		return
	}
	var arc models.StoryArc
	if s.DB.Where("id = ? AND project_id = ?", aid, p.ID).First(&arc).Error != nil {
		c.JSON(404, gin.H{"error": "arc not found"})
		return
	}
	if arcLocked(s.DB, &arc) {
		c.JSON(409, gin.H{"error": "已审核/生产批次使用的故事弧不可修改"})
		return
	}
	var req struct {
		Title        string `json:"title"`
		Summary      string `json:"summary"`
		ChapterStart int    `json:"chapter_start"`
		ChapterEnd   int    `json:"chapter_end"`
	}
	if c.ShouldBindJSON(&req) != nil || req.ChapterStart < 1 || req.ChapterEnd < req.ChapterStart {
		c.JSON(400, gin.H{"error": "invalid arc"})
		return
	}
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&arc).Updates(map[string]any{"title": req.Title, "summary": req.Summary, "chapter_start": req.ChapterStart, "chapter_end": req.ChapterEnd, "status": "draft", "review_status": ReviewStatusPending, "version": gorm.Expr("version + 1")}).Error; err != nil {
			return err
		}
		return staleArcDependents(tx, p.ID)
	})
	if err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	s.DB.First(&arc, aid)
	c.JSON(200, arc)
}
func (s *Service) HandleDeleteNovelArc(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	aid, ok := parseUintParam(c, "aid")
	if !ok {
		return
	}
	var arc models.StoryArc
	if s.DB.Where("id = ? AND project_id = ?", aid, p.ID).First(&arc).Error != nil {
		c.JSON(404, gin.H{"error": "arc not found"})
		return
	}
	if arcLocked(s.DB, &arc) {
		c.JSON(409, gin.H{"error": "故事弧已进入审核/生产批次"})
		return
	}
	if err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&arc).Error; err != nil {
			return err
		}
		return staleArcDependents(tx, p.ID)
	}); err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}
func (s *Service) HandleSplitNovelArc(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	aid, ok := parseUintParam(c, "aid")
	if !ok {
		return
	}
	var req struct {
		SplitChapter int    `json:"split_chapter"`
		SecondTitle  string `json:"second_title"`
	}
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "invalid request"})
		return
	}
	var arc models.StoryArc
	if s.DB.Where("id = ? AND project_id = ?", aid, p.ID).First(&arc).Error != nil {
		c.JSON(404, gin.H{"error": "arc not found"})
		return
	}
	if arcLocked(s.DB, &arc) || req.SplitChapter <= arc.ChapterStart || req.SplitChapter > arc.ChapterEnd {
		c.JSON(409, gin.H{"error": "故事弧不可在此处拆分"})
		return
	}
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.StoryArc{}).Where("project_id = ? AND arc_no > ?", p.ID, arc.ArcNo).Update("arc_no", gorm.Expr("arc_no + 100000")).Error; err != nil {
			return err
		}
		if err := tx.Model(&arc).Updates(map[string]any{"chapter_end": req.SplitChapter - 1, "review_status": ReviewStatusPending, "status": "draft", "version": gorm.Expr("version + 1")}).Error; err != nil {
			return err
		}
		title := req.SecondTitle
		if title == "" {
			title = arc.Title + "（下）"
		}
		second := models.StoryArc{ProjectID: p.ID, ArcNo: arc.ArcNo + 1, Title: title, ChapterStart: req.SplitChapter, ChapterEnd: arc.ChapterEnd, Summary: arc.Summary, Status: "draft", ReviewStatus: ReviewStatusPending, Version: 1}
		if err := tx.Create(&second).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.StoryArc{}).Where("project_id = ? AND arc_no > ?", p.ID, 100000).Update("arc_no", gorm.Expr("arc_no - 99999")).Error; err != nil {
			return err
		}
		return staleArcDependents(tx, p.ID)
	})
	if err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}
func (s *Service) HandleMergeNovelArcs(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	var req struct {
		ArcIDs []uint `json:"arc_ids"`
		Title  string `json:"title"`
	}
	if c.ShouldBindJSON(&req) != nil || len(req.ArcIDs) < 2 {
		c.JSON(400, gin.H{"error": "at least two arcs required"})
		return
	}
	var arcs []models.StoryArc
	if s.DB.Where("project_id = ? AND id IN ?", p.ID, req.ArcIDs).Order("arc_no").Find(&arcs).Error != nil || len(arcs) != len(req.ArcIDs) {
		c.JSON(404, gin.H{"error": "arc not found"})
		return
	}
	for i, a := range arcs {
		if arcLocked(s.DB, &a) || (i > 0 && a.ChapterStart != arcs[i-1].ChapterEnd+1) {
			c.JSON(409, gin.H{"error": "只能合并相邻且未锁定故事弧"})
			return
		}
	}
	title := req.Title
	if title == "" {
		title = arcs[0].Title
	}
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&arcs[0]).Updates(map[string]any{"title": title, "chapter_end": arcs[len(arcs)-1].ChapterEnd, "status": "draft", "review_status": ReviewStatusPending, "version": gorm.Expr("version + 1")}).Error; err != nil {
			return err
		}
		ids := req.ArcIDs[1:]
		if err := tx.Where("id IN ?", ids).Delete(&models.StoryArc{}).Error; err != nil {
			return err
		}
		return staleArcDependents(tx, p.ID)
	})
	if err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}
func (s *Service) HandleReorderNovelArcs(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	var req struct {
		ArcIDs []uint `json:"arc_ids"`
	}
	if c.ShouldBindJSON(&req) != nil || len(req.ArcIDs) == 0 {
		c.JSON(400, gin.H{"error": "arc_ids required"})
		return
	}
	var count int64
	s.DB.Model(&models.StoryArc{}).Where("project_id = ?", p.ID).Count(&count)
	if int(count) != len(req.ArcIDs) {
		c.JSON(409, gin.H{"error": "必须提交完整故事弧顺序"})
		return
	}
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		for i, id := range req.ArcIDs {
			if err := tx.Model(&models.StoryArc{}).Where("id = ? AND project_id = ?", id, p.ID).Update("arc_no", 100000+i).Error; err != nil {
				return err
			}
		}
		for i, id := range req.ArcIDs {
			if err := tx.Model(&models.StoryArc{}).Where("id = ? AND project_id = ?", id, p.ID).Update("arc_no", i+1).Error; err != nil {
				return err
			}
		}
		return staleArcDependents(tx, p.ID)
	})
	if err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (s *Service) HandleUpdatePlanningTarget(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	var req struct {
		TotalEpisodes int `json:"total_episodes"`
	}
	if c.ShouldBindJSON(&req) != nil || req.TotalEpisodes < 1 || req.TotalEpisodes > 100000 {
		c.JSON(400, gin.H{"error": "total_episodes must be 1-100000"})
		return
	}
	var maxEnd int
	s.DB.Model(&models.PlanningBatch{}).Where("project_id = ? AND status <> ?", p.ID, BatchStatusCancelled).Select("COALESCE(MAX(episode_end),0)").Scan(&maxEnd)
	if req.TotalEpisodes < maxEnd {
		c.JSON(409, gin.H{"error": fmt.Sprintf("总集数不能小于已规划的第%d集", maxEnd)})
		return
	}
	if err := s.DB.Model(p).Update("episodes", req.TotalEpisodes).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"total_episodes": req.TotalEpisodes})
}
