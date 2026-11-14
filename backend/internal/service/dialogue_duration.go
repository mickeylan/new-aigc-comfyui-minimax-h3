package service

import (
	"fmt"
	"math"
	"regexp"

	"comfyui-console/internal/models"
	"strconv"
	"strings"
	"unicode"
)

const (
	// Planning estimate requested for short-drama dialogue (8–10 chars/s).
	// This does not change TTS playback speed or measured audio duration.
	defaultChineseCharsPerSecond = 9.0
	dialogueOpeningClosingMargin = 0.4
	speakerChangePause           = 0.15
	// One readable on-screen action needs about 2.5 seconds in a continuous H3 shot.
	actionBeatSeconds = 2.5
	// A sparse scene may breathe a little past its content load, never to an arbitrary length.
	sceneStretchFactor     = 1.25
	minSceneStretchCeiling = 5.0
)

var (
	sceneTimelineHeaderPattern  = regexp.MustCompile(`【\s*\d+(?:\.\d+)?\s*[—–-]\s*(\d+(?:\.\d+)?)\s*秒\s*】`)
	sceneQuotedSpeechPattern    = regexp.MustCompile(`“[^”]*”|"[^"]*"`)
	sceneSpeechLabelLinePattern = regexp.MustCompile(`(?m)^\s*【(?:对白|画外对白|旁白|内心独白)[^】]*】.*$`)
	sceneSpeechAttributionTail  = regexp.MustCompile(`(?:说道|问道|答道|喊道|喝道|叫道|说|道)\s*$`)
)

// sceneActionBeats estimates the visible actions a scene's content asks for. A sentence
// that carries quoted speech is timed by the speech itself, so only silent action
// sentences count. Timeline headers are stripped: segments packed with many actions
// are exactly the overload the density check must see.
func sceneActionBeats(content string) int {
	content = sceneTimelineHeaderPattern.ReplaceAllString(content, "\n")
	content = sceneSpeechLabelLinePattern.ReplaceAllString(content, "")
	// A quote that ends its sentence also ends the speech sentence; the prose after it
	// (“……。”燕承握紧朴刀) is a new action, while “……，”他说道 stays attribution.
	content = sceneQuotedSpeechPattern.ReplaceAllStringFunc(content, func(quote string) string {
		inner := strings.TrimRight(strings.Trim(quote, "“”\""), " ")
		if strings.ContainsAny(lastRune(inner), "。！？!?…") {
			return "“”\n"
		}
		return "“”"
	})
	beats := 0
	for _, sentence := range strings.FieldsFunc(content, func(r rune) bool { return strings.ContainsRune("。！？!?；;\n", r) }) {
		if strings.Contains(sentence, "“”") || sceneSpeechAttributionTail.MatchString(sentence) {
			continue
		}
		han := 0
		for _, r := range sentence {
			if unicode.Is(unicode.Han, r) {
				han++
			}
		}
		if han >= 4 {
			beats++
		}
	}
	return beats
}

func lastRune(s string) string {
	runes := []rune(s)
	if len(runes) == 0 {
		return ""
	}
	return string(runes[len(runes)-1])
}

// sceneTimelineEnd returns the end second of an authored 【a—b秒】 timeline, or 0.
func sceneTimelineEnd(content string) float64 {
	end := 0.0
	for _, match := range sceneTimelineHeaderPattern.FindAllStringSubmatch(content, -1) {
		if value, err := strconv.ParseFloat(match[1], 64); err == nil && value > end {
			end = value
		}
	}
	return end
}

// sceneContentLoad is the time a scene needs on screen: natural speech plus the silent
// action beats around it. An authored timeline is an explicit duration and is not stretched.
func sceneContentLoad(content string, dialogueFloor float64) (load float64, authored bool) {
	if end := sceneTimelineEnd(content); end > 0 {
		return math.Max(end, dialogueFloor), true
	}
	return dialogueFloor + float64(sceneActionBeats(content))*actionBeatSeconds, false
}

// sceneDensityIssue is advisory only: it flags a scene whose content cannot be read
// on screen in its duration, or a long scene with almost nothing happening. It never
// blocks generation or changes durations.
func sceneDensityIssue(content string, speech, duration float64) string {
	if duration <= 0 {
		return ""
	}
	beats := sceneActionBeats(content)
	needed := speech + float64(beats)*actionBeatSeconds
	switch {
	// Descriptive sentences also count as beats, so only twice the readable load is flagged.
	case beats >= 3 && needed > duration*2:
		return fmt.Sprintf("过密：%.1f秒内约%d个动作%s，建议每个动作约%.1f秒，拆成连续场景或删减次要动作", duration, beats, speechNote(speech), actionBeatSeconds)
	case duration >= 7 && beats <= 1 && speech < duration/2:
		return fmt.Sprintf("过疏：%.1f秒内仅约%d个动作%s，建议与同一回合的相邻场景合并或补足原文动作", duration, beats, speechNote(speech))
	}
	return ""
}

func speechNote(speech float64) string {
	if speech <= 0 {
		return ""
	}
	return fmt.Sprintf("加%.1f秒对白", speech)
}

// sceneDensityWarnings reports advisory density issues for persisted scenes, keyed by
// scene order; dialogues are the scene's authoritative rows.
func sceneDensityWarnings(scenes []models.Scene, dialogues map[uint][]models.Dialogue) []string {
	var out []string
	for _, scene := range scenes {
		if issue := sceneDensityIssue(scene.Content, modelDialoguesMinDuration(dialogues[scene.ID]), scene.Duration); issue != "" {
			out = append(out, fmt.Sprintf("第%d场%s", scene.Order, issue))
		}
	}
	return out
}

