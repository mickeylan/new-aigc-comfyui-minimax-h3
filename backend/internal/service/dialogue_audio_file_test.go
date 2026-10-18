package service

import (
	"errors"
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

func TestDialogueAudioCandidateWithoutFileFingerprintCannotBeReviewedOrSelected(t *testing.T) {
	db := safetyDB(t, &models.DialogueAudioCandidate{})
	project := models.Project{Title: "legacy-fingerprint"}
	db.Create(&project)
	dialogue := models.Dialogue{ProjectID: project.ID, Text: "原文", Character: "甲", SpeechType: "dialogue", Speed: 1, Volume: 1}
	db.Create(&dialogue)
	service := &ProjectService{db: db}
	row := models.DialogueAudioCandidate{ProjectID: project.ID, DialogueID: dialogue.ID, InputHash: service.effectiveDialogueAudioHash(dialogue), Provider: "legacy", File: "legacy.wav", ReviewStatus: "pending"}
	db.Create(&row)
	if _, err := service.ReviewDialogueAudioCandidate(project.ID, dialogue.ID, row.ID, "accepted", "", row.InputHash, row.FileSHA256); !errors.Is(err, ErrDialogueAudioReviewConflict) {
		t.Fatalf("review err=%v", err)
	}
	if _, err := service.SelectDialogueAudioCandidate(project.ID, dialogue.ID, row.ID, row.InputHash, row.FileSHA256); !errors.Is(err, ErrDialogueAudioReviewConflict) {
		t.Fatalf("select err=%v", err)
	}
}

func TestReviewDialogueAudioCandidateRejectsChangedDialogueInput(t *testing.T) {
	db := safetyDB(t, &models.DialogueAudioCandidate{})
	project := models.Project{Title: "review-current-input"}
	db.Create(&project)
	dialogue := models.Dialogue{ProjectID: project.ID, Text: "旧文本", Character: "甲", SpeechType: "dialogue", Speed: 1, Volume: 1}
	db.Create(&dialogue)
	service := &ProjectService{db: db}
	inputHash := service.effectiveDialogueAudioHash(dialogue)
	row := models.DialogueAudioCandidate{ProjectID: project.ID, DialogueID: dialogue.ID, InputHash: inputHash, Provider: "test", File: "take.wav", FileSHA256: strings.Repeat("a", 64), FileSize: 128, ReviewStatus: "pending"}
	db.Create(&row)
	if err := db.Model(&dialogue).Update("text", "新文本").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReviewDialogueAudioCandidate(project.ID, dialogue.ID, row.ID, "accepted", "", row.InputHash, row.FileSHA256); !errors.Is(err, ErrDialogueAudioReviewConflict) {
		t.Fatalf("err=%v", err)
	}
	db.First(&row, row.ID)
	if row.ReviewStatus != "pending" || row.ReviewConfirmation != "" {
		t.Fatalf("row mutated after conflict: %+v", row)
	}
}

func TestCaptureDialogueAudioRequiresFileFingerprint(t *testing.T) {
	db := safetyDB(t, &models.DialogueAudioCandidate{})
	project := models.Project{Title: "capture-fingerprint"}
	db.Create(&project)
	dialogue := models.Dialogue{ProjectID: project.ID, Text: "原文", Status: "synthesizing", AudioToken: "token"}
	db.Create(&dialogue)
	service := &ProjectService{db: db}
	_, err := service.captureDialogueAudio(dialogueAudioCapture{ProjectID: project.ID, DialogueID: dialogue.ID, Token: "token", InputHash: "hash", Provider: "test", File: "take.wav"})
	if err == nil || !strings.Contains(err.Error(), "输入不完整") {
		t.Fatalf("err=%v", err)
	}
}
