package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"strings"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
)

type sceneDirectorDraft struct {
	Shots []sceneDirectorDraftShot `json:"shots"`
}
type dialogueRhythmFragmentReview struct {
	DialogueID uint   `json:"dialogue_id"`
	GroupKey   string `json:"group_key"`
	Character  string `json:"character"`
	SpeechType string `json:"speech_type"`
	StartRune  int    `json:"start_rune"`
	EndRune    int    `json:"end_rune"`
	LocalStart int    `json:"local_start_rune"`
	LocalEnd   int    `json:"local_end_rune"`
	Text       string `json:"text"`
}

type dialogueRhythmShotReview struct {
	Shot              int                            `json:"shot"`
	StartTime         float64                        `json:"start_time"`
	EndTime           float64                        `json:"end_time"`
	DialogueStart     int                            `json:"dialogue_start_rune"`
	DialogueEnd       int                            `json:"dialogue_end_rune"`
	NaturalMinimum    float64                        `json:"natural_minimum"`
	Ready             bool                           `json:"ready"`
	Status            string                         `json:"status"`
	BlockingIssues    []string                       `json:"blocking_issues"`
	Advisories        []string                       `json:"advisories"`
	EntryBoundary     string                         `json:"entry_boundary"`
	ExitBoundary      string                         `json:"exit_boundary"`
	Speakers          []string                       `json:"speakers"`
	Presentation      string                         `json:"presentation"`
	ContinuesFromPrev bool                           `json:"continues_from_previous"`
	ContinuesToNext   bool                           `json:"continues_to_next"`
	Fragments         []dialogueRhythmFragmentReview `json:"fragments"`
}

type sceneDirectorDraftShot struct {
	ActType        models.ShotActType               `json:"act_type"`
	ShotType       string                           `json:"shot_type"`
	CameraAngle    string                           `json:"camera_angle"`
	CameraMovement string                           `json:"camera_movement"`
	Duration       float64                          `json:"duration"`
	Description    string                           `json:"description"`
	Dialogue       string                           `json:"dialogue"`
	Emotion        string                           `json:"emotion"`
	TransitionType models.ShotTransitionType        `json:"transition_type"`
	TransitionNote string                           `json:"transition_note"`
	StartState     string                           `json:"start_state"`
	EndState       string                           `json:"end_state"`
	PromptSubject  string                           `json:"prompt_subject"`
	PromptAction   string                           `json:"prompt_action"`
	PromptCamera   string                           `json:"prompt_camera"`
	PromptLighting string                           `json:"prompt_lighting"`
	PromptStyle    string                           `json:"prompt_style"`
	NegativePrompt string                           `json:"negative_prompt"`
	ActionTimeline []models.ShotActionTimelineEntry `json:"action_timeline"`
	Checks         []string                         `json:"checks"`
}

func trimJSONFence(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "```json")
	value = strings.TrimPrefix(value, "```")
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(value), "```"))
}
func joinDirectorField(first, second string) string {
	first, second = strings.TrimSpace(first), strings.TrimSpace(second)
	if first == "" {
		return second
	}
	if second == "" || second == first {
		return first
	}
	return first + "；" + second
}

func mergeDirectorShotPair(first, second sceneDirectorDraftShot) sceneDirectorDraftShot {
	firstDuration := first.Duration
	first.Duration += second.Duration
	first.Description = joinDirectorField(first.Description, second.Description)
	first.Dialogue += second.Dialogue
	first.Emotion = joinDirectorField(first.Emotion, second.Emotion)
	first.EndState = second.EndState
	first.PromptSubject = joinDirectorField(first.PromptSubject, second.PromptSubject)
	first.PromptAction = joinDirectorField(first.PromptAction, second.PromptAction)
	first.PromptCamera = joinDirectorField(first.PromptCamera, second.PromptCamera)
	first.PromptLighting = joinDirectorField(first.PromptLighting, second.PromptLighting)
	first.PromptStyle = joinDirectorField(first.PromptStyle, second.PromptStyle)
	first.NegativePrompt = joinDirectorField(first.NegativePrompt, second.NegativePrompt)
	first.TransitionType, first.TransitionNote = second.TransitionType, second.TransitionNote
	if len(first.ActionTimeline) > 0 && len(second.ActionTimeline) > 0 {
		combined := append([]models.ShotActionTimelineEntry(nil), first.ActionTimeline...)
		for _, entry := range second.ActionTimeline {
			entry.Start += firstDuration
			entry.End += firstDuration
			combined = append(combined, entry)
		}
		first.ActionTimeline = combined
	} else {
		// A partial merged timeline would contain an uncovered time gap. The timeline is
		// optional, so discard it instead of inventing action facts for the missing part.
		first.ActionTimeline = []models.ShotActionTimelineEntry{}
	}
	first.Checks = append(first.Checks, second.Checks...)
	first.Checks = append(first.Checks, "系统已将不足3秒的相邻镜头合并为一个Native H3镜头")
	return first
}

