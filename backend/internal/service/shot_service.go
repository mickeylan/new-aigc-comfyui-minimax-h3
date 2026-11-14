package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

// ShotService manages the explicit director-shot layer below a Scene.
type ShotService struct{ db *gorm.DB }

func NewShotService(db *gorm.DB) *ShotService { return &ShotService{db: db} }

const shotDurationTolerance = 0.5

type shotDirectorHashV1 struct {
	Order                                                                                  int                `json:"order"`
	ActType                                                                                models.ShotActType `json:"act_type"`
	ShotType, CameraAngle, CameraMovement                                                  string
	TransitionType                                                                         models.ShotTransitionType `json:"transition_type"`
	TransitionNote, StartState, EndState                                                   string
	Duration                                                                               float64 `json:"duration"`
	Description, Dialogue, Emotion                                                         string
	DirectorIntent, AudienceInformation, PerformanceTrigger, Performance                   string                        `json:",omitempty"`
	EyeLine, Blocking, Reaction, FocusSubject, CutReason                                   string                        `json:",omitempty"`
	SpeechPresentation                                                                     models.ShotSpeechPresentation `json:"speech_presentation,omitempty"`
	ListenerCharacter                                                                      string                        `json:"listener_character,omitempty"`
	PromptSubject, PromptAction, PromptCamera, PromptLighting, PromptStyle, NegativePrompt string
	ActionTimeline                                                                         []models.ShotActionTimelineEntry `json:"action_timeline"`
}

