package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type sceneAdvisoryFixture struct {
	s     *Service
	db    *gorm.DB
	scene models.Scene
	root  string
}

func newSceneAdvisoryFixture(t *testing.T, aiURL string, vision bool) *sceneAdvisoryFixture {
	t.Helper()
	s, db, scene := expandHTTPService(t, aiURL)
	if err := db.AutoMigrate(&models.Task{}, &models.Setting{}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	dir := filepath.Join(root, "input", fmt.Sprint(scene.ProjectID))
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "current.png"), append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 504)...), 0600); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&scene).Updates(map[string]any{"image_file": "current.png", "content": "甲说：“别走。”甲又说：“别走。”", "image_prompt": "SECRET_IMAGE_PROMPT", "video_prompt": "SECRET_VIDEO_PROMPT"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&scene, scene.ID).Error; err != nil {
		t.Fatal(err)
	}
	settings := visionSettings{SettingTextProvider: "llama", SettingLlamaBaseURL: aiURL, SettingLlamaModel: "vision", SettingLlamaVisionEnabled: fmt.Sprint(vision)}
	s.Cfg = &config.Config{Storage: config.StorageConfig{DataDir: root}}
	s.TextProviderFact = NewTextProviderFactory(settings)
	return &sceneAdvisoryFixture{s: s, db: db, scene: scene, root: root}
}

func (f *sceneAdvisoryFixture) router() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/projects/:id/scenes/:sid/story-consistency", f.s.HandleGetSceneStoryConsistency)
	r.POST("/api/projects/:id/scenes/:sid/storyboard/check", f.s.HandleCheckSceneStoryboard)
	return r
}

func advisoryRequest(r http.Handler, method, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(method, path, nil))
	return w
}

func TestSceneAdvisoryHTTPSuccessIsReadOnlyAndUsesImageURL(t *testing.T) {
	var calls atomic.Int32
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		encoded, _ := json.Marshal(body)
		if !strings.Contains(string(encoded), `"type":"image_url"`) || !strings.Contains(string(encoded), "data:image/png;base64,") {
			t.Errorf("request has no image_url: %s", encoded)
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"status\":\"issues_found\",\"checks\":[{\"category\":\"orientation\",\"subject\":\"甲\",\"evidence\":\"朝向门内\",\"suggestion\":\"改为朝外\"}],\"uncertainty\":[]}"}}]}`)
	}))
	defer ai.Close()
	f := newSceneAdvisoryFixture(t, ai.URL, true)
	before, _ := json.Marshal(f.scene)
	var tasksBefore int64
	f.db.Model(&models.Task{}).Count(&tasksBefore)
	w := advisoryRequest(f.router(), http.MethodPost, fmt.Sprintf("/api/projects/%d/scenes/%d/storyboard/check", f.scene.ProjectID, f.scene.ID))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"persisted":false`) || !strings.Contains(w.Body.String(), `"status":"issues_found"`) {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
	var after models.Scene
	f.db.First(&after, f.scene.ID)
	afterJSON, _ := json.Marshal(after)
	var tasksAfter int64
	f.db.Model(&models.Task{}).Count(&tasksAfter)
	if string(before) != string(afterJSON) || tasksBefore != tasksAfter || calls.Load() != 1 {
		t.Fatalf("advisory mutated state or wrong calls: calls=%d tasks=%d/%d", calls.Load(), tasksBefore, tasksAfter)
	}
}