func normalizeShortDirectorShots(draft *sceneDirectorDraft) error {
	if draft == nil {
		return nil
	}
	for i := 0; i < len(draft.Shots); {
		shot := draft.Shots[i]
		if shot.Duration <= 0 || shot.Duration >= 3 {
			i++
			continue
		}
		if i+1 < len(draft.Shots) && shot.Duration+draft.Shots[i+1].Duration <= 15 {
			draft.Shots[i] = mergeDirectorShotPair(shot, draft.Shots[i+1])
			draft.Shots = append(draft.Shots[:i+1], draft.Shots[i+2:]...)
			continue
		}
		if i > 0 && draft.Shots[i-1].Duration+shot.Duration <= 15 {
			draft.Shots[i-1] = mergeDirectorShotPair(draft.Shots[i-1], shot)
			draft.Shots = append(draft.Shots[:i], draft.Shots[i+1:]...)
			i--
			if i < 0 {
				i = 0
			}
			continue
		}
		return fmt.Errorf("镜头%d时长%.1f秒，无法与相邻镜头合并到3至15秒范围", i+1, shot.Duration)
	}
	return nil
}

func parseSceneDirectorDraft(output string) (*sceneDirectorDraft, error) {
	dec := json.NewDecoder(bytes.NewBufferString(trimJSONFence(output)))
	dec.DisallowUnknownFields()
	var draft sceneDirectorDraft
	if err := dec.Decode(&draft); err != nil {
		return nil, err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("导演草稿含额外内容")
	}
	if len(draft.Shots) == 0 || len(draft.Shots) > 30 {
		return nil, fmt.Errorf("导演草稿镜头数必须为1至30")
	}
	for i := range draft.Shots {
		d := &draft.Shots[i]
		// action_timeline is optional for a simple single-action Shot. Some models emit
		// one empty/incomplete placeholder object despite being asked for []. Dropping
		// that one placeholder is deterministic and safer than inventing missing facts.
		if len(d.ActionTimeline) == 1 {
			entry := d.ActionTimeline[0]
			if strings.TrimSpace(entry.Subject) == "" || strings.TrimSpace(entry.Action) == "" || strings.TrimSpace(entry.State) == "" || strings.TrimSpace(entry.Camera) == "" {
				d.ActionTimeline = []models.ShotActionTimelineEntry{}
				d.Checks = append(d.Checks, "系统已移除模型输出的不完整单段动作时间轴；简单镜头不要求动作时间轴")
			}
		}
	}
	if err := normalizeShortDirectorShots(&draft); err != nil {
		return nil, err
	}
	for i := range draft.Shots {
		d := &draft.Shots[i]
		required := []struct {
			name  string
			value *string
		}{
			{"shot_type", &d.ShotType}, {"camera_angle", &d.CameraAngle}, {"camera_movement", &d.CameraMovement},
			{"description", &d.Description}, {"start_state", &d.StartState}, {"end_state", &d.EndState},
			{"prompt_subject", &d.PromptSubject}, {"prompt_action", &d.PromptAction}, {"prompt_camera", &d.PromptCamera},
			{"prompt_lighting", &d.PromptLighting}, {"prompt_style", &d.PromptStyle},
		}
		for _, field := range required {
			*field.value = strings.TrimSpace(*field.value)
			if *field.value == "" {
				return nil, fmt.Errorf("镜头%d的必填字段%s为空", i+1, field.name)
			}
		}
		d.Dialogue, d.Emotion, d.TransitionNote, d.NegativePrompt = strings.TrimSpace(d.Dialogue), strings.TrimSpace(d.Emotion), strings.TrimSpace(d.TransitionNote), strings.TrimSpace(d.NegativePrompt)
		if d.Duration < 3 || d.Duration > 15 || math.IsNaN(d.Duration) || math.IsInf(d.Duration, 0) {
			return nil, fmt.Errorf("镜头%d时长必须为3到15秒", i+1)
		}
		shot := models.Shot{ActType: d.ActType, ShotType: d.ShotType, CameraAngle: d.CameraAngle, CameraMovement: d.CameraMovement, TransitionType: d.TransitionType, TransitionNote: d.TransitionNote, StartState: d.StartState, EndState: d.EndState, Duration: d.Duration, Description: d.Description, Dialogue: d.Dialogue, Emotion: d.Emotion, PromptSubject: d.PromptSubject, PromptAction: d.PromptAction, PromptCamera: d.PromptCamera, PromptLighting: d.PromptLighting, PromptStyle: d.PromptStyle, NegativePrompt: d.NegativePrompt, ActionTimeline: d.ActionTimeline}
		if err := validateShot(&shot); err != nil {
			return nil, fmt.Errorf("镜头%d: %w", i+1, err)
		}
		for j := range d.Checks {
			d.Checks[j] = strings.TrimSpace(d.Checks[j])
		}
	}
	return &draft, nil
}