func shotDirectorSHA256(shot models.Shot) string {
	timeline := shot.ActionTimeline
	if timeline == nil {
		timeline = []models.ShotActionTimelineEntry{}
	}
	payload, _ := json.Marshal(shotDirectorHashV1{Order: shot.Order, ActType: shot.ActType, ShotType: shot.ShotType, CameraAngle: shot.CameraAngle, CameraMovement: shot.CameraMovement, TransitionType: shot.TransitionType, TransitionNote: shot.TransitionNote, StartState: shot.StartState, EndState: shot.EndState, Duration: shot.Duration, Description: shot.Description, DirectorIntent: shot.DirectorIntent, AudienceInformation: shot.AudienceInformation, PerformanceTrigger: shot.PerformanceTrigger, Performance: shot.Performance, EyeLine: shot.EyeLine, Blocking: shot.Blocking, Reaction: shot.Reaction, FocusSubject: shot.FocusSubject, CutReason: shot.CutReason, Dialogue: shot.Dialogue, Emotion: shot.Emotion, SpeechPresentation: shot.SpeechPresentation, ListenerCharacter: shot.ListenerCharacter, PromptSubject: shot.PromptSubject, PromptAction: shot.PromptAction, PromptCamera: shot.PromptCamera, PromptLighting: shot.PromptLighting, PromptStyle: shot.PromptStyle, NegativePrompt: shot.NegativePrompt, ActionTimeline: timeline})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

var (
	ErrDialogueSnapshotConflict = errors.New("结构化Dialogue已在导演草稿生成后发生变化，请重新生成对白节奏拆镜草稿")
	ErrDialogueTimingBlocked    = errors.New("对白节奏导演草稿未通过自然语速审计")
)

func dialogueSnapshotToken(dialogues []models.Dialogue) string {
	hash := sha256.New()
	for _, dialogue := range dialogues {
		fmt.Fprintf(hash, "%d\x00%d\x00%d\x00%s\x00%s\x00%s\n", dialogue.ID, dialogue.SceneID, dialogue.Order, strings.TrimSpace(dialogue.Character), strings.TrimSpace(dialogue.SpeechType), canonicalDialogueText(dialogue.Text))
	}
	return fmt.Sprintf("%x", hash.Sum(nil))
}

func loadSceneDialogueSnapshot(tx *gorm.DB, sceneID uint) ([]models.Dialogue, string, error) {
	var dialogues []models.Dialogue
	if err := tx.Where("scene_id = ?", sceneID).Order("`order`, id").Find(&dialogues).Error; err != nil {
		return nil, "", err
	}
	dialogues = authoritativeSceneDialogues(dialogues)
	return dialogues, dialogueSnapshotToken(dialogues), nil
}

// syncExplicitSceneSpeech persists only speech that is explicitly present in Scene.Content.
// Existing rows remain authoritative and untouched, including their IDs, audio and review state.
func syncExplicitSceneSpeech(tx *gorm.DB, scene *models.Scene) error {
	if scene == nil || scene.ID == 0 || scene.ProjectID == 0 {
		return fmt.Errorf("场景信息无效，无法同步正文台词")
	}
	var stored []models.Dialogue
	if err := tx.Where("scene_id = ? AND project_id = ?", scene.ID, scene.ProjectID).Order("`order`, id").Find(&stored).Error; err != nil {
		return err
	}
	merged := mergeExplicitSceneSpeech(scene, stored)
	if len(merged) == len(stored) {
		return nil
	}
	maxOrder := 0
	for _, dialogue := range stored {
		if dialogue.Order > maxOrder {
			maxOrder = dialogue.Order
		}
	}
	for _, dialogue := range merged[len(stored):] {
		maxOrder++
		dialogue.ID = 0
		dialogue.SceneID = scene.ID
		dialogue.ProjectID = scene.ProjectID
		dialogue.Order = maxOrder
		if err := tx.Create(&dialogue).Error; err != nil {
			return err
		}
	}
	return nil
}

func restoreAuthoritativeShotDialogues(shots []models.Shot, dialogues []models.Dialogue) error {
	if len(dialogues) == 0 || shotDialogueTextMatches(shots, dialogues) {
		return nil
	}
	draft := &sceneDirectorDraft{Shots: make([]sceneDirectorDraftShot, len(shots))}
	for i, shot := range shots {
		draft.Shots[i] = sceneDirectorDraftShot{Dialogue: shot.Dialogue, Duration: shot.Duration}
	}
	if err := restoreDialogueRhythmDraftText(draft, dialogues); err != nil {
		return err
	}
	for i := range shots {
		shots[i].Dialogue = draft.Shots[i].Dialogue
	}
	return nil
}

func shotDurationTotal(shots []models.Shot) float64 {
	total := 0.0
	for _, shot := range shots {
		total += shot.Duration
	}
	return math.Round(total*10) / 10
}

func rebalanceShotDurations(shots []models.Shot, target float64) ([]models.Shot, error) {
	if len(shots) == 0 || target < minSceneVideoDuration || target > maxLongSceneVideoDuration {
		return nil, fmt.Errorf("Scene时长必须为%.0f至%.0f秒且至少包含一个Shot", minSceneVideoDuration, maxLongSceneVideoDuration)
	}
	total := shotDurationTotal(shots)
	if total <= 0 {
		return nil, fmt.Errorf("Shot总时长必须大于0")
	}
	result := append([]models.Shot(nil), shots...)
	used := 0.0
	for i := range result {
		if i == len(result)-1 {
			result[i].Duration = math.Round((target-used)*10) / 10
		} else {
			result[i].Duration = math.Round((result[i].Duration/total*target)*10) / 10
			used += result[i].Duration
		}
		// Internal cut points may be short, matching validateShot.
		if result[i].Duration <= 0 || result[i].Duration > maxShotDuration {
			return nil, fmt.Errorf("按比例调整后Shot时长必须大于0且不超过%.0f秒，请调整Shot数量或Scene时长", maxShotDuration)
		}
		if shots[i].Duration > 0 && len(result[i].ActionTimeline) > 0 {
			ratio := result[i].Duration / shots[i].Duration
			for j := range result[i].ActionTimeline {
				result[i].ActionTimeline[j].Start = math.Round(result[i].ActionTimeline[j].Start*ratio*1000) / 1000
				result[i].ActionTimeline[j].End = math.Round(result[i].ActionTimeline[j].End*ratio*1000) / 1000
			}
			result[i].ActionTimeline[len(result[i].ActionTimeline)-1].End = result[i].Duration
		}
	}
	return result, nil
}

var shotUpdateFields = map[string]bool{
	"order": true, "act_type": true, "shot_type": true, "camera_angle": true, "camera_movement": true,
	"transition_type": true, "transition_note": true, "start_state": true, "end_state": true,
	"duration": true, "description": true, "director_intent": true, "audience_information": true,
	"trigger": true, "performance": true, "eye_line": true, "blocking": true, "reaction": true,
	"focus_subject": true, "cut_reason": true, "dialogue": true, "speech_presentation": true, "listener_character": true, "emotion": true,
	"prompt_subject": true, "prompt_action": true, "prompt_camera": true,
	"prompt_lighting": true, "prompt_style": true, "negative_prompt": true, "action_timeline": true,
}

func validateShot(shot *models.Shot) error {
	shot.ShotType = strings.TrimSpace(shot.ShotType)
	if shot.ActType == "" {
		shot.ActType = models.ShotActSetup
	}
	validAct := map[models.ShotActType]bool{models.ShotActSetup: true, models.ShotActRising: true, models.ShotActMidpoint: true, models.ShotActFalling: true, models.ShotActResolution: true}
	if !validAct[shot.ActType] {
		return fmt.Errorf("act_type 必须是 setup/rising/midpoint/falling/resolution")
	}
	shot.Description = strings.TrimSpace(shot.Description)
	shot.DirectorIntent = strings.TrimSpace(shot.DirectorIntent)
	shot.AudienceInformation = strings.TrimSpace(shot.AudienceInformation)
	shot.PerformanceTrigger = strings.TrimSpace(shot.PerformanceTrigger)
	shot.Performance = strings.TrimSpace(shot.Performance)
	shot.EyeLine = strings.TrimSpace(shot.EyeLine)
	shot.Blocking = strings.TrimSpace(shot.Blocking)
	shot.Reaction = strings.TrimSpace(shot.Reaction)
	shot.FocusSubject = strings.TrimSpace(shot.FocusSubject)
	shot.CutReason = strings.TrimSpace(shot.CutReason)
	shot.SpeechPresentation = models.ShotSpeechPresentation(strings.TrimSpace(string(shot.SpeechPresentation)))
	shot.ListenerCharacter = strings.TrimSpace(shot.ListenerCharacter)
	if !validShotSpeechPresentation(shot.SpeechPresentation) {
		return fmt.Errorf("speech_presentation 必须为空或使用受支持的权威发声呈现类型")
	}
	shot.TransitionType = models.ShotTransitionType(strings.TrimSpace(string(shot.TransitionType)))
	shot.TransitionNote = strings.TrimSpace(shot.TransitionNote)
	shot.StartState = strings.TrimSpace(shot.StartState)
	shot.EndState = strings.TrimSpace(shot.EndState)
	validTransition := map[models.ShotTransitionType]bool{
		"": true, models.ShotTransitionCut: true, models.ShotTransitionDissolve: true,
		models.ShotTransitionFade: true, models.ShotTransitionWipe: true, models.ShotTransitionMatchCut: true,
	}
	if !validTransition[shot.TransitionType] {
		return fmt.Errorf("transition_type 必须为空或 cut/dissolve/fade/wipe/match_cut（实际%q）", shot.TransitionType)
	}
	if shot.ShotType == "" {
		return fmt.Errorf("shot_type 不能为空")
	}
	// A persisted Shot is an internal cut point of its parent Scene, not an
	// independently submitted video template. Keep short cuts intact; standalone
	// Scene execution remains responsible for its 3–15 second template contract.
	if shot.Duration <= 0 || shot.Duration > maxShotDuration || math.IsNaN(shot.Duration) || math.IsInf(shot.Duration, 0) {
		return fmt.Errorf("每镜时长必须大于 0 且不超过 %.0f 秒", maxShotDuration)
	}
	if err := validateShotActionTimeline(shot.ActionTimeline, shot.Duration); err != nil {
		return err
	}
	if err := validateNativeShotMode(*shot); err != nil {
		return err
	}
	return nil
}

func validateNativeShotMode(shot models.Shot) error {
	text := strings.ToLower(strings.Join([]string{
		shot.ShotType, shot.CameraMovement, shot.Description, shot.StartState, shot.EndState,
		shot.PromptSubject, shot.PromptAction, shot.PromptCamera, shot.PromptStyle,
	}, " "))
	for _, negation := range []string{"禁止使用", "不得使用", "不要使用", "do not use", "never use"} {
		text = strings.ReplaceAll(text, negation, "forbid ")
	}
	for _, marker := range []string{
		"使用endless", "采用endless", "切换endless", "endless mode", "use endless",
		"使用canvas", "采用canvas", "切换canvas", "canvas workflow", "use canvas",
	} {
		if strings.Contains(text, marker) {
			return fmt.Errorf("导演镜头只能使用%.0f至%.0f秒Native H3流程，禁止使用Endless或Canvas", minSceneVideoDuration, maxSceneVideoDuration)
		}
	}
	return nil
}

func validateShotActionTimeline(entries []models.ShotActionTimelineEntry, duration float64) error {
	if len(entries) == 0 {
		return nil
	}
	if len(entries) > 20 {
		return fmt.Errorf("动作时间轴最多包含20个区间")
	}
	const tolerance = 0.001
	previousEnd := 0.0
	for i := range entries {
		entry := &entries[i]
		entry.Subject, entry.Action = strings.TrimSpace(entry.Subject), strings.TrimSpace(entry.Action)
		entry.State, entry.Camera = strings.TrimSpace(entry.State), strings.TrimSpace(entry.Camera)
		if math.IsNaN(entry.Start) || math.IsInf(entry.Start, 0) || math.IsNaN(entry.End) || math.IsInf(entry.End, 0) {
			return fmt.Errorf("动作时间轴第%d段时间无效", i+1)
		}
		if entry.Start < 0 || entry.End <= entry.Start || entry.End > duration+tolerance {
			return fmt.Errorf("动作时间轴第%d段必须满足 0 <= start < end <= %.1f", i+1, duration)
		}
		if math.Abs(entry.Start-previousEnd) > tolerance {
			return fmt.Errorf("动作时间轴第%d段必须从%.3f秒连续开始（实际%.3f秒）", i+1, previousEnd, entry.Start)
		}
		if entry.Subject == "" || entry.Action == "" || entry.State == "" || entry.Camera == "" {
			return fmt.Errorf("动作时间轴第%d段必须填写主体、动作、状态和摄影机", i+1)
		}
		previousEnd = entry.End
	}
	if math.Abs(previousEnd-duration) > tolerance {
		return fmt.Errorf("动作时间轴必须完整覆盖镜头0至%.1f秒", duration)
	}
	return nil
}

func shotDialogueTextMatches(shots []models.Shot, dialogues []models.Dialogue) bool {
	var shotText, dialogueText strings.Builder
	for _, shot := range shots {
		shotText.WriteString(canonicalDialogueText(shot.Dialogue))
	}
	for _, dialogue := range dialogues {
		dialogueText.WriteString(canonicalDialogueText(dialogue.Text))
	}
	return shotText.String() == dialogueText.String()
}

func clearShotDialogueRanges(tx *gorm.DB, sceneID uint, shots []models.Shot) error {
	for i := range shots {
		shots[i].DialogueRanges = []models.ShotDialogueRange{}
	}
	return tx.Model(&models.Shot{}).Where("scene_id = ?", sceneID).Update("dialogue_ranges_json", "[]").Error
}

func assignShotDialogueRanges(tx *gorm.DB, sceneID uint, shots []models.Shot) error {
	var dialogues []models.Dialogue
	if err := tx.Where("scene_id = ?", sceneID).Order("`order`, id").Find(&dialogues).Error; err != nil {
		return err
	}
	dialogues = authoritativeSceneDialogues(dialogues)
	// Build authoritative rune sequence from structured dialogues.
	canonical := make([][]rune, len(dialogues))
	groupKeys := make([]string, len(dialogues))
	groupStarts := make([]int, len(dialogues))
	// Dialogue ranges are metadata only. Ordinary director edits may intentionally
	// diverge from the structured Dialogue rows; in that case discard stale mappings
	// rather than rejecting the save or inventing ranges.
	if !shotDialogueTextMatches(shots, dialogues) {
		return clearShotDialogueRanges(tx, sceneID, shots)
	}
	for i := range dialogues {
		canonical[i] = []rune(canonicalDialogueText(dialogues[i].Text))
	}
	groupNo, groupOffset := 0, 0
	for i := range dialogues {
		canonical[i] = []rune(canonicalDialogueText(dialogues[i].Text))
		if i == 0 || !dialogueContinuesAcrossCut(dialogues[i-1], dialogues[i]) {
			groupNo++
			groupOffset = 0
		}
		groupKeys[i] = fmt.Sprintf("dialogue-group:%d", groupNo)
		groupStarts[i] = groupOffset
		groupOffset += len(canonical[i])
	}
	dialogueIndex, offset := 0, 0
	for i := range shots {
		remaining := len([]rune(canonicalDialogueText(shots[i].Dialogue)))
		ranges := []models.ShotDialogueRange{}
		for remaining > 0 && dialogueIndex < len(dialogues) {
			available := len(canonical[dialogueIndex]) - offset
			if available <= 0 {
				dialogueIndex++
				offset = 0
				continue
			}
			take := remaining
			if take > available {
				take = available
			}
			ranges = append(ranges, models.ShotDialogueRange{DialogueID: dialogues[dialogueIndex].ID, GroupKey: groupKeys[dialogueIndex], StartRune: groupStarts[dialogueIndex] + offset, EndRune: groupStarts[dialogueIndex] + offset + take})
			offset += take
			remaining -= take
			if offset == len(canonical[dialogueIndex]) {
				dialogueIndex++
				offset = 0
			}
		}
		if remaining != 0 {
			return fmt.Errorf("镜头%d对白无法映射到结构化Dialogue", i+1)
		}
		shots[i].DialogueRanges = ranges
		encoded, err := json.Marshal(ranges)
		if err != nil {
			return err
		}
		if err := tx.Model(&models.Shot{}).Where("id = ? AND scene_id = ?", shots[i].ID, sceneID).Update("dialogue_ranges_json", string(encoded)).Error; err != nil {
			return err
		}
	}
	for dialogueIndex < len(dialogues) && offset == len(canonical[dialogueIndex]) {
		dialogueIndex++
		offset = 0
	}
	if dialogueIndex != len(dialogues) || offset != 0 {
		return fmt.Errorf("导演镜头未完整覆盖场景正文中的原始台词，请重新生成导演方案")
	}
	return nil
}

func validateShotListenerCharacters(db *gorm.DB, projectID uint, shots []models.Shot) error {
	required := map[string]bool{}
	for _, shot := range shots {
		if shot.SpeechPresentation == models.ShotSpeechPresentationListenerCarry {
			if listener := strings.TrimSpace(shot.ListenerCharacter); listener != "" {
				required[listener] = true
			}
		}
	}
	if len(required) == 0 {
		return nil
	}
	names := make([]string, 0, len(required))
	for name := range required {
		names = append(names, name)
	}
	var characters []models.Character
	if err := db.Select("name").Where("project_id = ? AND name IN ?", projectID, names).Find(&characters).Error; err != nil {
		return err
	}
	for _, character := range characters {
		delete(required, strings.TrimSpace(character.Name))
	}
	if len(required) == 0 {
		return nil
	}
	missing := make([]string, 0, len(required))
	for name := range required {
		missing = append(missing, name)
	}
	sort.Strings(missing)
	return fmt.Errorf("%w: 闭口听者不是当前项目权威角色: %s", ErrDialogueTimingBlocked, strings.Join(missing, "、"))
}

func (s *ShotService) CreateShot(sceneID uint, shot models.Shot) (*models.Shot, error) {
	shot.ID, shot.SceneID = 0, sceneID
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		projectID, err := projectIDForScene(tx, sceneID)
		if err != nil {
			return err
		}
		if shot.Order <= 0 {
			var maxOrder int
			if err := tx.Model(&models.Shot{}).Where("scene_id = ?", sceneID).Select("COALESCE(MAX(order_num), 0)").Scan(&maxOrder).Error; err != nil {
				return err
			}
			shot.Order = maxOrder + 1
		}
		if err := validateShot(&shot); err != nil {
			return err
		}
		var proposed []models.Shot
		if err := tx.Where("scene_id = ?", sceneID).Order("order_num, id").Find(&proposed).Error; err != nil {
			return err
		}
		proposed = append(proposed, shot)
		sort.SliceStable(proposed, func(i, j int) bool { return proposed[i].Order < proposed[j].Order })
		var dialogues []models.Dialogue
		if err := tx.Where("scene_id = ?", sceneID).Order("`order`, id").Find(&dialogues).Error; err != nil {
			return err
		}
		// Incremental/manual Shot creation must not require a partial Shot list to
		// transcribe the complete Scene Dialogue. Authoritative text is projected when
		// the reviewed batch is saved or materialized.
		if err := tx.Create(&shot).Error; err != nil {
			return err
		}
		if err := recordManualShotPrompt(tx, projectID, shot); err != nil {
			return err
		}
		var shots []models.Shot
		if err := tx.Where("scene_id = ?", sceneID).Order("order_num, id").Find(&shots).Error; err != nil {
			return err
		}
		if err := assignShotDialogueRanges(tx, sceneID, shots); err != nil {
			return err
		}
		return aggregateShotsIntoScene(tx, sceneID, shots)
	}); err != nil {
		return nil, err
	}
	return &shot, nil
}

