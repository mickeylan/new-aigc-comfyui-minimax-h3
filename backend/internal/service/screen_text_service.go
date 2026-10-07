package service

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

var ErrScreenTextCueNotFound = errors.New("功能文字不存在")

type ScreenTextService struct{ db *gorm.DB }

func NewScreenTextService(db *gorm.DB) *ScreenTextService { return &ScreenTextService{db: db} }

type ScreenTextCueInput struct {
	EpisodeN     int      `json:"episode_n"`
	SceneID      *uint    `json:"scene_id"`
	ShotID       *uint    `json:"shot_id"`
	CharacterID  *uint    `json:"character_id"`
	Kind         string   `json:"kind"`
	Text         string   `json:"text"`
	Subtext      string   `json:"subtext"`
	StartTime    float64  `json:"start_time"`
	EndTime      float64  `json:"end_time"`
	WritingMode  string   `json:"writing_mode"`
	Anchor       string   `json:"anchor"`
	PositionX    *float64 `json:"position_x"`
	PositionY    *float64 `json:"position_y"`
	StyleCode    string   `json:"style_code"`
	Animation    string   `json:"animation"`
	Enabled      *bool    `json:"enabled"`
	Order        int      `json:"order"`
	Source       string   `json:"source"`
	ReviewStatus string   `json:"review_status"`
}

var screenTextKinds = map[string]bool{
	"character_intro": true, "location": true, "time_card": true,
	"transition": true, "chapter_title": true, "story_note": true,
	"end_card": true, "custom": true,
}

var screenTextStyles = map[string]bool{
	"xianxia-character-vertical": true, "xianxia-location-vertical": true,
	"historical-time-card": true, "ink-transition-card": true,
	"ink-end-card": true, "modern-horizontal-caption": true,
}

var screenTextWritingModes = map[string]bool{
	"horizontal-ltr": true, "vertical-rl": true, "vertical-lr": true, "stacked-upright": true,
}

var screenTextAnchors = map[string]bool{
	"top_left": true, "top_center": true, "top_right": true, "center": true,
	"bottom_left": true, "bottom_center": true, "bottom_right": true,
	"subject_left": true, "subject_right": true, "custom": true,
}