func dialogueRhythmDirectorInstruction(dialogues []models.Dialogue) string {
	var lines []string
	for i, d := range dialogues {
		lines = append(lines, fmt.Sprintf("D%d｜%s｜%s", i+1, strings.TrimSpace(d.Character), strings.TrimSpace(d.Text)))
	}
	return `按对白自然语速和语义停顿拆成多个3–15秒Native H3镜头。可以交替使用说话人近景、包含说话人的双人/前后景镜头，以及对白结束后的纯听者反应镜头。硬规则：
1. Dialogue字段只能填下列结构化对白的连续原文片段，不得改写、增删、重复或创造旁白；无发声镜头必须为空。
2. 所有镜头Dialogue按顺序拼接后必须逐字等于下列完整对白原文按顺序拼接的结果。
3. 普通dialogue默认让真实说话人清晰可见并由其同步口型；也允许同一句对白跨切到纯听者反应镜头，但必须明确写出“原说话人姓名+声音跨切延续”，听者必须闭口，且不得把这种连续对白改写成旁白或独立画外音。只有明确speech_type为offscreen_dialogue时才设计独立画外对白且说话人不得入镜；只有明确speech_type为narration或monologue时才使用旁白/内心独白语义。
4. 纯听者反应镜头承载普通对白时只能承接上一镜已经开始的同一句对白；不能在听者镜头中开始一条新对白，也不能改变说话人。
5. 每镜3–15秒，一个主要情绪、一个主要动作、一种主要构图和明确结束状态。
6. 对白自然时长决定总时长，不得压缩语速，也不得用重复动作填时长。
结构化对白：
` + strings.Join(lines, "\n")
}

func actionRhythmDirectorInstruction(dialogues []models.Dialogue) string {
	dialogueRule := "本场没有结构化对白；所有Shot的dialogue必须为空，禁止新增喊声、旁白或咒语。"
	if len(dialogues) > 0 {
		lines := make([]string, 0, len(dialogues))
		for i, d := range dialogues {
			lines = append(lines, fmt.Sprintf("D%d｜%s｜%s", i+1, strings.TrimSpace(d.Character), strings.TrimSpace(d.Text)))
		}
		dialogueRule = "结构化对白仍是唯一发声来源；各Shot的dialogue只能按顺序承载以下原文，拼接后必须逐字一致，不得把招式、动作或爆炸声写成对白：\n" + strings.Join(lines, "\n")
	}
	return `按武戏、追逐、近身攻防或仙术对轰的动作节拍拆成可审核的Native H3镜头。保持Scene为生产单元，Shot只作为内部导演节拍；不使用Endless。硬规则：
1. 每个Shot为3–6秒，只承载一个可验证结果：位移完成、一次攻击命中/落空/被挡、一次闪避完成、一次法术释放、一次碰撞爆发或一次受力落点。禁止把冲刺、蓄力、连续挥砍、碰撞和落地塞进同一长镜。
2. 动作按实时速度执行：立即启动、快速完成、撞击后立刻产生位移；禁止慢动作、子弹时间、逐渐、缓缓、悬停展示、长时间蓄力、戏剧性停顿和命中后定格。
3. description与prompt_action必须明确主体、起点、运动方向、目标、武器/法术归属、命中或落空、受力方向及本Shot结束姿态；仙术对轰还必须明确双方能量来源、飞行方向、碰撞点和爆发后的空间结果。
4. 每个Shot只能使用一种主要摄影机策略。主体高速移动时优先固定机位、短促横移或有限跟随；禁止同时推拉摇移、环绕和变焦，禁止用慢运镜拖慢动作。
5. 相邻Shot的start_state必须逐项承接上一Shot的end_state，保持人物左右位置、朝向、距离、武器持有者、伤势、法术状态和空间轴线连续。
6. prompt_action必须包含“实时速度、迅速完成、无慢动作停顿”的明确约束；negative_prompt必须包含“慢动作、子弹时间、悬停、动作拖沓、重复动作”。
7. ` + dialogueRule
}

