package service

import (
	"comfyui-console/internal/models"
	"testing"
)

func TestImageRegenerationCompletesWithRetainedVideoAndRepairsStuckState(t *testing.T) {
	db := newTestDBWithNewModels(t)
	if err := db.AutoMigrate(&models.GenerationCandidate{}); err != nil {
		t.Fatal(err)
	}
	p := models.Project{Title: "completion"}
	db.Create(&p)
	for _, video := range []string{"", "retained.mp4"} {
		t.Run("video="+video, func(t *testing.T) {
			sc := models.Scene{ProjectID: p.ID, Order: 1, Status: "image_pending", ImageFile: "old.png", ImageTaskID: "new-image-" + video, ImageToken: "claim", VideoFile: video, VideoInputFile: video, VideoTaskID: "retained-video", VideoFirstFrameImg: "first.png", VideoLastFrameImg: "last.png"}
			if err := db.Create(&sc).Error; err != nil {
				t.Fatal(err)
			}
			in := SceneCandidateCapture{ProjectID: p.ID, SceneID: sc.ID, MediaType: "image", TaskID: sc.ImageTaskID, ExpectedTaskID: sc.ImageTaskID, File: "new.png"}
			svc := NewGenerationCandidateService(db)
			candidate, err := svc.CaptureSceneSuccess(in)
			if err != nil {
				t.Fatal(err)
			}
			want := "image_ready"
			if video != "" {
				want = "video_ready"
			}
			check := func() {
				t.Helper()
				var got models.Scene
				if err := db.First(&got, sc.ID).Error; err != nil {
					t.Fatal(err)
				}
				if got.Status != want || got.ImageToken != "" || got.ImageFile != "new.png" || got.VideoFile != video || got.VideoInputFile != video || got.VideoTaskID != "retained-video" || got.VideoFirstFrameImg != "first.png" || got.VideoLastFrameImg != "last.png" {
					t.Fatalf("completion lost state or media: %+v", got)
				}
			}
			check()
			if err := db.Model(&sc).Updates(map[string]any{"status": "image_pending", "image_token": ""}).Error; err != nil {
				t.Fatal(err)
			}
			again, err := svc.CaptureSceneSuccess(in)
			if err != nil {
				t.Fatal(err)
			}
			if again.ID != candidate.ID {
				t.Fatal("recovery duplicated candidate")
			}
			check()
			// An obsolete completion must not unlock a newer generation.
			db.Model(&sc).Updates(map[string]any{"status": "image_pending", "image_task_id": "newer-task", "image_token": "newer-claim"})
			if _, err := svc.CaptureSceneSuccess(in); err == nil {
				t.Fatal("obsolete task accepted")
			}
			var got models.Scene
			db.First(&got, sc.ID)
			if got.Status != "image_pending" || got.ImageToken != "newer-claim" {
				t.Fatal("obsolete task unlocked active generation")
			}
		})
	}
}
