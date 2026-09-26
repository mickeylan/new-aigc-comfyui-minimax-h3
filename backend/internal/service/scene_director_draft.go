package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
)

type sceneDirectorDraft struct {
	Shots []sceneDirectorDraftShot `json:"shots"`
}
type sceneDirectorDraftShot struct {
	ActType        models.ShotActType        `json:"act_type"`
	ShotType       string                    `json:"shot_type"`
	CameraAngle    string                    `json:"camera_angle"`
	CameraMovement string                    `json:"camera_movement"`
	Duration       float64                   `json:"duration"`
	Description    string                    `json:"description"`
	Dialogue       string                    `json:"dialogue"`
	Emotion        string                    `json:"emotion"`
	TransitionType models.ShotTransitionType `json:"transition_type"`
	TransitionNote string                    `json:"transition_note"`
	StartState     string                    `json:"start_state"`
	EndState       string                    `json:"end_state"`
	PromptSubject  string                    `json:"prompt_subject"`
	PromptAction   string                    `json:"prompt_action"`
	PromptCamera   string                    `json:"prompt_camera"`
	PromptLighting string                    `json:"prompt_lighting"`
	PromptStyle    string                    `json:"prompt_style"`
	NegativePrompt string                    `json:"negative_prompt"`
	Checks         []string                  `json:"checks"`
}

func trimJSONFence(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "```json")
	value = strings.TrimPrefix(value, "```")
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(value), "```"))
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
		if d.Duration <= 0 || d.Duration > 120 || math.IsNaN(d.Duration) || math.IsInf(d.Duration, 0) {
			return nil, fmt.Errorf("镜头%d时长无效", i+1)
		}
		shot := models.Shot{ActType: d.ActType, ShotType: d.ShotType, CameraAngle: d.CameraAngle, CameraMovement: d.CameraMovement, TransitionType: d.TransitionType, TransitionNote: d.TransitionNote, StartState: d.StartState, EndState: d.EndState, Duration: d.Duration, Description: d.Description, Dialogue: d.Dialogue, Emotion: d.Emotion, PromptSubject: d.PromptSubject, PromptAction: d.PromptAction, PromptCamera: d.PromptCamera, PromptLighting: d.PromptLighting, PromptStyle: d.PromptStyle, NegativePrompt: d.NegativePrompt}
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
3. 普通dialogue所在镜头必须让真实说话人清晰可见并由其同步口型；听者可以同时出镜但必须闭口。纯听者单人镜头只能放在该段对白结束后。只有明确speech_type为narration或monologue时才允许画面外发声。
4. 每镜3–15秒，一个主要情绪、一个主要动作、一种主要构图和明确结束状态。
5. 对白自然时长决定总时长，不得压缩语速，也不得用重复动作填时长。
结构化对白：
` + strings.Join(lines, "\n")
}

func canonicalDialogueText(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), "")
}

func normalizeDialogueRhythmDraftDurations(draft *sceneDirectorDraft) {
	if draft == nil {
		return
	}
	for i := range draft.Shots {
		if draft.Shots[i].Duration > 0 && draft.Shots[i].Duration < 3 {
			draft.Shots[i].Duration = 3
			draft.Shots[i].Checks = append(draft.Shots[i].Checks, "系统已将不足3秒的镜头调整为Native H3最短3秒")
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
		visual := strings.Join([]string{shot.ShotType, shot.Description, shot.PromptSubject, shot.PromptAction, shot.StartState, shot.EndState}, " ")
		for _, span := range spans {
			if end <= span.start || start >= span.end {
				continue
			}
			speechType := strings.TrimSpace(span.dialogue.SpeechType)
			if speechType != "" && speechType != "dialogue" {
				continue
			}
			speaker := strings.TrimSpace(span.dialogue.Character)
			if speaker != "" && !strings.Contains(visual, speaker) {
				return fmt.Errorf("对白拆镜%d承载角色“%s”的普通对白时，必须让真实说话人清晰可见；纯听者单人镜头只能放在该段对白结束后", i+1, speaker)
			}
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
	if req.Mode == "dialogue_rhythm" {
		if err := s.DB.Where("scene_id = ? AND project_id = ?", scene.ID, scene.ProjectID).Order("`order` ASC, `id` ASC").Find(&dialogues).Error; err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		dialogues = validSceneDialogues(dialogues)
		if len(dialogues) == 0 {
			c.JSON(400, gin.H{"error": "当前Scene没有可用于拆镜的结构化对白"})
			return
		}
		dialogueDuration = modelDialoguesMinDuration(dialogues)
		instruction = dialogueRhythmDirectorInstruction(dialogues)
		req.Requirements = strings.TrimSpace(req.Requirements + "\n" + instruction)
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
		}
		return draft, parseErr
	}
	draft, err := validateDraft(output)
	if err != nil && req.Mode == "dialogue_rhythm" {
		repairRequirements := fmt.Sprintf("%s\n上一次草稿校验失败：%s。只修复该错误及其他空必填字段；保持镜头顺序和结构化对白逐字不变。上一次JSON：\n%s", req.Requirements, err.Error(), output)
		repaired, repairErr := s.Skills.ChatWithConfiguredOrFallbackSkill(scene.ProjectID, models.SkillStageStoryboard, operation, s.TextProviderFact, "只输出修复后的完整合法JSON，不要Markdown或解释。", "修复对白节奏导演草稿；不得改写、遗漏、重复或新增任何对白。", map[string]string{"scene_facts": scene.Content, "asset_context": assets, "brief": req.Brief, "requirements": repairRequirements, "target_duration": fmt.Sprintf("%.1f", math.Max(scene.Duration, dialogueDuration))})
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
	c.JSON(200, gin.H{"draft": draft, "skill_code": "director-scene-draft", "provider_id": s.TextProviderFact.Name(), "audited": true, "mode": req.Mode, "dialogue_duration": dialogueDuration})
}
