package service

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

var ErrSceneAudioReviewConflict = errors.New("Scene音轨审核内容已变化，请刷新后重新审核")

const sceneAudioReviewConfirmationV1 = "native_shot_text_audio_ownership_v1"

func isReviewedAudioVideoTemplate(code string) bool {
	switch strings.TrimSpace(code) {
	case h3AudioDrivenRef2VTemplate, h3AudioDrivenT8EnhancedTemplate, h3VideoThenAudioMuxTemplate:
		return true
	default:
		return false
	}
}

type sceneReviewedAudio struct {
	Ready            bool                           `json:"ready"`
	RequiresSceneMix bool                           `json:"requires_scene_mix"`
	Reason           string                         `json:"reason,omitempty"`
	Dialogue         *models.Dialogue               `json:"dialogue,omitempty"`
	Candidate        *models.DialogueAudioCandidate `json:"candidate,omitempty"`
	SceneCandidate   *models.SceneAudioCandidate    `json:"scene_candidate,omitempty"`
	SourceHash       string                         `json:"source_hash,omitempty"`
}

type sceneAudioSource struct {
	DialogueID    uint    `json:"dialogue_id"`
	CandidateID   uint    `json:"candidate_id"`
	InputHash     string  `json:"input_hash"`
	File          string  `json:"file"`
	FileSHA256    string  `json:"file_sha256"`
	FileSize      int64   `json:"file_size"`
	Position      float64 `json:"position"`
	Offset        float64 `json:"offset"`
	Speed         float64 `json:"speed"`
	Pitch         float64 `json:"pitch"`
	Volume        float64 `json:"volume"`
	Duration      float64 `json:"duration"`
	TimelineStart float64 `json:"timeline_start"`
	TimelineEnd   float64 `json:"timeline_end"`
}

type sceneAudioFragment struct {
	DialogueID uint    `json:"dialogue_id"`
	StartRune  int     `json:"start_rune"`
	EndRune    int     `json:"end_rune"`
	AudioStart float64 `json:"audio_start"`
	AudioEnd   float64 `json:"audio_end"`
	Text       string  `json:"text"`
}

type sceneAudioShot struct {
	ID                    uint                       `json:"id"`
	Order                 int                        `json:"order"`
	Duration              float64                    `json:"duration"`
	Ranges                []models.ShotDialogueRange `json:"dialogue_ranges"`
	Fragments             []sceneAudioFragment       `json:"fragments,omitempty"`
	Presentation          string                     `json:"presentation"`
	ContinuesFromPrevious bool                       `json:"continues_from_previous"`
	ContinuesToNext       bool                       `json:"continues_to_next"`
}

func sceneAudioNeedsMix(dialogueCount int, snapshot sceneAudioSnapshot) bool {
	return dialogueCount > 1 || len(snapshot.Shots) > 1
}

type sceneAudioSnapshot struct {
	Version              int                `json:"version"`
	ProjectID            uint               `json:"project_id"`
	SceneID              uint               `json:"scene_id"`
	SceneDuration        float64            `json:"scene_duration"`
	Sources              []sceneAudioSource `json:"sources"`
	Shots                []sceneAudioShot   `json:"shots,omitempty"`
	FragmentTimingMethod string             `json:"fragment_timing_method,omitempty"`
}

