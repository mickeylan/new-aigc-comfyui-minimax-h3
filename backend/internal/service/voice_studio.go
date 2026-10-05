package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"comfyui-console/internal/models"

	"gorm.io/gorm"
)

var (
	ErrVoiceStudioEpisodeNotFound = errors.New("episode not found")
	structuredSRTTimeLine         = regexp.MustCompile(`^\s*(\d{1,2}):(\d{2}):(\d{2})[,.](\d{3})\s*-->\s*(\d{1,2}):(\d{2}):(\d{2})[,.](\d{3})(?:\s+.*)?$`)
	structuredSRTMetadataPair     = regexp.MustCompile(`(?i)(role|character|voice|speed|emotion|delivery|language|seed|角色|说话人|音色|语速|感情|情绪|语气|表达|语言|种子)\s*[:=]\s*(?:"([^"]*)"|'([^']*)'|([^\s;,；，\]\}]+))`)
)

// StructuredSRTCue is an import preview. It deliberately has no Dialogue ID: parsing an
// interchange file must not imply that it can replace the canonical Dialogue records.
type StructuredSRTCue struct {
	Index     int     `json:"index"`
	Start     float64 `json:"start"`
	End       float64 `json:"end"`
	Text      string  `json:"text"`
	Role      string  `json:"role,omitempty"`
	Character string  `json:"character,omitempty"`
	Voice     string  `json:"voice,omitempty"`
	Speed     float64 `json:"speed"`
	Emotion   string  `json:"emotion,omitempty"`
	Delivery  string  `json:"delivery,omitempty"`
	Language  string  `json:"language,omitempty"`
	Seed      *int64  `json:"seed,omitempty"`
}

type structuredSRTMetadata struct {
	Role      string          `json:"role"`
	Character string          `json:"character"`
	Voice     string          `json:"voice"`
	Speed     json.RawMessage `json:"speed"`
	Emotion   string          `json:"emotion"`
	Delivery  string          `json:"delivery"`
	Language  string          `json:"language"`
	Seed      json.RawMessage `json:"seed"`
}

// ParseStructuredSRT parses normal SRT timing plus an optional first cue line containing
// voice metadata. The metadata line may be a JSON object or a bracketed/key-value line,
// for example: [role=dialogue character="林夏" voice=v1 speed=1.1 emotion=calm
// delivery="softly" language=zh-CN seed=42]. It never writes to Dialogue.
func ParseStructuredSRT(input string) ([]StructuredSRTCue, error) {
	input = strings.TrimPrefix(strings.ReplaceAll(strings.ReplaceAll(input, "\r\n", "\n"), "\r", "\n"), "\ufeff")
	blocks := splitSRTBlocks(input)
	if len(blocks) == 0 {
		return nil, fmt.Errorf("SRT is empty")
	}
	cues := make([]StructuredSRTCue, 0, len(blocks))
	for blockNumber, block := range blocks {
		lines := strings.Split(block, "\n")
		if len(lines) < 2 {
			return nil, fmt.Errorf("SRT block %d is incomplete", blockNumber+1)
		}
		index, err := strconv.Atoi(strings.TrimSpace(lines[0]))
		if err != nil || index <= 0 {
			return nil, fmt.Errorf("SRT block %d has an invalid index", blockNumber+1)
		}
		start, end, err := parseStructuredSRTTimeLine(lines[1])
		if err != nil {
			return nil, fmt.Errorf("SRT block %d: %w", blockNumber+1, err)
		}
		if end <= start {
			return nil, fmt.Errorf("SRT block %d end time must be after start time", blockNumber+1)
		}
		cue := StructuredSRTCue{Index: index, Start: start, End: end, Speed: 1}
		textLines := lines[2:]
		if len(textLines) > 0 {
			metadata, recognized, metadataErr := parseStructuredSRTMetadata(strings.TrimSpace(textLines[0]))
			if metadataErr != nil {
				return nil, fmt.Errorf("SRT block %d metadata: %w", blockNumber+1, metadataErr)
			}
			if recognized {
				cue.Role, cue.Character, cue.Voice = metadata.Role, metadata.Character, metadata.Voice
				cue.Speed, cue.Emotion, cue.Delivery = metadata.Speed, metadata.Emotion, metadata.Delivery
				cue.Language, cue.Seed = metadata.Language, metadata.Seed
				textLines = textLines[1:]
			}
		}
		cue.Text = strings.TrimSpace(strings.Join(textLines, "\n"))
		if cue.Text == "" {
			return nil, fmt.Errorf("SRT block %d has no dialogue text", blockNumber+1)
		}
		cues = append(cues, cue)
	}
	return cues, nil
}

