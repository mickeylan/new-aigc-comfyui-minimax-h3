package service

import (
	"testing"

	"comfyui-console/internal/models"
)

func TestCaptureAndSelectDialogueAudioCandidates(t *testing.T) {
	db := safetyDB(t, &models.DialogueAudioCandidate{})
	project := models.Project{Title: "p"}
	db.Create(&project)
	dialogue := models.Dialogue{ProjectID: project.ID, Text: "你好", Character: "林夏", SpeechType: "dialogue", Speed: 1, Volume: 1, Status: "synthesizing", AudioToken: "first"}
	db.Create(&dialogue)
	service := &ProjectService{db: db}
	hash := service.effectiveDialogueAudioHash(dialogue)
	first, err := service.captureDialogueAudio(dialogueAudioCapture{ProjectID: project.ID, DialogueID: dialogue.ID, Token: "first", InputHash: hash, Provider: "index_tts_rust", File: "first.wav", SampleRate: 22050, Channels: 1, Duration: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !first.IsCurrent || first.ReviewStatus != "pending" {
		t.Fatalf("unexpected first candidate: %+v", first)
	}

	db.Model(&dialogue).Updates(map[string]any{"status": "synthesizing", "audio_token": "second"})
	second, err := service.captureDialogueAudio(dialogueAudioCapture{ProjectID: project.ID, DialogueID: dialogue.ID, Token: "second", InputHash: hash, Provider: "aliyun", File: "second.mp3"})
	if err != nil {
		t.Fatal(err)
	}
	if !second.IsCurrent {
		t.Fatalf("second candidate is not current: %+v", second)
	}

	selected, err := service.SelectDialogueAudioCandidate(project.ID, dialogue.ID, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !selected.IsCurrent || selected.ReviewStatus != "accepted" {
		t.Fatalf("candidate not accepted: %+v", selected)
	}
	db.First(&dialogue, dialogue.ID)
	if dialogue.AudioFile != "first.wav" || dialogue.PreviousAudioFile != "second.mp3" {
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
	if len(rows) != 1 || rows[0].Provider != "legacy" || rows[0].File != "legacy.wav" || !rows[0].IsCurrent || rows[0].ReviewStatus != "accepted" {
		t.Fatalf("legacy current audio not backfilled: %+v", rows)
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
	if _, err := service.SelectDialogueAudioCandidate(project.ID, dialogue.ID, row.ID); err == nil {
		t.Fatal("stale candidate was selected")
	}
	db.First(&row, row.ID)
	if !row.Stale {
		t.Fatalf("candidate was not marked stale: %+v", row)
	}
}
