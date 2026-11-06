package service

import (
	"testing"

	"comfyui-console/internal/models"
)

func TestBuildDialoguePreservationReportExactDuplicates(t *testing.T) {
	before := []models.Dialogue{{Text: "Wait!", Character: "A"}, {Text: "Wait!", Character: "B"}, {Text: "Go?", Character: "C"}}
	after := []models.Dialogue{{Text: "Wait!", Character: "A"}, {Text: "Go?", Character: "D"}, {Text: "Wait!", Character: "B"}}
	r := BuildDialoguePreservationReport(before, after)
	if len(r.Missing) != 0 || len(r.Reordered) != 1 || r.Reordered[0].Character != "B" {
		t.Fatalf("unexpected preservation report: %+v", r)
	}
	if len(r.SpeakerChanges) != 1 || r.SpeakerChanges[0].Text != "Go?" || r.SpeakerChanges[0].After != "D" {
		t.Fatalf("speaker change not detected exactly: %+v", r.SpeakerChanges)
	}
}

func TestBuildDialoguePreservationReportDoesNotCanonicalizePunctuation(t *testing.T) {
	r := BuildDialoguePreservationReport([]models.Dialogue{{Text: "No!", Character: "A"}}, []models.Dialogue{{Text: "No", Character: "A"}})
	if len(r.Missing) != 1 || r.Missing[0].Text != "No!" {
		t.Fatalf("punctuation-changing dialogue must be reported missing: %+v", r)
	}
}
