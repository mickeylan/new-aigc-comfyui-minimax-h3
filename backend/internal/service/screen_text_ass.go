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

func screenTextToASS(cue models.ScreenTextCue) string {
	text := escapeASSText(cue.Text)
	if cue.WritingMode == "vertical-rl" || cue.WritingMode == "stacked-upright" {
		text = verticalASSColumns(cue.Text, 8, true)
	} else if cue.WritingMode == "vertical-lr" {
		text = verticalASSColumns(cue.Text, 8, false)
	}
	if subtext := strings.TrimSpace(cue.Subtext); subtext != "" {
		text += `\N{\fs28}` + escapeASSText(subtext)
	}
	tags := fmt.Sprintf(`{\an%d}`, assAlignment(cue.Anchor))
	if cue.Animation == "fade" || cue.Animation == "ink_reveal" {
		tags = strings.TrimSuffix(tags, "}") + `\fad(250,350)}`
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
	fmt.Fprintf(&out, "Style: ScreenText,%s,54,&H00F4E4C0,&H00FFFFFF,&H802B1C12,&H50000000,0,0,0,0,100,100,3,0,1,2,2,2,80,80,60,1\n\n", strings.ReplaceAll(family, ",", ""))
	out.WriteString("[Events]\nFormat: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n")
	for _, item := range items {
		fmt.Fprintf(&out, "Dialogue: 1,%s,%s,ScreenText,,0,0,0,,%s\n", assTime(item.Start), assTime(item.End), screenTextToASS(item.Cue))
	}
	data := append([]byte{0xEF, 0xBB, 0xBF}, []byte(out.String())...)
	return data, len(items)
}
