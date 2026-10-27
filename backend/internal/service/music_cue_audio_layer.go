package service

import (
	"errors"
	"fmt"
	"strings"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

const (
	// audioLayerSourceManual marks a user authored audio layer.
	audioLayerSourceManual = "manual"
	// audioLayerSourceMusicCue marks a layer derived from an adopted Music 3
	// candidate. Its file and timeline stay bound to the source Cue.
	audioLayerSourceMusicCue = "music_cue"
)

// upsertMusicCueAudioLayer materializes an adopted Music 3 candidate as a BGM
// audio layer. The layer is derived state: the file and the timeline always
// follow the Cue, so adopting another candidate replaces the same layer.
func upsertMusicCueAudioLayer(tx *gorm.DB, cue *models.MusicCueSheet, candidate *models.MusicCandidate) (*models.AudioLayer, error) {
	if !tx.Migrator().HasTable(&models.AudioLayer{}) {
		return nil, nil
	}
	if strings.TrimSpace(candidate.File) == "" {
		return nil, fmt.Errorf("配乐候选还没有可用的音频文件")
	}
	if cue.EndTime <= cue.StartTime {
		return nil, fmt.Errorf("配乐Cue的时间区间无效，无法落到音频层")
	}
	var layer models.AudioLayer
	err := tx.Where("project_id = ? AND source = ? AND music_cue_sheet_id = ?", cue.ProjectID, audioLayerSourceMusicCue, cue.ID).First(&layer).Error
	switch {
	case err == nil:
		updates := map[string]any{
			"music_candidate_id": candidate.ID,
			"episode_n":          cue.EpisodeN,
			"scene_id":           cue.SceneID,
			"file":               candidate.File,
			"start_time":         cue.StartTime,
			"end_time":           cue.EndTime,
			"status":             "ready",
			"stale":              false,
			"stale_reason":       "",
		}
		if err := tx.Model(&layer).Updates(updates).Error; err != nil {
			return nil, err
		}
	case errors.Is(err, gorm.ErrRecordNotFound):
		layer = models.AudioLayer{
			ProjectID:        cue.ProjectID,
			EpisodeN:         cue.EpisodeN,
			SceneID:          cue.SceneID,
			Kind:             "bgm",
			Name:             fmt.Sprintf("Music 3 · %s", cue.Kind),
			File:             candidate.File,
			Description:      fmt.Sprintf("来自配乐Cue #%d 的已采用候选", cue.ID),
			StartTime:        cue.StartTime,
			EndTime:          cue.EndTime,
			Volume:           1,
			Status:           "ready",
			Source:           audioLayerSourceMusicCue,
			MusicCueSheetID:  &cue.ID,
			MusicCandidateID: &candidate.ID,
		}
		if err := tx.Create(&layer).Error; err != nil {
			return nil, err
		}
	default:
		return nil, err
	}
	if err := tx.First(&layer, layer.ID).Error; err != nil {
		return nil, err
	}
	return &layer, nil
}

// deleteMusicCueAudioLayer removes the derived BGM layer of a Cue.
func deleteMusicCueAudioLayer(tx *gorm.DB, projectID, cueID uint) error {
	if !tx.Migrator().HasTable(&models.AudioLayer{}) {
		return nil
	}
	return tx.Where("project_id = ? AND source = ? AND music_cue_sheet_id = ?", projectID, audioLayerSourceMusicCue, cueID).
		Delete(&models.AudioLayer{}).Error
}

// markMusicCueAudioLayerStale takes the derived BGM layer out of the final mix
// when its source Cue becomes outdated.
func markMusicCueAudioLayerStale(tx *gorm.DB, projectID, cueID uint, reason string) error {
	if !tx.Migrator().HasTable(&models.AudioLayer{}) {
		return nil
	}
	return tx.Model(&models.AudioLayer{}).
		Where("project_id = ? AND source = ? AND music_cue_sheet_id = ?", projectID, audioLayerSourceMusicCue, cueID).
		Updates(map[string]any{"stale": true, "stale_reason": strings.TrimSpace(reason), "status": "draft"}).Error
}

// overlappingManualBGMLayers lists the hand authored BGM layers that cover the
// same window as a Cue. They are reported to the user instead of being
// modified, because manual layers belong to the user.
func overlappingManualBGMLayers(tx *gorm.DB, projectID uint, episodeN int, start, end float64) ([]models.AudioLayer, error) {
	if !tx.Migrator().HasTable(&models.AudioLayer{}) {
		return nil, nil
	}
	var layers []models.AudioLayer
	err := tx.Where("project_id = ? AND episode_n = ? AND kind = ? AND (source = ? OR source = '' OR source IS NULL) AND start_time < ? AND end_time > ?",
		projectID, episodeN, "bgm", audioLayerSourceManual, end, start).
		Order("start_time, id").Find(&layers).Error
	return layers, err
}
