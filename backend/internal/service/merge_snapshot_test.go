package service

import (
	"encoding/json"
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

func TestMergeSnapshotRejectsScreenTextMutation(t *testing.T) {
	db := safetyDB(t, &models.MergeTask{}, &models.ScreenTextCue{})
	project := models.Project{Title: "series", Generation: 1}
	db.Create(&project)
	a := readyMergeScene(project.ID, 1, 1, 1, "a", "a.mp4")
	b := readyMergeScene(project.ID, 1, 2, 1, "b", "b.mp4")
	db.Create(&a)
	db.Create(&b)
	cue := models.ScreenTextCue{ProjectID: project.ID, EpisodeN: 1, SceneID: &a.ID, Kind: "location", Text: "玉霄宫", StartTime: 0, EndTime: 2, WritingMode: "horizontal-ltr", Anchor: "top_right", Enabled: true, ReviewStatus: "approved"}
	db.Create(&cue)
	svc := &ProjectService{db: db}
	merge, _, err := svc.createMergeTaskRecord(&project, []uint{a.ID, b.ID}, false, false, MergeAudioOptions{NativeVolume: 1, DialogueVolume: 1, BGMVolume: 1, ScreenText: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.validateMergeInputs(db, merge); err != nil {
		t.Fatalf("fresh snapshot rejected: %v", err)
	}
	db.Model(&cue).Update("font_size", 88)
	if _, err := svc.validateMergeInputs(db, merge); err == nil {
		t.Fatal("screen text font-size mutation did not invalidate merge snapshot")
	}
}

func TestMergeSnapshotExcludesUnselectedOrUnapprovedScreenText(t *testing.T) {
	db := safetyDB(t, &models.MergeTask{}, &models.ScreenTextCue{})
	project := models.Project{Title: "series", Generation: 1}
	db.Create(&project)
	a := readyMergeScene(project.ID, 1, 1, 1, "a", "a.mp4")
	b := readyMergeScene(project.ID, 1, 2, 1, "b", "b.mp4")
	other := readyMergeScene(project.ID, 1, 3, 1, "c", "c.mp4")
	db.Create(&a)
	db.Create(&b)
	db.Create(&other)
	db.Create(&models.ScreenTextCue{ProjectID: project.ID, EpisodeN: 1, SceneID: &other.ID, Kind: "location", Text: "未选场景", StartTime: 0, EndTime: 1, Enabled: true, ReviewStatus: "approved"})
	db.Create(&models.ScreenTextCue{ProjectID: project.ID, EpisodeN: 1, SceneID: &a.ID, Kind: "location", Text: "未审核", StartTime: 0, EndTime: 1, Enabled: true, ReviewStatus: "draft"})
	svc := &ProjectService{db: db}
	merge, _, err := svc.createMergeTaskRecord(&project, []uint{a.ID, b.ID}, false, false, MergeAudioOptions{NativeVolume: 1, DialogueVolume: 1, BGMVolume: 1, ScreenText: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.validateMergeInputs(db, merge); err != nil {
		t.Fatalf("excluded cues changed snapshot: %v", err)
	}
}

func TestMergeSnapshotIgnoresScreenTextWhenOptionDisabled(t *testing.T) {
	db := safetyDB(t, &models.MergeTask{}, &models.ScreenTextCue{})
	project := models.Project{Title: "series", Generation: 1}
	db.Create(&project)
	a := readyMergeScene(project.ID, 1, 1, 1, "a", "a.mp4")
	b := readyMergeScene(project.ID, 1, 2, 1, "b", "b.mp4")
	db.Create(&a)
	db.Create(&b)
	cue := models.ScreenTextCue{ProjectID: project.ID, EpisodeN: 1, SceneID: &a.ID, Kind: "location", Text: "玉霄宫", StartTime: 0, EndTime: 1, Enabled: true, ReviewStatus: "approved"}
	db.Create(&cue)
	svc := &ProjectService{db: db}
	merge, _, err := svc.createMergeTaskRecord(&project, []uint{a.ID, b.ID}, false, false, MergeAudioOptions{NativeVolume: 1, DialogueVolume: 1, BGMVolume: 1})
	if err != nil {
		t.Fatal(err)
	}
	if merge.RequestedScreenText {
		t.Fatal("screen text unexpectedly enabled")
	}
	db.Model(&cue).Update("text", "太运宗")
	if _, err := svc.validateMergeInputs(db, merge); err != nil {
		t.Fatalf("disabled screen text invalidated merge: %v", err)
	}
}

func TestMergeSequenceSupportsSceneAndMaterialAndDetectsMaterialMutation(t *testing.T) {
	db := safetyDB(t, &models.MergeTask{}, &models.Material{})
	project := models.Project{Title: "series", Generation: 1}
	db.Create(&project)
	scene := readyMergeScene(project.ID, 1, 1, 1, "a", "a.mp4")
	db.Create(&scene)
	material := models.Material{Name: "upload", Type: "video", Source: "upload", ProjectID: &project.ID, Path: "material_p1/upload.mp4", Size: 10}
	db.Create(&material)
	clips := []mergeEditClip{{SceneID: &scene.ID}, {MaterialID: &material.ID}}
	svc := &ProjectService{db: db}
	merge, scenes, err := svc.createMergeTaskRecord(&project, []uint{scene.ID}, false, false, MergeAudioOptions{NativeVolume: 1, DialogueVolume: 1, BGMVolume: 1}, mergeTaskSequence{Clips: clips, EpisodeN: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(scenes) != 1 || merge.EditSequence == "" {
		t.Fatalf("sequence not persisted: %+v", merge)
	}
	if _, err := svc.validateMergeInputs(db, merge); err != nil {
		t.Fatalf("fresh snapshot rejected: %v", err)
	}
	db.Model(&material).Update("size", 11)
	if _, err := svc.validateMergeInputs(db, merge); err == nil {
		t.Fatal("material mutation did not invalidate merge snapshot")
	}
}

func TestMergeSequenceSupportsMaterialsOnlyWithoutBorrowingSceneDialogue(t *testing.T) {
	db := safetyDB(t, &models.MergeTask{}, &models.Material{}, &models.ScreenTextCue{})
	project := models.Project{Title: "series", Generation: 1}
	db.Create(&project)
	// An unrelated Scene establishes the episode generation but is not part of the cut.
	unrelated := readyMergeScene(project.ID, 1, 1, 1, "other", "other.mp4")
	db.Create(&unrelated)
	db.Create(&models.Dialogue{ProjectID: project.ID, SceneID: unrelated.ID, Order: 1, Text: "must not leak"})
	db.Create(&models.ScreenTextCue{ProjectID: project.ID, EpisodeN: 1, SceneID: &unrelated.ID, Text: "must not leak", Enabled: true, ReviewStatus: "approved"})
	db.Create(&models.ScreenTextCue{ProjectID: project.ID, EpisodeN: 1, Text: "global", Enabled: true, ReviewStatus: "approved"})
	first := models.Material{Name: "one", Type: "video", ProjectID: &project.ID, Path: "material_p1/one.mp4", Size: 10}
	second := models.Material{Name: "two", Type: "video", ProjectID: &project.ID, Path: "material_p1/two.mp4", Size: 20}
	db.Create(&first)
	db.Create(&second)
	clips := []mergeEditClip{{MaterialID: &first.ID}, {MaterialID: &second.ID}}
	svc := &ProjectService{db: db}
	merge, scenes, err := svc.createMergeTaskRecord(&project, nil, false, true, MergeAudioOptions{NativeVolume: 1, DialogueVolume: 1, BGMVolume: 1, ScreenText: true}, mergeTaskSequence{Clips: clips, EpisodeN: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(scenes) != 0 {
		t.Fatalf("pure material cut invented scenes: %+v", scenes)
	}
	var snapshot mergeInputSnapshot
	if err := json.Unmarshal([]byte(merge.InputSnapshot), &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Materials) != 2 || len(snapshot.Dialogues) != 0 || len(snapshot.ScreenTextCues) != 1 || snapshot.ScreenTextCues[0].Text != "global" {
		t.Fatalf("unexpected pure-material snapshot: %+v", snapshot)
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
