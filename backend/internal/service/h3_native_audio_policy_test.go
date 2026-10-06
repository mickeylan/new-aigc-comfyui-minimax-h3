package service

import (
	"strings"
	"testing"
)

func TestSilentH3SceneUsesPromptPolicyWithoutDestructivePostprocessing(t *testing.T) {
	contract := h3SoundscapeContract(false)
	for _, want := range []string{"No dialogue", "narration", "human voice"} {
		if !strings.Contains(contract, want) {
			t.Fatalf("silent-scene prompt contract missing %q: %s", want, contract)
		}
	}
	if !strings.Contains(h3NoMusicContract(), "No background music") {
		t.Fatalf("no-music prompt contract missing: %s", h3NoMusicContract())
	}
}
