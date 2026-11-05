package service

import (
	"math"
	"strings"
	"testing"
)

func TestScriptDialoguesMinDurationIncludesSpeechPausesAndSpeakerChanges(t *testing.T) {
	dialogues := []scriptDialogue{
		{Character: "甲", SpeechType: "dialogue", Text: "你终于来了。"},
		{Character: "乙", SpeechType: "dialogue", Text: "路上耽搁了一会儿！"},
	}
	got := scriptDialoguesMinDuration(dialogues)
	speech := dialogueTextDuration(dialogues[0].Text) + dialogueTextDuration(dialogues[1].Text)
	want := math.Ceil((speech+dialogueOpeningClosingMargin+speakerChangePause)*2) / 2
	if got != want || got > 3 {
		t.Fatalf("unexpected duration %.2f", got)
	}
}

func TestSplitScenesForDialogueDurationPreservesTextAndCapsEachShot(t *testing.T) {
	original := strings.Repeat("这是一段必须完整保留的对白。", 24)
	result := &scriptResult{Scenes: []scriptScene{{Title: "长对白", Content: "人物持续讲话", ImagePrompt: "人物近景", Duration: 5, Dialogues: []scriptDialogue{{Character: "甲", SpeechType: "dialogue", Text: original}}}}}
	splitScenesForDialogueDuration(result)
	if len(result.Scenes) < 2 {
		t.Fatalf("expected split, got %d scene", len(result.Scenes))
	}
	var rebuilt strings.Builder
	for i, scene := range result.Scenes {
		if scene.Duration > 15 {
			t.Fatalf("scene %d duration %.1f exceeds cap", i, scene.Duration)
		}
		for _, dialogue := range scene.Dialogues {
			rebuilt.WriteString(dialogue.Text)
		}
	}
	if rebuilt.String() != original {
		t.Fatalf("dialogue changed during split\nwant=%q\ngot=%q", original, rebuilt.String())
	}
}

func TestShortDramaDialogueEstimateAvoidsExcessiveSplits(t *testing.T) {
	original := strings.Repeat("这是一段必须完整保留的对白。", 8)
	result := &scriptResult{Scenes: []scriptScene{{Duration: 5, Dialogues: []scriptDialogue{{Character: "甲", Text: original}}}}}
	splitScenesForDialogueDuration(result)
	if len(result.Scenes) != 1 || result.Scenes[0].Duration > 15 {
		t.Fatalf("ordinary dialogue split unnecessarily: %+v", result.Scenes)
	}
	if result.Scenes[0].Dialogues[0].Text != original {
		t.Fatal("dialogue modified")
	}
	if got := dialogueTextDuration(strings.Repeat("字", 90)); got != 10 {
		t.Fatalf("90 chars estimated as %.2f instead of 10 seconds", got)
	}
	if dialogueTextDuration("哪里走！！！") != dialogueTextDuration("哪里走！") {
		t.Fatal("repeated punctuation inflated pauses")
	}
	if dialogueTextDuration("<d>[Chinese]别走！</d><scenetrans><cutoff>") != dialogueTextDuration("别走！") {
		t.Fatal("control tags counted as speech")
	}
}

func TestRebalanceDoesNotCompressDialogueToEpisodeTarget(t *testing.T) {
	text := strings.Repeat("自然语速对白", 8)
	result := &scriptResult{Scenes: []scriptScene{{Duration: 3, Dialogues: []scriptDialogue{{Character: "甲", Text: text}}}}}
	floor := sceneDialogueDurationFloor(result.Scenes[0])
	rebalanceScriptDurations(result, 5)
	if result.Scenes[0].Duration < floor {
		t.Fatalf("duration %.1f below dialogue floor %.1f", result.Scenes[0].Duration, floor)
	}
}

func TestValidateScriptAllowsEpisodeToExceedReferenceDuration(t *testing.T) {
	result := &scriptResult{Script: "正文", VisualBible: "视觉", Scenes: []scriptScene{{Content: "动作", ImagePrompt: "画面", Duration: 15}, {Content: "动作", ImagePrompt: "画面", Duration: 15}}}
	if err := validateScriptResult(result, 20.0, 1); err != nil {
		t.Fatalf("reference duration must not be a hard maximum: %v", err)
	}
}
