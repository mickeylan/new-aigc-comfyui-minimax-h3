package service

import (
	"os"
	"path/filepath"
	"testing"

	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
)

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