func splitSRTBlocks(input string) []string {
	var blocks []string
	for _, block := range regexp.MustCompile(`\n[\t ]*\n+`).Split(strings.TrimSpace(input), -1) {
		if block = strings.TrimSpace(block); block != "" {
			blocks = append(blocks, block)
		}
	}
	return blocks
}

func parseStructuredSRTTimeLine(line string) (float64, float64, error) {
	parts := structuredSRTTimeLine.FindStringSubmatch(line)
	if parts == nil {
		return 0, 0, fmt.Errorf("invalid time range")
	}
	toSeconds := func(offset int) (float64, error) {
		hour, _ := strconv.Atoi(parts[offset])
		minute, _ := strconv.Atoi(parts[offset+1])
		second, _ := strconv.Atoi(parts[offset+2])
		millisecond, _ := strconv.Atoi(parts[offset+3])
		if minute > 59 || second > 59 {
			return 0, fmt.Errorf("invalid time range")
		}
		return float64(hour*3600+minute*60+second) + float64(millisecond)/1000, nil
	}
	start, err := toSeconds(1)
	if err != nil {
		return 0, 0, err
	}
	end, err := toSeconds(5)
	return start, end, err
}

func parseStructuredSRTMetadata(line string) (StructuredSRTCue, bool, error) {
	metadata := StructuredSRTCue{Speed: 1}
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return metadata, false, nil
	}
	if strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}") {
		var raw structuredSRTMetadata
		if err := json.Unmarshal([]byte(trimmed), &raw); err != nil {
			return metadata, true, fmt.Errorf("invalid JSON: %w", err)
		}
		metadata.Role, metadata.Character, metadata.Voice = raw.Role, raw.Character, raw.Voice
		metadata.Emotion, metadata.Delivery, metadata.Language = raw.Emotion, raw.Delivery, raw.Language
		if len(raw.Speed) > 0 && string(raw.Speed) != "null" {
			value, err := rawJSONFloat(raw.Speed)
			if err != nil {
				return metadata, true, fmt.Errorf("invalid speed")
			}
			metadata.Speed = value
		}
		if len(raw.Seed) > 0 && string(raw.Seed) != "null" {
			value, err := rawJSONInt(raw.Seed)
			if err != nil {
				return metadata, true, fmt.Errorf("invalid seed")
			}
			metadata.Seed = &value
		}
		if err := validateStructuredSRTMetadata(metadata); err != nil {
			return metadata, true, err
		}
		return metadata, true, nil
	}

	candidate := trimmed
	lower := strings.ToLower(candidate)
	if strings.HasPrefix(lower, "x-voice-studio:") {
		candidate = strings.TrimSpace(candidate[len("x-voice-studio:"):])
	} else if strings.HasPrefix(lower, "[voice-studio ") && strings.HasSuffix(candidate, "]") {
		candidate = strings.TrimSpace(candidate[len("[voice-studio ") : len(candidate)-1])
	} else if strings.HasPrefix(candidate, "[") && strings.HasSuffix(candidate, "]") {
		candidate = strings.TrimSpace(candidate[1 : len(candidate)-1])
	} else {
		return metadata, false, nil
	}
	matches := structuredSRTMetadataPair.FindAllStringSubmatch(candidate, -1)
	if len(matches) == 0 {
		return metadata, false, nil
	}
	for _, match := range matches {
		value := match[2]
		if value == "" {
			value = match[3]
		}
		if value == "" {
			value = match[4]
		}
		switch strings.ToLower(match[1]) {
		case "role":
			metadata.Role = value
		case "character", "角色", "说话人":
			metadata.Character = value
		case "voice", "音色":
			metadata.Voice = value
		case "speed", "语速":
			parsed, err := strconv.ParseFloat(value, 64)
			if err != nil {
				return metadata, true, fmt.Errorf("invalid speed")
			}
			metadata.Speed = parsed
		case "emotion", "感情", "情绪":
			metadata.Emotion = value
		case "delivery", "语气", "表达":
			metadata.Delivery = value
		case "language", "语言":
			metadata.Language = value
		case "seed", "种子":
			parsed, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return metadata, true, fmt.Errorf("invalid seed")
			}
			metadata.Seed = &parsed
		}
	}
	if err := validateStructuredSRTMetadata(metadata); err != nil {
		return metadata, true, err
	}
	return metadata, true, nil
}

