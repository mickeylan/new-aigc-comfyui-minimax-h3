package service

import (
	"encoding/json"
	"errors"
	"fmt"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

type ArtifactBackfillResult struct {
	Enabled             bool `json:"enabled"`
	MusicCandidateEdges int  `json:"music_candidate_edges"`
	DialogueAudioEdges  int  `json:"dialogue_audio_edges"`
	LookOutfitEdges     int  `json:"look_outfit_edges"`
	SceneAudioEdges     int  `json:"scene_audio_edges"`
	CurrentVideoEdges   int  `json:"current_video_edges"`
	Skipped             int  `json:"skipped"`
}

func (s *ArtifactDependencyService) BackfillProject(projectID uint) (ArtifactBackfillResult, error) {
	result := ArtifactBackfillResult{Enabled: s != nil && s.enabled}
	if !result.Enabled {
		return result, nil
	}
	if projectID == 0 {
		return result, fmt.Errorf("project_id is required")
	}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		managedUpstream := []string{ArtifactKindMusicCue, ArtifactKindDialogue, ArtifactKindCharacterLook, ArtifactKindDialogueAudioCandidate, ArtifactKindSceneImageCandidate, ArtifactKindContinuityFrame}
		if err := tx.Where("project_id = ? AND upstream_kind IN ?", projectID, managedUpstream).Delete(&models.ArtifactDependency{}).Error; err != nil {
			return err
		}
		var cues []models.MusicCandidate
		if err := tx.Where("project_id = ?", projectID).Find(&cues).Error; err != nil {
			return err
		}
		for _, row := range cues {
			var count int64
			if err := tx.Model(&models.MusicCueSheet{}).Where("id = ? AND project_id = ?", row.MusicCueSheetID, projectID).Count(&count).Error; err != nil {
				return err
			}
			if count != 1 {
				result.Skipped++
				continue
			}
			if err := s.LinkTx(tx, projectID, ArtifactRef{ArtifactKindMusicCue, row.MusicCueSheetID}, row.SourceVersionHash, ArtifactRef{ArtifactKindMusicCandidate, row.ID}); err != nil {
				return err
			}
			result.MusicCandidateEdges++
		}
		var dialogueCandidates []models.DialogueAudioCandidate
		if err := tx.Where("project_id = ?", projectID).Find(&dialogueCandidates).Error; err != nil {
			return err
		}
		for _, row := range dialogueCandidates {
			var count int64
			if err := tx.Model(&models.Dialogue{}).Where("id = ? AND project_id = ?", row.DialogueID, projectID).Count(&count).Error; err != nil {
				return err
			}
			if count != 1 {
				result.Skipped++
				continue
			}
			if err := s.LinkTx(tx, projectID, ArtifactRef{ArtifactKindDialogue, row.DialogueID}, row.InputHash, ArtifactRef{ArtifactKindDialogueAudioCandidate, row.ID}); err != nil {
				return err
			}
			result.DialogueAudioEdges++
		}
		var joins []models.CharacterOutfitLook
		if err := tx.Table("character_outfit_looks").Joins("JOIN character_outfits ON character_outfits.id = character_outfit_looks.outfit_id").Where("character_outfits.project_id = ?", projectID).Find(&joins).Error; err != nil {
			return err
		}
		for _, join := range joins {
			var look models.CharacterLook
			if err := tx.Where("id = ? AND project_id = ?", join.LookID, projectID).First(&look).Error; err != nil {
				if !errors.Is(err, gorm.ErrRecordNotFound) {
					return err
				}
				result.Skipped++
				continue
			}
			if err := s.LinkTx(tx, projectID, ArtifactRef{ArtifactKindCharacterLook, look.ID}, fmt.Sprint(look.Version), ArtifactRef{ArtifactKindCharacterOutfit, join.OutfitID}); err != nil {
				return err
			}
			result.LookOutfitEdges++
		}
		var sceneAudio []models.SceneAudioCandidate
		if err := tx.Where("project_id = ?", projectID).Find(&sceneAudio).Error; err != nil {
			return err
		}
		for _, audio := range sceneAudio {
			var snapshot sceneAudioSnapshot
			if err := json.Unmarshal([]byte(audio.SourceJSON), &snapshot); err != nil {
				result.Skipped++
				continue
			}
			for _, source := range snapshot.Sources {
				if source.CandidateID == 0 {
					result.Skipped++
					continue
				}
				var count int64
				if err := tx.Model(&models.DialogueAudioCandidate{}).Where("id = ? AND project_id = ?", source.CandidateID, projectID).Count(&count).Error; err != nil {
					return err
				}
				if count != 1 {
					result.Skipped++
					continue
				}
				if err := s.LinkTx(tx, projectID, ArtifactRef{ArtifactKindDialogueAudioCandidate, source.CandidateID}, source.InputHash, ArtifactRef{ArtifactKindSceneAudioCandidate, audio.ID}); err != nil {
					return err
				}
				result.SceneAudioEdges++
			}
		}
		var scenes []models.Scene
		if err := tx.Where("project_id = ? AND video_task_id != ''", projectID).Find(&scenes).Error; err != nil {
			return err
		}
		for _, scene := range scenes {
			var video models.GenerationCandidate
			if err := tx.Where("project_id = ? AND entity_type = ? AND entity_id = ? AND media_type = ? AND task_id = ?", projectID, "scene", scene.ID, "video", scene.VideoTaskID).First(&video).Error; err != nil {
				if !errors.Is(err, gorm.ErrRecordNotFound) {
					return err
				}
				result.Skipped++
				continue
			}
			var image models.GenerationCandidate
			if scene.ImageTaskID != "" && tx.Where("project_id = ? AND entity_type = ? AND entity_id = ? AND media_type = ? AND task_id = ?", projectID, "scene", scene.ID, "image", scene.ImageTaskID).First(&image).Error == nil {
				if err := s.LinkTx(tx, projectID, ArtifactRef{ArtifactKindSceneImageCandidate, image.ID}, image.TaskID, ArtifactRef{ArtifactKindSceneVideoCandidate, video.ID}); err != nil {
					return err
				}
				result.CurrentVideoEdges++
			}
			var continuity models.SceneContinuity
			if tx.Where("scene_id = ? AND selected_frame_id IS NOT NULL AND status = ?", scene.ID, "ready").First(&continuity).Error == nil && continuity.SelectedFrameID != nil {
				var frame models.FrameCandidate
				if tx.Where("id = ? AND project_id = ? AND video_task_id = ?", *continuity.SelectedFrameID, projectID, continuity.SourceVideoTaskID).First(&frame).Error == nil {
					if err := s.LinkTx(tx, projectID, ArtifactRef{ArtifactKindContinuityFrame, frame.ID}, frame.VideoTaskID, ArtifactRef{ArtifactKindSceneVideoCandidate, video.ID}); err != nil {
						return err
					}
					result.CurrentVideoEdges++
				}
			}
		}
		return nil
	})
	return result, err
}
