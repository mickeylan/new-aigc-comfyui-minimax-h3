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

// TestExtractFrameCandidatesReturnsPersistedIDs exercises the real local ffmpeg
// extraction path, then sends one returned ID through the selection HTTP handler.
func TestExtractFrameCandidatesReturnsPersistedIDs(t *testing.T) {
	ffmpeg, err := localFFmpegPath()
	if err != nil {
		t.Skipf("ffmpeg is required for the extraction regression test: %v", err)
	}

	continuity, db := newContinuityTestService(t)
	project := models.Project{Title: "extract regression"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	inputDir := filepath.Join(continuity.cfg.Comfy.ComfyDir, "input", fmt.Sprint(project.ID))
	if err := os.MkdirAll(inputDir, 0o755); err != nil {
		t.Fatal(err)
	}
	videoName := "source.mp4"
	videoPath := filepath.Join(inputDir, videoName)
	if output, err := runLocalProgram(ffmpeg, []string{
		"-y", "-f", "lavfi", "-i", "color=c=blue:s=64x64:r=24:d=2",
		"-an", "-c:v", "mpeg4", "-pix_fmt", "yuv420p", videoPath,
	}, 30*time.Second); err != nil {
		t.Fatalf("create video fixture: %v\n%s", err, output)
	}

	scene := models.Scene{
		ProjectID: project.ID, EpisodeN: 1, Generation: 1, Order: 1,
		Status: "video_ready", VideoTaskID: "fixture-task", VideoInputFile: videoName,
	}
	if err := db.Create(&scene).Error; err != nil {
		t.Fatal(err)
	}

	frames, err := continuity.ExtractFrameCandidates(project.ID, scene.ID, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 3 {
		t.Fatalf("returned %d candidate frames, want 3: %+v", len(frames), frames)
	}
	for i, frame := range frames {
		if frame.ID == 0 {
			t.Fatalf("returned frame %d has zero ID after persistence: %+v", i, frame)
		}
		var persisted models.FrameCandidate
		if err := db.First(&persisted, frame.ID).Error; err != nil {
			t.Fatalf("returned frame %d ID %d is not persisted: %v", i, frame.ID, err)
		}
	}

	gin.SetMode(gin.TestMode)
	handler := &Service{Continuity: continuity}
	router := gin.New()
	router.PUT("/api/projects/:id/scenes/:sid/continuity/frames/select", handler.HandleSelectFrame)
	body, err := json.Marshal(map[string]uint{"frame_id": frames[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPut,
		fmt.Sprintf("/api/projects/%d/scenes/%d/continuity/frames/select", project.ID, scene.ID),
		bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("select returned HTTP %d: %s", response.Code, response.Body.String())
	}
	var payload struct {
		Frame struct {
			ID       uint `json:"id"`
			Selected bool `json:"selected"`
		} `json:"frame"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Frame.ID != frames[0].ID || !payload.Frame.Selected {
		t.Fatalf("unexpected selected frame response: %s", response.Body.String())
	}
}
