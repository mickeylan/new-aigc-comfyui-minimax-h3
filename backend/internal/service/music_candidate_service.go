package service

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

const musicCandidateReviewConfirmationV1 = "music-candidate-listened-v1"

var (
	ErrMusicCandidateNotFound       = errors.New("配乐候选不存在")
	ErrMusicCandidateReviewConflict = errors.New("配乐候选审核冲突")
)

// MusicCandidateService manages future generated audio outputs without knowing or invoking a runtime workflow.
type MusicCandidateService struct {
	db           *gorm.DB
	dependencies *ArtifactDependencyService
}

func NewMusicCandidateService(db *gorm.DB, dependencies ...*ArtifactDependencyService) *MusicCandidateService {
	s := &MusicCandidateService{db: db}
	if len(dependencies) > 0 {
		s.dependencies = dependencies[0]
	}
	return s
}

func (s *MusicCandidateService) cue(projectID, cueID uint) (*models.MusicCueSheet, error) {
	var cue models.MusicCueSheet
	if err := s.db.Where("id = ? AND project_id = ?", cueID, projectID).First(&cue).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrMusicCueSheetNotFound
		}
		return nil, err
	}
	return &cue, nil
}

// RegisterGeneratedCandidate is the single ingestion point for future verified adapters.
// It persists provenance but never submits generation work itself.
func (s *MusicCandidateService) RegisterGeneratedCandidate(row models.MusicCandidate) (*models.MusicCandidate, error) {
	cue, err := s.cue(row.ProjectID, row.MusicCueSheetID)
	if err != nil {
		return nil, err
	}
	if cue.ReviewStatus != "approved" || cue.Stale || row.SourceVersionHash != cue.SourceVersionHash {
		return nil, fmt.Errorf("配乐候选来源不是当前已审核Cue")
	}
	if strings.TrimSpace(row.File) == "" || strings.TrimSpace(row.FileSHA256) == "" || row.FileSize <= 0 {
		return nil, fmt.Errorf("配乐候选缺少有效文件指纹")
	}
	row.ID = 0
	row.IsCurrent = false
	row.Stale = false
	row.StaleReason = ""
	row.ReviewStatus = "pending"
	row.ReviewReason = ""
	row.ReviewedAt = nil
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		if s.dependencies != nil {
			return s.dependencies.LinkMusicCandidateTx(tx, row.ProjectID, row.MusicCueSheetID, row.ID, row.SourceVersionHash)
		}
		return nil
	})
	return &row, err
}

func (s *MusicCandidateService) List(projectID, cueID uint) ([]models.MusicCandidate, error) {
	if _, err := s.cue(projectID, cueID); err != nil {
		return nil, err
	}
	var rows []models.MusicCandidate
	err := s.db.Where("project_id = ? AND music_cue_sheet_id = ?", projectID, cueID).
		Order("is_current DESC, created_at DESC, id DESC").Find(&rows).Error
	return rows, err
}

func (s *MusicCandidateService) Review(projectID, cueID, candidateID uint, status, reason, expectedSourceHash, expectedFileSHA256 string) (*models.MusicCandidate, error) {
	if status != "accepted" && status != "rejected" && status != "pending" {
		return nil, fmt.Errorf("无效审核状态")
	}
	var reviewed models.MusicCandidate
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var cue models.MusicCueSheet
		if err := tx.Where("id = ? AND project_id = ?", cueID, projectID).First(&cue).Error; err != nil {
			return ErrMusicCueSheetNotFound
		}
		var row models.MusicCandidate
		if err := tx.Where("id = ? AND project_id = ? AND music_cue_sheet_id = ?", candidateID, projectID, cueID).First(&row).Error; err != nil {
			return ErrMusicCandidateNotFound
		}
		if row.IsCurrent && status == "rejected" {
			return fmt.Errorf("当前采用的配乐不能拒绝，请先采用另一候选")
		}
		updates := map[string]any{"review_status": status, "review_reason": strings.TrimSpace(reason)}
		if status == "accepted" {
			if cue.ReviewStatus != "approved" || cue.Stale {
				return fmt.Errorf("配乐Cue尚未审核通过或已经过期")
			}
			if row.Stale || row.SourceVersionHash == "" || row.FileSHA256 == "" || row.FileSize <= 0 || strings.TrimSpace(row.File) == "" {
				return fmt.Errorf("%w：候选缺少有效音频或已经过期", ErrMusicCandidateReviewConflict)
			}
			if row.SourceVersionHash != cue.SourceVersionHash || expectedSourceHash != row.SourceVersionHash || expectedFileSHA256 != row.FileSHA256 {
				return fmt.Errorf("%w：候选不是用户刚才试听的当前Cue版本", ErrMusicCandidateReviewConflict)
			}
			now := time.Now()
			updates["review_source_hash"] = row.SourceVersionHash
			updates["review_file_sha256"] = row.FileSHA256
			updates["review_confirmation"] = musicCandidateReviewConfirmationV1
			updates["reviewed_at"] = now
		} else {
			updates["review_source_hash"] = ""
			updates["review_file_sha256"] = ""
			updates["review_confirmation"] = ""
			if status == "pending" {
				updates["reviewed_at"] = nil
			} else {
				updates["reviewed_at"] = time.Now()
			}
		}
		result := tx.Model(&models.MusicCandidate{}).
			Where("id = ? AND project_id = ? AND music_cue_sheet_id = ? AND source_version_hash = ? AND file_sha256 = ?", row.ID, projectID, cueID, row.SourceVersionHash, row.FileSHA256).
			Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("%w：候选在审核期间已变化", ErrMusicCandidateReviewConflict)
		}
		return tx.First(&reviewed, row.ID).Error
	})
	return &reviewed, err
}

