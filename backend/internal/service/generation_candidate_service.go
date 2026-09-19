package service

import (
	"encoding/json"
	"fmt"
	"time"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

type GenerationCandidateService struct{ db *gorm.DB }

func NewGenerationCandidateService(db *gorm.DB) *GenerationCandidateService {
	return &GenerationCandidateService{db: db}
}

func (s *GenerationCandidateService) EnsureSceneCurrent(projectID, sceneID uint) error {
	var scene models.Scene
	if err := s.db.Where("id = ? AND project_id = ?", sceneID, projectID).First(&scene).Error; err != nil {
		return err
	}
	for _, item := range []struct{ media, task, file, prompt string }{
		{"image", scene.ImageTaskID, scene.ImageFile, scene.ImagePrompt},
		{"video", scene.VideoTaskID, scene.VideoFile, scene.VideoFullPrompt},
	} {
		if item.file == "" {
			continue
		}
		taskID := item.task
		if taskID == "" {
			taskID = fmt.Sprintf("legacy-scene-%d-%s", scene.ID, item.media)
		}
		candidate := models.GenerationCandidate{ProjectID: projectID, EntityType: "scene", EntityID: sceneID, MediaType: item.media, TaskID: taskID, File: item.file, PromptSnapshot: item.prompt, ReferencesJSON: scene.ReferenceImagesJSON, ReviewStatus: "accepted", IsCurrent: true}
		if err := s.db.Where("project_id = ? AND task_id = ? AND media_type = ?", projectID, taskID, item.media).FirstOrCreate(&candidate).Error; err != nil {
			return err
		}
	}
	return nil
}

func (s *GenerationCandidateService) ListScene(projectID, sceneID uint, mediaType string) ([]models.GenerationCandidate, error) {
	if err := s.EnsureSceneCurrent(projectID, sceneID); err != nil {
		return nil, err
	}
	var rows []models.GenerationCandidate
	q := s.db.Where("project_id = ? AND entity_type = ? AND entity_id = ?", projectID, "scene", sceneID)
	if mediaType != "" {
		q = q.Where("media_type = ?", mediaType)
	}
	return rows, q.Order("created_at DESC, id DESC").Find(&rows).Error
}

func (s *GenerationCandidateService) CaptureSceneTask(projectID, sceneID uint, mediaType, taskID, file, prompt string, refs any, params any, provenance any, parentID *uint) (*models.GenerationCandidate, error) {
	if mediaType != "image" && mediaType != "video" {
		return nil, fmt.Errorf("不支持的候选媒体类型")
	}
	var scene models.Scene
	if err := s.db.Where("id = ? AND project_id = ?", sceneID, projectID).First(&scene).Error; err != nil {
		return nil, err
	}
	marshal := func(v any) string { b, _ := json.Marshal(v); return string(b) }
	row := models.GenerationCandidate{ProjectID: projectID, EntityType: "scene", EntityID: sceneID, MediaType: mediaType, TaskID: taskID, File: file, PromptSnapshot: prompt, ReferencesJSON: marshal(refs), ParamsJSON: marshal(params), ProvenanceJSON: marshal(provenance), ReviewStatus: "pending", ParentCandidateID: parentID}
	if err := s.db.Create(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *GenerationCandidateService) Review(projectID, id uint, status, reason string) (*models.GenerationCandidate, error) {
	if status != "accepted" && status != "rejected" && status != "pending" {
		return nil, fmt.Errorf("无效审核状态")
	}
	var row models.GenerationCandidate
	if err := s.db.Where("id = ? AND project_id = ?", id, projectID).First(&row).Error; err != nil {
		return nil, err
	}
	now := time.Now()
	updates := map[string]any{"review_status": status, "review_reason": reason}
	if status == "pending" {
		updates["reviewed_at"] = nil
	} else {
		updates["reviewed_at"] = &now
	}
	if err := s.db.Model(&row).Updates(updates).Error; err != nil {
		return nil, err
	}
	s.db.First(&row, row.ID)
	return &row, nil
}

func (s *GenerationCandidateService) SelectCurrent(projectID, id uint) (*models.GenerationCandidate, error) {
	var selected models.GenerationCandidate
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var row models.GenerationCandidate
		if err := tx.Where("id = ? AND project_id = ?", id, projectID).First(&row).Error; err != nil {
			return err
		}
		if row.Stale {
			return fmt.Errorf("过期候选不能设为当前")
		}
		if err := tx.Model(&models.GenerationCandidate{}).Where("project_id = ? AND entity_type = ? AND entity_id = ? AND media_type = ?", projectID, row.EntityType, row.EntityID, row.MediaType).Update("is_current", false).Error; err != nil {
			return err
		}
		if err := tx.Model(&row).Updates(map[string]any{"is_current": true, "review_status": "accepted"}).Error; err != nil {
			return err
		}
		if row.EntityType == "scene" {
			updates := map[string]any{}
			if row.MediaType == "image" {
				updates["image_file"] = row.File
				updates["image_task_id"] = row.TaskID
			}
			if row.MediaType == "video" {
				updates["video_file"] = row.File
				updates["video_task_id"] = row.TaskID
			}
			if err := tx.Model(&models.Scene{}).Where("id = ? AND project_id = ?", row.EntityID, projectID).Updates(updates).Error; err != nil {
				return err
			}
		}
		selected = row
		selected.IsCurrent, selected.ReviewStatus = true, "accepted"
		return nil
	})
	return &selected, err
}

func MarkSceneCandidatesStale(db *gorm.DB, projectID, sceneID uint, mediaType, reason string) error {
	q := db.Model(&models.GenerationCandidate{}).Where("project_id = ? AND entity_type = ? AND entity_id = ?", projectID, "scene", sceneID)
	if mediaType != "" {
		q = q.Where("media_type = ?", mediaType)
	}
	return q.Updates(map[string]any{"stale": true, "stale_reason": reason, "is_current": false}).Error
}
