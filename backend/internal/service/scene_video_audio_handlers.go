package service

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
)

type sceneAudioCandidateResponse struct {
	models.SceneAudioCandidate
	SourceSnapshot *sceneAudioSnapshot `json:"source_snapshot,omitempty"`
	AuditError     string              `json:"audit_error,omitempty"`
}

func sceneAudioCandidateResponses(rows []models.SceneAudioCandidate) []sceneAudioCandidateResponse {
	out := make([]sceneAudioCandidateResponse, 0, len(rows))
	for _, row := range rows {
		item := sceneAudioCandidateResponse{SceneAudioCandidate: row}
		var snapshot sceneAudioSnapshot
		if err := json.Unmarshal([]byte(row.SourceJSON), &snapshot); err != nil {
			item.AuditError = "来源审计快照无法解析"
		} else {
			item.SourceSnapshot = &snapshot
		}
		out = append(out, item)
	}
	return out
}

func (s *Service) HandleListSceneAudioCandidates(c *gin.Context) {
	sc, ok := s.loadScene(c)
	if !ok {
		return
	}
	selection := s.Projects.sceneReviewedAudioSelection(sc)
	var rows []models.SceneAudioCandidate
	if err := s.DB.Where("project_id = ? AND scene_id = ?", sc.ProjectID, sc.ID).Order("created_at DESC, id DESC").Find(&rows).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"candidates": sceneAudioCandidateResponses(rows), "selection": selection})
}

func (s *Service) HandleCreateSceneAudioCandidate(c *gin.Context) {
	sc, ok := s.loadScene(c)
	if !ok {
		return
	}
	row, err := s.Projects.CreateSceneAudioCandidate(sc)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, row)
}

func validateSceneAudioReviewConfirmation(status string, confirmed bool) error {
	if strings.TrimSpace(status) == "accepted" && !confirmed {
		return errors.New("接受前必须确认已核对完整音轨、Native Shot文字范围、音频范围和声音归属")
	}
	return nil
}

func (s *Service) HandleReviewSceneAudioCandidate(c *gin.Context) {
	sc, ok := s.loadScene(c)
	if !ok {
		return
	}
	candidateID, err := strconv.ParseUint(c.Param("cid"), 10, 64)
	if err != nil || candidateID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "候选ID无效"})
		return
	}
	var req struct {
		Status             string `json:"status"`
		Reason             string `json:"reason"`
		ExpectedSourceHash string `json:"expected_source_hash"`
		ExpectedFileSHA256 string `json:"expected_file_sha256"`
		AuditConfirmed     bool   `json:"audit_confirmed"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	if err := validateSceneAudioReviewConfirmation(req.Status, req.AuditConfirmed); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	row, err := s.Projects.ReviewSceneAudioCandidate(sc, uint(candidateID), strings.TrimSpace(req.Status), req.Reason, req.ExpectedSourceHash, req.ExpectedFileSHA256)
	if err != nil {
		status := http.StatusUnprocessableEntity
		if errors.Is(err, ErrSceneAudioReviewConflict) {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, row)
}
