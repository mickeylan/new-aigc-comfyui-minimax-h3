package service

import (
	"testing"

	"comfyui-console/internal/models"
)

func TestProjectSkillConfigurationsAreOperationScoped(t *testing.T) {
	db := newTestDBWithNewModels(t)
	if err := db.AutoMigrate(&models.Skill{}, &models.ProjectSkillConfig{}, &models.SkillAuditLog{}); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "p"}
	db.Create(&project)
	a := models.Skill{Name: "packet", Code: "packet-a", Version: 1, Stage: models.SkillStageVideoPrompt, Operation: "director-shot-packet", Enabled: true}
	b := models.Skill{Name: "state", Code: "state-b", Version: 1, Stage: models.SkillStageVideoPrompt, Operation: "reference-shot-state-prompt", Enabled: true}
	db.Create(&a)
	db.Create(&b)
	svc := NewSkillService(db)
	if _, err := svc.SetProjectOperationSkillConfig(project.ID, models.SkillStageVideoPrompt, a.Operation, &a.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetProjectOperationSkillConfig(project.ID, models.SkillStageVideoPrompt, b.Operation, &b.ID, true); err != nil {
		t.Fatal(err)
	}
	configs, err := svc.GetProjectConfig(project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(configs) != 2 {
		t.Fatalf("operation configs=%d", len(configs))
	}
	if _, err := svc.SetProjectOperationSkillConfig(project.ID, models.SkillStageVideoPrompt, a.Operation, &b.ID, true); err == nil {
		t.Fatal("operation mismatch accepted")
	}
}

func TestSavingShotPromotesNewestMatchingDraftWithoutLosingAction(t *testing.T) {
	db := newTestDBWithNewModels(t)
	project := models.Project{Title: "p"}
	db.Create(&project)
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1}
	db.Create(&scene)
	shot := models.Shot{SceneID: scene.ID, Order: 1, ShotType: "medium", Duration: 2, PromptSubject: "s", PromptAction: "a", PromptCamera: "c", PromptLighting: "l", PromptStyle: "style"}
	db.Create(&shot)
	content := canonicalShotPrompt(shot)
	first := models.PromptVersion{ProjectID: project.ID, EntityType: "shot", EntityID: shot.ID, Content: content, Action: string(PromptActionBuild), State: PromptVersionStateDraft}
	latest := models.PromptVersion{ProjectID: project.ID, EntityType: "shot", EntityID: shot.ID, Content: content, Action: string(PromptActionOptimize), State: PromptVersionStateDraft}
	db.Create(&first)
	db.Create(&latest)
	if _, err := NewShotService(db).ReplaceShots(scene.ID, []models.Shot{shot}); err != nil {
		t.Fatal(err)
	}
	db.First(&first, first.ID)
	db.First(&latest, latest.ID)
	if first.State != PromptVersionStateDraft || latest.State != PromptVersionStateApplied || latest.Action != string(PromptActionOptimize) {
		t.Fatalf("draft provenance corrupted: first=%+v latest=%+v", first, latest)
	}
}

func TestEditorialHashIncludesNegativePrompt(t *testing.T) {
	db := newTestDBWithNewModels(t)
	project := models.Project{Title: "p"}
	db.Create(&project)
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Generation: 1, Order: 1, NegativePrompt: "cars"}
	db.Create(&scene)
	before, err := episodeEditorialInputHash(db, project.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	db.Model(&scene).Update("negative_prompt", "cars, text")
	after, err := episodeEditorialInputHash(db, project.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatal("negative prompt did not affect editorial hash")
	}
}
