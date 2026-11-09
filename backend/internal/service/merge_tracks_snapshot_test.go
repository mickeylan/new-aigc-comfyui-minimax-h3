package service

import (
	"comfyui-console/internal/models"
	"encoding/json"
	"testing"
)

func TestMultiTrackSnapshotRejectsSourceAndEnvelopeChanges(t *testing.T) {
	db := safetyDB(t, &models.MergeTask{}, &models.Material{}, &models.AudioLayer{})
	p := models.Project{Title: "tracks snapshot", Generation: 1}
	db.Create(&p)
	a := readyMergeScene(p.ID, 1, 1, 1, "a", "a.mp4")
	b := readyMergeScene(p.ID, 1, 2, 1, "b", "b.mp4")
	db.Create(&a)
	db.Create(&b)
	m := models.Material{ProjectID: &p.ID, Type: "image", Path: "1/overlay.png", Size: 20}
	db.Create(&m)
	layer := models.AudioLayer{ProjectID: p.ID, EpisodeN: 1, Kind: "bgm", File: "music.wav", StartTime: 0, EndTime: 2, Status: "ready", VolumeEnvelope: []models.AudioVolumePoint{{Time: 0, Gain: 0}, {Time: 2, Gain: 1}}}
	db.Create(&layer)
	clips := []mergeEditClip{{SceneID: &a.ID, VisualTracks: []clipVisualTrack{{MaterialID: m.ID, Start: 0, End: 1, Width: .5, Opacity: 1}}, Markers: []clipMarker{{Time: 1, Label: "hit"}}}, {SceneID: &b.ID}}
	svc := &ProjectService{db: db}
	// Match the public handler's normalized defaults (not zero-value gains, which
	// GORM's pre-existing defaults replace on task creation).
	audio, err := mergeAudioOptions(nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	task, _, err := svc.createMergeTaskRecord(&p, []uint{a.ID, b.ID}, false, false, audio, mergeTaskSequence{Clips: clips, EpisodeN: 1})
	if err != nil {
		t.Fatal(err)
	}
	var snap mergeInputSnapshot
	if err := json.Unmarshal([]byte(task.InputSnapshot), &snap); err != nil || len(snap.Materials) != 1 || len(snap.AudioLayers) != 1 || len(snap.AudioLayers[0].VolumeEnvelope) != 2 {
		t.Fatalf("snapshot missing tracks %+v %v", snap, err)
	}
	if _, err := svc.validateMergeInputs(db, task); err != nil {
		t.Fatalf("fresh: %v", err)
	}
	db.Model(&m).Update("size", 21)
	if _, err := svc.validateMergeInputs(db, task); err == nil {
		t.Fatal("changed overlay accepted")
	}
	db.Model(&m).Update("size", 20)
	db.Model(&layer).Update("volume_envelope", `[{"time":0,"gain":1},{"time":2,"gain":0}]`)
	if _, err := svc.validateMergeInputs(db, task); err == nil {
		t.Fatal("changed gain envelope accepted")
	}
}
