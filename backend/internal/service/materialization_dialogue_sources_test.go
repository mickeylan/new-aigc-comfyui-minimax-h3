package service

import (
	"reflect"
	"testing"

	"comfyui-console/internal/models"
)

func TestPreviewMaterializationUsesReadOnlyEffectiveDialogueSource(t *testing.T) {
	db := newShotMaterializationDB(t)
	project := models.Project{Title: "effective dialogue", Generation: 1}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"甲", "乙"} {
		if err := db.Create(&models.Character{ProjectID: project.ID, Name: name}).Error; err != nil {
			t.Fatal(err)
		}
	}
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Generation: 1, Order: 1, Title: "会面", Content: `甲说：“你好。”乙说：“收到。”`}
	if err := db.Create(&scene).Error; err != nil {
		t.Fatal(err)
	}
	shots := []models.Shot{
		{SceneID: scene.ID, Order: 1, Duration: 3, Description: "甲开口", Dialogue: "你好。", SpeechPresentation: models.ShotSpeechPresentationVisibleSpeaker},
		{SceneID: scene.ID, Order: 2, Duration: 3, Description: "乙回应", Dialogue: "收到。", SpeechPresentation: models.ShotSpeechPresentationVisibleSpeaker},
	}
	for i := range shots {
		if err := db.Create(&shots[i]).Error; err != nil {
			t.Fatal(err)
		}
	}

	first := materializationDialogues(db, &scene)
	second := materializationDialogues(db, &scene)
	if len(first) != 2 || first[0].ID == 0 || first[1].ID == 0 || first[0].ID == first[1].ID {
		t.Fatalf("effective dialogues lack unique temporary IDs: %+v", first)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("temporary dialogue identities are not deterministic: first=%+v second=%+v", first, second)
	}
	preview, err := NewShotService(db).PreviewMaterialization(project.ID, scene.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Items) != 1 || preview.Items[0].Dialogue != "你好。收到。" {
		t.Fatalf("preview did not use effective source dialogue: %+v", preview)
	}
	var count int64
	if err := db.Model(&models.Dialogue{}).Where("scene_id = ?", scene.ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("preview persisted inferred dialogues: count=%d err=%v", count, err)
	}
	created, err := NewShotService(db).Materialize(project.ID, scene.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(created) != 1 || created[0].Content != preview.Items[0].Content {
		t.Fatalf("preview/apply mismatch: %+v", created)
	}
	var saved models.Scene
	if err := db.First(&saved, created[0].ID).Error; err != nil {
		t.Fatal(err)
	}
	got := (&ProjectService{db: db}).sceneVideoDialogues(&saved)
	if len(got) != 2 || got[0].Text != "你好。" || got[0].Character != "甲" || got[1].Text != "收到。" || got[1].Character != "乙" {
		t.Fatalf("materialized effective speech disappeared: %+v", got)
	}
}
