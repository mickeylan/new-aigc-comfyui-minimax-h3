package service

import (
	"crypto/sha256"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"comfyui-console/internal/config"
)

func TestMergeEditedClipsWithNativeAudioFFmpeg(t *testing.T) {
	ffmpeg, err := localFFmpegPath()
	if err != nil {
		t.Skipf("real ffmpeg integration test requires a locally installed ffmpeg: %v", err)
	}

	dir := t.TempDir()
	audioSource := filepath.Join(dir, "audio.mp4")
	silentSource := filepath.Join(dir, "silent.mp4")
	output := filepath.Join(dir, "edited.mp4")
	const timeout = 45 * time.Second

	if log, err := runLocalProgram(ffmpeg, []string{
		"-y", "-f", "lavfi", "-i", "color=c=blue:s=160x96:r=24:d=4",
		"-f", "lavfi", "-i", "sine=frequency=1000:sample_rate=48000:duration=4",
		"-shortest", "-c:v", "libx264", "-threads", "1", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-sample_fmt", "fltp", audioSource,
	}, timeout); err != nil {
		t.Fatalf("create audio fixture (timeout remains %s): %v\n%s", timeout, err, log)
	}
	if log, err := runLocalProgram(ffmpeg, []string{
		"-y", "-f", "lavfi", "-i", "color=c=red:s=160x96:r=24:d=4",
		"-an", "-c:v", "libx264", "-threads", "1", "-pix_fmt", "yuv420p", silentSource,
	}, timeout); err != nil {
		t.Fatalf("create silent fixture (timeout remains %s): %v\n%s", timeout, err, log)
	}

	beforeAudio := mergeFixtureSHA256(t, audioSource)
	beforeSilent := mergeFixtureSHA256(t, silentSource)
	silentEnd, audioEnd := 2.0, 2.0
	videos := []mergeResolvedVideo{
		{SourceStart: 1, SourceEnd: &silentEnd, SourceDuration: 4},
		{SourceStart: .5, SourceEnd: &audioEnd, SourceDuration: 4},
	}
	videoFilters := buildEditedVideoGraph(videos, 160, 96)
	native := buildEditedNativeAudioGraph(videos, []bool{false, true}, 2.5, 1)
	filters := strings.Join(append(videoFilters, native.Filters), ";")
	if log, err := runLocalProgram(ffmpeg, []string{
		"-y", "-i", silentSource, "-i", audioSource,
		"-filter_complex", filters, "-map", "[v]", "-map", native.Label,
		"-c:v", "libx264", "-threads", "1", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-sample_fmt", "fltp", output,
	}, timeout); err != nil {
		t.Fatalf("render edited clips (timeout remains %s): %v\n%s", timeout, err, log)
	}

	info, err := NewRemoteExec(config.RemoteConfig{}).ProbeMedia(output)
	if err != nil {
		t.Fatalf("ProbeMedia rendered output: %v", err)
	}
	if math.Abs(info.Duration-2.5) > .08 || info.Width != 160 || info.Height != 96 {
		t.Fatalf("rendered media = %dx%d duration %.3f, want 160x96 duration 2.5±.08", info.Width, info.Height, info.Duration)
	}
	if log, err := runLocalProgram(ffmpeg, []string{"-v", "error", "-i", output, "-map", "0:a:0", "-f", "null", "-"}, timeout); err != nil {
		t.Fatalf("rendered output has no decodable audio stream: %v\n%s", err, log)
	}

	early := rmsLevel(t, ffmpeg, output, 0, .8, timeout)
	late := rmsLevel(t, ffmpeg, output, 1.2, .8, timeout)
	if early != "-inf" {
		t.Fatalf("early silent slice RMS level = %q, want -inf", early)
	}
	if late == "-inf" {
		t.Fatalf("later audio slice RMS level = %q, want finite", late)
	}
	t.Logf("captured output RMS levels: early=%s dB, later=%s dB", early, late)

	if got := mergeFixtureSHA256(t, audioSource); got != beforeAudio {
		t.Fatal("audio source hash changed during render")
	}
	if got := mergeFixtureSHA256(t, silentSource); got != beforeSilent {
		t.Fatal("silent source hash changed during render")
	}
}

func mergeFixtureSHA256(t *testing.T, path string) [sha256.Size]byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(data)
}

func rmsLevel(t *testing.T, ffmpeg, path string, start, duration float64, timeout time.Duration) string {
	t.Helper()
	log, err := runLocalProgram(ffmpeg, []string{
		"-hide_banner", "-ss", fmt.Sprintf("%.3f", start), "-t", fmt.Sprintf("%.3f", duration),
		"-i", path, "-map", "0:a:0", "-af", "astats=metadata=0:reset=0", "-f", "null", "-",
	}, timeout)
	if err != nil {
		t.Fatalf("measure audio RMS: %v\n%s", err, log)
	}
	matches := regexp.MustCompile(`RMS level dB:\s*(-?inf|-?[0-9.]+)`).FindAllStringSubmatch(log, -1)
	if len(matches) == 0 {
		t.Fatalf("astats emitted no RMS level:\n%s", log)
	}
	return matches[len(matches)-1][1]
}
