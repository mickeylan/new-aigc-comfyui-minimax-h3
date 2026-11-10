package service

import (
	"comfyui-console/internal/models"
	"fmt"
	"strings"
	"testing"
)

func TestH3PreviewBindsVisualNamesAndRestoresRetentionWithoutChangingSpeech(t *testing.T) {
	names := []string{"柳乐儿", "燕承", "齐姓道士", "马脸男子", "柳石"}
	refs := make([]string, len(names))
	for i, name := range names {
		refs[i] = fmt.Sprintf("- <Picture %d>：角色「%s」四视图", i+1, name)
	}
	definitions, _, _ := h3VideoSubjects(refs)
	speech := "<d>[Chinese] 在下血刀会燕承。</d>"
	body := "[Shot 1] 燕承、马脸男子与齐姓道士释放神识，灵光掠过柳石。\n[Shot 2] 燕承双唇闭合。\n[Shot 3] 柳石木然立在柳乐儿右侧。\n[Shot 4] 燕承正对柳石，背对镜头。<Subject 2> (S1) says: " + speech
	prompt := "subject_definitions:\n" + strings.Join(definitions, "\n") + "\n\nsummary:\nsummary\n\nretention_analysis:\n\n\ndetailed_description:\n" + body + "\n\noverall_soundscape:\nambience\n\nnon_diegetic_music:\nN/A"
	got := bindH3ReferencePreview(prompt, refs)
	detail := h3PromptSection(got, "detailed_description:")
	outsideSpeech := dialogueBlockRE.ReplaceAllString(detail, "")
	for i, name := range names {
		if strings.Contains(outsideSpeech, name) {
			t.Fatalf("raw visual name %s: %s", name, detail)
		}
		if !strings.Contains(h3PromptSection(got, "retention_analysis:"), fmt.Sprintf("<Subject %d>", i+1)) {
			t.Fatalf("missing retention for %s: %s", name, got)
		}
	}
	if !strings.Contains(detail, speech) {
		t.Fatalf("speech changed: %s", detail)
	}
	for _, fact := range []string{"<Subject 5>木然立在<Subject 1>右侧", "<Subject 2>正对<Subject 5>，背对镜头"} {
		if !strings.Contains(detail, fact) {
			t.Fatalf("spatial fact changed: %s", detail)
		}
	}
	if h3PromptSection(got, "subject_definitions:") != h3PromptSection(prompt, "subject_definitions:") {
		t.Fatal("reference definitions changed")
	}
	if bindH3ReferencePreview(got, refs) != got {
		t.Fatal("not idempotent")
	}
	if bindH3ReferencePreview(prompt, nil) != prompt {
		t.Fatal("invented bindings without refs")
	}
}

func TestExplicitMonologueRecoveryMatchesPersistedOccurrences(t *testing.T) {
	sc := &models.Scene{Characters: "燕承", Content: "燕承内心独白：“没有灵力？”\n燕承内心独白：“没有灵力？”"}
	persisted := []models.Dialogue{{ID: 1, Character: "燕承", SpeechType: "monologue", Text: "没有灵力？"}}
	got := mergeExplicitSceneSpeech(sc, persisted)
	if len(got) != 2 || got[0].ID != 1 {
		t.Fatalf("expected two source occurrences, one persisted: %+v", got)
	}
	got = mergeExplicitSceneSpeech(sc, got)
	if len(got) != 2 {
		t.Fatalf("duplicated recovered speech on next pass: %+v", got)
	}
}
