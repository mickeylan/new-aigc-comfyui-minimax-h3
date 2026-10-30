package service

import (
	"errors"
	"reflect"
	"testing"

	"comfyui-console/internal/models"
)

func retryMergeFixture(t *testing.T) (*ProjectService, models.Project, models.Scene, models.MergeTask) {
	t.Helper()
	db := safetyDB(t, &models.MergeTask{}, &models.Material{})
	project := models.Project{Title: "retry", Generation: 1}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	scene := readyMergeScene(project.ID, 1, 1, 1, "task", "one.mp4")
	second := readyMergeScene(project.ID, 1, 2, 1, "task-2", "two.mp4")
	if err := db.Create(&scene).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&second).Error; err != nil {
		t.Fatal(err)
	}
	svc := &ProjectService{db: db}
	merge, _, err := svc.createMergeTaskRecord(&project, []uint{scene.ID, second.ID}, true, true, MergeAudioOptions{NativeVolume: .7, DialogueVolume: .8, BGMVolume: .9, DialogueMix: true, ScreenText: true}, mergeTaskSequence{Clips: []mergeEditClip{{SceneID: &scene.ID}, {SceneID: &second.ID}}, EpisodeN: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(merge).Updates(map[string]any{"status": "failed", "error": "original error", "preflight_warnings": `["old"]`}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(merge, merge.ID).Error; err != nil {
		t.Fatal(err)
	}
	return svc, project, scene, *merge
}

func TestCreateRetryMergeTaskCopiesImmutableInputsAndOptions(t *testing.T) {
	svc, project, _, original := retryMergeFixture(t)
	created, scenes, err := svc.createRetryMergeTask(project.ID, original.ID)
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == original.ID || created.ID == 0 || created.Status != "pending" || len(scenes) != 2 {
		t.Fatalf("bad retry: %+v scenes=%d", created, len(scenes))
	}
	if created.InputSnapshot != original.InputSnapshot || created.EditSequence != original.EditSequence || created.SceneOrder != original.SceneOrder {
		t.Fatal("snapshot/edit sequence bytes changed")
	}
	if created.RequestedDub != original.RequestedDub || created.RequestedSubtitles != original.RequestedSubtitles || created.RequestedScreenText != original.RequestedScreenText || created.NativeVolume != original.NativeVolume || created.DialogueVolume != original.DialogueVolume || created.BGMVolume != original.BGMVolume || created.DialogueMix != original.DialogueMix {
		t.Fatal("merge options changed")
	}
	var after models.MergeTask
	if err := svc.db.First(&after, original.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, original) {
		t.Fatalf("original changed:\nwant=%+v\ngot=%+v", original, after)
	}
}

func TestCreateRetryMergeTaskRejectsInvalidSourcesAndConflicts(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*testing.T, *ProjectService, models.Project, models.Scene, models.MergeTask)
		project func(models.Project) uint
		want    error
	}{
		{"no snapshot", func(t *testing.T, s *ProjectService, p models.Project, sc models.Scene, m models.MergeTask) {
			s.db.Model(&m).Update("input_snapshot", "")
		}, func(p models.Project) uint { return p.ID }, ErrMergeSnapshotChanged},
		{"source changed", func(t *testing.T, s *ProjectService, p models.Project, sc models.Scene, m models.MergeTask) {
			s.db.Model(&sc).Update("video_file", "changed.mp4")
		}, func(p models.Project) uint { return p.ID }, ErrMergeSnapshotChanged},
		{"cross project", func(t *testing.T, s *ProjectService, p models.Project, sc models.Scene, m models.MergeTask) {}, func(p models.Project) uint { return p.ID + 999 }, nil},
		{"active merge", func(t *testing.T, s *ProjectService, p models.Project, sc models.Scene, m models.MergeTask) {
			if err := s.db.Create(&models.MergeTask{ProjectID: p.ID, EpisodeN: m.EpisodeN, Status: "running"}).Error; err != nil {
				t.Fatal(err)
			}
		}, func(p models.Project) uint { return p.ID }, ErrMergeRetryConflict},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, project, scene, original := retryMergeFixture(t)
			tt.mutate(t, svc, project, scene, original)
			_, _, err := svc.createRetryMergeTask(tt.project(project), original.ID)
			if err == nil || (tt.want != nil && !errors.Is(err, tt.want)) {
				t.Fatalf("error=%v want=%v", err, tt.want)
			}
			var count int64
			svc.db.Model(&models.MergeTask{}).Where("id <> ? AND status = ?", original.ID, "pending").Count(&count)
			if count != 0 {
				t.Fatalf("created %d retry rows", count)
			}
		})
	}
}

func TestCreateRetryMergeTaskRollsBackCreateFailure(t *testing.T) {
	svc, project, _, original := retryMergeFixture(t)
	if err := svc.db.Exec(`CREATE TRIGGER reject_retry BEFORE INSERT ON merge_tasks WHEN NEW.status = 'pending' BEGIN SELECT RAISE(ABORT, 'injected'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.createRetryMergeTask(project.ID, original.ID); err == nil {
		t.Fatal("expected transaction failure")
	}
	var count int64
	if err := svc.db.Model(&models.MergeTask{}).Where("id <> ?", original.ID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("transaction retained %d new rows", count)
	}
}