func rawJSONFloat(raw json.RawMessage) (float64, error) {
	var number json.Number
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&number); err == nil {
		return number.Float64()
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return 0, err
	}
	return strconv.ParseFloat(text, 64)
}

func rawJSONInt(raw json.RawMessage) (int64, error) {
	var number json.Number
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&number); err == nil {
		return number.Int64()
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return 0, err
	}
	return strconv.ParseInt(text, 10, 64)
}

func validateStructuredSRTMetadata(metadata StructuredSRTCue) error {
	if metadata.Speed <= 0 || metadata.Speed > 4 {
		return fmt.Errorf("speed must be greater than 0 and at most 4")
	}
	return nil
}

type VoiceStudioRoleMapping struct {
	Source  string `json:"source"`
	Target  string `json:"target,omitempty"`
	Matched bool   `json:"matched"`
}

type VoiceStudioRoleSummary struct {
	Name            string `json:"name"`
	DialogueCount   int    `json:"dialogue_count"`
	Voice           string `json:"voice"`
	VoiceConfigured bool   `json:"voice_configured"`
}

// VoiceStudioDialogueSlotQA compares the selected take's real playback duration with
// the dialogue's canonical scene/SRT allocation. Non-current, stale, and missing audio
// is never estimated: those states remain explicit for production review.
type VoiceStudioDialogueSlotQA struct {
	DialogueID       uint     `json:"dialogue_id"`
	SceneID          uint     `json:"scene_id"`
	SceneOrder       int      `json:"scene_order"`
	Status           string   `json:"status"` // ok/overflow/stale/missing/duration_missing
	Speed            float64  `json:"speed"`
	AudioDuration    *float64 `json:"audio_duration,omitempty"`
	PlaybackDuration *float64 `json:"playback_duration,omitempty"`
	SlotStart        float64  `json:"slot_start"`
	SlotEnd          float64  `json:"slot_end"`
	SlotDuration     float64  `json:"slot_duration"`
	OverflowSeconds  float64  `json:"overflow_seconds"`
	Message          string   `json:"message"`
}

type VoiceStudioSlotQASummary struct {
	Total                int     `json:"total"`
	Evaluated            int     `json:"evaluated"`
	OKCount              int     `json:"ok_count"`
	OverflowCount        int     `json:"overflow_count"`
	StaleCount           int     `json:"stale_count"`
	MissingCount         int     `json:"missing_count"`
	DurationMissingCount int     `json:"duration_missing_count"`
	IssueCount           int     `json:"issue_count"`
	TotalOverflowSeconds float64 `json:"total_overflow_seconds"`
	MaxOverflowSeconds   float64 `json:"max_overflow_seconds"`
}

