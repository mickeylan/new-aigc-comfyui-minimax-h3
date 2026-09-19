package service

import (
	"testing"

	"comfyui-console/internal/models"
)

func readyMergeScene(projectID uint, episode, order int, generation uint, task, file string) models.Scene {
	gpu := 0
	return models.Scene{ProjectID: projectID, EpisodeN: episode, Order: order, Generation: generation, Status: "video_ready", VideoTaskID: task, VideoFile: file, VideoGPU: &gpu}
}

func TestMergeUsesLatestGenerationPerEpisodeAndAllowsParallelEpisodes(t *testing.T) {
	db := safetyDB(t, &models.MergeTask{})
	project := models.Project{Title: "series", Generation: 9}
	db.Create(&project)
	scenes := []models.Scene{
		readyMergeScene(project.ID, 1, 1, 3, "e1a", "e1a.mp4"),
		readyMergeScene(project.ID, 1, 2, 3, "e1b", "e1b.mp4"),
		readyMergeScene(project.ID, 2, 1, 9, "e2a", "e2a.mp4"),
		readyMergeScene(project.ID, 2, 2, 9, "e2b", "e2b.mp4"),
	}
	for i := range scenes {
		if err := db.Create(&scenes[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	svc := &ProjectService{db: db}
	first, _, err := svc.createMergeTaskRecord(&project, []uint{scenes[0].ID, scenes[1].ID}, true, false, MergeAudioOptions{NativeVolume: 1, DialogueVolume: 1, BGMVolume: 1})
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := svc.createMergeTaskRecord(&project, []uint{scenes[2].ID, scenes[3].ID}, true, false, MergeAudioOptions{NativeVolume: 1, DialogueVolume: 1, BGMVolume: 1})
	if err != nil {
		t.Fatal(err)
	}
	if first.Generation != 3 || second.Generation != 9 {
		t.Fatalf("episode generations not preserved: %d %d", first.Generation, second.Generation)
	}
	if _, _, err := svc.createMergeTaskRecord(&project, []uint{scenes[0].ID, scenes[1].ID}, true, false, MergeAudioOptions{NativeVolume: 1, DialogueVolume: 1, BGMVolume: 1}); err == nil {
		t.Fatal("second active merge for same episode was accepted")
	}
}

func TestMergeSnapshotRejectsDialogueMutation(t *testing.T) {
	db := safetyDB(t, &models.MergeTask{})
	project := models.Project{Title: "series", Generation: 1}
	db.Create(&project)
	a := readyMergeScene(project.ID, 1, 1, 1, "a", "a.mp4")
	b := readyMergeScene(project.ID, 1, 2, 1, "b", "b.mp4")
	db.Create(&a)
	db.Create(&b)
	dialogue := models.Dialogue{ProjectID: project.ID, SceneID: a.ID, Order: 1, Text: "original", Status: "ready", AudioFile: "line.mp3", AudioHash: "hash"}
	db.Create(&dialogue)
	svc := &ProjectService{db: db}
	merge, _, err := svc.createMergeTaskRecord(&project, []uint{a.ID, b.ID}, false, true, MergeAudioOptions{NativeVolume: 1, DialogueVolume: 1, BGMVolume: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.validateMergeInputs(db, merge); err != nil {
		t.Fatalf("fresh snapshot rejected: %v", err)
	}
	db.Model(&dialogue).Update("text", "changed")
	if _, err := svc.validateMergeInputs(db, merge); err == nil {
		t.Fatal("dialogue mutation did not invalidate merge snapshot")
	}
}
