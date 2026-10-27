package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
)

func TestStaleDialogueRejectsMergeBeforeRecordCreation(t *testing.T) {
	svc := newTestProjectService(t)
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
	_, _, err := svc.createMergeTaskRecord(&project, []uint{scenes[0].ID, scenes[1].ID}, true, false, MergeAudioOptions{DialogueMix: true, DialogueVolume: 1})
	if err == nil || !strings.Contains(err.Error(), "音频未就绪或已过期") {
		t.Fatalf("expected stale dialogue rejection, got %v", err)
	}
	var count int64
	if err := svc.db.Model(&models.MergeTask{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("stale dialogue created %d merge records", count)
	}
}

func TestFailedMergeWithoutOutputIsRemoved(t *testing.T) {
	db := newTestDBWithNewModels(t)
	if err := db.AutoMigrate(&models.Project{}, &models.MergeTask{}); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "问仙"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	svc := &ProjectService{db: db}
	failed := models.MergeTask{ProjectID: project.ID, EpisodeN: 1, Generation: 1, Status: "running"}
	if err := db.Create(&failed).Error; err != nil {
		t.Fatal(err)
	}
	svc.failMerge(&failed, &project, "对白音频未就绪")
	var count int64
	if err := db.Model(&models.MergeTask{}).Where("id = ?", failed.ID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("failed merge row retained")
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
	if len(merges) != 1 || merges[0].ID != success.ID {
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