type VoiceStudioSRTPreview struct {
	PreviewToken string                   `json:"preview_token"`
	ExpiresAt    time.Time                `json:"expires_at"`
	ProjectID    uint                     `json:"project_id"`
	EpisodeN     int                      `json:"episode_n"`
	Count        int                      `json:"count"`
	Duration     float64                  `json:"duration"`
	Cues         []StructuredSRTCue       `json:"cues"`
	Diagnostics  []string                 `json:"diagnostics"`
	RoleMapping  []VoiceStudioRoleMapping `json:"role_mapping"`
	ReadOnly     bool                     `json:"read_only"`
}

func (s *ProjectService) episodeExistsForVoiceStudio(projectID uint, episodeN int) (bool, error) {
	var count int64
	if err := s.db.Model(&models.Episode{}).Where("project_id = ? AND episode_number = ?", projectID, episodeN).Count(&count).Error; err != nil {
		return false, err
	}
	if count > 0 {
		return true, nil
	}
	if err := s.db.Model(&models.Scene{}).Where("project_id = ? AND episode_n = ?", projectID, episodeN).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func newVoiceStudioToken() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

func voiceStudioDialogueSnapshot(dialogues []models.Dialogue) string {
	hash := sha256.New()
	for _, dialogue := range dialogues {
		fmt.Fprintf(hash, "%d\x00%d\x00%d\x00%s\x00%s\x00%s\x00%d\n", dialogue.ID, dialogue.SceneID, dialogue.Order, canonicalDialogueText(dialogue.Text), strings.TrimSpace(dialogue.Character), strings.TrimSpace(dialogue.SpeechType), dialogue.UpdatedAt.UnixNano())
	}
	return fmt.Sprintf("%x", hash.Sum(nil))
}

func (s *ProjectService) currentEpisodeDialogues(projectID uint, episodeN int) (uint, []models.Dialogue, error) {
	return currentEpisodeDialoguesTx(s.db, projectID, episodeN)
}

// PreviewStructuredSRT validates an interchange file in project/episode scope and is read-only.
func (s *ProjectService) PreviewStructuredSRT(project *models.Project, episodeN int, input string) (*VoiceStudioSRTPreview, error) {
	exists, err := s.episodeExistsForVoiceStudio(project.ID, episodeN)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrVoiceStudioEpisodeNotFound
	}
	cues, err := ParseStructuredSRT(input)
	if err != nil {
		return nil, err
	}
	duration := 0.0
	roleNames := map[string]bool{}
	for _, cue := range cues {
		if cue.End > duration {
			duration = cue.End
		}
		if name := strings.TrimSpace(cue.Character); name != "" {
			roleNames[name] = true
		}
	}
	var characters []models.Character
	if err := s.db.Where("project_id = ?", project.ID).Order("name, id").Find(&characters).Error; err != nil {
		return nil, err
	}
	known := make(map[string]string, len(characters))
	for _, character := range characters {
		known[strings.TrimSpace(character.Name)] = character.Name
	}
	preview := &VoiceStudioSRTPreview{ProjectID: project.ID, EpisodeN: episodeN, Count: len(cues), Duration: duration, Cues: cues, Diagnostics: []string{}, RoleMapping: []VoiceStudioRoleMapping{}, ReadOnly: true}
	names := make([]string, 0, len(roleNames))
	for name := range roleNames {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		target, matched := known[name]
		if name == "旁白" {
			target, matched = "旁白", true
		}
		preview.RoleMapping = append(preview.RoleMapping, VoiceStudioRoleMapping{Source: name, Target: target, Matched: matched})
		if !matched {
			preview.Diagnostics = append(preview.Diagnostics, fmt.Sprintf("角色「%s」尚未映射到项目角色", name))
		}
	}
	generation, dialogues, err := s.currentEpisodeDialogues(project.ID, episodeN)
	if err != nil {
		return nil, err
	}
	if len(dialogues) != len(cues) {
		preview.Diagnostics = append(preview.Diagnostics, fmt.Sprintf("SRT 条目数 %d 与当前结构化 Dialogue 数 %d 不一致，不能直接应用", len(cues), len(dialogues)))
	} else {
		for i := range cues {
			if canonicalDialogueText(cues[i].Text) != canonicalDialogueText(dialogues[i].Text) {
				preview.Diagnostics = append(preview.Diagnostics, fmt.Sprintf("第 %d 条字幕文本与当前 Dialogue 不一致", cues[i].Index))
			}
			if name := strings.TrimSpace(cues[i].Character); name != "" && name != strings.TrimSpace(dialogues[i].Character) && !(name == "旁白" && dialogues[i].SpeechType == "narration") {
				preview.Diagnostics = append(preview.Diagnostics, fmt.Sprintf("第 %d 条字幕角色与当前 Dialogue 说话人不一致", cues[i].Index))
			}
		}
	}
	token, err := newVoiceStudioToken()
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(cues)
	if err != nil {
		return nil, err
	}
	expiresAt := time.Now().Add(30 * time.Minute)
	sourceHash := fmt.Sprintf("%x", sha256.Sum256([]byte(input)))
	row := models.VoiceStudioSRTImport{Token: token, ProjectID: project.ID, EpisodeN: episodeN, Generation: generation, DialogueHash: voiceStudioDialogueSnapshot(dialogues), SourceHash: sourceHash, PayloadJSON: string(payload), Status: "preview", ExpiresAt: expiresAt}
	if err := s.db.Create(&row).Error; err != nil {
		return nil, err
	}
	preview.PreviewToken, preview.ExpiresAt = token, expiresAt
	return preview, nil
}

