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

func shotFrameFixture(t *testing.T) (*translationHTTPFixture, *gin.Engine, models.Shot) {
	t.Helper()
	f := newTranslationHTTPFixture(t)
	if err := f.db.AutoMigrate(&models.ScenePrevisualization{}, &models.ShotPrevisFrame{}, &models.Episode{}, &models.PromptPolicyOverride{}, &models.Task{}); err != nil {
		t.Fatal(err)
	}
	d := timelineFixture()
	raw, _ := json.Marshal(d)
	if err := f.db.Create(&models.ScenePrevisualization{ProjectID: f.p.ID, SceneID: f.sc.ID, SchemaVersion: 2, Revision: 1, SourceContentHash: hashString(f.sc.Content), DocumentJSON: string(raw)}).Error; err != nil {
		t.Fatal(err)
	}
	var shot models.Shot
	f.db.Where("scene_id=?", f.sc.ID).First(&shot)
	r := gin.New()
	r.GET("/:id/:sid/frames", f.svc.HandleListShotPrevisFrames)
	r.POST("/:id/:sid/frames", f.svc.HandleBindShotPrevisFrame)
	r.POST("/:id/:sid/frames/:fid/preview", f.svc.HandlePreviewShotPrevisFramePrompt)
	r.POST("/:id/:sid/frames/:fid/apply", f.svc.HandleApplyShotPrevisFramePrompt)
	return f, r, shot
}
func frameRequest(t *testing.T, r *gin.Engine, method, path string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(payload)
	q := httptest.NewRequest(method, path, bytes.NewReader(raw))
	q.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, q)
	return w
}
func bindFrame(t *testing.T, f *translationHTTPFixture, r *gin.Engine, shot models.Shot, timeMS int) models.ShotPrevisFrame {
	t.Helper()
	w := frameRequest(t, r, "POST", fmt.Sprintf("/%d/%d/frames", f.p.ID, f.sc.ID), map[string]any{"shot_id": shot.ID, "previs_revision": 1, "source_content_hash": hashString(f.sc.Content), "time_ms": timeMS})
	if w.Code != 200 {
		t.Fatalf("bind %d %s", w.Code, w.Body.String())
	}
	var result struct {
		Item models.ShotPrevisFrame `json:"item"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result.Item
}
func TestShotPrevisFrameSampling(t *testing.T) {
	d := timelineFixture()
	d.Keyframes[0].Yaw = 170
	d.Keyframes[1].Yaw = -170
	end := d.Camera
	end.Position.X = 17
	d.CameraKeyframes = []previsCameraKeyframe{{TimeMS: 0, Camera: d.Camera}, {TimeMS: 2000, Camera: end}}
	before, _ := json.Marshal(d)
	frame, err := sampleShotPrevisFrame(d, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if frame.Objects[0].Position.X != 1 || math.Abs(frame.Objects[0].Yaw) != 180 || frame.Camera.Position.X != 12 {
		t.Fatalf("bad interpolation %+v", frame)
	}
	after, _ := json.Marshal(d)
	if !bytes.Equal(before, after) {
		t.Fatal("mutated original document")
	}
	d.CameraKeyframes[1].Cut = true
	frame, _ = sampleShotPrevisFrame(d, 1999)
	if frame.Camera.Position.X != 7 {
		t.Fatal("cut interpolated")
	}
	frame, _ = sampleShotPrevisFrame(d, 2000)
	if frame.Camera.Position.X != 17 {
		t.Fatal("cut endpoint missing")
	}
	if _, err = sampleShotPrevisFrame(d, 10001); err == nil {
		t.Fatal("out of range accepted")
	}
}
func TestShotPrevisFrameBindSnapshotHistoryAndOwnership(t *testing.T) {
	f, r, shot := shotFrameFixture(t)
	row := bindFrame(t, f, r, shot, 1000)
	var frame previsDocument
	json.Unmarshal([]byte(row.FrameJSON), &frame)
	if frame.Objects[0].Position.X != 1 || row.FrameHash != hashString(row.FrameJSON) || row.PrevisRevision != 1 {
		t.Fatal("frame was not derived on server")
	}
	second := bindFrame(t, f, r, shot, 2000)
	if second.ID == row.ID || row.FrameJSON == second.FrameJSON {
		t.Fatal("history overwritten")
	}
	var first models.ShotPrevisFrame
	f.db.First(&first, row.ID)
	if first.FrameHash != row.FrameHash {
		t.Fatal("original history changed")
	}
	w := frameRequest(t, r, "GET", fmt.Sprintf("/%d/%d/frames", f.p.ID, f.sc.ID), nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "同一Shot已有更新") {
		t.Fatalf("not stale %s", w.Body.String())
	}
	other := models.Scene{ProjectID: f.p.ID, Order: 2}
	f.db.Create(&other)
	foreign := models.Shot{SceneID: other.ID, Order: 1}
	f.db.Create(&foreign)
	for _, payload := range []map[string]any{{"shot_id": foreign.ID, "previs_revision": 1, "time_ms": 0, "source_content_hash": hashString(f.sc.Content)}, {"shot_id": shot.ID, "previs_revision": 2, "time_ms": 0, "source_content_hash": hashString(f.sc.Content)}, {"shot_id": shot.ID, "previs_revision": 1, "time_ms": 10001, "source_content_hash": hashString(f.sc.Content)}, {"shot_id": shot.ID, "previs_revision": 1, "time_ms": 1.5, "source_content_hash": hashString(f.sc.Content)}, {"shot_id": shot.ID, "previs_revision": 1, "source_content_hash": hashString(f.sc.Content)}} {
		w := frameRequest(t, r, "POST", fmt.Sprintf("/%d/%d/frames", f.p.ID, f.sc.ID), payload)
		if w.Code < 400 {
			t.Fatalf("accepted %+v", payload)
		}
	}
	w = frameRequest(t, r, "POST", fmt.Sprintf("/%d/%d/frames/%d/preview", f.p.ID, other.ID, row.ID), nil)
	if w.Code != 404 {
		t.Fatalf("cross-scene frame access %d", w.Code)
	}
}
func TestShotPrevisFrameStaleSources(t *testing.T) {
	for _, kind := range []string{"scene", "shot", "order", "previs", "delete"} {
		t.Run(kind, func(t *testing.T) {
			f, r, shot := shotFrameFixture(t)
			row := bindFrame(t, f, r, shot, 1000)
			switch kind {
			case "scene":
				f.db.Model(&models.Scene{}).Where("id=?", f.sc.ID).Update("content", "changed")
			case "shot":
				f.db.Model(&models.Shot{}).Where("id=?", shot.ID).Update("description", "changed")
			case "order":
				f.db.Model(&models.Shot{}).Where("id=?", shot.ID).Update("order_num", 2)
			case "previs":
				f.db.Model(&models.ScenePrevisualization{}).Where("scene_id=?", f.sc.ID).Update("revision", 2)
			case "delete":
				f.db.Delete(&shot)
			}
			w := frameRequest(t, r, "POST", fmt.Sprintf("/%d/%d/frames/%d/preview", f.p.ID, f.sc.ID, row.ID), nil)
			if w.Code != 409 || f.calls.Load() != 0 {
				t.Fatalf("stale preview %d calls %d", w.Code, f.calls.Load())
			}
			w = frameRequest(t, r, "GET", fmt.Sprintf("/%d/%d/frames", f.p.ID, f.sc.ID), nil)
			if w.Code != 200 || !strings.Contains(w.Body.String(), `"stale":true`) {
				t.Fatalf("history lost %s", w.Body.String())
			}
		})
	}
}
func TestShotPrevisFramePreviewApplyIsolationAndConflict(t *testing.T) {
	f, r, shot := shotFrameFixture(t)
	row := bindFrame(t, f, r, shot, 1000)
	f.rawResponse = true
	f.response = "[Shot 1] 人物位于画面中央，侧身面向门口，机位保持固定中景，室内自然光下呈现一个静止瞬间。"
	url := fmt.Sprintf("/%d/%d/frames/%d", f.p.ID, f.sc.ID, row.ID)
	preview := func() string {
		w := frameRequest(t, r, "POST", url+"/preview", nil)
		if w.Code != 200 {
			t.Fatalf("preview %d %s", w.Code, w.Body.String())
		}
		var data struct {
			SourceVersion string `json:"source_version"`
		}
		json.Unmarshal(w.Body.Bytes(), &data)
		return data.SourceVersion
	}
	version := preview()
	var current models.Scene
	f.db.First(&current, f.sc.ID)
	if current.ImagePrompt != f.sc.ImagePrompt || current.BlockingJSON != f.sc.BlockingJSON {
		t.Fatal("preview persisted changes")
	}
	body := map[string]any{"prompt": "人工审阅的选帧分镜图提示词", "source_version": version, "confirm_replace": true}
	f.db.Model(&models.Project{}).Where("id=?", f.p.ID).Update("style", "new style")
	w := frameRequest(t, r, "POST", url+"/apply", body)
	if w.Code != 409 {
		t.Fatalf("source mismatch accepted %d", w.Code)
	}
	body["source_version"] = preview()
	w = frameRequest(t, r, "POST", url+"/apply", body)
	if w.Code != 200 {
		t.Fatalf("apply %d %s", w.Code, w.Body.String())
	}
	f.db.First(&current, f.sc.ID)
	if current.ImagePrompt != body["prompt"] || current.Content != f.sc.Content || current.VideoFullPrompt != f.sc.VideoFullPrompt || current.BlockingJSON != f.sc.BlockingJSON || current.BlockingImageAppliedHash != "" {
		t.Fatal("apply wrote outside reviewed image prompt")
	}
	var original models.Shot
	f.db.First(&original, shot.ID)
	if preparationJSONVersion(original) != preparationJSONVersion(shot) {
		t.Fatal("Shot mutated")
	}
	var stored models.ShotPrevisFrame
	f.db.First(&stored, row.ID)
	if stored.AppliedPromptHash != hashString(body["prompt"].(string)) || stored.FrameHash != row.FrameHash {
		t.Fatal("missing independent provenance")
	}
	w = frameRequest(t, r, "POST", url+"/apply", body)
	if w.Code != 409 {
		t.Fatal("stale apply replay accepted")
	}
	var count int64
	f.db.Model(&models.Task{}).Count(&count)
	if count != 0 {
		t.Fatal("generation started")
	}
}
func TestShotPrevisFramePreviewConcurrentChange(t *testing.T) {
	f, r, shot := shotFrameFixture(t)
	row := bindFrame(t, f, r, shot, 0)
	f.rawResponse = true
	f.response = "[Shot 1] 人物在空房间中央侧身站立，固定中景机位，画面保持安静的静止瞬间。"
	f.beforeReply = func() {
		f.db.Model(&models.Shot{}).Where("id=?", shot.ID).Update("description", "changed while previewing")
	}
	w := frameRequest(t, r, "POST", fmt.Sprintf("/%d/%d/frames/%d/preview", f.p.ID, f.sc.ID, row.ID), nil)
	if w.Code != 409 {
		t.Fatalf("concurrent stale response %d %s", w.Code, w.Body.String())
	}
}
