package service

import (
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
