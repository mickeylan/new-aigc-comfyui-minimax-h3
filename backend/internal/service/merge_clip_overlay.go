package service

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"

	"comfyui-console/internal/models"
)

// resolveMergeOverlay resolves only project-owned image/video materials beneath the
// configured ComfyUI input root. Material paths are never interpreted as host paths.
func (s *ProjectService) resolveMergeOverlay(projectID uint, v mergeResolvedVideo) (string, bool, error) {
	if v.OverlayMaterialID == nil {
		return "", false, nil
	}
	var material models.Material
	if err := s.db.Where("id = ? AND project_id = ? AND type IN ?", *v.OverlayMaterialID, projectID, []string{"image", "video"}).First(&material).Error; err != nil {
		return "", false, fmt.Errorf("叠加素材不存在或不属于项目")
	}
	clean := filepath.Clean(filepath.FromSlash(strings.TrimSpace(material.Path)))
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", false, fmt.Errorf("叠加素材路径无效")
	}
	if s.remote != nil && s.remote.Enabled() {
		return s.mediaPath("input", clean), material.Type == "image", nil
	}
	if s.upload == nil {
		return "", false, fmt.Errorf("叠加素材存储不可用")
	}
	return filepath.Join(s.upload.InputDir(), clean), material.Type == "image", nil
}

// appendMergeOverlay filters one overlay onto one normalized, finite clip. It
// returns a fresh output label so no filter label is consumed more than once.
func appendMergeOverlay(filters []string, baseLabel string, video mergeResolvedVideo, outputWidth, clipIndex int) ([]string, string) {
	if video.OverlayMaterialID == nil || video.OverlayInputIndex < 0 {
		return filters, baseLabel
	}
	ratio := video.OverlayWidth
	if ratio == 0 {
		ratio = .25
	}
	width := int(math.Round(float64(outputWidth) * ratio))
	if width < 2 {
		width = 2
	}
	if width%2 != 0 {
		width--
	}
	opacity := video.OverlayOpacity
	if opacity == 0 {
		opacity = 1
	}
	overlayLabel := fmt.Sprintf("overlay_src_%d", clipIndex)
	outLabel := fmt.Sprintf("vnorm%d", clipIndex)
	if len(video.VisualTracks) > 0 {
		outLabel = fmt.Sprintf("legacyOverlay%d", clipIndex)
	}
	filters = append(filters,
		fmt.Sprintf("[%d:v]setpts=PTS-STARTPTS,fps=24,scale=%d:-2,format=rgba,colorchannelmixer=aa=%s[%s]", video.OverlayInputIndex, width, ffnum(opacity), overlayLabel),
		fmt.Sprintf("[%s][%s]overlay=x=(W-w)*%s:y=(H-h)*%s:shortest=1[%s]", baseLabel, overlayLabel, ffnum(video.OverlayX), ffnum(video.OverlayY), outLabel),
	)
	return filters, outLabel
}

func buildMergeTitleCues(videos []mergeResolvedVideo) []models.ScreenTextCue {
	clips, _, err := mergeTimelineClips(videos, makeVideoDurations(videos))
	if err != nil {
		return nil
	}
	cues := make([]models.ScreenTextCue, 0, len(videos))
	for i, video := range videos {
		if strings.TrimSpace(video.Title) == "" {
			continue
		}
		style, animation := "modern-horizontal-caption", ""
		if video.Packaging != nil {
			animation = video.Packaging.TitleAnimation
			switch video.Packaging.TitleStyle {
			case "chapter":
				style = "xianxia-gold-title"
			case "nameplate":
				style = "cold-silver-runes"
			}
		}
		anchor := "bottom_center"
		if video.TitlePosition == "top" {
			anchor = "top_center"
		}
		size := video.TitleSize
		if size <= 0 {
			size = 48
		}
		duration := (clips[i].sourceEnd - clips[i].sourceStart) / mergeClipSpeed(video)
		cues = append(cues, models.ScreenTextCue{Text: video.Title, StartTime: clips[i].outStart, EndTime: clips[i].outStart + duration, WritingMode: "horizontal-ltr", Anchor: anchor, StyleCode: style, Animation: animation, FontCode: "noto-sans-sc", FontSize: size, Enabled: true, ReviewStatus: "approved", Order: i})
	}
	return cues
}
