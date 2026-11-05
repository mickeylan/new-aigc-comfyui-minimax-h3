package service

import (
	"context"
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

type h3DialogueRepairProvider struct{ responses, users []string }

func (p *h3DialogueRepairProvider) Name() string                      { return "h3-dialogue-test" }
func (p *h3DialogueRepairProvider) HealthCheck(context.Context) error { return nil }
func (p *h3DialogueRepairProvider) Chat(_, user string) (string, error) {
	p.users = append(p.users, user)
	i := len(p.users) - 1
	if i >= len(p.responses) {
		i = len(p.responses) - 1
	}
	return p.responses[i], nil
}

func TestComposeH3DialogueTimelineUsesJSONPlanAndActualShots(t *testing.T) {
	visual := "[Shot 1] A独自站着。\n[Shot 3] 门打开。\n[Shot 7] B走进来。"
	p := &h3DialogueRepairProvider{responses: []string{"```json\n{\"placements\":[{\"index\":0,\"shot\":3},{\"index\":1,\"shot\":7}]}\n```"}}
	dubs := []models.Dialogue{{Character: "A", Text: "你来了？", Order: 1}, {Character: "B", Text: "是的。", Order: 2}}
	got, err := (&ProjectService{textProvider: p}).composeH3DialogueTimeline(&models.Scene{Content: "A问，B回答。"}, visual, dubs, []string{"背景", "道具", "角色「A」", "角色「B」"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Index(got, "你来了？") < strings.Index(got, "[Shot 3]") || strings.Index(got, "是的。") < strings.Index(got, "[Shot 7]") {
		t.Fatalf("wrong placement: %s", got)
	}
	if !strings.Contains(got, "<Subject 3> (S1)") || strings.Contains(got, "force") {
		t.Fatalf("wrong rendering: %s", got)
	}
}

func TestComposeH3DialogueTimelinePreservesKnownPlacementWithoutDuplicates(t *testing.T) {
	visual := "[Shot 1] A看门。 <Subject 1> (S1) says: <d>[Chinese] 已经来了。</d>.\n[Shot 2] B进门。"
	p := &h3DialogueRepairProvider{responses: []string{"{\"placements\":[{\"index\":0,\"shot\":1},{\"index\":1,\"shot\":2}]}"}}
	dubs := []models.Dialogue{{Character: "A", Text: "已经来了。"}, {Character: "B", Text: "你好。"}}
	got, err := (&ProjectService{textProvider: p}).composeH3DialogueTimeline(&models.Scene{}, visual, dubs, []string{"角色「A」", "角色「B」"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(got, "已经来了。") != 1 || strings.Count(got, "你好。") != 1 {
		t.Fatalf("duplicates: %s", got)
	}
	if !strings.Contains(p.users[0], `"0":1`) {
		t.Fatalf("known placement absent: %s", p.users[0])
	}
}

func TestComposeH3DialogueTimelineRendersSpeechTypesWithoutWrongActor(t *testing.T) {
	p := &h3DialogueRepairProvider{responses: []string{"{\"placements\":[{\"index\":0,\"shot\":1},{\"index\":1,\"shot\":1}]}"}}
	dubs := []models.Dialogue{{Character: "旁白", SpeechType: "narration", Text: "夜幕降临。"}, {Character: "A", SpeechType: "offscreen_dialogue", Text: "别动。"}}
	got, err := (&ProjectService{textProvider: p}).composeH3DialogueTimeline(&models.Scene{}, "[Shot 1] <Subject 9>站着。", dubs, []string{"背景", "道具", "角色「A」"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "narrates:") || !strings.Contains(got, "<Subject 3> (S2) says in a clearly identified off-screen voice") || strings.Contains(got, "<Subject 9> (S2)") {
		t.Fatalf("%s", got)
	}
}

func TestComposeH3DialogueTimelineRetriesInvalidAndRejectsExtraQuote(t *testing.T) {
	p := &h3DialogueRepairProvider{responses: []string{"not json", "{\"placements\":[{\"index\":0,\"shot\":1},{\"index\":1,\"shot\":1}]}"}}
	_, err := (&ProjectService{textProvider: p}).composeH3DialogueTimeline(&models.Scene{}, "[Shot 1] 空镜。", []models.Dialogue{{Character: "A", Text: "一句。"}}, nil)
	if err == nil || len(p.users) != 2 || !strings.Contains(err.Error(), "AI未能完整安排对白") {
		t.Fatalf("err=%v calls=%d", err, len(p.users))
	}
}

func TestComposeH3DialogueTimelineZeroValidDoesNotCallAI(t *testing.T) {
	p := &h3DialogueRepairProvider{responses: []string{"bad"}}
	visual := "[Shot 1] 空镜。"
	got, err := (&ProjectService{textProvider: p}).composeH3DialogueTimeline(&models.Scene{}, visual, []models.Dialogue{{Text: ""}}, nil)
	if err != nil || got != visual || len(p.users) != 0 {
		t.Fatalf("got=%q err=%v calls=%d", got, err, len(p.users))
	}
}
