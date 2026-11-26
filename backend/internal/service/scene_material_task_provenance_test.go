package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
)

// Controlled completion boundary, not a real model/decoder test. The Scene and
// CreateTask paths are real; submission is held so no scheduler/provider escapes.
func assertSceneMaterialTaskProvenance(t *testing.T, f *translationHTTPFixture, material models.Material, manifest string) {
	t.Helper()
	pool, err := f.db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(1)
	if err := f.db.AutoMigrate(&models.Setting{}, &models.Instance{}, &models.Event{}); err != nil {
		t.Fatal(err)
	}
	configureDefaultRef2VFixture(t, f)
	tpl := loadTemplateForTest(t, "minimax_h3_ref2v_3d.json")
	tpl.ID = 0
	tpl.Enabled = true
	if err := f.db.Create(&tpl).Error; err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"aqing.png", "teahouse.png", "start.png", "first.png", "last.png"} {
		if err := f.db.Create(&models.UploadFile{TaskID: fmt.Sprint(f.p.ID), Type: "image", Name: name, Path: fmt.Sprintf("%d/%s", f.p.ID, name)}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := f.db.Model(&material).Update("derivation_manifest", manifest).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Model(&models.Scene{}).Where("id = ?", f.sc.ID).Updates(map[string]any{"video_full_prompt": "", "video_3d_ref_video": material.Name}).Error; err != nil {
		t.Fatal(err)
	}
	var scene models.Scene
	if err := f.db.First(&scene, f.sc.ID).Error; err != nil {
		t.Fatal(err)
	}
	// A saved material is not consent to spend. Unconfirmed prompts must create zero tasks.
	if err := f.svc.Projects.GenerateSceneVideo(&f.p, &scene); err == nil || !strings.Contains(err.Error(), "尚未翻译并确认") {
		t.Fatalf("unconfirmed generation=%v", err)
	}
	var count int64
	if err := f.db.Model(&models.Task{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("unconfirmed tasks=%d err=%v", count, err)
	}
	if err := f.db.Model(&models.Scene{}).Where("id = ?", scene.ID).Update("video_full_prompt", bilingualRef2VEnglish).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.First(&scene, scene.ID).Error; err != nil {
		t.Fatal(err)
	}
	cfg := f.svc.Upload.cfg
	tasks := NewTaskService(cfg, f.db, NewInstanceManager(cfg, f.db, f.svc.Remote), nil, NewHub(), f.svc.Remote)
	f.svc.Projects.tasks = tasks
	// Serialize the asynchronous executor at its existing lock; install a controlled
	// accepted receipt before releasing it. Execute then refuses duplicate submission.
	tasks.mu.Lock()
	if err := f.svc.Projects.GenerateSceneVideo(&f.p, &scene); err != nil {
		tasks.mu.Unlock()
		t.Fatal(err)
	}
	var task models.Task
	if err := f.db.Where("task_id = ?", sceneVideoTaskID(t, f.db, scene.ID)).First(&task).Error; err != nil {
		tasks.mu.Unlock()
		t.Fatal(err)
	}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/view" {
			if r.URL.Query().Get("filename") != "controlled.mp4" || r.URL.Query().Get("type") != "output" {
				t.Errorf("wrong output identity: %s", r.URL.RawQuery)
			}
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write([]byte("controlled-output-not-model-bytes"))
			return
		}
		if r.URL.Path != "/history/controlled" {
			t.Errorf("unexpected provider request %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"controlled":{"status":{"status_str":"success"},"outputs":{"1":{"videos":[{"filename":"controlled.mp4","subfolder":"","type":"output"}]}}}}`))
	}))
	defer provider.Close()
	inst := models.Instance{Host: "127.0.0.1", Port: comfyTestPort(provider), Status: "running"}
	if err := f.db.Create(&inst).Error; err != nil {
		tasks.mu.Unlock()
		t.Fatal(err)
	}
	now := time.Now()
	if err := f.db.Model(&task).Updates(map[string]any{"instance_id": inst.ID, "port": inst.Port, "comfy_prompt_id": "controlled", "submit_attempted_at": now, "status": "running"}).Error; err != nil {
		tasks.mu.Unlock()
		t.Fatal(err)
	}
	tasks.mu.Unlock()
	if err := f.db.First(&task, task.ID).Error; err != nil {
		t.Fatal(err)
	}
	tasks.finishTask(&task)
	if task.Status != "success" || !strings.Contains(task.ResultFiles, "controlled.mp4") {
		t.Fatalf("controlled completion=%+v", task)
	}
	f.svc.Mgr = tasks.manager
	router := gin.New()
	router.GET("/api/instances/:id/output/*path", f.svc.HandleInstanceOutputFile)
	out := httptest.NewRecorder()
	router.ServeHTTP(out, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/instances/%d/output/controlled.mp4", inst.ID), nil))
	if out.Code != http.StatusOK || out.Body.String() != "controlled-output-not-model-bytes" {
		t.Fatalf("output fetch=%d %s", out.Code, out.Body.String())
	}
	var snapshot models.GenerationSnapshot
	if err := f.db.Where("task_id = ?", task.TaskID).First(&snapshot).Error; err != nil {
		t.Fatal(err)
	}
	if snapshot.SceneID != scene.ID || snapshot.ProjectID != f.p.ID || snapshot.FullPrompt != task.Prompt || snapshot.ParamsJSON != task.ParamsJSON {
		t.Fatal("task snapshot identity mismatch")
	}
	var refs []generationSnapshotRef
	if err := json.Unmarshal([]byte(snapshot.RefsJSON), &refs); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ref := range refs {
		if ref.Role == "ref_videos" {
			found = ref.MaterialID == material.ID && string(ref.DerivationManifest) == manifest && ref.File == material.Path
		}
	}
	if !found {
		t.Fatalf("real task lost material derivation: %s", snapshot.RefsJSON)
	}
	before := snapshot.RefsJSON
	if err := f.db.Model(&material).Updates(map[string]any{"derivation_manifest": "", "name": "edited.webm"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.First(&snapshot, snapshot.ID).Error; err != nil || snapshot.RefsJSON != before {
		t.Fatalf("material edit mutated snapshot: %v", err)
	}
	if err := f.db.First(&scene, scene.ID).Error; err != nil {
		t.Fatal(err)
	}
	if scene.VideoFirstFrameImg != "first.png" || scene.VideoLastFrameImg != "last.png" {
		t.Fatal("generation changed frame bindings")
	}
	t.Log("CONTROLLED_SCENE_TASK_PROVENANCE=PASS unconfirmedTasks=0 realCreateTask=1 completion=controlled materialSnapshot=immutable frames=preserved; NOT a real model pass")
}