var slowActionDraftPattern = regexp.MustCompile(`慢动作|慢镜头|子弹时间|缓缓|逐渐|慢慢|悬停|长时间蓄力|戏剧性停顿|定格展示|slow[ -]?motion|bullet time|lingering|gradually`)

func validateActionRhythmDraft(draft *sceneDirectorDraft, dialogues []models.Dialogue) error {
	if draft == nil || len(draft.Shots) == 0 {
		return fmt.Errorf("武戏拆镜草稿为空")
	}
	for i, shot := range draft.Shots {
		if shot.Duration < 3 || shot.Duration > 6 {
			return fmt.Errorf("武戏拆镜%d时长必须为3至6秒，避免动作被摊成慢镜头", i+1)
		}
		combined := strings.Join([]string{shot.Description, shot.PromptAction, shot.PromptCamera, shot.TransitionNote}, " ")
		combined = strings.NewReplacer("无慢动作停顿", "", "无慢动作", "", "no slow motion", "").Replace(strings.ToLower(combined))
		if slowActionDraftPattern.MatchString(combined) {
			return fmt.Errorf("武戏拆镜%d包含慢动作或拖延表达", i+1)
		}
		for _, want := range []string{"实时速度", "无慢动作"} {
			if !strings.Contains(shot.PromptAction, want) {
				return fmt.Errorf("武戏拆镜%d的prompt_action缺少“%s”约束", i+1, want)
			}
		}
		for _, want := range []string{"慢动作", "子弹时间", "动作拖沓"} {
			if !strings.Contains(shot.NegativePrompt, want) {
				return fmt.Errorf("武戏拆镜%d的negative_prompt缺少“%s”", i+1, want)
			}
		}
		if i > 0 && (strings.TrimSpace(shot.StartState) == "" || strings.TrimSpace(draft.Shots[i-1].EndState) == "") {
			return fmt.Errorf("武戏拆镜%d缺少与上一镜衔接的起止状态", i+1)
		}
	}
	return validateDialogueRhythmDraft(draft, dialogues)
}

func canonicalDialogueText(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), "")
}

func normalizeDialogueRhythmDraftDurations(draft *sceneDirectorDraft) {
	if draft == nil {
		return
	}
	for i := range draft.Shots {
		shot := &draft.Shots[i]
		minimum := 3.0
		if text := strings.TrimSpace(shot.Dialogue); text != "" {
			// Reserve visible performance time around the estimated natural speech. This
			// prevents a long line from being squeezed into an arbitrary 3–5 second Shot.
			minimum = math.Ceil((dialogueTextDuration(text)+0.6)*2) / 2
			if minimum < 3 {
				minimum = 3
			}
		}
		if shot.Duration > 0 && shot.Duration < minimum {
			shot.Duration = minimum
			shot.Checks = append(shot.Checks, fmt.Sprintf("系统已按对白自然语速将镜头时长调整为%.1f秒", minimum))
		}
	}
}

