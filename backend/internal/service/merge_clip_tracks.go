package service

import (
	"comfyui-console/internal/models"
	"fmt"
	"gorm.io/gorm"
	"path/filepath"
	"strconv"
	"strings"
)

// Tracks are ordered bottom-to-top and use output-local seconds after trim/speed.
// Overlay audio is intentionally excluded; dialogue/BGM/SFX use AudioLayer.
type clipVisualTrack struct {
	MaterialID  uint    `json:"material_id"`
	Start       float64 `json:"start"`
	End         float64 `json:"end"`
	SourceStart float64 `json:"source_start"`
	X           float64 `json:"x"`
	Y           float64 `json:"y"`
	Width       float64 `json:"width"`
	Opacity     float64 `json:"opacity"`
	Muted       bool    `json:"muted"`
	InputIndex  int     `json:"-"`
	IsImage     bool    `json:"-"`
}
type clipVolumePoint = models.AudioVolumePoint
type clipMarker struct {
	Time  float64 `json:"time"`
	Label string  `json:"label"`
}

func validateClipTracks(v mergeResolvedVideo, duration float64) error {
	if len(v.VisualTracks) > 8 || len(v.VolumeEnvelope) > 64 || len(v.Markers) > 100 {
		return fmt.Errorf("每段最多8条叠加轨、64个音量点、100个标记")
	}
	for _, tr := range v.VisualTracks {
		if tr.MaterialID == 0 || !finite(tr.Start) || !finite(tr.End) || tr.Start < 0 || tr.End <= tr.Start || tr.End > duration || !finite(tr.SourceStart) || tr.SourceStart < 0 || !finite(tr.X) || tr.X < 0 || tr.X > 1 || !finite(tr.Y) || tr.Y < 0 || tr.Y > 1 || !finite(tr.Width) || tr.Width < .05 || tr.Width > 1 || !finite(tr.Opacity) || tr.Opacity < 0 || tr.Opacity > 1 {
			return fmt.Errorf("叠加轨时间、位置、宽度或透明度无效")
		}
	}
	last := -1.0
	for _, p := range v.VolumeEnvelope {
		if !finite(p.Time) || p.Time < 0 || p.Time > duration || p.Time <= last || (last >= 0 && p.Time-last < .001-1e-9) || !finite(p.Gain) || p.Gain < 0 || p.Gain > 4 {
			return fmt.Errorf("音量包络须按片段秒数递增，增益0–4且不越界")
		}
		last = p.Time
	}
	for _, m := range v.Markers {
		if !finite(m.Time) || m.Time < 0 || m.Time > duration || len([]rune(m.Label)) > 100 {
			return fmt.Errorf("时间线标记无效")
		}
	}
	return nil
}
func clipEnvelopeFilter(points []clipVolumePoint) string {
	if len(points) == 0 {
		return ""
	}
	// A trim/speed mapping can create sub-millisecond boundary segments. Keep
	// sufficient precision so a valid slope never becomes division by zero.
	num := func(v float64) string { return strconv.FormatFloat(v, 'f', 9, 64) }
	expr := num(points[len(points)-1].Gain)
	for i := len(points) - 2; i >= 0; i-- {
		a, b := points[i], points[i+1]
		expr = fmt.Sprintf("if(lt(t,%s),(%s+(%s-%s)*(t-%s)/%s),%s)", num(b.Time), num(a.Gain), num(b.Gain), num(a.Gain), num(a.Time), num(b.Time-a.Time), expr)
	}
	expr = fmt.Sprintf("if(lt(t,%s),%s,%s)", num(points[0].Time), num(points[0].Gain), expr)
	return ",volume='" + expr + "':eval=frame"
}
func envelopeGainAt(points []clipVolumePoint, t float64) float64 {
	if len(points) == 0 {
		return 1
	}
	if t <= points[0].Time {
		return points[0].Gain
	}
	for i := 1; i < len(points); i++ {
		a, b := points[i-1], points[i]
		if t <= b.Time {
			return a.Gain + (b.Gain-a.Gain)*(t-a.Time)/(b.Time-a.Time)
		}
	}
	return points[len(points)-1].Gain
}
func sliceVolumeEnvelope(points []clipVolumePoint, start, end, speed float64) []clipVolumePoint {
	if len(points) == 0 {
		return nil
	}
	out := []clipVolumePoint{{Time: 0, Gain: envelopeGainAt(points, start)}}
	for _, p := range points {
		if p.Time > start && p.Time < end {
			out = append(out, clipVolumePoint{Time: (p.Time - start) / speed, Gain: p.Gain})
		}
	}
	out = append(out, clipVolumePoint{Time: (end - start) / speed, Gain: envelopeGainAt(points, end)})
	return out
}
func fingerprintClipTracks(tx *gorm.DB, project uint, clips []mergeEditClip) ([]mergeMaterialFingerprint, error) {
	result := []mergeMaterialFingerprint{}
	for _, c := range clips {
		for _, tr := range c.VisualTracks {
			id := tr.MaterialID
			var m models.Material
			if id == 0 || tx.Where("id = ? AND project_id = ? AND type IN ?", id, project, []string{"image", "video"}).First(&m).Error != nil {
				return nil, fmt.Errorf("叠加轨素材不存在或不属于项目")
			}
			clean := filepath.Clean(filepath.FromSlash(m.Path))
			if m.Path == "" || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || strings.Contains(m.Path, ":") {
				return nil, fmt.Errorf("叠加轨素材路径无效")
			}
			result = append(result, mergeMaterialFingerprint{ID: m.ID, ProjectID: project, Type: m.Type, Path: m.Path, Size: m.Size})
		}
	}
	return result, nil
}
func appendClipVisualTracks(filters []string, base string, video mergeResolvedVideo, width, index int) ([]string, string) {
	current := base
	for n, tr := range video.VisualTracks {
		if tr.Muted {
			continue
		}
		label := fmt.Sprintf("track_%d_%d", index, n)
		out := label + "out"
		scale := int(float64(width) * tr.Width)
		scale = scale / 2 * 2
		if scale < 2 {
			scale = 2
		}
		filters = append(filters, fmt.Sprintf("[%d:v]trim=start=%s:duration=%s,setpts=PTS-STARTPTS+%s/TB,fps=24,scale=%d:-2,format=rgba,colorchannelmixer=aa=%s[%s]", tr.InputIndex, ffnum(tr.SourceStart), ffnum(tr.End-tr.Start), ffnum(tr.Start), scale, ffnum(tr.Opacity), label))
		filters = append(filters, fmt.Sprintf("[%s][%s]overlay=x=(W-w)*%s:y=(H-h)*%s:eof_action=pass:repeatlast=0:enable='gte(t,%s)*lt(t,%s)'[%s]", current, label, ffnum(tr.X), ffnum(tr.Y), ffnum(tr.Start), ffnum(tr.End), out))
		current = out
	}
	return filters, current
}
