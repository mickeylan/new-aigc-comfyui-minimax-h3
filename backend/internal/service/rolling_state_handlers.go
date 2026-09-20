package service

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
)

func (s *Service) HandleGetBatchSnapshot(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	bid, ok := batchIDParam(c)
	if !ok {
		return
	}
	v, err := s.BatchPlanning.GetSnapshot(p.ID, bid)
	if err != nil {
		c.JSON(404, gin.H{"error": "snapshot not found"})
		return
	}
	c.JSON(200, v)
}
func (s *Service) HandleGenerateBatchSnapshot(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	bid, ok := batchIDParam(c)
	if !ok {
		return
	}
	v, err := s.BatchPlanning.GenerateSnapshot(p.ID, bid)
	if err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, v)
}
func (s *Service) HandleSaveBatchSnapshot(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	bid, ok := batchIDParam(c)
	if !ok {
		return
	}
	var req RollingStateInput
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "invalid request"})
		return
	}
	v, err := s.BatchPlanning.SaveSnapshot(p.ID, bid, req)
	if err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, v)
}
func (s *Service) HandleReviewBatchSnapshot(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	bid, ok := batchIDParam(c)
	if !ok {
		return
	}
	var req struct {
		Approve        bool   `json:"approve"`
		OverrideReason string `json:"override_reason"`
	}
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "invalid request"})
		return
	}
	v, err := s.BatchPlanning.ReviewSnapshot(p.ID, bid, req.Approve, req.OverrideReason)
	if err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, v)
}
func (s *Service) HandleListStoryClues(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	rows, err := s.BatchPlanning.ListClues(p.ID)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"clues": rows})
}
func (s *Service) HandleTransitionStoryClue(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	cid, e := strconv.ParseUint(c.Param("cid"), 10, 64)
	if e != nil {
		c.JSON(400, gin.H{"error": "invalid clue id"})
		return
	}
	var req struct {
		Status   string `json:"status"`
		Episode  int    `json:"episode"`
		Override bool   `json:"override"`
	}
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "invalid request"})
		return
	}
	v, err := s.BatchPlanning.TransitionClue(p.ID, uint(cid), req.Status, req.Episode, req.Override)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, v)
}
