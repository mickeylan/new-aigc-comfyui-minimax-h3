package service

import (
	"comfyui-console/internal/models"
	"fmt"
	"strings"
	"testing"
)

func TestSceneStoryDialogueSyncPreservesMediaAndScope(t *testing.T) {
	db := newTestDB(t)
	if err := db.AutoMigrate(&models.Project{}, &models.Scene{}, &models.Dialogue{}, &models.Shot{}, &models.GenerationCandidate{}); err != nil {
		t.Fatal(err)
	}
	p := models.Project{Title: "repair", Script: "手工剧本"}
	other := models.Project{Title: "other"}
	for _, v := range []*models.Project{&p, &other} {
		if err := db.Create(v).Error; err != nil {
			t.Fatal(err)
		}
	}
	scenes := []models.Scene{
		{ProjectID: p.ID, EpisodeN: 1, Order: 1, Content: "抬手。", ImageFile: "kept.png", VideoFile: "kept.mp4", VideoFullPrompt: "confirmed", ImageLocked: true, VideoLocked: true},
		{ProjectID: p.ID, EpisodeN: 1, Order: 2, Content: "旧版本", Archived: true},
		{ProjectID: p.ID, EpisodeN: 2, Order: 1, Content: "另一集"},
		{ProjectID: other.ID, EpisodeN: 1, Order: 1, Content: "另一项目"},
	}
	for i := range scenes {
		if err := db.Create(&scenes[i]).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&models.Dialogue{ProjectID: scenes[i].ProjectID, SceneID: scenes[i].ID, Order: 1, Character: "甲", SpeechType: "dialogue", Text: "不行，这妖狐必须活捉。"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	shot := models.Shot{SceneID: scenes[0].ID, Order: 1, Description: "existing shot"}
	if err := db.Create(&shot).Error; err != nil {
		t.Fatal(err)
	}
	candidate := models.GenerationCandidate{ProjectID: p.ID, EntityType: "scene", EntityID: scenes[0].ID, IsCurrent: true}
	if err := db.Create(&candidate).Error; err != nil {
		t.Fatal(err)
	}
	svc := &Service{DB: db}
	result, err := svc.syncEpisodeStoryDialogues(p.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if result.CountChanged != 1 {
		t.Fatalf("changed=%d", result.CountChanged)
	}
	for i, before := range scenes {
		var got models.Scene
		if err := db.First(&got, before.ID).Error; err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			if !strings.Contains(got.Content, "【对白｜甲】不行，这妖狐必须活捉。") || !got.PromptStale {
				t.Fatalf("content=%s", got.Content)
			}
			if got.ImageFile != before.ImageFile || got.VideoFile != before.VideoFile || got.VideoFullPrompt != before.VideoFullPrompt || !got.ImageLocked || !got.VideoLocked {
				t.Fatal("media/confirmation draft changed")
			}
		} else if got.Content != before.Content {
			t.Fatal("scope escaped")
		}
	}
	result, err = svc.syncEpisodeStoryDialogues(p.ID, 1)
	if err != nil || result.CountChanged != 0 {
		t.Fatalf("not idempotent: %+v %v", result, err)
	}
	var kept models.GenerationCandidate
	db.First(&kept, candidate.ID)
	if !kept.IsCurrent {
		t.Fatal("candidate revoked")
	}
	var keptShot models.Shot
	db.First(&keptShot, shot.ID)
	if keptShot.Description != shot.Description {
		t.Fatal("shot overwritten")
	}
	var keptProject models.Project
	db.First(&keptProject, p.ID)
	if keptProject.Script != p.Script {
		t.Fatal("authored script overwritten")
	}
}

func TestSceneStoryDialogueSyncRollsBackOnWriteFailure(t *testing.T) {
	db := newTestDB(t)
	if err := db.AutoMigrate(&models.Project{}, &models.Scene{}, &models.Dialogue{}); err != nil {
		t.Fatal(err)
	}
	p := models.Project{Title: "rollback"}
	db.Create(&p)
	var ids []uint
	for i := 1; i <= 2; i++ {
		sc := models.Scene{ProjectID: p.ID, EpisodeN: 1, Order: i, Content: "原动作"}
		if err := db.Create(&sc).Error; err != nil {
			t.Fatal(err)
		}
		ids = append(ids, sc.ID)
		if err := db.Create(&models.Dialogue{ProjectID: p.ID, SceneID: sc.ID, Text: "原话", Character: "甲", SpeechType: "dialogue"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Exec(fmt.Sprintf("CREATE TRIGGER reject_story_update BEFORE UPDATE ON scenes WHEN OLD.id=%d BEGIN SELECT RAISE(ABORT,'forced failure'); END", ids[1])).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := (&Service{DB: db}).syncEpisodeStoryDialogues(p.ID, 1); err == nil {
		t.Fatal("expected write failure")
	}
	for _, id := range ids {
		var sc models.Scene
		db.First(&sc, id)
		if sc.Content != "原动作" || sc.PromptStale {
			t.Fatal("partial update escaped rollback")
		}
	}
}
