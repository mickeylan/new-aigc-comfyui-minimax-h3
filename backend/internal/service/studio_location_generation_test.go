package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
)

func studioLocationFixture(t *testing.T) (*Service, *models.Scene, FileMeta) {
	t.Helper()
	db := newStudioCandidateDB(t)
	if err := db.AutoMigrate(&models.Task{}, &models.Template{}, &models.UploadFile{}, &models.Shot{}); err != nil {
		t.Fatal(err)
	}
	p := models.Project{Title: "location"}
	db.Create(&p)
	scene := models.Scene{ProjectID: p.ID, ImageFile: "keep.png", ImageTaskID: "keep-task", ImagePrompt: "keep prompt", Status: "video_ready", VideoFile: "keep.mp4"}
	db.Create(&scene)
	cfg := &config.Config{}
	cfg.Storage.DataDir = t.TempDir()
	remote := NewRemoteExec(cfg.Remote)
	remote.SetLocalRoots([]string{cfg.Storage.DataDir, cfg.AssetsRoot()})
	upload := NewUploadManager(cfg, db, remote)
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	if _, _, err := upload.SaveFile("fixture", "image", "snapshot.png", b.Bytes()); err != nil {
		t.Fatal(err)
	}
	tpl := loadTemplateForTest(t, "qwen_image_2_1_multi_edit.json")
	if err := db.Create(&tpl).Error; err != nil {
		t.Fatal(err)
	}
	tasks := &TaskService{cfg: cfg, db: db, outputDownloader: func(*models.Task) ([]byte, error) { return b.Bytes(), nil }}
	ps := &ProjectService{db: db, upload: upload, tasks: tasks}
	return &Service{DB: db, Upload: upload, Tasks: tasks, Projects: ps}, &scene, FileMeta{TaskID: "fixture", Name: "snapshot.png"}
}

func TestStudioLocationRequestReviewTemplateAndReferenceOrder(t *testing.T) {
	svc, scene, ref := studioLocationFixture(t)
	in := studioLocationInput{TemplateSHA256: svc.studioLocationTemplates()[0].TemplateSHA256, TemplateCode: "qwen_image_2_1_multi_edit", Prompt: "人工确认的机位提示词", Reviewed: true, Snapshot: ref, References: []FileMeta{ref}}
	req, err := svc.studioLocationRequest(scene, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Files["ref_images"]) != 2 || req.Files["ref_images"][0] != ref || req.Prompt != in.Prompt {
		t.Fatalf("ordered references or human prompt lost: %+v", req)
	}
	source := req.Params[studioLocationKey].(studioLocationSource)
	if len(source.ReferenceSHA256) != 2 || source.TemplateSHA256 == "" || source.SceneID != scene.ID || source.PromptSHA256 == "" {
		t.Fatalf("missing provenance: %+v", source)
	}
	cases := []studioLocationInput{in, in, in, in, in, in}
	cases[0].Reviewed = false
	cases[1].Prompt = " "
	cases[2].TemplateCode = "unknown"
	cases[3].ShotID = 999
	cases[4].Snapshot = FileMeta{TaskID: "..", Name: "snapshot.png"}
	cases[5].TemplateSHA256 = "stale-template"
	for i, c := range cases {
		if _, err := svc.studioLocationRequest(scene, c); err == nil {
			t.Fatalf("invalid case %d accepted", i)
		}
	}
	var tpl models.Template
	svc.DB.First(&tpl)
	svc.DB.Model(&tpl).Update("inputs_json", `[{"key":"ref_images","max_count":1}]`)
	if _, err := svc.studioLocationRequest(scene, in); err == nil {
		t.Fatal("capacity silently truncated")
	}
	svc.DB.Model(&tpl).Update("enabled", false)
	in.References = nil
	if _, err := svc.studioLocationRequest(scene, in); err == nil {
		t.Fatal("disabled template accepted")
	}
	var after models.Scene
	svc.DB.First(&after, scene.ID)
	if after.ImageFile != scene.ImageFile || after.ImageTaskID != scene.ImageTaskID {
		t.Fatal("validation changed current scene")
	}
}

