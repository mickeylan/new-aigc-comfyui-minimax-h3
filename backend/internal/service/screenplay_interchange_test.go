package service

import (
	"encoding/json"
	"testing"

	"comfyui-console/internal/models"
	"comfyui-console/internal/screenplay"
	"gorm.io/gorm"
)

func TestScreenplayApplySnapshotsBeforeReplacement(t *testing.T) {
	db := newTestDBWithNewModels(t)
	if err := db.AutoMigrate(&models.Dialogue{}, &models.ScriptRevision{}); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "Project", Scripts: `{"1":"old script"}`, Script: "old script", Generation: 3}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Episode{ProjectID: project.ID, Number: 1, Title: "Episode", Status: "approved"}).Error; err != nil {
		t.Fatal(err)
	}
	oldScene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1, Generation: 3, Title: "OLD", Content: "old action"}
	if err := db.Create(&oldScene).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Shot{SceneID: oldScene.ID, Order: 1, ActType: models.ShotActSetup, Description: "old shot"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Dialogue{ProjectID: project.ID, SceneID: oldScene.ID, Order: 1, Character: "OLD", Text: "old dialogue"}).Error; err != nil {
		t.Fatal(err)
	}

	preview := screenplay.Preview{Format: screenplay.FormatFountain, Scenes: []screenplay.Scene{{Heading: "INT. NEW ROOM - DAY", Elements: []screenplay.Element{
		{Type: "action", Text: "new action"}, {Type: "character", Text: "ALICE"}, {Type: "parenthetical", Text: "softly"}, {Type: "dialogue", Text: "new dialogue"},
	}}}}
	var revision *models.ScriptRevision
	err := db.Transaction(func(tx *gorm.DB) error {
		var err error
		revision, err = createScriptRevisionTx(tx, project.ID, 1, "before_screenplay_import", nil)
		if err != nil {
			return err
		}
		return previewToDatabase(tx, &project, 1, preview)
	})
	if err != nil {
		t.Fatal(err)
	}
	if revision == nil {
		t.Fatal("safety revision was not created")
	}
	var saved models.ScriptRevision
	if err := db.First(&saved, revision.ID).Error; err != nil {
		t.Fatal(err)
	}
	var snapshot ScriptRevisionSnapshot
	if err := json.Unmarshal([]byte(saved.SnapshotJSON), &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Scenes) != 1 || snapshot.Scenes[0].Scene.Title != "OLD" || len(snapshot.Scenes[0].Shots) != 1 || len(snapshot.Scenes[0].Dialogues) != 1 {
		t.Fatalf("snapshot did not preserve old hierarchy: %+v", snapshot.Scenes)
	}
	var scenes []models.Scene
	if err := db.Where("project_id = ? AND episode_n = ?", project.ID, 1).Find(&scenes).Error; err != nil {
		t.Fatal(err)
	}
	if len(scenes) != 1 || scenes[0].Title != "INT. NEW ROOM - DAY" || scenes[0].ID == oldScene.ID {
		t.Fatalf("replacement failed: %+v", scenes)
	}
	var dialogues []models.Dialogue
	db.Where("scene_id = ?", scenes[0].ID).Find(&dialogues)
	if len(dialogues) != 1 || dialogues[0].Delivery != "softly" || dialogues[0].Text != "new dialogue" {
		t.Fatalf("dialogue mapping failed: %+v", dialogues)
	}
}
