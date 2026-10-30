package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

var (
	ErrMergeSnapshotChanged = errors.New("合并输入快照已变化，请重新确认编排")
	ErrMergeRetryConflict   = errors.New("该集已有进行中的合并任务")
)

// validateMergeExecution checks only inputs which will actually be consumed by ffmpeg.
func (s *ProjectService) validateMergeExecution(p *models.Project, mt *models.MergeTask, scenes []models.Scene) error {
	videos, err := s.resolveMergeVideos(p, mt, scenes)
	if err != nil {
		return err
	}
	if s.remote == nil {
		return fmt.Errorf("媒体探测服务不可用")
	}
	total := 0.0
	for i, video := range videos {
		info, probeErr := s.remote.ProbeMedia(video.Path)
		if probeErr != nil {
			return fmt.Errorf("探测编排片段%d失败", i+1)
		}
		if info.Duration <= 0 || info.Width <= 0 || info.Height <= 0 {
			return fmt.Errorf("编排片段%d的视频尺寸或时长无效", i+1)
		}
		total += info.Duration
	}
	var layers []models.AudioLayer
	if err := s.db.Where("project_id = ? AND episode_n = ?", p.ID, mt.EpisodeN).Order("start_time, id").Find(&layers).Error; err != nil {
		return err
	}
	warnings := make([]string, 0)
	for _, decision := range resolveAudioLayerMix(layers) {
		if decision.Status == audioLayerMixYielded {
			warnings = append(warnings, fmt.Sprintf("音频层%d因优先级重叠未混入", decision.Layer.ID))
			continue
		}
		if decision.Status != audioLayerMixIncluded && decision.Status != audioLayerMixPartialOverlap {
			continue
		}
		layer := decision.Layer
		if layer.StartTime < 0 || layer.StartTime >= layer.EndTime || layer.EndTime > total {
			return fmt.Errorf("音频层%d时间范围超出成片时长", layer.ID)
		}
		abs, pathErr := s.audioMixMediaPath(p.ID, layer.File)
		if pathErr != nil {
			return fmt.Errorf("音频层%d文件无效", layer.ID)
		}
		if _, probeErr := s.remote.ProbeMedia(abs); probeErr != nil {
			return fmt.Errorf("音频层%d文件不可用", layer.ID)
		}
		if decision.Status == audioLayerMixPartialOverlap {
			warnings = append(warnings, fmt.Sprintf("音频层%d与高优先级音乐部分重叠", layer.ID))
		}
	}
	encoded, _ := json.Marshal(warnings)
	mt.PreflightWarnings = string(encoded)
	return nil
}

// createRetryMergeTask performs the transactional copy but deliberately does not start ffmpeg.
func (s *ProjectService) createRetryMergeTask(projectID, mergeID uint) (*models.MergeTask, []models.Scene, error) {
	var created models.MergeTask
	var scenes []models.Scene
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var original models.MergeTask
		if err := tx.Where("id = ? AND project_id = ? AND status = ?", mergeID, projectID, "failed").First(&original).Error; err != nil {
			return err
		}
		if strings.TrimSpace(original.InputSnapshot) == "" {
			return ErrMergeSnapshotChanged
		}
		current, err := s.validateMergeInputs(tx, &original)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrMergeSnapshotChanged, err)
		}
		var active int64
		if err := tx.Model(&models.MergeTask{}).Where("project_id = ? AND episode_n = ? AND status IN ?", projectID, original.EpisodeN, []string{"pending", "running"}).Count(&active).Error; err != nil {
			return err
		}
		if active > 0 {
			return ErrMergeRetryConflict
		}
		created = original
		created.ID, created.Status, created.OutputFile, created.Error = 0, "pending", "", ""
		created.Subtitle = false
		created.CreatedAt, created.UpdatedAt = time.Time{}, time.Time{}
		if err := tx.Create(&created).Error; err != nil {
			return err
		}
		scenes = current
		return nil
	})
	return &created, scenes, err
}

func (s *ProjectService) RetryMergeTask(p *models.Project, mergeID uint) (*models.MergeTask, error) {
	var original models.MergeTask
	if err := s.db.Where("id = ? AND project_id = ? AND status = ?", mergeID, p.ID, "failed").First(&original).Error; err != nil {
		return nil, err
	}
	if strings.TrimSpace(original.InputSnapshot) == "" {
		return nil, ErrMergeSnapshotChanged
	}
	scenes, err := s.validateMergeInputs(s.db, &original)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMergeSnapshotChanged, err)
	}
	if err := s.validateMergeExecution(p, &original, scenes); err != nil {
		return nil, err
	}
	mt, scenes, err := s.createRetryMergeTask(p.ID, mergeID)
	if err != nil {
		return nil, err
	}
	// createRetryMergeTask copies warnings in the same transaction as the pending task.
	go s.runAudioMerge(p, mt, scenes, mt.RequestedDub, mt.RequestedSubtitles)
	s.pushProject(p)
	return mt, nil
}
