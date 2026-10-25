package service

import (
	"testing"

	"comfyui-console/internal/models"
)

func TestInvalidateNarratorDialogueAudioCoversLegacyNarratorRowsAndClearsTaskClaim(t *testing.T) {
	db := safetyDB(t, &models.Dialogue{}, &models.DialogueAudioCandidate{})
	rows := []models.Dialogue{
		{ProjectID: 1, SceneID: 1, Character: "旁白", SpeechType: "dialogue", Text: "旧数据", Status: "synthesizing", AudioTaskID: "old-task", AudioTaskHash: "old-hash", AudioToken: "old-token"},
		{ProjectID: 1, SceneID: 1, Character: "", SpeechType: "narration", Text: "规范旁白", Status: "ready", AudioTaskID: "done-task"},
		{ProjectID: 1, SceneID: 1, Character: "角色甲", SpeechType: "dialogue", Text: "对白", Status: "ready"},
	}
	for i := range rows {
		if err := db.Create(&rows[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, dialogue := range rows[:2] {
		candidate := models.DialogueAudioCandidate{ProjectID: 1, DialogueID: dialogue.ID, InputHash: "old", Provider: "test", File: "old.wav", ReviewStatus: "pending"}
		if err := db.Create(&candidate).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := invalidateNarratorDialogueAudio(db, 1, "旁白音色已变化"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []uint{rows[0].ID, rows[1].ID} {
		var got models.Dialogue
		if err := db.First(&got, id).Error; err != nil {
			t.Fatal(err)
		}
		if got.Status != "pending" || !got.AudioStale || got.AudioToken != "" || got.AudioTaskID != "" || got.AudioTaskHash != "" {
			t.Fatalf("narrator dialogue was not reset for immediate regeneration: %+v", got)
		}
		var candidate models.DialogueAudioCandidate
		if err := db.Where("dialogue_id = ?", id).First(&candidate).Error; err != nil {
			t.Fatal(err)
		}
		if !candidate.Stale || candidate.IsCurrent {
			t.Fatalf("old candidate remains usable: %+v", candidate)
		}
	}
	var ordinary models.Dialogue
	if err := db.First(&ordinary, rows[2].ID).Error; err != nil {
		t.Fatal(err)
	}
	if ordinary.Status != "ready" || ordinary.AudioStale {
		t.Fatalf("ordinary character dialogue was invalidated: %+v", ordinary)
	}
}
