package service

import (
	"encoding/json"
	"testing"

	"comfyui-console/internal/models"
)

func TestSceneAudioCandidateResponsesExposeStructuredShotTimingAudit(t *testing.T) {
	snapshot := sceneAudioSnapshot{
		Version: 3, ProjectID: 1, SceneID: 2, SceneDuration: 8,
		FragmentTimingMethod: "dialogue_text_duration_weighted_v1",
		Shots:                []sceneAudioShot{{ID: 3, Order: 1, Duration: 4, Fragments: []sceneAudioFragment{{DialogueID: 4, StartRune: 0, EndRune: 2, AudioStart: 0.4, AudioEnd: 2.2, Text: "原文"}}}},
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	rows := sceneAudioCandidateResponses([]models.SceneAudioCandidate{{ID: 5, SourceJSON: string(raw)}})
	if len(rows) != 1 || rows[0].SourceSnapshot == nil {
		t.Fatalf("rows=%+v", rows)
	}
	got := rows[0].SourceSnapshot
	if got.FragmentTimingMethod != "dialogue_text_duration_weighted_v1" || len(got.Shots) != 1 || len(got.Shots[0].Fragments) != 1 {
		t.Fatalf("snapshot=%+v", got)
	}
	fragment := got.Shots[0].Fragments[0]
	if fragment.Text != "原文" || fragment.AudioStart != 0.4 || fragment.AudioEnd != 2.2 {
		t.Fatalf("fragment=%+v", fragment)
	}
}

func TestSceneAudioReviewRequiresExplicitAuditConfirmation(t *testing.T) {
	if err := validateSceneAudioReviewConfirmation("accepted", false); err == nil {
		t.Fatal("accepted review bypassed confirmation")
	}
	if err := validateSceneAudioReviewConfirmation("accepted", true); err != nil {
		t.Fatal(err)
	}
	if err := validateSceneAudioReviewConfirmation("rejected", false); err != nil {
		t.Fatal(err)
	}
}

func TestSceneAudioCandidateResponsesReportMalformedAudit(t *testing.T) {
	rows := sceneAudioCandidateResponses([]models.SceneAudioCandidate{{ID: 1, SourceJSON: "{"}})
	if rows[0].SourceSnapshot != nil || rows[0].AuditError == "" {
		t.Fatalf("rows=%+v", rows)
	}
}
