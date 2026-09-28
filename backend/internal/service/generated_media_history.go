package service

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

type GeneratedMediaHistoryItem struct {
	Family              string    `json:"family"`
	ID                  uint      `json:"id"`
	ProjectID           uint      `json:"project_id"`
	EntityType          string    `json:"entity_type"`
	EntityID            uint      `json:"entity_id"`
	MediaType           string    `json:"media_type"`
	File                string    `json:"file"`
	PreviewURL          string    `json:"preview_url"`
	Prompt              string    `json:"prompt"`
	Provenance          string    `json:"provenance"`
	TaskID              string    `json:"task_id,omitempty"`
	Selected            bool      `json:"selected"`
	Favorite            bool      `json:"favorite"`
	ReviewStatus        string    `json:"review_status,omitempty"`
	Stale               bool      `json:"stale"`
	StaleReason         string    `json:"stale_reason,omitempty"`
	DeleteBlockedReason string    `json:"delete_blocked_reason,omitempty"`
	CreatedAt           time.Time `json:"created_at"`
}

type GeneratedMediaHistoryService struct {
	db     *gorm.DB
	upload *UploadManager
}

func NewGeneratedMediaHistoryService(db *gorm.DB, upload *UploadManager) *GeneratedMediaHistoryService {
	return &GeneratedMediaHistoryService{db: db, upload: upload}
}