func (s *ProjectService) sceneAudioShotContract(sc *models.Scene, dialogues []models.Dialogue, sources []sceneAudioSource) ([]sceneAudioShot, error) {
	if !s.db.Migrator().HasTable(&models.Shot{}) {
		return nil, nil
	}
	var shots []models.Shot
	if err := s.db.Where("scene_id = ?", sc.ID).Order("order_num, id").Find(&shots).Error; err != nil {
		return nil, err
	}
	if len(shots) == 0 {
		return nil, nil
	}
	if math.Abs(shotDurationTotal(shots)-normalizeSceneDuration(sc.Duration)) > shotDurationTolerance {
		return nil, fmt.Errorf("Shot总时长 %.1f 秒与Scene时长 %.1f 秒不一致", shotDurationTotal(shots), normalizeSceneDuration(sc.Duration))
	}
	fragments, err := NewShotService(s.db).ResolveDialogueFragments(sc.ID, shots)
	if err != nil {
		return nil, err
	}
	validationShots := append([]models.Shot(nil), shots...)
	for i := range validationShots {
		var text strings.Builder
		for _, fragment := range fragments[validationShots[i].ID] {
			text.WriteString(fragment.Text)
		}
		validationShots[i].Dialogue = text.String()
	}
	if issues := dialogueRhythmBlockingIssuesForShots(validationShots, dialogues); len(issues) > 0 {
		return nil, fmt.Errorf("Scene音轨的Native Shot对白呈现未通过审核: %s", strings.Join(issues, "；"))
	}
	if err := validateDialogueShotTiming(validationShots, dialogues); err != nil {
		return nil, err
	}
	draft := &sceneDirectorDraft{Shots: make([]sceneDirectorDraftShot, len(validationShots))}
	for i, shot := range validationShots {
		draft.Shots[i] = sceneDirectorDraftShot{ShotType: shot.ShotType, Duration: shot.Duration, Description: shot.Description, Dialogue: shot.Dialogue, StartState: shot.StartState, EndState: shot.EndState, PromptSubject: shot.PromptSubject, PromptAction: shot.PromptAction}
	}
	reviews := buildDialogueRhythmReview(draft, dialogues)
	sourceByDialogue := make(map[uint]sceneAudioSource, len(sources))
	for _, source := range sources {
		sourceByDialogue[source.DialogueID] = source
	}
	dialogueByID := make(map[uint]models.Dialogue, len(dialogues))
	groupStart := map[uint]int{}
	groupOffsets := map[string]int{}
	groupOf := map[uint]string{}
	for _, shot := range shots {
		for _, r := range shot.DialogueRanges {
			if groupOf[r.DialogueID] == "" {
				groupOf[r.DialogueID] = r.GroupKey
			}
		}
	}
	for _, dialogue := range dialogues {
		key := groupOf[dialogue.ID]
		groupStart[dialogue.ID] = groupOffsets[key]
		groupOffsets[key] += len([]rune(canonicalDialogueText(dialogue.Text)))
		dialogueByID[dialogue.ID] = dialogue
	}
	totalWeights := map[uint]float64{}
	for _, shot := range shots {
		for _, fragment := range fragments[shot.ID] {
			totalWeights[fragment.DialogueID] += dialogueTextDuration(fragment.Text)
		}
	}
	usedWeights := map[uint]float64{}
	var actual strings.Builder
	audit := make([]sceneAudioShot, 0, len(shots))
	shotStart := 0.0
	reviewByShot := make(map[int]dialogueRhythmShotReview, len(reviews))
	for _, review := range reviews {
		reviewByShot[review.Shot] = review
	}
	for _, shot := range shots {
		shotEnd := shotStart + shot.Duration
		review := reviewByShot[shot.Order]
		shotAudit := sceneAudioShot{ID: shot.ID, Order: shot.Order, Duration: shot.Duration, Ranges: append([]models.ShotDialogueRange(nil), shot.DialogueRanges...), Presentation: review.Presentation, ContinuesFromPrevious: review.ContinuesFromPrev, ContinuesToNext: review.ContinuesToNext}
		shotFragments := fragments[shot.ID]
		if len(shotFragments) == 0 {
			for _, source := range sources {
				if source.TimelineStart < shotEnd-0.001 && source.TimelineEnd > shotStart+0.001 {
					return nil, fmt.Errorf("静音Shot %d 与Dialogue %d音频区间重叠", shot.Order, source.DialogueID)
				}
			}
		}
		for _, fragment := range shotFragments {
			actual.WriteString(canonicalDialogueText(fragment.Text))
			source, ok := sourceByDialogue[fragment.DialogueID]
			dialogue := dialogueByID[fragment.DialogueID]
			textLength := len([]rune(canonicalDialogueText(dialogue.Text)))
			if !ok || textLength == 0 {
				return nil, fmt.Errorf("Shot %d 的Dialogue %d缺少有效音频来源", shot.Order, fragment.DialogueID)
			}
			localStart, localEnd := fragment.StartRune-groupStart[fragment.DialogueID], fragment.EndRune-groupStart[fragment.DialogueID]
			if localStart < 0 || localEnd > textLength || localEnd <= localStart {
				return nil, fmt.Errorf("Shot %d 的Dialogue %d局部rune区间无效", shot.Order, fragment.DialogueID)
			}
			audioDuration := source.TimelineEnd - source.TimelineStart
			totalWeight, weight := totalWeights[fragment.DialogueID], dialogueTextDuration(fragment.Text)
			if totalWeight <= 0 || weight <= 0 {
				return nil, fmt.Errorf("Shot %d 的Dialogue %d缺少有效自然语速权重", shot.Order, fragment.DialogueID)
			}
			fragmentStart := source.TimelineStart + audioDuration*usedWeights[fragment.DialogueID]/totalWeight
			usedWeights[fragment.DialogueID] += weight
			fragmentEnd := source.TimelineStart + audioDuration*usedWeights[fragment.DialogueID]/totalWeight
			if fragmentStart < shotStart-0.001 || fragmentEnd > shotEnd+0.001 {
				return nil, fmt.Errorf("Shot %d 的Dialogue %d音频片段 %.3f-%.3f 秒超出Shot区间 %.3f-%.3f 秒", shot.Order, fragment.DialogueID, fragmentStart, fragmentEnd, shotStart, shotEnd)
			}
			shotAudit.Fragments = append(shotAudit.Fragments, sceneAudioFragment{DialogueID: fragment.DialogueID, StartRune: fragment.StartRune, EndRune: fragment.EndRune, AudioStart: fragmentStart, AudioEnd: fragmentEnd, Text: fragment.Text})
		}
		audit = append(audit, shotAudit)
		shotStart = shotEnd
	}
	var expected strings.Builder
	for _, dialogue := range dialogues {
		expected.WriteString(canonicalDialogueText(dialogue.Text))
	}
	if actual.String() != expected.String() {
		return nil, fmt.Errorf("Shot对白区间未逐字、按序且无重复覆盖完整权威Dialogue")
	}
	return audit, nil
}