func restoreDialogueRhythmDraftText(draft *sceneDirectorDraft, dialogues []models.Dialogue) error {
	if draft == nil {
		return fmt.Errorf("导演草稿为空")
	}
	var source []rune
	breaks := map[int]bool{}
	for _, d := range dialogues {
		text := []rune(strings.TrimSpace(d.Text))
		for _, r := range text {
			source = append(source, r)
			if strings.ContainsRune("。！？!?；;，,、", r) {
				breaks[len(source)] = true
			}
		}
		breaks[len(source)] = true
	}
	if len(source) == 0 {
		return nil
	}
	indices := make([]int, 0, len(draft.Shots))
	weights := make([]int, 0, len(draft.Shots))
	for i := range draft.Shots {
		if strings.TrimSpace(draft.Shots[i].Dialogue) == "" {
			continue
		}
		indices = append(indices, i)
		weight := len([]rune(canonicalDialogueText(draft.Shots[i].Dialogue)))
		if weight < 1 {
			weight = 1
		}
		weights = append(weights, weight)
	}
	if len(indices) == 0 {
		return fmt.Errorf("导演草稿没有承载结构化对白的镜头")
	}
	totalWeight := 0
	for _, weight := range weights {
		totalWeight += weight
	}
	start, usedWeight := 0, 0
	for pos, shotIndex := range indices {
		end := len(source)
		if pos < len(indices)-1 {
			usedWeight += weights[pos]
			target := int(math.Round(float64(len(source)) * float64(usedWeight) / float64(totalWeight)))
			minEnd, maxEnd := start+1, len(source)-(len(indices)-pos-1)
			if target < minEnd {
				target = minEnd
			}
			if target > maxEnd {
				target = maxEnd
			}
			end = target
			for distance := 0; distance <= len(source); distance++ {
				left, right := target-distance, target+distance
				if left >= minEnd && breaks[left] {
					end = left
					break
				}
				if right <= maxEnd && breaks[right] {
					end = right
					break
				}
			}
		}
		draft.Shots[shotIndex].Dialogue = string(source[start:end])
		draft.Shots[shotIndex].Checks = append(draft.Shots[shotIndex].Checks, "对白由系统按结构化Dialogue原文连续回填，禁止模型改写")
		start = end
	}
	return nil
}

func ordinaryDialogueCarryoverAllowed(shot sceneDirectorDraftShot, speaker string, startsAfterDialogueStart bool) bool {
	if !startsAfterDialogueStart || strings.TrimSpace(speaker) == "" {
		return false
	}
	visual := strings.Join([]string{shot.ShotType, shot.Description, shot.PromptSubject, shot.PromptAction, shot.StartState, shot.EndState}, " ")
	if !strings.Contains(visual, speaker) {
		return false
	}
	carryover := false
	for _, marker := range []string{"声音跨切延续", "台词跨切延续", "画外继续", "画外音继续", "声音继续", "继续说话"} {
		if strings.Contains(visual, marker) {
			carryover = true
			break
		}
	}
	closedLips := false
	for _, marker := range []string{"闭口", "双唇闭合", "嘴唇闭合", "不张口", "不做口型"} {
		if strings.Contains(visual, marker) {
			closedLips = true
			break
		}
	}
	return carryover && closedLips
}

func dialogueReviewNaturalMinimum(fragments []dialogueRhythmFragmentReview) float64 {
	if len(fragments) == 0 {
		return 3
	}
	duration := 0.6
	previousVoice := ""
	for _, fragment := range fragments {
		voice := strings.TrimSpace(fragment.Character) + "\x00" + fragment.SpeechType
		if previousVoice != "" && voice != previousVoice {
			duration += speakerChangePause
		}
		duration += dialogueTextDuration(fragment.Text)
		previousVoice = voice
	}
	return math.Max(3, math.Ceil(duration*2)/2)
}

