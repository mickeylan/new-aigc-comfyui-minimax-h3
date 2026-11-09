package service

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"comfyui-console/internal/models"
)

func TestSceneFramesUseDurableAssetDirectoryWhenLegacyInputIsAbsent(t *testing.T) {
	ffmpeg, err := localFFmpegPath()
	if err != nil {
		t.Skipf("ffmpeg required: %v", err)
	}
	svc, db := newContinuityTestService(t)
	svc.cfg.Storage.AssetDir = filepath.Join(t.TempDir(), "persistent-assets")
	p := models.Project{Title: "new storage"}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(svc.upload.InputDir(), fmt.Sprint(p.ID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	name := "fresh-video.mp4"
	video := filepath.Join(dir, name)
	if output, err := runLocalProgram(ffmpeg, []string{"-y", "-f", "lavfi", "-i", "color=c=blue:s=64x64:r=24:d=0.5", "-an", "-c:v", "mpeg4", "-pix_fmt", "yuv420p", video}, 30*time.Second); err != nil {
		t.Fatalf("fixture: %v %s", err, output)
	}
	oldDir := filepath.Join(svc.cfg.Comfy.ComfyDir, "input", fmt.Sprint(p.ID))
	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Fatalf("legacy directory must be absent: %v", err)
	}
	sc := models.Scene{ProjectID: p.ID, EpisodeN: 1, Generation: 1, Order: 1, Status: "video_ready", VideoTaskID: "new-video", VideoInputFile: name}
	if err := db.Create(&sc).Error; err != nil {
		t.Fatal(err)
	}
	all, err := svc.ExtractAllSceneFrames(p.ID, sc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Frames) != 12 {
		t.Fatalf("all frames=%d", len(all.Frames))
	}
	for _, frame := range all.Frames {
		if _, err := os.Stat(filepath.Join(dir, frame.File)); err != nil {
			t.Fatalf("preview file not in served asset directory: %v", err)
		}
	}
	saved, err := svc.SaveExtractedSceneFrame(p.ID, sc.ID, all.ExtractionID, 4)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, saved.File)); err != nil {
		t.Fatalf("selection missing: %v", err)
	}
	tail, err := svc.ExtractFrameCandidates(p.ID, sc.ID, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(tail) != 3 {
		t.Fatalf("tail frames=%d", len(tail))
	}
	for _, frame := range tail {
		if frame.ID == 0 {
			t.Fatal("missing persisted frame id")
		}
		if _, err := os.Stat(filepath.Join(dir, frame.ImageFile)); err != nil {
			t.Fatalf("tail in wrong directory: %v", err)
		}
	}
	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Fatal("extraction recreated the old ComfyUI input directory")
	}
}

func TestSceneFrameVideoPathPrefersRecordedAssetAndFallsBackOnlyToRecordedFiles(t *testing.T) {
	svc, _ := newContinuityTestService(t)
	svc.cfg.Storage.AssetDir = filepath.Join(t.TempDir(), "assets")
	sc := models.Scene{ProjectID: 7, VideoInputFile: "video.mp4"}
	legacy := filepath.Join(svc.cfg.Comfy.ComfyDir, "input", "7", "video.mp4")
	durable := filepath.Join(svc.cfg.AssetsRoot(), "7", "video.mp4")
	for _, file := range []string{legacy, durable} {
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte("recorded"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := svc.sceneFrameVideoPath(&sc)
	if err != nil || got != durable {
		t.Fatalf("asset priority: %s %v", got, err)
	}
	if err := os.Remove(durable); err != nil {
		t.Fatal(err)
	}
	got, err = svc.sceneFrameVideoPath(&sc)
	if err != nil || got != legacy {
		t.Fatalf("legacy fallback: %s %v", got, err)
	}
	if err := os.Remove(legacy); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.sceneFrameVideoPath(&sc); err == nil {
		t.Fatal("missing source silently accepted")
	}
	sc.VideoInputFile = "../other.mp4"
	if _, err := svc.sceneFrameVideoPath(&sc); err == nil {
		t.Fatal("traversal accepted")
	}
}
