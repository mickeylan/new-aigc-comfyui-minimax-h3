package service

import (
	"bytes"
	"comfyui-console/internal/models"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestShotPrevisFramePromptTargetsSelectedShotAndSampledGeometry(t *testing.T) {
	f, r, _ := shotFrameFixture(t)
	chosen := models.Shot{SceneID: f.sc.ID, Order: 2, Description: "SELECTED_SHOT_ONLY: 甲在门旁转身"}
	if err := f.db.Create(&chosen).Error; err != nil {
		t.Fatal(err)
	}
	row := bindFrame(t, f, r, chosen, 1000)
	captured := make(chan string, 1)
	handler := f.ai.Config.Handler
	f.ai.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		raw, _ := io.ReadAll(req.Body)
		req.Body = io.NopCloser(bytes.NewReader(raw))
		captured <- string(raw)
		handler.ServeHTTP(w, req)
	})
	f.rawResponse = true
	f.response = "[Shot 1] 甲在门旁侧身站立，镜头保持固定中景，人物与门形成清晰的空间层次。"
	w := frameRequest(t, r, "POST", fmt.Sprintf("/%d/%d/frames/%d/preview", f.p.ID, f.sc.ID, row.ID), nil)
	if w.Code != 200 {
		t.Fatalf("preview %d %s", w.Code, w.Body.String())
	}
	var payload struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(<-captured), &payload); err != nil {
		t.Fatal(err)
	}
	user := ""
	for _, m := range payload.Messages {
		if m.Role == "user" {
			user += m.Content
		}
	}
	if !strings.Contains(user, "SELECTED_SHOT_ONLY") || !strings.Contains(user, "(1.000,0.000,0.000)") || !strings.Contains(user, "GLB片段仅是预演") || !strings.Contains(user, "天气不从预演自动注入") {
		t.Fatalf("target or sampled geometry missing: %s", user)
	}
	var sc models.Scene
	if err := f.db.First(&sc, f.sc.ID).Error; err != nil {
		t.Fatal(err)
	}
	if sc.ImagePrompt != f.sc.ImagePrompt || sc.Content != f.sc.Content {
		t.Fatal("read-only preview changed scene")
	}
}
func TestShotPrevisFrameAdoptionRejectsMissingConfirmationAndRebinding(t *testing.T) {
	f, r, shot := shotFrameFixture(t)
	row := bindFrame(t, f, r, shot, 1000)
	version, err := f.svc.shotFrameSource(f.db, &f.sc, &row)
	if err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/%d/%d/frames/%d/apply", f.p.ID, f.sc.ID, row.ID)
	w := frameRequest(t, r, "POST", path, map[string]any{"prompt": "reviewed draft", "source_version": version})
	if w.Code != 400 {
		t.Fatal("confirmation not required")
	}
	bindFrame(t, f, r, shot, 2000)
	w = frameRequest(t, r, "POST", path, map[string]any{"prompt": "reviewed draft", "source_version": version, "confirm_replace": true})
	if w.Code != 409 {
		t.Fatalf("rebound old preview accepted %d", w.Code)
	}
	var tasks int64
	if err := f.db.Model(&models.Task{}).Count(&tasks).Error; err != nil {
		t.Fatal(err)
	}
	if tasks != 0 {
		t.Fatal("generated task")
	}
}
