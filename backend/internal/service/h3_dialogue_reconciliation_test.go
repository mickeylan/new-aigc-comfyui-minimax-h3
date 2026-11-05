package service

import (
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

func TestReconcileExistingH3DialogueKeepsIndependentSubjectAndSpeakerIDs(t *testing.T) {
	dubs := []models.Dialogue{
		{Character: "甲", Text: "甲的原话。"},
		{Character: "乙", Text: "乙的原话。"},
	}
	refs := []string{"角色「乙」", "角色「旁人」", "角色「甲」"}
	original := "[Shot 1] <Subject 3> 甲 (S1) says: <d>[Chinese] 甲的原话。</d>.\n[Shot 2] <Subject 1> 乙 (S2) says: <d>[Chinese] 乙的原话。</d>."
	cleaned := "[Shot 1] action.\n[Shot 2] reaction."

	got, remaining := reconcileExistingH3Dialogue(original, cleaned, dubs, refs)
	if len(remaining) != 0 || !strings.Contains(got, "[Shot 1] action. 甲 (S1)") || !strings.Contains(got, "[Shot 2] reaction. 乙 (S2)") {
		t.Fatalf("valid independent bindings not retained: %q remaining=%+v", got, remaining)
	}
}

func TestReconcileExistingH3DialogueRejectsKnownWrongSubject(t *testing.T) {
	dubs := []models.Dialogue{{Character: "甲", Text: "不能错绑。"}}
	refs := []string{"角色「乙」", "角色「甲」"}
	original := "[Shot 1] <Subject 2> 甲 (S1) says: <d>[Chinese] 不能错绑。</d>.\n[Shot 2] <Subject 2> 甲 (S1) says: <d>[Chinese] 不能错绑。</d>."
	// Make only the first occurrence wrong while retaining a later valid occurrence.
	original = strings.Replace(original, "<Subject 2> 甲", "<Subject 1> 甲", 1)
	got, remaining := reconcileExistingH3Dialogue(original, "[Shot 1] action.\n[Shot 2] reaction.", dubs, refs)
	if len(remaining) != 0 || strings.Contains(got, "[Shot 1] action. 甲 (S1)") || !strings.Contains(got, "[Shot 2] reaction. 甲 (S1)") {
		t.Fatalf("wrong subject retained or valid occurrence lost: %q remaining=%+v", got, remaining)
	}
}

func TestReconcileExistingH3DialogueKeepsSecondAfterMissingFirst(t *testing.T) {
	dubs := []models.Dialogue{{Character: "甲", Text: "缺失。"}, {Character: "乙", Text: "仍在第二镜。"}}
	original := "[Shot 1] action.\n[Shot 2] <Subject 1> 乙 (S2) says: <d>[Chinese] 仍在第二镜。</d>."
	got, remaining := reconcileExistingH3Dialogue(original, "[Shot 1] action.\n[Shot 2] reaction.", dubs, []string{"角色「乙」"})
	if len(remaining) != 1 || remaining[0].Character != "甲" || !strings.Contains(got, "乙 (S2) says: <d>[Chinese] 仍在第二镜。</d>") {
		t.Fatalf("second dialogue did not round trip: %q remaining=%+v", got, remaining)
	}
}

func TestReconcileExistingH3DialogueRetainsRepeatedQuoteOccurrences(t *testing.T) {
	dubs := []models.Dialogue{{Character: "甲", Text: "一样。"}, {Character: "甲", Text: "一样。"}}
	original := "[Shot 1] 甲 (S1) says: <d>[Chinese] 一样。</d>. 甲 (S1) says: <d>[Chinese] 一样。</d>."
	got, remaining := reconcileExistingH3Dialogue(original, "[Shot 1] action.", dubs, nil)
	if len(remaining) != 0 || strings.Count(got, "<d>[Chinese] 一样。</d>") != 2 {
		t.Fatalf("repeated occurrence suppressed: %q remaining=%+v", got, remaining)
	}
}