func buildDialogueRhythmReview(draft *sceneDirectorDraft, dialogues []models.Dialogue) []dialogueRhythmShotReview {
	if draft == nil {
		return []dialogueRhythmShotReview{}
	}
	type span struct {
		start, end int
		groupStart int
		groupKey   string
		text       []rune
		dialogue   models.Dialogue
	}
	spans, offset, groupNo, groupOffset := []span{}, 0, 0, 0
	for i, dialogue := range dialogues {
		text := []rune(canonicalDialogueText(dialogue.Text))
		if i == 0 || !dialogueContinuesAcrossCut(dialogues[i-1], dialogue) {
			groupNo++
			groupOffset = 0
		}
		end := offset + len(text)
		spans = append(spans, span{start: offset, end: end, groupStart: groupOffset, groupKey: fmt.Sprintf("dialogue-group:%d", groupNo), text: text, dialogue: dialogue})
		offset = end
		groupOffset += len(text)
	}
	reviews := make([]dialogueRhythmShotReview, 0, len(draft.Shots))
	timeCursor, dialogueCursor := 0.0, 0
	for i, shot := range draft.Shots {
		length := len([]rune(canonicalDialogueText(shot.Dialogue)))
		start, end := dialogueCursor, dialogueCursor+length
		speakers, seen := []string{}, map[string]bool{}
		fragments := []dialogueRhythmFragmentReview{}
		continuesFrom, continuesTo := false, false
		for _, item := range spans {
			if end <= item.start || start >= item.end {
				continue
			}
			from := max(start, item.start) - item.start
			to := min(end, item.end) - item.start
			name := strings.TrimSpace(item.dialogue.Character)
			if name != "" && !seen[name] {
				speakers = append(speakers, name)
				seen[name] = true
			}
			speechType, _ := normalizeScriptSpeech(item.dialogue.SpeechType, item.dialogue.Character)
			fragments = append(fragments, dialogueRhythmFragmentReview{
				DialogueID: item.dialogue.ID, GroupKey: item.groupKey, Character: name,
				SpeechType: speechType, StartRune: item.groupStart + from,
				EndRune: item.groupStart + to, LocalStart: from, LocalEnd: to, Text: string(item.text[from:to]),
			})
			if start > item.start {
				continuesFrom = true
			}
			if end < item.end {
				continuesTo = true
			}
		}
		presentation := "silent_visual"
		if length > 0 {
			types := map[string]bool{}
			for _, fragment := range fragments {
				types[fragment.SpeechType] = true
			}
			switch {
			case len(types) > 1:
				presentation = "mixed_authoritative_speech"
			case types["narration"]:
				presentation = "narration_voiceover"
			case types["monologue"]:
				presentation = "internal_monologue"
			case types["offscreen_dialogue"]:
				presentation = "offscreen_dialogue"
			default:
				presentation = "visible_speaker_lipsync"
				for _, speaker := range speakers {
					if ordinaryDialogueCarryoverAllowed(shot, speaker, continuesFrom) {
						presentation = "listener_reaction_carryover"
						break
					}
				}
			}
		}
		minimum := dialogueReviewNaturalMinimum(fragments)
		issues := []string{}
		if minimum > maxSceneVideoDuration {
			issues = append(issues, fmt.Sprintf("本镜权威语音自然时长下限%.1f秒超过Native H3的15秒上限，必须继续拆镜", minimum))
		}
		if shot.Duration+0.001 < minimum {
			issues = append(issues, fmt.Sprintf("本镜时长%.1f秒低于权威语音自然时长下限%.1f秒", shot.Duration, minimum))
		}
		status := "ready"
		if len(issues) > 0 {
			status = "blocked"
		}
		reviews = append(reviews, dialogueRhythmShotReview{Shot: i + 1, StartTime: timeCursor, EndTime: timeCursor + shot.Duration, DialogueStart: start, DialogueEnd: end, NaturalMinimum: minimum, Ready: len(issues) == 0, Status: status, BlockingIssues: issues, Advisories: []string{}, Speakers: speakers, Presentation: presentation, ContinuesFromPrev: continuesFrom, ContinuesToNext: continuesTo, Fragments: fragments})
		timeCursor += shot.Duration
		dialogueCursor = end
	}
	for i := range reviews {
		review := &reviews[i]
		if len(review.Fragments) == 0 {
			review.EntryBoundary, review.ExitBoundary = "静默镜头", "静默镜头"
			continue
		}
		if i == 0 {
			review.EntryBoundary = "场景开始"
		} else {
			review.EntryBoundary = dialogueBoundaryReason(reviews[i-1].Fragments, review.Fragments, review.ContinuesFromPrev)
		}
		if i == len(reviews)-1 {
			review.ExitBoundary = "场景结束"
		} else {
			review.ExitBoundary = dialogueBoundaryReason(review.Fragments, reviews[i+1].Fragments, review.ContinuesToNext)
		}
		if review.EntryBoundary == "同句跨镜续接" || review.ExitBoundary == "同句跨镜续接" {
			review.Advisories = append(review.Advisories, "本镜在无标点的同一句中切换，请重点审核口型连续性、听者闭口和声音跨切是否自然")
			if review.Status == "ready" {
				review.Status = "attention"
			}
		}
	}
	return reviews
}

func dialogueBoundaryReason(before, after []dialogueRhythmFragmentReview, sameDialogueContinuation bool) string {
	if len(before) == 0 || len(after) == 0 {
		return "静默边界"
	}
	left, right := before[len(before)-1], after[0]
	if left.SpeechType != right.SpeechType {
		return "语音类型切换"
	}
	if strings.TrimSpace(left.Character) != strings.TrimSpace(right.Character) {
		return "说话人切换"
	}
	if left.DialogueID != right.DialogueID {
		return "Dialogue边界"
	}
	text := []rune(strings.TrimSpace(left.Text))
	if len(text) > 0 && strings.ContainsRune("，,、；;：:。.!！?？…", text[len(text)-1]) {
		return "标点停顿"
	}
	if sameDialogueContinuation {
		return "同句跨镜续接"
	}
	return "Dialogue边界"
}