// ReplaceShots stores the supplied director shots as-is while preserving IDs for existing
// shots. Stable IDs keep prompt history and shot-level asset associations attached across saves.
func (s *ShotService) ReplaceShots(sceneID uint, shots []models.Shot) ([]models.Shot, error) {
	return s.ReplaceShotsWithDialogueSnapshot(sceneID, shots, "")
}

func (s *ShotService) ReplaceShotsWithDialogueSnapshot(sceneID uint, shots []models.Shot, expectedSnapshot string) ([]models.Shot, error) {
	for i := range shots {
		shots[i].SceneID, shots[i].Order = sceneID, i+1
		if err := validateShot(&shots[i]); err != nil {
			return nil, fmt.Errorf("镜头 %d: %w", i+1, err)
		}
	}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		projectID, err := projectIDForScene(tx, sceneID)
		if err != nil {
			return err
		}
		if strings.TrimSpace(expectedSnapshot) != "" {
			dialogues, currentSnapshot, snapshotErr := loadSceneDialogueSnapshot(tx, sceneID)
			if snapshotErr != nil {
				return snapshotErr
			}
			if currentSnapshot != strings.TrimSpace(expectedSnapshot) {
				return ErrDialogueSnapshotConflict
			}
			// Dialogue is database authority. The model/user does not need to transcribe
			// it perfectly: project the exact source text onto the reviewed Shot boundaries.
			if err := restoreAuthoritativeShotDialogues(shots, dialogues); err != nil {
				return fmt.Errorf("%w: %s", ErrDialogueTimingBlocked, err.Error())
			}
			// Presentation and semantic-cut diagnostics are advisory at this review/save
			// boundary. validateShot already enforces executable duration/schema limits.
		}
		var existing []models.Shot
		if err := tx.Where("scene_id = ?", sceneID).Order("order_num, id").Find(&existing).Error; err != nil {
			return err
		}
		existingByID := make(map[uint]models.Shot, len(existing))
		for _, shot := range existing {
			existingByID[shot.ID] = shot
		}
		keep := make(map[uint]bool, len(shots))
		// Move existing rows out of the positive order range first to avoid the unique
		// (scene_id, order_num) constraint while the requested order is applied.
		if len(existing) > 0 {
			if err := tx.Model(&models.Shot{}).Where("scene_id = ?", sceneID).
				Update("order_num", gorm.Expr("-id")).Error; err != nil {
				return err
			}
		}
		for i := range shots {
			if shots[i].ID == 0 {
				if err := tx.Create(&shots[i]).Error; err != nil {
					return err
				}
				keep[shots[i].ID] = true
				continue
			}
			if _, ok := existingByID[shots[i].ID]; !ok {
				return fmt.Errorf("镜头 %d 不属于当前场景", shots[i].ID)
			}
			keep[shots[i].ID] = true
			timelineJSON, err := json.Marshal(shots[i].ActionTimeline)
			if err != nil {
				return fmt.Errorf("序列化动作时间轴失败: %w", err)
			}
			updates := map[string]any{
				"order_num": shots[i].Order, "act_type": shots[i].ActType, "shot_type": shots[i].ShotType,
				"camera_angle": shots[i].CameraAngle, "camera_movement": shots[i].CameraMovement,
				"transition_type": shots[i].TransitionType, "transition_note": shots[i].TransitionNote, "start_state": shots[i].StartState, "end_state": shots[i].EndState,
				"duration": shots[i].Duration, "description": shots[i].Description,
				"director_intent": shots[i].DirectorIntent, "audience_information": shots[i].AudienceInformation,
				"performance_trigger": shots[i].PerformanceTrigger, "performance": shots[i].Performance,
				"eye_line": shots[i].EyeLine, "blocking": shots[i].Blocking, "reaction": shots[i].Reaction,
				"focus_subject": shots[i].FocusSubject, "cut_reason": shots[i].CutReason, "dialogue": shots[i].Dialogue,
				"speech_presentation": shots[i].SpeechPresentation, "listener_character": shots[i].ListenerCharacter, "emotion": shots[i].Emotion, "prompt_subject": shots[i].PromptSubject,
				"prompt_action": shots[i].PromptAction, "prompt_camera": shots[i].PromptCamera,
				"prompt_lighting": shots[i].PromptLighting, "prompt_style": shots[i].PromptStyle,
				"negative_prompt": shots[i].NegativePrompt, "action_timeline_json": string(timelineJSON),
			}
			// Empty provenance means an ordinary edit of an existing Shot. Preserve its
			// server-authenticated source instead of trusting or clearing client fields.
			if strings.TrimSpace(shots[i].DirectorSource) != "" {
				updates["director_source"] = shots[i].DirectorSource
				updates["director_source_sha256"] = shots[i].DirectorSourceSHA256
				updates["combat_reference_audit_json"] = shots[i].CombatReferenceAudit
			}
			if err := tx.Model(&models.Shot{}).Where("id = ? AND scene_id = ?", shots[i].ID, sceneID).Updates(updates).Error; err != nil {
				return err
			}
		}
		for _, shot := range existing {
			if !keep[shot.ID] {
				if err := deleteShotAssociations(tx, shot.ID); err != nil {
					return err
				}
				if err := tx.Delete(&shot).Error; err != nil {
					return err
				}
			}
		}
		var saved []models.Shot
		if err := tx.Where("scene_id = ?", sceneID).Order("order_num, id").Find(&saved).Error; err != nil {
			return err
		}
		shots = saved
		if err := assignShotDialogueRanges(tx, sceneID, shots); err != nil {
			return err
		}
		if err := tx.Where("scene_id = ?", sceneID).Order("order_num, id").Find(&shots).Error; err != nil {
			return err
		}
		for _, shot := range shots {
			if err := recordManualShotPrompt(tx, projectID, shot); err != nil {
				return err
			}
		}
		if err := aggregateShotsIntoScene(tx, sceneID, shots); err != nil {
			return err
		}
		// A successful full replacement is explicit acceptance of this complete
		// director design. Clear source invalidation in the same transaction, after
		// aggregation, without deriving or rewriting user-authored Scene.Content.
		return tx.Model(&models.Scene{}).Where("id = ?", sceneID).
			Update("director_source_stale", false).Error
	})
	if err == nil {
		decorateShotDialogueContinuity(shots)
	}
	return shots, err
}

