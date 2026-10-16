package service

import (
	"testing"

	"comfyui-console/internal/models"
)

func TestScreenTextCreateReplacesExistingTargetSlot(t *testing.T) {
	db := safetyDB(t, &models.Project{}, &models.Scene{}, &models.ScreenTextCue{})
	project := models.Project{Title: "项目"}
	db.Create(&project)
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1, Title: "殿内", Duration: 10}
	db.Create(&scene)
	service := NewScreenTextService(db)
	base := ScreenTextCueInput{EpisodeN: 1, SceneID: &scene.ID, Kind: "location", Text: "旧地点", StartTime: 0, EndTime: 3, WritingMode: "vertical-rl", Anchor: "left_center", FontSize: 68, PrimaryColor: "#FFFFFF"}
	first, err := service.Create(project.ID, base)
	if err != nil {
		t.Fatal(err)
	}

	base.Text = "新地点"
	base.FontSize = 24
	base.PrimaryColor = "#112233"
	base.StyleCode = "xianxia-gold-title"
	replaced, err := service.Create(project.ID, base)
	if err != nil {
		t.Fatal(err)
	}
	if replaced.ID != first.ID {
		t.Fatalf("slot replacement created id %d; want %d", replaced.ID, first.ID)
	}
	rows, err := service.List(project.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Text != "新地点" || rows[0].FontSize != 24 || rows[0].PrimaryColor != "#112233" || rows[0].StyleCode != "xianxia-gold-title" {
		t.Fatalf("old and new cue were not atomically replaced: %+v", rows)
	}
}

func TestScreenTextUpdateRemovesDuplicateTargetSlot(t *testing.T) {
	db := safetyDB(t, &models.Project{}, &models.Scene{}, &models.ScreenTextCue{})
	project := models.Project{Title: "项目"}
	db.Create(&project)
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1, Title: "殿内", Duration: 10}
	db.Create(&scene)
	service := NewScreenTextService(db)
	old := models.ScreenTextCue{ProjectID: project.ID, EpisodeN: 1, SceneID: &scene.ID, Kind: "location", Text: "旧层", StartTime: 0, EndTime: 3, WritingMode: "vertical-rl", Anchor: "left_center", StyleCode: "xianxia-location-vertical", FontCode: "noto-sans-sc", Enabled: true, ReviewStatus: "approved"}
	duplicate := old
	duplicate.ID = 0
	duplicate.Text = "重复层"
	db.Create(&old)
	db.Create(&duplicate)
	_, err := service.Update(project.ID, duplicate.ID, ScreenTextCueInput{EpisodeN: 1, SceneID: &scene.ID, Kind: "location", Text: "最终文字", StartTime: 0, EndTime: 3, WritingMode: "vertical-rl", Anchor: "left_center", FontSize: 18, PrimaryColor: "#123456"})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := service.List(project.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != duplicate.ID || rows[0].Text != "最终文字" || rows[0].FontSize != 18 {
		t.Fatalf("duplicate target layers remain: %+v", rows)
	}
}
