package service

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

type dialogueAudioCapture struct {
	ProjectID      uint
	DialogueID     uint
	Token          string
	InputHash      string
	Provider       string
	RuntimeVersion string
	VoiceIdentity  string
	Params         any
	File           string
	SampleRate     uint32
	Channels       uint32
	Duration       float64
}

func (s *ProjectService) captureDialogueAudio(in dialogueAudioCapture) (*models.DialogueAudioCandidate, error) {
	if in.ProjectID == 0 || in.DialogueID == 0 || strings.TrimSpace(in.Token) == "" || strings.TrimSpace(in.InputHash) == "" || strings.TrimSpace(in.File) == "" {
		return nil, fmt.Errorf("配音候选输入不完整")
	}
	params, err := json.Marshal(in.Params)
	if err != nil {
		return nil, fmt.Errorf("序列化配音参数失败: %w", err)
	}
	var candidate models.DialogueAudioCandidate
	err = s.db.Transaction(func(tx *gorm.DB) error {
		var dialogue models.Dialogue
		if err := tx.Where("id = ? AND project_id = ? AND audio_token = ?", in.DialogueID, in.ProjectID, in.Token).First(&dialogue).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return fmt.Errorf("对白合成结果已过期")
			}
			return err
		}
		if err := tx.Model(&models.DialogueAudioCandidate{}).
			Where("project_id = ? AND dialogue_id = ? AND is_current = ?", in.ProjectID, in.DialogueID, true).
			Update("is_current", false).Error; err != nil {
			return err
		}
		lookup := tx.Where("project_id = ? AND dialogue_id = ? AND input_hash = ? AND provider = ?", in.ProjectID, in.DialogueID, in.InputHash, in.Provider).First(&candidate)
		if lookup.Error != nil && lookup.Error != gorm.ErrRecordNotFound {
			return lookup.Error
		}
		values := map[string]any{
			"runtime_version": in.RuntimeVersion, "voice_identity": in.VoiceIdentity, "params_json": string(params),
			"file": in.File, "sample_rate": in.SampleRate, "channels": in.Channels, "duration": in.Duration,
			"review_status": "pending", "review_reason": "", "reviewed_at": nil, "is_current": true,
			"stale": false, "stale_reason": "",
		}
		if lookup.Error == gorm.ErrRecordNotFound {
			candidate = models.DialogueAudioCandidate{ProjectID: in.ProjectID, DialogueID: in.DialogueID, InputHash: in.InputHash, Provider: in.Provider}
			if err := tx.Create(&candidate).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&candidate).Updates(values).Error; err != nil {
			return err
		}
		result := tx.Model(&dialogue).Where("audio_token = ?", in.Token).Updates(map[string]any{
			"status": "ready", "audio_file": in.File, "previous_audio_file": dialogue.AudioFile, "previous_audio_hash": dialogue.AudioHash,
			"audio_revision": dialogue.AudioRevision + 1, "audio_hash": in.InputHash, "audio_token": "",
			"audio_task_id": "", "audio_task_hash": "", "audio_stale": false, "audio_stale_reason": "", "error": "",
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("对白合成结果已过期")
		}
		return tx.First(&candidate, candidate.ID).Error
	})
	return &candidate, err
}

func (s *ProjectService) ensureLegacyDialogueAudioCandidate(projectID, dialogueID uint) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		var dialogue models.Dialogue
		if err := tx.Where("id = ? AND project_id = ?", dialogueID, projectID).First(&dialogue).Error; err != nil {
			return fmt.Errorf("对白不存在")
		}
		if strings.TrimSpace(dialogue.AudioFile) == "" || strings.TrimSpace(dialogue.AudioHash) == "" {
			return nil
		}
		var count int64
		if err := tx.Model(&models.DialogueAudioCandidate{}).Where("project_id = ? AND dialogue_id = ? AND file = ?", projectID, dialogueID, dialogue.AudioFile).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return nil
		}
		if err := tx.Model(&models.DialogueAudioCandidate{}).Where("project_id = ? AND dialogue_id = ? AND is_current = ?", projectID, dialogueID, true).Update("is_current", false).Error; err != nil {
			return err
		}
		row := models.DialogueAudioCandidate{
			ProjectID: projectID, DialogueID: dialogueID, InputHash: dialogue.AudioHash,
			Provider: "legacy", RuntimeVersion: "pre-candidate-history", File: dialogue.AudioFile,
			ParamsJSON: "{}", ReviewStatus: "accepted", IsCurrent: true,
		}
		now := time.Now()
		row.ReviewedAt = &now
		return tx.Create(&row).Error
	})
}