type ShotDialogueFragment struct {
	DialogueID   uint   `json:"dialogue_id"`
	GroupKey     string `json:"group_key"`
	StartRune    int    `json:"start_rune"`
	EndRune      int    `json:"end_rune"`
	Text         string `json:"text"`
	Character    string `json:"character"`
	SpeechType   string `json:"speech_type"`
	SpeakerLabel string `json:"speaker_label"`
}

func (s *ShotService) ResolveDialogueFragments(sceneID uint, shots []models.Shot) (map[uint][]ShotDialogueFragment, error) {
	var dialogues []models.Dialogue
	if err := s.db.Where("scene_id=?", sceneID).Order("`order`, id").Find(&dialogues).Error; err != nil {
		return nil, err
	}
	byID := map[uint]models.Dialogue{}
	groupOf := map[uint]string{}
	for _, shot := range shots {
		for _, r := range shot.DialogueRanges {
			if _, ok := groupOf[r.DialogueID]; !ok {
				groupOf[r.DialogueID] = r.GroupKey
			}
		}
	}
	groups := map[string][]rune{}
	for _, d := range dialogues {
		byID[d.ID] = d
		if key := groupOf[d.ID]; key != "" {
			groups[key] = append(groups[key], []rune(canonicalDialogueText(d.Text))...)
		}
	}
	out := map[uint][]ShotDialogueFragment{}
	for _, shot := range shots {
		for _, r := range shot.DialogueRanges {
			text := groups[r.GroupKey]
			if r.StartRune < 0 || r.EndRune < r.StartRune || r.EndRune > len(text) {
				return nil, fmt.Errorf("Shot %d 对白区间越界", shot.Order)
			}
			d, ok := byID[r.DialogueID]
			if !ok {
				return nil, fmt.Errorf("Shot %d 引用了不存在的Dialogue", shot.Order)
			}
			kind, speaker := normalizeScriptSpeech(d.SpeechType, d.Character)
			label := speaker
			if kind == "monologue" && label == "" {
				label = "内心独白"
			}
			out[shot.ID] = append(out[shot.ID], ShotDialogueFragment{DialogueID: r.DialogueID, GroupKey: r.GroupKey, StartRune: r.StartRune, EndRune: r.EndRune, Text: string(text[r.StartRune:r.EndRune]), Character: d.Character, SpeechType: kind, SpeakerLabel: label})
		}
	}
	return out, nil
}

