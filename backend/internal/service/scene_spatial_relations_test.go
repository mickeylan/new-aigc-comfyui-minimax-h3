package service

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
)

func TestSceneSpatialRelationsBackwardCompatibleAndAllKinds(t *testing.T) {
	old, err := parseSceneBlocking(`{"facts":[],"suggestions":[],"characters":[],"camera":"","start":"","end":"","summary":"legacy","unknown_old_key":true}`)
	if err != nil || old.Summary != "legacy" || len(old.Relations) != 0 {
		t.Fatalf("legacy JSON rejected: %+v, %v", old, err)
	}
	raw := `{"schema_version":2,"facts":[],"suggestions":[],"characters":[],"relations":[` +
		`{"kind":"interpersonal","subject":"群臣","reference":"皇帝","relation":"面向","source":"fact","evidence":["群臣面向皇帝"]},` +
		`{"kind":"camera","subject":"机位","reference":"群臣","relation":"位于后方","source":"suggestion"},` +
		`{"kind":"object_function","subject":"剑尖","reference":"目标","relation":"可能朝向","source":"suggestion"},` +
		`{"kind":"motion","subject":"甲","reference":"殿门","relation":"移向","start":"殿中","end":"门边","functional_end":"离场","trajectory":"直线","source":"fact","evidence":["甲走向殿门"]}` +
		`],"camera":"","start":"","end":"","summary":""}`
	got, err := parseSceneBlocking(raw)
	if err != nil || len(got.Relations) != 4 {
		t.Fatalf("relations rejected: %+v, %v", got.Relations, err)
	}
}

func TestSceneSpatialRelationsValidationAndCaps(t *testing.T) {
	invalid := []string{
		`{"relations":[{"kind":"invalid","subject":"甲","reference":"乙","relation":"左侧","source":"suggestion"}]}`,
		`{"relations":[{"kind":"camera","subject":"","reference":"乙","relation":"后方","source":"suggestion"}]}`,
		`{"relations":[{"kind":"interpersonal","subject":"甲","reference":"乙","relation":"面向","source":"fact","evidence":[" "]}]}`,
		`{"relations":[{"kind":"motion","subject":"甲","reference":"乙","relation":"靠近","source":"guess"}]}`,
	}
	for _, raw := range invalid {
		if _, err := parseSceneBlocking(raw); err == nil {
			t.Fatalf("accepted malformed relation: %s", raw)
		}
	}
	items := strings.Repeat(`{"kind":"camera","subject":"机位","reference":"甲","relation":"后方","source":"suggestion"},`, 41)
	items = strings.TrimSuffix(items, ",")
	if _, err := parseSceneBlocking(`{"relations":[` + items + `]}`); err == nil {
		t.Fatal("accepted more than 40 relations")
	}
}

func TestSceneBlockingSaveRejectsStaleSourceHash(t *testing.T) {
	db := newTestDB(t)
	if err := db.AutoMigrate(&models.Scene{}); err != nil {
		t.Fatal(err)
	}
	scene := models.Scene{ProjectID: 31, EpisodeN: 1, Generation: 1, Order: 1, Content: "current", BlockingJSON: "keep"}
	if err := db.Create(&scene).Error; err != nil {
		t.Fatal(err)
	}
	s := &Service{DB: db}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.PUT("/api/projects/:id/scenes/:sid/blocking", s.HandleSaveSceneBlocking)
	body := `{"source_content_hash":"stale","facts":[],"suggestions":[],"characters":[],"camera":"","start":"","end":"","summary":""}`
	w := httptest.NewRecorder()
	q := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/projects/%d/scenes/%d/blocking", scene.ProjectID, scene.ID), bytes.NewBufferString(body))
	q.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, q)
	if w.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var got models.Scene
	db.First(&got, scene.ID)
	if got.BlockingJSON != "keep" {
		t.Fatalf("stale request persisted: %s", got.BlockingJSON)
	}
}
