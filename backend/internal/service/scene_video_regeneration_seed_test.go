package service

import (
	"encoding/json"
	"reflect"
	"testing"

	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

func TestGenerateSceneVideoRegenerationPersistsFreshSeedWithoutChangingReviewedInputsOrMedia(t *testing.T) {
	f := newTranslationHTTPFixture(t)
	// This fixture uses SQLite :memory:, which is private to each connection.
	// GenerateSceneVideo starts an executor goroutine; keep it on the same
	// connection as setup rather than opening an unrelated empty database.
	pool, err := f.db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(1)
	configureDefaultRef2VFixture(t, f)
	if err := f.db.AutoMigrate(&models.Task{}, &models.Setting{}, &models.Instance{}); err != nil {
		t.Fatal(err)
	}
	realTemplate := loadTemplateForTest(t, "minimax_h3_ref2v.json")
	if err := f.db.Model(&models.Template{}).Where("code = ?", realTemplate.Code).Updates(map[string]any{
		"inputs_json": realTemplate.InputsJSON, "workflow_json": realTemplate.WorkflowJSON,
	}).Error; err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"aqing.png", "teahouse.png", "start.png"} {
		if err := f.db.Create(&models.UploadFile{TaskID: "1", Type: "image", Name: name, Path: "1/" + name}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := f.db.Model(&models.Scene{}).Where("id = ?", f.sc.ID).Updates(map[string]any{
		"video_full_prompt": bilingualRef2VEnglish,
		"video_file":        "confirmed-output.mp4",
		"status":            "video_ready",
	}).Error; err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{}
	tasks := NewTaskService(cfg, f.db, NewInstanceManager(cfg, f.db, nil), nil, NewHub(), nil)
	f.svc.Projects.tasks = tasks

	generate := func() models.Task {
		t.Helper()
		var scene models.Scene
		if err := f.db.First(&scene, f.sc.ID).Error; err != nil {
			t.Fatal(err)
		}
		if err := f.svc.Projects.GenerateSceneVideo(&f.p, &scene); err != nil {
			t.Fatal(err)
		}
		var task models.Task
		if err := f.db.Where("task_id = ?", sceneVideoTaskID(t, f.db, f.sc.ID)).First(&task).Error; err != nil {
			t.Fatal(err)
		}
		return task
	}

	first := generate()
	// Model a completed task while deliberately retaining the adopted media.
	if err := f.db.Model(&models.Scene{}).Where("id = ?", f.sc.ID).Updates(map[string]any{
		"status": "video_ready", "video_file": "confirmed-output.mp4",
	}).Error; err != nil {
		t.Fatal(err)
	}
	second := generate()

	firstParams := decodeSceneVideoTaskParams(t, first)
	secondParams := decodeSceneVideoTaskParams(t, second)
	firstSeed := firstParams["seed"].(float64)
	secondSeed := secondParams["seed"].(float64)
	if firstSeed < 0 || secondSeed < 0 || firstSeed > float64(maxComfyGenerationSeed) || secondSeed > float64(maxComfyGenerationSeed) || firstSeed == secondSeed {
		t.Fatalf("regeneration seeds must be distinct non-negative bounded values: first=%v second=%v", firstSeed, secondSeed)
	}
	if first.Prompt != bilingualRef2VEnglish || second.Prompt != bilingualRef2VEnglish || first.Prompt != second.Prompt {
		t.Fatalf("confirmed English prompt bytes changed: first=%q second=%q", first.Prompt, second.Prompt)
	}
	if first.InputsJSON == "" || first.InputsJSON != second.InputsJSON || !reflect.DeepEqual(firstParams["ref_images"], secondParams["ref_images"]) {
		t.Fatalf("reference inputs changed: first=%v second=%v", firstParams["ref_images"], secondParams["ref_images"])
	}
	for _, key := range []string{"width", "height", "duration", "steps", "cfg", "fps", "ref_image_size"} {
		if !reflect.DeepEqual(firstParams[key], secondParams[key]) {
			t.Fatalf("parameter %q changed: first=%v second=%v", key, firstParams[key], secondParams[key])
		}
	}
	var saved models.Scene
	if err := f.db.First(&saved, f.sc.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.VideoFile != "confirmed-output.mp4" || saved.VideoFullPrompt != bilingualRef2VEnglish {
		t.Fatalf("regeneration changed adopted media or reviewed prompt: %+v", saved)
	}
}

func sceneVideoTaskID(t *testing.T, db *gorm.DB, sceneID uint) string {
	t.Helper()
	var scene models.Scene
	if err := db.First(&scene, sceneID).Error; err != nil {
		t.Fatal(err)
	}
	return scene.VideoTaskID
}

func decodeSceneVideoTaskParams(t *testing.T, task models.Task) map[string]any {
	t.Helper()
	var params map[string]any
	if err := json.Unmarshal([]byte(task.ParamsJSON), &params); err != nil {
		t.Fatal(err)
	}
	return params
}
