package service

import (
	"encoding/json"
	"fmt"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

// linkMergeTaskDependenciesTx mirrors only sources that are already captured by the authoritative merge snapshot.
func (s *ProjectService) linkMergeTaskDependenciesTx(tx *gorm.DB, mt *models.MergeTask, scenes []models.Scene, raw []byte) error {
	if s.artifactDependencies == nil {
		return nil
	}
	downstream := ArtifactRef{Kind: ArtifactKindMergeTask, ID: mt.ID}
	for _, scene := range scenes {
		var candidate models.GenerationCandidate
		err := tx.Where("project_id = ? AND entity_type = ? AND entity_id = ? AND media_type = ? AND task_id = ?", mt.ProjectID, "scene", scene.ID, "video", scene.VideoTaskID).First(&candidate).Error
		if err == nil {
			if err := s.artifactDependencies.LinkTx(tx, mt.ProjectID, ArtifactRef{Kind: ArtifactKindSceneVideoCandidate, ID: candidate.ID}, candidate.TaskID, downstream); err != nil {
				return err
			}
		} else if err != gorm.ErrRecordNotFound {
			return err
		}
	}
	var snapshot mergeInputSnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return fmt.Errorf("解析合并输入快照失败: %w", err)
	}
	for _, layer := range snapshot.AudioLayers {
		if err := s.artifactDependencies.LinkTx(tx, mt.ProjectID, ArtifactRef{Kind: ArtifactKindAudioLayer, ID: layer.ID}, fmt.Sprintf("%s:%.3f:%.3f:%.3f:%t:%s", layer.File, layer.StartTime, layer.EndTime, layer.Volume, layer.Stale, layer.Status), downstream); err != nil {
			return err
		}
	}
	for _, cue := range snapshot.ScreenTextCues {
		if err := s.artifactDependencies.LinkTx(tx, mt.ProjectID, ArtifactRef{Kind: ArtifactKindScreenTextCue, ID: cue.ID}, cue.ReviewStatus, downstream); err != nil {
			return err
		}
	}
	return nil
}
