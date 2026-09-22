package service

import (
	"math"

	"comfyui-console/internal/models"
	"strings"
	"unicode"
)

const (
	defaultChineseCharsPerSecond = 3.8
	dialogueOpeningClosingMargin = 1.2
	speakerChangePause           = 0.35
	maxSceneVideoDuration        = 15.0
)

func dialogueTextDuration(text string) float64 {
	chars, pause := 0, 0.0
	for _, r := range strings.TrimSpace(text) {
		switch r {
		case '，', ',', '、', '；', ';', ':', '：':
			pause += 0.2
		case '。', '.', '！', '!', '？', '?':
			pause += 0.45
		case '…':
			pause += 0.3
		default:
			if !unicode.IsSpace(r) {
				chars++
			}
		}
	}
	return float64(chars)/defaultChineseCharsPerSecond + pause
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
	floor := scriptDialoguesMinDuration(scene.Dialogues)
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
		if sceneDialogueDurationFloor(scene) <= maxSceneVideoDuration {
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