func TestSceneAdvisoryHTTPErrorsAndOwnership(t *testing.T) {
	var calls atomic.Int32
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		fmt.Fprint(w, `{"choices":[{"message":{"content":"not json"}}]}`)
	}))
	defer ai.Close()
	f := newSceneAdvisoryFixture(t, ai.URL, true)
	r := f.router()
	for _, path := range []string{fmt.Sprintf("/api/projects/%d/scenes/%d/story-consistency", f.scene.ProjectID+99, f.scene.ID), fmt.Sprintf("/api/projects/%d/scenes/%d/storyboard/check", f.scene.ProjectID+99, f.scene.ID)} {
		method := http.MethodGet
		if strings.Contains(path, "storyboard") {
			method = http.MethodPost
		}
		if w := advisoryRequest(r, method, path); w.Code != 404 {
			t.Errorf("cross-project %s = %d", path, w.Code)
		}
	}
	f.s.TextProviderFact = NewTextProviderFactory(visionSettings{SettingTextProvider: "llama", SettingLlamaBaseURL: ai.URL, SettingLlamaVisionEnabled: "false"})
	w := advisoryRequest(r, http.MethodPost, fmt.Sprintf("/api/projects/%d/scenes/%d/storyboard/check", f.scene.ProjectID, f.scene.ID))
	if w.Code != 422 || !strings.Contains(w.Body.String(), `"code":"multimodal_unsupported"`) {
		t.Fatalf("unsupported=%d %s", w.Code, w.Body)
	}
	f.s.TextProviderFact = NewTextProviderFactory(visionSettings{SettingTextProvider: "llama", SettingLlamaBaseURL: ai.URL, SettingLlamaModel: "vision", SettingLlamaVisionEnabled: "true"})
	f.db.Model(&f.scene).Update("image_file", "missing.png")
	if w = advisoryRequest(r, http.MethodPost, fmt.Sprintf("/api/projects/%d/scenes/%d/storyboard/check", f.scene.ProjectID, f.scene.ID)); w.Code != 422 {
		t.Fatalf("missing=%d %s", w.Code, w.Body)
	}
	if calls.Load() != 0 {
		t.Fatal("HTTP called for preflight failures")
	}
	f.db.Model(&f.scene).Update("image_file", "current.png")
	if w = advisoryRequest(r, http.MethodPost, fmt.Sprintf("/api/projects/%d/scenes/%d/storyboard/check", f.scene.ProjectID, f.scene.ID)); w.Code != 502 {
		t.Fatalf("malformed=%d %s", w.Code, w.Body)
	}
}

func TestSceneAdvisoryHTTPRejectsTraversalBeforeHTTP(t *testing.T) {
	var calls atomic.Int32
	ai := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer ai.Close()
	f := newSceneAdvisoryFixture(t, ai.URL, true)
	f.db.Model(&f.scene).Update("image_file", "../current.png")
	w := advisoryRequest(f.router(), http.MethodPost, fmt.Sprintf("/api/projects/%d/scenes/%d/storyboard/check", f.scene.ProjectID, f.scene.ID))
	if w.Code != 422 || calls.Load() != 0 {
		t.Fatalf("traversal status=%d calls=%d", w.Code, calls.Load())
	}
}

func TestSceneAdvisoryHTTPConcurrentEditReturnsConflict(t *testing.T) {
	var f *sceneAdvisoryFixture
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := f.db.Model(&models.Scene{}).Where("id = ?", f.scene.ID).Update("content", "concurrent edit").Error; err != nil {
			t.Error(err)
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"status\":\"no_obvious_issues\",\"checks\":[],\"uncertainty\":[]}"}}]}`)
	}))
	defer ai.Close()
	f = newSceneAdvisoryFixture(t, ai.URL, true)
	w := advisoryRequest(f.router(), http.MethodPost, fmt.Sprintf("/api/projects/%d/scenes/%d/storyboard/check", f.scene.ProjectID, f.scene.ID))
	if w.Code != 409 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
	var got models.Scene
	f.db.First(&got, f.scene.ID)
	if got.Content != "concurrent edit" {
		t.Fatalf("edit overwritten: %q", got.Content)
	}
}

func TestSceneAdvisoryHTTPStoryConsistencyRepeatedQuotesAndStaleShots(t *testing.T) {
	f := newSceneAdvisoryFixture(t, "http://unused.invalid", false)
	f.db.Create(&models.Dialogue{ProjectID: f.scene.ProjectID, SceneID: f.scene.ID, Order: 1, Character: "甲", Text: "别走。", SpeechType: "dialogue"})
	f.db.Create(&models.Dialogue{ProjectID: f.scene.ProjectID, SceneID: f.scene.ID, Order: 2, Character: "甲", Text: "别走。", SpeechType: "dialogue"})
	f.db.Create(&models.Shot{SceneID: f.scene.ID, Order: 1, Dialogue: "旧台词"})
	f.db.Model(&f.scene).Updates(map[string]any{"director_source_stale": true, "prompt_stale": true})
	w := advisoryRequest(f.router(), http.MethodGet, fmt.Sprintf("/api/projects/%d/scenes/%d/story-consistency", f.scene.ProjectID, f.scene.ID))
	body := w.Body.String()
	if w.Code != 200 || !strings.Contains(body, `"status":"attention"`) || !strings.Contains(body, `"dialogue_count":2`) || !strings.Contains(body, `"content_hash"`) {
		t.Fatalf("status=%d body=%s", w.Code, body)
	}
	for _, secret := range []string{"别走", "旧台词", "SECRET_IMAGE_PROMPT", "SECRET_VIDEO_PROMPT"} {
		if strings.Contains(body, secret) {
			t.Fatalf("response leaked source %q: %s", secret, body)
		}
	}
}
