package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
)

func TestAudioLayerPreflightMissingFileDoesNotExposeStoragePath(t *testing.T) {
	_, db := newAudioLayerTestService(t)
	root := t.TempDir()
	project := models.Project{Title: "missing audio"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Scene{ProjectID: project.ID, EpisodeN: 1, Duration: 60}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.AudioLayer{ProjectID: project.ID, EpisodeN: 1, Kind: "bgm", File: "absent.flac", StartTime: 0, EndTime: 60, Status: "ready"}).Error; err != nil {
		t.Fatal(err)
	}
	ps := &ProjectService{cfg: &config.Config{Comfy: config.ComfyConfig{ComfyDir: root}}, db: db}
	result, err := ps.AudioLayerPreflight(project.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, message := range result.Messages {
		if strings.Contains(message.Message, root) {
			t.Fatal("preflight exposed absolute storage path")
		}
		if message.Code == "invalid_timing" {
			t.Fatal("60-second scene was incorrectly clamped to 15 seconds")
		}
		found = found || message.Code == "missing_file"
	}
	if !found || result.Ready {
		t.Fatalf("missing audio not diagnosed: %+v", result)
	}
	missing, err := ps.AudioLayerPreflight(project.ID, 999)
	if err != nil {
		t.Fatal(err)
	}
	if missing.Ready {
		t.Fatal("missing episode timeline cannot be ready")
	}
}

func TestAudioLayerPreflightReportsPriorityAndSafety(t *testing.T) {
	_, db := newAudioLayerTestService(t)
	root := t.TempDir()
	project := models.Project{Title: "preflight"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Scene{ProjectID: project.ID, EpisodeN: 1, Generation: 1, Order: 1, Duration: 10}).Error; err != nil {
		t.Fatal(err)
	}
	mediaDir := filepath.Join(root, "input", "1")
	if err := os.MkdirAll(mediaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"manual.mp3", "cue.mp3"} {
		if err := os.WriteFile(filepath.Join(mediaDir, name), []byte("test"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	layers := []models.AudioLayer{
		{ProjectID: project.ID, EpisodeN: 1, Kind: "bgm", Source: audioLayerSourceManual, File: "manual.mp3", StartTime: 0, EndTime: 10, Status: "ready"},
		{ProjectID: project.ID, EpisodeN: 1, Kind: "bgm", Source: audioLayerSourceMusicCue, File: "cue.mp3", StartTime: 0, EndTime: 5, Status: "ready"},
		{ProjectID: project.ID, EpisodeN: 1, Kind: "sfx", File: "../escape.wav", StartTime: 9, EndTime: 11, Status: "ready", Stale: true},
	}
	if err := db.Create(&layers).Error; err != nil {
		t.Fatal(err)
	}
	ps := &ProjectService{cfg: &config.Config{Comfy: config.ComfyConfig{ComfyDir: root}}, db: db}
	got, err := ps.AudioLayerPreflight(project.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.Ready {
		t.Fatal("unsafe path and out-of-episode timing must fail readiness")
	}
	if got.LoudnessAssessed || got.LoudnessAssessment == "" {
		t.Fatal("loudness must be explicitly not assessed")
	}
	if got.Layers[0].MixStatus != audioLayerMixPartialOverlap {
		t.Fatalf("manual status=%q", got.Layers[0].MixStatus)
	}
	codes := map[string]bool{}
	for _, message := range got.Messages {
		codes[message.Code] = true
	}
	for _, code := range []string{"manual_bgm_partial_overlap", "stale", "invalid_timing", "unsafe_file_path"} {
		if !codes[code] {
			t.Fatalf("missing preflight message %q: %+v", code, got.Messages)
		}
	}
}
