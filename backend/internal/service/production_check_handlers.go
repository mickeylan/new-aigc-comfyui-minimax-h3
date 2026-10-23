package service

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

func (s *Service) HandleCreateProductionCheckReport(c *gin.Context) {
	projectID, ok := s.dependencyProjectID(c)
	if !ok {
		return
	}
	var body struct {
		EpisodeN int `json:"episode_n"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.EpisodeN < 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid episode_n"})
		return
	}
	row, err := NewProductionCheckService(s.DB).CreateReport(projectID, body.EpisodeN)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, row)
}

func (s *Service) HandleListProductionCheckReports(c *gin.Context) {
	projectID, ok := s.dependencyProjectID(c)
	if !ok {
		return
	}
	episode := 0
	if raw := c.Query("episode_n"); raw != "" {
		var err error
		episode, err = strconv.Atoi(raw)
		if err != nil || episode < 1 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid episode_n"})
			return
		}
	}
	rows, err := NewProductionCheckService(s.DB).ListReports(projectID, episode)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": rows})
}

func (s *Service) HandleReviewProductionCheckReport(c *gin.Context) {
	projectID, ok := s.dependencyProjectID(c)
	if !ok {
		return
	}
	id, err := strconv.ParseUint(c.Param("rid"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid report id"})
		return
	}
	var body struct {
		Status string `json:"status"`
		Note   string `json:"note"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	row, err := NewProductionCheckService(s.DB).ReviewReport(projectID, uint(id), body.Status, body.Note)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, row)
}