func decorateShotDialogueContinuity(shots []models.Shot) {
	for i := range shots {
		if len(shots[i].DialogueRanges) == 0 {
			continue
		}
		first := shots[i].DialogueRanges[0]
		shots[i].ContinuesFromPrevious = first.StartRune > 0
		if i+1 < len(shots) && len(shots[i+1].DialogueRanges) > 0 {
			next := shots[i+1].DialogueRanges[0]
			last := shots[i].DialogueRanges[len(shots[i].DialogueRanges)-1]
			sameGroup := last.GroupKey != "" && last.GroupKey == next.GroupKey
			if last.GroupKey == "" && next.GroupKey == "" {
				sameGroup = last.DialogueID == next.DialogueID
			}
			shots[i].ContinuesToNext = sameGroup && last.EndRune == next.StartRune
		}
	}
}

func (s *ShotService) GetSceneShots(sceneID uint) ([]models.Shot, error) {
	var shots []models.Shot
	err := s.db.Where("scene_id = ?", sceneID).Order("order_num, id").Find(&shots).Error
	if err == nil {
		decorateShotDialogueContinuity(shots)
	}
	return shots, err
}

func (s *ShotService) UpdateShot(shotID uint, updates map[string]any) (*models.Shot, error) {
	filtered := map[string]any{}
	for key, value := range updates {
		if !shotUpdateFields[key] {
			return nil, fmt.Errorf("不允许修改字段: %s", key)
		}
		column := key
		if key == "order" {
			column = "order_num"
		} else if key == "trigger" {
			column = "performance_trigger"
		}
		filtered[column] = value
	}
	var shot models.Shot
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&shot, shotID).Error; err != nil {
			return err
		}
		// Decode into a copy of the row read inside this transaction, so validation
		// and persistence cannot be based on an earlier stale read.
		candidate := shot
		if v, ok := updates["act_type"].(string); ok {
			candidate.ActType = models.ShotActType(v)
		}
		if v, ok := updates["shot_type"].(string); ok {
			candidate.ShotType = v
		}
		if v, ok := updates["description"].(string); ok {
			candidate.Description = v
		}
		if v, ok := updates["director_intent"].(string); ok {
			candidate.DirectorIntent = v
		}
		if v, ok := updates["audience_information"].(string); ok {
			candidate.AudienceInformation = v
		}
		if v, ok := updates["trigger"].(string); ok {
			candidate.PerformanceTrigger = v
		}
		if v, ok := updates["performance"].(string); ok {
			candidate.Performance = v
		}
		if v, ok := updates["eye_line"].(string); ok {
			candidate.EyeLine = v
		}
		if v, ok := updates["blocking"].(string); ok {
			candidate.Blocking = v
		}
		if v, ok := updates["reaction"].(string); ok {
			candidate.Reaction = v
		}
		if v, ok := updates["focus_subject"].(string); ok {
			candidate.FocusSubject = v
		}
		if v, ok := updates["cut_reason"].(string); ok {
			candidate.CutReason = v
		}
		if v, ok := updates["dialogue"].(string); ok {
			candidate.Dialogue = v
		}
		if v, ok := updates["speech_presentation"].(string); ok {
			candidate.SpeechPresentation = models.ShotSpeechPresentation(v)
		}
		if v, ok := updates["listener_character"].(string); ok {
			candidate.ListenerCharacter = v
		}
		if v, ok := updates["camera_angle"].(string); ok {
			candidate.CameraAngle = v
		}
		if v, ok := updates["camera_movement"].(string); ok {
			candidate.CameraMovement = v
		}
		if v, ok := updates["emotion"].(string); ok {
			candidate.Emotion = v
		}
		if v, ok := updates["prompt_subject"].(string); ok {
			candidate.PromptSubject = v
		}
		if v, ok := updates["prompt_action"].(string); ok {
			candidate.PromptAction = v
		}
		if v, ok := updates["prompt_camera"].(string); ok {
			candidate.PromptCamera = v
		}
		if v, ok := updates["prompt_lighting"].(string); ok {
			candidate.PromptLighting = v
		}
		if v, ok := updates["prompt_style"].(string); ok {
			candidate.PromptStyle = v
		}
		if v, ok := updates["negative_prompt"].(string); ok {
			candidate.NegativePrompt = v
		}
		if v, ok := updates["transition_type"].(string); ok {
			candidate.TransitionType = models.ShotTransitionType(v)
		}
		if v, ok := updates["transition_note"].(string); ok {
			candidate.TransitionNote = v
		}
		if v, ok := updates["start_state"].(string); ok {
			candidate.StartState = v
		}
		if v, ok := updates["end_state"].(string); ok {
			candidate.EndState = v
		}
		if v, ok := updates["order"].(float64); ok && v < 1 {
			return fmt.Errorf("order 必须大于0")
		}
		if v, ok := updates["duration"].(float64); ok {
			candidate.Duration = v
		}
		if value, ok := updates["action_timeline"]; ok {
			data, err := json.Marshal(value)
			if err != nil || json.Unmarshal(data, &candidate.ActionTimeline) != nil {
				return fmt.Errorf("action_timeline 格式无效")
			}
			filtered["action_timeline_json"] = string(data)
			delete(filtered, "action_timeline")
		}
		if err := validateShot(&candidate); err != nil {
			return err
		}
		// Persist the normalized values that were actually validated, not raw client
		// strings. Otherwise whitespace can pass ownership checks after trimming but
		// produce a different listener identity in H3/audio hashes on reload.
		if _, ok := updates["speech_presentation"]; ok {
			filtered["speech_presentation"] = candidate.SpeechPresentation
		}
		if _, ok := updates["listener_character"]; ok {
			filtered["listener_character"] = candidate.ListenerCharacter
		}
		for key, value := range map[string]string{
			"director_intent": candidate.DirectorIntent, "audience_information": candidate.AudienceInformation,
			"trigger": candidate.PerformanceTrigger, "performance": candidate.Performance, "eye_line": candidate.EyeLine,
			"blocking": candidate.Blocking, "reaction": candidate.Reaction, "focus_subject": candidate.FocusSubject, "cut_reason": candidate.CutReason,
		} {
			if _, ok := updates[key]; ok {
				column := key
				if key == "trigger" {
					column = "performance_trigger"
				}
				filtered[column] = value
			}
		}
		projectID, err := projectIDForScene(tx, shot.SceneID)
		if err != nil {
			return err
		}
		var sceneShots []models.Shot
		if err := tx.Where("scene_id = ?", shot.SceneID).Order("order_num, id").Find(&sceneShots).Error; err != nil {
			return err
		}
		for i := range sceneShots {
			if sceneShots[i].ID == shot.ID {
				candidate.Order = sceneShots[i].Order
				sceneShots[i] = candidate
				break
			}
		}
		if err := tx.Model(&shot).Updates(filtered).Error; err != nil {
			return err
		}
		var saved models.Shot
		if err := tx.First(&saved, shot.ID).Error; err != nil {
			return err
		}
		if err := recordManualShotPrompt(tx, projectID, saved); err != nil {
			return err
		}
		var shots []models.Shot
		if err := tx.Where("scene_id = ?", shot.SceneID).Order("order_num, id").Find(&shots).Error; err != nil {
			return err
		}
		if err := assignShotDialogueRanges(tx, shot.SceneID, shots); err != nil {
			return err
		}
		if err := tx.Where("scene_id = ?", shot.SceneID).Order("order_num, id").Find(&shots).Error; err != nil {
			return err
		}
		return aggregateShotsIntoScene(tx, shot.SceneID, shots)
	}); err != nil {
		return nil, err
	}
	if err := s.db.First(&shot, shotID).Error; err != nil {
		return nil, err
	}
	return &shot, nil
}