func validateDialogueRhythmDraft(draft *sceneDirectorDraft, dialogues []models.Dialogue) error {
	var expected, actual strings.Builder
	for _, d := range dialogues {
		expected.WriteString(canonicalDialogueText(d.Text))
	}
	for i, shot := range draft.Shots {
		if shot.Duration < 3 || shot.Duration > 15 {
			return fmt.Errorf("对白拆镜%d时长必须为3至15秒", i+1)
		}
		actual.WriteString(canonicalDialogueText(shot.Dialogue))
	}
	if actual.String() != expected.String() {
		return fmt.Errorf("导演草稿对白未逐字覆盖结构化对白，禁止遗漏、改写、重复或新增")
	}
	type dialogueSpan struct {
		start, end int
		dialogue   models.Dialogue
	}
	spans := make([]dialogueSpan, 0, len(dialogues))
	offset := 0
	for _, d := range dialogues {
		text := canonicalDialogueText(d.Text)
		end := offset + len([]rune(text))
		spans = append(spans, dialogueSpan{offset, end, d})
		offset = end
	}
	cursor := 0
	for i, shot := range draft.Shots {
		text := canonicalDialogueText(shot.Dialogue)
		start, end := cursor, cursor+len([]rune(text))
		cursor = end
		if text == "" {
			continue
		}
		for _, span := range spans {
			if end <= span.start || start >= span.end {
				continue
			}
			speechType, normalizedSpeaker := normalizeScriptSpeech(span.dialogue.SpeechType, span.dialogue.Character)
			speaker := strings.TrimSpace(normalizedSpeaker)
			if speaker == "" {
				continue
			}
			if speechType == "offscreen_dialogue" {
				speakerVisible := strings.Contains(strings.Join([]string{shot.PromptSubject, shot.ShotType}, " "), speaker)
				if speakerVisible {
					return fmt.Errorf("对白拆镜%d承载角色“%s”的独立画外对白时，说话人不得出现在画面主体中；如需可见口型请改用dialogue", i+1, speaker)
				}
				continue
			}
			if speechType != "dialogue" {
				continue
			}
			speakerVisible := strings.Contains(strings.Join([]string{shot.PromptSubject, shot.ShotType}, " "), speaker)
			if speakerVisible || ordinaryDialogueCarryoverAllowed(shot, speaker, start > span.start) {
				continue
			}
			return fmt.Errorf("对白拆镜%d承载角色“%s”的普通对白时，必须让说话人可见并同步口型，或明确标注同一句对白跨切延续且听者闭口", i+1, speaker)
		}
	}
	return nil
}