func (s *ProjectService) currentSceneAudioSnapshot(sc *models.Scene, dialogues []models.Dialogue) (sceneAudioSnapshot, string, error) {
	snapshot := sceneAudioSnapshot{Version: 3, ProjectID: sc.ProjectID, SceneID: sc.ID, SceneDuration: normalizeSceneDuration(sc.Duration), FragmentTimingMethod: "dialogue_text_duration_weighted_v1"}
	for _, dialogue := range dialogues {
		if dialogue.AudioStale || dialogue.Status != "ready" || strings.TrimSpace(dialogue.AudioFile) == "" || dialogue.AudioHash == "" || dialogue.AudioHash != s.effectiveDialogueAudioHash(dialogue) {
			return snapshot, "", fmt.Errorf("Dialogue %d 的生产音频未就绪、已过期或哈希不匹配", dialogue.ID)
		}
		var rows []models.DialogueAudioCandidate
		if err := s.db.Where("project_id = ? AND dialogue_id = ? AND is_current = ? AND review_status = ? AND stale = ? AND input_hash = ? AND file = ?", sc.ProjectID, dialogue.ID, true, "accepted", false, dialogue.AudioHash, dialogue.AudioFile).Order("id DESC").Find(&rows).Error; err != nil {
			return snapshot, "", err
		}
		if len(rows) != 1 {
			return snapshot, "", fmt.Errorf("Dialogue %d 必须且只能有一个已接受、当前且哈希一致的候选", dialogue.ID)
		}
		if rows[0].Duration <= 0 {
			return snapshot, "", fmt.Errorf("Dialogue %d 的已审核候选缺少有效时长", dialogue.ID)
		}
		if strings.TrimSpace(rows[0].FileSHA256) == "" || rows[0].FileSize <= 0 {
			return snapshot, "", fmt.Errorf("Dialogue %d 的已审核候选缺少文件内容指纹，请重新生成并审核", dialogue.ID)
		}
		if rows[0].ReviewConfirmation != dialogueAudioReviewConfirmationV1 || rows[0].ReviewInputHash != rows[0].InputHash || rows[0].ReviewFileSHA256 != rows[0].FileSHA256 {
			return snapshot, "", fmt.Errorf("Dialogue %d 的候选缺少匹配当前内容的人工审核证据", dialogue.ID)
		}
		snapshot.Sources = append(snapshot.Sources, sceneAudioSource{DialogueID: dialogue.ID, CandidateID: rows[0].ID, InputHash: rows[0].InputHash, File: rows[0].File, FileSHA256: rows[0].FileSHA256, FileSize: rows[0].FileSize, Position: dialogue.Position, Offset: dialogue.Offset, Speed: dialogueSpeed(dialogue), Pitch: dialogue.Pitch, Volume: dialogueVolume(dialogue), Duration: rows[0].Duration})
	}
	cursor := 0.4
	for i := range snapshot.Sources {
		source := &snapshot.Sources[i]
		if source.Position > 0 {
			cursor = source.Position
		}
		start := math.Max(0, cursor+source.Offset)
		source.TimelineStart, source.TimelineEnd = start, start+source.Duration/source.Speed
		if source.TimelineEnd > snapshot.SceneDuration+0.001 {
			return snapshot, "", fmt.Errorf("Dialogue %d 音频结束于 %.3f 秒，超过Scene时长 %.3f 秒", source.DialogueID, source.TimelineEnd, snapshot.SceneDuration)
		}
		cursor = source.TimelineEnd + 0.35
	}
	shots, err := s.sceneAudioShotContract(sc, dialogues, snapshot.Sources)
	if err != nil {
		return snapshot, "", err
	}
	snapshot.Shots = shots
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return snapshot, "", err
	}
	digest := sha256.Sum256(encoded)
	return snapshot, hex.EncodeToString(digest[:]), nil
}

