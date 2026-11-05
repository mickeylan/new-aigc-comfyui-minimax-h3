package service

import (
	"encoding/json"
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

func reviewedExclusionJSON(t *testing.T, content string, excludedIndex int) string {
	t.Helper()
	items := extractQuotedDialogue(content)
	items[excludedIndex].Excluded = true
	raw, err := json.Marshal(SceneDialogueAnalysis{Items: items, Source: "ai_reviewed", ContentHash: sceneDialogueContentHash(content)})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestSceneDialogueGenerationContentMasksOnlyReviewedOccurrence(t *testing.T) {
	content := "门被撞响，她听见“砰”后退一步；随后他说“砰”，并抬手示意。"
	sc := &models.Scene{Content: content, DialogueAnalysisJSON: reviewedExclusionJSON(t, content, 0)}

	got := sceneDialogueGenerationContent(sc)
	want := "门被撞响，她听见后退一步；随后他说“砰”，并抬手示意。"
	if got != want {
		t.Fatalf("generation content mismatch\n got: %q\nwant: %q", got, want)
	}
	if sc.Content != content {
		t.Fatalf("source Scene.Content was mutated: %q", sc.Content)
	}
}

func TestSceneDialogueGenerationContentRejectsStaleHash(t *testing.T) {
	old := "她听见“砰”后退。"
	sc := &models.Scene{Content: old, DialogueAnalysisJSON: reviewedExclusionJSON(t, old, 0)}
	sc.Content = old + "门外起风。"
	if got := sceneDialogueGenerationContent(sc); got != sc.Content {
		t.Fatalf("stale exclusion applied: %q", got)
	}
}

func TestGenerateSceneVideoActionDoesNotSendReviewedExcludedQuote(t *testing.T) {
	ps := newTestProjectService(t)
	if err := ps.db.AutoMigrate(&models.Shot{}, &models.Dialogue{}); err != nil {
		t.Fatal(err)
	}
	provider := &captureTextProvider{response: "[Shot 1] A woman steps back from the door while the camera holds steady."}
	ps.textProvider = provider
	project := models.Project{Title: "对白排除测试"}
	if err := ps.db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	content := "门被撞响，她听见“砰”后退一步；随后他说“别怕”，并抬手示意。"
	sc := models.Scene{ProjectID: project.ID, Duration: 5, Content: content, DialogueAnalysisJSON: reviewedExclusionJSON(t, content, 0)}
	if err := ps.db.Create(&sc).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := ps.GenerateSceneVideoAction(&sc); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(provider.firstUser, "砰") {
		t.Fatalf("provider request leaked excluded text: %s", provider.firstUser)
	}
	if !strings.Contains(provider.firstUser, "别怕") || !strings.Contains(provider.firstUser, "用户已排除1条引号误识别，不得当作发声") {
		t.Fatalf("provider request lost retained content or exclusion note: %s", provider.firstUser)
	}
	var persisted models.Scene
	if err := ps.db.First(&persisted, sc.ID).Error; err != nil {
		t.Fatal(err)
	}
	if persisted.Content != content {
		t.Fatalf("persisted Scene.Content changed: %q", persisted.Content)
	}
}