func (s *Service) HandleGenerateSceneDirectorDraft(c *gin.Context) {
	scene, ok := s.loadScene(c)
	if !ok {
		return
	}
	if !s.textProviderConfigured() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "当前文生文 provider 未配置"})
		return
	}
	var req struct {
		Brief        string `json:"brief"`
		Requirements string `json:"requirements"`
		Mode         string `json:"mode"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	req.Brief, req.Requirements = strings.TrimSpace(req.Brief), strings.TrimSpace(req.Requirements)
	if req.Brief == "" {
		req.Brief = strings.TrimSpace(scene.Content)
	}
	if req.Brief == "" || len([]rune(req.Brief)) > 4000 || len([]rune(req.Requirements)) > 2000 {
		c.JSON(400, gin.H{"error": "请填写有效的场景概况（最多4000字）和补充要求（最多2000字）"})
		return
	}
	assets, err := s.sceneAssetBible(scene)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	operation, instruction := "director-scene-draft", "生成完整Scene导演方案草稿；只预览，不保存。"
	var dialogues []models.Dialogue
	dialogueDuration := 0.0
	if req.Mode == "dialogue_rhythm" || req.Mode == "action_rhythm" {
		if err := s.DB.Where("scene_id = ? AND project_id = ?", scene.ID, scene.ProjectID).Order("`order` ASC, `id` ASC").Find(&dialogues).Error; err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		dialogues = validSceneDialogues(dialogues)
		if req.Mode == "dialogue_rhythm" {
			if len(dialogues) == 0 {
				c.JSON(400, gin.H{"error": "当前Scene没有可用于拆镜的结构化对白"})
				return
			}
			dialogueDuration = modelDialoguesMinDuration(dialogues)
			instruction = dialogueRhythmDirectorInstruction(dialogues)
		} else {
			instruction = actionRhythmDirectorInstruction(dialogues)
		}
		req.Requirements = strings.TrimSpace(req.Requirements + "\n" + instruction)
	}
	if req.Mode == "standard" {
		var modeDialogues []models.Dialogue
		_ = s.DB.Where("scene_id = ? AND project_id = ?", scene.ID, scene.ProjectID).Order("`order` ASC, `id` ASC").Find(&modeDialogues).Error
		resolvedMode := classifyH3SceneMode(scene, nil, modeDialogues)
		modeInstruction := h3SceneModeInstruction(resolvedMode)
		instruction = strings.TrimSpace(instruction + "\n" + modeInstruction)
		req.Requirements = strings.TrimSpace(req.Requirements + "\n用户选择的导演类型：" + string(resolvedMode) + "。\n" + modeInstruction)
	}
	output, err := s.Skills.ChatWithConfiguredOrFallbackSkill(scene.ProjectID, models.SkillStageStoryboard, operation, s.TextProviderFact, "只输出完整合法JSON，不要Markdown或解释。", instruction, map[string]string{"scene_facts": scene.Content, "asset_context": assets, "brief": req.Brief, "requirements": req.Requirements, "target_duration": fmt.Sprintf("%.1f", math.Max(scene.Duration, dialogueDuration))})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	validateDraft := func(raw string) (*sceneDirectorDraft, error) {
		draft, parseErr := parseSceneDirectorDraft(raw)
		if parseErr == nil && req.Mode == "dialogue_rhythm" {
			normalizeDialogueRhythmDraftDurations(draft)
			parseErr = validateDialogueRhythmDraft(draft, dialogues)
		} else if parseErr == nil && req.Mode == "action_rhythm" {
			parseErr = validateActionRhythmDraft(draft, dialogues)
		}
		return draft, parseErr
	}
	draft, err := validateDraft(output)
	if err != nil && (req.Mode == "dialogue_rhythm" || req.Mode == "action_rhythm") {
		repairRequirements := fmt.Sprintf("%s\n上一次草稿校验失败：%s。只修复该错误及其他空必填字段；保持镜头顺序、剧情事实和结构化对白逐字不变。上一次JSON：\n%s", req.Requirements, err.Error(), output)
		repairInstruction := "修复对白节奏导演草稿；不得改写、遗漏、重复或新增任何对白。"
		if req.Mode == "action_rhythm" {
			repairInstruction = "修复武戏/仙术动作节拍草稿；保持实时速度、单一动作结果、空间轴线和结构化对白，不得使用慢动作。"
		}
		repaired, repairErr := s.Skills.ChatWithConfiguredOrFallbackSkill(scene.ProjectID, models.SkillStageStoryboard, operation, s.TextProviderFact, "只输出修复后的完整合法JSON，不要Markdown或解释。", repairInstruction, map[string]string{"scene_facts": scene.Content, "asset_context": assets, "brief": req.Brief, "requirements": repairRequirements, "target_duration": fmt.Sprintf("%.1f", math.Max(scene.Duration, dialogueDuration))})
		if repairErr != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "AI导演方案自动修复失败: " + repairErr.Error()})
			return
		}
		draft, err = validateDraft(repaired)
		if err != nil && strings.Contains(err.Error(), "导演草稿对白未逐字覆盖结构化对白") && draft != nil {
			if restoreErr := restoreDialogueRhythmDraftText(draft, dialogues); restoreErr != nil {
				err = restoreErr
			} else {
				err = validateDialogueRhythmDraft(draft, dialogues)
			}
		}
	}
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "AI导演方案自动修复后仍无效: " + err.Error()})
		return
	}
	response := gin.H{"draft": draft, "skill_code": "director-scene-draft", "provider_id": s.TextProviderFact.Name(), "audited": true, "mode": req.Mode, "dialogue_duration": dialogueDuration}
	if req.Mode == "dialogue_rhythm" {
		response["dialogue_review"] = buildDialogueRhythmReview(draft, dialogues)
		response["dialogue_snapshot"] = dialogueSnapshotToken(dialogues)
	}
	c.JSON(200, response)
}
