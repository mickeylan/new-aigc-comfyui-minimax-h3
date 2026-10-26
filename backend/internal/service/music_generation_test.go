package service

import (
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

func TestCompileMusic3CaptionRequiresInstrumentalStructuredFields(t *testing.T) {
	raw := `{"global_metadata":"East Asian fantasy story score, 76 BPM, lyrical and spacious","vocal_details":"Fully instrumental; no voice, narration, choir or humming.","arrangement":"Intro: a memorable flowing theme enters with poetic restraint. Middle: tension develops under dialogue. Outro: resolve into a deep scenic atmosphere.","lyrics_input":""}`
	caption, lyrics, err := compileMusic3Caption(raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"### Global Metadata", "### Vocal Details", "### Arrangement", "Fully instrumental", "Intro:", "Outro:"} {
		if !strings.Contains(caption, want) {
			t.Fatalf("compiled caption missing %q: %s", want, caption)
		}
	}
	if lyrics != "" {
		t.Fatalf("instrumental execution must submit empty lyrics, got %q", lyrics)
	}
	if _, _, err := compileMusic3Caption(`{"global_metadata":"score","vocal_details":"soft singer","arrangement":"intro"}`); err == nil {
		t.Fatal("vocal caption was accepted for episode instrumental score")
	}
	if _, _, err := compileMusic3Caption(`{"global_metadata":"Hollywood trailer score","vocal_details":"Fully instrumental","arrangement":"heroic brass"}`); err == nil {
		t.Fatal("generic Western score without oriental identity was accepted")
	}
	if _, _, err := compileMusic3Caption(`{"global_metadata":"East Asian fantasy story score","vocal_details":"Fully instrumental","arrangement":"A lyrical theme follows the story environment","lyrics_input":"[Verse]\nSing in English"}`); err == nil {
		t.Fatal("lyrics text was accepted for instrumental score")
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
	for _, className := range []string{"MiniMaxMusic3TextEncode", "EmptyMiniMaxMusic3LatentAudio", "KSampler", "VAEDecodeAudioTiled", "AudioSeparation", "AudioCombine", "SaveAudio"} {
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

func TestMusic3StoryContractDoesNotMandateScaleModeOrInstrument(t *testing.T) {
	lower := strings.ToLower(music3OrientalInstrumentalContract)
	for _, forbidden := range []string{"pentatonic", "feature guqin", "must use guqin", "must use xiao"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("contract still mandates %q: %s", forbidden, music3OrientalInstrumentalContract)
		}
	}
	for _, required := range []string{"storytelling", "story", "fixed scale", "fixed mode", "fixed instrument list"} {
		if !strings.Contains(lower, required) {
			t.Fatalf("contract missing %q", required)
		}
	}
	if !strings.HasPrefix(musicCueSourceHash("ctx", "req"), "music3-dense-sections-duration-v9:") {
		t.Fatal("music policy version is not bound into source hash")
	}
}

func TestMusic3InstrumentalStructureExtendsLongFormControl(t *testing.T) {
	short := music3InstrumentalStructure(30)
	long := music3InstrumentalStructure(157)
	if !strings.Contains(short, "[Intro]") || !strings.Contains(short, "[Outro]") {
		t.Fatalf("short structure=%q", short)
	}
	if strings.Count(long, "[Instrumental]") < 15 || !strings.Contains(long, "[Solo]") {
		t.Fatalf("long structure does not provide roughly one section per ten seconds: %q", long)
	}
	if strings.Contains(long, "Sing") || strings.Contains(long, "Verse") || strings.Contains(long, "Chorus") {
		t.Fatalf("instrumental control contains vocal-song section: %q", long)
	}
}

func TestInspectMusicFLACReadsActualDuration(t *testing.T) {
	sampleRate, channels, totalSamples := uint64(44100), uint64(2), uint64(1023120)
	packed := sampleRate<<44 | (channels-1)<<41 | (16-1)<<36 | totalSamples
	data := make([]byte, 4+4+34)
	copy(data[:4], "fLaC")
	data[4] = 0x80
	data[7] = 34
	for i := 0; i < 8; i++ {
		data[4+4+10+i] = byte(packed >> (56 - 8*i))
	}
	info, err := inspectMusicFLAC(data)
	if err != nil {
		t.Fatal(err)
	}
	if info.SampleRate != 44100 || info.Channels != 2 || info.Duration < 23.19 || info.Duration > 23.21 {
		t.Fatalf("info=%+v", info)
	}
}

func TestPendingMusicCandidateFingerprintIsTaskUniqueAndStable(t *testing.T) {
	first := pendingMusicCandidateFingerprint("task-a")
	if first == pendingMusicCandidateFingerprint("task-b") {
		t.Fatal("different pending Music 3 tasks share a unique-index fingerprint")
	}
	if first != pendingMusicCandidateFingerprint("task-a") || len(first) != 64 {
		t.Fatalf("pending fingerprint is unstable or not SHA-256: %q", first)
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
