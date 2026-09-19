package service

import (
	"strings"
	"testing"
)

func TestBuildMergeAudioGraphMixesNativeDialogueAndLayers(t *testing.T) {
	graph := buildMergeAudioGraph(2, true, 12, 0.75, []mergeAudioInput{
		{Index: 2, Start: 1.25, Duration: 3, Volume: 0.8, FadeIn: 0.5, FadeOut: 1, Kind: "sfx"},
		{Index: 3, Start: 0, Duration: 12, Volume: 0.3, FadeIn: 2, FadeOut: 2, Loop: true, Kind: "bgm"},
		{Index: 4, Start: 4.5, Duration: 2.25, Volume: 1.2, Kind: "dialogue"},
	})
	for _, want := range []string{
		"[0:a][1:a]concat=n=2:v=0:a=1,volume=0.750",
		"[2:a]atrim=start=0:end=3.000",
		"volume=0.800,afade=t=in:st=0:d=0.500,afade=t=out:st=2.000:d=1.000,adelay=1250:all=1",
		"[3:a]atrim=start=0:end=12.000",
		"volume=0.300,afade=t=in:st=0:d=2.000,afade=t=out:st=10.000:d=2.000,adelay=0:all=1",
		"[4:a]atrim=start=0:end=2.250",
		"volume=1.200,adelay=4500:all=1",
		"amix=inputs=4:duration=longest:dropout_transition=0:normalize=0",
		"alimiter=limit=0.95[amix]",
	} {
		if !strings.Contains(graph.Filters, want) {
			t.Fatalf("filter graph missing %q:\n%s", want, graph.Filters)
		}
	}
	if graph.Label != "[amix]" {
		t.Fatalf("audio map label = %q", graph.Label)
	}
}

func TestBuildMergeAudioGraphPreservesLegacyAudioSelection(t *testing.T) {
	withoutAudio := buildMergeAudioGraph(2, false, 10, 1, nil)
	if withoutAudio.Filters != "" || withoutAudio.Label != "" {
		t.Fatalf("legacy silent graph should not create audio: %+v", withoutAudio)
	}
	withAudio := buildMergeAudioGraph(2, true, 10, 1, nil)
	if !strings.Contains(withAudio.Filters, "[0:a][1:a]concat=n=2:v=0:a=1,volume=1.000") || withAudio.Label != "[am_native]" {
		t.Fatalf("legacy native graph changed: %+v", withAudio)
	}
}

func TestBuildMergeAudioGraphClampsFadesAndTimeline(t *testing.T) {
	graph := buildMergeAudioGraph(1, false, 5, 1, []mergeAudioInput{{Index: 1, Start: 4, Duration: 8, Volume: 1, FadeIn: 9, FadeOut: 9}})
	for _, want := range []string{"atrim=start=0:end=1.000", "afade=t=in:st=0:d=1.000", "afade=t=out:st=0.000:d=1.000", "adelay=4000:all=1"} {
		if !strings.Contains(graph.Filters, want) {
			t.Fatalf("clamped graph missing %q: %s", want, graph.Filters)
		}
	}
}
