package service

import (
	"net/http"
	"strconv"

	"comfyui-console/internal/models"
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
		GroupSize    int `json:"group_size"`
		ChapterStart int `json:"chapter_start"`
		ChapterEnd   int `json:"chapter_end"`
	}
	if c.Request.ContentLength > 0 {
		_ = c.ShouldBindJSON(&req)
	}
	arcs, job, err := s.NovelAnalysis.GenerateArcsRange(p.ID, req.ChapterStart, req.ChapterEnd, req.GroupSize)
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
func (s *Service) HandleReviewNovelArc(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	arcID, ok := parseUintParam(c, "aid")
	if !ok {
		return
	}
	var req struct {
		Status string `json:"status"`
		Notes  string `json:"notes"`
	}
	if c.ShouldBindJSON(&req) != nil || (req.Status != "approved" && req.Status != "rejected") {
		c.JSON(400, gin.H{"error": "status must be approved or rejected"})
		return
	}
	updates := map[string]any{"review_status": req.Status, "reviewer_notes": req.Notes, "status": req.Status}
	result := s.DB.Model(&models.StoryArc{}).Where("id = ? AND project_id = ?", arcID, p.ID).Updates(updates)
	if result.Error != nil {
		c.JSON(500, gin.H{"error": result.Error.Error()})
		return
	}
	if result.RowsAffected != 1 {
		c.JSON(404, gin.H{"error": "story arc not found"})
		return
	}
	var arc models.StoryArc
	_ = s.DB.First(&arc, arcID).Error
	c.JSON(200, arc)
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
func (s *Service) HandleListStoryBibleChanges(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	rows, err := s.NovelAnalysis.ListBibleChanges(p.ID)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"changes": rows})
}

func (s *Service) HandleProposeStoryBibleChange(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	wid, ok := parseUintParam(c, "wid")
	if !ok {
		return
	}
	row, err := s.NovelAnalysis.ProposeBibleChange(p.ID, wid)
	if err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	c.JSON(201, row)
}

func (s *Service) HandleReviewStoryBibleChange(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	cid, ok := parseUintParam(c, "cid")
	if !ok {
		return
	}
	var req struct {
		Approve bool   `json:"approve"`
		Note    string `json:"note"`
	}
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "invalid request"})
		return
	}
	bible, err := s.NovelAnalysis.ReviewBibleChange(p.ID, cid, req.Approve, req.Note)
	if err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, bible)
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

// -------------------- Analysis Window Handlers --------------------

func (s *Service) HandleListAnalysisWindows(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	windows, err := s.NovelAnalysis.ListWindows(p.ID)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"windows": windows})
}

func (s *Service) HandleGetAnalysisWindow(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	wid, ok := parseUintParam(c, "wid")
	if !ok {
		return
	}
	window, err := s.NovelAnalysis.GetWindow(p.ID, wid)
	if err != nil {
		c.JSON(404, gin.H{"error": "window not found"})
		return
	}
	c.JSON(200, window)
}

func (s *Service) HandleCreateAnalysisWindow(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	var req struct {
		ChapterStart   int   `json:"chapter_start" binding:"required,min=1"`
		ChapterEnd     int   `json:"chapter_end" binding:"required,min=1"`
		ContextStart   int   `json:"context_start"`
		ContextEnd     int   `json:"context_end"`
		WindowSize     int   `json:"window_size"`
		PreviousWindow *uint `json:"previous_window_id"`
	}
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "invalid request"})
		return
	}
	if req.ChapterEnd < req.ChapterStart {
		c.JSON(400, gin.H{"error": "chapter_end must be >= chapter_start"})
		return
	}

	opts := WindowOptions{
		ChapterStart:   req.ChapterStart,
		ChapterEnd:     req.ChapterEnd,
		ContextStart:   req.ContextStart,
		ContextEnd:     req.ContextEnd,
		WindowSize:     req.WindowSize,
		PreviousWindow: req.PreviousWindow,
	}
	window, err := s.NovelAnalysis.CreateWindow(p.ID, opts)
	if err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	c.JSON(201, window)
}

