package service

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
)

func TestMergeClipEffectsRealFFmpeg(t *testing.T) {
	ffmpeg, err := localFFmpegPath()
	if err != nil {
		t.Skipf("real ffmpeg integration test requires a locally installed ffmpeg: %v", err)
	}
	const timeout = 45 * time.Second
	dir := t.TempDir()
	red, blue := filepath.Join(dir, "red.mp4"), filepath.Join(dir, "blue.mp4")
	overlay := filepath.Join(dir, "green.png")
	makeAV := func(path, color, tone string) {
		t.Helper()
		log, runErr := runLocalProgram(ffmpeg, []string{
			"-y", "-f", "lavfi", "-i", "color=c=" + color + ":s=160x96:r=24:d=4",
			"-f", "lavfi", "-i", "sine=frequency=" + tone + ":sample_rate=48000:duration=4",
			"-shortest", "-c:v", "libx264", "-threads", "1", "-pix_fmt", "yuv420p",
			"-c:a", "aac", "-sample_fmt", "fltp", path,
		}, timeout)
		if runErr != nil {
			t.Fatalf("create %s fixture: %v\n%s", color, runErr, log)
		}
	}
	makeAV(red, "red", "440")
	makeAV(blue, "blue", "880")
	if log, runErr := runLocalProgram(ffmpeg, []string{
		"-y", "-f", "lavfi", "-i", "color=c=green:s=64x64", "-frames:v", "1", "-threads", "1", overlay,
	}, timeout); runErr != nil {
		t.Fatalf("create overlay fixture: %v\n%s", runErr, log)
	}
	redHash, blueHash, overlayHash := mergeFixtureSHA256(t, red), mergeFixtureSHA256(t, blue), mergeFixtureSHA256(t, overlay)

	for _, transition := range []string{"fade", "wipeleft", "slideright", "cut"} {
		transition := transition
		t.Run(transition, func(t *testing.T) {
			saturation, volume, overlayID := .5, .7, uint(1)
			duration := .5
			if transition == "cut" {
				duration = 0
			}
			videos := []mergeResolvedVideo{
				{SourceDuration: 4, Speed: 2, Volume: &volume, FadeIn: .1, Transition: transition, TransitionDuration: duration,
					Brightness: .05, Saturation: &saturation, OverlayMaterialID: &overlayID, OverlayInputIndex: 2,
					OverlayIsImage: true, OverlayWidth: .4, OverlayOpacity: .8, OverlayX: 1, OverlayY: 1},
				{SourceDuration: 4, Speed: 1, Volume: &volume, FadeIn: .1},
			}
			wantDuration := 6 - duration
			if err := validateMergeTimelineEffects(videos); err != nil {
				t.Fatalf("validate effects: %v", err)
			}
			videoGraph := buildEditedVideoGraph(videos, 160, 96)
			audioGraph := buildEditedNativeAudioGraph(videos, []bool{true, true}, wantDuration, 1)
			filters := strings.Join(append(videoGraph, audioGraph.Filters), ";")
			output := filepath.Join(dir, "effects-"+transition+".mp4")
			log, runErr := runLocalProgram(ffmpeg, []string{
				"-y", "-i", red, "-i", blue, "-loop", "1", "-i", overlay,
				"-filter_complex_threads", "1", "-filter_complex", filters,
				"-map", "[v]", "-map", audioGraph.Label, "-t", ffnum(wantDuration),
				"-c:v", "libx264", "-threads", "1", "-pix_fmt", "yuv420p", "-c:a", "aac", output,
			}, timeout)
			if runErr != nil {
				t.Fatalf("render %s: %v\n%s", transition, runErr, log)
			}
			assertEffectsMedia(t, ffmpeg, output, wantDuration, timeout)
		})
	}
	if mergeFixtureSHA256(t, red) != redHash || mergeFixtureSHA256(t, blue) != blueHash || mergeFixtureSHA256(t, overlay) != overlayHash {
		t.Fatal("an input fixture changed during effects rendering")
	}
}

