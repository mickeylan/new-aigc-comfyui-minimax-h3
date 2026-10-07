package service

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"comfyui-console/internal/models"
)

type screenTextASSCue struct {
	Cue        models.ScreenTextCue
	Start, End float64
}

func assTime(seconds float64) string {
	if seconds < 0 || math.IsNaN(seconds) || math.IsInf(seconds, 0) {
		seconds = 0
	}
	centiseconds := int64(math.Round(seconds * 100))
	hours := centiseconds / 360000
	centiseconds %= 360000
	minutes := centiseconds / 6000
	centiseconds %= 6000
	secs := centiseconds / 100
	cs := centiseconds % 100
	return fmt.Sprintf("%d:%02d:%02d.%02d", hours, minutes, secs, cs)
}

func escapeASSText(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, "{", `\{`)
	value = strings.ReplaceAll(value, "}", `\}`)
	value = strings.ReplaceAll(value, "\r\n", `\N`)
	value = strings.ReplaceAll(value, "\n", `\N`)
	value = strings.ReplaceAll(value, "\r", `\N`)
	return value
}

func verticalASSColumns(value string, maxRows int, rightToLeft bool) string {
	runes := []rune(strings.TrimSpace(value))
	if maxRows <= 0 {
		maxRows = 8
	}
	if len(runes) == 0 {
		return ""
	}
	columns := make([]string, 0, (len(runes)+maxRows-1)/maxRows)
	for start := 0; start < len(runes); start += maxRows {
		end := start + maxRows
		if end > len(runes) {
			end = len(runes)
		}
		parts := make([]string, 0, end-start)
		for _, r := range runes[start:end] {
			parts = append(parts, escapeASSText(string(r)))
		}
		columns = append(columns, strings.Join(parts, `\N`))
	}
	if rightToLeft {
		for left, right := 0, len(columns)-1; left < right; left, right = left+1, right-1 {
			columns[left], columns[right] = columns[right], columns[left]
		}
	}
	// ASS has no native vertical typesetting. Multiple columns are separated with
	// ideographic spaces; short names and title cards remain deterministic. The
	// future PNG renderer can replace this without changing cue persistence.
	return strings.Join(columns, "　")
}

func assAlignment(anchor string) int {
	switch anchor {
	case "top_left":
		return 7
	case "top_center":
		return 8
	case "top_right":
		return 9
	case "center":
		return 5
	case "bottom_left":
		return 1
	case "bottom_right":
		return 3
	case "subject_left":
		return 4
	case "subject_right":
		return 6
	default:
		return 2
	}
}

type screenTextASSStyle struct {
	FontSize, SubtextSize int
	PrimaryColor          string
	OutlineColor          string
	Outline, Shadow       int
	Spacing               int
	ForceCenter           bool
}

func resolvedScreenTextStyle(cue models.ScreenTextCue) screenTextASSStyle {
	style := screenTextASSStyle{FontSize: 54, SubtextSize: 28, PrimaryColor: "&H00F4E4C0&", OutlineColor: "&H802B1C12&", Outline: 2, Shadow: 2, Spacing: 3}
	switch cue.StyleCode {
	case "xianxia-character-vertical":
		style.FontSize, style.SubtextSize, style.Spacing = 68, 32, 7
	case "xianxia-location-vertical":
		style.FontSize, style.SubtextSize, style.Spacing = 60, 28, 6
	case "historical-time-card":
		style.FontSize, style.SubtextSize, style.Spacing = 64, 30, 5
	case "ink-transition-card":
		style.FontSize, style.SubtextSize, style.Spacing, style.ForceCenter = 72, 32, 8, true
		style.PrimaryColor, style.OutlineColor = "&H00E8E3D8&", "&HA0000000&"
	case "ink-end-card":
		style.FontSize, style.SubtextSize, style.Spacing, style.ForceCenter = 88, 36, 10, true
		style.PrimaryColor, style.OutlineColor, style.Outline, style.Shadow = "&H00F2DFC0&", "&H90000000&", 3, 3
	case "modern-horizontal-caption":
		style.FontSize, style.SubtextSize, style.Spacing = 46, 26, 2
		style.PrimaryColor, style.OutlineColor = "&H00FFFFFF&", "&H90000000&"
	}
	if cue.Kind == "end_card" || cue.Kind == "chapter_title" {
		style.ForceCenter = true
	}
	return style
}

func anchorPosition(anchor string, width, height int) (int, int) {
	points := map[string][2]float64{
		"top_left": {.1, .1}, "top_center": {.5, .1}, "top_right": {.9, .1},
		"center": {.5, .5}, "bottom_left": {.1, .9}, "bottom_center": {.5, .9}, "bottom_right": {.9, .9},
		"subject_left": {.25, .5}, "subject_right": {.75, .5},
	}
	point, ok := points[anchor]
	if !ok {
		point = points["bottom_center"]
	}
	return int(math.Round(point[0] * float64(width))), int(math.Round(point[1] * float64(height)))
}

func karaokeASSText(value string, duration float64) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) == 0 {
		return ""
	}
	step := int(math.Max(1, math.Round(duration*100*0.65/float64(len(runes)))))
	var out strings.Builder
	for _, r := range runes {
		fmt.Fprintf(&out, `{\kf%d}%s`, step, escapeASSText(string(r)))
	}
	return out.String()
}

