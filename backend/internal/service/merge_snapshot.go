package service

import (
	"encoding/json"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

// captureMergeInputSnapshot records every database input that can affect the rendered merge.
func (s *ProjectService) captureMergeInputSnapshot(tx *gorm.DB, projectID uint, episodeN int, generation uint, videos []sceneVideoFingerprint, sceneIDs []uint, dub, subtitles bool, audio MergeAudioOptions) ([]byte, error) {
	snapshot := mergeInputSnapshot{
		Version: 1, EpisodeGeneration: generation, Videos: videos,
		Dub: dub, Subtitles: subtitles, NativeVolume: audio.NativeVolume,
		DialogueVolume: audio.DialogueVolume, BGMVolume: audio.BGMVolume, DialogueMix: audio.DialogueMix,
		Dialogues: []mergeDialogueFingerprint{}, AudioLayers: []mergeAudioLayerFingerprint{},
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
	return json.Marshal(snapshot)
}
