package service

import (
	"fmt"
	"math"
	"strings"
	"unicode/utf8"
)

func mergeClipSpeed(v mergeResolvedVideo) float64 {
	if v.Speed == 0 {
		return 1
	}
	return v.Speed
}
func mergeClipOutputDuration(v mergeResolvedVideo) (float64, error) {
	start, end, err := resolvedMergeClipRange(v, v.SourceDuration)
	if err != nil {
		return 0, err
	}
	speed := mergeClipSpeed(v)
	if speed < .25 || speed > 4 || math.IsNaN(speed) || math.IsInf(speed, 0) {
		return 0, fmt.Errorf("片段速度无效")
	}
	return (end - start) / speed, nil
}
func mergeClipTransitionOverlap(v mergeResolvedVideo) float64 {
	if v.Transition == "" || v.Transition == "cut" {
		return 0
	}
	return v.TransitionDuration
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
func validateMergeClipEffects(v mergeResolvedVideo) error {
	d, err := mergeClipOutputDuration(v)
	if err != nil {
		return err
	}
	if err := validateVisualPackaging(v.Packaging); err != nil {
		return err
	}
	if err := validateClipTracks(v, d); err != nil {
		return err
	}
	if err := validateClipTransform(v.Transform, d); err != nil {
		return err
	}
	speed := mergeClipSpeed(v)
	if speed < .25 || speed > 4 {
		return fmt.Errorf("片段速度须在0.25到4之间")
	}
	volume := 1.0
	if v.Volume != nil {
		volume = *v.Volume
	}
	if !finite(volume) || volume < 0 || volume > 4 {
		return fmt.Errorf("片段音量须在0到4之间")
	}
	if !finite(v.FadeIn) || !finite(v.FadeOut) || v.FadeIn < 0 || v.FadeOut < 0 || v.FadeIn > d || v.FadeOut > d {
		return fmt.Errorf("片段淡入淡出时长无效")
	}
	tr := v.Transition
	if tr == "" {
		tr = "cut"
	}
	if !validMergeTransition(tr) {
		return fmt.Errorf("片段转场类型无效")
	}
	if tr == "cut" {
		if v.TransitionDuration != 0 {
			return fmt.Errorf("硬切转场时长必须为0")
		}
	} else if !finite(v.TransitionDuration) || v.TransitionDuration < .05 || v.TransitionDuration > 2 || v.TransitionDuration > d/2 {
		return fmt.Errorf("片段转场时长无效")
	}
	contrast := v.Contrast
	if contrast == 0 {
		contrast = 1
	}
	saturation := 1.0
	if v.Saturation != nil {
		saturation = *v.Saturation
	}
	if !finite(v.Brightness) || v.Brightness < -1 || v.Brightness > 1 || !finite(contrast) || contrast < .1 || contrast > 3 || !finite(saturation) || saturation < 0 || saturation > 3 {
		return fmt.Errorf("片段画面参数无效")
	}
	width := v.OverlayWidth
	if width == 0 {
		width = .25
	}
	opacity := v.OverlayOpacity
	if opacity == 0 {
		opacity = 1
	}
	if !finite(v.OverlayX) || v.OverlayX < 0 || v.OverlayX > 1 || !finite(v.OverlayY) || v.OverlayY < 0 || v.OverlayY > 1 || !finite(width) || width < .05 || width > 1 || !finite(opacity) || opacity < .05 || opacity > 1 {
		return fmt.Errorf("叠加素材参数无效")
	}
	if utf8.RuneCountInString(v.Title) > 200 {
		return fmt.Errorf("标题不能超过200字")
	}
	pos := v.TitlePosition
	if pos == "" {
		pos = "bottom"
	}
	if pos != "top" && pos != "bottom" {
		return fmt.Errorf("标题位置无效")
	}
	size := v.TitleSize
	if size == 0 {
		size = 48
	}
	if size < 16 || size > 96 {
		return fmt.Errorf("标题字号无效")
	}
	return nil
}
func validateMergeTimelineEffects(videos []mergeResolvedVideo) error {
	for i := range videos {
		if err := validateMergeClipEffects(videos[i]); err != nil {
			return fmt.Errorf("第%d个片段: %w", i+1, err)
		}
		overlap := mergeClipTransitionOverlap(videos[i])
		if i == len(videos)-1 && overlap > 0 {
			return fmt.Errorf("最后一个片段不能设置转场")
		}
		if overlap > 0 {
			next, err := mergeClipOutputDuration(videos[i+1])
			if err != nil {
				return err
			}
			if overlap > next/2 {
				return fmt.Errorf("第%d个片段转场超过下一片段时长的一半", i+1)
			}
		}
	}
	return nil
}
func resolvedVideoFromClip(c mergeEditClip, v mergeResolvedVideo) mergeResolvedVideo {
	v.VisualTracks = append([]clipVisualTrack(nil), c.VisualTracks...)
	v.VolumeEnvelope = c.VolumeEnvelope
	v.Markers = c.Markers
	v.Packaging = c.Packaging
	v.Transform = c.Transform
	v.Speed = c.Speed
	v.Volume = c.Volume
	v.FadeIn = c.FadeIn
	v.FadeOut = c.FadeOut
	v.Transition = c.Transition
	v.TransitionDuration = c.TransitionDuration
	v.Brightness = c.Brightness
	v.Contrast = c.Contrast
	v.Saturation = c.Saturation
	v.OverlayMaterialID = c.OverlayMaterialID
	v.OverlayX = c.OverlayX
	v.OverlayY = c.OverlayY
	v.OverlayWidth = c.OverlayWidth
	v.OverlayOpacity = c.OverlayOpacity
	v.Title = strings.TrimSpace(c.Title)
	v.TitlePosition = c.TitlePosition
	v.TitleSize = c.TitleSize
	return v
}
