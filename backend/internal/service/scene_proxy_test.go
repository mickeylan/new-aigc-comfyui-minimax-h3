package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
)

func proxyTestService(t *testing.T) (*Service, *config.Config, string) {
	t.Helper()
	root := t.TempDir()
	cfg := &config.Config{
		Comfy:   config.ComfyConfig{ComfyDir: filepath.Join(root, "ComfyUI")},
		Storage: config.StorageConfig{DataDir: filepath.Join(root, "data"), AssetDir: filepath.Join(root, "project-assets")},
	}
	db := safetyDB(t, &models.MergeTask{}, &models.Material{}, &models.ScreenTextCue{}, &models.AudioLayer{})
	if err := db.AutoMigrate(&models.Task{}, &models.UploadFile{}); err != nil {
		t.Fatal(err)
	}
	remote := NewRemoteExec(config.RemoteConfig{})
	remote.SetLocalRoots([]string{cfg.Comfy.ComfyDir, cfg.Storage.DataDir, cfg.AssetsRoot()})
	upload := NewUploadManager(cfg, db, remote)
	materials := NewMaterialService(cfg, db, remote, upload)
	projects := NewProjectService(cfg, db, nil, nil, nil, remote, upload, nil, materials)
	return &Service{Cfg: cfg, DB: db, Projects: projects, Upload: upload, Remote: remote, Materials: materials, Fonts: NewFontRegistry(cfg.Storage.DataDir)}, cfg, root
}

