package service

import (
	"errors"
	"testing"

	"comfyui-console/internal/models"
)

func boolPointer(value bool) *bool        { return &value }
func floatPointer(value float64) *float64 { return &value }

func TestScreenTextCueCRUDAndProjectIsolation(t *testing.T) {
	db := safetyDB(t, &models.Project{}, &models.Scene{}, &models.Shot{}, &models.Character{}, &models.ScreenTextCue{})
	p1 := models.Project{Title: "甲"}
	p2 := models.Project{Title: "乙"}
	db.Create(&p1)
	db.Create(&p2)
	scene := models.Scene{ProjectID: p1.ID, EpisodeN: 1, Order: 1, Title: "殿内", Duration: 10}
	db.Create(&scene)
	character := models.Character{ProjectID: p1.ID, Name: "上官若琳"}
	db.Create(&character)
	service := NewScreenTextService(db)

	created, err := service.Create(p1.ID, ScreenTextCueInput{
		EpisodeN: 1, SceneID: &scene.ID, CharacterID: &character.ID, Kind: "character_intro",
		Text: "上官若琳", Subtext: "玉霄宫主", StartTime: 0.5, EndTime: 3,
		WritingMode: "vertical-rl", Anchor: "subject_right", StyleCode: "xianxia-character-vertical",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.WritingMode != "vertical-rl" || !created.Enabled || created.Source != "manual" || created.ReviewStatus != "approved" {
		t.Fatalf("unexpected normalized cue: %+v", created)
	}
	if rows, err := service.List(p2.ID, 1); err != nil || len(rows) != 0 {
		t.Fatalf("cross-project cue leaked: rows=%+v err=%v", rows, err)
	}
	created.Text = "ignored"
	updated, err := service.Update(p1.ID, created.ID, ScreenTextCueInput{
		EpisodeN: 1, SceneID: &scene.ID, CharacterID: &character.ID, Kind: "character_intro",
		Text: "上官若琳", StartTime: 1, EndTime: 2.5, WritingMode: "vertical-rl",
		Anchor: "subject_left", Animation: "ink_reveal", Enabled: boolPointer(false),
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Enabled || updated.Anchor != "subject_left" || updated.Animation != "ink_reveal" {
		t.Fatalf("update not persisted: %+v", updated)
	}
	if _, err := service.Update(p2.ID, created.ID, ScreenTextCueInput{}); !errors.Is(err, ErrScreenTextCueNotFound) {
		t.Fatalf("cross-project update was not hidden: %v", err)
	}
	if err := service.Delete(p2.ID, created.ID); !errors.Is(err, ErrScreenTextCueNotFound) {
		t.Fatalf("cross-project delete was not hidden: %v", err)
	}
	if err := service.Delete(p1.ID, created.ID); err != nil {
		t.Fatal(err)
	}
}

func TestScreenTextCueStrictBindingsAndTimeline(t *testing.T) {
	db := safetyDB(t, &models.Project{}, &models.Scene{}, &models.Shot{}, &models.Character{}, &models.ScreenTextCue{})
	p1 := models.Project{Title: "甲"}
	p2 := models.Project{Title: "乙"}
	db.Create(&p1)
	db.Create(&p2)
	scene := models.Scene{ProjectID: p1.ID, EpisodeN: 1, Duration: 5}
	db.Create(&scene)
	foreignCharacter := models.Character{ProjectID: p2.ID, Name: "旁人"}
	db.Create(&foreignCharacter)
	service := NewScreenTextService(db)

	validBase := ScreenTextCueInput{EpisodeN: 1, SceneID: &scene.ID, Kind: "location", Text: "玉霄宫", StartTime: 0, EndTime: 2}
	if _, err := service.Create(p1.ID, validBase); err != nil {
		t.Fatal(err)
	}
	invalid := []ScreenTextCueInput{
		{EpisodeN: 1, Kind: "location", Text: "", StartTime: 0, EndTime: 1},
		{EpisodeN: 1, Kind: "unknown", Text: "文字", StartTime: 0, EndTime: 1},
		{EpisodeN: 1, SceneID: &scene.ID, Kind: "location", Text: "越界", StartTime: 0, EndTime: 6},
		{EpisodeN: 1, Kind: "character_intro", Text: "未绑定", StartTime: 0, EndTime: 1},
		{EpisodeN: 1, Kind: "character_intro", CharacterID: &foreignCharacter.ID, Text: "旁人", StartTime: 0, EndTime: 1},
		{EpisodeN: 1, Kind: "custom", Text: "错误排版", StartTime: 0, EndTime: 1, WritingMode: "sideways"},
		{EpisodeN: 1, Kind: "custom", Text: "未知样式", StartTime: 0, EndTime: 1, StyleCode: "unlicensed-fancy-font"},
	}
	for index, input := range invalid {
		if _, err := service.Create(p1.ID, input); err == nil {
			t.Fatalf("invalid cue %d accepted: %+v", index, input)
		}
	}
}

func TestScreenTextCueCustomPositionStaysInsideSafeArea(t *testing.T) {
	valid, err := normalizeScreenTextCue(1, ScreenTextCueInput{EpisodeN: 1, Kind: "location", Text: "玉霄宫", StartTime: 0, EndTime: 2, Anchor: "custom", PositionX: floatPointer(.9), PositionY: floatPointer(.1)})
	if err != nil {
		t.Fatal(err)
	}
	if valid.PositionX == nil || valid.PositionY == nil || *valid.PositionX != .9 || *valid.PositionY != .1 {
		t.Fatalf("custom position lost: %+v", valid)
	}
	for _, input := range []ScreenTextCueInput{
		{EpisodeN: 1, Kind: "location", Text: "缺坐标", StartTime: 0, EndTime: 1, Anchor: "custom"},
		{EpisodeN: 1, Kind: "location", Text: "越界", StartTime: 0, EndTime: 1, Anchor: "custom", PositionX: floatPointer(.99), PositionY: floatPointer(.5)},
		{EpisodeN: 1, Kind: "location", Text: "负数", StartTime: 0, EndTime: 1, Anchor: "custom", PositionX: floatPointer(.5), PositionY: floatPointer(-.1)},
	} {
		if _, err := normalizeScreenTextCue(1, input); err == nil {
			t.Fatalf("invalid custom position accepted: %+v", input)
		}
	}
	preset, err := normalizeScreenTextCue(1, ScreenTextCueInput{EpisodeN: 1, Kind: "location", Text: "预设", StartTime: 0, EndTime: 1, Anchor: "top_right", PositionX: floatPointer(.4), PositionY: floatPointer(.4)})
	if err != nil {
		t.Fatal(err)
	}
	if preset.PositionX != nil || preset.PositionY != nil {
		t.Fatalf("preset retained stale custom coordinates: %+v", preset)
	}
}