// sceneReviewedAudioSelection accepts one directly reviewed take or one separately reviewed
// Scene mix whose source hash still matches every authoritative Dialogue and accepted take.
func sceneAudioFileSHA256(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	digest := sha256.New()
	size, err := io.Copy(digest, file)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(digest.Sum(nil)), size, nil
}

type sceneMixWAVInfo struct {
	SampleRate uint32
	Channels   uint16
	Bits       uint16
	DataBytes  uint32
	Duration   float64
}

func inspectPCM16WAV(path string) (sceneMixWAVInfo, error) {
	file, err := os.Open(path)
	if err != nil {
		return sceneMixWAVInfo{}, err
	}
	defer file.Close()
	var header [12]byte
	if _, err := io.ReadFull(file, header[:]); err != nil || string(header[:4]) != "RIFF" || string(header[8:]) != "WAVE" {
		return sceneMixWAVInfo{}, fmt.Errorf("不是有效RIFF/WAVE文件")
	}
	info := sceneMixWAVInfo{}
	formatFound, dataFound := false, false
	for {
		var chunk [8]byte
		if _, err := io.ReadFull(file, chunk[:]); err != nil {
			break
		}
		size := binary.LittleEndian.Uint32(chunk[4:])
		switch string(chunk[:4]) {
		case "fmt ":
			if size < 16 {
				return info, fmt.Errorf("WAV fmt块无效")
			}
			payload := make([]byte, size)
			if _, err := io.ReadFull(file, payload); err != nil {
				return info, err
			}
			if binary.LittleEndian.Uint16(payload[0:2]) != 1 {
				return info, fmt.Errorf("Scene音轨必须为PCM WAV")
			}
			info.Channels = binary.LittleEndian.Uint16(payload[2:4])
			info.SampleRate = binary.LittleEndian.Uint32(payload[4:8])
			info.Bits = binary.LittleEndian.Uint16(payload[14:16])
			formatFound = true
		case "data":
			info.DataBytes, dataFound = size, true
			if _, err := file.Seek(int64(size), io.SeekCurrent); err != nil {
				return info, err
			}
		default:
			if _, err := file.Seek(int64(size), io.SeekCurrent); err != nil {
				return info, err
			}
		}
		if size%2 == 1 {
			_, _ = file.Seek(1, io.SeekCurrent)
		}
		if formatFound && dataFound {
			break
		}
	}
	if !formatFound || !dataFound || info.SampleRate == 0 || info.Channels == 0 || info.Bits != 16 || info.DataBytes == 0 {
		return info, fmt.Errorf("Scene音轨必须是非空PCM16 WAV")
	}
	bytesPerFrame := float64(info.Channels) * float64(info.Bits/8)
	info.Duration = float64(info.DataBytes) / bytesPerFrame / float64(info.SampleRate)
	return info, nil
}

