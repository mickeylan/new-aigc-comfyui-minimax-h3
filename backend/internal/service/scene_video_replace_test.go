package service

import (
	"testing"

	"comfyui-console/internal/models"
)

func TestReplaceSceneVideoPreservesHistoryAndInvalidatesActiveMergeAndFrames(t *testing.T) {
	db := newTestDBWithNewModels(t)
	if err := db.AutoMigrate(&models.MergeTask{}, &models.FrameCandidate{}, &models.GenerationCandidate{}); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "replace video"}
	db.Create(&project)
	gpu := 2
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Generation: 1, Order: 1, Status: "video_ready", VideoFile: "old.mp4", VideoInputFile: "old.mp4", VideoGPU: &gpu, VideoTaskID: "old-task"}
	db.Create(&scene)
	candidate := models.GenerationCandidate{ProjectID: project.ID, EntityType: "scene", EntityID: scene.ID, MediaType: "video", File: "old.mp4", TaskID: "old-task"}
	db.Create(&candidate)
	frame := models.FrameCandidate{ProjectID: project.ID, SceneID: scene.ID, VideoTaskID: "old-task", Type: models.FrameCandidateVideoFirst, ImageFile: "first.png"}
	db.Create(&frame)
	merge := models.MergeTask{ProjectID: project.ID, EpisodeN: 1, Status: "pending"}
	db.Create(&merge)

	svc := &ProjectService{db: db}
	if err := svc.ReplaceSceneVideo(&scene, "manual.mp4", "upload"); err != nil {
		t.Fatal(err)
	}
	var got models.Scene
	db.First(&got, scene.ID)
	if got.VideoInputFile != "manual.mp4" || got.VideoFile != "manual.mp4" || got.Status != "video_ready" || got.VideoTaskID == "old-task" {
		t.Fatalf("scene=%+v", got)
	}
	var candidateCount, frameCount int64
	db.Model(&models.GenerationCandidate{}).Where("id = ?", candidate.ID).Count(&candidateCount)
	db.Model(&models.FrameCandidate{}).Where("scene_id = ? AND type = ?", scene.ID, models.FrameCandidateVideoFirst).Count(&frameCount)
	if candidateCount != 1 || frameCount != 0 {
		t.Fatalf("candidate=%d frames=%d", candidateCount, frameCount)
	}
	db.First(&merge, merge.ID)
	if merge.Status != "failed" {
		t.Fatalf("merge status=%s", merge.Status)
	}
}
