package service

import (
	"encoding/json"
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

func TestSceneStoryGenerationAddsMissingDialogueToVisualSynopsis(t *testing.T) {
	provider := &adaptationMockTextProvider{}
	svc, project, chapter := newAdaptationDialogueTestService(t, provider)
	ledger := extractSourceDialogues(chapter, "虬髯大汉", "马脸男子", "齐姓道士")
	raw := adaptationScriptFixture(t, "", ledger, true)
	var wire map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &wire); err != nil {
		t.Fatal(err)
	}
	var scenes []scriptScene
	if err := json.Unmarshal(wire["scenes"], &scenes); err != nil {
		t.Fatal(err)
	}
	// Use distinct visual synopses: twenty identical scenes would correctly
	// trigger the unrelated duplicate-scene repair before testing content assembly.
	visualOnly, err := parseScriptJSON(adaptationScriptFixture(t, "", ledger, false))
	if err != nil {
		t.Fatal(err)
	}
	for i := range scenes {
		scenes[i].Content = visualOnly.Scenes[i].Content
	}
	wire["scenes"], _ = json.Marshal(scenes)
	input, _ := json.Marshal(wire)
	provider.responses = []string{string(input)}
	ep, err := svc.GenerateScript(project.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	var saved []models.Scene
	if err := svc.db.Where("project_id = ? AND episode_n = ? AND archived = ?", project.ID, 1, false).Order("`order`").Find(&saved).Error; err != nil {
		t.Fatal(err)
	}
	for i, d := range ledger {
		want := "【对白｜" + d.Speaker + "】" + d.Text
		if !strings.Contains(saved[i].Content, want) || strings.Count(saved[i].Content, d.Text) != 1 {
			t.Fatalf("scene %d does not contain one readable source dialogue: %s", i+1, saved[i].Content)
		}
		if !strings.Contains(ep.Script, want) {
			t.Fatalf("episode script missing scene %d speech", i+1)
		}
		dubs := svc.projects.sceneVideoDialogues(&saved[i])
		if len(dubs) != 1 || dubs[0].Text != d.Text {
			t.Fatalf("scene %d downstream duplicated/lost speech: %+v", i+1, dubs)
		}
	}
}

func TestSceneStoryDialogueIntegrationPersistsReadableStoryAndFallbackScript(t *testing.T) {
	provider := &adaptationMockTextProvider{}
	svc, project, chapter := newAdaptationDialogueTestService(t, provider)
	ledger := extractSourceDialogues(chapter, "虬髯大汉", "马脸男子", "齐姓道士")
	if len(ledger) != 13 {
		t.Fatalf("fixture ledger=%d, want 13", len(ledger))
	}
	provider.responses = []string{adaptationScriptFixture(t, "", ledger, true)}

	ep, err := svc.GenerateScript(project.ID, 1)
	if err != nil {
		t.Fatal(err)
	}

	var scenes []models.Scene
	if err := svc.db.Where("project_id = ? AND episode_n = ? AND archived = ?", project.ID, 1, false).Order("`order`").Find(&scenes).Error; err != nil {
		t.Fatal(err)
	}
	if len(scenes) != 20 {
		t.Fatalf("stored scenes=%d, want 20", len(scenes))
	}

	totalVideoDialogues := 0
	for i := range scenes {
		dialogues := svc.projects.sceneVideoDialogues(&scenes[i])
		totalVideoDialogues += len(dialogues)
		if i >= len(ledger) {
			continue
		}
		want := ledger[i]
		labelled := want.Speaker + "说道：" + want.Text
		if !strings.Contains(scenes[i].Content, labelled) {
			t.Errorf("scene %d content lacks exact labelled source dialogue %q: %q", i+1, labelled, scenes[i].Content)
		}
		if occurrences := strings.Count(scenes[i].Content, want.Text); occurrences != 1 {
			t.Errorf("scene %d source text occurrences=%d, want 1: %q", i+1, occurrences, scenes[i].Content)
		}
		if len(dialogues) != 1 || dialogues[0].Character != want.Speaker || dialogues[0].Text != want.Text {
			t.Errorf("scene %d video dialogues=%+v, want one exact %q/%q", i+1, dialogues, want.Speaker, want.Text)
		}
	}
	if totalVideoDialogues != len(ledger) {
		t.Fatalf("sceneVideoDialogues total=%d, want %d (Content and rows must not double count)", totalVideoDialogues, len(ledger))
	}

	var storedProject models.Project
	if err := svc.db.First(&storedProject, project.ID).Error; err != nil {
		t.Fatal(err)
	}
	var scripts map[int]string
	if err := json.Unmarshal([]byte(storedProject.Scripts), &scripts); err != nil {
		t.Fatalf("decode project scripts %q: %v", storedProject.Scripts, err)
	}
	if strings.TrimSpace(ep.Script) == "" || strings.TrimSpace(storedProject.Script) == "" || strings.TrimSpace(scripts[1]) == "" {
		t.Fatalf("empty fallback persisted: episode=%q project=%q scripts[1]=%q", ep.Script, storedProject.Script, scripts[1])
	}
	for _, d := range ledger {
		for name, text := range map[string]string{"episode script": ep.Script, "project script": storedProject.Script, "project scripts[1]": scripts[1]} {
			if !strings.Contains(text, d.Speaker+"说道："+d.Text) {
				t.Errorf("%s lacks exact labelled source dialogue %q", name, d.Speaker+"说道："+d.Text)
			}
		}
	}

	if err := svc.db.AutoMigrate(&models.SceneContinuity{}, &models.Shot{}, &models.FrameCandidate{}, &models.Asset{}); err != nil {
		t.Fatal(err)
	}
	contextService := &Service{DB: svc.db}
	for i := 0; i < len(ledger); i++ {
		context, err := contextService.sceneContentExpandContext(&scenes[i])
		if err != nil {
			t.Fatalf("scene %d expansion context: %v", i+1, err)
		}
		if !strings.Contains(context, ledger[i].Text) {
			t.Errorf("scene %d expansion context lacks source dialogue %q", i+1, ledger[i].Text)
		}
	}
}
