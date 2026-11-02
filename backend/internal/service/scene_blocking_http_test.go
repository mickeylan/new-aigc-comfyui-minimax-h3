package service

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
)

func TestSceneBlockingSaveOwnershipAndPreservesMedia(t *testing.T) {
	db := newTestDB(t)
	if err := db.AutoMigrate(&models.Scene{}); err != nil {
		t.Fatal(err)
	}
	scene := models.Scene{ProjectID: 11, EpisodeN: 1, Generation: 2, Order: 1, ImageFile: "keep.png", VideoFile: "keep.mp4", VideoFullPrompt: "approved", Status: "video_ready"}
	if err := db.Create(&scene).Error; err != nil {
		t.Fatal(err)
	}
	s := &Service{DB: db}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.PUT("/api/projects/:id/scenes/:sid/blocking", s.HandleSaveSceneBlocking)
	body := `{"facts":["甲在门边"],"suggestions":["建议侧机位"],"characters":[{"name":"甲","position":"门边","facing":"门外","gaze":"乙","visibility":"无遮挡"}],"camera":"侧面","start":"站立","end":"迈出","summary":"甲在门边"}`
	request := func(project uint) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		q := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/projects/%d/scenes/%d/blocking", project, scene.ID), bytes.NewBufferString(body))
		q.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, q)
		return w
	}
	if w := request(99); w.Code != http.StatusNotFound {
		t.Fatalf("cross-project status=%d", w.Code)
	}
	if w := request(scene.ProjectID); w.Code != http.StatusOK {
		t.Fatalf("save status=%d body=%s", w.Code, w.Body.String())
	}
	var got models.Scene
	db.First(&got, scene.ID)
	if got.ImageFile != "keep.png" || got.VideoFile != "keep.mp4" || got.VideoFullPrompt != "approved" || got.Status != "video_ready" {
		t.Fatalf("media changed: %+v", got)
	}
	if !got.PromptStale || got.BlockingJSON == "" {
		t.Fatalf("blocking not saved/staled: %+v", got)
	}
}

func TestSceneBlockingAnalyzePreviewDoesNotWrite(t *testing.T) {
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"facts\":[\"甲站左侧\"],\"suggestions\":[\"建议侧拍\"],\"characters\":[],\"camera\":\"\",\"start\":\"\",\"end\":\"\",\"summary\":\"站位预览\"}"}}]}`)
	}))
	defer ai.Close()
	s, db, scene := expandHTTPService(t, ai.URL)
	if err := s.Skills.InitSystemSkills(); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/projects/:id/scenes/:sid/blocking/analyze", s.HandleAnalyzeSceneBlocking)
	w := httptest.NewRecorder()
	q := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/projects/%d/scenes/%d/blocking/analyze", scene.ProjectID, scene.ID), nil)
	r.ServeHTTP(w, q)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var got models.Scene
	db.First(&got, scene.ID)
	if got.BlockingJSON != "" {
		t.Fatalf("preview persisted: %s", got.BlockingJSON)
	}
}
