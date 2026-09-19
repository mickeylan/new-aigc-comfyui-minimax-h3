package service

import (
	"testing"

	"comfyui-console/internal/models"
)

func TestGenerationCandidateReviewAndSelectProjectsToScene(t *testing.T) {
	db := newTestDBWithNewModels(t)
	if err := db.AutoMigrate(&models.GenerationCandidate{}); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "p"}
	db.Create(&project)
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1}
	db.Create(&scene)
	svc := NewGenerationCandidateService(db)
	one, err := svc.CaptureSceneTask(project.ID, scene.ID, "image", "task-1", "one.png", "prompt", []string{"a.png"}, map[string]any{"seed": 1}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	two, err := svc.CaptureSceneTask(project.ID, scene.ID, "image", "task-2", "two.png", "prompt2", nil, nil, nil, &one.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Review(project.ID, one.ID, "rejected", "构图不符"); err != nil {
		t.Fatal(err)
	}
	selected, err := svc.SelectCurrent(project.ID, two.ID)
	if err != nil || !selected.IsCurrent {
		t.Fatalf("selected=%+v err=%v", selected, err)
	}
	db.First(&scene, scene.ID)
	if scene.ImageFile != "two.png" || scene.ImageTaskID != "task-2" {
		t.Fatalf("scene projection=%+v", scene)
	}
	rows, err := svc.ListScene(project.ID, scene.ID, "image")
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
}

func TestGenerationCandidateStaleCannotBecomeCurrent(t *testing.T) {
	db := newTestDBWithNewModels(t)
	if err := db.AutoMigrate(&models.GenerationCandidate{}); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "p"}
	db.Create(&project)
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1}
	db.Create(&scene)
	svc := NewGenerationCandidateService(db)
	row, _ := svc.CaptureSceneTask(project.ID, scene.ID, "video", "v1", "v.mp4", "prompt", nil, nil, nil, nil)
	if err := MarkSceneCandidatesStale(db, project.ID, scene.ID, "video", "提示词变化"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SelectCurrent(project.ID, row.ID); err == nil {
		t.Fatal("stale candidate selected")
	}
}