func (s *ShotService) DeleteShot(shotID uint) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		var shot models.Shot
		if err := tx.First(&shot, shotID).Error; err != nil {
			return err
		}
		var remaining []models.Shot
		if err := tx.Where("scene_id = ? AND id <> ?", shot.SceneID, shot.ID).Order("order_num, id").Find(&remaining).Error; err != nil {
			return err
		}
		if err := deleteShotAssociations(tx, shot.ID); err != nil {
			return err
		}
		if err := tx.Delete(&shot).Error; err != nil {
			return err
		}
		var shots []models.Shot
		if err := tx.Where("scene_id = ?", shot.SceneID).Order("order_num, id").Find(&shots).Error; err != nil {
			return err
		}
		if err := assignShotDialogueRanges(tx, shot.SceneID, shots); err != nil {
			return err
		}
		return aggregateShotsIntoScene(tx, shot.SceneID, shots)
	})
}

func canonicalShotPrompt(shot models.Shot) string {
	parts := []string{shot.PromptSubject, shot.PromptAction, shot.PromptCamera, shot.PromptLighting, shot.PromptStyle}
	for i := range parts {
		parts[i] = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(parts[i], "\r\n", " "), "\n", " "))
	}
	return strings.Join(parts, "\n")
}

func projectIDForScene(db *gorm.DB, sceneID uint) (uint, error) {
	var scene models.Scene
	if err := db.Select("project_id").First(&scene, sceneID).Error; err != nil {
		return 0, err
	}
	return scene.ProjectID, nil
}

