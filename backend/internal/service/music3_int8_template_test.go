package service

import (
	"os"
	"strings"
	"testing"
)

func TestMusic3EpisodeTemplateDefaultsToInstalledINT8Models(t *testing.T) {
	data, err := os.ReadFile("templates/minimax_music_3_episode_segment.json")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, expected := range []string{
		`minimax-music3\\minimax_music3_dit_int8_convrot.safetensors`,
		`minimax-music3\\minimax_music3_text_encoder_pruned_int8_convrot.safetensors`,
		`MiniMax-Music-3\\minimax_music3_dav.safetensors`,
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("Music 3 template missing %q", expected)
		}
	}
	for _, forbidden := range []string{
		`minimax_music3_dit_fp32.safetensors`,
		`minimax_music3_text_encoder_bf16.safetensors`,
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("Music 3 template still references high-memory model %q", forbidden)
		}
	}
}
