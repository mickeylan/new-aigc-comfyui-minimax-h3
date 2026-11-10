package service

import (
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

func TestH3PlannedMonologueClosesMouthOnlyDuringInnerVoice(t *testing.T) {
	refs := []string{"角色「柳乐儿」四视图", "角色「燕承」四视图", "角色「齐姓道士」四视图"}
	dubs := []models.Dialogue{
		{Character: "燕承", SpeechType: "monologue", Text: "怎么没有灵力？", Order: 1},
		{Character: "燕承", SpeechType: "dialogue", Text: "不知阁下如何称呼？", Order: 2},
	}
	visual := "[Shot 1] The group observes the stone.\n[Shot 2] <Subject 2> frowns slightly. His gaze settles on the stranger.\n[Shot 3] The girl holds the rattle.\n[Shot 4] <Subject 2> steps forward. The stranger looks back."
	plan := h3DialoguePlan{Placements: []h3DialoguePlanPlacement{{Index: 0, Shot: 2, After: "<Subject 2> frowns slightly."}, {Index: 1, Shot: 4, After: "<Subject 2> steps forward."}}}
	got := applyH3DialoguePlan(visual, dubs, refs, plan)
	two, ok := h3ShotBody(got, 2)
	if !ok {
		t.Fatal("missing Shot 2")
	}
	want := renderStructuredDialogue(dubs[0], 1, refs)
	if !strings.Contains(two, want) || !strings.Contains(two, "while their lips remain completely closed") {
		t.Fatalf("planned monologue lost closed-mouth contract: %s", got)
	}
	if strings.Index(two, "<d>[Chinese] 怎么没有灵力？</d>") > strings.Index(two, "His gaze settles") {
		t.Fatalf("lost inline placement: %s", got)
	}
	four, ok := h3ShotBody(got, 4)
	if !ok {
		t.Fatal("missing Shot 4")
	}
	if !strings.Contains(four, "<Subject 2> (S1) says: <d>[Chinese] 不知阁下如何称呼？</d>") || strings.Contains(four, "lips remain completely closed") {
		t.Fatalf("later spoken dialogue incorrectly silenced: %s", four)
	}
	clean := stripStructuredDialogueFromAction(got, dubs)
	if strings.Contains(clean, "<d>") || strings.Contains(clean, "while their lips remain completely closed") {
		t.Fatalf("render/strip roundtrip left audio instructions: %s", clean)
	}
	contract := h3SoundscapeContractForDialogues(dubs)
	for _, s := range []string{"audio-only inner voice", "no lip-sync", "other intervals"} {
		if !strings.Contains(contract, s) {
			t.Fatalf("missing %q: %s", s, contract)
		}
	}
}

func TestH3MonologuePlanningPreservesCrossShotMarkersAndOriginalQuote(t *testing.T) {
	d := models.Dialogue{Character: "燕承", SpeechType: "monologue", Text: "怎么没有灵力？<scenetrans><cutoff>"}
	got := renderH3PlannedDialogue(d, 1, []string{"角色「燕承」四视图"})
	if got != renderStructuredDialogue(d, 1, []string{"角色「燕承」四视图"}) || strings.Count(got, "<d>[Chinese] 怎么没有灵力？</d>") != 1 || !strings.HasSuffix(got, "<scenetrans><cutoff>") {
		t.Fatalf("inconsistent monologue rendering: %s", got)
	}
}

func TestH3ActionGenerationReceivesTypedMixedSpeechContext(t *testing.T) {
	ps := newTestProjectService(t)
	if err := ps.db.AutoMigrate(&models.Shot{}, &models.Dialogue{}, &models.Character{}); err != nil {
		t.Fatal(err)
	}
	provider := &captureTextProvider{response: "[Shot 1] 燕承 frowns slightly, then takes one step toward the stranger. The camera remains static."}
	ps.textProvider = provider
	p := models.Project{Title: "mixed thought and dialogue"}
	if err := ps.db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	if err := ps.db.Create(&models.Character{ProjectID: p.ID, Name: "燕承", Sheet: "yan.png"}).Error; err != nil {
		t.Fatal(err)
	}
	sc := models.Scene{ProjectID: p.ID, EpisodeN: 1, Characters: "燕承", Duration: 7, Content: "燕承心中暗道：‘怎么没有灵力？’随后上前问道：‘不知阁下如何称呼？’"}
	if err := ps.db.Create(&sc).Error; err != nil {
		t.Fatal(err)
	}
	for i, d := range []models.Dialogue{{Character: "燕承", SpeechType: "monologue", Text: "怎么没有灵力？"}, {Character: "燕承", SpeechType: "dialogue", Text: "不知阁下如何称呼？"}} {
		d.ProjectID = p.ID
		d.SceneID = sc.ID
		d.Order = i + 1
		if err := ps.db.Create(&d).Error; err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ps.GenerateSceneVideoAction(&sc); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"[speech_type=monologue] 燕承：怎么没有灵力？", "[speech_type=dialogue] 燕承：不知阁下如何称呼？"} {
		if !strings.Contains(provider.user, want) {
			t.Fatalf("missing typed speech %q: %s", want, provider.user)
		}
	}
	if !strings.Contains(provider.system, "闭口要求仅作用于对应语句/时段") {
		t.Fatalf("missing interval-scoped contract: %s", provider.system)
	}
}
