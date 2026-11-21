package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"comfyui-console/internal/models"
)

func newStudioCandidateDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&models.Project{}, &models.Scene{}, &models.GenerationCandidate{},
		&models.FrameCandidate{}, &models.SceneContinuity{}, &models.MergeTask{},
	); err != nil {
		t.Fatal(err)
	}
	return db
}

// 剪辑器写回：新版本成为当前候选，旧版本降级但保留可切回；Scene.Duration 不被覆盖；
// 正在跑的成片合并按既有规则标失败，避免用旧片段继续渲染。
func TestSaveStudioSceneVideoCandidateKeepsHistoryAndFailsPendingMerge(t *testing.T) {
	db := newStudioCandidateDB(t)
	project := models.Project{Title: "studio"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 2, Order: 1, Duration: 5, VideoFile: "old.mp4", VideoInputFile: "old.mp4", VideoTaskID: "old-task", Status: "video_ready"}
	if err := db.Create(&scene).Error; err != nil {
		t.Fatal(err)
	}
	oldCandidate := models.GenerationCandidate{ProjectID: project.ID, EntityType: "scene", EntityID: scene.ID, MediaType: "video", TaskID: "old-task", File: "old.mp4", VideoInputFile: "old.mp4", IsCurrent: true}
	if err := db.Create(&oldCandidate).Error; err != nil {
		t.Fatal(err)
	}
	merge := models.MergeTask{ProjectID: project.ID, EpisodeN: 2, Status: "pending"}
	if err := db.Create(&merge).Error; err != nil {
		t.Fatal(err)
	}

	svc := &ProjectService{db: db}
	// 剪辑器导出的精修版比目标时长长：允许，不改 Scene.Duration。
	candidate, err := svc.SaveStudioSceneVideoCandidate(&scene, "/tmp/export/精修版.mp4", "精修版.mp4")
	if err != nil {
		t.Fatal(err)
	}

	if !candidate.IsCurrent || candidate.MediaType != "video" {
		t.Fatalf("新版本必须是当前的视频候选：%+v", candidate)
	}
	if candidate.File != "精修版.mp4" || candidate.VideoInputFile != "精修版.mp4" {
		t.Fatalf("候选文件应使用项目素材目录内的文件名：%+v", candidate)
	}
	var provenance map[string]any
	if err := json.Unmarshal([]byte(candidate.ProvenanceJSON), &provenance); err != nil {
		t.Fatalf("provenance 不是合法 JSON：%v", err)
	}
	if provenance["source"] != "studio_video" || provenance["source_filename"] != "精修版.mp4" {
		t.Fatalf("候选来源不可追溯：%v", provenance)
	}
	if !strings.HasPrefix(candidate.TaskID, "studio-video-") {
		t.Fatalf("剪辑器候选应带独立 task id：%q", candidate.TaskID)
	}

	var old models.GenerationCandidate
	if err := db.First(&old, oldCandidate.ID).Error; err != nil {
		t.Fatal(err)
	}
	if old.IsCurrent {
		t.Fatal("旧版本应降级为历史候选，不能被静默丢弃")
	}

	var saved models.Scene
	if err := db.First(&saved, scene.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.VideoInputFile != "精修版.mp4" {
		t.Fatalf("Scene 应切到精修版：%q", saved.VideoInputFile)
	}
	if saved.Duration != 5 {
		t.Fatalf("写回不应改目标时长：%v", saved.Duration)
	}
	if saved.Status != "video_ready" {
		t.Fatalf("状态应为 video_ready：%q", saved.Status)
	}

	var merged models.MergeTask
	if err := db.First(&merged, merge.ID).Error; err != nil {
		t.Fatal(err)
	}
	if merged.Status != "failed" {
		t.Fatalf("在跑的成片合并必须标失败等待重新确认，实际 %q", merged.Status)
	}

	// 再写回一次：候选历史继续累积，只有最新一条是当前。
	second, err := svc.SaveStudioSceneVideoCandidate(&saved, "精修版2.mp4", "精修版2.mp4")
	if err != nil {
		t.Fatal(err)
	}
	var current int64
	if err := db.Model(&models.GenerationCandidate{}).Where("entity_id = ? AND media_type = ? AND is_current = ?", scene.ID, "video", true).Count(&current).Error; err != nil {
		t.Fatal(err)
	}
	if current != 1 {
		t.Fatalf("同一时刻只能有一个当前候选，实际 %d", current)
	}
	var latest models.GenerationCandidate
	if err := db.First(&latest, second.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !latest.IsCurrent {
		t.Fatal("第二次写回后最新候选应为当前")
	}
	var count int64
	if err := db.Model(&models.GenerationCandidate{}).Where("entity_id = ? AND media_type = ?", scene.ID, "video").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("三次版本都应保留为候选，实际 %d 条", count)
	}
}