func TestStudioLocationCompletionPendingIdempotentAndRecovery(t *testing.T) {
	svc, scene, ref := studioLocationFixture(t)
	req, err := svc.studioLocationRequest(scene, studioLocationInput{TemplateSHA256: svc.studioLocationTemplates()[0].TemplateSHA256, TemplateCode: "qwen_image_2_1_multi_edit", Prompt: "keep camera", Reviewed: true, Snapshot: ref})
	if err != nil {
		t.Fatal(err)
	}
	params, _ := json.Marshal(req.Params)
	task := models.Task{TaskID: "fixture-location", Prompt: req.Prompt, ParamsJSON: string(params), InputsJSON: snapshotJSON(req.Files), Status: "success", ResultFiles: `[{"filename":"frame.png","subfolder":"","type":"images"}]`}
	if err := svc.DB.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	svc.Projects.syncStudioLocations()
	row, err := svc.Projects.captureStudioLocation(&task)
	if err != nil {
		t.Fatal(err)
	}
	if row.IsCurrent || row.ReviewStatus != "pending" || row.EntityID != scene.ID {
		t.Fatalf("must remain pending: %+v", row)
	}
	var n int64
	svc.DB.Model(&models.GenerationCandidate{}).Where("task_id = ?", task.TaskID).Count(&n)
	if n != 1 {
		t.Fatalf("duplicate completion: %d", n)
	}
	var after models.Scene
	svc.DB.First(&after, scene.ID)
	if after.ImageFile != scene.ImageFile || after.ImageTaskID != scene.ImageTaskID || after.Status != scene.Status || after.ImagePrompt != scene.ImagePrompt || after.VideoFile != scene.VideoFile {
		t.Fatalf("automatic production mutation: %+v", after)
	}
	// Existing user adoption is the only path that promotes the new image.
	if _, err := NewGenerationCandidateService(svc.DB).SelectCurrent(scene.ProjectID, row.ID); err != nil {
		t.Fatal(err)
	}
	svc.DB.First(&after, scene.ID)
	if after.ImageFile != row.File {
		t.Fatal("explicit adoption did not select image")
	}
}

func TestStudioLocationExecutionRejectsChangedReviewSnapshot(t *testing.T) {
	svc, scene, ref := studioLocationFixture(t)
	req, err := svc.studioLocationRequest(scene, studioLocationInput{TemplateCode: "qwen_image_2_1_multi_edit", TemplateSHA256: svc.studioLocationTemplates()[0].TemplateSHA256, Prompt: "reviewed", Reviewed: true, Snapshot: ref})
	if err != nil {
		t.Fatal(err)
	}
	params, _ := json.Marshal(req.Params)
	inputs, _ := json.Marshal(req.Files)
	task := models.Task{ParamsJSON: string(params), InputsJSON: string(inputs), Prompt: req.Prompt}
	var tpl models.Template
	svc.DB.First(&tpl)
	if err := svc.Tasks.validateStudioLocationSnapshot(&task, &tpl); err != nil {
		t.Fatalf("unchanged snapshot rejected: %v", err)
	}
	changed := tpl
	changed.WorkflowJSON += " "
	if err := svc.Tasks.validateStudioLocationSnapshot(&task, &changed); err == nil {
		t.Fatal("changed execution template accepted")
	}
	task.Prompt = "unreviewed"
	if err := svc.Tasks.validateStudioLocationSnapshot(&task, &tpl); err == nil {
		t.Fatal("changed execution prompt accepted")
	}
	task.Prompt = req.Prompt
	if err := os.WriteFile(filepath.Join(svc.Upload.InputDir(), ref.TaskID, ref.Name), []byte("different bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := svc.Tasks.validateStudioLocationSnapshot(&task, &tpl); err == nil {
		t.Fatal("changed reference bytes accepted")
	}
}

func TestStudioLocationHTTPRejectsForeignSceneAndTask(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc, scene, _ := studioLocationFixture(t)
	router := gin.New()
	router.POST("/api/projects/:id/scenes/:sid/location-generations", svc.HandleCreateStudioLocation)
	router.GET("/api/projects/:id/scenes/:sid/location-generations/:tid", svc.HandleGetStudioLocation)
	for _, url := range []string{fmt.Sprintf("/api/projects/%d/scenes/%d/location-generations", scene.ProjectID+1, scene.ID), fmt.Sprintf("/api/projects/%d/scenes/%d/location-generations/missing", scene.ProjectID, scene.ID)} {
		method := "POST"
		if strings.HasSuffix(url, "/missing") {
			method = "GET"
		}
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, url, strings.NewReader(`{}`))
		r.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, r)
		if w.Code < 400 {
			t.Fatalf("foreign context accepted: %d %s", w.Code, w.Body.String())
		}
	}
}
