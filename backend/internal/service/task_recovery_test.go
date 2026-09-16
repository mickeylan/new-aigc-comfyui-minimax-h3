package service

import "testing"

func TestHistoryHasPromptSupportsKeyedAndDirectResponses(t *testing.T) {
	if !historyHasPrompt(map[string]any{"prompt-1": map[string]any{"outputs": map[string]any{}}}, "prompt-1") {
		t.Fatal("keyed history was not detected")
	}
	if !historyHasPrompt(map[string]any{"outputs": map[string]any{}, "status": map[string]any{}}, "prompt-1") {
		t.Fatal("direct history response was not detected")
	}
	if historyHasPrompt(map[string]any{}, "prompt-1") {
		t.Fatal("empty history must not be detected as completed")
	}
}

func TestMatchesTaskOutputNameAllowsTemplatePrefix(t *testing.T) {
	id := "look-123"
	for _, name := range []string{"look-123.png", "character_look-123_00001.png", "asset_look-123.webp"} {
		if !matchesTaskOutputName(name, id) {
			t.Fatalf("expected %q to match %q", name, id)
		}
	}
	if matchesTaskOutputName("other.png", id) {
		t.Fatal("unrelated output matched task")
	}
}
