package service

import (
	"comfyui-console/internal/models"
	"encoding/json"
	"testing"
)

func TestEditedMergeSnapshotRejectsChangedTrimAndTimelineInputs(t *testing.T) {
	for _, field := range []string{"trim", "source_start", "ducking", "time_segment", "episode_duration"} {
		t.Run(field, func(t *testing.T) {
			db := safetyDB(t, &models.MergeTask{})
			p := models.Project{Title: "edit snapshot", Generation: 1}
			if err := db.Create(&p).Error; err != nil {
				t.Fatal(err)
			}
			a := readyMergeScene(p.ID, 1, 1, 1, "a", "a.mp4")
			a.Duration = 6
			b := readyMergeScene(p.ID, 1, 2, 1, "b", "b.mp4")
			b.Duration = 4
			if err := db.Create(&a).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&b).Error; err != nil {
				t.Fatal(err)
			}
			d := models.Dialogue{ProjectID: p.ID, SceneID: a.ID, Order: 1, Text: "原话", TimeSegment: "1-2秒"}
			if err := db.Create(&d).Error; err != nil {
				t.Fatal(err)
			}
			layer := models.AudioLayer{ProjectID: p.ID, EpisodeN: 1, Kind: "bgm", File: "music.wav", Status: "ready", StartTime: 0, EndTime: 3, SourceStart: 1, Volume: 1}
			if err := db.Create(&layer).Error; err != nil {
				t.Fatal(err)
			}
			s := &ProjectService{db: db}
			clips := []mergeEditClip{{ID: "a-left", SceneID: &a.ID, SourceStart: 1, SourceEnd: float64Ptr(3)}, {ID: "b", SceneID: &b.ID}}
			mt, _, err := s.createMergeTaskRecord(&p, []uint{a.ID, b.ID}, true, true, MergeAudioOptions{NativeVolume: 1, DialogueVolume: 1, BGMVolume: 1}, mergeTaskSequence{Clips: clips, EpisodeN: 1})
			if err != nil {
				t.Fatal(err)
			}
			var snap mergeInputSnapshot
			if err := json.Unmarshal([]byte(mt.InputSnapshot), &snap); err != nil {
				t.Fatal(err)
			}
			if snap.Version != 7 || len(snap.EditSequence) != 2 || len(snap.EpisodeTimeline) != 2 {
				t.Fatalf("incomplete edited snapshot: %+v", snap)
			}
			if _, err := s.validateMergeInputs(db, mt); err != nil {
				t.Fatalf("fresh snapshot: %v", err)
			}
			var mutateErr error
			switch field {
			case "trim":
				clips[0].SourceEnd = float64Ptr(2)
				data, _ := json.Marshal(clips)
				mutateErr = db.Model(mt).Update("edit_sequence", string(data)).Error
			case "source_start":
				mutateErr = db.Model(&layer).Update("source_start", 2).Error
			case "ducking":
				mutateErr = db.Model(&layer).Update("ducking", !layer.Ducking).Error
			case "time_segment":
				mutateErr = db.Model(&d).Update("time_segment", "2-3秒").Error
			case "episode_duration":
				mutateErr = db.Model(&a).Update("duration", 7).Error
			}
			if mutateErr != nil {
				t.Fatal(mutateErr)
			}
			if _, err := s.validateMergeInputs(db, mt); err == nil {
				t.Fatalf("%s mutation accepted", field)
			}
		})
	}
}