func (s *MusicCandidateService) Select(projectID, cueID, candidateID uint, expectedSourceHash, expectedFileSHA256 string) (*models.MusicCandidate, error) {
	var selected models.MusicCandidate
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var cue models.MusicCueSheet
		if err := tx.Where("id = ? AND project_id = ?", cueID, projectID).First(&cue).Error; err != nil {
			return ErrMusicCueSheetNotFound
		}
		var row models.MusicCandidate
		if err := tx.Where("id = ? AND project_id = ? AND music_cue_sheet_id = ?", candidateID, projectID, cueID).First(&row).Error; err != nil {
			return ErrMusicCandidateNotFound
		}
		if cue.ReviewStatus != "approved" || cue.Stale || row.Stale || row.SourceVersionHash != cue.SourceVersionHash {
			return fmt.Errorf("%w：候选或配乐Cue已经过期", ErrMusicCandidateReviewConflict)
		}
		if row.ReviewStatus != "accepted" || row.ReviewConfirmation != musicCandidateReviewConfirmationV1 || row.ReviewSourceHash != row.SourceVersionHash || row.ReviewFileSHA256 != row.FileSHA256 {
			return fmt.Errorf("%w：候选尚未完成匹配当前内容的人工试听审核", ErrMusicCandidateReviewConflict)
		}
		if expectedSourceHash != row.SourceVersionHash || expectedFileSHA256 != row.FileSHA256 {
			return fmt.Errorf("%w：候选不是用户刚才试听的版本", ErrMusicCandidateReviewConflict)
		}
		if err := tx.Model(&models.MusicCandidate{}).Where("project_id = ? AND music_cue_sheet_id = ?", projectID, cueID).Update("is_current", false).Error; err != nil {
			return err
		}
		if err := tx.Model(&row).Update("is_current", true).Error; err != nil {
			return err
		}
		row.IsCurrent = true
		selected = row
		return nil
	})
	return &selected, err
}

func (s *MusicCandidateService) Delete(projectID, cueID, candidateID uint) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		var row models.MusicCandidate
		if err := tx.Where("id = ? AND project_id = ? AND music_cue_sheet_id = ?", candidateID, projectID, cueID).First(&row).Error; err != nil {
			return ErrMusicCandidateNotFound
		}
		if row.IsCurrent {
			return fmt.Errorf("当前采用的配乐候选不能删除")
		}
		if s.dependencies != nil {
			if err := s.dependencies.DeleteArtifactTx(tx, projectID, ArtifactRef{Kind: "music_candidate", ID: row.ID}); err != nil {
				return err
			}
		}
		return tx.Delete(&row).Error
	})
}

func markMusicCandidatesStale(tx *gorm.DB, projectID, cueID uint, reason string) error {
	if !tx.Migrator().HasTable(&models.MusicCandidate{}) {
		return nil
	}
	return tx.Model(&models.MusicCandidate{}).
		Where("project_id = ? AND music_cue_sheet_id = ? AND stale = ?", projectID, cueID, false).
		Updates(map[string]any{"stale": true, "stale_reason": strings.TrimSpace(reason), "is_current": false}).Error
}
