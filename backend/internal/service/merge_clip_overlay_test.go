package service

import (
	"strings"
	"testing"
)

func TestBuildEditedVideoGraphAddsNormalizedOverlay(t *testing.T) {
	id := uint(9)
	graph := strings.Join(buildEditedVideoGraph([]mergeResolvedVideo{
		{SourceDuration: 3, OverlayMaterialID: &id, OverlayInputIndex: 2, OverlayWidth: .25, OverlayX: .5, OverlayY: .75, OverlayOpacity: .4},
		{SourceDuration: 2},
	}, 1280, 720), ";")
	for _, want := range []string{
		"[2:v]setpts=PTS-STARTPTS,fps=24,scale=320:-2,format=rgba,colorchannelmixer=aa=0.400[overlay_src_0]",
		"[vbase0][overlay_src_0]overlay=x=(W-w)*0.500:y=(H-h)*0.750:shortest=1[vnorm0]",
		"[vnorm0][vnorm1]concat=n=2:v=1:a=0",
	} {
		if !strings.Contains(graph, want) {
			t.Fatalf("missing %q in %s", want, graph)
		}
	}
}

func TestBuildMergeTitleCuesUsesClipTimelineAndNoScene(t *testing.T) {
	videos := []mergeResolvedVideo{
		{SourceDuration: 4, Title: `第一章{序}`, TitlePosition: "top", TitleSize: 40, Transition: "fade", TransitionDuration: 1},
		{SourceDuration: 3, Title: "终章", TitlePosition: "bottom"},
	}
	cues := buildMergeTitleCues(videos)
	if len(cues) != 2 || cues[0].SceneID != nil || cues[0].StartTime != 0 || cues[0].EndTime != 4 || cues[1].StartTime != 3 || cues[1].EndTime != 6 {
		t.Fatalf("unexpected title cues: %+v", cues)
	}
	if cues[0].Anchor != "top_center" || cues[1].Anchor != "bottom_center" || cues[0].FontCode != "noto-sans-sc" || cues[0].StyleCode != "modern-horizontal-caption" {
		t.Fatalf("unexpected title styles: %+v", cues)
	}
}
