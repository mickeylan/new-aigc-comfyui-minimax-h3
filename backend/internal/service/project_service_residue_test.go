package service

import (
	"strings"
	"testing"
)

func TestCompactFinalH3VisualBodyRemovesReportedTemplateResidue(t *testing.T) {
	input := `[Shot 1] Close-up of <Subject 1> at MM:SS.mmm, her gaze softening; she says hand releasing from her sister's shoulder to fall naturally at her side. Camera slowly pulls back. Shot dissolves gently into the <Subject 3> remains visible in the background.
[Shot 2] At 00:05.000, <Subject 2> in medium close-up profile at MM:SS.mmm, listening as her sister's voice continues off-screen with ; tears fading. Camera slightly pans and pushes in.`
	got := compactFinalH3VisualBody(input)
	for _, bad := range []string{"MM:SS.mmm", "she says hand", "voice continues off-screen", "Shot dissolves", "dissolve"} {
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

func TestValidateGeneratedH3PromptRejectsVisualTemplateResidue(t *testing.T) {
	prompt := "subject_definitions:\nx\n\nsummary:\nx\n\nretention_analysis:\nx\n\ndetailed_description:\n[Shot 1] <Subject 1> at MM:SS.mmm, she says hand releasing at her side.\n[Shot 2] At 00:05.000, the listener's voice continues off-screen with ; tears fading.\n\noverall_soundscape:\nQuiet room tone.\n\nnon_diegetic_music:\nN/A"
	issues := strings.Join(validateGeneratedH3Prompt(prompt, "minimax_h3_ref2v", 11), "|")
	if !strings.Contains(issues, "模板残片") {
		t.Fatalf("template residue accepted: %s", issues)
	}
}
