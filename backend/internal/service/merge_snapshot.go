package service

import (
	"encoding/json"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

// captureMergeInputSnapshot records every database input that can affect the rendered merge.
func (s *ProjectService) captureMergeInputSnapshot(tx *gorm.DB, projectID uint, episodeN int, generation uint, videos []sceneVideoFingerprint, sceneIDs []uint, dub, subtitles bool, audio MergeAudioOptions) ([]byte, error) {
	snapshot := mergeInputSnapshot{
		Version: 2, EpisodeGeneration: generation, Videos: videos,
		Dub: dub, Subtitles: subtitles, NativeVolume: audio.NativeVolume,
		DialogueVolume: audio.DialogueVolume, BGMVolume: audio.BGMVolume, DialogueMix: audio.DialogueMix,
		Dialogues: []mergeDialogueFingerprint{}, AudioLayers: []mergeAudioLayerFingerprint{}, ScreenTextCues: []mergeScreenTextCueFingerprint{},
	}
	if subtitles || audio.DialogueMix {
		var dialogues []models.Dialogue
		if err := tx.Where("project_id = ? AND scene_id IN ?", projectID, sceneIDs).Order("scene_id, `order`, id").Find(&dialogues).Error; err != nil {
			return nil, err
		}
		for _, d := range dialogues {
			snapshot.Dialogues = append(snapshot.Dialogues, mergeDialogueFingerprint{
				ID: d.ID, SceneID: d.SceneID, Order: d.Order, Character: d.Character, SpeechType: d.SpeechType,
				Text: d.Text, Voice: d.Voice, H3VoiceDescription: d.H3VoiceDescription, Position: d.Position,
				Offset: d.Offset, Speed: d.Speed, Pitch: d.Pitch, Volume: d.Volume, Emotion: d.Emotion,
				Delivery: d.Delivery, AudioFile: d.AudioFile, AudioHash: d.AudioHash, Status: d.Status, AudioStale: d.AudioStale,
			})
		}
	}
	var layers []models.AudioLayer
	if err := tx.Where("project_id = ? AND episode_n = ? AND status = ? AND muted = ? AND file != ''", projectID, episodeN, "ready", false).Order("start_time, id").Find(&layers).Error; err != nil {
		return nil, err
	}
	for _, layer := range layers {
		snapshot.AudioLayers = append(snapshot.AudioLayers, mergeAudioLayerFingerprint{
			ID: layer.ID, ProjectID: layer.ProjectID, EpisodeN: layer.EpisodeN, SceneID: layer.SceneID,
			Kind: layer.Kind, File: layer.File, StartTime: layer.StartTime, EndTime: layer.EndTime,
			Volume: layer.Volume, FadeIn: layer.FadeIn, FadeOut: layer.FadeOut, Loop: layer.Loop,
			Muted: layer.Muted, Stale: layer.Stale, Status: layer.Status,
		})
	}
	if tx.Migrator().HasTable(&models.ScreenTextCue{}) {
		var cues []models.ScreenTextCue
		query := tx.Where("project_id = ? AND episode_n = ? AND enabled = ? AND review_status = ?", projectID, episodeN, true, "approved")
		if len(sceneIDs) > 0 {
			query = query.Where("scene_id IS NULL OR scene_id IN ?", sceneIDs)
		}
		if err := query.Order("start_time, `order`, id").Find(&cues).Error; err != nil {
			return nil, err
		}
		for _, cue := range cues {
			snapshot.ScreenTextCues = append(snapshot.ScreenTextCues, mergeScreenTextCueFingerprint{
				ID: cue.ID, ProjectID: cue.ProjectID, EpisodeN: cue.EpisodeN, SceneID: cue.SceneID,
				ShotID: cue.ShotID, CharacterID: cue.CharacterID, Kind: cue.Kind, Text: cue.Text,
				Subtext: cue.Subtext, StartTime: cue.StartTime, EndTime: cue.EndTime,
				WritingMode: cue.WritingMode, Anchor: cue.Anchor, StyleCode: cue.StyleCode,
				Animation: cue.Animation, Enabled: cue.Enabled, Order: cue.Order, ReviewStatus: cue.ReviewStatus,
			})
		}
	}
	return json.Marshal(snapshot)
}