func normalizeScreenTextCue(projectID uint, in ScreenTextCueInput) (models.ScreenTextCue, error) {
	kind := strings.ToLower(strings.TrimSpace(in.Kind))
	if !screenTextKinds[kind] {
		return models.ScreenTextCue{}, fmt.Errorf("不支持的功能文字类型")
	}
	if in.EpisodeN <= 0 {
		return models.ScreenTextCue{}, fmt.Errorf("episode_n 必须大于 0")
	}
	text := strings.TrimSpace(in.Text)
	if text == "" {
		return models.ScreenTextCue{}, fmt.Errorf("功能文字不能为空")
	}
	if len([]rune(text)) > 200 || len([]rune(strings.TrimSpace(in.Subtext))) > 200 {
		return models.ScreenTextCue{}, fmt.Errorf("功能文字或副标题不能超过 200 个字符")
	}
	if math.IsNaN(in.StartTime) || math.IsInf(in.StartTime, 0) || math.IsNaN(in.EndTime) || math.IsInf(in.EndTime, 0) || in.StartTime < 0 || in.EndTime <= in.StartTime {
		return models.ScreenTextCue{}, fmt.Errorf("start_time 必须非负且 end_time 必须大于 start_time")
	}
	writingMode := strings.ToLower(strings.TrimSpace(in.WritingMode))
	if writingMode == "" {
		writingMode = "horizontal-ltr"
	}
	if !screenTextWritingModes[writingMode] {
		return models.ScreenTextCue{}, fmt.Errorf("不支持的排版方向")
	}
	anchor := strings.ToLower(strings.TrimSpace(in.Anchor))
	if anchor == "" {
		anchor = "bottom_center"
	}
	if !screenTextAnchors[anchor] {
		return models.ScreenTextCue{}, fmt.Errorf("不支持的文字位置")
	}
	var positionX, positionY *float64
	if anchor == "custom" {
		if in.PositionX == nil || in.PositionY == nil || !isFinite(*in.PositionX) || !isFinite(*in.PositionY) {
			return models.ScreenTextCue{}, fmt.Errorf("自定义位置必须填写有效的 position_x 和 position_y")
		}
		if *in.PositionX < 0.05 || *in.PositionX > 0.95 || *in.PositionY < 0.05 || *in.PositionY > 0.95 {
			return models.ScreenTextCue{}, fmt.Errorf("自定义位置必须位于画面 5%%~95%% 安全区")
		}
		x, y := *in.PositionX, *in.PositionY
		positionX, positionY = &x, &y
	}
	styleCode := strings.ToLower(strings.TrimSpace(in.StyleCode))
	if styleCode == "" {
		styleCode = "modern-horizontal-caption"
	}
	if !screenTextStyles[styleCode] {
		return models.ScreenTextCue{}, fmt.Errorf("不支持的功能文字样式")
	}
	animation := strings.ToLower(strings.TrimSpace(in.Animation))
	if animation == "" {
		animation = "fade"
	}
	if animation != "none" && animation != "fade" && animation != "slide" && animation != "typewriter" && animation != "ink_reveal" {
		return models.ScreenTextCue{}, fmt.Errorf("不支持的文字动画")
	}
	source := strings.ToLower(strings.TrimSpace(in.Source))
	if source == "" {
		source = "manual"
	}
	if source != "manual" && source != "ai" {
		return models.ScreenTextCue{}, fmt.Errorf("source 必须是 manual 或 ai")
	}
	reviewStatus := strings.ToLower(strings.TrimSpace(in.ReviewStatus))
	if reviewStatus == "" {
		reviewStatus = "approved"
	}
	if reviewStatus != "draft" && reviewStatus != "approved" && reviewStatus != "rejected" {
		return models.ScreenTextCue{}, fmt.Errorf("review_status 必须是 draft、approved 或 rejected")
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	return models.ScreenTextCue{
		ProjectID: projectID, EpisodeN: in.EpisodeN, SceneID: in.SceneID, ShotID: in.ShotID,
		CharacterID: in.CharacterID, Kind: kind, Text: text, Subtext: strings.TrimSpace(in.Subtext),
		StartTime: in.StartTime, EndTime: in.EndTime, WritingMode: writingMode, Anchor: anchor,
		PositionX: positionX, PositionY: positionY, StyleCode: styleCode, Animation: animation, Enabled: enabled,
		Order: in.Order, Source: source, ReviewStatus: reviewStatus,
	}, nil
}

func (s *ScreenTextService) validateProductionRules(cue *models.ScreenTextCue, excludeID uint) error {
	if !cue.Enabled || cue.ReviewStatus != "approved" {
		return nil
	}
	if cue.Kind == "character_intro" && cue.CharacterID != nil {
		var count int64
		query := s.db.Model(&models.ScreenTextCue{}).Where("project_id = ? AND episode_n = ? AND kind = ? AND character_id = ? AND enabled = ? AND review_status = ?", cue.ProjectID, cue.EpisodeN, "character_intro", *cue.CharacterID, true, "approved")
		if excludeID > 0 {
			query = query.Where("id <> ?", excludeID)
		}
		if err := query.Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return fmt.Errorf("该角色本集已有已审核的人物出场题名")
		}
	}
	if cue.Kind == "end_card" {
		if cue.SceneID == nil {
			return fmt.Errorf("本集完必须绑定本集最后一个场景")
		}
		var last models.Scene
		if err := s.db.Where("project_id = ? AND episode_n = ?", cue.ProjectID, cue.EpisodeN).Order("`order` DESC, id DESC").First(&last).Error; err != nil {
			return err
		}
		if last.ID != *cue.SceneID {
			return fmt.Errorf("本集完只能绑定本集最后一个场景")
		}
		if cue.EndTime < last.Duration-0.001 {
			return fmt.Errorf("本集完必须持续到最后场景结尾")
		}
	}
	if cue.SceneID != nil {
		var count int64
		query := s.db.Model(&models.ScreenTextCue{}).Where("project_id = ? AND episode_n = ? AND scene_id = ? AND anchor = ? AND enabled = ? AND review_status = ? AND start_time < ? AND end_time > ?", cue.ProjectID, cue.EpisodeN, *cue.SceneID, cue.Anchor, true, "approved", cue.EndTime, cue.StartTime)
		if excludeID > 0 {
			query = query.Where("id <> ?", excludeID)
		}
		if err := query.Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return fmt.Errorf("同一位置已有时间重叠的功能文字，请调整时间或位置")
		}
	}
	return nil
}

