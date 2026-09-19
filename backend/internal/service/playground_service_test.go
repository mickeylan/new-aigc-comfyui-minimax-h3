package service

import (
	"encoding/json"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
)

func newPlaygroundTestService(t *testing.T) (*PlaygroundService, *gorm.DB, models.Template) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Template{}, &models.Task{}, &models.PlaygroundRun{}, &models.Material{}); err != nil {
		t.Fatal(err)
	}
	template := models.Template{
		Name: "Text image", Code: "text_image", Enabled: true,
		InputsJSON:   `[{"key":"prompt","type":"prompt","required":true},{"key":"width","type":"int","default":1024}]`,
		WorkflowJSON: `{}`,
	}
	if err := db.Create(&template).Error; err != nil {
		t.Fatal(err)
	}
	tasks := NewTaskService(config.Default(), db, nil, nil, NewHub(), nil)
	return NewPlaygroundService(db, tasks, nil), db, template
}

func TestPlaygroundCreateValidatesCatalogAndBatch(t *testing.T) {
	svc, _, _ := newPlaygroundTestService(t)
	if _, err := svc.Create(CreatePlaygroundRunInput{Mode: "text-to-video", Template: "text_image", Prompt: "test"}); err == nil {
		t.Fatal("expected mode mismatch")
	}
	if _, err := svc.Create(CreatePlaygroundRunInput{Mode: "text-to-image", Template: "missing", Prompt: "test"}); err == nil {
		t.Fatal("expected missing template error")
	}
	if _, err := svc.Create(CreatePlaygroundRunInput{Mode: "text-to-image", Template: "text_image", Prompt: "test", BatchCount: 5}); err == nil {
		t.Fatal("expected batch cap error")
	}

	runs, err := svc.Create(CreatePlaygroundRunInput{
		Mode: "text-to-image", Template: "text_image", Prompt: "test", Params: map[string]any{"width": 512}, BatchCount: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 || runs[0].TaskID == "" || runs[0].TaskID == runs[1].TaskID {
		t.Fatalf("unexpected runs: %#v", runs)
	}
	var params map[string]any
	if err := json.Unmarshal(runs[0].Params, &params); err != nil || params["width"] != float64(512) {
		t.Fatalf("params not preserved: %s (%v)", runs[0].Params, err)
	}
}

func TestPlaygroundReadSyncsTaskStatusAndResult(t *testing.T) {
	svc, db, _ := newPlaygroundTestService(t)
	runs, err := svc.Create(CreatePlaygroundRunInput{Mode: "text-to-image", Template: "text_image", Prompt: "test"})
	if err != nil {
		t.Fatal(err)
	}
	result := `[{"type":"images","filename":"result.png","subfolder":"samples"}]`
	if err := db.Model(&models.Task{}).Where("task_id = ?", runs[0].TaskID).Updates(map[string]any{"status": "success", "result_files": result}).Error; err != nil {
		t.Fatal(err)
	}

	run, err := svc.Get(runs[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "success" || string(run.Result) != result {
		t.Fatalf("task state was not synced: %#v", run)
	}
}

func TestPlaygroundPromotesSuccessfulResultToMaterial(t *testing.T) {
	svc, db, _ := newPlaygroundTestService(t)
	runs, err := svc.Create(CreatePlaygroundRunInput{Mode: "text-to-image", Template: "text_image", Prompt: "promoted prompt"})
	if err != nil {
		t.Fatal(err)
	}
	result := `[{"type":"images","filename":"result.png","subfolder":"samples"}]`
	if err := db.Model(&models.Task{}).Where("task_id = ?", runs[0].TaskID).Updates(map[string]any{"status": "success", "result_files": result}).Error; err != nil {
		t.Fatal(err)
	}
	material, err := svc.Promote(runs[0].ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if material.Source != "playground" || material.Type != "image" || material.Prompt != "promoted prompt" || material.Name != "result.png" {
		t.Fatalf("unexpected material: %#v", material)
	}
	var count int64
	if err := db.Model(&models.Material{}).Where("id = ?", material.ID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("material not persisted: count=%d err=%v", count, err)
	}
}
