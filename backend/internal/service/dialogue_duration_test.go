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

func TestSceneActionBeatsCountsSilentActionsAndTimelineSegments(t *testing.T) {
	if got := sceneActionBeats("燕承拔刀上前。青年抬手扣住刀腕！燕承发力回抽却纹丝不动。"); got != 3 {
		t.Fatalf("silent action beats=%d, want 3", got)
	}
	if got := sceneActionBeats("齐姓道士眯起双眼传音：“这方圆万里，若非修为通天。”燕承的右手缓缓握紧朴刀。"); got != 1 {
		t.Fatalf("speech sentence must be timed by dialogue, beats=%d want 1", got)
	}
	if got := sceneActionBeats("【对白｜燕承】两位道友没什么事吧？\n燕承收刀回望。"); got != 1 {
		t.Fatalf("speech label lines must not count as actions, beats=%d", got)
	}
	timeline := "【0.00—1.00秒】大汉赶到草丛边。蛇群扑出。\n【1.00—2.00秒】拍出护罩。\n【2.00—6.00秒】挥刀斩蛇。"
	if got := sceneActionBeats(timeline); got != 4 {
		t.Fatalf("timeline beats=%d, want the 4 actions inside its segments", got)
	}
	if load, authored := sceneContentLoad(timeline, 3); !authored || load != 6 {
		t.Fatalf("authored timeline load=%.1f authored=%v, want 6/true", load, authored)
	}
}

func TestSceneDensityIssueFlagsOverloadedAndThinScenesOnly(t *testing.T) {
	overloaded := strings.Repeat("【0.00—1.00秒】大汉后仰卸力。左手拍出护罩。右腕翻刀横扫。\n", 2)
	if issue := sceneDensityIssue(overloaded, 0, 6); !strings.Contains(issue, "过密") {
		t.Fatalf("6 actions in 6 s must be flagged, got %q", issue)
	}
	if issue := sceneDensityIssue("风吹过齐人高的草丛。", 0, 7); !strings.Contains(issue, "过疏") {
		t.Fatalf("1 action in 7 s must be flagged, got %q", issue)
	}
	if issue := sceneDensityIssue("燕承拔刀上前。青年抬手扣住刀腕。燕承发力回抽却纹丝不动。", 0, 7.5); issue != "" {
		t.Fatalf("3 actions in 7.5 s is a normal load, got %q", issue)
	}
	if issue := sceneDensityIssue("齐姓道士眯眼传音：“这方圆万里，若非修为通天，便是真的傻子。”", 6.5, 8); issue != "" {
		t.Fatalf("a scene carried by speech is not thin, got %q", issue)
	}
}

func TestRebalanceWeightsByContentLoadAndCapsSparseScenes(t *testing.T) {
	speech := strings.Repeat("这一段对白必须完整说完", 8) + "。"
	result := &scriptResult{Scenes: []scriptScene{
		{Content: "风吹过草丛。", Duration: 7.5},
		{Content: "燕承拔刀上前。青年抬手扣住刀腕。燕承发力回抽却纹丝不动。", Duration: 7.5},
		{Content: "齐姓道士传音。", Duration: 7.5, Dialogues: []scriptDialogue{{Character: "齐姓道士", SpeechType: "dialogue", Text: speech}}},
	}}
	floor := sceneDialogueDurationFloor(result.Scenes[2])
	if rebalanceScriptDurations(result, 180) {
		t.Fatal("three scenes cannot honestly fill 180 seconds; the shortfall must be reported")
	}
	sparse, action, talk := result.Scenes[0].Duration, result.Scenes[1].Duration, result.Scenes[2].Duration
	if sparse > minSceneStretchCeiling {
		t.Fatalf("one-beat scene stretched to %.1f, cap %.1f", sparse, minSceneStretchCeiling)
	}
	if action < 7.5 || action > 7.5*sceneStretchFactor+0.05 {
		t.Fatalf("three-beat scene duration %.1f outside [7.5, %.2f]", action, 7.5*sceneStretchFactor)
	}
	if talk < floor+actionBeatSeconds {
		t.Fatalf("dialogue scene %.1f below speech floor %.1f plus its action beat", talk, floor)
	}
	if sparse >= action {
		t.Fatalf("sparse scene %.1f should be shorter than the loaded scene %.1f", sparse, action)
	}
}

func TestRebalanceNeverStretchesActionHeavySceneBeyondOneShot(t *testing.T) {
	result := &scriptResult{Scenes: []scriptScene{{Content: strings.Repeat("燕承挥刀横斩逼退青蛇。", 30), Duration: 12}}}
	rebalanceScriptDurations(result, 180)
	if got := result.Scenes[0].Duration; got > maxSceneVideoDuration {
		t.Fatalf("30 silent beats stretched the scene to %.1f s; overload must stay within %.0f s", got, maxSceneVideoDuration)
	}
}

func TestMinDurationKeepsStrippedDialogueFloorWithoutSplitting(t *testing.T) {
	long := []scriptDialogue{{Character: "马脸男子", SpeechType: "dialogue", Text: strings.Repeat("这妖孽当真狡猾得紧，", 14)}}
	floor := scriptDialoguesMinDuration(long)
	if floor <= maxSceneVideoDuration {
		t.Fatalf("fixture floor %.1f must exceed one shot", floor)
	}
	result := &scriptResult{Scenes: []scriptScene{{Content: "马脸男子急切追问。", Duration: 7.5, MinDuration: floor}}}
	splitScenesForDialogueDuration(result)
	if len(result.Scenes) != 1 {
		t.Fatalf("rows persisted separately must not be split here, got %d scenes", len(result.Scenes))
	}
	rebalanceScriptDurations(result, 180)
	if result.Scenes[0].Duration < floor {
		t.Fatalf("duration %.1f below stripped dialogue floor %.1f", result.Scenes[0].Duration, floor)
	}
}

func TestValidateScriptAllowsEpisodeToExceedReferenceDuration(t *testing.T) {
	result := &scriptResult{Script: "正文", VisualBible: "视觉", Scenes: []scriptScene{{Content: "动作", ImagePrompt: "画面", Duration: 15}, {Content: "动作", ImagePrompt: "画面", Duration: 15}}}
	if err := validateScriptResult(result, 20.0, 1); err != nil {
		t.Fatalf("reference duration must not be a hard maximum: %v", err)
	}
}
