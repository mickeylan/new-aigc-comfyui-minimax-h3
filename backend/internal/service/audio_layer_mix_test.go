package service

import (
	"testing"

	"comfyui-console/internal/models"
)

func TestResolveAudioLayerMixManualBGMPriority(t *testing.T) {
	layers := []models.AudioLayer{
		{ID: 1, Kind: "bgm", Source: audioLayerSourceManual, File: "manual.mp3", StartTime: 0, EndTime: 10, Status: "ready"},
		{ID: 2, Kind: "bgm", Source: audioLayerSourceMusicCue, File: "a.mp3", StartTime: 0, EndTime: 4, Status: "ready"},
		{ID: 3, Kind: "bgm", Source: audioLayerSourceMusicCue, File: "b.mp3", StartTime: 4, EndTime: 10, Status: "ready"},
	}
	got := resolveAudioLayerMix(layers)
	if got[0].Status != audioLayerMixYielded {
		t.Fatalf("manual BGM status = %q, want yielded", got[0].Status)
	}

	layers[2].Muted = true
	got = resolveAudioLayerMix(layers)
	if got[0].Status != audioLayerMixPartialOverlap {
		t.Fatalf("manual BGM status = %q, want partial_overlap", got[0].Status)
	}

	layers[1].Stale = true
	got = resolveAudioLayerMix(layers)
	if got[0].Status != audioLayerMixIncluded {
		t.Fatalf("inactive cue layers must not affect priority; got %q", got[0].Status)
	}
}
