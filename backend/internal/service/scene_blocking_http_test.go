package service

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
)

func TestSceneBlockingPromptCarriesInteractionFacing(t *testing.T) {
	sc := &models.Scene{BlockingJSON: blockingJSON(SceneBlocking{Characters: []SceneBlockingCharacter{{Name: "燕承", Facing: "面向青年，相对镜头侧面", Gaze: "青年"}}, Camera: "双方侧面"})}
	context := sceneBlockingPromptContext(sc)
	for _, expected := range []string{"面向青年，相对镜头侧面", "面向乙而非镜头", "机位位于互动双方的哪一侧", "AI建议"} {
		if !strings.Contains(context, expected) {
			t.Fatalf("downstream image/video context missing %q", expected)
		}
	}
	if sceneBlockingPromptContext(&models.Scene{}) != "" {
		t.Fatal("unset blocking should remain optional")
	}
}

func TestSceneBlockingSaveOwnershipAndPreservesMedia(t *testing.T) {
	db := newTestDB(t)
	if err := db.AutoMigrate(&models.Scene{}); err != nil {
		t.Fatal(err)
	}
	scene := models.Scene{ProjectID: 11, EpisodeN: 1, Generation: 2, Order: 1, ImageFile: "keep.png", VideoFile: "keep.mp4", VideoFullPrompt: "approved", Status: "video_ready"}
	if err := db.Create(&scene).Error; err != nil {
		t.Fatal(err)
	}
	s := &Service{DB: db}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.PUT("/api/projects/:id/scenes/:sid/blocking", s.HandleSaveSceneBlocking)
	body := `{"facts":["甲在门边"],"suggestions":["建议侧机位"],"characters":[{"name":"甲","position":"门边","facing":"门外","gaze":"乙","visibility":"无遮挡"}],"camera":"侧面","start":"站立","end":"迈出","summary":"甲在门边"}`
	request := func(project uint) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		q := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/projects/%d/scenes/%d/blocking", project, scene.ID), bytes.NewBufferString(body))
		q.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, q)
		return w
	}
	if w := request(99); w.Code != http.StatusNotFound {
		t.Fatalf("cross-project status=%d", w.Code)
	}
	if w := request(scene.ProjectID); w.Code != http.StatusOK {
		t.Fatalf("save status=%d body=%s", w.Code, w.Body.String())
	}
	var got models.Scene
	db.First(&got, scene.ID)
	if got.ImageFile != "keep.png" || got.VideoFile != "keep.mp4" || got.VideoFullPrompt != "approved" || got.Status != "video_ready" {
		t.Fatalf("media changed: %+v", got)
	}
	if !got.PromptStale || got.BlockingJSON == "" {
		t.Fatalf("blocking not saved/staled: %+v", got)
	}
}

func TestSceneBlockingPromptApplyIsScopedAndMarkersInvalidate(t *testing.T) {
	db := newTestDB(t)
	if err := db.AutoMigrate(&models.Scene{}); err != nil {
		t.Fatal(err)
	}
	blocking := blockingJSON(SceneBlocking{Summary: "相向而立"})
	scene := models.Scene{ProjectID: 21, EpisodeN: 1, Generation: 1, Order: 1, BlockingJSON: blocking, ImagePrompt: "old image", VideoPrompt: "old video", VideoFullPrompt: "confirmed exec", ImageFile: "keep.png", VideoFile: "keep.mp4", Status: "video_ready", ImageTaskID: "image-task", VideoTaskID: "video-task"}
	if err := db.Create(&scene).Error; err != nil {
		t.Fatal(err)
	}
	s := &Service{DB: db}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/projects/:id/scenes/:sid/blocking/prompts/:kind/apply", s.HandleApplySceneBlockingPrompt)
	apply := func(project uint, kind, prompt, hash string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		body := fmt.Sprintf(`{"prompt":%q,"blocking_hash":%q}`, prompt, hash)
		q := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/projects/%d/scenes/%d/blocking/prompts/%s/apply", project, scene.ID, kind), bytes.NewBufferString(body))
		q.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, q)
		return w
	}
	if w := apply(99, "image", "new image", blockingDigest(blocking)); w.Code != http.StatusNotFound {
		t.Fatalf("ownership status=%d", w.Code)
	}
	if w := apply(scene.ProjectID, "image", "new image", blockingDigest(blocking)); w.Code != http.StatusOK {
		t.Fatalf("image apply=%d %s", w.Code, w.Body.String())
	}
	var got models.Scene
	db.First(&got, scene.ID)
	if got.ImagePrompt != "new image" || got.VideoPrompt != "old video" || got.VideoFullPrompt != "confirmed exec" {
		t.Fatalf("wrong prompt fields changed: %+v", got)
	}
	if got.ImageFile != "keep.png" || got.VideoFile != "keep.mp4" || got.Status != "video_ready" || got.ImageTaskID != "image-task" || got.VideoTaskID != "video-task" {
		t.Fatalf("media/task state changed: %+v", got)
	}
	if got.BlockingImageAppliedHash != blockingAppliedHash(blocking, "new image") {
		t.Fatal("image marker missing")
	}
	if w := apply(scene.ProjectID, "video", "detailed_description:\n[Shot 1] <Subject 1> moves.", "stale"); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"applied":false`) || !strings.Contains(w.Body.String(), `"warning"`) {
		t.Fatalf("stale blocking save=%d %s", w.Code, w.Body.String())
	}
	db.First(&got, scene.ID)
	if got.VideoFullPrompt == "" || got.VideoPrompt == got.VideoFullPrompt || got.BlockingVideoAppliedHash != "" || got.PromptStale {
		t.Fatalf("video full/action save semantics are wrong: %+v", got)
	}
	if err := db.Model(&got).Update("image_prompt", "manual edit").Error; err != nil {
		t.Fatal(err)
	}
	db.First(&got, scene.ID)
	if got.BlockingImageApplied {
		t.Fatal("manual prompt change must invalidate applied state")
	}
	if err := db.Model(&got).Update("blocking_json", blockingJSON(SceneBlocking{Summary: "changed"})).Error; err != nil {
		t.Fatal(err)
	}
	db.First(&got, scene.ID)
	if got.BlockingImageApplied {
		t.Fatal("blocking change must invalidate applied state")
	}
}

func TestSceneBlockingAnalyzePreviewDoesNotWrite(t *testing.T) {
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		for _, expected := range []string{"三分之二侧面", "施动者", "面向乙而非镜头", "不能用目光方向代替身体朝向"} {
			if !strings.Contains(string(body), expected) {
				t.Errorf("AI request missing facing guidance %q", expected)
			}
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"facts\":[\"甲站左侧\"],\"suggestions\":[\"建议侧拍\"],\"characters\":[],\"camera\":\"\",\"start\":\"\",\"end\":\"\",\"summary\":\"站位预览\"}"}}]}`)
	}))
	defer ai.Close()
	s, db, scene := expandHTTPService(t, ai.URL)
	if err := s.Skills.InitSystemSkills(); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/projects/:id/scenes/:sid/blocking/analyze", s.HandleAnalyzeSceneBlocking)
	w := httptest.NewRecorder()
	q := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/projects/%d/scenes/%d/blocking/analyze", scene.ProjectID, scene.ID), nil)
	r.ServeHTTP(w, q)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var got models.Scene
	db.First(&got, scene.ID)
	if got.BlockingJSON != "" {
		t.Fatalf("preview persisted: %s", got.BlockingJSON)
	}
}
