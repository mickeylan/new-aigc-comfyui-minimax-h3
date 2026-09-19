package service

import (
	"math"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"comfyui-console/internal/models"
)

func newAudioLayerTestService(t *testing.T) (*AudioLayerService, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Project{}, &models.Scene{}, &models.AudioLayer{}, &models.Dialogue{}); err != nil {
		t.Fatal(err)
	}
	return NewAudioLayerService(db), db
}

func TestAudioLayerCRUDIsScopedToProject(t *testing.T) {
	svc, db := newAudioLayerTestService(t)
	p1 := models.Project{Title: "one"}
	p2 := models.Project{Title: "two"}
	if err := db.Create(&p1).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&p2).Error; err != nil {
		t.Fatal(err)
	}
	scene1 := models.Scene{ProjectID: p1.ID, EpisodeN: 1, Generation: 1, Order: 1}
	scene2 := models.Scene{ProjectID: p2.ID, EpisodeN: 1, Generation: 1, Order: 1}
	if err := db.Create(&scene1).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&scene2).Error; err != nil {
		t.Fatal(err)
	}

	volume := 1.5
	created, err := svc.Create(p1.ID, AudioLayerInput{
		EpisodeN: 1, SceneID: &scene1.ID, Kind: " SFX ", Name: "hit", File: "hit.wav",
		StartTime: 0.5, EndTime: 1.25, Volume: &volume,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ProjectID != p1.ID || created.Kind != "sfx" || created.Volume != volume || created.Status != "draft" {
		t.Fatalf("unexpected created layer: %+v", created)
	}

	other, err := svc.Create(p2.ID, AudioLayerInput{
		EpisodeN: 1, SceneID: &scene2.ID, Kind: "bgm", StartTime: 0, EndTime: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	layers, err := svc.List(p1.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(layers) != 1 || layers[0].ID != created.ID {
		t.Fatalf("project-scoped list returned %+v", layers)
	}

	if _, err := svc.Update(p2.ID, created.ID, AudioLayerInput{EpisodeN: 1, Kind: "bgm", StartTime: 0, EndTime: 2}); err == nil {
		t.Fatal("cross-project update should fail")
	}
	if err := svc.Delete(p1.ID, other.ID); err == nil {
		t.Fatal("cross-project delete should fail")
	}
	if err := svc.Delete(p1.ID, created.ID); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&models.AudioLayer{}).Where("id = ?", created.ID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("owned layer was not deleted")
	}
	if err := db.Model(&models.AudioLayer{}).Where("id = ?", other.ID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("cross-project layer was deleted")
	}
}

func TestAudioLayerValidation(t *testing.T) {
	svc, db := newAudioLayerTestService(t)
	p1 := models.Project{Title: "one"}
	p2 := models.Project{Title: "two"}
	db.Create(&p1)
	db.Create(&p2)
	scene := models.Scene{ProjectID: p2.ID, EpisodeN: 1, Generation: 1, Order: 1}
	db.Create(&scene)

	valid := AudioLayerInput{EpisodeN: 1, Kind: "soundscape", StartTime: 0, EndTime: 2}
	for name, mutate := range map[string]func(*AudioLayerInput){
		"kind":          func(in *AudioLayerInput) { in.Kind = "voice" },
		"negative time": func(in *AudioLayerInput) { in.StartTime = -1 },
		"reversed time": func(in *AudioLayerInput) { in.EndTime = in.StartTime },
		"non-finite":    func(in *AudioLayerInput) { in.EndTime = math.Inf(1) },
		"volume":        func(in *AudioLayerInput) { v := 2.01; in.Volume = &v },
		"fade":          func(in *AudioLayerInput) { in.FadeIn = -0.1 },
		"episode":       func(in *AudioLayerInput) { in.EpisodeN = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			input := valid
			mutate(&input)
			if _, err := svc.Create(p1.ID, input); err == nil {
				t.Fatalf("expected validation error for %+v", input)
			}
		})
	}
	valid.SceneID = &scene.ID
	if _, err := svc.Create(p1.ID, valid); err == nil {
		t.Fatal("scene from another project should be rejected")
	}
}

func TestDialogueAudioMetadataPersists(t *testing.T) {
	_, db := newAudioLayerTestService(t)
	dialogue := models.Dialogue{H3VoiceDescription: "低沉男声", Position: 1.75}
	if err := db.Create(&dialogue).Error; err != nil {
		t.Fatal(err)
	}
	var got models.Dialogue
	if err := db.First(&got, dialogue.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.H3VoiceDescription != dialogue.H3VoiceDescription || got.Position != dialogue.Position {
		t.Fatalf("dialogue audio metadata did not persist: %+v", got)
	}
}