func (s *GeneratedMediaHistoryService) List(projectID uint, entityType, mediaType string) ([]GeneratedMediaHistoryItem, error) {
	items := []GeneratedMediaHistoryItem{}
	var variants []models.AssetVariant
	qv := s.db.Where("project_id = ?", projectID)
	if entityType != "" {
		qv = qv.Where("entity_type = ?", entityType)
	}
	if err := qv.Find(&variants).Error; err != nil {
		return nil, err
	}
	for _, row := range variants {
		items = append(items, GeneratedMediaHistoryItem{Family: "asset_variant", ID: row.ID, ProjectID: projectID, EntityType: row.EntityType, EntityID: row.EntityID, MediaType: "image", File: row.File, PreviewURL: fmt.Sprintf("/api/input/%d/%s", projectID, row.File), Prompt: row.Prompt, Provenance: row.Provenance, Selected: row.Selected, Favorite: row.Favorite, DeleteBlockedReason: selectedReason(row.Selected), CreatedAt: row.CreatedAt})
	}
	if entityType == "" || entityType == "scene" {
		var candidates []models.GenerationCandidate
		qc := s.db.Where("project_id = ?", projectID)
		if mediaType != "" {
			qc = qc.Where("media_type = ?", mediaType)
		}
		if err := qc.Find(&candidates).Error; err != nil {
			return nil, err
		}
		for _, row := range candidates {
			file, url := row.File, ""
			if row.MediaType == "video" && row.VideoInputFile != "" {
				file = row.VideoInputFile
			}
			if file != "" {
				url = fmt.Sprintf("/api/input/%d/%s", projectID, file)
			}
			reason := selectedReason(row.IsCurrent)
			if reason == "" {
				reason = s.candidateDeleteBlock(row)
			}
			items = append(items, GeneratedMediaHistoryItem{Family: "scene_candidate", ID: row.ID, ProjectID: projectID, EntityType: row.EntityType, EntityID: row.EntityID, MediaType: row.MediaType, File: file, PreviewURL: url, Prompt: row.PromptSnapshot, Provenance: row.ProvenanceJSON, TaskID: row.TaskID, Selected: row.IsCurrent, ReviewStatus: row.ReviewStatus, Stale: row.Stale, StaleReason: row.StaleReason, DeleteBlockedReason: reason, CreatedAt: row.CreatedAt})
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	return items, nil
}

func selectedReason(selected bool) string {
	if selected {
		return "当前使用中的版本不能删除，请先选择其他版本"
	}
	return ""
}

func (s *GeneratedMediaHistoryService) candidateDeleteBlock(row models.GenerationCandidate) string {
	var children int64
	_ = s.db.Model(&models.GenerationCandidate{}).Where("parent_candidate_id = ?", row.ID).Count(&children).Error
	if children > 0 {
		return "存在基于此版本生成的分支"
	}
	var task models.Task
	if s.db.Where("task_id = ?", row.TaskID).First(&task).Error == nil && (task.Status == "pending" || task.Status == "queued" || task.Status == "running") {
		return "生成任务仍在运行"
	}
	return ""
}

func (s *GeneratedMediaHistoryService) Select(projectID uint, family string, id uint) error {
	if family == "asset_variant" {
		_, err := NewAssetVariantService(s.db).Select(projectID, id)
		return err
	}
	if family == "scene_candidate" {
		_, err := NewGenerationCandidateService(s.db).SelectCurrent(projectID, id)
		return err
	}
	return fmt.Errorf("不支持的历史类型")
}

func (s *GeneratedMediaHistoryService) Favorite(projectID uint, family string, id uint, favorite bool) error {
	if family != "asset_variant" {
		return fmt.Errorf("Scene候选请使用审核状态，不支持收藏")
	}
	_, err := NewAssetVariantService(s.db).SetFavorite(projectID, id, favorite)
	return err
}

func (s *GeneratedMediaHistoryService) Delete(projectID uint, family string, id uint) error {
	file := ""
	if family == "asset_variant" {
		var row models.AssetVariant
		if err := s.db.Where("id=? AND project_id=?", id, projectID).First(&row).Error; err != nil {
			return err
		}
		if row.Selected {
			return errors.New(selectedReason(true))
		}
		file = row.File
		if err := s.db.Delete(&row).Error; err != nil {
			return err
		}
	} else if family == "scene_candidate" {
		var row models.GenerationCandidate
		if err := s.db.Where("id=? AND project_id=?", id, projectID).First(&row).Error; err != nil {
			return err
		}
		if reason := selectedReason(row.IsCurrent); reason != "" {
			return errors.New(reason)
		}
		if reason := s.candidateDeleteBlock(row); reason != "" {
			return errors.New(reason)
		}
		if row.MediaType == "video" && row.VideoInputFile != "" {
			file = row.VideoInputFile
		} else if row.MediaType == "image" {
			file = row.File
		}
		if err := s.db.Delete(&row).Error; err != nil {
			return err
		}
	} else {
		return fmt.Errorf("不支持的历史类型")
	}
	return s.removeUnreferencedProjectFile(projectID, file)
}

func (s *GeneratedMediaHistoryService) BulkDelete(projectID uint, refs []struct {
	Family string `json:"family"`
	ID     uint   `json:"id"`
}) error {
	for _, ref := range refs {
		if err := s.Delete(projectID, ref.Family, ref.ID); err != nil {
			return err
		}
	}
	return nil
}

func (s *GeneratedMediaHistoryService) removeUnreferencedProjectFile(projectID uint, file string) error {
	file = strings.TrimSpace(file)
	if file == "" || s.upload == nil || s.upload.remote == nil {
		return nil
	}
	var refs int64
	queries := []struct {
		model any
		where string
	}{
		{&models.AssetVariant{}, "project_id=? AND file=?"}, {&models.GenerationCandidate{}, "project_id=? AND (file=? OR video_input_file=?)"},
		{&models.Character{}, "project_id=? AND (portrait=? OR sheet=?)"}, {&models.CharacterLook{}, "project_id=? AND image=?"},
		{&models.Asset{}, "project_id=? AND (image=? OR sheet=?)"}, {&models.Scene{}, "project_id=? AND (image_file=? OR video_input_file=?)"},
		{&models.FrameCandidate{}, "project_id=? AND image_file=?"},
	}
	for _, query := range queries {
		args := []any{projectID, file}
		if strings.Count(query.where, "?") == 3 {
			args = append(args, file)
		}
		var count int64
		if err := s.db.Model(query.model).Where(query.where, args...).Count(&count).Error; err != nil {
			return err
		}
		refs += count
	}
	if refs > 0 {
		return nil
	}
	name, err := safeFileSegment(filepath.Base(file), "history file")
	if err != nil || name != file {
		return nil
	}
	path := filepath.Join(s.upload.InputDir(), fmt.Sprint(projectID), name)
	if err := s.upload.remote.Remove(path); err != nil {
		return fmt.Errorf("历史已删除，但清理文件失败: %w", err)
	}
	return s.db.Where("task_id=? AND name=?", fmt.Sprint(projectID), name).Delete(&models.UploadFile{}).Error
}
