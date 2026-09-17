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
	if err := db.AutoMigrate(&models.Project{}, &models.Scene{}, &models.FrameCandidate{}, &models.SceneContinuity{}, &models.UploadFile{}); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Comfy.ComfyDir = t.TempDir()
	remote := NewRemoteExec(config.RemoteConfig{})
	upload := NewUploadManager(cfg, db, remote)
	return NewContinuityService(cfg, db, remote, upload), db
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

func TestContinuityPreConfigureWithoutFrame(t *testing.T) {
	svc, db := newContinuityTestService(t)
	p := models.Project{Title: "p"}
	db.Create(&p)
	s1 := models.Scene{ProjectID: p.ID, EpisodeN: 1, Generation: 1, Order: 1, Status: "video_ready", VideoTaskID: "task-1", VideoInputFile: "one.mp4", ImageFile: "one.png"}
	s2 := models.Scene{ProjectID: p.ID, EpisodeN: 1, Generation: 1, Order: 2, Status: "image_ready", ImageFile: "two.png"}
	db.Create(&s1)
	db.Create(&s2)
	// No frame selected yet; configure should succeed with status=waiting
	cfg, err := svc.Configure(p.ID, s2.ID, ConfigureContinuityRequest{Mode: models.ContinuityModeContinue})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SourceSceneID == nil || *cfg.SourceSceneID != s1.ID || cfg.Status != "waiting" || cfg.SelectedFrameID != nil {
		t.Fatalf("unexpected pre-config continuity: %+v", cfg)
	}
	// The mode may be preconfigured, but generation must wait for a selected source frame.
	var prepared models.Scene
	db.First(&prepared, s2.ID)
	if err := svc.PrepareScene(&prepared); err == nil {
		t.Fatal("expected generation to be blocked until a source frame is selected")
	}
	// Now select a frame for s1
	frame := models.FrameCandidate{ProjectID: p.ID, SceneID: s1.ID, VideoTaskID: s1.VideoTaskID, FrameIndex: 21, ImageFile: "end.png"}
	db.Create(&frame)
	if _, err := svc.SelectFrame(p.ID, s1.ID, frame.ID); err != nil {
		t.Fatal(err)
	}
	// ContinuationFrame should now return the selected frame
	got, mode, err := svc.ContinuationFrame(s2.ID)
	if err != nil || mode != models.ContinuityModeContinue || got == nil || got.ImageFile != "end.png" {
		t.Fatalf("frame=%+v mode=%s err=%v", got, mode, err)
	}
	// Reload config; status should be ready
	cfg2, _ := svc.Get(p.ID, s2.ID)
	if cfg2.Status != "ready" || cfg2.SelectedFrameID == nil || *cfg2.SelectedFrameID != frame.ID {
		t.Fatalf("expected ready with selected frame: %+v", cfg2)
	}
}

func TestContinuityReplaceSelectedFramePreservesOriginalAndInvalidatesDependent(t *testing.T) {
	svc, db := newContinuityTestService(t)
	p := models.Project{Title: "p"}
	db.Create(&p)
	s1 := models.Scene{ProjectID: p.ID, VideoTaskID: "task-1", Status: "video_ready"}
	s2 := models.Scene{ProjectID: p.ID}
	db.Create(&s1)
	db.Create(&s2)
	frame := models.FrameCandidate{ProjectID: p.ID, SceneID: s1.ID, VideoTaskID: "task-1", Type: models.FrameCandidateSelected, ImageFile: "original.png", Source: "extracted"}
	db.Create(&frame)
	db.Create(&models.SceneContinuity{SceneID: s2.ID, Mode: models.ContinuityModeContinue, SourceSceneID: &s1.ID, SelectedFrameID: &frame.ID, SourceVideoTaskID: "task-1", Status: "ready"})
	replaced, err := svc.ReplaceSelectedFrame(p.ID, s1.ID, "hd.png", []byte("image-data"))
	if err != nil {
		t.Fatal(err)
	}
	if replaced.Source != "manual_upload" || replaced.OriginalImageFile != "original.png" || replaced.ImageFile == "original.png" {
		t.Fatalf("unexpected replaced frame: %+v", replaced)
	}
	var dependent models.SceneContinuity
	db.Where("scene_id = ?", s2.ID).First(&dependent)
	if dependent.Status != "source_invalidated" {
		t.Fatalf("dependent status=%s", dependent.Status)
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