func (s *ProjectService) dialogueAudioCandidateFile(sc *models.Scene, candidate *models.DialogueAudioCandidate) error {
	if candidate == nil || strings.TrimSpace(candidate.FileSHA256) == "" || candidate.FileSize <= 0 {
		return fmt.Errorf("Dialogue音频候选缺少文件内容指纹，请重新生成并审核")
	}
	if s.cfg == nil {
		return nil
	}
	root := filepath.Join(s.cfg.Comfy.ComfyDir, "input", fmt.Sprint(sc.ProjectID))
	path := filepath.Join(root, filepath.Base(candidate.File))
	if !pathInside(root, path) {
		return fmt.Errorf("Dialogue音频文件路径越界")
	}
	hash, size, err := sceneAudioFileSHA256(path)
	if err != nil || size <= 0 {
		return fmt.Errorf("Dialogue音频文件不存在或不可读")
	}
	if hash != candidate.FileSHA256 || size != candidate.FileSize {
		return fmt.Errorf("Dialogue音频文件内容与已审核候选不一致")
	}
	return nil
}

func (s *ProjectService) sceneAudioCandidateFile(sc *models.Scene, candidate *models.SceneAudioCandidate) (string, error) {
	if sc == nil || candidate == nil || s.cfg == nil || strings.TrimSpace(candidate.File) == "" {
		return "", fmt.Errorf("Scene音轨文件路径无效")
	}
	if strings.TrimSpace(candidate.FileSHA256) == "" || candidate.FileSize <= 44 {
		return "", fmt.Errorf("Scene音轨候选缺少可信内容哈希，请重新生成并审核")
	}
	root := filepath.Join(s.cfg.Comfy.ComfyDir, "input", fmt.Sprint(sc.ProjectID))
	path := filepath.Join(root, filepath.Base(candidate.File))
	if !pathInside(root, path) {
		return "", fmt.Errorf("Scene音轨文件路径越界")
	}
	hash, size, err := sceneAudioFileSHA256(path)
	if err != nil || size <= 44 {
		return "", fmt.Errorf("Scene音轨文件不存在、不可读或为空")
	}
	if size != candidate.FileSize || hash != candidate.FileSHA256 {
		return "", fmt.Errorf("Scene音轨文件内容与审核记录不一致")
	}
	wav, wavErr := inspectPCM16WAV(path)
	if wavErr != nil {
		return "", wavErr
	}
	allowedDelta := 1.0/24.0 + 1.0/float64(wav.SampleRate)
	if math.Abs(wav.Duration-candidate.Duration) > allowedDelta {
		return "", fmt.Errorf("Scene音轨实际时长 %.3f 秒与候选记录 %.3f 秒不一致", wav.Duration, candidate.Duration)
	}
	expected := normalizeSceneDuration(sc.Duration)
	if math.Abs(wav.Duration-expected) > allowedDelta {
		return "", fmt.Errorf("Scene音轨实际时长 %.3f 秒与当前Scene时长 %.3f 秒不一致", wav.Duration, expected)
	}
	return path, nil
}

func (s *ProjectService) refreshSceneAudioCandidates(sc *models.Scene, dialogues []models.Dialogue) (string, error) {
	_, sourceHash, err := s.currentSceneAudioSnapshot(sc, dialogues)
	reason := "Scene音轨来源已变化"
	if err != nil {
		reason = "Scene音轨来源当前无效: " + err.Error()
	}
	query := s.db.Model(&models.SceneAudioCandidate{}).Where("project_id = ? AND scene_id = ? AND stale = ?", sc.ProjectID, sc.ID, false)
	if sourceHash != "" {
		query = query.Where("source_hash <> ?", sourceHash)
	}
	if updateErr := query.Updates(map[string]any{"stale": true, "stale_reason": reason, "is_current": false}).Error; updateErr != nil {
		return sourceHash, updateErr
	}
	return sourceHash, err
}