func (s *Service) HandleGetWindowSuggestion(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	windowSize, _ := strconv.Atoi(c.DefaultQuery("window_size", "10"))
	suggestion, err := s.NovelAnalysis.GetWindowSuggestion(p.ID, windowSize)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, suggestion)
}

func (s *Service) HandleExtendAnalysisWindow(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	wid, ok := parseUintParam(c, "wid")
	if !ok {
		return
	}
	var req struct {
		ChapterEnd int `json:"chapter_end"`
	}
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "invalid request"})
		return
	}
	window, err := s.NovelAnalysis.ExtendWindow(p.ID, wid, req.ChapterEnd)
	if err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, window)
}

func (s *Service) HandleAnalyzeAnalysisWindow(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	wid, ok := parseUintParam(c, "wid")
	if !ok {
		return
	}
	window, err := s.NovelAnalysis.GetWindow(p.ID, wid)
	if err != nil {
		c.JSON(404, gin.H{"error": "window not found"})
		return
	}
	if window.Status == models.WindowStatusAnalysing {
		c.JSON(409, gin.H{"error": "window analysis is already running"})
		return
	}
	if window.Status == models.WindowStatusCommitted {
		c.JSON(409, gin.H{"error": "window is already committed"})
		return
	}
	if err := s.DB.Model(window).Update("status", models.WindowStatusAnalysing).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	go func(projectID, windowID uint) {
		if _, runErr := s.NovelAnalysis.AnalyzeWindow(projectID, windowID); runErr != nil {
			_ = s.DB.Model(&models.AnalysisWindow{}).Where("id = ? AND project_id = ?", windowID, projectID).Updates(map[string]any{"status": models.WindowStatusPending, "summary": runErr.Error()}).Error
		}
	}(p.ID, wid)
	c.JSON(http.StatusAccepted, gin.H{"ok": true, "window_id": wid})
}

func (s *Service) HandleGetWindowProgress(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	wid, ok := parseUintParam(c, "wid")
	if !ok {
		return
	}
	progress, err := s.NovelAnalysis.GetWindowProgress(p.ID, wid)
	if err != nil {
		c.JSON(404, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, progress)
}

func (s *Service) HandleGetWindowContext(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	wid, ok := parseUintParam(c, "wid")
	if !ok {
		return
	}
	chapters, err := s.NovelAnalysis.GetWindowContext(p.ID, wid)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"chapters": chapters})
}

func (s *Service) HandleApproveAnalysisWindow(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	wid, ok := parseUintParam(c, "wid")
	if !ok {
		return
	}
	window, err := s.NovelAnalysis.ApproveWindow(p.ID, wid)
	if err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, window)
}

func (s *Service) HandleBindAnalysisWindowBatch(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	wid, ok := parseUintParam(c, "wid")
	if !ok {
		return
	}
	var req struct {
		BatchID uint `json:"batch_id"`
	}
	if c.ShouldBindJSON(&req) != nil || req.BatchID == 0 {
		c.JSON(400, gin.H{"error": "batch_id is required"})
		return
	}
	window, err := s.NovelAnalysis.BindWindowBatch(p.ID, wid, req.BatchID)
	if err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, window)
}

func (s *Service) HandleCommitAnalysisWindow(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	wid, ok := parseUintParam(c, "wid")
	if !ok {
		return
	}
	window, err := s.NovelAnalysis.CommitWindow(p.ID, wid)
	if err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, window)
}

func (s *Service) HandleAdvanceToNextWindow(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	wid, ok := parseUintParam(c, "wid")
	if !ok {
		return
	}
	var req struct {
		WindowSize int `json:"window_size"`
	}
	_ = c.ShouldBindJSON(&req)
	if req.WindowSize <= 0 {
		req.WindowSize = 10
	}
	window, err := s.NovelAnalysis.AdvanceToNextWindow(p.ID, wid, req.WindowSize)
	if err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	c.JSON(201, window)
}
