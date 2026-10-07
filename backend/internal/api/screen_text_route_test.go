package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
	"comfyui-console/internal/service"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestScreenTextCueRoutes(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Project{}, &models.Scene{}, &models.Character{}, &models.ScreenTextCue{}); err != nil {
		t.Fatal(err)
	}
	p1, p2 := models.Project{Title: "one"}, models.Project{Title: "two"}
	db.Create(&p1)
	db.Create(&p2)
	scene := models.Scene{ProjectID: p1.ID, EpisodeN: 1, Duration: 8}
	character := models.Character{ProjectID: p1.ID, Name: "上官若琳"}
	db.Create(&scene)
	db.Create(&character)

	svc := &service.Service{DB: db, ScreenTexts: service.NewScreenTextService(db)}
	router := NewRouter(&config.Config{}, svc)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		router.ServeHTTP(response, req)
		return response
	}
	base := fmt.Sprintf("/api/projects/%d/screen-text-cues", p1.ID)
	body := fmt.Sprintf(`{"episode_n":1,"scene_id":%d,"character_id":%d,"kind":"character_intro","text":"上官若琳","start_time":0,"end_time":2,"writing_mode":"vertical-rl"}`, scene.ID, character.ID)
	response := request(http.MethodPost, base, body)
	if response.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", response.Code, response.Body.String())
	}
	var cue models.ScreenTextCue
	if err := json.Unmarshal(response.Body.Bytes(), &cue); err != nil {
		t.Fatal(err)
	}
	response = request(http.MethodGet, base+"?episode_n=1", "")
	if response.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", response.Code, response.Body.String())
	}
	response = request(http.MethodGet, base+"/preflight?episode_n=1&width=1080&height=1920", "")
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"font_unverified"`)) || !bytes.Contains(response.Body.Bytes(), []byte(`"width":1080`)) {
		t.Fatalf("preflight status=%d body=%s", response.Code, response.Body.String())
	}
	wrongProject := fmt.Sprintf("/api/projects/%d/screen-text-cues/%d", p2.ID, cue.ID)
	if response = request(http.MethodDelete, wrongProject, ""); response.Code != http.StatusNotFound {
		t.Fatalf("cross-project delete status=%d body=%s", response.Code, response.Body.String())
	}
	if response = request(http.MethodDelete, fmt.Sprintf("%s/%d", base, cue.ID), ""); response.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", response.Code, response.Body.String())
	}
}