func (s *ProjectService) sceneReviewedAudioSelection(sc *models.Scene) sceneReviewedAudio {
	if sc == nil {
		return sceneReviewedAudio{Reason: "场景不存在"}
	}
	var dialogues []models.Dialogue
	if err := s.db.Where("project_id = ? AND scene_id = ?", sc.ProjectID, sc.ID).Order("`order`, id").Find(&dialogues).Error; err != nil {
		return sceneReviewedAudio{Reason: err.Error()}
	}
	dialogues = validSceneDialogues(dialogues)
	if len(dialogues) == 0 {
		return sceneReviewedAudio{Reason: "场景没有有效结构化 Dialogue"}
	}
	snapshot, sourceHash, snapshotErr := s.currentSceneAudioSnapshot(sc, dialogues)
	if _, refreshErr := s.refreshSceneAudioCandidates(sc, dialogues); refreshErr != nil && snapshotErr == nil {
		return sceneReviewedAudio{Reason: refreshErr.Error()}
	}
	if snapshotErr != nil {
		return sceneReviewedAudio{Reason: snapshotErr.Error()}
	}
	requiresSceneMix := sceneAudioNeedsMix(len(dialogues), snapshot)
	if requiresSceneMix {
		var candidate models.SceneAudioCandidate
		err := s.db.Where("project_id = ? AND scene_id = ? AND source_hash = ? AND is_current = ? AND review_status = ? AND stale = ?", sc.ProjectID, sc.ID, sourceHash, true, "accepted", false).Order("id DESC").First(&candidate).Error
		if err != nil {
			return sceneReviewedAudio{RequiresSceneMix: true, SourceHash: sourceHash, Reason: fmt.Sprintf("场景包含 %d 条 Dialogue 或单条长Dialogue跨 %d 个Native Shot；当前来源尚无已审核的Scene连续音轨", len(dialogues), len(snapshot.Shots))}
		}
		_ = snapshot
		if candidate.ReviewConfirmation != sceneAudioReviewConfirmationV1 || candidate.ReviewSourceHash != sourceHash || candidate.ReviewFileSHA256 != candidate.FileSHA256 {
			_ = markSceneAudioCandidatesStale(s.db, sc.ProjectID, sc.ID, "Scene音轨缺少匹配当前内容的审核确认记录")
			return sceneReviewedAudio{RequiresSceneMix: true, SourceHash: sourceHash, Reason: "Scene音轨缺少匹配当前内容的审核确认记录，请重新审核"}
		}
		if _, fileErr := s.sceneAudioCandidateFile(sc, &candidate); fileErr != nil {
			_ = markSceneAudioCandidatesStale(s.db, sc.ProjectID, sc.ID, fileErr.Error())
			return sceneReviewedAudio{RequiresSceneMix: true, SourceHash: sourceHash, Reason: fileErr.Error()}
		}
		return sceneReviewedAudio{Ready: true, RequiresSceneMix: true, SceneCandidate: &candidate, SourceHash: sourceHash}
	}
	dialogue := dialogues[0]
	if dialogue.AudioStale || strings.TrimSpace(dialogue.AudioFile) == "" || strings.TrimSpace(dialogue.AudioHash) == "" {
		return sceneReviewedAudio{Dialogue: &dialogue, Reason: "当前 Dialogue 没有有效且未过期的生产音频"}
	}
	var candidate models.DialogueAudioCandidate
	err := s.db.Where(
		"project_id = ? AND dialogue_id = ? AND is_current = ? AND review_status = ? AND stale = ? AND input_hash = ? AND file = ?",
		sc.ProjectID, dialogue.ID, true, "accepted", false, dialogue.AudioHash, dialogue.AudioFile,
	).Order("id DESC").First(&candidate).Error
	if err != nil {
		return sceneReviewedAudio{Dialogue: &dialogue, Reason: "生产音频未对应已接受、当前且输入哈希一致的候选"}
	}
	if fileErr := s.dialogueAudioCandidateFile(sc, &candidate); fileErr != nil {
		_ = markDialogueAudioCandidatesStale(s.db, sc.ProjectID, dialogue.ID, fileErr.Error())
		return sceneReviewedAudio{Dialogue: &dialogue, Reason: fileErr.Error()}
	}
	return sceneReviewedAudio{Ready: true, Dialogue: &dialogue, Candidate: &candidate, SourceHash: sourceHash}
}

func (s *ProjectService) attachReviewedSceneAudio(sc *models.Scene, files map[string][]FileMeta) (map[string][]FileMeta, error) {
	selection := s.sceneReviewedAudioSelection(sc)
	if !selection.Ready || (selection.Candidate == nil && selection.SceneCandidate == nil) {
		return nil, fmt.Errorf("所选视频模板需要已审核音频: %s", selection.Reason)
	}
	if files == nil {
		files = map[string][]FileMeta{}
	}
	file := ""
	if selection.Candidate != nil {
		file = selection.Candidate.File
	}
	if selection.SceneCandidate != nil {
		file = selection.SceneCandidate.File
	}
	files["dialogue_audio"] = []FileMeta{{TaskID: fmt.Sprint(sc.ProjectID), Name: filepath.Base(file)}}
	return files, nil
}