type VoiceStudioSRTApplyResult struct {
	Applied int `json:"applied"`
}

// ApplyStructuredSRT applies only voice-performance metadata to the current authoritative
// Dialogue rows. It never overwrites Dialogue.Text, Character, SpeechType, Scene or ordering.
func (s *ProjectService) ApplyStructuredSRT(project *models.Project, episodeN int, token string) (*VoiceStudioSRTApplyResult, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, fmt.Errorf("缺少 SRT 预览令牌")
	}
	result := &VoiceStudioSRTApplyResult{}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var imported models.VoiceStudioSRTImport
		if err := tx.Where("token = ? AND project_id = ? AND episode_n = ?", token, project.ID, episodeN).First(&imported).Error; err != nil {
			return fmt.Errorf("SRT 预览不存在或不属于当前项目")
		}
		if imported.Status != "preview" || time.Now().After(imported.ExpiresAt) {
			return fmt.Errorf("SRT 预览已应用或已过期，请重新预览")
		}
		generation, dialogues, err := currentEpisodeDialoguesTx(tx, project.ID, episodeN)
		if err != nil {
			return err
		}
		if generation != imported.Generation || voiceStudioDialogueSnapshot(dialogues) != imported.DialogueHash {
			return fmt.Errorf("预览后本集 Dialogue 已变化，请重新预览")
		}
		var cues []StructuredSRTCue
		if err := json.Unmarshal([]byte(imported.PayloadJSON), &cues); err != nil {
			return fmt.Errorf("SRT 预览数据损坏")
		}
		if len(cues) != len(dialogues) {
			return fmt.Errorf("SRT 条目数与当前 Dialogue 数不一致")
		}
		for i := range dialogues {
			cue, dialogue := cues[i], dialogues[i]
			if canonicalDialogueText(cue.Text) != canonicalDialogueText(dialogue.Text) {
				return fmt.Errorf("第 %d 条字幕文本与当前 Dialogue 不一致", cue.Index)
			}
			if name := strings.TrimSpace(cue.Character); name != "" && name != strings.TrimSpace(dialogue.Character) && !(name == "旁白" && dialogue.SpeechType == "narration") {
				return fmt.Errorf("第 %d 条字幕角色与当前 Dialogue 说话人不一致", cue.Index)
			}
			if cue.Speed < 0.5 || cue.Speed > 2 {
				return fmt.Errorf("第 %d 条字幕语速需在 0.5~2 之间", cue.Index)
			}
			updates := map[string]any{"speed": cue.Speed, "emotion": strings.TrimSpace(cue.Emotion), "delivery": strings.TrimSpace(cue.Delivery)}
			if voice := strings.TrimSpace(cue.Voice); voice != "" {
				updates["voice"] = voice
			}
			changed := cue.Speed != dialogueSpeed(dialogue) || strings.TrimSpace(cue.Emotion) != dialogue.Emotion || strings.TrimSpace(cue.Delivery) != dialogue.Delivery || (strings.TrimSpace(cue.Voice) != "" && strings.TrimSpace(cue.Voice) != dialogue.Voice)
			if changed {
				updates["status"], updates["audio_stale"], updates["audio_stale_reason"], updates["audio_token"], updates["error"] = "pending", true, "SRT 配音参数已应用", "", ""
				if err := markDialogueAudioCandidatesStale(tx, project.ID, dialogue.ID, "SRT 配音参数已应用"); err != nil {
					return err
				}
			}
			if err := tx.Model(&models.Dialogue{}).Where("id = ? AND project_id = ?", dialogue.ID, project.ID).Updates(updates).Error; err != nil {
				return err
			}
			result.Applied++
		}
		now := time.Now()
		update := tx.Model(&imported).Where("status = ?", "preview").Updates(map[string]any{"status": "applied", "applied_at": &now})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return fmt.Errorf("SRT 预览已被应用")
		}
		return nil
	})
	return result, err
}

