package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
	"comfyui-console/internal/service"
)

func TestMusicCueSheetRoutes(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Project{}, &models.Scene{}, &models.MusicCueSheet{}); err != nil {
		t.Fatal(err)
	}
	p1 := models.Project{Title: "one"}
	p2 := models.Project{Title: "two"}
	db.Create(&p1)
	db.Create(&p2)
	scene := models.Scene{ProjectID: p1.ID, EpisodeN: 1, Generation: 1, Order: 1, Duration: 10}
	db.Create(&scene)

	svc := &service.Service{DB: db, MusicCues: service.NewMusicCueService(db)}
	router := NewRouter(&config.Config{}, svc)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		router.ServeHTTP(resp, req)
		return resp
	}

	base := fmt.Sprintf("/api/projects/%d/music-cue-sheets", p1.ID)
	body := fmt.Sprintf(`{"episode_n":1,"scene_id":%d,"kind":"action","start_time":0,"end_time":5,"intensity":"intense","mood":"tension"}`, scene.ID)
	resp := request(http.MethodPost, base, body)
	if resp.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", resp.Code, resp.Body.String())
	}
	var cue models.MusicCueSheet
	if err := json.Unmarshal(resp.Body.Bytes(), &cue); err != nil {
		t.Fatal(err)
	}

	resp = request(http.MethodGet, base+"?episode_n=1", "")
	if resp.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", resp.Code, resp.Body.String())
	}
	var listed []models.MusicCueSheet
	if err := json.Unmarshal(resp.Body.Bytes(), &listed); err != nil || len(listed) != 1 || listed[0].ID != cue.ID {
		t.Fatalf("unexpected list: %+v err=%v", listed, err)
	}

	resp = request(http.MethodPut, fmt.Sprintf("%s/%d", base, cue.ID), `{"episode_n":1,"kind":"emotion","start_time":1,"end_time":4,"intensity":"soft","mood":"calm"}`)
	if resp.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", resp.Code, resp.Body.String())
	}

	wrongProjectPath := fmt.Sprintf("/api/projects/%d/music-cue-sheets/%d", p2.ID, cue.ID)
	resp = request(http.MethodDelete, wrongProjectPath, "")
	if resp.Code != http.StatusNotFound {
		t.Fatalf("cross-project delete status=%d body=%s", resp.Code, resp.Body.String())
	}
	resp = request(http.MethodDelete, fmt.Sprintf("%s/%d", base, cue.ID), "")
	if resp.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestMusicCuePreflightRouteAndBatchConflict(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Project{}, &models.Scene{}, &models.Shot{}, &models.Dialogue{}, &models.MusicCueSheet{}); err != nil {
		t.Fatal(err)
	}
	p := models.Project{Title: "p"}
	db.Create(&p)
	db.Create(&models.Scene{ProjectID: p.ID, EpisodeN: 1, Duration: 5})
	svc := &service.Service{DB: db, MusicCues: service.NewMusicCueService(db)}
	router := NewRouter(&config.Config{}, svc)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/projects/%d/music-cue-sheets/preflight?episode_n=1", p.ID), nil))
	if resp.Code != http.StatusOK || !bytes.Contains(resp.Body.Bytes(), []byte(`"code":"empty"`)) {
		t.Fatalf("preflight status=%d body=%s", resp.Code, resp.Body.String())
	}
	resp = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/projects/%d/music-cue-sheets/batch-approve", p.ID), bytes.NewBufferString(`{"episode_n":1}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusConflict {
		t.Fatalf("expected conflict, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestMusicCueSubmissionManifestRouteIsReviewGatedAndProjectScoped(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Project{}, &models.Scene{}, &models.Shot{}, &models.Dialogue{}, &models.MusicCueSheet{}); err != nil {
		t.Fatal(err)
	}
	p1, p2 := models.Project{Title: "one"}, models.Project{Title: "two"}
	db.Create(&p1)
	db.Create(&p2)
	db.Create(&models.Scene{ProjectID: p1.ID, EpisodeN: 1, Duration: 5})
	svc := &service.Service{DB: db, MusicCues: service.NewMusicCueService(db)}
	router := NewRouter(&config.Config{}, svc)
	caption := `{"global_metadata":"score","vocal_details":"Fully instrumental","arrangement":"Intro: build. Outro: stop."}`
	cue, err := svc.MusicCues.Create(p1.ID, service.MusicCueSheetInput{EpisodeN: 1, Kind: "scene", StartTime: 0, EndTime: 5, CaptionJSON: caption})
	if err != nil {
		t.Fatal(err)
	}
	request := func(projectID uint) *httptest.ResponseRecorder {
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/projects/%d/music-cue-sheets/submission-manifest?episode_n=1", projectID), nil))
		return resp
	}
	if resp := request(p1.ID); resp.Code != http.StatusConflict {
		t.Fatalf("draft manifest status=%d body=%s", resp.Code, resp.Body.String())
	}
	if _, err := svc.MusicCues.Review(p1.ID, cue.ID, true, "reviewed"); err != nil {
		t.Fatal(err)
	}
	resp := request(p1.ID)
	if resp.Code != http.StatusOK || !bytes.Contains(resp.Body.Bytes(), []byte(`"ready":true`)) || !bytes.Contains(resp.Body.Bytes(), []byte("### Global Metadata")) {
		t.Fatalf("manifest status=%d body=%s", resp.Code, resp.Body.String())
	}
	if resp := request(p2.ID); resp.Code != http.StatusConflict {
		t.Fatalf("cross-project manifest status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestMusicCueSheetBatchApprove(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Project{}, &models.Scene{}, &models.Shot{}, &models.Dialogue{}, &models.MusicCueSheet{}); err != nil {
		t.Fatal(err)
	}
	p := models.Project{Title: "proj"}
	db.Create(&p)
	db.Create(&models.Scene{ProjectID: p.ID, EpisodeN: 1, Generation: 1, Order: 1, Duration: 6, Title: "scene"})
	svc := &service.Service{DB: db, MusicCues: service.NewMusicCueService(db)}
	router := NewRouter(&config.Config{}, svc)

	caption := `{"global_metadata":"cinematic score","vocal_details":"Fully instrumental, no voices.","arrangement":"Intro: sparse. Outro: resolve."}`
	for _, times := range [][2]float64{{0, 3}, {3, 6}} {
		if _, err := svc.MusicCues.Create(p.ID, service.MusicCueSheetInput{EpisodeN: 1, Kind: "scene", StartTime: times[0], EndTime: times[1], Intensity: "medium", CaptionJSON: caption}); err != nil {
			t.Fatal(err)
		}
	}

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/projects/%d/music-cue-sheets/batch-approve", p.ID), bytes.NewBufferString(`{"episode_n":1,"review_reason":"reviewed"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("batch approve status=%d body=%s", resp.Code, resp.Body.String())
	}

	var cues []models.MusicCueSheet
	db.Find(&cues)
	for _, cue := range cues {
		if cue.ReviewStatus != "approved" {
			t.Fatalf("cue %d: expected approved, got %s", cue.ID, cue.ReviewStatus)
		}
	}
}
