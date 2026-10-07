package service

import (
	"fmt"
	"strings"

	"comfyui-console/internal/models"
)

type ScreenTextPreflightIssue struct {
	CueID    uint   `json:"cue_id,omitempty"`
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
}

type ScreenTextRenderPreview struct {
	CueID             uint   `json:"cue_id"`
	Text              string `json:"text"`
	StyleCode         string `json:"style_code"`
	FontFamily        string `json:"font_family"`
	FontSize          int    `json:"font_size"`
	SubtextSize       int    `json:"subtext_size"`
	PrimaryColor      string `json:"primary_color"`
	OutlineColor      string `json:"outline_color"`
	WritingMode       string `json:"writing_mode"`
	Anchor            string `json:"anchor"`
	Animation         string `json:"animation"`
	AnimationRendered string `json:"animation_rendered"`
	ASSEvent          string `json:"ass_event"`
}

type ScreenTextPreflightResult struct {
	OK           bool                       `json:"ok"`
	EpisodeN     int                        `json:"episode_n"`
	Width        int                        `json:"width"`
	Height       int                        `json:"height"`
	FontCode     string                     `json:"font_code"`
	FontFamily   string                     `json:"font_family"`
	FontVerified bool                       `json:"font_verified"`
	Issues       []ScreenTextPreflightIssue `json:"issues"`
	Previews     []ScreenTextRenderPreview  `json:"previews"`
}

func renderedAnimation(animation string) string {
	switch animation {
	case "ink_reveal":
		return "slow_fade_fallback"
	case "typewriter":
		return "ass_karaoke_reveal"
	case "slide":
		return "ass_move"
	case "fade":
		return "ass_fade"
	default:
		return "static"
	}
}

func (s *ScreenTextService) Preflight(projectID uint, episodeN, width, height int, fonts *FontRegistry) (ScreenTextPreflightResult, error) {
	if episodeN <= 0 {
		return ScreenTextPreflightResult{}, fmt.Errorf("episode_n 必须大于 0")
	}
	if width <= 0 {
		width = 1920
	}
	if height <= 0 {
		height = 1080
	}
	result := ScreenTextPreflightResult{EpisodeN: episodeN, Width: width, Height: height, FontCode: "noto-sans-sc", Issues: []ScreenTextPreflightIssue{}, Previews: []ScreenTextRenderPreview{}}
	font, err := fonts.Approved(result.FontCode)
	if err != nil {
		result.Issues = append(result.Issues, ScreenTextPreflightIssue{Severity: "error", Code: "font_unverified", Message: err.Error()})
	} else {
		result.FontVerified = true
		result.FontFamily = font.Family
	}
	var cues []models.ScreenTextCue
	if err := s.db.Where("project_id = ? AND episode_n = ? AND enabled = ? AND review_status = ?", projectID, episodeN, true, "approved").Order("start_time, `order`, id").Find(&cues).Error; err != nil {
		return ScreenTextPreflightResult{}, err
	}
	for i := range cues {
		cue := &cues[i]
		if err := s.validateBindings(cue); err != nil {
			result.Issues = append(result.Issues, ScreenTextPreflightIssue{CueID: cue.ID, Severity: "error", Code: "invalid_binding", Message: err.Error()})
		}
		if err := s.validateProductionRules(cue, cue.ID); err != nil {
			result.Issues = append(result.Issues, ScreenTextPreflightIssue{CueID: cue.ID, Severity: "error", Code: "production_rule", Message: err.Error()})
		}
		style := resolvedScreenTextStyle(*cue)
		family := result.FontFamily
		if strings.TrimSpace(family) == "" {
			family = "Noto Sans SC"
		}
		result.Previews = append(result.Previews, ScreenTextRenderPreview{
			CueID: cue.ID, Text: cue.Text, StyleCode: cue.StyleCode, FontFamily: family,
			FontSize: style.FontSize, SubtextSize: style.SubtextSize, PrimaryColor: style.PrimaryColor,
			OutlineColor: style.OutlineColor, WritingMode: cue.WritingMode, Anchor: cue.Anchor,
			Animation: cue.Animation, AnimationRendered: renderedAnimation(cue.Animation),
			ASSEvent: screenTextToASS(*cue, width, height, cue.EndTime-cue.StartTime),
		})
		if cue.Animation == "ink_reveal" {
			result.Issues = append(result.Issues, ScreenTextPreflightIssue{CueID: cue.ID, Severity: "warning", Code: "ink_reveal_fallback", Message: "墨迹显字当前按慢淡入渲染，尚未使用透明墨迹遮罩"})
		}
	}
	result.OK = result.FontVerified
	for _, issue := range result.Issues {
		if issue.Severity == "error" {
			result.OK = false
			break
		}
	}
	return result, nil
}
