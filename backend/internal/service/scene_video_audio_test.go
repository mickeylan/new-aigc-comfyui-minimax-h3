package service

import (
	"testing"

	"comfyui-console/internal/models"
)

func TestSceneReviewedAudioSelectionRequiresAcceptedCurrentMatchingCandidate(t *testing.T) {
	svc := newTestProjectService(t)
	db := svc.db
	if err := db.AutoMigrate(&models.Dialogue{}, &models.DialogueAudioCandidate{}); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "audio-scene"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Generation: 1, Order: 1}
	if err := db.Create(&scene).Error; err != nil {
		t.Fatal(err)
	}
	dialogue := models.Dialogue{ProjectID: project.ID, SceneID: scene.ID, Order: 1, Character: "甲", SpeechType: "dialogue", Text: "原文", AudioFile: "take.flac", AudioHash: "hash", Status: "ready"}
	if err := db.Create(&dialogue).Error; err != nil {
		t.Fatal(err)
	}
	if got := svc.sceneReviewedAudioSelection(&scene); got.Ready {
		t.Fatal("unreviewed audio became eligible")
	}
	candidate := models.DialogueAudioCandidate{ProjectID: project.ID, DialogueID: dialogue.ID, InputHash: "hash", Provider: "test", File: "take.flac", ReviewStatus: "accepted", IsCurrent: true}
	if err := db.Create(&candidate).Error; err != nil {
		t.Fatal(err)
	}
	got := svc.sceneReviewedAudioSelection(&scene)
	if !got.Ready || got.Candidate == nil || got.Candidate.ID != candidate.ID {
		t.Fatalf("selection=%+v", got)
	}
	files, err := svc.attachReviewedSceneAudio(&scene, nil)
	if err != nil {
		t.Fatal(err)
	}
	if files["dialogue_audio"][0].Name != "take.flac" {
		t.Fatalf("files=%+v", files)
	}
}

func TestSceneReviewedAudioSelectionRejectsMultipleDialogueAutoConcatenation(t *testing.T) {
	svc := newTestProjectService(t)
	db := svc.db
	if err := db.AutoMigrate(&models.Dialogue{}, &models.DialogueAudioCandidate{}); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "multi-audio-scene"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Generation: 1, Order: 1}
	if err := db.Create(&scene).Error; err != nil {
		t.Fatal(err)
	}
	for i, text := range []string{"甲说", "乙说"} {
		d := models.Dialogue{ProjectID: project.ID, SceneID: scene.ID, Order: i + 1, Character: "角色", SpeechType: "dialogue", Text: text}
		if err := db.Create(&d).Error; err != nil {
			t.Fatal(err)
		}
	}
	got := svc.sceneReviewedAudioSelection(&scene)
	if got.Ready || got.Reason == "" {
		t.Fatalf("selection=%+v", got)
	}
}

func TestReviewedAudioTemplateClassification(t *testing.T) {
	for _, code := range []string{h3AudioDrivenRef2VTemplate, h3AudioDrivenT8EnhancedTemplate, h3VideoThenAudioMuxTemplate} {
		if !isReviewedAudioVideoTemplate(code) {
			t.Fatalf("%s not classified", code)
		}
	}
	if isReviewedAudioVideoTemplate("minimax_h3_ref2v") {
		t.Fatal("ordinary template classified as reviewed audio")
	}
}
