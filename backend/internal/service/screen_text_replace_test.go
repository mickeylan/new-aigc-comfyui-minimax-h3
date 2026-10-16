package service

import (
	"testing"

	"comfyui-console/internal/models"
)

func TestScreenTextCreateNeverOverwritesExistingCue(t *testing.T) {
	db := safetyDB(t, &models.Project{}, &models.Scene{}, &models.ScreenTextCue{})
	project := models.Project{Title: "项目"}
	db.Create(&project)
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1, Title: "殿内", Duration: 10}
	db.Create(&scene)
	service := NewScreenTextService(db)
	first, err := service.Create(project.ID, ScreenTextCueInput{EpisodeN: 1, SceneID: &scene.ID, Kind: "location", Text: "天阙宗", StartTime: 0, EndTime: 3, WritingMode: "vertical-rl", Anchor: "left_center", FontSize: 68, PrimaryColor: "#FFFFFF"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Create(project.ID, ScreenTextCueInput{EpisodeN: 1, SceneID: &scene.ID, Kind: "story_note", Text: "三年后", StartTime: 0, EndTime: 3, WritingMode: "horizontal-ltr", Anchor: "bottom_center", FontSize: 24, PrimaryColor: "#112233"})
	if err != nil {
		t.Fatal(err)
	}
	if second.ID == first.ID {
		t.Fatalf("new cue overwrote existing cue id=%d", first.ID)
	}
	rows, err := service.List(project.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Text != "天阙宗" || rows[1].Text != "三年后" {
		t.Fatalf("new cue did not preserve previous cue: %+v", rows)
	}
}

func TestScreenTextUpdateChangesOnlySelectedCue(t *testing.T) {
	db := safetyDB(t, &models.Project{}, &models.Scene{}, &models.ScreenTextCue{})
	project := models.Project{Title: "项目"}
	db.Create(&project)
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1, Title: "殿内", Duration: 10}
	db.Create(&scene)
	service := NewScreenTextService(db)
	first, err := service.Create(project.ID, ScreenTextCueInput{EpisodeN: 1, SceneID: &scene.ID, Kind: "location", Text: "天阙宗", StartTime: 0, EndTime: 3, WritingMode: "vertical-rl", Anchor: "left_center", FontSize: 68})
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Create(project.ID, ScreenTextCueInput{EpisodeN: 1, SceneID: &scene.ID, Kind: "story_note", Text: "旧提示", StartTime: 0, EndTime: 3, WritingMode: "horizontal-ltr", Anchor: "bottom_center", FontSize: 32})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := service.Update(project.ID, second.ID, ScreenTextCueInput{EpisodeN: 1, SceneID: &scene.ID, Kind: "story_note", Text: "新提示", StartTime: 0, EndTime: 3, WritingMode: "horizontal-ltr", Anchor: "bottom_center", FontSize: 18, PrimaryColor: "#123456"})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != second.ID || updated.Text != "新提示" || updated.FontSize != 18 {
		t.Fatalf("selected cue not updated: %+v", updated)
	}
	var preserved models.ScreenTextCue
	if err := db.First(&preserved, first.ID).Error; err != nil {
		t.Fatal(err)
	}
	if preserved.Text != "天阙宗" || preserved.FontSize != 68 {
		t.Fatalf("editing one cue changed previous cue: %+v", preserved)
	}
	var count int64
	db.Model(&models.ScreenTextCue{}).Where("project_id = ?", project.ID).Count(&count)
	if count != 2 {
		t.Fatalf("update deleted another cue; count=%d", count)
	}
}

func TestScreenTextCreateRejectsOverlapWithoutDeletingExistingCue(t *testing.T) {
	db := safetyDB(t, &models.Project{}, &models.Scene{}, &models.ScreenTextCue{})
	project := models.Project{Title: "项目"}
	db.Create(&project)
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1, Title: "殿内", Duration: 10}
	db.Create(&scene)
	service := NewScreenTextService(db)
	first, err := service.Create(project.ID, ScreenTextCueInput{EpisodeN: 1, SceneID: &scene.ID, Kind: "location", Text: "原字幕", StartTime: 0, EndTime: 3, WritingMode: "vertical-rl", Anchor: "left_center"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Create(project.ID, ScreenTextCueInput{EpisodeN: 1, SceneID: &scene.ID, Kind: "story_note", Text: "重叠字幕", StartTime: 0, EndTime: 3, WritingMode: "vertical-rl", Anchor: "left_center"}); err == nil {
		t.Fatal("overlapping new cue unexpectedly accepted")
	}
	var preserved models.ScreenTextCue
	if err := db.First(&preserved, first.ID).Error; err != nil {
		t.Fatal(err)
	}
	if preserved.Text != "原字幕" {
		t.Fatalf("rejected create changed existing cue: %+v", preserved)
	}
}
