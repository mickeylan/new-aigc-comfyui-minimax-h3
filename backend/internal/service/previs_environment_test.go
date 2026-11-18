package service

import (
	"bytes"
	"comfyui-console/internal/models"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"math"
	"net/http/httptest"
	"strings"
	"testing"
)

func environmentFixture() previsDocument {
	d := timelineFixture()
	d.SchemaVersion = 3
	d.Weather = &previsWeather{Preset: "rain", Intensity: .6, Seed: 42, SunElevation: 35, FogDensity: .025}
	d.Models = []previsModelBinding{{ObjectID: "character-1", AssetHash: strings.Repeat("a", 64), FileName: "rig.glb", ClipIndex: 1, StartMS: 500, EndMS: 3500, Loop: true}}
	return d
}
func TestPrevisEnvironmentValidation(t *testing.T) {
	if err := validatePrevisDocument(environmentFixture()); err != nil {
		t.Fatal(err)
	}
	for i, change := range []func(*previsDocument){func(d *previsDocument) { d.SchemaVersion = 2 }, func(d *previsDocument) { d.Weather.Intensity = math.NaN() }, func(d *previsDocument) { d.Weather.Preset = "storm" }, func(d *previsDocument) { d.Weather.Seed = -1 }, func(d *previsDocument) { d.Weather.SunElevation = 91 }, func(d *previsDocument) { d.Weather.FogDensity = .2 }, func(d *previsDocument) { d.Models[0].ObjectID = "missing" }, func(d *previsDocument) { d.Models[0].AssetHash = "x" }, func(d *previsDocument) { d.Models[0].ClipIndex = 256 }, func(d *previsDocument) { d.Models[0].EndMS = 10001 }, func(d *previsDocument) { d.Models[0].StartMS = d.Models[0].EndMS }, func(d *previsDocument) { d.Models = append(d.Models, d.Models[0]) }} {
		d := environmentFixture()
		change(&d)
		if validatePrevisDocument(d) == nil {
			t.Fatalf("accepted invalid environment %d", i)
		}
	}
}
func TestPrevisEnvironmentRoundtripIsolationAndCAS(t *testing.T) {
	f := newTranslationHTTPFixture(t)
	if err := f.db.AutoMigrate(&models.ScenePrevisualization{}); err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.PUT("/:id/:sid", f.svc.HandleSaveScenePrevisualization)
	r.GET("/:id/:sid", f.svc.HandleGetScenePrevisualization)
	save := func(revision int) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(map[string]any{"revision": revision, "source_content_hash": hashString(f.sc.Content), "document": environmentFixture()})
		q := httptest.NewRequest("PUT", fmt.Sprintf("/%d/%d", f.p.ID, f.sc.ID), bytes.NewReader(raw))
		q.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, q)
		return w
	}
	if w := save(0); w.Code != 200 {
		t.Fatalf("save %d %s", w.Code, w.Body.String())
	}
	if w := save(0); w.Code != 409 {
		t.Fatalf("stale revision not rejected %d", w.Code)
	}
	var row models.ScenePrevisualization
	f.db.First(&row)
	var doc previsDocument
	if err := json.Unmarshal([]byte(row.DocumentJSON), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.SchemaVersion != 3 || doc.Weather.Seed != 42 || doc.Weather.SunElevation != 35 || doc.Weather.FogDensity != .025 || len(doc.Models) != 1 || doc.Models[0].AssetHash != strings.Repeat("a", 64) || doc.Models[0].ClipIndex != 1 || doc.Models[0].StartMS != 500 {
		t.Fatalf("lost environment metadata %+v", doc)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", fmt.Sprintf("/%d/%d", f.p.ID, f.sc.ID), nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "rig.glb") {
		t.Fatalf("get lost metadata %s", w.Body.String())
	}
	var sc models.Scene
	f.db.First(&sc, f.sc.ID)
	if sc.Content != f.sc.Content || sc.VideoFullPrompt != f.sc.VideoFullPrompt {
		t.Fatal("previs changed source scene")
	}
	var tasks int64
	f.db.Model(&models.Task{}).Count(&tasks)
	if tasks != 0 {
		t.Fatal("previs created generation task")
	}
}