func dialogueTextDuration(text string) float64 {
	chars, pause, clusterPause := 0, 0.0, 0.0
	text = strings.NewReplacer("<scenetrans>", "", "<cutoff>", "", "[Chinese]", "", "<d>", "", "</d>", "").Replace(text)
	for _, r := range strings.TrimSpace(text) {
		weight := 0.0
		switch r {
		case '，', ',', '、', '；', ';', ':', '：':
			weight = 0.08
		case '。', '.', '！', '!', '？', '?', '…', '—', '–':
			weight = 0.15
		default:
			if unicode.IsSpace(r) || unicode.IsPunct(r) {
				continue
			}
			pause += clusterPause
			clusterPause = 0
			chars++
		}
		// “！！！” and “……” describe one pause, not three/six pauses.
		clusterPause = math.Max(clusterPause, weight)
	}
	return float64(chars)/defaultChineseCharsPerSecond + pause + clusterPause
}

func scriptDialoguesMinDuration(dialogues []scriptDialogue) float64 {
	if len(dialogues) == 0 {
		return 0
	}
	total, previous := dialogueOpeningClosingMargin, ""
	count := 0
	for _, dialogue := range dialogues {
		text := strings.TrimSpace(dialogue.Text)
		if text == "" {
			continue
		}
		speaker := strings.TrimSpace(dialogue.Character) + "\x00" + strings.TrimSpace(dialogue.SpeechType)
		if count > 0 && speaker != previous {
			total += speakerChangePause
		}
		total += dialogueTextDuration(text)
		previous, count = speaker, count+1
	}
	if count == 0 {
		return 0
	}
	return math.Ceil(total*2) / 2
}

func sceneDialogueDurationFloor(scene scriptScene) float64 {
	floor := math.Max(scriptDialoguesMinDuration(scene.Dialogues), scene.MinDuration)
	if floor < 3 {
		floor = 3
	}
	return floor
}

func modelDialoguesMinDuration(dialogues []models.Dialogue) float64 {
	items := make([]scriptDialogue, 0, len(dialogues))
	for _, dialogue := range dialogues {
		items = append(items, scriptDialogue{Character: dialogue.Character, SpeechType: dialogue.SpeechType, Text: dialogue.Text})
	}
	return scriptDialoguesMinDuration(items)
}

// splitScenesForDialogueDuration preserves dialogue text verbatim while ensuring each generated
// video segment can play it at a natural rate. It never shortens speech to satisfy episode targets.
func splitScenesForDialogueDuration(result *scriptResult) {
	if result == nil {
		return
	}
	out := make([]scriptScene, 0, len(result.Scenes))
	for _, scene := range result.Scenes {
		// Without dialogue rows there is nothing to split; a MinDuration above the
		// single-shot cap stays one long Scene rather than losing its content.
		if sceneDialogueDurationFloor(scene) <= maxSceneVideoDuration || len(scene.Dialogues) == 0 {
			if scene.Duration < sceneDialogueDurationFloor(scene) {
				scene.Duration = sceneDialogueDurationFloor(scene)
			}
			out = append(out, scene)
			continue
		}
		chunks := splitDialogueList(scene.Dialogues)
		for i, chunk := range chunks {
			part := scene
			part.Dialogues = chunk
			if len(chunks) > 1 {
				part.Title = strings.TrimSpace(scene.Title) + "（续" + chinesePartNumber(i+1) + "）"
			}
			part.Duration = sceneDialogueDurationFloor(part)
			out = append(out, part)
		}
	}
	result.Scenes = out
}

func splitDialogueList(dialogues []scriptDialogue) [][]scriptDialogue {
	expanded := make([]scriptDialogue, 0, len(dialogues))
	for _, dialogue := range dialogues {
		for _, text := range splitDialogueText(dialogue.Text) {
			part := dialogue
			part.Text = text
			expanded = append(expanded, part)
		}
	}
	chunks, current := [][]scriptDialogue{}, []scriptDialogue{}
	for _, dialogue := range expanded {
		candidate := append(append([]scriptDialogue{}, current...), dialogue)
		if len(current) > 0 && scriptDialoguesMinDuration(candidate) > maxSceneVideoDuration {
			chunks, current = append(chunks, current), nil
		}
		current = append(current, dialogue)
	}
	if len(current) > 0 {
		chunks = append(chunks, current)
	}
	return chunks
}

func splitDialogueText(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if scriptDialoguesMinDuration([]scriptDialogue{{Text: text}}) <= maxSceneVideoDuration {
		return []string{text}
	}
	parts, start := []string{}, 0
	runes := []rune(text)
	for i, r := range runes {
		if !strings.ContainsRune("。！？!?；;", r) {
			continue
		}
		candidate := strings.TrimSpace(string(runes[start : i+1]))
		if candidate != "" {
			parts = append(parts, candidate)
		}
		start = i + 1
	}
	if tail := strings.TrimSpace(string(runes[start:])); tail != "" {
		parts = append(parts, tail)
	}
	maxChars := int(math.Floor((maxSceneVideoDuration - dialogueOpeningClosingMargin) * defaultChineseCharsPerSecond))
	if maxChars < 1 {
		maxChars = 1
	}
	safe := make([]string, 0, len(parts))
	for _, part := range parts {
		partRunes := []rune(part)
		for len(partRunes) > maxChars {
			safe = append(safe, string(partRunes[:maxChars]))
			partRunes = partRunes[maxChars:]
		}
		if len(partRunes) > 0 {
			safe = append(safe, string(partRunes))
		}
	}
	return safe
}

func chinesePartNumber(n int) string {
	values := []string{"一", "二", "三", "四", "五", "六", "七", "八", "九", "十"}
	if n >= 1 && n <= len(values) {
		return values[n-1]
	}
	return "后续"
}
