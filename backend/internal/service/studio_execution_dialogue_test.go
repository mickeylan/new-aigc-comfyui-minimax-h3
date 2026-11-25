package service

import (
	"comfyui-console/internal/models"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestStudioExecutionSourceHashIncludesDialogueEdits(t *testing.T) {
	s, db, scene, shots := newShotRefineTestService(t, "")
	row := models.Dialogue{SceneID: scene.ID, ProjectID: scene.ProjectID, Order: 1, Character: "画外乙", SpeechType: "offscreen_dialogue", Text: "别动😀。", TimeSegment: "0.5-2秒"}
	var seeded models.Dialogue
	if err := db.Where("scene_id = ?", scene.ID).First(&seeded).Error; err != nil {
		t.Fatal(err)
	}
	row.ID = seeded.ID // Reuse the shared fixture's existing canonical dialogue, do not add a second row.
	if err := db.Save(&row).Error; err != nil {
		t.Fatal(err)
	}
	ranges := []models.ShotDialogueRange{{DialogueID: row.ID, GroupKey: "line", StartRune: 0, EndRune: 4}}
	shots[0].DialogueRanges = ranges
	if err := db.Save(&shots[0]).Error; err != nil {
		t.Fatal(err)
	}
	source, hash, err := s.studioExecutionSource(scene.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(source.Dialogues) != 1 || len(source.DialogueTimeline.Lines) != 1 || source.DialogueTimeline.Lines[0].Text != row.Text {
		t.Fatalf("%+v", source.DialogueTimeline)
	}
	if err := db.Model(&row).Update("text", "新的台词").Error; err != nil {
		t.Fatal(err)
	}
	_, latest, err := s.studioExecutionSource(scene.ID)
	if err != nil || hash == latest {
		t.Fatalf("dialogue edit did not invalidate hash: err=%v", err)
	}
}

func TestStudioExecutionDialogueRejectsOverlappingSingleCaptionTiming(t *testing.T) {
	sc, shots, rows := executionDialogueFixture()
	rows = append(rows, models.Dialogue{ID: 4, Character: "乙", SpeechType: "dialogue", Text: "好。", TimeSegment: "1-2.5秒"})
	shots[0].DialogueRanges = append(shots[0].DialogueRanges, models.ShotDialogueRange{DialogueID: 4, GroupKey: "second", StartRune: 0, EndRune: 2})
	result := buildStudioExecutionDialogueTimeline(sc, shots, rows)
	if len(result.Lines) != 1 || !strings.Contains(strings.Join(result.Blockers, ""), "重叠") {
		t.Fatalf("%+v", result)
	}
}

func TestStudioExecutionDiscardsModelReplyWhenDialogueChanges(t *testing.T) {
	s, db, scene, _ := newShotRefineTestService(t, "")
	source, hash, err := s.studioExecutionSource(scene.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(executionPlanFixture(source, hash))
	provider := &executionChangingProvider{stubTextProvider: stubTextProvider{response: string(raw)}, change: func() {
		if err := db.Model(&models.Dialogue{}).Where("scene_id = ?", scene.ID).Update("time_segment", "1-2秒").Error; err != nil {
			t.Error(err)
		}
	}}
	useExecutionProvider(t, s, provider)
	w := executionHTTP(t, s, scene.ProjectID, scene.ID, "POST", map[string]any{"source_hash": hash, "context": executionContextFixture()})
	if w.Code != 409 || provider.calls != 1 {
		t.Fatalf("status=%d calls=%d body=%s", w.Code, provider.calls, w.Body.String())
	}
}

func executionDialogueFixture() (models.Scene, []models.Shot, []models.Dialogue) {
	scene := models.Scene{ID: 2, ProjectID: 1, Duration: 6, Content: "甲说：‘别动😀。’"}
	d := models.Dialogue{ID: 3, ProjectID: 1, SceneID: 2, Order: 1, Character: "甲", SpeechType: "offscreen_dialogue", Text: "别动😀。", TimeSegment: "0.5-2秒"}
	shots := []models.Shot{{ID: 9, SceneID: 2, Order: 1, Duration: 3, Dialogue: "过期缓存不能作为正文", DialogueRanges: []models.ShotDialogueRange{{DialogueID: 3, GroupKey: "g", StartRune: 0, EndRune: 4}}}, {ID: 10, SceneID: 2, Order: 2, Duration: 3}}
	return scene, shots, []models.Dialogue{d}
}
func TestStudioExecutionDialogueUsesAuthoritativeTextAndExplicitTimesWithoutMutating(t *testing.T) {
	sc, shots, rows := executionDialogueFixture()
	before, _ := json.Marshal(rows)
	result := buildStudioExecutionDialogueTimeline(sc, shots, rows)
	if len(result.Blockers) != 0 || len(result.Lines) != 1 {
		t.Fatalf("%+v", result)
	}
	line := result.Lines[0]
	if line.Text != rows[0].Text || line.Speaker != "甲" || line.SpeechType != "offscreen_dialogue" || line.Start != 0.5 || line.End != 2 || line.ShotID != 9 {
		t.Fatalf("%+v", line)
	}
	after, _ := json.Marshal(rows)
	if string(before) != string(after) {
		t.Fatal("mutated dialogues")
	}
}
func TestStudioExecutionDialogueRefusesMissingTimingPartialRangesAndUnreferencedRows(t *testing.T) {
	for _, name := range []string{"timing", "partial", "crosscut", "outside", "missing", "unreferenced", "group"} {
		t.Run(name, func(t *testing.T) {
			sc, shots, rows := executionDialogueFixture()
			switch name {
			case "timing":
				rows[0].TimeSegment = ""
			case "partial":
				shots[0].DialogueRanges[0].EndRune = 2
			case "crosscut":
				shots[1].DialogueRanges = append(shots[1].DialogueRanges, shots[0].DialogueRanges[0])
			case "outside":
				rows[0].TimeSegment = "2-4秒"
			case "missing":
				rows = nil
			case "unreferenced":
				shots[0].DialogueRanges = nil
				shots[0].Dialogue = ""
			case "group":
				rows = append(rows, models.Dialogue{ID: 4, Text: "第二行", Character: "乙", TimeSegment: "0-1秒"})
				shots[1].DialogueRanges = []models.ShotDialogueRange{{DialogueID: 4, GroupKey: "g", EndRune: 3}}
			}
			result := buildStudioExecutionDialogueTimeline(sc, shots, rows)
			if len(result.Blockers) == 0 || len(result.Lines) != 0 {
				t.Fatalf("did not fail closed %+v", result)
			}
		})
	}
}
func TestStudioExecutionDialogueReusesExplicitSourceTimingOnly(t *testing.T) {
	sc, shots, rows := executionDialogueFixture()
	rows[0].TimeSegment = ""
	sc.Content = "【0.5—2秒】甲说：\"别动😀。\""
	result := buildStudioExecutionDialogueTimeline(sc, shots, rows)
	if len(result.Lines) != 1 || result.Lines[0].Start != 0.5 {
		t.Fatalf("%+v", result)
	}
	if rows[0].TimeSegment != "" {
		t.Fatal("persisted inferred timing")
	}
	sc.Content = "甲说：\"别动😀。\""
	result = buildStudioExecutionDialogueTimeline(sc, shots, rows)
	if len(result.Lines) != 0 || !strings.Contains(strings.Join(result.Blockers, ""), "时间段") {
		t.Fatalf("guessed timing %+v", result)
	}
	if !reflect.DeepEqual(rows[0].Text, "别动😀。") {
		t.Fatal("rewrote original")
	}
}