func (s *ScreenTextService) validateBindings(cue *models.ScreenTextCue) error {
	if cue.SceneID != nil {
		var scene models.Scene
		if err := s.db.Where("id = ? AND project_id = ? AND episode_n = ?", *cue.SceneID, cue.ProjectID, cue.EpisodeN).First(&scene).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("场景不属于当前项目或集")
			}
			return err
		}
		if cue.EndTime > scene.Duration+0.001 {
			return fmt.Errorf("功能文字结束时间不能超过场景时长 %.1f 秒", scene.Duration)
		}
	}
	if cue.ShotID != nil {
		if cue.SceneID == nil {
			return fmt.Errorf("绑定镜头时必须同时绑定场景")
		}
		var count int64
		if err := s.db.Model(&models.Shot{}).Where("id = ? AND scene_id = ? AND project_id = ?", *cue.ShotID, *cue.SceneID, cue.ProjectID).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return fmt.Errorf("镜头不属于绑定场景")
		}
	}
	if cue.CharacterID != nil {
		var count int64
		if err := s.db.Model(&models.Character{}).Where("id = ? AND project_id = ?", *cue.CharacterID, cue.ProjectID).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return fmt.Errorf("角色不属于当前项目")
		}
	}
	if cue.Kind == "character_intro" && cue.CharacterID == nil {
		return fmt.Errorf("人物出场文字必须绑定角色")
	}
	return nil
}

func (s *ScreenTextService) List(projectID uint, episodeN int) ([]models.ScreenTextCue, error) {
	var cues []models.ScreenTextCue
	q := s.db.Where("project_id = ?", projectID)
	if episodeN > 0 {
		q = q.Where("episode_n = ?", episodeN)
	}
	err := q.Order("episode_n, start_time, `order`, id").Find(&cues).Error
	return cues, err
}

func (s *ScreenTextService) Create(projectID uint, in ScreenTextCueInput) (*models.ScreenTextCue, error) {
	cue, err := normalizeScreenTextCue(projectID, in)
	if err != nil {
		return nil, err
	}
	if err := s.validateBindings(&cue); err != nil {
		return nil, err
	}
	if err := s.validateProductionRules(&cue, 0); err != nil {
		return nil, err
	}
	if err := s.db.Create(&cue).Error; err != nil {
		return nil, err
	}
	return &cue, nil
}

func (s *ScreenTextService) Update(projectID, id uint, in ScreenTextCueInput) (*models.ScreenTextCue, error) {
	var existing models.ScreenTextCue
	if err := s.db.Where("id = ? AND project_id = ?", id, projectID).First(&existing).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrScreenTextCueNotFound
		}
		return nil, err
	}
	cue, err := normalizeScreenTextCue(projectID, in)
	if err != nil {
		return nil, err
	}
	if err := s.validateBindings(&cue); err != nil {
		return nil, err
	}
	if err := s.validateProductionRules(&cue, id); err != nil {
		return nil, err
	}
	updates := map[string]any{
		"episode_n": cue.EpisodeN, "scene_id": cue.SceneID, "shot_id": cue.ShotID,
		"character_id": cue.CharacterID, "kind": cue.Kind, "text": cue.Text, "subtext": cue.Subtext,
		"start_time": cue.StartTime, "end_time": cue.EndTime, "writing_mode": cue.WritingMode,
		"anchor": cue.Anchor, "position_x": cue.PositionX, "position_y": cue.PositionY,
		"style_code": cue.StyleCode, "animation": cue.Animation,
		"enabled": cue.Enabled, "order": cue.Order, "source": cue.Source, "review_status": cue.ReviewStatus,
	}
	if err := s.db.Model(&existing).Updates(updates).Error; err != nil {
		return nil, err
	}
	if err := s.db.First(&existing, id).Error; err != nil {
		return nil, err
	}
	return &existing, nil
}

func (s *ScreenTextService) Delete(projectID, id uint) error {
	result := s.db.Where("id = ? AND project_id = ?", id, projectID).Delete(&models.ScreenTextCue{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrScreenTextCueNotFound
	}
	return nil
}
