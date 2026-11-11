package service

import (
	"regexp"
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

const h3BriefTestPrompt = "subject_definitions:\n<Subject 1> is 「柳石」 from <Picture 1>.\n<Subject 2> is 「燕承」 from <Picture 2>.\n\nsummary:\n[reference generation] Generate a 15-second video from the defined references and shot timeline as one continuous full-frame scene.\n\nretention_analysis:\n<Subject 1> (appears in [Shot 1]): fully_preserved - identity and visible appearance.\n\ndetailed_description:\n[Shot 1] <Subject 2> flings a black net over <Subject 1>.\n\noverall_soundscape:\nambience\n\nnon_diegetic_music:\nN/A"

func TestApplyH3DirectorBriefReplacesSummaryAndAppendsContinuity(t *testing.T) {
	ps := &ProjectService{textProvider: &captureTextProvider{response: "```json\n{\"summary\":\"[reference generation] Generate one continuous 15-second confrontation. <Subject 2> flings a black net that drops over <Subject 1>, and the video ends before any strike lands.\",\"retention\":[\"<Subject 2> stays on screen-left facing right; <Subject 1> stays on screen-right facing left.\",\"The black net originates only from <Subject 2>'s left hand.\"]}\n```"}}
	got := ps.applyH3DirectorBrief(&models.Scene{Duration: 15}, h3BriefTestPrompt)
	summary := h3PromptSection(got, "summary:")
	if !strings.Contains(summary, "ends before any strike lands") || strings.Contains(summary, "defined references and shot timeline") {
		t.Fatalf("summary not replaced: %s", got)
	}
	retention := h3PromptSection(got, "retention_analysis:")
	for _, want := range []string{"fully_preserved - identity and visible appearance", "screen-left facing right", "originates only from <Subject 2>"} {
		if !strings.Contains(retention, want) {
			t.Fatalf("retention missing %q: %s", want, got)
		}
	}
	if len(validateGeneratedH3Prompt(got, "minimax_h3_ref2v", 15)) != len(validateGeneratedH3Prompt(h3BriefTestPrompt, "minimax_h3_ref2v", 15)) {
		t.Fatalf("brief changed prompt structure validity: %s", got)
	}
}

// Real models wrote "Subject 2" and "Subjects 5 and 1" without brackets, which
// H3 cannot bind; such references are restored to <Subject N> tags.
func TestApplyH3DirectorBriefRestoresBareSubjectReferences(t *testing.T) {
	ps := &ProjectService{textProvider: &captureTextProvider{response: `{"summary":"[reference generation] Subject 2 stays at frame left while Subject 1 stands firm; the net encloses Subjects 1 and 2 before any strike lands.","retention":["The black net moves over Subjects 1, 2 and 1 without Subject 1 retreating."]}`}}
	got := ps.applyH3DirectorBrief(&models.Scene{Duration: 15}, h3BriefTestPrompt)
	summary, retention := h3PromptSection(got, "summary:"), h3PromptSection(got, "retention_analysis:")
	if !strings.Contains(summary, "<Subject 2> stays at frame left while <Subject 1> stands firm") || !strings.Contains(summary, "encloses <Subject 1> and <Subject 2> before") {
		t.Fatalf("summary bare subjects not tagged: %s", summary)
	}
	if !strings.Contains(retention, "over <Subject 1>, <Subject 2> and <Subject 1> without <Subject 1> retreating") {
		t.Fatalf("retention bare subjects not tagged: %s", retention)
	}
	if regexp.MustCompile(`(^|[^<])Subjects? [0-9]`).MatchString(summary + "\n" + retention) {
		t.Fatalf("bare subject reference left: %s", got)
	}
	bare := &ProjectService{textProvider: &captureTextProvider{response: `{"summary":"[reference generation] Subject 3 appears and throws a net over everyone in the frame quickly.","retention":[]}`}}
	if out := bare.applyH3DirectorBrief(&models.Scene{Duration: 15}, h3BriefTestPrompt); out != h3BriefTestPrompt {
		t.Fatalf("undefined bare subject must keep the template: %s", out)
	}
}

func TestApplyH3DirectorBriefFallsBackToTemplateOnUnsafeOutput(t *testing.T) {
	for _, response := range []string{
		"not json",
		`{"summary":"[reference generation] 柳石挡在前面，燕承撒网罩住两人并且银钩闪烁。","retention":[]}`,
		`{"summary":"[reference generation] <Subject 3> appears and throws a net over everyone in the frame quickly.","retention":[]}`,
		`{"summary":"[reference generation] <Subject 2> shouts <d>[Chinese] 动手</d> and flings the black net down.","retention":[]}`,
	} {
		ps := &ProjectService{textProvider: &captureTextProvider{response: response}}
		if got := ps.applyH3DirectorBrief(&models.Scene{Duration: 15}, h3BriefTestPrompt); got != h3BriefTestPrompt {
			t.Fatalf("unsafe brief %q must keep the template: %s", response, got)
		}
	}
}
