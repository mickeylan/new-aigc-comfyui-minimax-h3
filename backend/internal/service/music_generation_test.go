package service

import (
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

func TestCompileMusic3CaptionRequiresInstrumentalStructuredFields(t *testing.T) {
	raw := `{"global_metadata":"cinematic xianxia score, 76 BPM","vocal_details":"Fully instrumental; no voice, narration, choir or humming.","arrangement":"Intro: guqin and xiao. Middle: low strings. Outro: resolve.","lyrics_input":"[Intro]\n[Instrumental]\n[Outro]"}`
	caption, lyrics, err := compileMusic3Caption(raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Global Metadata:", "Vocal Details:", "Arrangement:", "Fully instrumental"} {
		if !strings.Contains(caption, want) {
			t.Fatalf("compiled caption missing %q: %s", want, caption)
		}
	}
	if lyrics != "[Intro]\n[Instrumental]\n[Outro]" {
		t.Fatalf("lyrics=%q", lyrics)
	}
	if _, _, err := compileMusic3Caption(`{"global_metadata":"score","vocal_details":"soft singer","arrangement":"intro"}`); err == nil {
		t.Fatal("vocal caption was accepted for episode instrumental score")
	}
}

func TestMusic3SegmentTemplateRendersOfficialNodes(t *testing.T) {
	tpl := loadAudioVideoTemplateFixture(t, "minimax_music_3_episode_segment.json")
	if tpl.Code != music3EpisodeSegmentTemplate {
		t.Fatalf("code=%q", tpl.Code)
	}
	classes := map[string]bool{}
	for _, node := range tpl.Workflow {
		classes[node.ClassType] = true
	}
	for _, className := range []string{"MiniMaxMusic3TextEncode", "EmptyMiniMaxMusic3LatentAudio", "KSampler", "VAEDecodeAudioTiled", "SaveAudio"} {
		if !classes[className] {
			t.Fatalf("missing official node %s", className)
		}
	}
}

func TestValidateGeneratedMusicCueCoverageSplitsLongEpisodeIntoTwoContinuousSegments(t *testing.T) {
	valid := []generatedMusicCue{{StartTime: 0, EndTime: 195}, {StartTime: 195, EndTime: 389}}
	if err := validateGeneratedMusicCueCoverage(valid, 389); err != nil {
		t.Fatal(err)
	}
	for name, cues := range map[string][]generatedMusicCue{
		"one overlong segment": {{StartTime: 0, EndTime: 389}},
		"gap":                  {{StartTime: 0, EndTime: 190}, {StartTime: 195, EndTime: 389}},
		"not full coverage":    {{StartTime: 0, EndTime: 195}, {StartTime: 195, EndTime: 380}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateGeneratedMusicCueCoverage(cues, 389); err == nil {
				t.Fatal("invalid episode music segmentation was accepted")
			}
		})
	}
}

func TestMusicCueDurationUsesExplicitOrTimelineDuration(t *testing.T) {
	if got := musicCueDuration(&models.MusicCueSheet{Duration: 195, StartTime: 0, EndTime: 200}); got != 195 {
		t.Fatalf("explicit duration=%v", got)
	}
	if got := musicCueDuration(&models.MusicCueSheet{StartTime: 195, EndTime: 389}); got != 194 {
		t.Fatalf("timeline duration=%v", got)
	}
}
