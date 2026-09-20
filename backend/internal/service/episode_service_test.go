package service

import (
	"testing"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

func TestEnsureProjectEpisodesBackfillsAndBuildsHierarchy(t *testing.T) {
	db := newTestDBWithNewModels(t)
	if err := db.AutoMigrate(&models.Episode{}); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "系列", Episodes: 2, Plan: `{"episodes":[{"n":1,"title":"初见","target_duration":120,"target_scenes":18},{"n":2,"title":"冲突","target_duration":150,"target_scenes":22}]}`}
	db.Create(&project)
	scene1 := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1, Generation: 1, Title: "面馆"}
	scene2 := models.Scene{ProjectID: project.ID, EpisodeN: 2, Order: 1, Generation: 1, Title: "后院"}
	db.Create(&scene1)
	db.Create(&scene2)
	db.Create(&models.Shot{SceneID: scene1.ID, Order: 1, ActType: models.ShotActSetup, ShotType: "中景", Duration: 2})

	if err := EnsureProjectEpisodes(db, project.ID); err != nil {
		t.Fatal(err)
	}
	creates := 0
	if err := db.Callback().Create().Before("gorm:create").Register("test:count_episode_creates", func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "episodes" {
			creates++
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := EnsureProjectEpisodes(db, project.ID); err != nil {
		t.Fatal(err)
	}
	if creates != 0 {
		t.Fatalf("idempotent episode read attempted %d writes", creates)
	}
	var count int64
	db.Model(&models.Episode{}).Where("project_id = ?", project.ID).Count(&count)
	if count != 2 {
		t.Fatalf("episode backfill is not idempotent: count=%d", count)
	}
	hierarchy, err := ProjectEpisodeHierarchy(db, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(hierarchy) != 2 || hierarchy[0].Episode.Title != "初见" || hierarchy[0].Episode.TargetDuration != 120 {
		t.Fatalf("episode metadata lost: %+v", hierarchy)
	}
	if len(hierarchy[0].Scenes) != 1 || hierarchy[0].Scenes[0].Scene.ID != scene1.ID || len(hierarchy[0].Scenes[0].Shots) != 1 {
		t.Fatalf("episode hierarchy incorrect: %+v", hierarchy[0])
	}
}

func TestEpisodeCRUDProtectsReferencedEpisodes(t *testing.T) {
	db := newTestDBWithNewModels(t)
	if err := db.AutoMigrate(&models.Episode{}); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "系列"}
	db.Create(&project)
	episode, err := CreateProjectEpisode(db, project.ID, models.Episode{Number: 3, Title: "新集"})
	if err != nil || episode.Version != 1 {
		t.Fatalf("create=%+v err=%v", episode, err)
	}
	updated, err := UpdateProjectEpisode(db, project.ID, 3, map[string]any{"title": "修改后", "target_duration": float64(90), "target_scenes": float64(12), "status": "planned"})
	if err != nil || updated.Title != "修改后" || updated.Version != 2 || updated.TargetDuration != 90 || updated.TargetScenes != 12 {
		t.Fatalf("update=%+v err=%v", updated, err)
	}
	db.Create(&models.Scene{ProjectID: project.ID, EpisodeN: 3, Order: 1})
	if err := DeleteProjectEpisode(db, project.ID, 3); err == nil {
		t.Fatal("referenced episode was deleted")
	}
	db.Where("project_id = ? AND episode_n = ?", project.ID, 3).Delete(&models.Scene{})
	if err := DeleteProjectEpisode(db, project.ID, 3); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureProjectEpisodesPreservesExistingEpisode(t *testing.T) {
	db := newTestDBWithNewModels(t)
	if err := db.AutoMigrate(&models.Episode{}); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "系列", Episodes: 1, Plan: `{"episodes":[{"n":1,"title":"计划标题"}]}`}
	db.Create(&project)
	db.Create(&models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1})
	db.Create(&models.Episode{ProjectID: project.ID, Number: 1, Title: "用户标题", TargetDuration: 88, TargetScenes: 9, Status: "approved", Version: 3})
	if err := EnsureProjectEpisodes(db, project.ID); err != nil {
		t.Fatal(err)
	}
	var episode models.Episode
	db.Where("project_id = ? AND episode_number = ?", project.ID, 1).First(&episode)
	if episode.Title != "用户标题" || episode.TargetDuration != 88 || episode.Status != "approved" || episode.Version != 3 {
		t.Fatalf("existing episode was overwritten: %+v", episode)
	}
}
