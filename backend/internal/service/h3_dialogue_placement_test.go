package service

import (
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

func TestResolveH3DialoguePlacementUsesAuthoritativeTimeSegments(t *testing.T) {
	body := "[Shot 1] [0.00s] one\n[Shot 2] At 00:03.000, two\n[Shot 3] At 00:07.000, three\n[Shot 4] At 00:11.000, four"
	dubs := []models.Dialogue{
		{ID: 1, Character: "甲", Text: "第一句", TimeSegment: "00:01.000-00:02.000"},
		{ID: 2, Character: "乙", Text: "第二句", TimeSegment: "00:04.000-00:05.000"},
		{ID: 3, Character: "丙", Text: "第三句", TimeSegment: "8-10秒"},
	}
	got, report := ResolveH3DialoguePlacement(body, dubs, nil, make([]models.Shot, 4))
	if len(report.Unplaced) != 0 {
		t.Fatalf("unexpected advisory: %+v", report)
	}
	for i, text := range []string{"第一句", "第二句", "第三句"} {
		start := strings.Index(got, "[Shot "+string(rune('1'+i))+">")
		_ = start
		if strings.Count(got, text) != 1 {
			t.Fatalf("dialogue %q not preserved once: %s", text, got)
		}
	}
	if !(strings.Index(got, "第一句") < strings.Index(got, "[Shot 2]") && strings.Index(got, "第二句") < strings.Index(got, "[Shot 3]") && strings.Index(got, "第三句") < strings.Index(got, "[Shot 4]")) {
		t.Fatalf("time placement incorrect: %s", got)
	}
}

func TestResolveH3DialoguePlacementUnmappedIsVerbatimAndAdvisory(t *testing.T) {
	body := "unknown original clause\n[Shot 2] reaction\n[Shot 7] ending"
	dubs := []models.Dialogue{{ID: 1, Character: "旁白", SpeechType: "narration", Text: "重复"}, {ID: 2, Character: "甲", Text: "重复"}}
	got, report := ResolveH3DialoguePlacement(body, dubs, nil, nil)
	if got != body {
		t.Fatalf("unmapped prompt changed:\n%q\n%q", body, got)
	}
	if len(report.Unplaced) != 2 {
		t.Fatalf("expected occurrence-specific advisories: %+v", report)
	}
	if strings.HasPrefix(got, "旁白") || strings.Contains(got, "<d>") {
		t.Fatalf("unmapped narration/dialogue was guessed: %s", got)
	}
}

func TestAppendExplicitDialogueInvalidRangesReturnsOriginal(t *testing.T) {
	body := "[Shot 1] unknown original clause where her lips move while listening."
	dubs := []models.Dialogue{{ID: 4, Character: "甲", Text: "保留原话"}}
	shots := []models.Shot{{Order: 1, DialogueRanges: []models.ShotDialogueRange{{DialogueID: 4, StartRune: 0, EndRune: 99}}}}
	got, ok := appendExplicitDialogueRangesToShots(body, dubs, nil, shots)
	if ok || got != body {
		t.Fatalf("invalid ranges accepted or changed original: ok=%v body=%q", ok, got)
	}
}

func TestResolveH3DialoguePlacementKeepsExistingSpeakerOccurrence(t *testing.T) {
	body := "[Shot 1] 甲 (S1) says: <d>[Chinese] 相同台词</d>.\n[Shot 2] At 00:03.000, reaction."
	dubs := []models.Dialogue{{ID: 1, Character: "甲", Text: "相同台词"}, {ID: 2, Character: "乙", Text: "相同台词"}}
	got, report := ResolveH3DialoguePlacement(body, dubs, nil, nil)
	if got != body || len(report.Unplaced) != 1 || report.Unplaced[0].Character != "乙" {
		t.Fatalf("speaker/occurrence comparison failed: %s %+v", got, report)
	}
}

func TestDiagnoseH3DialoguePlacementAssemblesConsecutiveCrossShotFragments(t *testing.T) {
	body := "[Shot 1] 甲 (S1) says: <d>旧</d>.\n[Shot 2] 甲 (S1) says: <d>对白</d>."
	dubs := []models.Dialogue{{ID: 9, Character: "甲", Text: "旧对白"}}
	report := DiagnoseH3DialoguePlacement(body, dubs, nil)
	if len(report.Placed) != 1 || report.Placed[0].DialogueID != 9 || report.Placed[0].ShotNumber != 1 || len(report.Unplaced) != 0 {
		t.Fatalf("cross-shot fragments not diagnosed as one occurrence: %+v", report)
	}
}

func TestDiagnoseH3DialoguePlacementRejectsUnrelatedOrRepeatedShotFragments(t *testing.T) {
	dubs := []models.Dialogue{{ID: 9, Character: "甲", Text: "旧对白"}}
	for _, body := range []string{
		"[Shot 1] 甲 (S1) says: <d>旧</d>.\n[Shot 2] 乙 (S2) says: <d>对白</d>.",
		"[Shot 1] 甲 (S1) says: <d>旧</d>.\n[Shot 1] 甲 (S1) says: <d>对白</d>.",
	} {
		report := DiagnoseH3DialoguePlacement(body, dubs, nil)
		if len(report.Placed) != 0 || len(report.Unplaced) != 1 {
			t.Fatalf("unreliable fragments were assembled: %+v", report)
		}
	}
}
