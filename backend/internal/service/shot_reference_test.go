package service

import (
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

// Saved director Shots are a reference: a prompt whose director merged or added
// cuts keeps its own structure, timing and speech placement.
func TestSavedShotsAreReferenceWhenPromptChangesCutPlan(t *testing.T) {
	shots := []models.Shot{{Order: 1, Duration: 4, Description: "第一镜"}, {Order: 2, Duration: 4, Description: "第二镜"}, {Order: 3, Duration: 4, Description: "第三镜"}}
	merged := "[Shot 1] Wide two-shot.\n[Shot 2] At 00:06.500, close-up reaction."
	if got := ensureStructuredShotMarkers(merged, shots, false); got != merged {
		t.Fatalf("director-merged cut plan was rebuilt from saved Shots: %s", got)
	}
	if got := applyShotTimeline(merged, shots, 12); got != merged {
		t.Fatalf("saved Shot timing overwrote the director's own cut time: %s", got)
	}
	added := "[Shot 1] A.\n[Shot 2] At 00:03.000, B.\n[Shot 3] At 00:06.000, C.\n[Shot 4] At 00:09.000, D."
	if got := ensureStructuredShotMarkers(added, shots, false); got != added {
		t.Fatalf("director-added cut was rebuilt from saved Shots: %s", got)
	}
}

func TestSavedShotsStillRepairBrokenOrCollapsedStructure(t *testing.T) {
	shots := []models.Shot{{Order: 1, Duration: 4, PromptAction: "first action"}, {Order: 2, Duration: 4, PromptAction: "second action"}, {Order: 3, Duration: 4, PromptAction: "third action"}}
	gap := "[Shot 1] A.\n[Shot 3] At 00:08.000, C."
	if got := ensureStructuredShotMarkers(gap, shots, false); got == gap {
		t.Fatalf("numbering gap was kept: %s", got)
	}
	if got := ensureStructuredShotMarkers("[Shot 1] collapsed.", shots, true); !strings.Contains(got, "second action") {
		t.Fatalf("collapsed duplicate Shots were not rebuilt from the plan: %s", got)
	}
	matching := "[Shot 1] A.\n[Shot 2] At 00:05.000, B.\n[Shot 3] At 00:09.000, C."
	if got := applyShotTimeline(matching, shots, 12); !strings.Contains(got, "[Shot 2] At 00:04.000,") || !strings.Contains(got, "[Shot 3] At 00:08.000,") {
		t.Fatalf("matching cut plan did not take saved Shot timing: %s", got)
	}
}

func TestDialogueRangesYieldWhenPromptChangesCutPlan(t *testing.T) {
	dubs := []models.Dialogue{{ID: 1, Character: "林夏", SpeechType: "dialogue", Text: "甲乙丙丁"}}
	shots := []models.Shot{
		{Order: 1, DialogueRanges: []models.ShotDialogueRange{{DialogueID: 1, GroupKey: "g", StartRune: 0, EndRune: 2}}},
		{Order: 2, DialogueRanges: []models.ShotDialogueRange{{DialogueID: 1, GroupKey: "g", StartRune: 2, EndRune: 4}}},
		{Order: 3},
	}
	body := "[Shot 1] A.\n[Shot 2] At 00:05.000, B."
	if got, ok := appendExplicitDialogueRangesToShots(body, dubs, nil, shots); ok || got != body {
		t.Fatalf("Shot-order ranges were applied to a different cut plan: ok=%v %s", ok, got)
	}
}
