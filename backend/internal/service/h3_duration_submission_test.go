package service

import (
	"math"
	"testing"

	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
)

func TestH3DurationSubmissionDoesNotUseLocalConfiguredCap(t *testing.T) {
	f := newTranslationHTTPFixture(t)
	pool, err := f.db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(1)
	configureDefaultRef2VFixture(t, f)
	if err := f.db.AutoMigrate(&models.Task{}, &models.Setting{}, &models.Instance{}); err != nil {
		t.Fatal(err)
	}
	tpl := loadTemplateForTest(t, "minimax_h3_ref2v.json")
	if err := f.db.Model(&models.Template{}).Where("code = ?", tpl.Code).Updates(map[string]any{"inputs_json": tpl.InputsJSON, "workflow_json": tpl.WorkflowJSON}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Create(&models.Setting{Key: settingH3MaxDurationSeconds, Value: "15"}).Error; err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"aqing.png", "teahouse.png", "start.png"} {
		if err := f.db.Create(&models.UploadFile{TaskID: "1", Type: "image", Name: name, Path: "1/" + name}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := f.db.Model(&models.Scene{}).Where("id = ?", f.sc.ID).Updates(map[string]any{"duration": 16, "video_full_prompt": bilingualRef2VEnglish, "video_file": "keep.mp4", "status": "video_ready"}).Error; err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	tasks := NewTaskService(cfg, f.db, NewInstanceManager(cfg, f.db, nil), nil, NewHub(), nil)
	f.svc.Projects.tasks = tasks
	var sc models.Scene
	if err := f.db.First(&sc, f.sc.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Projects.GenerateSceneVideo(&f.p, &sc); err != nil {
		t.Fatalf("16-second scene was blocked: %v", err)
	}
	var task models.Task
	if err := f.db.Where("task_id = ?", sceneVideoTaskID(t, f.db, sc.ID)).First(&task).Error; err != nil {
		t.Fatal(err)
	}
	if got := decodeSceneVideoTaskParams(t, task)["duration"]; got != float64(16) {
		t.Fatalf("duration changed: %v", got)
	}
	var saved models.Scene
	if err := f.db.First(&saved, sc.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.VideoFile != "keep.mp4" || saved.VideoFullPrompt != bilingualRef2VEnglish {
		t.Fatal("submission changed existing media or reviewed prompt")
	}
	for _, duration := range []float64{16, 24, 60, 120} {
		if err := tasks.validateH3TaskDuration(tpl.Code, map[string]any{"duration": duration}); err != nil {
			t.Fatalf("positive duration %v blocked: %v", duration, err)
		}
	}
	for _, duration := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		if err := tasks.validateH3TaskDuration(tpl.Code, map[string]any{"duration": duration}); err == nil {
			t.Fatalf("invalid duration %v accepted", duration)
		}
	}
}
