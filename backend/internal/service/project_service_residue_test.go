package service

import (
	"strings"
	"testing"
)

func TestCompactFinalH3VisualBodyRemovesReportedTemplateResidue(t *testing.T) {
	input := `[Shot 1] Close-up of <Subject 1> at MM:SS.mmm, her gaze softening; she says hand releasing from her sister's shoulder to fall naturally at her side. Camera slowly pulls back. Shot dissolves gently into the <Subject 3> remains visible in the background.
[Shot 2] At 00:05.000, 0, <Subject 2> in medium close-up profile at MM:SS.mmm, listening as her sister's voice continues off-screen with ; tears fading. Camera slightly pans and pushes in.`
	got := compactFinalH3VisualBody(input)
	for _, bad := range []string{"MM:SS.mmm", "At 00:05.000, 0,", "she says hand", "voice continues off-screen", "Shot dissolves", "dissolve"} {
		if strings.Contains(strings.ToLower(got), strings.ToLower(bad)) {
			t.Fatalf("retained template residue %q: %s", bad, got)
		}
	}
	for _, want := range []string{"her hand releases", "tears fading", "Camera slightly pans and pushes in"} {
		if !strings.Contains(got, want) {
			t.Fatalf("lost visible fact %q: %s", want, got)
		}
	}
}

func TestH3SubjectDefinitionsPreserveExactReferenceIdentity(t *testing.T) {
	definitions, _, _ := h3VideoSubjects([]string{"- <Picture 1>：角色「上官若琳」四视图", "- <Picture 2>：角色「上官若彤」四视图", "- <Picture 3>：场景「天阙宗玉霄宫梵心桃花林」参考图"})
	joined := strings.Join(definitions, "\n")
	for _, want := range []string{"<Subject 1> is the character named 「上官若琳」 shown in the four-view reference from <Picture 1>.", "<Subject 2> is the character named 「上官若彤」 shown in the four-view reference from <Picture 2>.", "<Subject 3> is the referenced environment named 「天阙宗玉霄宫梵心桃花林」 from <Picture 3>."} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing exact identity binding %q: %s", want, joined)
		}
	}
	if strings.Contains(joined, "<Subject 1> is the character shown") || strings.Contains(joined, "<Subject 2> is the character shown") {
		t.Fatalf("anonymous character binding retained: %s", joined)
	}
}

func TestValidateGeneratedH3PromptRejectsVisualTemplateResidue(t *testing.T) {
	prompt := "subject_definitions:\nx\n\nsummary:\nx\n\nretention_analysis:\nx\n\ndetailed_description:\n[Shot 1] <Subject 1> at MM:SS.mmm, she says hand releasing at her side.\n[Shot 2] At 00:05.000, the listener's voice continues off-screen with ; tears fading.\n\noverall_soundscape:\nQuiet room tone.\n\nnon_diegetic_music:\nN/A"
	issues := strings.Join(validateGeneratedH3Prompt(prompt, "minimax_h3_ref2v", 11), "|")
	if !strings.Contains(issues, "模板残片") {
		t.Fatalf("template residue accepted: %s", issues)
	}
}
