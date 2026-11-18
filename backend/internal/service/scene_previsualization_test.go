package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/http/httptest"
	"testing"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
)

func fixturePrevis() previsDocument {
	return previsDocument{SchemaVersion: 1, Objects: []previsObject{{ID: "character-1", Name: "甲", Kind: "character", Position: previsVector{}, Width: 0.6, Height: 1.7, Depth: 0.4}}, Camera: previsCamera{Position: previsVector{X: 7, Y: 5, Z: 9}, Target: previsVector{Y: 1}, FOV: 45}, Views: []previsView{}}
}
func TestPrevisualizationValidation(t *testing.T) {
	valid := fixturePrevis()
	if err := validatePrevisDocument(valid); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		change func(*previsDocument)
	}{
		{"schema", func(d *previsDocument) { d.SchemaVersion = 3 }},
		{"NaN", func(d *previsDocument) { d.Camera.FOV = math.NaN() }},
		{"infinity", func(d *previsDocument) { d.Objects[0].Yaw = math.Inf(1) }},
		{"zero camera distance", func(d *previsDocument) { d.Camera.Position = d.Camera.Target }},
		{"negative size", func(d *previsDocument) { d.Objects[0].Width = -1 }},
		{"duplicate ID", func(d *previsDocument) { d.Objects = append(d.Objects, d.Objects[0]) }},
		{"too many", func(d *previsDocument) { d.Objects = make([]previsObject, 33) }},
		{"duplicate view", func(d *previsDocument) {
			d.Views = []previsView{{Name: "A", Camera: d.Camera}, {Name: "A", Camera: d.Camera}}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := fixturePrevis()
			tc.change(&d)
			if validatePrevisDocument(d) == nil {
				t.Fatal("invalid rehearsal accepted")
			}
		})
	}
}

func TestPrevisualizationHTTPSaveCASAndIsolation(t *testing.T) {
	f := newTranslationHTTPFixture(t)
	if err := f.db.AutoMigrate(&models.ScenePrevisualization{}); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	base := "/api/projects/:id/scenes/:sid/previsualization"
	r.GET(base, f.svc.HandleGetScenePrevisualization)
	r.PUT(base, f.svc.HandleSaveScenePrevisualization)
	path := fmt.Sprintf("/api/projects/%d/scenes/%d/previsualization", f.p.ID, f.sc.ID)
	request := func(method, path string, payload any) *httptest.ResponseRecorder {
		data, _ := json.Marshal(payload)
		q := httptest.NewRequest(method, path, bytes.NewReader(data))
		q.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, q)
		return w
	}
	w := request("GET", path, nil)
	if w.Code != 200 {
		t.Fatalf("get=%d %s", w.Code, w.Body.String())
	}
	var count int64
	f.db.Model(&models.ScenePrevisualization{}).Count(&count)
	if count != 0 {
		t.Fatal("GET wrote a rehearsal")
	}
	body := map[string]any{"revision": 0, "source_content_hash": hashString(f.sc.Content), "document": fixturePrevis()}
	w = request("PUT", path, body)
	if w.Code != 200 {
		t.Fatalf("save=%d %s", w.Code, w.Body.String())
	}
	if w = request("PUT", path, body); w.Code != 409 {
		t.Fatalf("duplicate create=%d", w.Code)
	}
	body["revision"] = 1
	w = request("PUT", path, body)
	if w.Code != 200 {
		t.Fatalf("CAS save=%d %s", w.Code, w.Body.String())
	}
	if w = request("PUT", path, body); w.Code != 409 {
		t.Fatalf("obsolete revision=%d", w.Code)
	}
	if w = request("GET", fmt.Sprintf("/api/projects/%d/scenes/%d/previsualization", f.p.ID+99, f.sc.ID), nil); w.Code != 404 {
		t.Fatalf("cross project=%d", w.Code)
	}
	var scene models.Scene
	f.db.First(&scene, f.sc.ID)
	if scene.BlockingJSON != f.sc.BlockingJSON || scene.VideoFullPrompt != f.sc.VideoFullPrompt || scene.VideoFile != f.sc.VideoFile || scene.VideoPromptMasterZH != f.sc.VideoPromptMasterZH {
		t.Fatalf("saving rehearsal changed adopted inputs/media: %+v", scene)
	}
	f.db.Model(&models.Scene{}).Where("id = ?", f.sc.ID).Update("content", "updated story")
	body["revision"] = 2
	if w = request("PUT", path, body); w.Code != 409 {
		t.Fatalf("stale story=%d", w.Code)
	}
	var row models.ScenePrevisualization
	f.db.First(&row)
	if row.Revision != 2 {
		t.Fatal("stale write modified rehearsal")
	}
	w = request("GET", path, nil)
	var result map[string]any
	json.Unmarshal(w.Body.Bytes(), &result)
	if result["stale"] != true {
		t.Fatalf("not marked stale: %s", w.Body.String())
	}
}