func (s *ProjectService) CreateSceneAudioCandidate(sc *models.Scene) (*models.SceneAudioCandidate, error) {
	s.sceneAudioMu.Lock()
	defer s.sceneAudioMu.Unlock()
	if sc == nil || s.cfg == nil {
		return nil, fmt.Errorf("Scene连续音轨服务未配置")
	}
	var dialogues []models.Dialogue
	if err := s.db.Where("project_id = ? AND scene_id = ?", sc.ProjectID, sc.ID).Order("`order`, id").Find(&dialogues).Error; err != nil {
		return nil, err
	}
	dialogues = validSceneDialogues(dialogues)
	if len(dialogues) == 0 {
		return nil, fmt.Errorf("场景没有有效结构化 Dialogue")
	}
	snapshot, sourceHash, err := s.currentSceneAudioSnapshot(sc, dialogues)
	if err != nil {
		return nil, err
	}
	if !sceneAudioNeedsMix(len(dialogues), snapshot) {
		return nil, fmt.Errorf("Scene连续音轨仅用于多条Dialogue或单条长Dialogue跨多个Native Shot的场景")
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return nil, err
	}
	var existing models.SceneAudioCandidate
	if err := s.db.Where("project_id = ? AND scene_id = ? AND source_hash = ?", sc.ProjectID, sc.ID, sourceHash).First(&existing).Error; err == nil {
		if _, fileErr := s.sceneAudioCandidateFile(sc, &existing); fileErr == nil {
			return &existing, nil
		}
		if deleteErr := s.db.Delete(&existing).Error; deleteErr != nil {
			return nil, deleteErr
		}
	}
	inputRoot := filepath.Join(s.cfg.Comfy.ComfyDir, "input", fmt.Sprint(sc.ProjectID))
	if err := os.MkdirAll(inputRoot, 0o755); err != nil {
		return nil, err
	}
	byID := make(map[uint]models.Dialogue, len(dialogues))
	for _, dialogue := range dialogues {
		byID[dialogue.ID] = dialogue
	}
	segments := make([]dubSegment, 0, len(snapshot.Sources))
	for _, source := range snapshot.Sources {
		dialogue := byID[source.DialogueID]
		start, end := source.TimelineStart, source.TimelineEnd
		abs := filepath.Join(inputRoot, filepath.Base(source.File))
		hash, size, hashErr := sceneAudioFileSHA256(abs)
		if hashErr != nil || size <= 0 {
			return nil, fmt.Errorf("Dialogue %d 音频文件缺失", dialogue.ID)
		}
		if hash != source.FileSHA256 || size != source.FileSize {
			return nil, fmt.Errorf("Dialogue %d 音频文件内容与已审核候选不一致", dialogue.ID)
		}
		segments = append(segments, dubSegment{DialogueID: dialogue.ID, SceneID: sc.ID, Start: start, End: end, Character: dialogue.Character, Text: dialogue.Text, AudioAbs: abs, SourceDur: source.Duration, Speed: source.Speed, Pitch: source.Pitch, Volume: source.Volume})
	}
	graph := buildDialogueExportGraph(segments, snapshot.SceneDuration)
	if graph.Label == "" {
		return nil, fmt.Errorf("Scene连续音轨为空")
	}
	ffmpeg, err := localFFmpegPath()
	if err != nil {
		return nil, err
	}
	name := fmt.Sprintf("scene_audio_%d_%s.wav", sc.ID, sourceHash[:16])
	output := filepath.Join(inputRoot, name)
	temp := output + ".tmp.wav"
	args := []string{"-y"}
	for _, segment := range segments {
		args = append(args, "-i", segment.AudioAbs)
	}
	args = append(args, "-filter_complex", graph.Filters, "-map", graph.Label, "-vn", "-c:a", "pcm_s16le", "-ar", "48000", "-ac", "2", temp)
	if _, err := runLocalProgram(ffmpeg, args, 10*time.Minute); err != nil {
		return nil, err
	}
	if err := os.Rename(temp, output); err != nil {
		_ = os.Remove(temp)
		return nil, err
	}
	published := true
	defer func() {
		if published {
			_ = os.Remove(output)
		}
	}()
	fileHash, fileSize, err := sceneAudioFileSHA256(output)
	if err != nil || fileSize <= 44 {
		return nil, fmt.Errorf("Scene连续音轨哈希计算失败或输出为空")
	}
	candidate := models.SceneAudioCandidate{ProjectID: sc.ProjectID, SceneID: sc.ID, SourceHash: sourceHash, SourceJSON: string(encoded), File: name, FileSHA256: fileHash, FileSize: fileSize, Duration: snapshot.SceneDuration, ReviewStatus: "pending"}
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.SceneAudioCandidate{}).Where("project_id = ? AND scene_id = ? AND source_hash <> ?", sc.ProjectID, sc.ID, sourceHash).Updates(map[string]any{"stale": true, "stale_reason": "Dialogue或候选来源已变化", "is_current": false}).Error; err != nil {
			return err
		}
		return tx.Create(&candidate).Error
	})
	if err != nil {
		return nil, err
	}
	published = false
	return &candidate, nil
}

