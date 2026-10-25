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

	caption := `{"global_metadata":"East Asian fantasy story score with a lyrical theme","vocal_details":"Fully instrumental, no voices.","arrangement":"Intro: spacious phrasing follows the story environment. Outro: resolve with poetic restraint.","lyrics_input":""}`
	for _, status := range []string{"pending", "draft"} {
		cue := models.MusicCueSheet{ProjectID: p.ID, EpisodeN: 1, Kind: "scene", StartTime: 0, EndTime: 3, Intensity: "medium", CaptionJSON: caption, ReviewStatus: status}
		db.Create(&cue)
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
