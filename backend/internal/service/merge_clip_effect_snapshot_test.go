package service

import (
	"comfyui-console/internal/models"
	"encoding/json"
	"testing"
)

func TestEditedMergeSnapshotRejectsOverlayMaterialMutation(t *testing.T) {
	for _, field := range []string{"path", "size", "type"} {
		t.Run(field, func(t *testing.T) {
			db := safetyDB(t, &models.MergeTask{}, &models.Material{})
			project := models.Project{Title: "overlay snapshot", Generation: 1}
			if err := db.Create(&project).Error; err != nil {
				t.Fatal(err)
			}
			a := readyMergeScene(project.ID, 1, 1, 1, "a", "a.mp4")
			b := readyMergeScene(project.ID, 1, 2, 1, "b", "b.mp4")
			if err := db.Create(&a).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&b).Error; err != nil {
				t.Fatal(err)
			}
			overlay := models.Material{Name: "overlay", Type: "image", ProjectID: &project.ID, Path: "material_p1/overlay.png", Size: 10}
			if err := db.Create(&overlay).Error; err != nil {
				t.Fatal(err)
			}
			clips := []mergeEditClip{{ID: "a", SceneID: &a.ID, OverlayMaterialID: &overlay.ID}, {ID: "b", SceneID: &b.ID}}
			svc := &ProjectService{db: db}
			merge, _, err := svc.createMergeTaskRecord(&project, []uint{a.ID, b.ID}, false, false, MergeAudioOptions{NativeVolume: 1, DialogueVolume: 1, BGMVolume: 1}, mergeTaskSequence{Clips: clips, EpisodeN: 1})
			if err != nil {
				t.Fatal(err)
			}
			var snapshot mergeInputSnapshot
			if err := json.Unmarshal([]byte(merge.InputSnapshot), &snapshot); err != nil {
				t.Fatal(err)
			}
			if len(snapshot.Materials) != 1 || snapshot.Materials[0].ID != overlay.ID {
				t.Fatalf("overlay absent from snapshot: %+v", snapshot.Materials)
			}
			if _, err := svc.validateMergeInputs(db, merge); err != nil {
				t.Fatalf("fresh snapshot rejected: %v", err)
			}
			var update any
			switch field {
			case "path":
				update = "material_p1/changed.png"
			case "size":
				update = int64(11)
			case "type":
				update = "audio"
			}
			if err := db.Model(&overlay).Update(field, update).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := svc.validateMergeInputs(db, merge); err == nil {
				t.Fatalf("overlay %s mutation accepted", field)
			}
		})
	}
}

func TestEditedMergeRejectsInvalidOverlayMaterial(t *testing.T) {
	db := safetyDB(t, &models.MergeTask{}, &models.Material{})
	project := models.Project{Title: "overlay validation", Generation: 1}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	a := readyMergeScene(project.ID, 1, 1, 1, "a", "a.mp4")
	b := readyMergeScene(project.ID, 1, 2, 1, "b", "b.mp4")
	if err := db.Create(&a).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&b).Error; err != nil {
		t.Fatal(err)
	}
	zero := uint(0)
	clips := []mergeEditClip{{SceneID: &a.ID, OverlayMaterialID: &zero}, {SceneID: &b.ID}}
	_, _, err := (&ProjectService{db: db}).createMergeTaskRecord(&project, []uint{a.ID, b.ID}, false, false, MergeAudioOptions{}, mergeTaskSequence{Clips: clips, EpisodeN: 1})
	if err == nil {
		t.Fatal("zero overlay material ID accepted")
	}
}
