package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

type GenerationCandidateService struct{ db *gorm.DB }

type SceneCandidateCapture struct {
	ProjectID         uint
	SceneID           uint
	MediaType         string
	TaskID            string
	File              string
	VideoInputFile    string
	VideoGPU          *int
	Prompt            string
	References        any
	Params            any
	Provenance        any
	ParentCandidateID *uint
	// ExpectedTaskID prevents a late successful task from replacing a newer Scene task.
	ExpectedTaskID string
	// RequireParentFresh is used by detached candidate retries. It prevents a successful
	// retry from promoting a snapshot that became stale while the task was running.
	RequireParentFresh bool
}

func NewGenerationCandidateService(db *gorm.DB) *GenerationCandidateService {
	return &GenerationCandidateService{db: db}
}

func snapshotJSON(v any) string {
	if v == nil {
		return "null"
	}
	if raw, ok := v.(string); ok && json.Valid([]byte(raw)) {
		return raw
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "null"
	}
	return string(b)
}

func (s *GenerationCandidateService) EnsureSceneCurrent(projectID, sceneID uint) error {
	var scene models.Scene
	if err := s.db.Where("id = ? AND project_id = ?", sceneID, projectID).First(&scene).Error; err != nil {
		return err
	}
	for _, item := range []struct {
		media, task, file, input, prompt string
		gpu                              *int
	}{
		{"image", scene.ImageTaskID, scene.ImageFile, "", scene.ImagePrompt, nil},
		{"video", scene.VideoTaskID, scene.VideoFile, scene.VideoInputFile, scene.VideoFullPrompt, scene.VideoGPU},
	} {
		if item.file == "" {
			continue
		}
		taskID := item.task
		if taskID == "" {
			taskID = fmt.Sprintf("legacy-scene-%d-%s-%s", scene.ID, item.media, item.file)
		}
		var count int64
		if err := s.db.Model(&models.GenerationCandidate{}).Where("project_id = ? AND task_id = ? AND media_type = ?", projectID, taskID, item.media).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			continue
		}
		_, err := s.CaptureSceneSuccess(SceneCandidateCapture{
			ProjectID: projectID, SceneID: sceneID, MediaType: item.media, TaskID: taskID,
			File: item.file, VideoInputFile: item.input, VideoGPU: item.gpu, Prompt: item.prompt,
			References: scene.ReferenceImagesJSON, Provenance: map[string]any{"source": "legacy_scene_backfill"},
		})
		if err != nil {
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

// CaptureSceneSuccess records immutable task inputs and projects the result to Scene in one transaction.
// Scene remains the generation authority; candidates are the retained take history.
func (s *GenerationCandidateService) CaptureSceneSuccess(in SceneCandidateCapture) (*models.GenerationCandidate, error) {
	if in.MediaType != "image" && in.MediaType != "video" {
		return nil, fmt.Errorf("不支持的候选媒体类型")
	}
	if strings.TrimSpace(in.TaskID) == "" || strings.TrimSpace(in.File) == "" {
		return nil, fmt.Errorf("候选任务和文件不能为空")
	}
	var captured models.GenerationCandidate
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var scene models.Scene
		if err := tx.Where("id = ? AND project_id = ?", in.SceneID, in.ProjectID).First(&scene).Error; err != nil {
			return err
		}
		if in.RequireParentFresh {
			if in.ParentCandidateID == nil {
				return fmt.Errorf("候选重试缺少父候选")
			}
			var parent models.GenerationCandidate
			if err := tx.Where("id = ? AND project_id = ? AND entity_type = ? AND entity_id = ? AND media_type = ?", *in.ParentCandidateID, in.ProjectID, "scene", in.SceneID, in.MediaType).First(&parent).Error; err != nil {
				return fmt.Errorf("父候选不可用: %w", err)
			}
			if parent.Stale {
				return fmt.Errorf("候选已过期")
			}
		}
		if in.ExpectedTaskID != "" {
			actual := scene.ImageTaskID
			if in.MediaType == "video" {
				actual = scene.VideoTaskID
			}
			if actual != in.ExpectedTaskID {
				return fmt.Errorf("生成任务已过期")
			}
		}
		var existing models.GenerationCandidate
		err := tx.Where("project_id = ? AND task_id = ? AND media_type = ?", in.ProjectID, in.TaskID, in.MediaType).First(&existing).Error
		if err == nil {
			captured = existing
			return nil
		}
		if err != gorm.ErrRecordNotFound {
			return err
		}
		if err := tx.Model(&models.GenerationCandidate{}).
			Where("project_id = ? AND entity_type = ? AND entity_id = ? AND media_type = ? AND is_current = ?", in.ProjectID, "scene", in.SceneID, in.MediaType, true).
			Update("is_current", false).Error; err != nil {
			return err
		}
		captured = models.GenerationCandidate{
			ProjectID: in.ProjectID, EntityType: "scene", EntityID: in.SceneID, MediaType: in.MediaType,
			TaskID: in.TaskID, File: in.File, VideoInputFile: in.VideoInputFile, VideoGPU: in.VideoGPU,
			PromptSnapshot: in.Prompt, ReferencesJSON: snapshotJSON(in.References), ParamsJSON: snapshotJSON(in.Params),
			ProvenanceJSON: snapshotJSON(in.Provenance), ReviewStatus: "pending", ParentCandidateID: in.ParentCandidateID, IsCurrent: true,
		}
		if err := tx.Create(&captured).Error; err != nil {
			return err
		}
		updates := map[string]any{"error": ""}
		if in.MediaType == "image" {
			updates["image_file"], updates["image_task_id"], updates["image_token"], updates["image_candidate_parent_id"] = in.File, in.TaskID, "", nil
			updates["status"] = "image_ready"
		} else {
			updates["video_file"], updates["video_task_id"] = in.File, in.TaskID
			updates["video_input_file"], updates["video_gpu"], updates["video_candidate_parent_id"] = in.VideoInputFile, in.VideoGPU, nil
			updates["status"] = "video_ready"
		}
		return tx.Model(&models.Scene{}).Where("id = ? AND project_id = ?", in.SceneID, in.ProjectID).Updates(updates).Error
	})
	return &captured, err
}

