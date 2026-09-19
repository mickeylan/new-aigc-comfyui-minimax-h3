package screenplay

import (
	"strings"
	"testing"
)

func assertCoreRoundTrip(t *testing.T, got Preview) {
	t.Helper()
	if len(got.Scenes) != 1 || got.Scenes[0].Heading != "INT. KITCHEN - NIGHT" {
		t.Fatalf("scene lost: %+v", got.Scenes)
	}
	want := []string{"action", "character", "parenthetical", "dialogue", "transition"}
	if len(got.Scenes[0].Elements) != len(want) {
		t.Fatalf("elements = %+v", got.Scenes[0].Elements)
	}
	for i, typ := range want {
		if got.Scenes[0].Elements[i].Type != typ {
			t.Fatalf("element %d type = %q", i, got.Scenes[0].Elements[i].Type)
		}
	}
}

func TestFountainRoundTrip(t *testing.T) {
	input := "Title: Test\n\nINT. KITCHEN - NIGHT\n\nRain hits the window.\n\nALICE\n(quietly)\nWe should go.\n\n>CUT TO:\n"
	preview, err := ParseFountain(input)
	if err != nil {
		t.Fatal(err)
	}
	assertCoreRoundTrip(t, preview)
	roundTrip, err := ParseFountain(WriteFountain(preview))
	if err != nil {
		t.Fatal(err)
	}
	assertCoreRoundTrip(t, roundTrip)
	if roundTrip.Title != "Test" {
		t.Fatalf("title = %q", roundTrip.Title)
	}
}

func TestFDXRoundTrip(t *testing.T) {
	original := Preview{Format: FormatFDX, Scenes: []Scene{{Heading: "INT. KITCHEN - NIGHT", Elements: []Element{
		{Type: "action", Text: "Rain hits the window."}, {Type: "character", Text: "ALICE"},
		{Type: "parenthetical", Text: "quietly"}, {Type: "dialogue", Text: "We should go."}, {Type: "transition", Text: "CUT TO:"},
	}}}}
	encoded, err := WriteFDX(original)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(encoded, `<Paragraph Type="Scene Heading">`) {
		t.Fatalf("unexpected FDX: %s", encoded)
	}
	parsed, err := ParseFDX(encoded)
	if err != nil {
		t.Fatal(err)
	}
	assertCoreRoundTrip(t, parsed)
}
