package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"comfyui-console/internal/models"
)

func savedAnalysisJSON(t *testing.T, content string, items []SceneDialogueAnalysisItem, hash string) string {
	t.Helper()
	if hash == "" {
		hash = sceneDialogueContentHash(content)
	}
	b, err := json.Marshal(SceneDialogueAnalysis{Items: items, Source: "ai_reviewed", ContentHash: hash})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestAssessCurrentStorySpeechCoverageBoundaries(t *testing.T) {
	content := `甲说：“别走。”甲又说：“别走。”`
	tests := []struct {
		name      string
		scene     models.Scene
		effective []models.Dialogue
		want      string
	}{
		{
			name:      "legal same-speaker split",
			scene:     models.Scene{Content: `甲说：“不要走。”`},
			effective: []models.Dialogue{{Character: "甲", Text: "不要"}, {Character: "甲", Text: "走。"}},
			want:      "ok",
		},
		{
			name:      "extra production repetition is not unquoted speech",
			scene:     models.Scene{Content: `甲说：“别走。”`},
			effective: []models.Dialogue{{Character: "甲", Text: "别走。"}, {Character: "甲", Text: "别走。"}},
			want:      "attention",
		},
		{
			name:      "duplicate occurrence missing",
			scene:     models.Scene{Content: content},
			effective: []models.Dialogue{{Character: "甲", Text: "别走。"}},
			want:      "attention",
		},
		{
			name:      "current reviewed exclusion",
			scene:     models.Scene{Content: content, DialogueAnalysisJSON: savedAnalysisJSON(t, content, []SceneDialogueAnalysisItem{{Index: 1, Text: "别走。", Excluded: true}}, "")},
			effective: []models.Dialogue{{Character: "甲", Text: "别走。"}},
			want:      "ok",
		},
		{
			name:      "stale exclusion has no authority",
			scene:     models.Scene{Content: content, DialogueAnalysisJSON: savedAnalysisJSON(t, content, []SceneDialogueAnalysisItem{{Index: 1, Text: "别走。", Excluded: true}}, sceneDialogueContentHash("old"))},
			effective: []models.Dialogue{{Character: "甲", Text: "别走。"}},
			want:      "attention",
		},
		{
			name:  "all quotes explicitly excluded",
			scene: models.Scene{Content: `甲说：“别走。”`, DialogueAnalysisJSON: savedAnalysisJSON(t, `甲说：“别走。”`, []SceneDialogueAnalysisItem{{Index: 0, Text: "别走。", Excluded: true}}, "")},
			want:  "not_assessed",
		},
		{
			name:      "mixed extra effective speech is not guessed",
			scene:     models.Scene{Content: `甲说：“别走。”随后乙答道：好的。`},
			effective: []models.Dialogue{{Character: "甲", Text: "别走。"}, {Character: "乙", Text: "好的。"}},
			want:      "not_assessed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := assessCurrentStorySpeechCoverage(&tt.scene, tt.effective)
			if got != tt.want {
				t.Fatalf("status=%q want %q", got, tt.want)
			}
		})
	}
}

func TestSceneStoryConsistencyHTTPCurrentExclusionLegalShotSplitReadOnly(t *testing.T) {
	f := newSceneAdvisoryFixture(t, "http://unused.invalid", false)
	content := `甲说：“不要走。”乙说：“好的。”`
	analysis := savedAnalysisJSON(t, content, []SceneDialogueAnalysisItem{{Index: 1, Text: "好的。", Excluded: true}}, "")
	if err := f.db.Model(&f.scene).Updates(map[string]any{"content": content, "dialogue_analysis_json": analysis}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Create(&models.Dialogue{ProjectID: f.scene.ProjectID, SceneID: f.scene.ID, Order: 1, Character: "甲", Text: "不要"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Create(&models.Dialogue{ProjectID: f.scene.ProjectID, SceneID: f.scene.ID, Order: 2, Character: "甲", Text: "走。"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Create(&models.Shot{SceneID: f.scene.ID, Order: 1, Dialogue: "不要"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Create(&models.Shot{SceneID: f.scene.ID, Order: 2, Dialogue: "走。"}).Error; err != nil {
		t.Fatal(err)
	}
	var before int64
	f.db.Model(&models.Dialogue{}).Count(&before)
	w := advisoryRequest(f.router(), http.MethodGet, fmt.Sprintf("/api/projects/%d/scenes/%d/story-consistency", f.scene.ProjectID, f.scene.ID))
	var report StoryConsistencyReport
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &report) != nil {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
	checks := map[string]string{}
	for _, check := range report.Checks {
		checks[check.Code] = check.Status
	}
	if checks["dialogue_source_coverage"] != "ok" || checks["shot_dialogue_consistency"] != "ok" || report.ExcludedDialogueCount != 1 {
		t.Fatalf("unexpected report: %+v", report)
	}
	var after int64
	f.db.Model(&models.Dialogue{}).Count(&after)
	if before != after {
		t.Fatalf("GET mutated dialogue rows: %d -> %d", before, after)
	}
}