func recordManualShotPrompt(db *gorm.DB, projectID uint, shot models.Shot) error {
	content := canonicalShotPrompt(shot)
	var count int64
	if err := db.Model(&models.PromptVersion{}).Where(
		"project_id = ? AND entity_type = ? AND entity_id = ? AND content = ? AND state = ?",
		projectID, "shot", shot.ID, content, PromptVersionStateApplied,
	).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	var draft models.PromptVersion
	err := db.Where("project_id = ? AND entity_type = ? AND entity_id = ? AND content = ? AND state = ?", projectID, "shot", shot.ID, content, PromptVersionStateDraft).
		Order("id DESC").First(&draft).Error
	if err == nil {
		return db.Model(&draft).Update("state", PromptVersionStateApplied).Error
	}
	if err != gorm.ErrRecordNotFound {
		return err
	}
	return db.Create(&models.PromptVersion{
		ProjectID: projectID, EntityType: "shot", EntityID: shot.ID,
		Content: content, Action: string(PromptActionManual), State: PromptVersionStateApplied, Metadata: "{}",
	}).Error
}

func deleteShotAssociations(db *gorm.DB, shotID uint) error {
	if err := db.Where("shot_id = ?", shotID).Delete(&models.ShotCharacterLook{}).Error; err != nil {
		return err
	}
	return db.Where("shot_id = ?", shotID).Delete(&models.ShotCharacterOutfit{}).Error
}

