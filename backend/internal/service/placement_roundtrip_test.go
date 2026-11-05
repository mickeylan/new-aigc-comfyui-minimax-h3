package service

import (
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

func TestNormalizeSavedH3AudioPreservesAuthoritativeDialogueShot(t *testing.T) {
	dubs := []models.Dialogue{{ID: 7, Character: "甲", SpeechType: "dialogue", Text: "留在第二镜。"}}
	prompt := "[Shot 1] wide.\n[Shot 2] At 00:04.000, close. 甲 (S1) says: <d>[Chinese] 留在第二镜。</d>."
	got := normalizeSavedH3Audio(prompt, dubs, nil)
	detail := h3PromptSection(got, "detailed_description:")
	cut := strings.Index(detail, "[Shot 2]")
	if cut < 0 || strings.Contains(detail[:cut], dubs[0].Text) || !strings.Contains(detail[cut:], "甲 (S1) says: <d>[Chinese] 留在第二镜。</d>") {
		t.Fatalf("authoritative placement moved: %s", got)
	}
	if strings.Count(got, dubs[0].Text) != 1 {
		t.Fatalf("dialogue duplicated: %s", got)
	}
}

func TestNormalizeSavedH3AudioRejectsWrongBindingWithoutInventingMultiShotPlacement(t *testing.T) {
	dubs := []models.Dialogue{{ID: 8, Character: "乙", SpeechType: "dialogue", Text: "不能错绑。"}}
	prompt := "[Shot 1] wide. 甲 (S1) says: <d>[Chinese] 不能错绑。</d>.\n[Shot 2] close."
	got := normalizeSavedH3Audio(prompt, dubs, nil)
	if strings.Contains(got, "<d>") || strings.Contains(got, dubs[0].Text) {
		t.Fatalf("wrong binding was retained or relocated: %s", got)
	}
}

func TestAppendStructuredDialogueIsIdempotentForExistingBoundClause(t *testing.T) {
	dubs := []models.Dialogue{{Character: "甲", Text: "只出现一次。"}}
	body := "[Shot 1] action. 甲 (S1) says: <d>[Chinese] 只出现一次。</d>."
	if got := appendStructuredDialogue(body, dubs, nil); got != body {
		t.Fatalf("existing clause changed:\n%s\n---\n%s", body, got)
	}
}
