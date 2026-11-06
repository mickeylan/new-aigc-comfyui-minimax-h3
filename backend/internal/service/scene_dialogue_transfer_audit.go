package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

// DialogueTransferAudit is an advisory, read-only comparison of the effective
// speech stream on either side of a scene structure transformation.
type DialogueTransferAudit struct {
	Advisory          bool     `json:"advisory"`
	Assessed          bool     `json:"assessed"`
	Status            string   `json:"status"`
	BeforeTextHash    string   `json:"before_text_hash"`
	AfterTextHash     string   `json:"after_text_hash"`
	BeforeOccurrences int      `json:"before_occurrences"`
	AfterOccurrences  int      `json:"after_occurrences"`
	Warnings          []string `json:"warnings"`
}

type dialogueTransferSegment struct {
	Character  string `json:"character"`
	SpeechType string `json:"speech_type"`
	Text       string `json:"text"`
}

func dialogueTransferStream(dialogues []models.Dialogue) []dialogueTransferSegment {
	out := make([]dialogueTransferSegment, 0, len(dialogues))
	for _, d := range dialogues {
		if d.Text == "" || d.Source == "user_excluded" || d.SpeechType == "sound_effect" {
			continue
		}
		n := len(out)
		if n > 0 && out[n-1].Character == d.Character && out[n-1].SpeechType == d.SpeechType {
			out[n-1].Text += d.Text
			continue
		}
		out = append(out, dialogueTransferSegment{Character: d.Character, SpeechType: d.SpeechType, Text: d.Text})
	}
	return out
}

func dialogueTransferHash(stream []dialogueTransferSegment) string {
	text := ""
	for _, item := range stream {
		text += item.Text
	}
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func assessDialogueTransfer(before, after []models.Dialogue) DialogueTransferAudit {
	b, a := dialogueTransferStream(before), dialogueTransferStream(after)
	report := DialogueTransferAudit{Advisory: true, Assessed: true, Status: "consistent", BeforeTextHash: dialogueTransferHash(b), AfterTextHash: dialogueTransferHash(a), BeforeOccurrences: len(b), AfterOccurrences: len(a), Warnings: []string{}}
	bj, _ := json.Marshal(b)
	aj, _ := json.Marshal(a)
	if string(bj) != string(aj) {
		report.Status = "attention"
		report.Warnings = append(report.Warnings, "对白文本顺序、重复次数或说话人/类型归属在场景转换后发生变化，请复核。")
	}
	return report
}

func dialogueTransferNotAssessed() DialogueTransferAudit {
	return DialogueTransferAudit{Advisory: true, Assessed: false, Status: "not_assessed", Warnings: []string{"对白转移复核暂不可用；场景操作已正常完成，请手动复核对白。"}}
}

// resolveSceneDialogueTransfer deliberately validates the scoped DB read before
// using sceneVideoDialogues, whose legacy API does not return read errors.
func resolveSceneDialogueTransfer(db *gorm.DB, projectID uint, ids []uint) ([]models.Dialogue, error) {
	if len(ids) == 0 {
		return []models.Dialogue{}, nil
	}
	var scenes []models.Scene
	if err := db.Where("id IN ? AND project_id = ?", ids, projectID).Find(&scenes).Error; err != nil {
		return nil, err
	}
	byID := make(map[uint]*models.Scene, len(scenes))
	for i := range scenes {
		byID[scenes[i].ID] = &scenes[i]
	}
	ps := &ProjectService{db: db}
	out := []models.Dialogue{}
	for _, id := range ids {
		sc := byID[id]
		if sc == nil {
			return nil, fmt.Errorf("scene not found")
		}
		var probe []models.Dialogue
		if err := db.Where("scene_id = ? AND project_id = ?", id, projectID).Order("`order`, id").Find(&probe).Error; err != nil {
			return nil, err
		}
		out = append(out, ps.sceneVideoDialogues(sc)...)
	}
	return out, nil
}

func auditDialogueTransfer(db *gorm.DB, projectID uint, before []models.Dialogue, beforeErr error, afterIDs []uint) DialogueTransferAudit {
	if beforeErr != nil {
		return dialogueTransferNotAssessed()
	}
	after, err := resolveSceneDialogueTransfer(db, projectID, afterIDs)
	if err != nil {
		return dialogueTransferNotAssessed()
	}
	return assessDialogueTransfer(before, after)
}