func currentEpisodeDialoguesTx(tx *gorm.DB, projectID uint, episodeN int) (uint, []models.Dialogue, error) {
	generation, err := latestEpisodeGeneration(tx, projectID, episodeN)
	if err != nil {
		return 0, nil, err
	}
	var sceneIDs []uint
	if err := tx.Model(&models.Scene{}).Where("project_id = ? AND episode_n = ? AND generation = ?", projectID, episodeN, generation).Order("`order`, id").Pluck("id", &sceneIDs).Error; err != nil {
		return 0, nil, err
	}
	dialogues := make([]models.Dialogue, 0)
	for _, sceneID := range sceneIDs {
		var sceneDialogues []models.Dialogue
		if err := tx.Where("project_id = ? AND scene_id = ?", projectID, sceneID).Order("`order`, id").Find(&sceneDialogues).Error; err != nil {
			return 0, nil, err
		}
		dialogues = append(dialogues, sceneDialogues...)
	}
	return generation, dialogues, nil
}

func roundVoiceStudioSeconds(value float64) float64 {
	return math.Round(value*1000) / 1000
}

func (s *ProjectService) voiceStudioSlotQA(projectID uint, episodeN int) ([]VoiceStudioDialogueSlotQA, VoiceStudioSlotQASummary, error) {
	generation, err := latestEpisodeGeneration(s.db, projectID, episodeN)
	if err != nil {
		return nil, VoiceStudioSlotQASummary{}, err
	}
	var scenes []models.Scene
	if err := s.db.Where("project_id = ? AND episode_n = ? AND generation = ?", projectID, episodeN, generation).Order("`order`, id").Find(&scenes).Error; err != nil {
		return nil, VoiceStudioSlotQASummary{}, err
	}
	sceneIDs := make([]uint, 0, len(scenes))
	for _, scene := range scenes {
		sceneIDs = append(sceneIDs, scene.ID)
	}
	var dialogues []models.Dialogue
	if len(sceneIDs) > 0 {
		if err := s.db.Where("project_id = ? AND scene_id IN ?", projectID, sceneIDs).Order("scene_id, `order`, id").Find(&dialogues).Error; err != nil {
			return nil, VoiceStudioSlotQASummary{}, err
		}
	}
	byScene := make(map[uint][]models.Dialogue, len(scenes))
	dialogueIDs := make([]uint, 0, len(dialogues))
	for _, dialogue := range dialogues {
		byScene[dialogue.SceneID] = append(byScene[dialogue.SceneID], dialogue)
		dialogueIDs = append(dialogueIDs, dialogue.ID)
	}
	var candidates []models.DialogueAudioCandidate
	if len(dialogueIDs) > 0 {
		if err := s.db.Where("project_id = ? AND dialogue_id IN ?", projectID, dialogueIDs).Order("is_current DESC, id DESC").Find(&candidates).Error; err != nil {
			return nil, VoiceStudioSlotQASummary{}, err
		}
	}
	selected := make(map[uint]models.DialogueAudioCandidate, len(dialogues))
	for _, candidate := range candidates {
		if _, exists := selected[candidate.DialogueID]; exists || !candidate.IsCurrent {
			continue
		}
		selected[candidate.DialogueID] = candidate
	}

	rows := make([]VoiceStudioDialogueSlotQA, 0, len(dialogues))
	summary := VoiceStudioSlotQASummary{Total: len(dialogues)}
	sceneStart := 0.0
	for _, scene := range scenes {
		list := byScene[scene.ID]
		sceneDuration := normalizeSceneDuration(scene.Duration)
		if len(list) == 0 {
			sceneStart += sceneDuration
			continue
		}
		slotSize := sceneDuration / float64(len(list))
		cursor := sceneStart
		for _, dialogue := range list {
			if dialogue.Position > 0 {
				cursor = sceneStart + dialogue.Position
			}
			start := math.Max(sceneStart, cursor+dialogue.Offset)
			end := math.Min(sceneStart+sceneDuration, start+slotSize)
			if end < start {
				end = start
			}
			row := VoiceStudioDialogueSlotQA{
				DialogueID: dialogue.ID, SceneID: dialogue.SceneID, SceneOrder: scene.Order,
				Status: "missing", Speed: dialogueSpeed(dialogue), SlotStart: roundVoiceStudioSeconds(start),
				SlotEnd: roundVoiceStudioSeconds(end), SlotDuration: roundVoiceStudioSeconds(end - start),
				Message: "未选择可用于成片的音频",
			}
			candidate, hasCandidate := selected[dialogue.ID]
			switch {
			case dialogue.AudioStale || (hasCandidate && candidate.Stale):
				row.Status, row.Message = "stale", "所选音频已过期，需重新合成或选择有效候选"
				summary.StaleCount++
			case strings.TrimSpace(dialogue.AudioFile) == "" || dialogue.Status != "ready":
				row.Status, row.Message = "missing", "未选择已就绪的音频"
				summary.MissingCount++
			case !hasCandidate || strings.TrimSpace(candidate.File) != strings.TrimSpace(dialogue.AudioFile) || candidate.Duration <= 0:
				row.Status, row.Message = "duration_missing", "所选音频缺少可核验的实际时长"
				summary.DurationMissingCount++
			default:
				audioDuration := roundVoiceStudioSeconds(candidate.Duration)
				playbackDuration := roundVoiceStudioSeconds(candidate.Duration / row.Speed)
				row.AudioDuration, row.PlaybackDuration = &audioDuration, &playbackDuration
				overflow := math.Max(0, playbackDuration-row.SlotDuration)
				row.OverflowSeconds = roundVoiceStudioSeconds(overflow)
				summary.Evaluated++
				if row.OverflowSeconds > 0 {
					row.Status = "overflow"
					row.Message = fmt.Sprintf("实际播放时长超出字幕/场景槽位 %.3f 秒", row.OverflowSeconds)
					summary.OverflowCount++
					summary.TotalOverflowSeconds += row.OverflowSeconds
					summary.MaxOverflowSeconds = math.Max(summary.MaxOverflowSeconds, row.OverflowSeconds)
				} else {
					row.Status, row.Message = "ok", "实际播放时长位于字幕/场景槽位内"
					summary.OKCount++
				}
			}
			rows = append(rows, row)
			cursor = start + slotSize
		}
		sceneStart += sceneDuration
	}
	summary.TotalOverflowSeconds = roundVoiceStudioSeconds(summary.TotalOverflowSeconds)
	summary.MaxOverflowSeconds = roundVoiceStudioSeconds(summary.MaxOverflowSeconds)
	summary.IssueCount = summary.OverflowCount + summary.StaleCount + summary.MissingCount + summary.DurationMissingCount
	return rows, summary, nil
}