func aggregateShotsIntoScene(db *gorm.DB, sceneID uint, shots []models.Shot) error {
	negativePrompts := make([]string, 0, len(shots)+1)
	seenNegative := map[string]bool{}
	var scene models.Scene
	if err := db.Where("id = ?", sceneID).First(&scene).Error; err != nil {
		return err
	}
	// Scene 级负向约束可能来自人工编辑，Shot 保存不得将其静默清空。
	if negative := strings.TrimSpace(scene.NegativePrompt); negative != "" {
		seenNegative[negative] = true
		negativePrompts = append(negativePrompts, negative)
	}
	for _, shot := range shots {
		if negative := strings.TrimSpace(shot.NegativePrompt); negative != "" && !seenNegative[negative] {
			seenNegative[negative] = true
			negativePrompts = append(negativePrompts, negative)
		}
	}
	// Shot edits change generation inputs, but are not an explicit media replacement.
	// Preserve adopted/completed media, selected candidates, continuity frames, upload
	// selections and locks. Only detach in-flight tasks so stale callbacks cannot win.
	updates := map[string]any{
		"shot_count":      len(shots),
		"negative_prompt": strings.Join(negativePrompts, ", "),
		"prompt_stale":    true,
	}
	staleSceneTaskUpdates(db, &scene, updates)
	if err := db.Model(&scene).Updates(updates).Error; err != nil {
		return err
	}
	if err := markSceneMediaSourceChanged(db, scene.ProjectID, sceneID, "", "导演镜头已修改"); err != nil {
		return err
	}
	if err := markSceneAudioCandidatesStale(db, scene.ProjectID, sceneID, "导演Shot或权威Dialogue区间已修改"); err != nil {
		return err
	}
	return markEpisodeEditorialStale(db, scene.ProjectID, scene.EpisodeN)
}

func nonEmptyStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	return result
}

func updateSceneShotCount(db *gorm.DB, sceneID uint) error {
	var count int64
	if err := db.Model(&models.Shot{}).Where("scene_id = ?", sceneID).Count(&count).Error; err != nil {
		return err
	}
	return db.Model(&models.Scene{}).Where("id = ?", sceneID).Update("shot_count", count).Error
}
