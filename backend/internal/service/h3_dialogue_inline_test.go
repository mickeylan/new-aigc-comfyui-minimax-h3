package service

import (
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

func TestApplyH3DialoguePlanInsertsAfterVisualBeat(t *testing.T) {
	visual := "[Shot 1] He finds the disturbance. He springs up, runs to the cart, and whisks his companions away."
	dubs := []models.Dialogue{{Character: "A", Text: "快走！"}}
	plan := h3DialoguePlan{Placements: []h3DialoguePlanPlacement{{Index: 0, Shot: 1, After: "He finds the disturbance."}}}
	got := applyH3DialoguePlan(visual, dubs, []string{"角色「A」"}, plan)
	speech := strings.Index(got, "<d>[Chinese] 快走！</d>")
	if speech < strings.Index(got, "disturbance.") || speech > strings.Index(got, "He springs up") {
		t.Fatalf("dialogue was not inserted between the planned beats: %s", got)
	}
}

func TestApplyH3DialoguePlanPreservesOrderAtSameAnchor(t *testing.T) {
	visual := "[Shot 1] A door opens. Everyone turns."
	dubs := []models.Dialogue{{Character: "A", Text: "一。"}, {Character: "B", Text: "二。"}}
	plan := h3DialoguePlan{Placements: []h3DialoguePlanPlacement{
		{Index: 0, Shot: 1, After: "A door opens."},
		{Index: 1, Shot: 1, After: "A door opens."},
	}}
	got := applyH3DialoguePlan(visual, dubs, []string{"角色「A」", "角色「B」"}, plan)
	if strings.Index(got, "一。") >= strings.Index(got, "二。") || strings.Index(got, "二。") >= strings.Index(got, "Everyone turns") {
		t.Fatalf("same-anchor source order was not preserved: %s", got)
	}
}

func TestValidateH3DialoguePlanRejectsInvalidOrAmbiguousAnchor(t *testing.T) {
	dubs := []models.Dialogue{{Character: "A", Text: "一句。"}}
	shots := map[int]bool{1: true}
	for _, after := range []string{"missing", "beat"} {
		visual := "[Shot 1] beat once."
		if after == "beat" {
			visual = "[Shot 1] beat, then beat."
		}
		plan := h3DialoguePlan{Placements: []h3DialoguePlanPlacement{{Index: 0, Shot: 1, After: after}}}
		if err := validateH3DialoguePlan(plan, dubs, visual, shots, nil); err == nil {
			t.Fatalf("anchor %q unexpectedly accepted", after)
		}
	}
}
