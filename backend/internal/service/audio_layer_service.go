package service

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

var ErrAudioLayerNotFound = errors.New("音频层不存在")

type AudioLayerService struct {
	db *gorm.DB
}

func NewAudioLayerService(db *gorm.DB) *AudioLayerService { return &AudioLayerService{db: db} }

type AudioLayerInput struct {
	EpisodeN    int      `json:"episode_n"`
	SceneID     *uint    `json:"scene_id"`
	Kind        string   `json:"kind"`
	Name        string   `json:"name"`
	File        string   `json:"file"`
	Description string   `json:"description"`
	StartTime   float64  `json:"start_time"`
	EndTime     float64  `json:"end_time"`
	Volume      *float64 `json:"volume"`
	FadeIn      float64  `json:"fade_in"`
	FadeOut     float64  `json:"fade_out"`
	Loop        bool     `json:"loop"`
	Muted       bool     `json:"muted"`
	Status      string   `json:"status"`
}

func isFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func normalizeAudioLayer(projectID uint, in AudioLayerInput) (models.AudioLayer, error) {
	kind := strings.ToLower(strings.TrimSpace(in.Kind))
	if kind != "soundscape" && kind != "sfx" && kind != "bgm" {
		return models.AudioLayer{}, fmt.Errorf("kind 必须是 soundscape、sfx 或 bgm")
	}
	if in.EpisodeN <= 0 {
		return models.AudioLayer{}, fmt.Errorf("episode_n 必须大于 0")
	}
	if !isFinite(in.StartTime) || !isFinite(in.EndTime) || in.StartTime < 0 || in.EndTime <= in.StartTime {
		return models.AudioLayer{}, fmt.Errorf("start_time 必须非负且 end_time 必须大于 start_time")
	}
	volume := 1.0
	if in.Volume != nil {
		volume = *in.Volume
	}
	if !isFinite(volume) || volume < 0 || volume > 2 {
		return models.AudioLayer{}, fmt.Errorf("volume 必须在 0~2 之间")
	}
	if !isFinite(in.FadeIn) || !isFinite(in.FadeOut) || in.FadeIn < 0 || in.FadeOut < 0 {
		return models.AudioLayer{}, fmt.Errorf("fade_in 和 fade_out 必须是非负数")
	}
	status := strings.TrimSpace(in.Status)
	if status == "" {
		status = "draft"
	}
	if status != "draft" && status != "ready" && status != "failed" {
		return models.AudioLayer{}, fmt.Errorf("status 必须是 draft、ready 或 failed")
	}
	return models.AudioLayer{
		ProjectID: projectID, EpisodeN: in.EpisodeN, SceneID: in.SceneID, Kind: kind,
		Name: strings.TrimSpace(in.Name), File: strings.TrimSpace(in.File), Description: strings.TrimSpace(in.Description),
		StartTime: in.StartTime, EndTime: in.EndTime, Volume: volume, FadeIn: in.FadeIn, FadeOut: in.FadeOut,
		Loop: in.Loop, Muted: in.Muted, Status: status,
	}, nil
}

func (s *AudioLayerService) validateScene(projectID uint, sceneID *uint, episodeN int) error {
	if sceneID == nil {
		return nil
	}
	var count int64
	if err := s.db.Model(&models.Scene{}).Where("id = ? AND project_id = ? AND episode_n = ?", *sceneID, projectID, episodeN).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("场景不属于当前项目或集")
	}
	return nil
}

func (s *AudioLayerService) List(projectID uint, episodeN int) ([]models.AudioLayer, error) {
	var layers []models.AudioLayer
	q := s.db.Where("project_id = ?", projectID)
	if episodeN > 0 {
		q = q.Where("episode_n = ?", episodeN)
	}
	err := q.Order("episode_n, start_time, id").Find(&layers).Error
	return layers, err
}

func (s *AudioLayerService) Create(projectID uint, in AudioLayerInput) (*models.AudioLayer, error) {
	layer, err := normalizeAudioLayer(projectID, in)
	if err != nil {
		return nil, err
	}
	if err := s.validateScene(projectID, layer.SceneID, layer.EpisodeN); err != nil {
		return nil, err
	}
	if err := s.db.Create(&layer).Error; err != nil {
		return nil, err
	}
	return &layer, nil
}

func (s *AudioLayerService) Update(projectID, id uint, in AudioLayerInput) (*models.AudioLayer, error) {
	var existing models.AudioLayer
	if err := s.db.Where("id = ? AND project_id = ?", id, projectID).First(&existing).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrAudioLayerNotFound
		}
		return nil, err
	}
	layer, err := normalizeAudioLayer(projectID, in)
	if err != nil {
		return nil, err
	}
	if err := s.validateScene(projectID, layer.SceneID, layer.EpisodeN); err != nil {
		return nil, err
	}
	updates := map[string]any{
		"episode_n": layer.EpisodeN, "scene_id": layer.SceneID, "kind": layer.Kind, "name": layer.Name,
		"file": layer.File, "description": layer.Description, "start_time": layer.StartTime, "end_time": layer.EndTime,
		"volume": layer.Volume, "fade_in": layer.FadeIn, "fade_out": layer.FadeOut, "loop": layer.Loop,
		"muted": layer.Muted, "status": layer.Status,
	}
	if err := s.db.Model(&existing).Updates(updates).Error; err != nil {
		return nil, err
	}
	if err := s.db.First(&existing, id).Error; err != nil {
		return nil, err
	}
	return &existing, nil
}

func (s *AudioLayerService) Delete(projectID, id uint) error {
	result := s.db.Where("id = ? AND project_id = ?", id, projectID).Delete(&models.AudioLayer{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrAudioLayerNotFound
	}
	return nil
}
