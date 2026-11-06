package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
)

func advisoryReportsRouter(f *sceneAdvisoryFixture) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/projects/:id/scenes/:sid/storyboard/reports", f.s.HandleGetSceneAdvisoryReports)
	r.POST("/api/projects/:id/scenes/:sid/storyboard/reports", f.s.HandleSaveSceneAdvisoryReport)
	r.POST("/api/projects/:id/scenes/:sid/storyboard/reports/:rid/correction/preview", f.s.HandlePreviewSceneAdvisoryCorrection)
	r.POST("/api/projects/:id/scenes/:sid/storyboard/reports/:rid/correction/apply", f.s.HandleApplySceneAdvisoryCorrection)
	return r
}

func reportHTTP(t *testing.T, r http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var b bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&b).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &b)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func currentReportSourceHash(t *testing.T, f *sceneAdvisoryFixture) string {
	t.Helper()
	var scene models.Scene
	if err := f.db.First(&scene, f.scene.ID).Error; err != nil {
		t.Fatal(err)
	}
	_, imageHash, _, err := f.s.currentStoryboardImageSnapshot(&scene)
	if err != nil {
		t.Fatal(err)
	}
	return sceneVisualSourceHash(&scene, imageHash)
}

func saveReportHTTP(t *testing.T, f *sceneAdvisoryFixture, status string, checks []SceneVisualCheckItem) SceneAdvisoryReport {
	t.Helper()
	path := fmt.Sprintf("/api/projects/%d/scenes/%d/storyboard/reports", f.scene.ProjectID, f.scene.ID)
	w := reportHTTP(t, advisoryReportsRouter(f), http.MethodPost, path, map[string]any{
		"source_hash": currentReportSourceHash(t, f), "status": status, "checks": checks, "uncertainty": []string{"光线略暗"},
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("save status=%d body=%s", w.Code, w.Body)
	}
	var got SceneAdvisoryReport
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestSceneAdvisoryReportHTTP(t *testing.T) {
	t.Run("save list persistence and bounded history", func(t *testing.T) {
		var calls atomic.Int32
		ai := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
		defer ai.Close()
		f := newSceneAdvisoryFixture(t, ai.URL, false)
		r := advisoryReportsRouter(f)
		path := fmt.Sprintf("/api/projects/%d/scenes/%d/storyboard/reports", f.scene.ProjectID, f.scene.ID)
		w := reportHTTP(t, r, http.MethodGet, path, nil)
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"items":[]`) {
			t.Fatalf("initial=%d %s", w.Code, w.Body)
		}
		before := f.scene
		check := []SceneVisualCheckItem{{Category: "orientation", Subject: "甲", Evidence: "朝内", Suggestion: "改为朝外"}}
		first := saveReportHTTP(t, f, "issues_found", check)
		if first.Origin != "user_saved" || first.Provider == "" || first.SourceHash == "" || first.Result.Status != "issues_found" {
			t.Fatalf("bad saved report: %+v", first)
		}
		for i := 0; i < 20; i++ {
			saveReportHTTP(t, f, "issues_found", check)
		}
		var after models.Scene
		if err := f.db.First(&after, f.scene.ID).Error; err != nil {
			t.Fatal(err)
		}
		var persisted []SceneAdvisoryReport
		if err := json.Unmarshal([]byte(after.AdvisoryReportsJSON), &persisted); err != nil {
			t.Fatal(err)
		}
		if len(persisted) != 20 || persisted[0].ID == first.ID {
			t.Fatalf("history ids=%d first retained=%v", len(persisted), persisted[0].ID == first.ID)
		}
		if after.Content != before.Content || after.ImageFile != before.ImageFile || after.VideoPrompt != before.VideoPrompt || after.VideoFullPrompt != before.VideoFullPrompt || after.VideoGPU != before.VideoGPU {
			t.Fatal("saving report mutated scene source/media fields")
		}
		var tasks int64
		f.db.Model(&models.Task{}).Count(&tasks)
		if tasks != 0 || calls.Load() != 0 {
			t.Fatalf("tasks=%d AI calls=%d", tasks, calls.Load())
		}
	})

	t.Run("stale ownership and validation", func(t *testing.T) {
		f := newSceneAdvisoryFixture(t, "http://unused.invalid", false)
		report := saveReportHTTP(t, f, "issues_found", []SceneVisualCheckItem{{Category: "orientation", Subject: "甲", Evidence: "朝内", Suggestion: "turn"}})
		r := advisoryReportsRouter(f)
		base := fmt.Sprintf("/api/projects/%d/scenes/%d/storyboard/reports", f.scene.ProjectID, f.scene.ID)
		if w := reportHTTP(t, r, http.MethodGet, fmt.Sprintf("/api/projects/%d/scenes/%d/storyboard/reports", f.scene.ProjectID+1, f.scene.ID), nil); w.Code != 404 {
			t.Fatalf("cross project=%d", w.Code)
		}
		if w := reportHTTP(t, r, http.MethodPost, base+"/foreign/correction/preview", nil); w.Code != 404 {
			t.Fatalf("foreign rid=%d", w.Code)
		}
		bad := reportHTTP(t, r, http.MethodPost, base, map[string]any{"source_hash": report.SourceHash, "status": "no_obvious_issues", "checks": []any{map[string]any{"category": "orientation", "suggestion": "x"}}})
		if bad.Code != 422 {
			t.Fatalf("no-obvious with checks=%d %s", bad.Code, bad.Body)
		}
		if err := os.WriteFile(filepath.Join(f.root, "input", fmt.Sprint(f.scene.ProjectID), "current.png"), []byte("changed image"), 0600); err != nil {
			t.Fatal(err)
		}
		w := reportHTTP(t, r, http.MethodGet, base, nil)
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"stale":true`) {
			t.Fatalf("stale get=%d %s", w.Code, w.Body)
		}
		if w = reportHTTP(t, r, http.MethodPost, base+"/"+report.ID+"/correction/preview", nil); w.Code != 409 {
			t.Fatalf("stale preview=%d %s", w.Code, w.Body)
		}
	})

	t.Run("preview and apply are CAS and do not enqueue work", func(t *testing.T) {
		f := newSceneAdvisoryFixture(t, "http://unused.invalid", false)
		h3 := "Subject: hero\nEnvironment: hall\nLighting: dusk\nCamera: wide\nDetailed_Description: original action\nOverall_Soundscape: wind"
		if err := f.db.Model(&models.Scene{}).Where("id=?", f.scene.ID).Updates(map[string]any{"image_prompt": h3, "video_prompt": "vp", "video_full_prompt": "vf", "video_prompt_master_zh": "master", "video_file": "v.mp4"}).Error; err != nil {
			t.Fatal(err)
		}
		report := saveReportHTTP(t, f, "issues_found", []SceneVisualCheckItem{{Category: "orientation", Subject: "甲", Evidence: "朝内", Suggestion: "face the exit"}})
		r := advisoryReportsRouter(f)
		path := fmt.Sprintf("/api/projects/%d/scenes/%d/storyboard/reports/%s/correction", f.scene.ProjectID, f.scene.ID, report.ID)
		w := reportHTTP(t, r, http.MethodPost, path+"/preview", nil)
		if w.Code != 200 {
			t.Fatalf("preview=%d %s", w.Code, w.Body)
		}
		var preview struct {
			SourceHash     string `json:"source_hash"`
			PromptHash     string `json:"prompt_hash"`
			OriginalPrompt string `json:"original_prompt"`
			ProposedPrompt string `json:"proposed_prompt"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &preview); err != nil {
			t.Fatal(err)
		}
		for _, section := range []string{"Subject:", "Environment:", "Lighting:", "Camera:", "Detailed_Description:", "Overall_Soundscape:"} {
			if !strings.Contains(preview.ProposedPrompt, section) {
				t.Fatalf("missing %s in %q", section, preview.ProposedPrompt)
			}
		}
		if preview.OriginalPrompt != h3 || !strings.Contains(preview.ProposedPrompt, "face the exit") {
			t.Fatalf("bad preview: %+v", preview)
		}
		edited := preview.ProposedPrompt + "\ncaller edit"
		w = reportHTTP(t, r, http.MethodPost, path+"/apply", map[string]any{"prompt": edited, "source_hash": preview.SourceHash, "prompt_hash": preview.PromptHash})
		if w.Code != 200 {
			t.Fatalf("apply=%d %s", w.Code, w.Body)
		}
		var got models.Scene
		f.db.First(&got, f.scene.ID)
		if got.ImagePrompt != edited || !got.PromptStale || got.VideoPrompt != "vp" || got.VideoFullPrompt != "vf" || got.VideoPromptMasterZH != "master" || got.VideoFile != "v.mp4" {
			t.Fatalf("unexpected apply mutation: %+v", got)
		}
		var tasks int64
		f.db.Model(&models.Task{}).Count(&tasks)
		if tasks != 0 {
			t.Fatalf("apply created %d tasks", tasks)
		}
		if stale := reportHTTP(t, r, http.MethodPost, path+"/apply", map[string]any{"prompt": "again", "source_hash": preview.SourceHash, "prompt_hash": preview.PromptHash}); stale.Code != 409 {
			t.Fatalf("old prompt hash=%d %s", stale.Code, stale.Body)
		}
	})

	t.Run("active image token blocks apply", func(t *testing.T) {
		f := newSceneAdvisoryFixture(t, "http://unused.invalid", false)
		report := saveReportHTTP(t, f, "issues_found", []SceneVisualCheckItem{{Category: "orientation", Subject: "甲", Evidence: "朝内", Suggestion: "turn"}})
		if err := f.db.Model(&models.Scene{}).Where("id=?", f.scene.ID).Update("image_token", "active").Error; err != nil {
			t.Fatal(err)
		}
		path := fmt.Sprintf("/api/projects/%d/scenes/%d/storyboard/reports/%s/correction/apply", f.scene.ProjectID, f.scene.ID, report.ID)
		w := reportHTTP(t, advisoryReportsRouter(f), http.MethodPost, path, map[string]any{"prompt": "edited", "source_hash": report.SourceHash, "prompt_hash": imagePromptHash(f.scene.ImagePrompt)})
		if w.Code != 409 {
			t.Fatalf("active token=%d %s", w.Code, w.Body)
		}
	})
}
