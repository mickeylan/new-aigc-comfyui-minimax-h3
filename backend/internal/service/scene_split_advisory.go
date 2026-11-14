package service

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"comfyui-console/internal/models"
)

// sceneSplitRule is shared by every episode-script prompt: a Scene is one independent
// H3 generation, so its size and count follow the plot and the 2.5 s action capacity.
const sceneSplitRule = "【场景拆分】每个场景是一次独立的视频生成，承载同一时空、同一回合的一段连续剧情，时长5~15秒（只有完整对白自然说完需要更久时才可更长）；场景内部可以按剧情切换机位（说话人、听者反应、关键细节特写等），场景拆分只决定剧情段落，不等于一镜到底。按每个可见动作约2.5秒安排容量：场景内动作数约为时长÷2.5，6秒场景不得塞入6个分秒动作，7秒以上场景不得只有一个静态动作。同一回合的连续招式（出手、格挡、反击、受制、挣脱）必须放在同一场景，不得把一个动作的起势与结果拆进两个场景。场景数完全随剧情发展：不设上限，也不为凑目标镜头数或总时长而拆分、合并或注水；总时长原则上不少于目标，原文内容确实不够时不强求，也绝不压缩对白。拟声词与音效（如“轰”“砰”“嗖”、破空声、撞击声）只写进content的动作描写，不得作为dialogues条目。"

var (
	soundEffectSpeakerPattern = regexp.MustCompile(`(?:声|音效|拟声)$`)
	sceneLabeledSpeechPattern = regexp.MustCompile(`(?m)【(?:对白|画外对白|内心独白)[｜|]([^】]+)】([^\r\n]*)`)
)

// onomatopoeiaRunes are characters that, on their own, spell a sound rather than speech.
const onomatopoeiaRunes = "轰砰嗖噗咔嚓哗啦呼铛锵叮咚嗤唰喀嘭隆吱嘎咻飕刷啪咣当哐噼嘶簌"

func isOnomatopoeiaText(text string) bool {
	count := 0
	for _, r := range text {
		if unicode.IsSpace(r) || unicode.IsPunct(r) {
			continue
		}
		if !strings.ContainsRune(onomatopoeiaRunes, r) {
			return false
		}
		count++
	}
	return count > 0 && count <= 8
}

// isSoundEffectSpeech recognizes SFX that a model stored as a dialogue row: an
// unattributed onomatopoeia (“轰”) or a sound-named speaker such as “破空声（画外）”.
// A named character uttering a short sound keeps its row.
func isSoundEffectSpeech(character, text string) bool {
	speaker := strings.TrimSpace(character)
	for _, suffix := range []string{"（画外）", "(画外)", "（音效）", "(音效)"} {
		speaker = strings.TrimSpace(strings.TrimSuffix(speaker, suffix))
	}
	if speaker != "" && !strings.Contains(speaker, "心声") && soundEffectSpeakerPattern.MatchString(speaker) {
		return true
	}
	return (speaker == "" || speaker == "待确认") && isOnomatopoeiaText(text)
}

// moveSoundEffectsToContent drops SFX rows from scene dialogues. A sound the content does
// not already describe is kept as an 【音效】 note so no authored detail is lost. Texts in
// protected (speechMatchKey of source-ledger speech) always stay dialogue.
func moveSoundEffectsToContent(scenes []scriptScene, protected map[string]bool) {
	for i := range scenes {
		kept := scenes[i].Dialogues[:0:0]
		for _, d := range scenes[i].Dialogues {
			if protected[speechMatchKey(d.Text)] || !isSoundEffectSpeech(d.Character, d.Text) {
				kept = append(kept, d)
				continue
			}
			if text := strings.TrimSpace(d.Text); text != "" && !strings.Contains(scenes[i].Content, text) {
				scenes[i].Content = strings.TrimSpace(scenes[i].Content) + "\n【音效】" + text
			}
		}
		scenes[i].Dialogues = kept
	}
}

func speechMatchKey(text string) string {
	var b strings.Builder
	for _, r := range text {
		if !unicode.IsSpace(r) && !unicode.IsPunct(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// sceneDialogueConsistencyIssues is advisory: it compares the quoted lines a scene's
// content shows with its authoritative rows — order, extra quotes, labeled speakers.
// Natural-prose attribution is not parsed, so unlabeled speaker drift is not detected.
func sceneDialogueConsistencyIssues(content string, rows []models.Dialogue) []string {
	keys := make([]string, len(rows))
	for i, row := range rows {
		keys[i] = speechMatchKey(row.Text)
	}
	find := func(text string, from int) int {
		key := speechMatchKey(text)
		if key == "" {
			return -2
		}
		for i := from; i < len(keys); i++ {
			if strings.Contains(keys[i], key) {
				return i
			}
		}
		return -1
	}
	var issues []string
	last := 0
	for _, quote := range sceneQuotedSpeechPattern.FindAllString(sceneSpeechLabelLinePattern.ReplaceAllString(content, ""), -1) {
		text := strings.Trim(quote, "“”\"")
		if isOnomatopoeiaText(text) {
			continue
		}
		switch at := find(text, last); {
		case at >= 0:
			last = at
		case at == -1 && find(text, 0) >= 0:
			issues = append(issues, fmt.Sprintf("正文引号“%s”与对白行顺序不一致", text))
		case at == -1:
			issues = append(issues, fmt.Sprintf("正文引号“%s”没有对应的对白行", text))
		}
	}
	for _, match := range sceneLabeledSpeechPattern.FindAllStringSubmatch(content, -1) {
		speaker, text := strings.TrimSpace(match[1]), strings.TrimSpace(match[2])
		if speaker == "待确认" {
			continue
		}
		if at := find(text, 0); at >= 0 && strings.TrimSpace(rows[at].Character) != speaker {
			issues = append(issues, fmt.Sprintf("正文标注说话人“%s”与对白行“%s”不一致", speaker, rows[at].Character))
		}
	}
	return issues
}

// sceneAdvisoryWarnings collects non-blocking density and dialogue-consistency notes
// for persisted scenes, keyed by scene order.
func sceneAdvisoryWarnings(scenes []models.Scene, dialogues []models.Dialogue) []string {
	byScene := map[uint][]models.Dialogue{}
	for _, d := range dialogues {
		byScene[d.SceneID] = append(byScene[d.SceneID], d)
	}
	out := sceneDensityWarnings(scenes, byScene)
	for _, scene := range scenes {
		for _, issue := range sceneDialogueConsistencyIssues(scene.Content, byScene[scene.ID]) {
			out = append(out, fmt.Sprintf("第%d场%s", scene.Order, issue))
		}
	}
	return out
}
