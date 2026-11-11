package service

import (
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

func TestApplyH3DialogueMarkersPlacesSpeechAtDirectorBeat(t *testing.T) {
	refs := []string{"- <Picture 1>：角色「柳石」四视图", "- <Picture 2>：角色「柳乐儿」四视图", "- <Picture 3>：角色「燕承」四视图"}
	line := "好，既然阁下非要与那妖女为伍，那么……动手！"
	dubs := []models.Dialogue{{ID: 1, Order: 1, Character: "燕承", SpeechType: "dialogue", Text: line}}
	// The earlier compiler pass appended the line at the Shot end; the marker wins.
	body := "[Shot 1] <Subject 1> stands in front of <Subject 2>.\n[Shot 2] At 00:02.500, <Subject 3> glares at <Subject 1> and shouts. [[D1]]. As the last word lands, white light surges along the blade. <Subject 3> (S1) says: <d>[Chinese] " + line + "</d>.\n[Shot 3] At 00:06.500, <Subject 3> flings a black light."
	got, ok := applyH3DialogueMarkers(body, dubs, refs)
	if !ok {
		t.Fatalf("valid marker set rejected: %s", got)
	}
	clause := "<Subject 3> (S1) says: <d>[Chinese] " + line + "</d>."
	if strings.Count(got, "<d>") != 1 || strings.Contains(got, "[[D") {
		t.Fatalf("speech must appear exactly once and markers must be gone: %s", got)
	}
	shot2 := got[strings.Index(got, "[Shot 2]"):strings.Index(got, "[Shot 3]")]
	speech, light := strings.Index(shot2, clause), strings.Index(shot2, "white light")
	if speech < 0 || light < speech || strings.Index(shot2, "shouts.") > speech {
		t.Fatalf("speech not placed between speaking beat and simultaneous action: %s", got)
	}
}

func TestApplyH3DialogueMarkersRejectsIncompleteOrReorderedSets(t *testing.T) {
	dubs := []models.Dialogue{{Character: "甲", SpeechType: "dialogue", Text: "第一句"}, {Character: "乙", SpeechType: "dialogue", Text: "第二句"}}
	for _, body := range []string{
		"[Shot 1] A speaks. [[D1]] B waits.",
		"[Shot 1] B speaks. [[D2]] A speaks. [[D1]]",
		"[Shot 1] A speaks. [[D1]] A again. [[D1]]",
	} {
		got, ok := applyH3DialogueMarkers(body, dubs, nil)
		if ok || strings.Contains(got, "[[D") || strings.Contains(got, "<d>") {
			t.Fatalf("invalid marker set must be stripped without speech: ok=%v got=%s", ok, got)
		}
	}
	if got, ok := applyH3DialogueMarkers("[Shot 1] No markers.", dubs, nil); ok || got != "[Shot 1] No markers." {
		t.Fatalf("marker-free body must be untouched: %v %s", ok, got)
	}
}
