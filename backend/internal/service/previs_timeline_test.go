package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
)

func timelineFixture() previsDocument {
	d := fixturePrevis()
	d.SchemaVersion = 2
	d.DurationMS = 10000
	d.Routes = []previsRoute{{ID: "route-a", ObjectID: "character-1", A: previsVector{}, B: previsVector{X: 2}}}
	d.Keyframes = []previsKeyframe{{ObjectID: "character-1", TimeMS: 0}, {ObjectID: "character-1", TimeMS: 2000, Position: previsVector{X: 2}, Yaw: 90}}
	return d
}
func TestPrevisTimelineVersionsAndBounds(t *testing.T) {
	if err := validatePrevisDocument(fixturePrevis()); err != nil {
		t.Fatal(err)
	}
	if err := validatePrevisDocument(timelineFixture()); err != nil {
		t.Fatal(err)
	}
	cases := []func(*previsDocument){func(d *previsDocument) { d.SchemaVersion = 4 }, func(d *previsDocument) { d.SchemaVersion = 1 }, func(d *previsDocument) { d.DurationMS = 999 }, func(d *previsDocument) { d.Keyframes[0].ObjectID = "unknown" }, func(d *previsDocument) { d.Keyframes[1].TimeMS = 0 }, func(d *previsDocument) { d.Keyframes[1].TimeMS = 10001 }, func(d *previsDocument) { d.Routes[0].B.X = 101 }, func(d *previsDocument) { d.Keyframes[1].Yaw = 361 }, func(d *previsDocument) {
		d.CameraKeyframes = []previsCameraKeyframe{{TimeMS: 0, Camera: d.Camera}, {TimeMS: 0, Camera: d.Camera}}
	}}
	for i, change := range cases {
		d := timelineFixture()
		change(&d)
		if validatePrevisDocument(d) == nil {
			t.Fatalf("accepted invalid case %d", i)
		}
	}
}
func TestPrevisTimelineSaveRoundtripAndCAS(t *testing.T) {
	f := newTranslationHTTPFixture(t)
	if err := f.db.AutoMigrate(&models.ScenePrevisualization{}); err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.PUT("/:id/:sid", f.svc.HandleSaveScenePrevisualization)
	save := func(revision int) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(map[string]any{"revision": revision, "source_content_hash": hashString(f.sc.Content), "document": timelineFixture()})
		q := httptest.NewRequest("PUT", fmt.Sprintf("/%d/%d", f.p.ID, f.sc.ID), bytes.NewReader(raw))
		q.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, q)
		return w
	}
	if w := save(0); w.Code != 200 {
		t.Fatalf("create %d %s", w.Code, w.Body.String())
	}
	if w := save(1); w.Code != 200 {
		t.Fatalf("update %d %s", w.Code, w.Body.String())
	}
	if w := save(1); w.Code != 409 {
		t.Fatalf("stale %d", w.Code)
	}
	var row models.ScenePrevisualization
	f.db.First(&row)
	if row.SchemaVersion != 2 || row.Revision != 2 {
		t.Fatalf("bad persisted version %+v", row)
	}
	var d previsDocument
	if json.Unmarshal([]byte(row.DocumentJSON), &d) != nil || validatePrevisDocument(d) != nil || len(d.Keyframes) != 2 {
		t.Fatal("timeline lost on roundtrip")
	}
	var sc models.Scene
	f.db.First(&sc, f.sc.ID)
	if sc.VideoFullPrompt != f.sc.VideoFullPrompt || sc.Content != f.sc.Content {
		t.Fatal("timeline save changed authority")
	}
}
func TestPrevisAgentPlanningReadOnlyQuestionsAndGuards(t *testing.T) {
	cases := []struct {
		name, response string
		status         int
	}{
		{"questions", `{"questions":["几秒从A走到B？"],"operations":[],"summary":"补问"}`, 200},
		{"plan", `{"questions":[],"summary":"移动","operations":[{"kind":"keyframe","keyframe":{"object_id":"character-1","time_ms":1000,"position":{"x":2,"y":0,"z":0},"yaw":90}}]}`, 200},
		{"unknown operation", `{"questions":[],"operations":[{"kind":"run_code"}]}`, 422},
		{"unknown object", `{"questions":[],"operations":[{"kind":"keyframe","keyframe":{"object_id":"foreign","time_ms":0,"position":{"x":0,"y":0,"z":0},"yaw":0}}]}`, 422},
		{"bad json", `not-json`, 422},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newTranslationHTTPFixture(t)
			f.rawResponse = true
			f.response = tc.response
			var before models.Scene
			f.db.First(&before, f.sc.ID)
			r := gin.New()
			r.POST("/:id/:sid", f.svc.HandlePlanScenePrevisualization)
			raw, _ := json.Marshal(map[string]any{"document": timelineFixture(), "command": "甲从A走到B"})
			q := httptest.NewRequest("POST", fmt.Sprintf("/%d/%d", f.p.ID, f.sc.ID), bytes.NewReader(raw))
			q.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, q)
			if w.Code != tc.status {
				t.Fatalf("status %d want %d %s", w.Code, tc.status, w.Body.String())
			}
			var after models.Scene
			f.db.First(&after, f.sc.ID)
			if preparationJSONVersion(before) != preparationJSONVersion(after) {
				t.Fatal("planning wrote Scene")
			}
			if f.calls.Load() != 1 {
				t.Fatal("unexpected model calls")
			}
		})
	}
}
