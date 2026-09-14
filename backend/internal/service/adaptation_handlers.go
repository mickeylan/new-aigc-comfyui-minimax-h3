package service

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"comfyui-console/internal/models"
)

func (s *Service) HandleGetAdaptationStrategy(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	v, err := s.Adaptations.GetStrategy(p.ID)
	if err == gorm.ErrRecordNotFound {
		c.JSON(404, gin.H{"error": "strategy not found"})
		return
	}
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, v)
}
func (s *Service) HandleSaveAdaptationStrategy(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	var req models.AdaptationStrategy
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "invalid request"})
		return
	}
	v, err := s.Adaptations.SaveStrategy(p.ID, req)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, v)
}
func (s *Service) HandleListAdaptations(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	v, err := s.Adaptations.List(p.ID)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"adaptations": v})
}
func (s *Service) HandleGenerateAdaptations(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	v, err := s.Adaptations.Generate(p.ID)
	if err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"adaptations": v})
}
func episodeParam(c *gin.Context) (int, bool) {
	n, e := strconv.Atoi(c.Param("episode"))
	if e != nil || n < 1 {
		c.JSON(400, gin.H{"error": "invalid episode"})
		return 0, false
	}
	return n, true
}
func (s *Service) HandleUpdateAdaptation(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	n, ok := episodeParam(c)
	if !ok {
		return
	}
	var req map[string]any
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "invalid request"})
		return
	}
	v, err := s.Adaptations.Update(p.ID, n, req)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, v)
}
func (s *Service) HandleApproveAdaptations(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	var req struct {
		Episodes []int `json:"episodes"`
	}
	if c.Param("episode") != "" {
		n, o := episodeParam(c)
		if !o {
			return
		}
		req.Episodes = []int{n}
	} else if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "invalid request"})
		return
	}
	if err := s.Adaptations.Approve(p.ID, req.Episodes); err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}
func (s *Service) HandleGenerateAdaptationScript(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	n, ok := episodeParam(c)
	if !ok {
		return
	}
	v, err := s.Adaptations.GenerateScript(p.ID, n)
	if err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, v)
}
func (s *Service) HandleBatchGenerateAdaptationScripts(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	var req struct {
		Episodes []int `json:"episodes"`
	}
	if c.ShouldBindJSON(&req) != nil || len(req.Episodes) == 0 {
		c.JSON(400, gin.H{"error": "episodes are required"})
		return
	}
	rows := make([]*models.EpisodeAdaptation, 0, len(req.Episodes))
	for _, n := range req.Episodes {
		v, err := s.Adaptations.GenerateScript(p.ID, n)
		if err != nil {
			c.JSON(409, gin.H{"error": err.Error(), "completed": rows})
			return
		}
		rows = append(rows, v)
	}
	c.JSON(200, gin.H{"adaptations": rows})
}
func (s *Service) HandleReviewAdaptation(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	n, ok := episodeParam(c)
	if !ok {
		return
	}
	var req struct {
		OverrideReason string `json:"override_reason"`
	}
	if c.Request.ContentLength > 0 && c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "invalid request"})
		return
	}
	v, err := s.Adaptations.Review(p.ID, n, req.OverrideReason)
	if err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, v)
}
func (s *Service) HandleBatchReviewAdaptations(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	var req struct {
		Episodes []int `json:"episodes"`
	}
	if c.ShouldBindJSON(&req) != nil || len(req.Episodes) == 0 {
		c.JSON(400, gin.H{"error": "episodes are required"})
		return
	}
	rows := make([]*models.EpisodeAdaptation, 0, len(req.Episodes))
	for _, n := range req.Episodes {
		v, err := s.Adaptations.Review(p.ID, n, "")
		if err != nil {
			c.JSON(409, gin.H{"error": err.Error(), "completed": rows})
			return
		}
		rows = append(rows, v)
	}
	c.JSON(200, gin.H{"adaptations": rows})
}
func (s *Service) HandleAdaptationContext(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	n, ok := episodeParam(c)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(c.Query("max_chars"))
	v, err := s.Adaptations.BuildContext(p.ID, n, limit)
	if err != nil {
		c.JSON(404, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, v)
}
func (s *Service) HandleNovelUsage(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	v, err := s.Adaptations.Usage(p.ID)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, v)
}