func (s *ProjectService) ListDialogueAudioCandidates(projectID, dialogueID uint) ([]models.DialogueAudioCandidate, error) {
	if err := s.ensureLegacyDialogueAudioCandidate(projectID, dialogueID); err != nil {
		return nil, err
	}
	var rows []models.DialogueAudioCandidate
	return rows, s.db.Where("project_id = ? AND dialogue_id = ?", projectID, dialogueID).Order("created_at DESC, id DESC").Find(&rows).Error
}

func (s *ProjectService) ReviewDialogueAudioCandidate(projectID, dialogueID, candidateID uint, status, reason string) (*models.DialogueAudioCandidate, error) {
	if status != "pending" && status != "accepted" && status != "rejected" {
		return nil, fmt.Errorf("无效审核状态")
	}
	var row models.DialogueAudioCandidate
	if err := s.db.Where("id = ? AND project_id = ? AND dialogue_id = ?", candidateID, projectID, dialogueID).First(&row).Error; err != nil {
		return nil, fmt.Errorf("配音候选不存在")
	}
	if row.IsCurrent && status == "rejected" {
		return nil, fmt.Errorf("当前采用的配音不能拒绝，请先选择另一候选")
	}
	updates := map[string]any{"review_status": status, "review_reason": strings.TrimSpace(reason)}
	if status == "pending" {
		updates["reviewed_at"] = nil
	} else {
		updates["reviewed_at"] = time.Now()
	}
	if err := s.db.Model(&row).Updates(updates).Error; err != nil {
		return nil, err
	}
	s.db.First(&row, row.ID)
	return &row, nil
}

func (s *ProjectService) ensureDialogueAudioCandidateFile(projectID uint, file string) error {
	if s.upload == nil || s.remote == nil {
		return nil
	}
	path := filepath.Join(s.upload.InputDir(), fmt.Sprint(projectID), filepath.FromSlash(file))
	reader, err := s.remote.Open(path)
	if err != nil {
		return fmt.Errorf("配音候选文件不存在或不可读取")
	}
	return reader.Close()
}

func (s *ProjectService) SelectDialogueAudioCandidate(projectID, dialogueID, candidateID uint) (*models.DialogueAudioCandidate, error) {
	var selected models.DialogueAudioCandidate
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var dialogue models.Dialogue
		if err := tx.Where("id = ? AND project_id = ?", dialogueID, projectID).First(&dialogue).Error; err != nil {
			return fmt.Errorf("对白不存在")
		}
		var row models.DialogueAudioCandidate
		if err := tx.Where("id = ? AND project_id = ? AND dialogue_id = ?", candidateID, projectID, dialogueID).First(&row).Error; err != nil {
			return fmt.Errorf("配音候选不存在")
		}
		if row.Stale || row.ReviewStatus == "rejected" {
			return fmt.Errorf("过期或已拒绝的配音候选不能采用")
		}
		if err := s.ensureDialogueAudioCandidateFile(projectID, row.File); err != nil {
			return err
		}
		if row.InputHash != s.effectiveDialogueAudioHash(dialogue) {
			return fmt.Errorf("配音候选与当前对白或音色配置不一致")
		}
		if err := tx.Model(&models.DialogueAudioCandidate{}).Where("project_id = ? AND dialogue_id = ?", projectID, dialogueID).Update("is_current", false).Error; err != nil {
			return err
		}
		now := time.Now()
		if err := tx.Model(&row).Updates(map[string]any{"is_current": true, "review_status": "accepted", "reviewed_at": now}).Error; err != nil {
			return err
		}
		if err := tx.Model(&dialogue).Updates(map[string]any{
			"previous_audio_file": dialogue.AudioFile, "previous_audio_hash": dialogue.AudioHash,
			"audio_file": row.File, "audio_hash": row.InputHash, "audio_revision": dialogue.AudioRevision + 1,
			"audio_stale": false, "audio_stale_reason": "", "status": "ready", "error": "", "audio_token": "",
		}).Error; err != nil {
			return err
		}
		selected = row
		selected.IsCurrent, selected.ReviewStatus, selected.ReviewedAt = true, "accepted", &now
		return nil
	})
	return &selected, err
}

func markDialogueAudioCandidatesStale(tx *gorm.DB, projectID, dialogueID uint, reason string) error {
	return tx.Model(&models.DialogueAudioCandidate{}).
		Where("project_id = ? AND dialogue_id = ? AND stale = ?", projectID, dialogueID, false).
		Updates(map[string]any{"stale": true, "stale_reason": reason, "is_current": false}).Error
}
