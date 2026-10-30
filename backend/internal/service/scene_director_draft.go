package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
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
	ListenerCharacter string                         `json:"listener_character,omitempty"`
	ContinuesFromPrev bool                           `json:"continues_from_previous"`
	ContinuesToNext   bool                           `json:"continues_to_next"`
	Fragments         []dialogueRhythmFragmentReview `json:"fragments"`
}

type sceneDirectorDraftShot struct {
	ActType            models.ShotActType               `json:"act_type"`
	ShotType           string                           `json:"shot_type"`
	CameraAngle        string                           `json:"camera_angle"`
	CameraMovement     string                           `json:"camera_movement"`
	Duration           float64                          `json:"duration"`
	Description        string                           `json:"description"`
	Dialogue           string                           `json:"dialogue"`
	SpeechPresentation models.ShotSpeechPresentation    `json:"speech_presentation"`
	ListenerCharacter  string                           `json:"listener_character"`
	Emotion            string                           `json:"emotion"`
	TransitionType     models.ShotTransitionType        `json:"transition_type"`
	TransitionNote     string                           `json:"transition_note"`
	StartState         string                           `json:"start_state"`
	EndState           string                           `json:"end_state"`
	PromptSubject      string                           `json:"prompt_subject"`
	PromptAction       string                           `json:"prompt_action"`
	PromptCamera       string                           `json:"prompt_camera"`
	PromptLighting     string                           `json:"prompt_lighting"`
	PromptStyle        string                           `json:"prompt_style"`
	NegativePrompt     string                           `json:"negative_prompt"`
	ActionTimeline     []models.ShotActionTimelineEntry `json:"action_timeline"`
	Checks             []string                         `json:"checks"`
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

func directorShotsSpeechCompatible(first, second sceneDirectorDraftShot) bool {
	firstPresentation := models.ShotSpeechPresentation(strings.TrimSpace(string(first.SpeechPresentation)))
	secondPresentation := models.ShotSpeechPresentation(strings.TrimSpace(string(second.SpeechPresentation)))
	firstVoiced := strings.TrimSpace(first.Dialogue) != "" && firstPresentation != "" && firstPresentation != models.ShotSpeechPresentationSilent
	secondVoiced := strings.TrimSpace(second.Dialogue) != "" && secondPresentation != "" && secondPresentation != models.ShotSpeechPresentationSilent
	if firstVoiced && secondVoiced && firstPresentation != secondPresentation {
		return false
	}
	if firstPresentation == models.ShotSpeechPresentationListenerCarry && secondPresentation == models.ShotSpeechPresentationListenerCarry {
		return strings.TrimSpace(first.ListenerCharacter) == strings.TrimSpace(second.ListenerCharacter)
	}
	return true
}

func mergeDirectorShotPair(first, second sceneDirectorDraftShot) sceneDirectorDraftShot {
	firstDuration := first.Duration
	first.Duration += second.Duration
	first.Description = joinDirectorField(first.Description, second.Description)
	first.Dialogue += second.Dialogue
	if first.ListenerCharacter == "" {
		first.ListenerCharacter = second.ListenerCharacter
	} else if second.ListenerCharacter != "" && first.ListenerCharacter != second.ListenerCharacter {
		first.ListenerCharacter = ""
	}
	if first.SpeechPresentation != second.SpeechPresentation {
		switch {
		case first.SpeechPresentation == "" || first.SpeechPresentation == models.ShotSpeechPresentationSilent:
			first.SpeechPresentation = second.SpeechPresentation
		case second.SpeechPresentation == "" || second.SpeechPresentation == models.ShotSpeechPresentationSilent:
			// Preserve the first voiced presentation.
		default:
			// A merged Shot with incompatible speech modes requires authoritative reclassification.
			first.SpeechPresentation = ""
		}
	}
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
		if i+1 < len(draft.Shots) && shot.Duration+draft.Shots[i+1].Duration <= 15 && directorShotsSpeechCompatible(shot, draft.Shots[i+1]) {
			draft.Shots[i] = mergeDirectorShotPair(shot, draft.Shots[i+1])
			draft.Shots = append(draft.Shots[:i+1], draft.Shots[i+2:]...)
			continue
		}
		if i > 0 && draft.Shots[i-1].Duration+shot.Duration <= 15 && directorShotsSpeechCompatible(draft.Shots[i-1], shot) {
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

func unwrapSceneDirectorDraftEnvelope(output string) string {
	trimmed := trimJSONFence(output)
	var root map[string]json.RawMessage
	if json.Unmarshal([]byte(trimmed), &root) != nil {
		return trimmed
	}
	candidate := root
	if wrapped, ok := root["project"]; ok {
		var nested map[string]json.RawMessage
		if json.Unmarshal(wrapped, &nested) == nil {
			candidate = nested
		}
	}
	shots := candidate["shots"]
	if shots == nil {
		return trimmed
	}
	normalized, err := json.Marshal(map[string]json.RawMessage{"shots": shots})
	if err != nil {
		return trimmed
	}
	return string(normalized)
}

func normalizeDirectorAct(value models.ShotActType) models.ShotActType {
	normalized := strings.ToLower(strings.TrimSpace(string(value)))
	switch normalized {
	case "setup", "opening", "introduction", "开端", "建立", "起势":
		return models.ShotActSetup
	case "rising", "development", "escalation", "发展", "升级", "交锋":
		return models.ShotActRising
	case "midpoint", "climax", "turning_point", "中点", "转折", "反击":
		return models.ShotActMidpoint
	case "falling", "aftermath", "回落", "收束", "余波":
		return models.ShotActFalling
	case "resolution", "ending", "conclusion", "结局", "解决", "结束":
		return models.ShotActResolution
	default:
		// Act labels are planning metadata, not story facts. Unknown model labels use the
		// neutral setup value rather than invalidating an otherwise executable Shot.
		return models.ShotActSetup
	}
}

func normalizeDirectorTransition(value models.ShotTransitionType) models.ShotTransitionType {
	normalized := strings.ToLower(strings.TrimSpace(string(value)))
	switch normalized {
	case "", "none", "无", "无转场":
		return ""
	case "cut", "硬切", "直切", "直接切换":
		return models.ShotTransitionCut
	case "dissolve", "叠化":
		return models.ShotTransitionDissolve
	case "fade", "淡入淡出", "淡入", "淡出":
		return models.ShotTransitionFade
	case "wipe", "划像":
		return models.ShotTransitionWipe
	case "match_cut", "match cut", "匹配剪辑":
		return models.ShotTransitionMatchCut
	default:
		// Transition wording is presentation metadata, not a story fact. Unknown model
		// aliases are deterministically reduced to the safest supported hard cut.
		return models.ShotTransitionCut
	}
}

func normalizeDirectorActionTimeline(entries []models.ShotActionTimelineEntry, duration float64) {
	if len(entries) == 0 || duration <= 0 {
		return
	}
	weights := make([]float64, len(entries))
	total := 0.0
	for index := range entries {
		weight := entries[index].End - entries[index].Start
		if weight <= 0 || math.IsNaN(weight) || math.IsInf(weight, 0) {
			return
		}
		weights[index] = weight
		total += weight
	}
	if total <= 0 {
		return
	}
	previousEnd := 0.0
	for index := range entries {
		entries[index].Start = previousEnd
		entries[index].End = previousEnd + duration*weights[index]/total
		if index == len(entries)-1 {
			entries[index].End = duration
		}
		previousEnd = entries[index].End
	}
}

// Normalize only the unambiguous string-array variant emitted by some models.
// Keep all other fields raw so the strict decoder still rejects unknown fields.
func normalizeDirectorDialogueArrays(output string) (string, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal([]byte(output), &root); err != nil {
		return "", err
	}
	var shots []map[string]json.RawMessage
	if err := json.Unmarshal(root["shots"], &shots); err != nil {
		return "", err
	}
	changed := false
	for i, shot := range shots {
		raw := bytes.TrimSpace(shot["dialogue"])
		if len(raw) == 0 || raw[0] != '[' {
			continue
		}
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return "", fmt.Errorf("镜头%d的dialogue数组无效: %w", i+1, err)
		}
		parts := make([]string, len(items))
		for j, item := range items {
			item = bytes.TrimSpace(item)
			if len(item) == 0 || item[0] != '"' {
				return "", fmt.Errorf("镜头%d的dialogue数组第%d项必须为原始台词字符串，不能自动丢弃对象或其他类型", i+1, j+1)
			}
			if err := json.Unmarshal(item, &parts[j]); err != nil {
				return "", err
			}
		}
		// No added separators: authoritative dialogue range matching uses the exact
		// concatenated rune sequence, including the original punctuation and spaces.
		shot["dialogue"], _ = json.Marshal(strings.Join(parts, ""))
		changed = true
	}
	if !changed {
		return output, nil
	}
	root["shots"], _ = json.Marshal(shots)
	encoded, err := json.Marshal(root)
	return string(encoded), err
}

func parseSceneDirectorDraft(output string) (*sceneDirectorDraft, error) {
	return parseSceneDirectorDraftWithOptions(output, false)
}

func parseSceneDirectorDraftWithOptions(output string, preserveShortShots bool) (*sceneDirectorDraft, error) {
	normalized, err := normalizeDirectorDialogueArrays(unwrapSceneDirectorDraftEnvelope(output))
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewBufferString(normalized))
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
		d.ActType = normalizeDirectorAct(d.ActType)
		d.TransitionType = normalizeDirectorTransition(d.TransitionType)
		normalizeDirectorActionTimeline(d.ActionTimeline, d.Duration)
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
	if !preserveShortShots {
		if err := normalizeShortDirectorShots(&draft); err != nil {
			return nil, err
		}
	}
	for i := range draft.Shots {
		d := &draft.Shots[i]
		d.ActType = normalizeDirectorAct(d.ActType)
		d.TransitionType = normalizeDirectorTransition(d.TransitionType)
		normalizeDirectorActionTimeline(d.ActionTimeline, d.Duration)
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
		d.SpeechPresentation = models.ShotSpeechPresentation(strings.TrimSpace(string(d.SpeechPresentation)))
		d.ListenerCharacter = strings.TrimSpace(d.ListenerCharacter)
		if d.Duration <= 0 || d.Duration > 15 || (!preserveShortShots && d.Duration < 3) || math.IsNaN(d.Duration) || math.IsInf(d.Duration, 0) {
			return nil, fmt.Errorf("镜头%d时长必须为3到15秒", i+1)
		}
		validationDuration := d.Duration
		if preserveShortShots && validationDuration < 3 {
			validationDuration = 3
		}
		shot := models.Shot{ActType: d.ActType, ShotType: d.ShotType, CameraAngle: d.CameraAngle, CameraMovement: d.CameraMovement, TransitionType: d.TransitionType, TransitionNote: d.TransitionNote, StartState: d.StartState, EndState: d.EndState, Duration: validationDuration, Description: d.Description, Dialogue: d.Dialogue, SpeechPresentation: d.SpeechPresentation, ListenerCharacter: d.ListenerCharacter, Emotion: d.Emotion, PromptSubject: d.PromptSubject, PromptAction: d.PromptAction, PromptCamera: d.PromptCamera, PromptLighting: d.PromptLighting, PromptStyle: d.PromptStyle, NegativePrompt: d.NegativePrompt, ActionTimeline: d.ActionTimeline}
		if preserveShortShots && d.Duration < 3 && len(shot.ActionTimeline) > 0 {
			normalizeDirectorActionTimeline(shot.ActionTimeline, validationDuration)
		}
		if err := validateShot(&shot); err != nil {
			return nil, fmt.Errorf("镜头%d: %w", i+1, err)
		}
		for j := range d.Checks {
			d.Checks[j] = strings.TrimSpace(d.Checks[j])
		}
	}
	return &draft, nil
}

func dialogueRhythmDirectorInstruction(dialogues []models.Dialogue, characterNames ...string) string {
	var lines []string
	for i, d := range dialogues {
		speechType, speaker := normalizeScriptSpeech(d.SpeechType, d.Character)
		lines = append(lines, fmt.Sprintf("D%d｜speech_type=%s｜speaker=%s｜text=%s", i+1, speechType, strings.TrimSpace(speaker), strings.TrimSpace(d.Text)))
	}
	allowed := []string{}
	seen := map[string]bool{}
	for _, name := range characterNames {
		name = strings.TrimSpace(name)
		if name != "" && !seen[name] {
			seen[name] = true
			allowed = append(allowed, name)
		}
	}
	sort.Strings(allowed)
	listenerRule := "当前项目没有可选的权威角色；不得生成listener_reaction_carryover。"
	if len(allowed) > 0 {
		listenerRule = "listener_character只能从以下当前项目权威角色中逐字选择，不得使用别名、称谓或创造人物：" + strings.Join(allowed, "、") + "。"
	}
	return `按对白自然语速和语义停顿拆成多个3–15秒Native H3镜头。可以交替使用说话人近景、包含说话人的双人/前后景镜头，以及对白结束后的纯听者反应镜头。硬规则：
1. Dialogue字段只能填下列结构化对白的连续原文片段，不得改写、增删、重复或创造旁白；无发声镜头必须为空。
2. 所有镜头Dialogue按顺序拼接后必须逐字等于下列完整对白原文按顺序拼接的结果。
3. 普通dialogue默认让真实说话人清晰可见并由其同步口型；也允许同一句对白跨切到纯听者反应镜头，但必须明确写出“原说话人姓名+声音跨切延续”，听者必须闭口，且不得把这种连续对白改写成旁白或独立画外音。只有明确speech_type为offscreen_dialogue时才设计独立画外对白且说话人不得入镜；narration不得驱动画面人物口型；monologue必须明确人物闭口且不做口型。
4. 纯听者反应镜头承载普通对白时只能承接上一镜已经开始的同一句对白；不能在听者镜头中开始一条新对白，也不能改变说话人。
5. 同一句对白没有逗号、句号、分号、冒号或省略号等停顿依据时，默认禁止在该处切镜；仅允许说话人在前后镜持续可见并同步表演，或按第3、4条明确切到闭口听者反应且原说话人声音跨切延续。
6. 每个镜头只能承载一个权威声音身份（同一说话人且同一speech_type）；说话人变化或dialogue、offscreen_dialogue、narration、monologue之间切换时必须切成新的Native镜头，不能用一个speech_presentation覆盖多个声音。
7. 每镜3–15秒，一个主要情绪、一个主要动作、一种主要构图和明确结束状态。
8. 对白自然时长决定总时长，不得压缩语速，也不得用重复动作填时长。
9. 每个输出必须是独立可制作的Native H3镜头，禁止使用 Endless 或 Canvas，也禁止无限延长和单条连续长镜头。
10. 每镜必须填写speech_presentation：可见说话人口型用visible_speaker_lipsync；闭口听者承接同句声音用listener_reaction_carryover并在listener_character填写唯一听者真实姓名；独立画外对白用offscreen_dialogue；旁白用narration_voiceover；内心独白用internal_monologue；无发声用silent_visual。
` + listenerRule + `
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

func normalizeCombatDraftTempoText(value string) string {
	return strings.NewReplacer(
		"慢动作", "实时动作", "慢镜头", "实时镜头", "子弹时间", "实时速度", "缓缓", "立即", "逐渐", "随即", "慢慢", "立即", "悬停", "短暂维持", "长时间蓄力", "立即起势", "戏剧性停顿", "动作余势", "定格展示", "清晰展示",
		"slow-motion", "real-time", "slow motion", "real-time", "bullet time", "real-time speed", "lingering", "brief", "gradually", "then",
	).Replace(value)
}

func normalizeCombatReferenceDraftContracts(draft *sceneDirectorDraft) {
	if draft == nil {
		return
	}
	for index := range draft.Shots {
		shot := &draft.Shots[index]
		shot.Description = normalizeCombatDraftTempoText(shot.Description)
		shot.PromptAction = normalizeCombatDraftTempoText(shot.PromptAction)
		shot.PromptCamera = normalizeCombatDraftTempoText(shot.PromptCamera)
		shot.TransitionNote = normalizeCombatDraftTempoText(shot.TransitionNote)
		if index > 0 && strings.TrimSpace(draft.Shots[index-1].EndState) != "" && strings.TrimSpace(shot.StartState) != strings.TrimSpace(draft.Shots[index-1].EndState) {
			shot.StartState = strings.TrimSpace(draft.Shots[index-1].EndState)
			shot.Checks = append(shot.Checks, "系统已将本镜start_state确定性对齐上一镜end_state")
		}
		for _, constraint := range []string{"实时速度", "无慢动作"} {
			if !strings.Contains(shot.PromptAction, constraint) {
				shot.PromptAction = joinDirectorField(shot.PromptAction, constraint)
			}
		}
		for _, constraint := range []string{"慢动作", "子弹时间", "动作拖沓"} {
			if !strings.Contains(shot.NegativePrompt, constraint) {
				shot.NegativePrompt = joinDirectorField(shot.NegativePrompt, constraint)
			}
		}
	}
}

func validateCombatReferenceDraft(draft *sceneDirectorDraft, dialogues []models.Dialogue) error {
	if draft == nil || len(draft.Shots) == 0 {
		return fmt.Errorf("战斗资料导演草稿为空")
	}
	for i, shot := range draft.Shots {
		if shot.Duration < 3 || shot.Duration > 15 {
			return fmt.Errorf("战斗资料镜头%d时长必须为3至15秒", i+1)
		}
		if shot.Duration > 6 && len(shot.ActionTimeline) == 0 {
			return fmt.Errorf("战斗资料镜头%d超过6秒，必须用动作时间轴证明动作因果和连续覆盖", i+1)
		}
		combined := strings.Join([]string{shot.Description, shot.PromptAction, shot.PromptCamera, shot.TransitionNote}, " ")
		combined = strings.NewReplacer("无慢动作停顿", "", "无慢动作", "", "no slow motion", "").Replace(strings.ToLower(combined))
		if slowActionDraftPattern.MatchString(combined) {
			return fmt.Errorf("战斗资料镜头%d包含慢动作或拖延表达", i+1)
		}
		for _, want := range []string{"实时速度", "无慢动作"} {
			if !strings.Contains(shot.PromptAction, want) {
				return fmt.Errorf("战斗资料镜头%d的prompt_action缺少“%s”约束", i+1, want)
			}
		}
		for _, want := range []string{"慢动作", "子弹时间", "动作拖沓"} {
			if !strings.Contains(shot.NegativePrompt, want) {
				return fmt.Errorf("战斗资料镜头%d的negative_prompt缺少“%s”", i+1, want)
			}
		}
		if i > 0 {
			if strings.TrimSpace(shot.StartState) == "" || strings.TrimSpace(draft.Shots[i-1].EndState) == "" {
				return fmt.Errorf("战斗资料镜头%d缺少与上一镜衔接的起止状态", i+1)
			}
			if strings.TrimSpace(shot.StartState) != strings.TrimSpace(draft.Shots[i-1].EndState) {
				return fmt.Errorf("战斗资料镜头%d的start_state必须原样承接上一镜end_state", i+1)
			}
		}
	}
	return validateDialogueRhythmDraft(draft, dialogues)
}

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
	// Dialogue text is production authority. Normalize only platform newlines;
	// spaces, line breaks and punctuation inside the spoken text remain exact.
	return strings.ReplaceAll(value, "\r\n", "\n")
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

func normalizeDialogueRhythmReviewDurations(draft *sceneDirectorDraft, dialogues []models.Dialogue) error {
	for _, review := range buildDialogueRhythmReview(draft, dialogues) {
		if review.NaturalMinimum > maxSceneVideoDuration {
			return fmt.Errorf("对白拆镜%d权威语音自然时长下限%.1f秒超过Native H3的15秒上限，必须继续拆镜", review.Shot, review.NaturalMinimum)
		}
		shot := &draft.Shots[review.Shot-1]
		if shot.Duration+0.001 < review.NaturalMinimum {
			shot.Duration = review.NaturalMinimum
			shot.Checks = append(shot.Checks, fmt.Sprintf("系统已按权威Dialogue审计将镜头时长调整为%.1f秒", review.NaturalMinimum))
		}
	}
	return nil
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

func shotVisualText(shot sceneDirectorDraftShot) string {
	return strings.Join([]string{shot.ShotType, shot.Description, shot.PromptSubject, shot.PromptAction, shot.StartState, shot.EndState}, " ")
}

func shotExplicitlyExcludesSpeaker(shot sceneDirectorDraftShot, speaker string) bool {
	speaker = strings.TrimSpace(speaker)
	if speaker == "" {
		return false
	}
	visual := shotVisualText(shot)
	for _, marker := range []string{"不含" + speaker, "没有" + speaker, "无" + speaker, speaker + "不入镜", speaker + "未入镜", speaker + "画外"} {
		if strings.Contains(visual, marker) {
			return true
		}
	}
	return false
}

func shotExplicitlyClosesSpeaker(shot sceneDirectorDraftShot, speaker string) bool {
	speaker = strings.TrimSpace(speaker)
	if speaker == "" {
		return false
	}
	visual := strings.ToLower(shotVisualText(shot))
	for _, marker := range []string{speaker + "闭口", speaker + "双唇闭合", speaker + "嘴唇闭合", speaker + "不张口", speaker + "不做口型", speaker + "不做同步口型", speaker + "不得同步口型", speaker + "禁止同步口型"} {
		if strings.Contains(visual, strings.ToLower(marker)) {
			return true
		}
	}
	return false
}

func shotShowsSpeaker(shot sceneDirectorDraftShot, speaker string) bool {
	speaker = strings.TrimSpace(speaker)
	if speaker == "" {
		return false
	}
	visual := shotVisualText(shot)
	return strings.Contains(visual, speaker) && !shotExplicitlyExcludesSpeaker(shot, speaker)
}

func shotExplicitlyShowsSpeaker(shot sceneDirectorDraftShot, speaker string) bool {
	speaker = strings.TrimSpace(speaker)
	if speaker == "" || shotExplicitlyExcludesSpeaker(shot, speaker) {
		return false
	}
	visual := strings.ToLower(shotVisualText(shot))
	for _, marker := range []string{
		speaker + "入镜", speaker + "在画面内", speaker + "站在画面内", speaker + "位于画面内", speaker + "出现在画面", speaker + "正面入镜",
		speaker + "近景", speaker + "特写", speaker + "中景", speaker + "全身", speaker + "开口说", speaker + "张口说",
	} {
		if strings.Contains(visual, strings.ToLower(marker)) {
			return true
		}
	}
	return false
}

func shotHasLipSync(shot sceneDirectorDraftShot) bool {
	visual := strings.ToLower(shotVisualText(shot))
	for _, negated := range []string{
		"不做同步口型", "不进行同步口型", "不同步口型", "不得同步口型", "禁止同步口型", "无需同步口型", "无口型同步",
		"不张口说", "不开口说", "禁止开口说", "不得开口说", "no lip-sync", "no lip sync", "without lip-sync", "without lip sync", "do not lip-sync", "must not lip-sync",
	} {
		visual = strings.ReplaceAll(visual, negated, "")
	}
	for _, marker := range []string{"同步口型", "口型同步", "开口说", "张口说", "嘴唇同步", "lip-sync", "lip sync"} {
		if strings.Contains(visual, marker) {
			return true
		}
	}
	return false
}

func shotHasClosedLips(shot sceneDirectorDraftShot) bool {
	visual := shotVisualText(shot)
	for _, marker := range []string{"闭口", "双唇闭合", "嘴唇闭合", "不张口", "不做口型", "不做同步口型", "不得同步口型", "禁止同步口型", "禁止开口说", "不得开口说", "不开口说", "closed lips", "mouth closed", "no lip-sync", "no lip sync"} {
		if strings.Contains(strings.ToLower(visual), strings.ToLower(marker)) {
			return true
		}
	}
	return false
}

func validShotSpeechPresentation(value models.ShotSpeechPresentation) bool {
	switch value {
	case "", models.ShotSpeechPresentationVisibleSpeaker, models.ShotSpeechPresentationListenerCarry,
		models.ShotSpeechPresentationOffscreen, models.ShotSpeechPresentationNarration,
		models.ShotSpeechPresentationMonologue, models.ShotSpeechPresentationSilent:
		return true
	default:
		return false
	}
}

func ordinaryDialogueCarryoverAllowed(shot sceneDirectorDraftShot, speaker string, startsAfterDialogueStart bool) bool {
	if !startsAfterDialogueStart || strings.TrimSpace(speaker) == "" {
		return false
	}
	if shot.SpeechPresentation == models.ShotSpeechPresentationListenerCarry {
		// The persisted enum is the closed-mouth listener contract. Free text is only
		// consulted to reject an explicit contradictory lip-sync instruction.
		return !shotHasLipSync(shot)
	}
	// Compatibility fallback for historical shots without structured presentation.
	visual := shotVisualText(shot)
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
	listenerReaction := strings.Contains(visual, "听者") || strings.Contains(visual, "聆听") || strings.Contains(visual, "倾听") || strings.Contains(visual, "反应")
	return carryover && listenerReaction && shotHasClosedLips(shot) && !shotHasLipSync(shot)
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
		voiceIdentities := map[string]bool{}
		if length > 0 {
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
				speechType, normalizedSpeaker := normalizeScriptSpeech(item.dialogue.SpeechType, item.dialogue.Character)
				voiceIdentities[speechType+"\x00"+strings.TrimSpace(normalizedSpeaker)] = true
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
		}
		presentation := string(shot.SpeechPresentation)
		if presentation == "" {
			presentation = string(models.ShotSpeechPresentationSilent)
			if length > 0 {
				types := map[string]bool{}
				for _, fragment := range fragments {
					types[fragment.SpeechType] = true
				}
				switch {
				case len(types) > 1:
					presentation = "mixed_authoritative_speech"
				case types["narration"]:
					presentation = string(models.ShotSpeechPresentationNarration)
				case types["monologue"]:
					presentation = string(models.ShotSpeechPresentationMonologue)
				case types["offscreen_dialogue"]:
					presentation = string(models.ShotSpeechPresentationOffscreen)
				default:
					presentation = string(models.ShotSpeechPresentationVisibleSpeaker)
					for _, speaker := range speakers {
						if ordinaryDialogueCarryoverAllowed(shot, speaker, continuesFrom) {
							presentation = string(models.ShotSpeechPresentationListenerCarry)
							break
						}
					}
				}
			}
		}
		minimum := dialogueReviewNaturalMinimum(fragments)
		issues := []string{}
		if len(voiceIdentities) > 1 {
			issues = append(issues, "本镜跨越多个权威说话人或语音类型，单一speech_presentation无法无歧义表达；必须按说话人或语音类型拆成多个Native镜头")
		}
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
		reviews = append(reviews, dialogueRhythmShotReview{Shot: i + 1, StartTime: timeCursor, EndTime: timeCursor + shot.Duration, DialogueStart: start, DialogueEnd: end, NaturalMinimum: minimum, Ready: len(issues) == 0, Status: status, BlockingIssues: issues, Advisories: []string{}, Speakers: speakers, Presentation: presentation, ListenerCharacter: strings.TrimSpace(shot.ListenerCharacter), ContinuesFromPrev: continuesFrom, ContinuesToNext: continuesTo, Fragments: fragments})
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
	}
	for i := 0; i+1 < len(reviews); i++ {
		left, right := &reviews[i], &reviews[i+1]
		if left.ExitBoundary != "同句跨镜续接" || right.EntryBoundary != "同句跨镜续接" || dialogueCutHasValidPerformance(draft.Shots[i], draft.Shots[i+1], *left, *right) {
			continue
		}
		issue := "无标点的同一句对白不得跨镜切分；仅允许说话人持续可见并同步表演，或明确的闭口听者反应声音跨切"
		for _, review := range []*dialogueRhythmShotReview{left, right} {
			review.BlockingIssues = append(review.BlockingIssues, issue)
			review.Ready = false
			review.Status = "blocked"
		}
	}
	return reviews
}

func dialogueRhythmBlockingIssuesForShots(shots []models.Shot, dialogues []models.Dialogue) []string {
	draft := &sceneDirectorDraft{Shots: make([]sceneDirectorDraftShot, len(shots))}
	for i, shot := range shots {
		draft.Shots[i] = sceneDirectorDraftShot{ShotType: shot.ShotType, Duration: shot.Duration, Description: shot.Description, Dialogue: shot.Dialogue, SpeechPresentation: shot.SpeechPresentation, ListenerCharacter: shot.ListenerCharacter, StartState: shot.StartState, EndState: shot.EndState, PromptSubject: shot.PromptSubject, PromptAction: shot.PromptAction}
	}
	seen := map[string]bool{}
	var issues []string
	for _, review := range buildDialogueRhythmReview(draft, dialogues) {
		for _, issue := range review.BlockingIssues {
			if !strings.Contains(issue, "无标点的同一句对白不得跨镜切分") || seen[issue] {
				continue
			}
			seen[issue] = true
			issues = append(issues, issue)
		}
	}
	return issues
}

func dialogueCutHasValidPerformance(leftShot, rightShot sceneDirectorDraftShot, left, right dialogueRhythmShotReview) bool {
	if len(left.Fragments) == 0 || len(right.Fragments) == 0 {
		return false
	}
	before, after := left.Fragments[len(left.Fragments)-1], right.Fragments[0]
	speaker := strings.TrimSpace(before.Character)
	if before.SpeechType != "dialogue" || after.SpeechType != "dialogue" || before.DialogueID != after.DialogueID || speaker == "" || speaker != strings.TrimSpace(after.Character) {
		return false
	}
	if left.Presentation == "visible_speaker_lipsync" && right.Presentation == "visible_speaker_lipsync" && shotShowsSpeaker(leftShot, speaker) && shotShowsSpeaker(rightShot, speaker) {
		return true
	}
	return right.Presentation == "listener_reaction_carryover" && ordinaryDialogueCarryoverAllowed(rightShot, speaker, right.ContinuesFromPrev)
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

func authoritativeSceneListenerCharacters(db *gorm.DB, scene *models.Scene) ([]string, map[string]bool, error) {
	list := []string{}
	set := map[string]bool{}
	if scene == nil {
		return list, set, nil
	}
	visibleNames := visibleSceneCharacterNames(scene)
	if len(visibleNames) == 0 {
		return list, set, nil
	}
	var characters []models.Character
	if err := db.Select("name").Where("project_id = ? AND name IN ?", scene.ProjectID, visibleNames).Find(&characters).Error; err != nil {
		return nil, nil, err
	}
	for _, character := range characters {
		name := strings.TrimSpace(character.Name)
		if name != "" && !set[name] {
			set[name] = true
			list = append(list, name)
		}
	}
	sort.Strings(list)
	return list, set, nil
}

func validateDraftListenerCharacters(draft *sceneDirectorDraft, authoritativeNames map[string]bool) error {
	if draft == nil {
		return fmt.Errorf("对白节奏导演草稿为空")
	}
	for i, shot := range draft.Shots {
		if shot.SpeechPresentation != models.ShotSpeechPresentationListenerCarry {
			continue
		}
		listener := strings.TrimSpace(shot.ListenerCharacter)
		if listener != "" && !authoritativeNames[listener] {
			return fmt.Errorf("对白拆镜%d的listener_character“%s”不是当前项目权威角色", i+1, listener)
		}
	}
	return nil
}

func validateGeneratedDialogueRhythmDraft(draft *sceneDirectorDraft, dialogues []models.Dialogue) error {
	if err := requireStructuredSpeechPresentations(draft); err != nil {
		return err
	}
	if err := validateDialogueRhythmDraft(draft, dialogues); err != nil {
		return err
	}
	return normalizeDialogueRhythmReviewDurations(draft, dialogues)
}

func requireStructuredSpeechPresentations(draft *sceneDirectorDraft) error {
	if draft == nil {
		return fmt.Errorf("对白节奏导演草稿为空")
	}
	for i, shot := range draft.Shots {
		if shot.SpeechPresentation == "" {
			return fmt.Errorf("对白拆镜%d缺少speech_presentation；新草稿必须显式声明权威发声呈现", i+1)
		}
		if !validShotSpeechPresentation(shot.SpeechPresentation) {
			return fmt.Errorf("对白拆镜%d的speech_presentation无效", i+1)
		}
		listener := strings.TrimSpace(shot.ListenerCharacter)
		if shot.SpeechPresentation == models.ShotSpeechPresentationListenerCarry && listener == "" {
			return fmt.Errorf("对白拆镜%d使用listener_reaction_carryover时必须填写listener_character", i+1)
		}
		if shot.SpeechPresentation != models.ShotSpeechPresentationListenerCarry && listener != "" {
			return fmt.Errorf("对白拆镜%d仅listener_reaction_carryover可填写listener_character", i+1)
		}
	}
	return nil
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
		if !validShotSpeechPresentation(shot.SpeechPresentation) {
			return fmt.Errorf("对白拆镜%d的speech_presentation无效", i+1)
		}
		text := canonicalDialogueText(shot.Dialogue)
		if text == "" && shot.SpeechPresentation != "" && shot.SpeechPresentation != models.ShotSpeechPresentationSilent {
			return fmt.Errorf("对白拆镜%d没有权威发声片段，speech_presentation必须为silent_visual", i+1)
		}
		if text != "" && shot.SpeechPresentation == models.ShotSpeechPresentationSilent {
			return fmt.Errorf("对白拆镜%d承载权威发声片段，speech_presentation不能为silent_visual", i+1)
		}
		actual.WriteString(text)
	}
	if actual.String() != expected.String() {
		return fmt.Errorf("导演方案未逐字覆盖场景正文中的原始台词，禁止遗漏、改写、重复或新增")
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
		voiceIdentities := map[string]bool{}
		for _, span := range spans {
			if end <= span.start || start >= span.end {
				continue
			}
			speechType, normalizedSpeaker := normalizeScriptSpeech(span.dialogue.SpeechType, span.dialogue.Character)
			voiceIdentities[speechType+"\x00"+strings.TrimSpace(normalizedSpeaker)] = true
		}
		if len(voiceIdentities) > 1 {
			return fmt.Errorf("对白拆镜%d跨越多个权威说话人或语音类型，必须拆成多个Native镜头", i+1)
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
				if shot.SpeechPresentation != "" && shot.SpeechPresentation != models.ShotSpeechPresentationOffscreen {
					return fmt.Errorf("对白拆镜%d的speech_presentation必须为offscreen_dialogue", i+1)
				}
				speakerVisible := shotShowsSpeaker(shot, speaker)
				if shot.SpeechPresentation == models.ShotSpeechPresentationOffscreen {
					// The persisted enum establishes that the named speaker is outside the
					// frame. Text is only used to reject an explicit visual contradiction.
					speakerVisible = shotExplicitlyShowsSpeaker(shot, speaker)
				}
				if speakerVisible {
					return fmt.Errorf("对白拆镜%d承载角色“%s”的独立画外对白时，说话人不得出现在画面主体中；如需可见口型请改用dialogue", i+1, speaker)
				}
				continue
			}
			if speechType == "narration" {
				if shot.SpeechPresentation != "" && shot.SpeechPresentation != models.ShotSpeechPresentationNarration {
					return fmt.Errorf("对白拆镜%d的speech_presentation必须为narration_voiceover", i+1)
				}
				if shotHasLipSync(shot) {
					return fmt.Errorf("对白拆镜%d承载旁白时不得驱动画面人物口型", i+1)
				}
				continue
			}
			if speechType == "monologue" {
				if shot.SpeechPresentation != "" && shot.SpeechPresentation != models.ShotSpeechPresentationMonologue {
					return fmt.Errorf("对白拆镜%d的speech_presentation必须为internal_monologue", i+1)
				}
				if shotHasLipSync(shot) || (shot.SpeechPresentation == "" && !shotHasClosedLips(shot)) {
					return fmt.Errorf("对白拆镜%d承载角色“%s”的内心独白时，必须明确人物闭口且不得同时要求同步口型", i+1, speaker)
				}
				continue
			}
			if speechType != "dialogue" {
				continue
			}
			if shot.SpeechPresentation != "" && shot.SpeechPresentation != models.ShotSpeechPresentationVisibleSpeaker && shot.SpeechPresentation != models.ShotSpeechPresentationListenerCarry {
				return fmt.Errorf("对白拆镜%d的普通对白speech_presentation必须为visible_speaker_lipsync或listener_reaction_carryover", i+1)
			}
			if shot.SpeechPresentation == models.ShotSpeechPresentationListenerCarry {
				listener := strings.TrimSpace(shot.ListenerCharacter)
				if listener == "" {
					return fmt.Errorf("对白拆镜%d使用listener_reaction_carryover时必须填写listener_character", i+1)
				}
				if listener == speaker {
					return fmt.Errorf("对白拆镜%d的listener_character不能与权威说话人“%s”相同", i+1, speaker)
				}
			}
			if shot.SpeechPresentation == models.ShotSpeechPresentationVisibleSpeaker {
				if shotExplicitlyExcludesSpeaker(shot, speaker) || shotExplicitlyClosesSpeaker(shot, speaker) {
					return fmt.Errorf("对白拆镜%d声明visible_speaker_lipsync，但文本明确要求说话人“%s”不入镜或闭口", i+1, speaker)
				}
				continue
			}
			speakerVisible := shotShowsSpeaker(shot, speaker)
			if speakerVisible && shotHasLipSync(shot) {
				continue
			}
			if ordinaryDialogueCarryoverAllowed(shot, speaker, start > span.start) {
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
		Brief            string                     `json:"brief"`
		Requirements     string                     `json:"requirements"`
		Mode             string                     `json:"mode"`
		CombatReferences []CombatReferenceSelection `json:"combat_references"`
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
	// Director generation is an explicit write operation: materialize only unambiguous
	// speech from Scene.Content before taking the snapshot used by generation and save.
	if err := s.DB.Transaction(func(tx *gorm.DB) error { return syncExplicitSceneSpeech(tx, scene) }); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "同步场景正文台词失败，请稍后重试"})
		return
	}
	assets, err := s.sceneAssetBible(scene)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	operation, instruction := "director-scene-draft", "生成完整Scene导演方案草稿；只预览，不保存。"
	combatReferenceText := ""
	var combatReferenceAudit []CombatReferenceAudit
	if req.Mode == "combat_design" {
		if s.CombatReferences == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "战斗资料库未加载"})
			return
		}
		bundle, compileErr := s.CombatReferences.CompileSelection(req.CombatReferences, 30000)
		if compileErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": compileErr.Error()})
			return
		}
		combatReferenceText, combatReferenceAudit = bundle.Text, bundle.Audit
		operation = "fight-video-action-design"
		instruction = "依据用户已确认的战斗资料生成可审核的武戏导演方案；资料只提供机制与结构，不得改写权威事实。"
	}
	var dialogues []models.Dialogue
	authoritativeCharacterNames := map[string]bool{}
	authoritativeCharacterList := []string{}
	dialogueDuration := 0.0
	if err := s.DB.Where("scene_id = ? AND project_id = ?", scene.ID, scene.ProjectID).Order("`order` ASC, `id` ASC").Find(&dialogues).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	dialogues = authoritativeSceneDialogues(dialogues)
	if req.Mode == "dialogue_rhythm" || req.Mode == "action_rhythm" || req.Mode == "combat_design" {
		if req.Mode == "dialogue_rhythm" {
			var listenerErr error
			authoritativeCharacterList, authoritativeCharacterNames, listenerErr = authoritativeSceneListenerCharacters(s.DB, scene)
			if listenerErr != nil {
				c.JSON(500, gin.H{"error": listenerErr.Error()})
				return
			}
			if len(dialogues) == 0 {
				c.JSON(400, gin.H{"error": "当前Scene没有可用于拆镜的结构化对白"})
				return
			}
			dialogueDuration = modelDialoguesMinDuration(dialogues)
			instruction = dialogueRhythmDirectorInstruction(dialogues, authoritativeCharacterList...)
		} else if req.Mode == "action_rhythm" {
			instruction = actionRhythmDirectorInstruction(dialogues)
		}
		req.Requirements = strings.TrimSpace(req.Requirements + "\n" + instruction)
	}
	if req.Mode == "standard" {
		resolvedMode := classifyH3SceneMode(scene, nil, dialogues)
		modeInstruction := h3SceneModeInstruction(resolvedMode)
		instruction = strings.TrimSpace(instruction + "\n" + modeInstruction)
		req.Requirements = strings.TrimSpace(req.Requirements + "\n用户选择的导演类型：" + string(resolvedMode) + "。\n" + modeInstruction)
	}
	dialogueContext := ""
	for _, dialogue := range dialogues {
		dialogueContext += fmt.Sprintf("[%s|%s] %s\n", dialogue.SpeechType, dialogue.Character, dialogue.Text)
	}
	output, err := s.Skills.ChatWithConfiguredOrFallbackSkill(scene.ProjectID, models.SkillStageStoryboard, operation, s.TextProviderFact, "只输出完整合法JSON，不要Markdown或解释。", instruction, map[string]string{"scene_facts": scene.Content, "asset_context": assets, "dialogues": dialogueContext, "brief": req.Brief, "requirements": req.Requirements, "combat_references": combatReferenceText, "target_duration": fmt.Sprintf("%.1f", math.Max(scene.Duration, dialogueDuration))})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	validateDraft := func(raw string) (*sceneDirectorDraft, error) {
		preserveShortShots := req.Mode == "dialogue_rhythm"
		draft, parseErr := parseSceneDirectorDraftWithOptions(raw, preserveShortShots)
		if parseErr == nil && req.Mode == "dialogue_rhythm" {
			normalizeDialogueRhythmDraftDurations(draft)
			parseErr = validateGeneratedDialogueRhythmDraft(draft, dialogues)
			if parseErr == nil {
				parseErr = validateDraftListenerCharacters(draft, authoritativeCharacterNames)
			}
		} else if parseErr == nil && req.Mode == "action_rhythm" {
			parseErr = validateActionRhythmDraft(draft, dialogues)
		} else if parseErr == nil && req.Mode == "combat_design" {
			normalizeCombatReferenceDraftContracts(draft)
			parseErr = validateCombatReferenceDraft(draft, dialogues)
		}
		return draft, parseErr
	}
	type serverRepair struct {
		Stage  string `json:"stage"`
		Reason string `json:"reason"`
		Result string `json:"result"`
	}
	serverRepairs := []serverRepair{}
	draft, err := validateDraft(output)
	if err != nil && (req.Mode == "dialogue_rhythm" || req.Mode == "action_rhythm" || req.Mode == "combat_design") {
		initialValidationError := err.Error()
		repairRequirements := fmt.Sprintf("%s\n上一次草稿校验失败：%s。只修复该错误及其他空必填字段；保持镜头顺序、剧情事实和结构化对白逐字不变。上一次JSON：\n%s", req.Requirements, err.Error(), output)
		repairInstruction := dialogueRhythmDirectorInstruction(dialogues, authoritativeCharacterList...) + "\n修复对白节奏导演草稿；不得改写、遗漏、重复或新增任何对白。若一个镜头跨越多个权威说话人或speech_type，必须保持原文顺序并拆成多个Native镜头，每镜只保留一个声音身份。必须保留或补齐每镜speech_presentation；仅listener_reaction_carryover可填写listener_character，且必须从上述权威角色白名单逐字选择。"
		if req.Mode == "action_rhythm" || req.Mode == "combat_design" {
			repairInstruction = "修复武戏/仙术动作节拍草稿；保持实时速度、单一动作结果、空间轴线和结构化对白，不得使用慢动作。"
		}
		repaired, repairErr := s.Skills.ChatWithConfiguredOrFallbackSkill(scene.ProjectID, models.SkillStageStoryboard, operation, s.TextProviderFact, "只输出修复后的完整合法JSON，不要Markdown或解释。", repairInstruction, map[string]string{"scene_facts": scene.Content, "asset_context": assets, "dialogues": dialogueContext, "brief": req.Brief, "requirements": repairRequirements, "combat_references": combatReferenceText, "target_duration": fmt.Sprintf("%.1f", math.Max(scene.Duration, dialogueDuration))})
		if repairErr != nil {
			serverRepairs = append(serverRepairs, serverRepair{Stage: "model_repair", Reason: initialValidationError, Result: "provider_error: " + repairErr.Error()})
			c.JSON(http.StatusBadGateway, gin.H{"error": "AI导演方案自动修复失败: " + repairErr.Error(), "server_repairs": serverRepairs})
			return
		}
		draft, err = validateDraft(repaired)
		repairResult := "accepted"
		if err != nil {
			repairResult = "still_invalid: " + err.Error()
		}
		serverRepairs = append(serverRepairs, serverRepair{Stage: "model_repair", Reason: initialValidationError, Result: repairResult})
		if err != nil && strings.Contains(err.Error(), "导演方案未逐字覆盖场景正文中的原始台词") && draft != nil {
			restoreReason := err.Error()
			if restoreErr := restoreDialogueRhythmDraftText(draft, dialogues); restoreErr != nil {
				err = restoreErr
			} else {
				err = validateGeneratedDialogueRhythmDraft(draft, dialogues)
				if err == nil {
					err = validateDraftListenerCharacters(draft, authoritativeCharacterNames)
				}
			}
			if strings.TrimSpace(restoreReason) != "" {
				restoreResult := "accepted"
				if err != nil {
					restoreResult = "still_invalid: " + err.Error()
				}
				serverRepairs = append(serverRepairs, serverRepair{Stage: "authoritative_dialogue_restore", Reason: restoreReason, Result: restoreResult})
			}
		}
	}
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "AI导演方案自动修复后仍无效: " + err.Error(), "server_repairs": serverRepairs})
		return
	}
	response := gin.H{"draft": draft, "skill_code": operation, "provider_id": s.TextProviderFact.Name(), "audited": true, "mode": req.Mode, "dialogue_duration": dialogueDuration, "server_repairs": serverRepairs}
	if req.Mode == "combat_design" {
		response["combat_reference_audit"] = combatReferenceAudit
		response["combat_reference_selection"] = req.CombatReferences
	}
	if req.Mode == "dialogue_rhythm" {
		response["dialogue_review"] = buildDialogueRhythmReview(draft, dialogues)
	}
	response["dialogue_snapshot"] = dialogueSnapshotToken(dialogues)
	c.JSON(200, response)
}
