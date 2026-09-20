package service

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

func batchIDParam(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("bid"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid batch id"})
		return 0, false
	}
	return uint(id), true
}

func (s *Service) HandleListPlanningBatches(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	rows, err := s.BatchPlanning.ListBatches(p.ID)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"batches": rows, "total_episode_target": p.Episodes, "batch_min": DefaultBatchSizeMin, "batch_max": DefaultBatchSizeMax})
}
func (s *Service) HandleCreatePlanningBatch(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	var req BatchCreateInput
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "invalid request"})
		return
	}
	v, err := s.BatchPlanning.CreateBatch(p.ID, req)
	if err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	c.JSON(201, v)
}
func (s *Service) HandleGetPlanningBatch(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	id, ok := batchIDParam(c)
	if !ok {
		return
	}
	v, err := s.BatchPlanning.Detail(p.ID, id)
	if err != nil {
		c.JSON(404, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, v)
}
func (s *Service) HandleGeneratePlanningBatch(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	id, ok := batchIDParam(c)
	if !ok {
		return
	}
	v, err := s.BatchPlanning.GenerateBatchDraft(p.ID, id)
	if err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, v)
}
func (s *Service) HandleReviewPlanningBatch(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	id, ok := batchIDParam(c)
	if !ok {
		return
	}
	var req struct {
		Action string `json:"action"`
		Notes  string `json:"notes"`
	}
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "invalid request"})
		return
	}
	var v any
	var err error
	if req.Action == "approve" {
		snapshot, snapshotErr := s.BatchPlanning.GetSnapshot(p.ID, id)
		if snapshotErr != nil || snapshot.Status != "approved" {
			c.JSON(409, gin.H{"error": "请先生成并审核通过批次状态快照"})
			return
		}
		v, err = s.BatchPlanning.ApproveBatch(p.ID, id, 0, req.Notes)
	} else if req.Action == "reject" {
		v, err = s.BatchPlanning.RejectBatch(p.ID, id, 0, req.Notes)
	} else {
		c.JSON(400, gin.H{"error": "action must be approve or reject"})
		return
	}
	if err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, v)
}
