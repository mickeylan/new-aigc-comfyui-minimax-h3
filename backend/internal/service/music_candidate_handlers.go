package service

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
)

func musicCandidateRouteIDs(c *gin.Context) (uint, uint, uint, bool) {
	pid, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid project id"})
		return 0, 0, 0, false
	}
	cueID, err := strconv.ParseUint(c.Param("cid"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid music cue id"})
		return 0, 0, 0, false
	}
	candidateID := uint64(0)
	if raw := c.Param("mid"); raw != "" {
		candidateID, err = strconv.ParseUint(raw, 10, 32)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid music candidate id"})
			return 0, 0, 0, false
		}
	}
	return uint(pid), uint(cueID), uint(candidateID), true
}

func (s *Service) musicCandidateResponse(projectID uint, row models.MusicCandidate) gin.H {
	url := ""
	if file := strings.TrimLeft(strings.TrimSpace(row.File), "/\\"); file != "" {
		url = fmt.Sprintf("/api/input/%d/%s", projectID, file)
	}
	response := gin.H{"candidate": row, "audio_url": url, "task_status": ""}
	if row.TaskID != nil {
		var task models.Task
		if s.DB.First(&task, *row.TaskID).Error == nil {
			response["task_status"] = task.Status
			response["task_id"] = task.TaskID
			response["task_error"] = task.Error
		}
	}
	return response
}

func (s *Service) HandleGenerateMusicCandidate(c *gin.Context) {
	projectID, cueID, _, ok := musicCandidateRouteIDs(c)
	if !ok {
		return
	}
	var body struct {
		Seed int64 `json:"seed"`
	}
	if err := c.ShouldBindJSON(&body); err != nil && !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	if body.Seed == 0 {
		body.Seed = -1
	}
	row, err := s.Projects.StartMusic3Cue(projectID, cueID, body.Seed)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, s.musicCandidateResponse(projectID, *row))
}

func (s *Service) HandleListMusicCandidates(c *gin.Context) {
	projectID, cueID, _, ok := musicCandidateRouteIDs(c)
	if !ok {
		return
	}
	rows, err := s.MusicCandidates.List(projectID, cueID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, ErrMusicCueSheetNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	items := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		items = append(items, s.musicCandidateResponse(projectID, row))
	}
	c.JSON(http.StatusOK, items)
}

func (s *Service) HandleReviewMusicCandidate(c *gin.Context) {
	projectID, cueID, candidateID, ok := musicCandidateRouteIDs(c)
	if !ok {
		return
	}
	var body struct {
		Status                string `json:"status"`
		Reason                string `json:"reason"`
		ExpectedSourceHash    string `json:"expected_source_hash"`
		ExpectedFileSHA256    string `json:"expected_file_sha256"`
		ConfirmedFullPlayback bool   `json:"confirmed_full_playback"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	if body.Status == "accepted" && !body.ConfirmedFullPlayback {
		c.JSON(http.StatusBadRequest, gin.H{"error": "审核通过前必须确认已完整试听"})
		return
	}
	row, err := s.MusicCandidates.Review(projectID, cueID, candidateID, body.Status, body.Reason, body.ExpectedSourceHash, body.ExpectedFileSHA256)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, ErrMusicCandidateNotFound) || errors.Is(err, ErrMusicCueSheetNotFound) {
			status = http.StatusNotFound
		}
		if errors.Is(err, ErrMusicCandidateReviewConflict) {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, s.musicCandidateResponse(projectID, *row))
}

func (s *Service) HandleSelectMusicCandidate(c *gin.Context) {
	projectID, cueID, candidateID, ok := musicCandidateRouteIDs(c)
	if !ok {
		return
	}
	var body struct {
		ExpectedSourceHash string `json:"expected_source_hash"`
		ExpectedFileSHA256 string `json:"expected_file_sha256"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	row, err := s.MusicCandidates.Select(projectID, cueID, candidateID, body.ExpectedSourceHash, body.ExpectedFileSHA256)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, ErrMusicCandidateNotFound) || errors.Is(err, ErrMusicCueSheetNotFound) {
			status = http.StatusNotFound
		}
		if errors.Is(err, ErrMusicCandidateReviewConflict) {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, s.musicCandidateResponse(projectID, *row))
}

func (s *Service) HandleDeleteMusicCandidate(c *gin.Context) {
	projectID, cueID, candidateID, ok := musicCandidateRouteIDs(c)
	if !ok {
		return
	}
	if err := s.MusicCandidates.Delete(projectID, cueID, candidateID); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, ErrMusicCandidateNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