// CaptureSceneTask is retained for callers/tests and now models a completed successful task.
func (s *GenerationCandidateService) CaptureSceneTask(projectID, sceneID uint, mediaType, taskID, file, prompt string, refs any, params any, provenance any, parentID *uint) (*models.GenerationCandidate, error) {
	return s.CaptureSceneSuccess(SceneCandidateCapture{ProjectID: projectID, SceneID: sceneID, MediaType: mediaType, TaskID: taskID, File: file, Prompt: prompt, References: refs, Params: params, Provenance: provenance, ParentCandidateID: parentID})
}

func (s *GenerationCandidateService) Review(projectID, id uint, status, reason string) (*models.GenerationCandidate, error) {
	if status != "accepted" && status != "rejected" && status != "pending" {
		return nil, fmt.Errorf("无效审核状态")
	}
	var row models.GenerationCandidate
	if err := s.db.Where("id = ? AND project_id = ?", id, projectID).First(&row).Error; err != nil {
		return nil, err
	}
	if status == "rejected" && row.IsCurrent {
		return nil, fmt.Errorf("当前候选不能拒绝，请先选择另一候选")
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
		if row.ReviewStatus == "rejected" {
			return fmt.Errorf("已拒绝候选不能设为当前")
		}
		if err := tx.Model(&models.GenerationCandidate{}).Where("project_id = ? AND entity_type = ? AND entity_id = ? AND media_type = ?", projectID, row.EntityType, row.EntityID, row.MediaType).Update("is_current", false).Error; err != nil {
			return err
		}
		if err := tx.Model(&row).Updates(map[string]any{"is_current": true, "review_status": "accepted", "reviewed_at": time.Now()}).Error; err != nil {
			return err
		}
		if row.EntityType == "scene" {
			updates := map[string]any{"error": ""}
			if row.MediaType == "image" {
				updates["image_file"], updates["image_task_id"], updates["status"] = row.File, row.TaskID, "image_ready"
				updates["video_file"], updates["video_input_file"], updates["video_task_id"], updates["video_gpu"] = "", "", "", nil
				if err := tx.Model(&models.GenerationCandidate{}).Where("project_id = ? AND entity_type = ? AND entity_id = ? AND media_type = ?", projectID, "scene", row.EntityID, "video").Updates(map[string]any{"stale": true, "stale_reason": "分镜画面候选已切换", "is_current": false}).Error; err != nil {
					return err
				}
			} else {
				updates["video_file"], updates["video_task_id"], updates["video_input_file"], updates["video_gpu"], updates["status"] = row.File, row.TaskID, row.VideoInputFile, row.VideoGPU, "video_ready"
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
