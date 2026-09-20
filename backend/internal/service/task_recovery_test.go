package service

import "testing"

func TestHistoryHasPromptRequiresUsableOutputOrTerminalError(t *testing.T) {
	fileOutput := map[string]any{"9": map[string]any{"images": []any{map[string]any{"filename": "done.png"}}}}
	if !historyHasPrompt(map[string]any{"prompt-1": map[string]any{"outputs": fileOutput}}, "prompt-1") {
		t.Fatal("keyed history with output was not detected")
	}
	if !historyHasPrompt(map[string]any{"outputs": fileOutput, "status": map[string]any{"status_str": "success"}}, "prompt-1") {
		t.Fatal("direct history response with output was not detected")
	}
	for name, history := range map[string]map[string]any{
		"empty":            {},
		"running metadata": {"status": map[string]any{"status_str": "running"}},
		"empty outputs":    {"status": map[string]any{"status_str": "success"}, "outputs": map[string]any{}},
		"missing filename": {"outputs": map[string]any{"9": map[string]any{"images": []any{map[string]any{}}}}},
	} {
		if historyHasPrompt(history, "prompt-1") {
			t.Fatalf("%s history must remain recoverable, not completed", name)
		}
	}
	if !historyHasPrompt(map[string]any{"status": map[string]any{"status_str": "error"}}, "prompt-1") {
		t.Fatal("terminal error must be handled")
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
