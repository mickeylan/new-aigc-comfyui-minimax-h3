package service

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
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

func isReviewedAudioVideoTemplate(code string) bool {
	switch strings.TrimSpace(code) {
	case h3AudioDrivenRef2VTemplate, h3AudioDrivenT8EnhancedTemplate, h3VideoThenAudioMuxTemplate:
		return true
	default:
		return false
	}
}

type sceneReviewedAudio struct {
	Ready          bool                           `json:"ready"`
	Reason         string                         `json:"reason,omitempty"`
	Dialogue       *models.Dialogue               `json:"dialogue,omitempty"`
	Candidate      *models.DialogueAudioCandidate `json:"candidate,omitempty"`
	SceneCandidate *models.SceneAudioCandidate    `json:"scene_candidate,omitempty"`
	SourceHash     string                         `json:"source_hash,omitempty"`
}

type sceneAudioSource struct {
	DialogueID  uint    `json:"dialogue_id"`
	CandidateID uint    `json:"candidate_id"`
	InputHash   string  `json:"input_hash"`
	File        string  `json:"file"`
	Position    float64 `json:"position"`
	Offset      float64 `json:"offset"`
	Speed       float64 `json:"speed"`
	Pitch       float64 `json:"pitch"`
	Volume      float64 `json:"volume"`
	Duration    float64 `json:"duration"`
}

type sceneAudioSnapshot struct {
	Version       int                `json:"version"`
	ProjectID     uint               `json:"project_id"`
	SceneID       uint               `json:"scene_id"`
	SceneDuration float64            `json:"scene_duration"`
	Sources       []sceneAudioSource `json:"sources"`
}

func (s *ProjectService) currentSceneAudioSnapshot(sc *models.Scene, dialogues []models.Dialogue) (sceneAudioSnapshot, string, error) {
	snapshot := sceneAudioSnapshot{Version: 1, ProjectID: sc.ProjectID, SceneID: sc.ID, SceneDuration: normalizeSceneDuration(sc.Duration)}
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
		snapshot.Sources = append(snapshot.Sources, sceneAudioSource{DialogueID: dialogue.ID, CandidateID: rows[0].ID, InputHash: rows[0].InputHash, File: rows[0].File, Position: dialogue.Position, Offset: dialogue.Offset, Speed: dialogueSpeed(dialogue), Pitch: dialogue.Pitch, Volume: dialogueVolume(dialogue), Duration: rows[0].Duration})
	}
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
	if len(dialogues) != 1 {
		var candidate models.SceneAudioCandidate
		err := s.db.Where("project_id = ? AND scene_id = ? AND source_hash = ? AND is_current = ? AND review_status = ? AND stale = ?", sc.ProjectID, sc.ID, sourceHash, true, "accepted", false).Order("id DESC").First(&candidate).Error
		if err != nil {
			return sceneReviewedAudio{SourceHash: sourceHash, Reason: fmt.Sprintf("场景包含 %d 条 Dialogue；当前来源尚无已审核的Scene连续音轨", len(dialogues))}
		}
		_ = snapshot
		if _, fileErr := s.sceneAudioCandidateFile(sc, &candidate); fileErr != nil {
			_ = markSceneAudioCandidatesStale(s.db, sc.ProjectID, sc.ID, fileErr.Error())
			return sceneReviewedAudio{SourceHash: sourceHash, Reason: fileErr.Error()}
		}
		return sceneReviewedAudio{Ready: true, SceneCandidate: &candidate, SourceHash: sourceHash}
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
	if len(dialogues) < 2 {
		return nil, fmt.Errorf("Scene连续音轨仅用于包含多条有效 Dialogue 的场景")
	}
	snapshot, sourceHash, err := s.currentSceneAudioSnapshot(sc, dialogues)
	if err != nil {
		return nil, err
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
	const gap = 0.35
	cursor := 0.4
	segments := make([]dubSegment, 0, len(snapshot.Sources))
	for _, source := range snapshot.Sources {
		dialogue := byID[source.DialogueID]
		if source.Position > 0 {
			cursor = source.Position
		}
		start := cursor + source.Offset
		if start < 0 {
			start = 0
		}
		duration := source.Duration / source.Speed
		end := start + duration
		if end > snapshot.SceneDuration+0.001 {
			return nil, fmt.Errorf("Dialogue %d 结束于 %.3f 秒，超过Scene时长 %.3f 秒；请先调整权威时间线", dialogue.ID, end, snapshot.SceneDuration)
		}
		abs := filepath.Join(inputRoot, filepath.Base(source.File))
		if info, statErr := os.Stat(abs); statErr != nil || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("Dialogue %d 音频文件缺失", dialogue.ID)
		}
		segments = append(segments, dubSegment{DialogueID: dialogue.ID, SceneID: sc.ID, Start: start, End: end, Character: dialogue.Character, Text: dialogue.Text, AudioAbs: abs, SourceDur: source.Duration, Speed: source.Speed, Pitch: source.Pitch, Volume: source.Volume})
		cursor = end + gap
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

func (s *ProjectService) ReviewSceneAudioCandidate(sc *models.Scene, candidateID uint, status, reason string) (*models.SceneAudioCandidate, error) {
	if status != "accepted" && status != "rejected" && status != "pending" {
		return nil, fmt.Errorf("无效审核状态")
	}
	var dialogues []models.Dialogue
	if err := s.db.Where("project_id = ? AND scene_id = ?", sc.ProjectID, sc.ID).Order("`order`, id").Find(&dialogues).Error; err != nil {
		return nil, err
	}
	dialogues = validSceneDialogues(dialogues)
	_, sourceHash, err := s.currentSceneAudioSnapshot(sc, dialogues)
	if err != nil {
		return nil, err
	}
	var row models.SceneAudioCandidate
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id = ? AND project_id = ? AND scene_id = ?", candidateID, sc.ProjectID, sc.ID).First(&row).Error; err != nil {
			return fmt.Errorf("Scene音轨候选不存在")
		}
		if row.Stale || row.SourceHash != sourceHash {
			return fmt.Errorf("Scene音轨候选来源已过期")
		}
		if status == "accepted" {
			if _, fileErr := s.sceneAudioCandidateFile(sc, &row); fileErr != nil {
				return fileErr
			}
			if err := tx.Model(&models.SceneAudioCandidate{}).Where("project_id = ? AND scene_id = ?", sc.ProjectID, sc.ID).Update("is_current", false).Error; err != nil {
				return err
			}
			row.IsCurrent = true
		} else {
			row.IsCurrent = false
		}
		now := time.Now()
		row.ReviewStatus, row.ReviewReason, row.ReviewedAt = status, strings.TrimSpace(reason), &now
		return tx.Save(&row).Error
	})
	return &row, err
}
