package service

import (
	"fmt"
	"strings"

	"comfyui-console/internal/models"
)

// Display timing only. No audio synthesis, probing, rune-proportional timing or DB writes.
type studioExecutionDialogueLine struct {
	ShotID     uint    `json:"shot_id"`
	DialogueID uint    `json:"dialogue_id"`
	Speaker    string  `json:"speaker"`
	SpeechType string  `json:"speech_type"`
	Text       string  `json:"text"`
	Start      float64 `json:"start"`
	End        float64 `json:"end"`
}
type studioExecutionDialogueTimeline struct {
	Version  int                           `json:"version"`
	Lines    []studioExecutionDialogueLine `json:"lines"`
	Blockers []string                      `json:"blockers"`
}

func buildStudioExecutionDialogueTimeline(scene models.Scene, shots []models.Shot, dialogues []models.Dialogue) studioExecutionDialogueTimeline {
	result := studioExecutionDialogueTimeline{Version: 1, Lines: []studioExecutionDialogueLine{}, Blockers: []string{}}
	timed := withSceneDialogueTiming(&scene, dialogues)
	byID := map[uint]models.Dialogue{}
	uses := map[uint]int{}
	groups := map[string]map[uint]bool{}
	for _, d := range timed {
		byID[d.ID] = d
	}
	for _, shot := range shots {
		for _, r := range shot.DialogueRanges {
			uses[r.DialogueID]++
			if groups[r.GroupKey] == nil {
				groups[r.GroupKey] = map[uint]bool{}
			}
			groups[r.GroupKey][r.DialogueID] = true
		}
	}
	cursor := 0.0
	for _, shot := range shots {
		shotStart, shotEnd := cursor, cursor+shot.Duration
		cursor = shotEnd
		previousEnd := shotStart
		if len(shot.DialogueRanges) == 0 && strings.TrimSpace(shot.Dialogue) != "" {
			result.Blockers = append(result.Blockers, fmt.Sprintf("镜%d：对白缓存没有权威Dialogue引用", shot.ID))
		}
		for _, r := range shot.DialogueRanges {
			d, ok := byID[r.DialogueID]
			fail := func(reason string) {
				result.Blockers = append(result.Blockers, fmt.Sprintf("镜%d对白%d：%s", shot.ID, r.DialogueID, reason))
			}
			if !ok {
				fail("引用不存在或不属于本场次")
				continue
			}
			// Group ranges may refer to concatenated canonical text. Only a unique full
			// row is unambiguous here; never approximate split syllable/character timing.
			if len(groups[r.GroupKey]) != 1 || uses[d.ID] != 1 || r.StartRune != 0 || r.EndRune != len([]rune(canonicalDialogueText(d.Text))) {
				fail("跨镜或局部rune区间缺少精确分句时序")
				continue
			}
			start, end, valid := parseDialogueTimeSegment(d.TimeSegment)
			if !valid || !previsFinite(start, 0, 86400) || !previsFinite(end, 0, 86400) || start < shotStart || end > shotEnd {
				fail("缺少本镜内明确对白时间段")
				continue
			}
			if start < previousEnd {
				fail("重叠或乱序对白超出现有单行字幕采样能力")
				continue
			}
			previousEnd = end
			kind, speaker := normalizeScriptSpeech(d.SpeechType, d.Character)
			if strings.TrimSpace(speaker) == "" || strings.TrimSpace(d.Text) == "" {
				fail("缺少明确说话人或可显示原文")
				continue
			}
			result.Lines = append(result.Lines, studioExecutionDialogueLine{ShotID: shot.ID, DialogueID: d.ID, Speaker: speaker, SpeechType: kind, Text: d.Text, Start: start, End: end})
		}
	}
	for _, d := range timed {
		if strings.TrimSpace(d.Text) != "" && uses[d.ID] == 0 {
			result.Blockers = append(result.Blockers, fmt.Sprintf("对白%d尚未关联导演镜头，不可静默丢弃", d.ID))
		}
	}
	return result
}
