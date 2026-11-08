package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

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
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Generation: 1, Order: 1, Status: "video_ready", VideoTaskID: "frame-task", VideoInputFile: videoName, VideoFile: "keep-video.mp4", ImageFile: "old-storyboard.png"}
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
	if saved.File == "" || saved.URL == "" || filepath.Ext(saved.File) != ".png" {
		t.Fatalf("saved=%+v", saved)
	}
	// Selected frames are durable assets, while extracted frames stay transient.
	if _, err := os.Stat(filepath.Join(continuity.cfg.AssetsRoot(), fmt.Sprint(project.ID), saved.File)); err != nil {
		t.Fatalf("saved frame missing: %v", err)
	}
	var upload models.UploadFile
	if err := db.Where("task_id=? AND name=?", fmt.Sprint(project.ID), saved.File).First(&upload).Error; err != nil {
		t.Fatalf("saved frame upload not registered: %v", err)
	}

	gin.SetMode(gin.TestMode)
	handler := &Service{DB: db, Continuity: continuity, Projects: &ProjectService{db: db, continuity: continuity}}
	router := gin.New()
	router.POST("/api/projects/:id/scenes/:sid/frames/save", handler.HandleSaveExtractedSceneFrame)
	body, err := json.Marshal(map[string]any{"extraction_id": result.ExtractionID, "frame_index": 6, "set_as_scene_image": true})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/projects/%d/scenes/%d/frames/save", project.ID, scene.ID), bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("adopt frame returned HTTP %d: %s", response.Code, response.Body.String())
	}
	if err := db.First(&scene, scene.ID).Error; err != nil {
		t.Fatal(err)
	}
	if filepath.Ext(scene.ImageFile) != ".png" || scene.ImageFile == "old-storyboard.png" {
		t.Fatalf("selected frame was not adopted as PNG storyboard: %+v", scene)
	}
	if scene.VideoFile != "keep-video.mp4" || scene.VideoInputFile != videoName || scene.Status != "video_ready" {
		t.Fatalf("adopting frame unexpectedly replaced current video: %+v", scene)
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
