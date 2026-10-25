package service

import (
	"crypto/sha256"
	"encoding/hex"
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

func TestListDialogueAudioCandidatesMarksMismatchedAcceptedCandidateStale(t *testing.T) {
	db := safetyDB(t, &models.DialogueAudioCandidate{}, &models.SceneAudioCandidate{})
	project := models.Project{Title: "refresh-candidate"}
	db.Create(&project)
	scene := models.Scene{ProjectID: project.ID, Order: 1}
	db.Create(&scene)
	dialogue := models.Dialogue{ProjectID: project.ID, SceneID: scene.ID, Text: "当前", Character: "甲", SpeechType: "dialogue", Speed: 1, Volume: 1}
	db.Create(&dialogue)
	service := &ProjectService{db: db}
	row := models.DialogueAudioCandidate{ProjectID: project.ID, DialogueID: dialogue.ID, InputHash: "old", Provider: "test", File: "old.wav", FileSHA256: strings.Repeat("a", 64), FileSize: 128, ReviewStatus: "accepted", ReviewInputHash: "old", ReviewFileSHA256: strings.Repeat("a", 64), ReviewConfirmation: dialogueAudioReviewConfirmationV1, IsCurrent: true}
	db.Create(&row)
	sceneMix := models.SceneAudioCandidate{ProjectID: project.ID, SceneID: scene.ID, SourceHash: "mix", File: "mix.wav", ReviewStatus: "accepted", IsCurrent: true}
	db.Create(&sceneMix)
	rows, err := service.ListDialogueAudioCandidates(project.ID, dialogue.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || !rows[0].Stale || rows[0].IsCurrent {
		t.Fatalf("rows=%+v", rows)
	}
	db.First(&sceneMix, sceneMix.ID)
	if !sceneMix.Stale || sceneMix.IsCurrent {
		t.Fatalf("scene mix=%+v", sceneMix)
	}
}

func TestDialogueAudioFingerprintUsesPersistedBytesNotRelativeStoragePath(t *testing.T) {
	data := []byte("generated FLAC bytes")
	want := sha256.Sum256(data)
	got, size := sceneAudioBytesSHA256(data)
	if got != hex.EncodeToString(want[:]) || size != int64(len(data)) {
		t.Fatalf("hash=%q size=%d", got, size)
	}
	// UploadManager.SaveFile returns paths such as 1/dub_52_x.flac. The hashing
	// path must not attempt os.Open on that storage-relative value.
	if _, _, err := sceneAudioFileSHA256("1/dub_52_x.flac"); err == nil {
		t.Fatal("fixture unexpectedly exists; regression no longer proves relative paths are unsafe")
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
