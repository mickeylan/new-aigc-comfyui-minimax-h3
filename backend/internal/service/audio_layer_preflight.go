package service

import (
	"fmt"
	"os"
	"strings"

	"comfyui-console/internal/models"
)

type AudioLayerPreflightMessage struct {
	Level   string `json:"level"`
	Code    string `json:"code"`
	Message string `json:"message"`
	LayerID uint   `json:"layer_id,omitempty"`
}

type AudioLayerPreflightLayer struct {
	ID        uint   `json:"id"`
	MixStatus string `json:"mix_status"`
}

type AudioLayerPreflight struct {
	ProjectID          uint                         `json:"project_id"`
	EpisodeN           int                          `json:"episode_n"`
	Ready              bool                         `json:"ready"`
	LoudnessAssessed   bool                         `json:"loudness_assessed"`
	LoudnessAssessment string                       `json:"loudness_assessment"`
	Messages           []AudioLayerPreflightMessage `json:"messages"`
	Layers             []AudioLayerPreflightLayer   `json:"layers"`
}

func (s *ProjectService) audioLayerFileAvailable(projectID uint, file string) (bool, error) {
	abs, err := s.audioMixMediaPath(projectID, file)
	if err != nil {
		return false, err
	}
	if s.remote != nil && s.remote.Enabled() {
		_, err = s.remote.ProbeMedia(abs)
		return err == nil, err
	}
	info, err := os.Stat(abs)
	return err == nil && info.Mode().IsRegular(), err
}

func (s *ProjectService) AudioLayerPreflight(projectID uint, episodeN int) (*AudioLayerPreflight, error) {
	var layers []models.AudioLayer
	if err := s.db.Where("project_id = ? AND episode_n = ?", projectID, episodeN).Order("start_time, id").Find(&layers).Error; err != nil {
		return nil, err
	}
	var scenes []models.Scene
	if err := s.db.Where("project_id = ? AND episode_n = ?", projectID, episodeN).Find(&scenes).Error; err != nil {
		return nil, err
	}
	sceneIDs := make(map[uint]bool, len(scenes))
	episodeDuration := 0.0
	for _, scene := range scenes {
		sceneIDs[scene.ID] = true
		if isFinite(scene.Duration) && scene.Duration > 0 {
			episodeDuration += scene.Duration
		}
	}
	result := &AudioLayerPreflight{
		ProjectID: projectID, EpisodeN: episodeN, Ready: true,
		LoudnessAssessed: false, LoudnessAssessment: "not_assessed: no safe loudness/clipping probe is available",
		Messages: []AudioLayerPreflightMessage{}, Layers: []AudioLayerPreflightLayer{},
	}
	add := func(level, code, message string, id uint) {
		result.Messages = append(result.Messages, AudioLayerPreflightMessage{Level: level, Code: code, Message: message, LayerID: id})
		if level == "error" {
			result.Ready = false
		}
	}
	if len(scenes) == 0 {
		add("error", "missing_episode_timeline", "本集没有场景时间轴，无法检查音频范围", 0)
	}
	for _, decision := range resolveAudioLayerMix(layers) {
		layer := decision.Layer
		result.Layers = append(result.Layers, AudioLayerPreflightLayer{ID: layer.ID, MixStatus: decision.Status})
		if !isFinite(layer.StartTime) || !isFinite(layer.EndTime) || layer.StartTime < 0 || layer.EndTime <= layer.StartTime || (episodeDuration > 0 && layer.EndTime > episodeDuration) {
			add("error", "invalid_timing", fmt.Sprintf("音频层 #%d 的时间范围不在本集 0–%.1f 秒内", layer.ID, episodeDuration), layer.ID)
		}
		if layer.SceneID != nil && !sceneIDs[*layer.SceneID] {
			add("error", "scene_outside_episode", fmt.Sprintf("音频层 #%d 关联的场景不属于本集", layer.ID), layer.ID)
		}
		if layer.Stale {
			reason := strings.TrimSpace(layer.StaleReason)
			if reason == "" {
				reason = "来源已过期"
			}
			add("warning", "stale", fmt.Sprintf("音频层 #%d 已过期：%s", layer.ID, reason), layer.ID)
		}
		if strings.TrimSpace(layer.File) == "" {
			add("error", "missing_file", fmt.Sprintf("音频层 #%d 没有文件", layer.ID), layer.ID)
		} else if _, err := s.audioMixMediaPath(projectID, layer.File); err != nil {
			add("error", "unsafe_file_path", fmt.Sprintf("音频层 #%d 文件路径不安全", layer.ID), layer.ID)
		} else if ok, _ := s.audioLayerFileAvailable(projectID, layer.File); !ok {
			add("error", "missing_file", fmt.Sprintf("音频层 #%d 文件不存在、不可读或不是普通文件", layer.ID), layer.ID)
		}
		switch decision.Status {
		case audioLayerMixYielded:
			add("info", "manual_bgm_yielded", fmt.Sprintf("手工 BGM 层 #%d 已被 Music 3 配乐完整覆盖，合成时将排除", layer.ID), layer.ID)
		case audioLayerMixPartialOverlap:
			add("warning", "manual_bgm_partial_overlap", fmt.Sprintf("手工 BGM 层 #%d 与 Music 3 配乐部分重叠，合成时仍会保留", layer.ID), layer.ID)
		}
	}
	return result, nil
}