func (s *ProjectService) ReviewSceneAudioCandidate(sc *models.Scene, candidateID uint, status, reason, expectedSourceHash, expectedFileSHA256 string) (*models.SceneAudioCandidate, error) {
	if status != "accepted" && status != "rejected" && status != "pending" {
		return nil, fmt.Errorf("无效审核状态")
	}
	if status == "accepted" && (strings.TrimSpace(expectedSourceHash) == "" || strings.TrimSpace(expectedFileSHA256) == "") {
		return nil, fmt.Errorf("%w：缺少用户已审核的来源或文件哈希", ErrSceneAudioReviewConflict)
	}
	var row models.SceneAudioCandidate
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var currentScene models.Scene
		if err := tx.Where("id = ? AND project_id = ?", sc.ID, sc.ProjectID).First(&currentScene).Error; err != nil {
			return fmt.Errorf("Scene不存在")
		}
		var dialogues []models.Dialogue
		if err := tx.Where("project_id = ? AND scene_id = ?", sc.ProjectID, sc.ID).Order("`order`, id").Find(&dialogues).Error; err != nil {
			return err
		}
		dialogues = validSceneDialogues(dialogues)
		txService := *s
		txService.db = tx
		_, currentSourceHash, err := txService.currentSceneAudioSnapshot(&currentScene, dialogues)
		if err != nil {
			return fmt.Errorf("%w：%v", ErrSceneAudioReviewConflict, err)
		}
		if err := tx.Where("id = ? AND project_id = ? AND scene_id = ?", candidateID, sc.ProjectID, sc.ID).First(&row).Error; err != nil {
			return fmt.Errorf("Scene音轨候选不存在")
		}
		if row.Stale || row.SourceHash != currentSourceHash {
			return fmt.Errorf("%w：候选来源已过期", ErrSceneAudioReviewConflict)
		}
		if status == "accepted" {
			if row.SourceHash != strings.TrimSpace(expectedSourceHash) || row.FileSHA256 != strings.TrimSpace(expectedFileSHA256) {
				return fmt.Errorf("%w：候选不是用户刚才查看的版本", ErrSceneAudioReviewConflict)
			}
			if _, fileErr := txService.sceneAudioCandidateFile(&currentScene, &row); fileErr != nil {
				return fmt.Errorf("%w：%v", ErrSceneAudioReviewConflict, fileErr)
			}
			if err := tx.Model(&models.SceneAudioCandidate{}).Where("project_id = ? AND scene_id = ?", sc.ProjectID, sc.ID).Update("is_current", false).Error; err != nil {
				return err
			}
			row.IsCurrent = true
			row.ReviewSourceHash = row.SourceHash
			row.ReviewFileSHA256 = row.FileSHA256
			row.ReviewConfirmation = sceneAudioReviewConfirmationV1
		} else {
			row.IsCurrent = false
			row.ReviewSourceHash, row.ReviewFileSHA256, row.ReviewConfirmation = "", "", ""
		}
		now := time.Now()
		row.ReviewStatus, row.ReviewReason, row.ReviewedAt = status, strings.TrimSpace(reason), &now
		return tx.Save(&row).Error
	})
	return &row, err
}
