package service

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
)

func newContinuityTestService(t *testing.T) (*ContinuityService, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Project{}, &models.Scene{}, &models.FrameCandidate{}, &models.SceneContinuity{}); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	return NewContinuityService(cfg, db, NewRemoteExec(config.RemoteConfig{})), db
}

func TestContinuitySelectAndConfigureContinue(t *testing.T) {
	svc, db := newContinuityTestService(t)
	p := models.Project{Title: "p"}
	db.Create(&p)
	s1 := models.Scene{ProjectID: p.ID, EpisodeN: 1, Generation: 1, Order: 1, Status: "video_ready", VideoTaskID: "task-1", VideoInputFile: "one.mp4", ImageFile: "one.png"}
	s2 := models.Scene{ProjectID: p.ID, EpisodeN: 1, Generation: 1, Order: 2, Status: "image_ready", ImageFile: "two.png"}
	db.Create(&s1)
	db.Create(&s2)
	frame := models.FrameCandidate{ProjectID: p.ID, SceneID: s1.ID, VideoTaskID: s1.VideoTaskID, FrameIndex: 21, ImageFile: "end.png"}
	db.Create(&frame)
	if _, err := svc.SelectFrame(p.ID, s1.ID, frame.ID); err != nil {
		t.Fatal(err)
	}
	cfg, err := svc.Configure(p.ID, s2.ID, ConfigureContinuityRequest{Mode: models.ContinuityModeContinue})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SourceSceneID == nil || *cfg.SourceSceneID != s1.ID || cfg.SelectedFrameID == nil || *cfg.SelectedFrameID != frame.ID || cfg.Status != "ready" {
		t.Fatalf("unexpected continuity: %+v", cfg)
	}
	var prepared models.Scene
	db.First(&prepared, s2.ID)
	if err := svc.PrepareScene(&prepared); err != nil {
		t.Fatal(err)
	}
	got, mode, err := svc.ContinuationFrame(s2.ID)
	if err != nil || mode != models.ContinuityModeContinue || got == nil || got.ImageFile != "end.png" {
		t.Fatalf("frame=%+v mode=%s err=%v", got, mode, err)
	}
}

func TestContinuityBridgeUsesPreviousFrameAndCurrentStoryboard(t *testing.T) {
	svc, db := newContinuityTestService(t)
	p := models.Project{Title: "p"}
	db.Create(&p)
	s1 := models.Scene{ProjectID: p.ID, EpisodeN: 1, Generation: 1, Order: 1, Status: "video_ready", VideoTaskID: "task-1", ImageFile: "one.png"}
	s2 := models.Scene{ProjectID: p.ID, EpisodeN: 1, Generation: 1, Order: 2, Status: "image_ready", ImageFile: "target.png"}
	db.Create(&s1)
	db.Create(&s2)
	frame := models.FrameCandidate{ProjectID: p.ID, SceneID: s1.ID, VideoTaskID: s1.VideoTaskID, Type: models.FrameCandidateSelected, ImageFile: "previous-end.png"}
	db.Create(&frame)
	if _, err := svc.Configure(p.ID, s2.ID, ConfigureContinuityRequest{Mode: models.ContinuityModeBridge}); err != nil {
		t.Fatal(err)
	}
	if err := svc.PrepareScene(&s2); err != nil {
		t.Fatal(err)
	}
	if s2.VideoTemplate != "minimax_h3_first_last" || s2.VideoFirstFrameImg != "previous-end.png" || s2.VideoLastFrameImg != "target.png" {
		t.Fatalf("bridge not prepared: %+v", s2)
	}
}

func TestContinuityInvalidatesOnlyDirectDependents(t *testing.T) {
	svc, db := newContinuityTestService(t)
	p := models.Project{Title: "p"}
	db.Create(&p)
	s1 := models.Scene{ProjectID: p.ID, Order: 1}
	s2 := models.Scene{ProjectID: p.ID, Order: 2}
	s3 := models.Scene{ProjectID: p.ID, Order: 3}
	db.Create(&s1)
	db.Create(&s2)
	db.Create(&s3)
	db.Create(&models.SceneContinuity{SceneID: s2.ID, Mode: models.ContinuityModeContinue, SourceSceneID: &s1.ID, Status: "ready"})
	db.Create(&models.SceneContinuity{SceneID: s3.ID, Mode: models.ContinuityModeContinue, SourceSceneID: &s2.ID, Status: "ready"})
	svc.InvalidateDependents(s1.ID, "changed")
	var c2, c3 models.SceneContinuity
	db.Where("scene_id = ?", s2.ID).First(&c2)
	db.Where("scene_id = ?", s3.ID).First(&c3)
	if c2.Status != "source_invalidated" || c3.Status != "ready" {
		t.Fatalf("unexpected statuses: %s %s", c2.Status, c3.Status)
	}
}
