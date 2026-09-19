package service

import (
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

func TestReplaceShotsAllowsEmptyAndInvalidatesSceneGraph(t *testing.T) {
	db := newTestDBWithNewModels(t)
	project := models.Project{Title: "p"}
	db.Create(&project)
	episode := models.Episode{ProjectID: project.ID, Number: 1, Summary: "old", NextHook: "hook"}
	db.Create(&episode)
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1, Generation: 1, Content: "old", ImagePrompt: "old", ImageFile: "old.png", VideoFile: "old.mp4", VideoInputFile: "old-local.mp4", Status: "video_ready"}
	db.Create(&scene)
	dependent := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 2, Generation: 1, VideoFile: "dependent.mp4", Status: "video_ready"}
	db.Create(&dependent)
	db.Create(&models.SceneContinuity{SceneID: dependent.ID, SourceSceneID: &scene.ID, Mode: models.ContinuityModeContinue, Status: "ready"})
	db.Create(&models.GenerationCandidate{ProjectID: project.ID, EntityType: "scene", EntityID: scene.ID, MediaType: "video", File: scene.VideoFile, IsCurrent: true})
	svc := NewShotService(db)
	if _, err := svc.ReplaceShots(scene.ID, []models.Shot{{ShotType: "medium", Duration: 2, Description: "beat"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ReplaceShots(scene.ID, []models.Shot{}); err != nil {
		t.Fatal(err)
	}
	db.First(&scene, scene.ID)
	db.First(&dependent, dependent.ID)
	db.First(&episode, episode.ID)
	if scene.ShotCount != 0 || scene.Content != "" || scene.ImagePrompt != "" || scene.ImageFile != "" || scene.VideoInputFile != "" || scene.Status != "pending" {
		t.Fatalf("scene not reset: %+v", scene)
	}
	if dependent.VideoFile != "" {
		t.Fatalf("continuity dependent not invalidated: %+v", dependent)
	}
	if !episode.SummaryStale || !episode.NextHookStale {
		t.Fatalf("episode freshness not invalidated: %+v", episode)
	}
}

func TestShotTransitionAndNegativePromptAggregateIntoScene(t *testing.T) {
	db := newTestDBWithNewModels(t)
	project := models.Project{Title: "p"}
	db.Create(&project)
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1}
	db.Create(&scene)
	_, err := NewShotService(db).ReplaceShots(scene.ID, []models.Shot{{ShotType: "close", Duration: 2, Description: "look", TransitionType: models.ShotTransitionMatchCut, TransitionNote: "match eyes", NegativePrompt: "modern cars"}})
	if err != nil {
		t.Fatal(err)
	}
	db.First(&scene, scene.ID)
	if !strings.Contains(scene.Content, "match_cut") || !strings.Contains(scene.Content, "match eyes") || scene.NegativePrompt != "modern cars" {
		t.Fatalf("director metadata missing: %+v", scene)
	}
}
