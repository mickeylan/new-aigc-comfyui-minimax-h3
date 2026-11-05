package service

import (
	"reflect"
	"testing"

	"comfyui-console/internal/models"
)

func TestWithSceneDialogueTimingUsesExplicitBracketedSourceIntervals(t *testing.T) {
	sc := &models.Scene{Duration: 14, Content: `【0—2秒】甲说道：“A。”
【2—4.2秒】众人转身。
【4.2—11.8秒】乙答道：“B！”
【11.8—14秒】甲说：“C？”`}
	in := []models.Dialogue{
		{ID: 10, SceneID: 7, Character: "甲", Text: "A。"},
		{ID: 11, SceneID: 7, Character: "乙", Text: "B！"},
		{ID: 12, SceneID: 7, Character: "甲", Text: "C？"},
	}
	before := append([]models.Dialogue(nil), in...)
	got := withSceneDialogueTiming(sc, in)
	if got[0].TimeSegment != "0—2秒" || got[1].TimeSegment != "4.2—11.8秒" || got[2].TimeSegment != "11.8—14秒" {
		t.Fatalf("timings=%q, %q, %q", got[0].TimeSegment, got[1].TimeSegment, got[2].TimeSegment)
	}
	if !reflect.DeepEqual(in, before) {
		t.Fatalf("input mutated: before=%+v after=%+v", before, in)
	}
	if got[1].ID != 11 || got[1].SceneID != 7 {
		t.Fatalf("record identity was not preserved: %+v", got[1])
	}
}

func TestWithSceneDialogueTimingSingleFullRangeAndExplicitPriority(t *testing.T) {
	sc := &models.Scene{Duration: 3, Content: `0—3秒｜甲说：“整句。”`}
	got := withSceneDialogueTiming(sc, []models.Dialogue{{Text: "整句。"}, {Text: "别处引用", SpeechType: "offscreen_dialogue"}})
	if got[0].TimeSegment != "0—3秒" || got[1].TimeSegment != "" {
		t.Fatalf("unexpected timings: %+v", got)
	}

	got = withSceneDialogueTiming(sc, []models.Dialogue{{Text: "整句。", TimeSegment: "00:00.500-00:02.500"}})
	if got[0].TimeSegment != "00:00.500-00:02.500" {
		t.Fatalf("valid explicit timing replaced: %+v", got[0])
	}
}

func TestWithSceneDialogueTimingSkipsAmbiguousRepeatedTextCountMismatch(t *testing.T) {
	sc := &models.Scene{Duration: 4, Content: `0—2秒｜甲说：“一样。”
2—4秒｜乙说：“一样。”`}
	// This can represent an excluded first occurrence or second occurrence; do not guess.
	got := withSceneDialogueTiming(sc, []models.Dialogue{{ID: 2, Text: "一样。"}})
	if got[0].TimeSegment != "" {
		t.Fatalf("ambiguous occurrence assigned: %+v", got[0])
	}
}

func TestWithSceneDialogueTimingRejectsInvalidSceneRanges(t *testing.T) {
	for _, sc := range []*models.Scene{
		{Duration: 5, Content: "0—3秒｜甲说：“话。”\n2—5秒｜结尾。"},
		{Duration: 4, Content: "0—5秒｜甲说：“话。”"},
	} {
		got := withSceneDialogueTiming(sc, []models.Dialogue{{Text: "话。"}})
		if got[0].TimeSegment != "" {
			t.Fatalf("invalid timeline assigned: scene=%q got=%+v", sc.Content, got[0])
		}
	}
}
