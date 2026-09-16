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
	cases := map[string][]string{
		"look-123":                     {"look-123.png", "character_look-123_00001.png", "asset_look-123.webp"},
		"20260916-122622-ce50634b0e8f": {"character_20260916-122622-ce50634b0e8f_00001_.png"},
	}
	for id, names := range cases {
		for _, name := range names {
			if !matchesTaskOutputName(name, id) {
				t.Fatalf("expected %q to match %q", name, id)
			}
		}
	}
	if matchesTaskOutputName("other.png", "look-123") {
		t.Fatal("unrelated output matched task")
	}
}
