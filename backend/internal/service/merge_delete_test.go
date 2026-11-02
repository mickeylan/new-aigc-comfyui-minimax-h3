package service

import (
	"os"
	"path/filepath"
	"testing"

	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
)

func TestStaleDialogueIsSkippedWithReason(t *testing.T) {
	svc := newTestProjectService(t)
	svc.cfg = &config.Config{}
	if err := svc.db.AutoMigrate(&models.Dialogue{}); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "问仙"}
	if err := svc.db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	gpu := 0
	scenes := []models.Scene{
		{ProjectID: project.ID, EpisodeN: 1, Generation: 0, Order: 1, Status: "video_ready", VideoFile: "one.mp4", VideoGPU: &gpu},
		{ProjectID: project.ID, EpisodeN: 1, Generation: 0, Order: 2, Status: "video_ready", VideoFile: "two.mp4", VideoGPU: &gpu},
	}
	for i := range scenes {
		if err := svc.db.Create(&scenes[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	dialogue := models.Dialogue{ProjectID: project.ID, SceneID: scenes[0].ID, Order: 1, Text: "姐姐", Status: "ready", AudioFile: "dialogue.wav", AudioHash: "old", AudioStale: true}
	if err := svc.db.Create(&dialogue).Error; err != nil {
		t.Fatal(err)
	}
	ready, skipped, err := svc.loadReadyDialogues(svc.db, []uint{scenes[0].ID, scenes[1].ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(ready) != 0 || len(skipped) != 1 || skipped[0].Dialogue.ID != dialogue.ID || skipped[0].Reason != "音频已过期" {
		t.Fatalf("unexpected selection: ready=%+v skipped=%+v", ready, skipped)
	}
}

func TestReadyAndPendingDialoguesSelectSubsetAndKeepTimelineSlot(t *testing.T) {
	svc := newTestProjectService(t)
	svc.cfg = &config.Config{}
	if err := svc.db.AutoMigrate(&models.Dialogue{}); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "mix"}
	svc.db.Create(&project)
	gpu := 0
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1, Status: "video_ready", VideoFile: "one.mp4", VideoGPU: &gpu}
	svc.db.Create(&scene)
	dialogues := []models.Dialogue{
		{ProjectID: project.ID, SceneID: scene.ID, Order: 1, Text: "ready one", Status: "ready", AudioFile: "one.wav"},
		{ProjectID: project.ID, SceneID: scene.ID, Order: 2, Text: "pending", Status: "pending"},
		{ProjectID: project.ID, SceneID: scene.ID, Order: 3, Text: "ready two", Status: "ready", AudioFile: "two.wav"},
	}
	for i := range dialogues {
		if err := svc.db.Create(&dialogues[i]).Error; err != nil {
			t.Fatal(err)
		}
		dialogues[i].AudioHash = svc.effectiveDialogueAudioHash(dialogues[i])
		if err := svc.db.Model(&dialogues[i]).Update("audio_hash", dialogues[i].AudioHash).Error; err != nil {
			t.Fatal(err)
		}
	}
	ready, skipped, err := svc.loadReadyDialogues(svc.db, []uint{scene.ID})
	if err != nil || len(ready) != 2 || len(skipped) != 1 || skipped[0].Dialogue.ID != dialogues[1].ID {
		t.Fatalf("selection ready=%v skipped=%v err=%v", ready, skipped, err)
	}
	segments, _, audible, err := svc.buildDubTimeline(&project, 1, []uint{scene.ID}, []sceneVideo{{dur: 10}})
	if err != nil || !audible || len(segments) != 2 {
		t.Fatalf("timeline segments=%+v audible=%v err=%v", segments, audible, err)
	}
	if segments[1].Start < segments[0].End+2.35 {
		t.Fatalf("pending dialogue slot collapsed: %+v", segments)
	}
}

func TestFailedMergeWithoutOutputRetainsFailureSnapshotAndError(t *testing.T) {
	db := newTestDBWithNewModels(t)
	if err := db.AutoMigrate(&models.Project{}, &models.MergeTask{}); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "问仙"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	svc := &ProjectService{db: db}
	failed := models.MergeTask{ProjectID: project.ID, EpisodeN: 1, Generation: 1, Status: "running", InputSnapshot: `{"version":1}`, EditSequence: `[{"scene_id":1}]`}
	if err := db.Create(&failed).Error; err != nil {
		t.Fatal(err)
	}
	svc.failMerge(&failed, &project, "对白音频未就绪")
	var retained models.MergeTask
	if err := db.First(&retained, failed.ID).Error; err != nil {
		t.Fatal(err)
	}
	if retained.Status != "failed" || retained.Error != "对白音频未就绪" || retained.InputSnapshot != failed.InputSnapshot || retained.EditSequence != failed.EditSequence {
		t.Fatalf("failed merge contract not retained: %+v", retained)
	}

	legacy := models.MergeTask{ProjectID: project.ID, EpisodeN: 1, Status: "failed", Error: "旧失败记录"}
	success := models.MergeTask{ProjectID: project.ID, EpisodeN: 1, Status: "success", OutputFile: "merged/cut.mp4"}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&success).Error; err != nil {
		t.Fatal(err)
	}
	merges, err := svc.ListMerges(project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(merges) != 3 || merges[0].ID != success.ID || merges[1].ID != legacy.ID || merges[2].ID != failed.ID {
		t.Fatalf("unexpected merge list: %+v", merges)
	}
}

func TestDeleteMergeRemovesCompletedArtifactsAndRejectsActiveTask(t *testing.T) {
	db := newTestDBWithNewModels(t)
	if err := db.AutoMigrate(&models.MergeTask{}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	outputDir := filepath.Join(root, "output_workers", "gpu0", "merged")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"cut.mp4", "cut.srt", "cut.screen.ass"} {
		if err := os.WriteFile(filepath.Join(outputDir, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	completed := models.MergeTask{ProjectID: 7, EpisodeN: 1, Status: "success", OutputFile: "merged/cut.mp4"}
	active := models.MergeTask{ProjectID: 7, EpisodeN: 1, Status: "running"}
	db.Create(&completed)
	db.Create(&active)
	svc := &ProjectService{db: db, cfg: &config.Config{Comfy: config.ComfyConfig{ComfyDir: root}}}
	if err := svc.DeleteMerge(7, completed.ID); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"cut.mp4", "cut.srt", "cut.screen.ass"} {
		if _, err := os.Stat(filepath.Join(outputDir, name)); !os.IsNotExist(err) {
			t.Fatalf("artifact still exists: %s", name)
		}
	}
	var count int64
	db.Model(&models.MergeTask{}).Where("id = ?", completed.ID).Count(&count)
	if count != 0 {
		t.Fatalf("completed merge row retained")
	}
	if err := svc.DeleteMerge(7, active.ID); err != ErrMergeDeleteConflict {
		t.Fatalf("active delete error=%v", err)
	}
	db.Model(&models.MergeTask{}).Where("id = ?", active.ID).Count(&count)
	if count != 1 {
		t.Fatalf("active merge row deleted")
	}
}
