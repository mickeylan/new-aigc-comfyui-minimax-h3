package service

import (
	"testing"

	"comfyui-console/internal/models"
)

func TestEditedSceneDialoguesUsesOnlyCurrentTextAndKeepsOccurrenceMetadata(t *testing.T) {
	sc := &models.Scene{ID: 7, ProjectID: 9, StoryContentEdited: true, Characters: "甲,乙", VisibleCharacters: "甲,乙", Content: `甲说道：“新句。”乙又说道：“重复。”随后再说：“重复。”`}
	rows := []models.Dialogue{
		{ID: 1, Text: "旧句。", Character: "旧角色", SpeechType: "offscreen_dialogue", Source: "ai_reviewed", AudioFile: "old.wav"},
		{ID: 2, Text: "新句。", Character: "旧归属", SpeechType: "dialogue", Source: "ai_reviewed"},
		{ID: 3, Text: "重复。", Character: "乙", SpeechType: "offscreen_dialogue", Source: "user", H3VoiceDescription: "keep-first"},
		{ID: 4, Text: "重复。", Character: "丙", SpeechType: "dialogue", Source: "user", H3VoiceDescription: "keep-second"},
	}

	got := editedSceneDialogues(sc, rows)
	if len(got) != 3 {
		t.Fatalf("current occurrence count=%d, dialogues=%+v", len(got), got)
	}
	if got[0].Text != "新句。" || got[0].Character != "甲" || got[0].Source == "ai_reviewed" {
		t.Fatalf("new explicit speaker did not override stale review: %+v", got[0])
	}
	if got[1].H3VoiceDescription != "keep-first" || got[2].H3VoiceDescription != "keep-second" || got[1].SpeechType != "offscreen_dialogue" {
		t.Fatalf("repeated occurrence metadata/type lost: %+v", got)
	}
	for _, d := range got {
		if d.Text == "旧句。" || d.AudioFile == "old.wav" {
			t.Fatalf("removed source text resurrected through DB/audio: %+v", got)
		}
	}
}

func TestEditedSceneDialoguesDeletedQuotesProduceNoDubs(t *testing.T) {
	sc := &models.Scene{StoryContentEdited: true, Content: "甲转身离开。"}
	got := editedSceneDialogues(sc, []models.Dialogue{{Text: "已经删除。", AudioFile: "old.wav", Source: "ai_reviewed"}})
	if len(got) != 0 {
		t.Fatalf("deleted dialogue survived: %+v", got)
	}
}

func TestSceneBlockingContextRequiresCurrentStoryAuthority(t *testing.T) {
	blocking := blockingJSON(SceneBlocking{Summary: "旧动作"})
	content := "甲站在门边。"
	current := &models.Scene{Content: content, BlockingJSON: blocking, BlockingContentHash: sceneDialogueContentHash(content), StoryContentEdited: true}
	if sceneBlockingPromptContext(current) == "" {
		t.Fatal("current saved blocking was skipped")
	}
	current.Content = "甲已经离开。"
	if sceneBlockingPromptContext(current) != "" {
		t.Fatal("stale blocking leaked into prompt context")
	}
	legacyEdited := &models.Scene{Content: content, BlockingJSON: blocking, StoryContentEdited: true}
	if sceneBlockingPromptContext(legacyEdited) != "" {
		t.Fatal("hashless blocking survived known story edit")
	}
}
