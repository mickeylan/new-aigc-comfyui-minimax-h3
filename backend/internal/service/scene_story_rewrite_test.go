package service

import (
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

func TestSceneStoryEvidenceKeepsAttributionAndReactionAndExcludesOtherChapter(t *testing.T) {
	dialogues := []models.Dialogue{{ID: 7, Text: "妖孽哪里跑", Character: "", SpeechType: "offscreen_dialogue"}}
	chapters := []models.Chapter{
		{ID: 1, Order: 1, Content: "林中风声骤紧。\n“妖孽哪里跑”\n就在此时后方远处男子厉喝。\n虬髯客闻声回头，脚下一顿。\n再后一段不应带入。"},
		{ID: 2, Order: 2, Content: "整本书其他章节秘密"},
	}
	got, err := sceneStoryEvidence(dialogues, chapters)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"妖孽哪里跑", "就在此时后方远处男子厉喝", "虬髯客闻声回头"} {
		if !strings.Contains(got, want) {
			t.Fatalf("evidence missing %q: %s", want, got)
		}
	}
	for _, unwanted := range []string{"再后一段不应带入", "整本书其他章节秘密"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("evidence leaked %q: %s", unwanted, got)
		}
	}
}

func TestSceneStoryEvidenceRepeatedExactQuoteUsesDistinctOccurrences(t *testing.T) {
	dialogues := []models.Dialogue{
		{ID: 1, Order: 1, Text: "在那里", Character: "齐师兄", SpeechType: "dialogue"},
		{ID: 2, Order: 2, Text: "在那里", Character: "马师弟", SpeechType: "dialogue"},
	}
	chapters := []models.Chapter{{ID: 9, Order: 3, Content: "齐师兄说道：\n“在那里”\n齐师兄抬手。\n“别动”\n马师弟说道：\n“在那里”\n马师弟转身。"}}
	got, err := sceneStoryEvidence(dialogues, chapters)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(got, `“在那里”`) != 2 {
		t.Fatalf("expected two distinct exact occurrences: %s", got)
	}
	first, second := strings.Index(got, "speaker_attribution=齐师兄"), strings.Index(got, "speaker_attribution=马师弟")
	if first < 0 || second <= first {
		t.Fatalf("speaker-matched occurrences not kept in scene order: %s", got)
	}
	if strings.Contains(got, `“别动”`) {
		t.Fatalf("future dialogue leaked into evidence: %s", got)
	}
}

func TestSceneStoryEvidenceCapturesPrecedingAttributionParagraph(t *testing.T) {
	dialogues := []models.Dialogue{{ID: 3, Text: "虽然如此", Character: "齐师兄", SpeechType: "dialogue"}}
	chapters := []models.Chapter{{ID: 2, Order: 1, Content: "齐师兄阴沉说道：\n“虽然如此”\n众人沉默。"}}
	got, err := sceneStoryEvidence(dialogues, chapters)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "齐师兄阴沉说道：\n“虽然如此”") {
		t.Fatalf("preceding attribution missing: %s", got)
	}
}

func TestSceneStoryEvidenceDuplicateAcrossChaptersKeepsAllCandidates(t *testing.T) {
	dialogues := []models.Dialogue{{ID: 5, Text: "在那里", SpeechType: "dialogue"}}
	chapters := []models.Chapter{
		{ID: 11, Order: 4, Content: "齐师兄道：“在那里”"},
		{ID: 12, Order: 5, Content: "马师弟道：“在那里”"},
	}
	got, err := sceneStoryEvidence(dialogues, chapters)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"不得随意选定唯一出处", "chapter_id=11 order=4", "chapter_id=12 order=5", "source_offset="} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing duplicate-candidate detail %q: %s", want, got)
		}
	}
}

func TestValidateSceneStoryRewriteExactDialogueAndNoMechanicalAppendix(t *testing.T) {
	ds := []models.Dialogue{{ID: 4, Text: "妖孽哪里跑", SpeechType: "offscreen_dialogue"}}
	good := sceneStoryRewriteResult{Content: "就在此时，后方远处追兵男子厉喝：“妖孽哪里跑”虬髯客闻声回头。", Dialogues: []sceneStoryDialogue{{ID: 4, Speaker: "远处追兵男子", SpeechType: "offscreen_dialogue", Text: "妖孽哪里跑"}}}
	if err := validateSceneStoryRewrite(good, ds); err != nil {
		t.Fatal(err)
	}
	cases := []sceneStoryRewriteResult{
		{Content: "虬髯客回头。", Dialogues: good.Dialogues},
		{Content: good.Content + "“站住”", Dialogues: good.Dialogues},
		{Content: good.Content + "\n【对白】妖孽哪里跑", Dialogues: good.Dialogues},
	}
	for i, tc := range cases {
		if validateSceneStoryRewrite(tc, ds) == nil {
			t.Fatalf("case %d unexpectedly valid", i)
		}
	}
}