func screenTextToASS(cue models.ScreenTextCue, width, height int, duration float64) string {
	text := escapeASSText(cue.Text)
	if cue.WritingMode == "vertical-rl" || cue.WritingMode == "stacked-upright" {
		text = verticalASSColumns(cue.Text, 8, true)
	} else if cue.WritingMode == "vertical-lr" {
		text = verticalASSColumns(cue.Text, 8, false)
	}
	style := resolvedScreenTextStyle(cue)
	if cue.Animation == "typewriter" && cue.WritingMode == "horizontal-ltr" {
		text = karaokeASSText(cue.Text, duration)
	}
	if subtext := strings.TrimSpace(cue.Subtext); subtext != "" {
		text += fmt.Sprintf(`\N{\fs%d}`, style.SubtextSize) + escapeASSText(subtext)
	}
	alignment := assAlignment(cue.Anchor)
	if style.ForceCenter {
		alignment = 5
	}
	tags := fmt.Sprintf(`{\an%d\fs%d\c%s\3c%s\bord%d\shad%d\fsp%d}`, alignment, style.FontSize, style.PrimaryColor, style.OutlineColor, style.Outline, style.Shadow, style.Spacing)
	if cue.Anchor == "custom" && cue.PositionX != nil && cue.PositionY != nil && !style.ForceCenter {
		x := int(math.Round(*cue.PositionX * float64(width)))
		y := int(math.Round(*cue.PositionY * float64(height)))
		tags = fmt.Sprintf(`{\an5\pos(%d,%d)}`, x, y)
	}
	if style.ForceCenter {
		tags = strings.TrimSuffix(tags, "}") + fmt.Sprintf(`\pos(%d,%d)}`, width/2, height/2)
	}
	switch cue.Animation {
	case "fade":
		tags = strings.TrimSuffix(tags, "}") + `\fad(250,350)}`
	case "ink_reveal":
		// Deterministic fallback until the reviewed transparent-PNG ink mask ships.
		tags = strings.TrimSuffix(tags, "}") + `\fad(500,450)}`
	case "slide":
		x, y := anchorPosition(cue.Anchor, width, height)
		if cue.Anchor == "custom" && cue.PositionX != nil && cue.PositionY != nil {
			x, y = int(*cue.PositionX*float64(width)), int(*cue.PositionY*float64(height))
		}
		fromX := x + int(math.Round(float64(width)*.08))
		tags = strings.TrimSuffix(tags, "}") + fmt.Sprintf(`\an5\move(%d,%d,%d,%d,0,450)}`, fromX, y, x, y)
	}
	return tags + text
}

func buildScreenTextASS(cues []models.ScreenTextCue, scenes []models.Scene, durations []sceneVideo, width, height int, family string) ([]byte, int) {
	if width <= 0 {
		width = 1920
	}
	if height <= 0 {
		height = 1080
	}
	offsets := map[uint]float64{}
	cursor := 0.0
	for i, scene := range scenes {
		offsets[scene.ID] = cursor
		duration := normalizeSceneDuration(scene.Duration)
		if i < len(durations) && durations[i].dur > 0 {
			duration = durations[i].dur
		}
		cursor += duration
	}
	items := make([]screenTextASSCue, 0, len(cues))
	for _, cue := range cues {
		if !cue.Enabled || cue.ReviewStatus != "approved" {
			continue
		}
		start, end := cue.StartTime, cue.EndTime
		if cue.SceneID != nil {
			offset, exists := offsets[*cue.SceneID]
			if !exists {
				continue
			}
			start += offset
			end += offset
		}
		if end <= start || start >= cursor {
			continue
		}
		if end > cursor {
			end = cursor
		}
		items = append(items, screenTextASSCue{Cue: cue, Start: start, End: end})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Start == items[j].Start {
			return items[i].Cue.Order < items[j].Cue.Order
		}
		return items[i].Start < items[j].Start
	})
	if strings.TrimSpace(family) == "" {
		family = "Noto Sans SC"
	}
	var out strings.Builder
	fmt.Fprintf(&out, "[Script Info]\nScriptType: v4.00+\nPlayResX: %d\nPlayResY: %d\nScaledBorderAndShadow: yes\n\n", width, height)
	out.WriteString("[V4+ Styles]\nFormat: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding\n")
	fmt.Fprintf(&out, "Style: ScreenText,%s,54,&H00F4E4C0,&HFFFFFFFF,&H802B1C12,&H50000000,0,0,0,0,100,100,3,0,1,2,2,2,80,80,60,1\n\n", strings.ReplaceAll(family, ",", ""))
	out.WriteString("[Events]\nFormat: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n")
	for _, item := range items {
		fmt.Fprintf(&out, "Dialogue: 1,%s,%s,ScreenText,,0,0,0,,%s\n", assTime(item.Start), assTime(item.End), screenTextToASS(item.Cue, width, height, item.End-item.Start))
	}
	data := append([]byte{0xEF, 0xBB, 0xBF}, []byte(out.String())...)
	return data, len(items)
}
