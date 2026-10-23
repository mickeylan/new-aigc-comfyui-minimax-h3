package service

import (
	"errors"
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

func TestDialogueAudioCandidateRegistersAuthoritativeDependency(t *testing.T) {
	db := safetyDB(t, &models.DialogueAudioCandidate{}, &models.ArtifactDependency{}, &models.ArtifactInvalidationEvent{})
	project := models.Project{Title: "p"}
	db.Create(&project)
	dialogue := models.Dialogue{ProjectID: project.ID, Text: "你好", Status: "synthesizing", AudioToken: "token"}
	db.Create(&dialogue)
	graph := NewArtifactDependencyService(db, true)
	service := &ProjectService{db: db, artifactDependencies: graph}
	candidate, err := service.captureDialogueAudio(dialogueAudioCapture{ProjectID: project.ID, DialogueID: dialogue.ID, Token: "token", InputHash: "hash-v1", Provider: "test", File: "take.wav", FileSHA256: strings.Repeat("a", 64), FileSize: 128})
	if err != nil {
		t.Fatal(err)
	}
	var edge models.ArtifactDependency
	if err := db.Where("upstream_kind = ? AND upstream_id = ? AND downstream_kind = ? AND downstream_id = ?", ArtifactKindDialogue, dialogue.ID, ArtifactKindDialogueAudioCandidate, candidate.ID).First(&edge).Error; err != nil {
		t.Fatal(err)
	}
	if edge.UpstreamVersion != "hash-v1" {
		t.Fatalf("unexpected edge: %+v", edge)
	}
}

func TestCaptureAndSelectDialogueAudioCandidates(t *testing.T) {
	db := safetyDB(t, &models.DialogueAudioCandidate{})
	project := models.Project{Title: "p"}
	db.Create(&project)
	dialogue := models.Dialogue{ProjectID: project.ID, Text: "你好", Character: "林夏", SpeechType: "dialogue", Speed: 1, Volume: 1, Status: "synthesizing", AudioToken: "first"}
	db.Create(&dialogue)
	service := &ProjectService{db: db}
	hash := service.effectiveDialogueAudioHash(dialogue)
	first, err := service.captureDialogueAudio(dialogueAudioCapture{ProjectID: project.ID, DialogueID: dialogue.ID, Token: "first", InputHash: hash, Provider: "index_tts_rust", File: "first.wav", FileSHA256: strings.Repeat("a", 64), FileSize: 128, SampleRate: 22050, Channels: 1, Duration: 1})
	if err != nil {
		t.Fatal(err)
	}
	if first.IsCurrent || first.ReviewStatus != "pending" {
		t.Fatalf("unreviewed first candidate was promoted: %+v", first)
	}
	db.First(&dialogue, dialogue.ID)
	if dialogue.AudioFile != "" || dialogue.Status != "pending" {
		t.Fatalf("unreviewed candidate changed production audio: %+v", dialogue)
	}

	db.Model(&dialogue).Updates(map[string]any{"status": "synthesizing", "audio_token": "second"})
	second, err := service.captureDialogueAudio(dialogueAudioCapture{ProjectID: project.ID, DialogueID: dialogue.ID, Token: "second", InputHash: hash, Provider: "aliyun", File: "second.mp3", FileSHA256: strings.Repeat("b", 64), FileSize: 256})
	if err != nil {
		t.Fatal(err)
	}
	if second.IsCurrent {
		t.Fatalf("unreviewed second candidate was promoted: %+v", second)
	}

	if _, err := service.SelectDialogueAudioCandidate(project.ID, dialogue.ID, first.ID, first.InputHash, first.FileSHA256); !errors.Is(err, ErrDialogueAudioReviewConflict) {
		t.Fatalf("unreviewed select err=%v", err)
	}
	reviewed, err := service.ReviewDialogueAudioCandidate(project.ID, dialogue.ID, first.ID, "accepted", "", first.InputHash, first.FileSHA256)
	if err != nil || reviewed.ReviewConfirmation != dialogueAudioReviewConfirmationV1 {
		t.Fatalf("reviewed=%+v err=%v", reviewed, err)
	}
	if _, err := service.SelectDialogueAudioCandidate(project.ID, dialogue.ID, first.ID, "wrong", first.FileSHA256); !errors.Is(err, ErrDialogueAudioReviewConflict) {
		t.Fatalf("wrong CAS err=%v", err)
	}
	selected, err := service.SelectDialogueAudioCandidate(project.ID, dialogue.ID, first.ID, first.InputHash, first.FileSHA256)
	if err != nil {
		t.Fatal(err)
	}
	if !selected.IsCurrent || selected.ReviewStatus != "accepted" {
		t.Fatalf("candidate not accepted: %+v", selected)
	}
	db.First(&dialogue, dialogue.ID)
	if dialogue.AudioFile != "first.wav" || dialogue.PreviousAudioFile != "" {
		t.Fatalf("dialogue projection not switched: %+v", dialogue)
	}
}

func TestListDialogueAudioCandidatesBackfillsCurrentLegacyAudio(t *testing.T) {
	db := safetyDB(t, &models.DialogueAudioCandidate{})
	project := models.Project{Title: "p"}
	db.Create(&project)
	dialogue := models.Dialogue{ProjectID: project.ID, Text: "你好", AudioFile: "legacy.wav", AudioHash: "verified-hash", Status: "ready"}
	db.Create(&dialogue)
	service := &ProjectService{db: db}
	rows, err := service.ListDialogueAudioCandidates(project.ID, dialogue.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Provider != "legacy" || rows[0].File != "legacy.wav" || rows[0].IsCurrent || rows[0].ReviewStatus != "pending" || !rows[0].Stale {
		t.Fatalf("legacy audio was not safely quarantined: %+v", rows)
	}
	rows, err = service.ListDialogueAudioCandidates(project.ID, dialogue.ID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("backfill was not idempotent: rows=%+v err=%v", rows, err)
	}
}

func TestStaleDialogueAudioCandidateCannotBeSelected(t *testing.T) {
	db := safetyDB(t, &models.DialogueAudioCandidate{})
	project := models.Project{Title: "p"}
	db.Create(&project)
	dialogue := models.Dialogue{ProjectID: project.ID, Text: "旧文本", Character: "林夏", SpeechType: "dialogue", Speed: 1, Volume: 1, Status: "ready"}
	db.Create(&dialogue)
	service := &ProjectService{db: db}
	row := models.DialogueAudioCandidate{ProjectID: project.ID, DialogueID: dialogue.ID, InputHash: service.effectiveDialogueAudioHash(dialogue), Provider: "index_tts_rust", File: "old.wav"}
	db.Create(&row)
	updated := "新文本"
	if _, err := service.UpdateDialogueFields(&project, dialogue.ID, DialogueUpdateInput{Text: &updated}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SelectDialogueAudioCandidate(project.ID, dialogue.ID, row.ID, row.InputHash, row.FileSHA256); err == nil {
		t.Fatal("stale candidate was selected")
	}
	db.First(&row, row.ID)
	if !row.Stale {
		t.Fatalf("candidate was not marked stale: %+v", row)
	}
}