func TestProxyCacheKeyFollowsSourceIdentityAndRejectsTraversal(t *testing.T) {
	svc, _, root := proxyTestService(t)
	source := filepath.Join(root, "source.mp4")
	if err := os.WriteFile(source, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := svc.Projects.proxyPath(1, 2, source)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := svc.Projects.proxyPath(1, 2, source)
	if first != again || filepath.Dir(first) != svc.Projects.proxyRoot() {
		t.Fatalf("proxy key not stable: %q %q", first, again)
	}
	if err := os.WriteFile(source, []byte("v2-longer"), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, _ := svc.Projects.proxyPath(1, 2, source)
	if changed == first {
		t.Fatal("replaced Scene source reused stale proxy")
	}
	scene := models.Scene{ProjectID: 1, VideoInputFile: "../escape.mp4"}
	if _, err := svc.Projects.sceneVideoSource(&models.Project{ID: 1}, &scene); err == nil {
		t.Fatal("traversal source accepted")
	}
	if _, err := svc.Projects.proxyPath(1, 2, filepath.Join(root, "absent.mp4")); err == nil {
		t.Fatal("missing source produced a proxy key")
	}
}

func TestPreflightMergeUsesExportValidationWithoutPersistingTask(t *testing.T) {
	ffmpeg, err := localFFmpegPath()
	if err != nil {
		t.Skip(err)
	}
	svc, cfg, root := proxyTestService(t)
	project := models.Project{Title: "preflight", Generation: 1}
	if err := svc.DB.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	makeClip := func(name string) models.Scene {
		t.Helper()
		path := filepath.Join(root, name)
		if log, runErr := runLocalProgram(ffmpeg, []string{"-y", "-f", "lavfi", "-i", "color=c=red:s=160x96:r=24:d=4", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000:duration=4", "-shortest", "-c:v", "libx264", "-threads", "1", "-pix_fmt", "yuv420p", "-c:a", "aac", path}, 45*time.Second); runErr != nil {
			t.Fatalf("fixture: %v %s", runErr, log)
		}
		data, _ := os.ReadFile(path)
		rel, _, err := svc.Upload.SaveFile(fmt.Sprint(project.ID), "video", name, data)
		if err != nil {
			t.Fatal(err)
		}
		scene := readyMergeScene(project.ID, 1, 1, 1, "task-"+name, "unused.mp4")
		scene.VideoInputFile = filepath.Base(rel)
		scene.Duration = 4
		if err := svc.DB.Create(&scene).Error; err != nil {
			t.Fatal(err)
		}
		return scene
	}
	first, second := makeClip("a.mp4"), makeClip("b.mp4")

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/projects/:id/audio-merge/preflight", svc.HandlePreflightAudioMerge)
	payload, _ := json.Marshal(map[string]any{
		"episode_n":     1,
		"edit_sequence": []mergeEditClip{{SceneID: &first.ID, Transition: "fade", TransitionDuration: .5}, {SceneID: &second.ID}},
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/projects/"+strconv.FormatUint(uint64(project.ID), 10)+"/audio-merge/preflight", bytes.NewReader(payload)))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var result struct {
		Duration float64  `json:"duration"`
		Clips    int      `json:"clips"`
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Clips != 2 || math.Abs(result.Duration-7.5) > .01 {
		t.Fatalf("preflight summary=%+v", result)
	}
	var count int64
	svc.DB.Model(&models.MergeTask{}).Count(&count)
	if count != 0 {
		t.Fatalf("preflight persisted %d merge tasks", count)
	}
	if _, err := os.Stat(filepath.Join(cfg.Storage.DataDir, "output_workers")); err == nil {
		// Rendering must not have been launched; an output worker directory would
		// only appear if a merge task ran.
		t.Fatal("preflight started rendering")
	}

	bad, _ := json.Marshal(map[string]any{"episode_n": 1, "edit_sequence": []mergeEditClip{{SceneID: &first.ID}}})
	rejected := httptest.NewRecorder()
	router.ServeHTTP(rejected, httptest.NewRequest(http.MethodPost, "/api/projects/"+strconv.FormatUint(uint64(project.ID), 10)+"/audio-merge/preflight", bytes.NewReader(bad)))
	if rejected.Code != http.StatusBadRequest && rejected.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid preflight status=%d body=%s", rejected.Code, rejected.Body.String())
	}
	svc.DB.Model(&models.MergeTask{}).Count(&count)
	if count != 0 {
		t.Fatal("invalid preflight persisted a task")
	}
}

func TestSceneProxyAndMediaInfoEndpoints(t *testing.T) {
	ffmpeg, err := localFFmpegPath()
	if err != nil {
		t.Skip(err)
	}
	svc, cfg, root := proxyTestService(t)
	project := models.Project{Title: "proxy"}
	if err := svc.DB.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "scene.mp4")
	if log, runErr := runLocalProgram(ffmpeg, []string{"-y", "-f", "lavfi", "-i", "color=c=blue:s=160x96:r=24:d=2", "-c:v", "libx264", "-threads", "1", "-pix_fmt", "yuv420p", source}, 45*time.Second); runErr != nil {
		t.Fatalf("fixture: %v %s", runErr, log)
	}
	data, _ := os.ReadFile(source)
	rel, _, err := svc.Upload.SaveFile(fmt.Sprint(project.ID), "video", "scene.mp4", data)
	if err != nil {
		t.Fatal(err)
	}
	scene := readyMergeScene(project.ID, 1, 1, 1, "task-proxy", "unused.mp4")
	scene.VideoInputFile = filepath.Base(rel)
	scene.Status = "video_ready"
	if err := svc.DB.Create(&scene).Error; err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(cfg.AssetsRoot(), filepath.FromSlash(rel))
	before := mergeFixtureSHA256(t, sourcePath)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	base := "/api/projects/" + strconv.FormatUint(uint64(project.ID), 10) + "/scenes/" + strconv.FormatUint(uint64(scene.ID), 10)
	router.GET("/api/projects/:id/scenes/:sid/media-info", svc.HandleSceneMediaInfo)
	router.POST("/api/projects/:id/scenes/:sid/proxy", svc.HandleCreateSceneProxy)
	router.GET("/api/projects/:id/scenes/:sid/proxy", svc.HandleGetSceneProxy)

	infoResp := httptest.NewRecorder()
	router.ServeHTTP(infoResp, httptest.NewRequest(http.MethodGet, base+"/media-info", nil))
	if infoResp.Code != http.StatusOK {
		t.Fatalf("media-info status=%d body=%s", infoResp.Code, infoResp.Body.String())
	}
	var info map[string]any
	json.Unmarshal(infoResp.Body.Bytes(), &info)
	if info["has_video"] != true || info["proxy_ready"] != false || info["duration"].(float64) < 1.9 {
		t.Fatalf("media-info=%+v", info)
	}

	created := httptest.NewRecorder()
	router.ServeHTTP(created, httptest.NewRequest(http.MethodPost, base+"/proxy", nil))
	if created.Code != http.StatusOK {
		t.Fatalf("proxy status=%d body=%s", created.Code, created.Body.String())
	}
	served := httptest.NewRecorder()
	router.ServeHTTP(served, httptest.NewRequest(http.MethodGet, base+"/proxy", nil))
	if served.Code != http.StatusOK || served.Body.Len() == 0 {
		t.Fatalf("served proxy status=%d bytes=%d", served.Code, served.Body.Len())
	}
	if mergeFixtureSHA256(t, sourcePath) != before {
		t.Fatal("proxy generation modified the Scene source")
	}
	var afterScene models.Scene
	svc.DB.First(&afterScene, scene.ID)
	if afterScene.VideoInputFile != scene.VideoInputFile || afterScene.Status != scene.Status {
		t.Fatal("proxy generation mutated Scene state")
	}
	proxies, _ := os.ReadDir(svc.Projects.proxyRoot())
	if len(proxies) != 1 {
		t.Fatalf("expected one cached proxy, got %d", len(proxies))
	}
}
