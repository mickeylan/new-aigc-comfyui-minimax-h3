package service

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildDialogueExportGraphUsesEpisodeTimeline(t *testing.T) {
	graph := buildDialogueExportGraph([]dubSegment{
		{Start: 0.4, End: 2.4, SourceDur: 3, Speed: 1.5, Volume: 0.8},
		{Start: 4.25, End: 5.25, SourceDur: 1, Speed: 1, Pitch: 2, Volume: 1},
	}, 8)
	for _, want := range []string{
		"[0:a]atrim=start=0:end=3.000",
		"atempo=1.500,volume=0.800,adelay=400:all=1,apad,atrim=duration=8.000",
		"[1:a]atrim=start=0:end=1.000",
		"asetrate=53878.178,aresample=48000,atempo=0.891,volume=1.000,adelay=4250:all=1",
		"amix=inputs=2:duration=longest:dropout_transition=0:normalize=0",
	} {
		if !strings.Contains(graph.Filters, want) {
			t.Fatalf("dialogue graph missing %q:\n%s", want, graph.Filters)
		}
	}
	if graph.Label != "[amix]" {
		t.Fatalf("audio map label = %q", graph.Label)
	}
}

func TestSelectDialogueStemsSeparatesCharactersAndKeepsTimelineOrder(t *testing.T) {
	segments := []dubSegment{
		{DialogueID: 1, Character: " Bob ", Start: 0.4},
		{DialogueID: 2, Character: "Alice", Start: 2},
		{DialogueID: 3, Character: "Bob", Start: 4},
		{DialogueID: 4, Character: "", Start: 6},
	}
	got := selectDialogueStems(segments)
	if len(got) != 3 {
		t.Fatalf("selection count = %d: %+v", len(got), got)
	}
	if got[0].Character != "" || len(got[0].Segments) != 1 || got[0].Segments[0].DialogueID != 4 {
		t.Fatalf("narration selection = %+v", got[0])
	}
	if got[1].Character != "Alice" || len(got[1].Segments) != 1 || got[1].Segments[0].DialogueID != 2 {
		t.Fatalf("Alice selection = %+v", got[1])
	}
	if got[2].Character != "Bob" || len(got[2].Segments) != 2 || got[2].Segments[0].DialogueID != 1 || got[2].Segments[1].DialogueID != 3 {
		t.Fatalf("Bob selection = %+v", got[2])
	}
}

func TestPathInsideRejectsTraversalAndRoot(t *testing.T) {
	root := t.TempDir()
	if !pathInside(root, filepath.Join(root, "voice.wav")) {
		t.Fatal("expected child path to be accepted")
	}
	if pathInside(root, root) || pathInside(root, filepath.Join(root, "..", "other.wav")) {
		t.Fatal("root or traversal path was accepted")
	}
}