func TestMergeClipTitleASSRealFFmpeg(t *testing.T) {
	ffmpeg, err := localFFmpegPath()
	if err != nil {
		t.Skipf("real ffmpeg integration test requires a locally installed ffmpeg: %v", err)
	}
	const timeout = 45 * time.Second
	dir := t.TempDir()
	input, output, assPath := filepath.Join(dir, "title-source.mp4"), filepath.Join(dir, "title-output.mp4"), filepath.Join(dir, "title.ass")
	if log, runErr := runLocalProgram(ffmpeg, []string{"-y", "-f", "lavfi", "-i", "color=c=black:s=160x96:r=24:d=4", "-c:v", "libx264", "-threads", "1", "-pix_fmt", "yuv420p", input}, timeout); runErr != nil {
		t.Fatalf("create title fixture: %v\n%s", runErr, log)
	}
	before := mergeFixtureSHA256(t, input)
	title := `Demo: apostrophe's \ path {plain}`
	videos := []mergeResolvedVideo{{SourceDuration: 4, Title: title, TitlePosition: "bottom", TitleSize: 24}}
	cues := buildMergeTitleCues(videos)
	ass, count := buildScreenTextASS(cues, []models.Scene{{Duration: 4}}, []sceneVideo{{dur: 4}}, 160, 96, "sans-serif", map[string]string{"noto-sans-sc": "sans-serif"})
	if count != 1 || !strings.Contains(string(ass), `Demo: apostrophe's \\ path \{plain\}`) {
		t.Fatalf("ASS did not preserve and safely escape title plaintext:\n%s", ass)
	}
	if err := os.WriteFile(assPath, ass, 0o600); err != nil {
		t.Fatal(err)
	}
	filters := applyMergeTextFilters(buildEditedVideoGraph(videos, 160, 96), []string{"subtitles=filename=" + escapeFilterPath(assPath)})
	if log, runErr := runLocalProgram(ffmpeg, []string{
		"-y", "-i", input, "-filter_complex_threads", "1", "-filter_complex", strings.Join(filters, ";"),
		"-map", "[v]", "-t", "4", "-c:v", "libx264", "-threads", "1", "-pix_fmt", "yuv420p", output,
	}, timeout); runErr != nil {
		t.Fatalf("render title (libass is required): %v\n%s", runErr, log)
	}
	info, probeErr := NewRemoteExec(config.RemoteConfig{}).ProbeMedia(output)
	if probeErr != nil || math.Abs(info.Duration-4) > .1 {
		t.Fatalf("title output probe = %+v, err %v; want duration 4±.1", info, probeErr)
	}
	if mergeFixtureSHA256(t, input) != before {
		t.Fatal("title input fixture changed during render")
	}
}

func TestValidateMergeClipEffectsBoundaries(t *testing.T) {
	volume := 1.0
	valid := mergeResolvedVideo{SourceDuration: 4, Speed: 0, Volume: &volume}
	if err := validateMergeClipEffects(valid); err != nil {
		t.Fatalf("zero speed must retain default speed semantics: %v", err)
	}
	badVolume, end := 4.1, 5.0
	cases := map[string]mergeResolvedVideo{
		"NaN speed":             {SourceDuration: 4, Speed: math.NaN()},
		"unknown transition":    {SourceDuration: 4, Transition: "spin", TransitionDuration: .5},
		"speed out of bounds":   {SourceDuration: 4, Speed: .2},
		"volume out of bounds":  {SourceDuration: 4, Volume: &badVolume},
		"fade out of bounds":    {SourceDuration: 4, FadeIn: 4.1},
		"source out of bounds":  {SourceDuration: 4, SourceEnd: &end},
		"overlay out of bounds": {SourceDuration: 4, OverlayX: 1.1},
	}
	for name, clip := range cases {
		t.Run(name, func(t *testing.T) {
			if err := validateMergeClipEffects(clip); err == nil {
				t.Fatal("expected invalid effect to be rejected")
			}
		})
	}
}

func assertEffectsMedia(t *testing.T, ffmpeg, path string, duration float64, timeout time.Duration) {
	t.Helper()
	info, err := NewRemoteExec(config.RemoteConfig{}).ProbeMedia(path)
	if err != nil || math.Abs(info.Duration-duration) > .1 || info.Width != 160 || info.Height != 96 {
		t.Fatalf("output probe = %+v, err %v; want 160x96 duration %.1f±.1", info, err, duration)
	}
	if log, runErr := runLocalProgram(ffmpeg, []string{"-v", "error", "-i", path, "-map", "0:a:0", "-f", "null", "-"}, timeout); runErr != nil {
		t.Fatalf("output has no decodable audio: %v\n%s", runErr, log)
	}
}