// VoiceStudioData aggregates the existing canonical episode dialogue with its QA timeline and
// character voice assignments. It does not import or synthesize anything.
func (s *ProjectService) VoiceStudioData(project *models.Project, episodeN int) (map[string]any, error) {
	exists, err := s.episodeExistsForVoiceStudio(project.ID, episodeN)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrVoiceStudioEpisodeNotFound
	}
	data, err := s.EditorData(project, episodeN)
	if err != nil {
		return nil, err
	}
	data["episode_n"] = episodeN
	data["timeline"] = data["subtitles"]
	var characters []models.Character
	if err := s.db.Where("project_id = ?", project.ID).Order("name, id").Find(&characters).Error; err != nil {
		return nil, err
	}
	data["project"] = project
	data["characters"] = characters
	characterByName := make(map[string]models.Character, len(characters))
	for _, character := range characters {
		characterByName[strings.TrimSpace(character.Name)] = character
	}
	counts := map[string]int{}
	for _, dialogue := range data["dialogues"].([]models.Dialogue) {
		name := strings.TrimSpace(dialogue.Character)
		if dialogue.SpeechType == "narration" {
			name = "旁白"
		}
		if name == "" {
			name = "未指定说话人"
		}
		counts[name]++
	}
	roleNames := make([]string, 0, len(counts))
	for name := range counts {
		roleNames = append(roleNames, name)
	}
	sort.Strings(roleNames)
	roles := make([]VoiceStudioRoleSummary, 0, len(roleNames))
	for _, name := range roleNames {
		character := characterByName[name]
		voice := strings.TrimSpace(character.VoiceRef)
		if voice == "" {
			voice = strings.TrimSpace(character.Voice)
		}
		if name == "旁白" && voice == "" && s.indexTTS != nil {
			voice = strings.TrimSpace(s.indexTTS.cfg.DefaultVoice)
		}
		roles = append(roles, VoiceStudioRoleSummary{Name: name, DialogueCount: counts[name], Voice: voice, VoiceConfigured: voice != ""})
	}
	data["roles"] = roles
	slotQA, slotQASummary, err := s.voiceStudioSlotQA(project.ID, episodeN)
	if err != nil {
		return nil, err
	}
	data["dialogue_slot_qa"] = slotQA
	data["slot_qa_summary"] = slotQASummary
	data["tts_provider"] = "aliyun"
	data["tts_available"] = true
	data["tts_error"] = ""
	if s.indexTTS != nil {
		configured, available, runtimeError := s.indexTTS.Status()
		if configured {
			data["tts_provider"] = "index_tts_rust"
			data["tts_available"] = available
			data["tts_error"] = runtimeError
			capabilities, info, health := s.indexTTS.RuntimeDetails()
			data["tts_emotion_supported"] = capabilities.SupportsEmotionText
			data["tts_voice_cache_supported"] = capabilities.SupportsVoiceCache
			data["tts_request_cancel_supported"] = capabilities.SupportsRequestCancel
			data["tts_runtime_version"] = info.RuntimeVersion
			data["tts_model_version"] = info.ModelVersion
			data["tts_device_healthy"] = health.DeviceHealthy
		}
	}
	data["dialogue_authority"] = "dialogues"
	data["asr_available"] = false
	data["asr_status"] = "未配置生产级本地 ASR；候选审核需人工试听，系统不会伪造准确率结果"
	return data, nil
}
