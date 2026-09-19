package service

import (
	"encoding/json"
	"testing"

	"comfyui-console/internal/models"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func safetyDB(t *testing.T, extra ...any) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	base := []any{&models.Project{}, &models.Episode{}, &models.Scene{}, &models.Shot{}, &models.Dialogue{}, &models.ScriptRevision{}, &models.GenerationCandidate{}, &models.FrameCandidate{}, &models.SceneContinuity{}, &models.SceneCharacterLook{}, &models.SceneCharacterOutfit{}, &models.ShotCharacterLook{}, &models.ShotCharacterOutfit{}, &models.AudioLayer{}, &models.PromptPolicyOverride{}, &models.SharedAssetReference{}}
	base = append(base, extra...)
	if err := db.AutoMigrate(base...); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestScriptRevisionRestoreDoesNotRewindOtherEpisodesOrGeneration(t *testing.T) {
	db := safetyDB(t)
	project := models.Project{Title: "p", Generation: 3, PipelineStage: "videos", Scripts: `{"1":"old one","2":"keep two"}`}
	db.Create(&project)
	db.Create(&models.Episode{ProjectID: project.ID, Number: 1, Title: "one"})
	db.Create(&models.Scene{ProjectID: project.ID, EpisodeN: 1, Generation: 3, Order: 1, Title: "old"})
	svc := NewScriptRevisionService(db)
	revision, err := svc.Create(project.ID, 1, "manual")
	if err != nil {
		t.Fatal(err)
	}
	db.Model(&project).Updates(map[string]any{"generation": 9, "pipeline_stage": "finished", "scripts": `{"1":"new one","2":"newer two"}`})
	db.Where("project_id = ? AND episode_n = ?", project.ID, 1).Delete(&models.Scene{})
	if _, err := svc.Restore(project.ID, revision.ID); err != nil {
		t.Fatal(err)
	}
	db.First(&project, project.ID)
	if project.Generation != 9 || project.PipelineStage != "finished" {
		t.Fatalf("global project state rewound: %+v", project)
	}
	var scripts map[string]string
	_ = json.Unmarshal([]byte(project.Scripts), &scripts)
	if scripts["1"] != "old one" || scripts["2"] != "newer two" {
		t.Fatalf("scripts merged incorrectly: %+v", scripts)
	}
}

func TestApplyDialoguePreviewRejectsStaleAudio(t *testing.T) {
	db := safetyDB(t)
	project := models.Project{Title: "p"}
	db.Create(&project)
	scene := models.Scene{ProjectID: project.ID, Order: 1}
	db.Create(&scene)
	d := models.Dialogue{ProjectID: project.ID, SceneID: scene.ID, Text: "new", Voice: "v", AudioFile: "old.mp3", AudioHash: "old-hash", AudioStale: true}
	db.Create(&d)
	ps := &ProjectService{db: db}
	if _, err := ps.ApplyDialoguePreview(&project, d.ID); err == nil {
		t.Fatal("stale audio was approved")
	}
}

func TestPlaygroundPromoteRejectsTraversal(t *testing.T) {
	db := safetyDB(t, &models.PlaygroundRun{}, &models.Task{}, &models.Material{})
	task := models.Task{TaskID: "t", Status: "success", ResultFiles: `[{"filename":"secret.txt","subfolder":"../../../outside","type":"images"}]`}
	db.Create(&task)
	run := models.PlaygroundRun{TaskID: task.TaskID, Status: "success", Result: json.RawMessage(task.ResultFiles)}
	db.Create(&run)
	if _, err := NewPlaygroundService(db, nil, nil).Promote(run.ID, 0); err == nil {
		t.Fatal("traversal result was promoted")
	}
}
