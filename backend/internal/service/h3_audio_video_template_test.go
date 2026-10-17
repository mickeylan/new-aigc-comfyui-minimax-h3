package service

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

type audioVideoTemplateFixture struct {
	Code        string `json:"code"`
	Description string `json:"description"`
	Inputs      []struct {
		Key, Type string
		Required  bool
	} `json:"inputs"`
	Workflow map[string]struct {
		ClassType string         `json:"class_type"`
		Inputs    map[string]any `json:"inputs"`
	} `json:"workflow"`
}

func loadAudioVideoTemplateFixture(t *testing.T, name string) audioVideoTemplateFixture {
	t.Helper()
	data, err := os.ReadFile("templates/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var fixture audioVideoTemplateFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func requireConnection(t *testing.T, value any, node string, output float64) {
	t.Helper()
	connection, ok := value.([]any)
	if !ok || len(connection) != 2 || connection[0] != node || connection[1] != output {
		t.Fatalf("connection=%#v, want [%s,%v]", value, node, output)
	}
}

func TestH3AudioDrivenTemplateUsesReviewedAudioAsConditionAndFinalTrack(t *testing.T) {
	tpl := loadAudioVideoTemplateFixture(t, "minimax_h3_audio_driven_ref2v.json")
	if tpl.Code != h3AudioDrivenRef2VTemplate {
		t.Fatalf("code=%q", tpl.Code)
	}
	if tpl.Workflow["419"].ClassType != "LoadAudio" {
		t.Fatalf("audio loader=%+v", tpl.Workflow["419"])
	}
	conditioning := tpl.Workflow["416"]
	if conditioning.ClassType != "MiniMaxH3ReferenceToVideo" {
		t.Fatalf("conditioning=%s", conditioning.ClassType)
	}
	planner := tpl.Workflow["421"]
	if planner.ClassType != "IndexTTS25H3ReviewedAudioPlan" {
		t.Fatalf("reviewed audio planner=%+v", planner)
	}
	requireConnection(t, planner.Inputs["reviewed_audio"], "423", 0)
	preflight := tpl.Workflow["427"]
	if preflight.ClassType != "IndexTTS25H3ProductionPreflight" {
		t.Fatalf("production preflight=%+v", preflight)
	}
	requireConnection(t, preflight.Inputs["conditioned_prompt"], "421", 4)
	if _, manual := preflight.Inputs["picture_count"]; manual {
		t.Fatal("production preflight still trusts a manually entered reference count")
	}
	for i, node := range []string{"137", "139", "144", "145", "146", "147", "148", "149", "150"} {
		requireConnection(t, preflight.Inputs[fmt.Sprintf("ref_images.ref_image_%d", i)], node, 0)
	}
	requireConnection(t, conditioning.Inputs["prompt"], "427", 0)
	requireConnection(t, conditioning.Inputs["length"], "421", 1)
	requireConnection(t, conditioning.Inputs["ref_audios.ref_audio_0"], "421", 0)
	guide := tpl.Workflow["420"]
	if guide.ClassType != "MiniMaxH3AddGuide" || guide.Inputs["frame_idx"] != float64(0) {
		t.Fatalf("official audio guide=%+v", guide)
	}
	requireConnection(t, guide.Inputs["positive"], "416", 0)
	requireConnection(t, guide.Inputs["latent"], "416", 1)
	requireConnection(t, guide.Inputs["audio"], "421", 0)
	requireConnection(t, tpl.Workflow["397"].Inputs["positive"], "420", 0)
	if tpl.Workflow["422"].ClassType != "IndexTTS25LocalSaveReport" || tpl.Workflow["428"].ClassType != "IndexTTS25LocalSaveReport" {
		t.Fatal("audio and production preflight reports are not persisted")
	}
	window := tpl.Workflow["423"]
	if window.ClassType != "IndexTTS25H3AudioWindow" {
		t.Fatalf("audio window=%+v", window)
	}
	trim := tpl.Workflow["424"]
	if trim.ClassType != "IndexTTS25H3OutputTrim" {
		t.Fatalf("output trim=%+v", trim)
	}
	requireConnection(t, trim.Inputs["frames"], "405", 0)
	requireConnection(t, trim.Inputs["trim_start_seconds"], "423", 2)
	requireConnection(t, tpl.Workflow["418"].Inputs["images"], "424", 0)
	policy := tpl.Workflow["425"]
	if policy.ClassType != "IndexTTS25H3AudioPolicy" || policy.Inputs["policy"] != "{{audio_policy}}" {
		t.Fatalf("audio policy=%+v", policy)
	}
	requireConnection(t, policy.Inputs["source_audio"], "423", 1)
	requireConnection(t, policy.Inputs["generated_audio"], "408", 0)
	requireConnection(t, tpl.Workflow["418"].Inputs["audio"], "425", 0)
	if tpl.Workflow["418"].Inputs["trim_to_audio"] != false {
		t.Fatal("VHS must not perform a second implicit trim after exact frame trimming")
	}
	if !isH3VideoPromptTemplate(tpl.Code) || !isH3Ref2VTemplate(tpl.Code) {
		t.Fatal("audio-driven template is not protected by final H3 validation")
	}
}

func TestH3AudioDrivenTemplateAvoidsNonOfficialConditioningNode(t *testing.T) {
	tpl := loadAudioVideoTemplateFixture(t, "minimax_h3_audio_driven_ref2v.json")
	for id, node := range tpl.Workflow {
		if node.ClassType == "MiniMaxH3AudioConditioningT8" {
			t.Fatalf("node %s still uses non-official conditioning", id)
		}
	}
}

func TestH3VideoThenAudioMuxTemplateDoesNotConditionGenerationOnAudio(t *testing.T) {
	tpl := loadAudioVideoTemplateFixture(t, "minimax_h3_video_then_audio_mux.json")
	if tpl.Code != h3VideoThenAudioMuxTemplate {
		t.Fatalf("code=%q", tpl.Code)
	}
	if tpl.Workflow["416"].ClassType == "MiniMaxH3AudioConditioningT8" {
		t.Fatal("mux-only workflow incorrectly conditions video on audio")
	}
	requireConnection(t, tpl.Workflow["418"].Inputs["audio"], "419", 0)
	if tpl.Workflow["418"].Inputs["trim_to_audio"] != true {
		t.Fatal("mux output is not aligned to reviewed audio")
	}
	if !isH3VideoPromptTemplate(tpl.Code) || !isH3Ref2VTemplate(tpl.Code) {
		t.Fatal("mux template is not protected by final H3 validation")
	}
}
