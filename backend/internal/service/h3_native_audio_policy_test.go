package service

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"comfyui-console/internal/models"
)

func TestSceneMustMuteGeneratedAudioOnlyWithoutDialogue(t *testing.T) {
	if !sceneMustMuteGeneratedAudio(nil) {
		t.Fatal("silent scene did not request native-audio removal")
	}
	if sceneMustMuteGeneratedAudio([]models.Dialogue{{Character: "舒寒", Text: "别怕。", SpeechType: "dialogue"}}) {
		t.Fatal("dialogue scene requested native-audio removal")
	}
}

func TestStripGeneratedVideoAudioRemovesEntireAudioStream(t *testing.T) {
	ffmpeg, err := localFFmpegPath()
	if err != nil {
		t.Skip(err)
	}
	ffprobe, err := localFFprobePath()
	if err != nil {
		t.Skip(err)
	}
	file := filepath.Join(t.TempDir(), "generated.mp4")
	_, err = runLocalProgram(ffmpeg, []string{
		"-y", "-f", "lavfi", "-i", "color=c=black:s=64x64:r=24:d=1",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=1",
		"-shortest", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", file,
	}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	probeAudio := func() string {
		out, probeErr := exec.Command(ffprobe, "-v", "error", "-select_streams", "a", "-show_entries", "stream=codec_type", "-of", "default=nw=1:nk=1", file).CombinedOutput()
		if probeErr != nil {
			t.Fatalf("ffprobe failed: %v: %s", probeErr, out)
		}
		return strings.TrimSpace(string(out))
	}
	if probeAudio() != "audio" {
		t.Fatal("fixture did not contain an audio stream")
	}
	if err := stripGeneratedVideoAudio(file); err != nil {
		t.Fatal(err)
	}
	if got := probeAudio(); got != "" {
		t.Fatalf("audio stream remained after stripping: %q", got)
	}
}
