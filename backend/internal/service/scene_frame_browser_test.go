package service

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"comfyui-console/internal/models"
)

func TestSceneFrameBrowserExtractsEveryFrameAndSavesSelection(t *testing.T) {
	ffmpeg, err := localFFmpegPath()
	if err != nil {
		t.Skipf("ffmpeg is required: %v", err)
	}
	continuity, db := newContinuityTestService(t)
	project := models.Project{Title: "frame browser"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	inputDir := filepath.Join(continuity.cfg.Comfy.ComfyDir, "input", fmt.Sprint(project.ID))
	if err := os.MkdirAll(inputDir, 0o755); err != nil {
		t.Fatal(err)
	}
	videoName := "all-frames.mp4"
	videoPath := filepath.Join(inputDir, videoName)
	if output, err := runLocalProgram(ffmpeg, []string{
		"-y", "-f", "lavfi", "-i", "testsrc=size=64x64:rate=24:duration=0.5",
		"-an", "-c:v", "mpeg4", "-pix_fmt", "yuv420p", videoPath,
	}, 30*time.Second); err != nil {
		t.Fatalf("create video fixture: %v\n%s", err, output)
	}
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Generation: 1, Order: 1, Status: "video_ready", VideoTaskID: "frame-task", VideoInputFile: videoName}
	if err := db.Create(&scene).Error; err != nil {
		t.Fatal(err)
	}

	result, err := continuity.ExtractAllSceneFrames(project.ID, scene.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Frames) != 12 {
		t.Fatalf("frames=%d want=12", len(result.Frames))
	}
	if result.Frames[0].Index != 0 || result.Frames[11].Index != 11 {
		t.Fatalf("unexpected indexes: first=%+v last=%+v", result.Frames[0], result.Frames[11])
	}

	saved, err := continuity.SaveExtractedSceneFrame(project.ID, scene.ID, result.ExtractionID, 5)
	if err != nil {
		t.Fatal(err)
	}
	if saved.File == "" || saved.URL == "" {
		t.Fatalf("saved=%+v", saved)
	}
	if _, err := os.Stat(filepath.Join(inputDir, saved.File)); err != nil {
		t.Fatalf("saved frame missing: %v", err)
	}
	var upload models.UploadFile
	if err := db.Where("task_id=? AND name=?", fmt.Sprint(project.ID), saved.File).First(&upload).Error; err != nil {
		t.Fatalf("saved frame upload not registered: %v", err)
	}
}

func TestSceneFrameBrowserRejectsForeignExtraction(t *testing.T) {
	continuity, db := newContinuityTestService(t)
	project := models.Project{Title: "frame browser safety"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Generation: 1, Order: 1, Status: "video_ready", VideoTaskID: "task", VideoInputFile: "video.mp4"}
	if err := db.Create(&scene).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := continuity.SaveExtractedSceneFrame(project.ID, scene.ID, "scene_frames_s999_foreign", 0); err == nil {
		t.Fatal("expected foreign extraction rejection")
	}
}
