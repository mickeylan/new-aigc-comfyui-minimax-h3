package service

import (
	"math"
	"strings"
	"testing"
)

func TestResolvedMergeClipRangeRejectsInvalidBoundsWithoutClamping(t *testing.T) {
	end := 4.0
	start, gotEnd, err := resolvedMergeClipRange(mergeResolvedVideo{SourceStart: 1.25, SourceEnd: &end}, 5)
	if err != nil || start != 1.25 || gotEnd != 4 {
		t.Fatalf("range=(%v,%v) err=%v", start, gotEnd, err)
	}
	for _, video := range []mergeResolvedVideo{
		{SourceStart: -1},
		{SourceStart: math.NaN()},
		{SourceStart: 2, SourceEnd: float64Ptr(2)},
		{SourceEnd: float64Ptr(6)},
	} {
		if _, _, err := resolvedMergeClipRange(video, 5); err == nil {
			t.Fatalf("accepted invalid range: %+v", video)
		}
	}
}

func TestBuildEditedVideoGraphTrimsBeforeNormalization(t *testing.T) {
	end := 2.75
	graph := strings.Join(buildEditedVideoGraph([]mergeResolvedVideo{{SourceStart: .5, SourceEnd: &end}, {SourceStart: 1}}, 1280, 720), ";")
	for _, want := range []string{
		"[0:v]trim=start=0.500:end=2.750,setpts=PTS-STARTPTS,scale=1280:720",
		"[1:v]trim=start=1.000,setpts=PTS-STARTPTS,scale=1280:720",
		"[vnorm0][vnorm1]concat=n=2:v=1:a=0[vc];[vc]fps=24[v]",
	} {
		if !strings.Contains(graph, want) {
			t.Fatalf("missing %q in %s", want, graph)
		}
	}
}

func TestBuildEditedNativeAudioGraphUsesSilencePerMissingInput(t *testing.T) {
	videos := []mergeResolvedVideo{{SourceStart: 1, SourceDuration: 3}, {SourceDuration: 3}}
	graph := buildEditedNativeAudioGraph(videos, []bool{true, false}, 5, .8)
	for _, want := range []string{
		"[0:a]atrim=start=1.000:end=3.000,asetpts=PTS-STARTPTS",
		"anullsrc=r=48000:cl=stereo,atrim=duration=3.000",
		"[am_native_clip_0][am_native_clip_1]concat=n=2:v=0:a=1,volume=0.800",
	} {
		if !strings.Contains(graph.Filters, want) {
			t.Fatalf("missing %q in %s", want, graph.Filters)
		}
	}
	if graph.Label != "[am_native_edited]" {
		t.Fatalf("label=%q", graph.Label)
	}
}

func float64Ptr(value float64) *float64 { return &value }
