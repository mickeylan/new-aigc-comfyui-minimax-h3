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

func TestAudioLayerRoutes(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Project{}, &models.Scene{}, &models.AudioLayer{}); err != nil {
		t.Fatal(err)
	}
	p1 := models.Project{Title: "one"}
	p2 := models.Project{Title: "two"}
	db.Create(&p1)
	db.Create(&p2)
	scene := models.Scene{ProjectID: p1.ID, EpisodeN: 1, Generation: 1, Order: 1}
	db.Create(&scene)

	svc := &service.Service{DB: db, AudioLayers: service.NewAudioLayerService(db)}
	router := NewRouter(&config.Config{}, svc)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		response := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		router.ServeHTTP(response, req)
		return response
	}

	base := fmt.Sprintf("/api/projects/%d/audio-layers", p1.ID)
	body := fmt.Sprintf(`{"episode_n":1,"scene_id":%d,"kind":"sfx","name":"door","start_time":0,"end_time":1,"volume":2}`, scene.ID)
	response := request(http.MethodPost, base, body)
	if response.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", response.Code, response.Body.String())
	}
	var layer models.AudioLayer
	if err := json.Unmarshal(response.Body.Bytes(), &layer); err != nil {
		t.Fatal(err)
	}

	response = request(http.MethodGet, base+"?episode_n=1", "")
	if response.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", response.Code, response.Body.String())
	}
	var listed []models.AudioLayer
	if err := json.Unmarshal(response.Body.Bytes(), &listed); err != nil || len(listed) != 1 || listed[0].ID != layer.ID {
		t.Fatalf("unexpected list: %+v err=%v", listed, err)
	}

	response = request(http.MethodPut, fmt.Sprintf("%s/%d", base, layer.ID), `{"episode_n":1,"kind":"bgm","name":"score","start_time":1,"end_time":3}`)
	if response.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", response.Code, response.Body.String())
	}

	wrongProjectPath := fmt.Sprintf("/api/projects/%d/audio-layers/%d", p2.ID, layer.ID)
	response = request(http.MethodDelete, wrongProjectPath, "")
	if response.Code != http.StatusNotFound {
		t.Fatalf("cross-project delete status=%d body=%s", response.Code, response.Body.String())
	}
	response = request(http.MethodDelete, fmt.Sprintf("%s/%d", base, layer.ID), "")
	if response.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", response.Code, response.Body.String())
	}
}
