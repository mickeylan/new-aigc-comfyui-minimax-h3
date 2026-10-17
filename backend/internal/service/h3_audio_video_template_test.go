package service

import (
	"encoding/json"
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
	if conditioning.ClassType != "MiniMaxH3AudioConditioningT8" {
		t.Fatalf("conditioning=%s", conditioning.ClassType)
	}
	if conditioning.Inputs["audio_mode"] != "lock_source" || conditioning.Inputs["audio_denoise_strength"] != float64(0) {
		t.Fatalf("source audio is not locked: %+v", conditioning.Inputs)
	}
	requireConnection(t, conditioning.Inputs["drive_audio"], "419", 0)
	requireConnection(t, conditioning.Inputs["final_audio"], "419", 0)
	requireConnection(t, tpl.Workflow["418"].Inputs["audio"], "416", 2)
	if tpl.Workflow["418"].Inputs["trim_to_audio"] != true {
		t.Fatal("final video is not trimmed to reviewed audio")
	}
	if !isH3VideoPromptTemplate(tpl.Code) || !isH3Ref2VTemplate(tpl.Code) {
		t.Fatal("audio-driven template is not protected by final H3 validation")
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
