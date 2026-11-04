package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

func adaptationStoryIntegrationFixture(t *testing.T, ledger []SourceDialogue, integrated bool, includeStates bool) string {
	t.Helper()
	raw := adaptationScriptFixture(t, "story-integration-script", ledger, true)
	var root map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &root); err != nil {
		t.Fatal(err)
	}
	var scenes []scriptScene
	if err := json.Unmarshal(root["scenes"], &scenes); err != nil {
		t.Fatal(err)
	}
	visual, err := parseScriptJSON(adaptationScriptFixture(t, "", ledger, false))
	if err != nil {
		t.Fatal(err)
	}
	for i := range scenes {
		scenes[i].Content = visual.Scenes[i].Content
		if integrated && i < len(ledger) {
			d := ledger[i]
			scenes[i].Content = fmt.Sprintf("%s说：“%s”随即继续观察四周。", d.Speaker, d.Text)
		}
	}
	root["scenes"], _ = json.Marshal(scenes)
	if includeStates {
		root["opening_state"] = json.RawMessage(`{"action":"三人开始追踪","place":"荒原入口"}`)
		root["ending_state"] = json.RawMessage(`{"action":"村落灯火出现","place":"山谷出口"}`)
	}
	encoded, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func TestGenerateScriptRepairsStoryIntegrationWithCompleteStructuredDialogue(t *testing.T) {
	provider := &adaptationMockTextProvider{}
	svc, project, chapter := newAdaptationDialogueTestService(t, provider)
	ledger := extractSourceDialogues(chapter, "虬髯大汉", "马脸男子", "齐姓道士")
	if len(ledger) != 13 {
		t.Fatalf("fixture ledger=%d, want 13", len(ledger))
	}
	first := adaptationStoryIntegrationFixture(t, ledger, false, true)
	second := adaptationStoryIntegrationFixture(t, ledger, true, false)
	provider.responses = []string{first, second}

	ep, err := svc.GenerateScript(project.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(provider.users) != 2 {
		t.Fatalf("provider calls=%d, want initial generation plus exactly one story-integration revision", len(provider.users))
	}
	for _, want := range []string{"scenes_requiring_story_integration", "source_chapters", "虬髯大汉没有说话，朝眼前齐人高的杂草丛指了指"} {
		if !strings.Contains(provider.users[1], want) {
			t.Fatalf("revision user prompt lacks %q", want)
		}
	}
	for _, d := range ledger {
		if !strings.Contains(provider.users[1], d.Text) {
			t.Fatalf("revision user prompt lacks original source dialogue %q", d.Text)
		}
	}
	if ep.OpeningState != `{"action":"三人开始追踪","place":"荒原入口"}` || ep.EndingState != `{"action":"村落灯火出现","place":"山谷出口"}` {
		t.Fatalf("revision omitted and lost root states: opening=%q ending=%q", ep.OpeningState, ep.EndingState)
	}

	var coverage DialogueCoverage
	if err := json.Unmarshal([]byte(ep.DialogueCoverageJSON), &coverage); err != nil {
		t.Fatal(err)
	}
	if coverage.Total != 13 || coverage.Covered != 13 || len(coverage.Missing) != 0 || len(coverage.StoryPendingSceneOrders) != 0 {
		t.Fatalf("coverage=%+v", coverage)
	}
	var saved []models.Scene
	if err := svc.db.Where("project_id = ? AND episode_n = ? AND archived = ?", project.ID, 1, false).Order("`order`").Find(&saved).Error; err != nil {
		t.Fatal(err)
	}
	if len(saved) != 20 {
		t.Fatalf("saved scenes=%d, want stable 20", len(saved))
	}
	for i, d := range ledger {
		if strings.Count(saved[i].Content, d.Text) != 1 {
			t.Fatalf("scene %d exact dialogue occurrences=%d, content=%q", i+1, strings.Count(saved[i].Content, d.Text), saved[i].Content)
		}
		if !strings.Contains(saved[i].Content, d.Speaker+"说：“"+d.Text+"”") {
			t.Fatalf("scene %d lacks natural speaker prefix: %q", i+1, saved[i].Content)
		}
	}
}

func TestGenerateScriptStoryIntegrationRepairFailurePersistsDiagnostics(t *testing.T) {
	provider := &adaptationMockTextProvider{}
	svc, project, chapter := newAdaptationDialogueTestService(t, provider)
	ledger := extractSourceDialogues(chapter, "虬髯大汉", "马脸男子", "齐姓道士")
	first := adaptationStoryIntegrationFixture(t, ledger, false, true)
	provider.responses = []string{first, ""}
	provider.errors = []error{nil, errors.New("story integration revision unavailable")}

	ep, err := svc.GenerateScript(project.ID, 1)
	if err != nil {
		t.Fatalf("optional story integration repair must not hard-block persistence: %v", err)
	}
	if len(provider.users) != 2 {
		t.Fatalf("provider calls=%d, want exactly 2", len(provider.users))
	}
	var coverage DialogueCoverage
	if err := json.Unmarshal([]byte(ep.DialogueCoverageJSON), &coverage); err != nil {
		t.Fatal(err)
	}
	if coverage.Total != 13 || coverage.Covered != 13 || len(coverage.Missing) != 0 || len(coverage.StoryPendingSceneOrders) != 13 {
		t.Fatalf("failed repair silently reported success: %+v", coverage)
	}
	if ep.OpeningState != `{"action":"三人开始追踪","place":"荒原入口"}` || ep.EndingState != `{"action":"村落灯火出现","place":"山谷出口"}` {
		t.Fatalf("first output root states not preserved: opening=%q ending=%q", ep.OpeningState, ep.EndingState)
	}
	var saved []models.Scene
	if err := svc.db.Where("project_id = ? AND episode_n = ? AND archived = ?", project.ID, 1, false).Order("`order`").Find(&saved).Error; err != nil {
		t.Fatal(err)
	}
	if len(saved) != 20 {
		t.Fatalf("saved scenes=%d, want original 20", len(saved))
	}
}
