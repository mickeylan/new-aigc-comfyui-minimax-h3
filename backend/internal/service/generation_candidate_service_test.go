package service

import (
	"errors"
	"testing"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

func TestGenerationCandidateReviewAndSelectProjectsToScene(t *testing.T) {
	db := newTestDBWithNewModels(t)
	if err := db.AutoMigrate(&models.GenerationCandidate{}); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "p"}
	db.Create(&project)
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1}
	db.Create(&scene)
	svc := NewGenerationCandidateService(db)
	one, err := svc.CaptureSceneTask(project.ID, scene.ID, "image", "task-1", "one.png", "prompt", []string{"a.png"}, map[string]any{"seed": 1}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	two, err := svc.CaptureSceneTask(project.ID, scene.ID, "image", "task-2", "two.png", "prompt2", nil, nil, nil, &one.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Review(project.ID, one.ID, "rejected", "构图不符"); err != nil {
		t.Fatal(err)
	}
	selected, err := svc.SelectCurrent(project.ID, two.ID)
	if err != nil || !selected.IsCurrent {
		t.Fatalf("selected=%+v err=%v", selected, err)
	}
	db.First(&scene, scene.ID)
	if scene.ImageFile != "two.png" || scene.ImageTaskID != "task-2" {
		t.Fatalf("scene projection=%+v", scene)
	}
	rows, err := svc.ListScene(project.ID, scene.ID, "image")
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
}

func TestDetachedImageRetrySuccessInvalidatesVideoFramesAndContinuity(t *testing.T) {
	db := newTestDBWithNewModels(t)
	if err := db.AutoMigrate(&models.GenerationCandidate{}, &models.FrameCandidate{}, &models.SceneContinuity{}); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "p"}
	db.Create(&project)
	source := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1, Status: "video_ready", ImageFile: "old.png", ImageTaskID: "image-old", VideoFile: "old.mp4", VideoInputFile: "old-local.mp4", VideoTaskID: "video-old", VideoFullPrompt: "old prompt"}
	direct := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 2, Status: "video_ready", ImageFile: "direct.png", VideoFile: "direct.mp4", VideoTaskID: "video-direct"}
	transitive := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 3, Status: "video_ready", ImageFile: "transitive.png", VideoFile: "transitive.mp4", VideoTaskID: "video-transitive"}
	db.Create(&source)
	db.Create(&direct)
	db.Create(&transitive)
	parent := models.GenerationCandidate{ProjectID: project.ID, EntityType: "scene", EntityID: source.ID, MediaType: "image", TaskID: source.ImageTaskID, File: source.ImageFile, IsCurrent: true}
	oldVideo := models.GenerationCandidate{ProjectID: project.ID, EntityType: "scene", EntityID: source.ID, MediaType: "video", TaskID: source.VideoTaskID, File: source.VideoFile, IsCurrent: true}
	directVideo := models.GenerationCandidate{ProjectID: project.ID, EntityType: "scene", EntityID: direct.ID, MediaType: "video", TaskID: direct.VideoTaskID, File: direct.VideoFile, IsCurrent: true}
	transitiveVideo := models.GenerationCandidate{ProjectID: project.ID, EntityType: "scene", EntityID: transitive.ID, MediaType: "video", TaskID: transitive.VideoTaskID, File: transitive.VideoFile, IsCurrent: true}
	db.Create(&parent)
	db.Create(&oldVideo)
	db.Create(&directVideo)
	db.Create(&transitiveVideo)
	frame := models.FrameCandidate{ProjectID: project.ID, SceneID: source.ID, VideoTaskID: source.VideoTaskID, Type: models.FrameCandidateSelected, FrameIndex: 21, ImageFile: "tail.png"}
	db.Create(&frame)
	db.Create(&models.SceneContinuity{SceneID: direct.ID, Mode: models.ContinuityModeContinue, SourceSceneID: &source.ID, SelectedFrameID: &frame.ID, SourceVideoTaskID: source.VideoTaskID, Status: "ready"})
	db.Create(&models.SceneContinuity{SceneID: transitive.ID, Mode: models.ContinuityModeContinue, SourceSceneID: &direct.ID, SourceVideoTaskID: direct.VideoTaskID, Status: "ready"})

	_, err := NewGenerationCandidateService(db).CaptureSceneSuccess(SceneCandidateCapture{
		ProjectID: project.ID, SceneID: source.ID, MediaType: "image", TaskID: "image-retry", File: "retry.png",
		ParentCandidateID: &parent.ID, RequireParentFresh: true, FinalizeDerived: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	db.First(&source, source.ID)
	if source.ImageFile != "retry.png" || source.VideoFile != "" || source.VideoTaskID != "" || source.VideoInputFile != "" || source.Status != "image_ready" {
		t.Fatalf("source projection retained stale video: %+v", source)
	}
	var frameCount int64
	db.Model(&models.FrameCandidate{}).Where("scene_id = ?", source.ID).Count(&frameCount)
	if frameCount != 0 {
		t.Fatalf("old frame candidates retained: %d", frameCount)
	}
	for _, candidate := range []*models.GenerationCandidate{&oldVideo, &directVideo, &transitiveVideo} {
		db.First(candidate, candidate.ID)
		if !candidate.Stale || candidate.IsCurrent {
			t.Fatalf("video candidate not invalidated: %+v", candidate)
		}
	}
	for _, scene := range []*models.Scene{&direct, &transitive} {
		db.First(scene, scene.ID)
		if scene.VideoFile != "" || scene.VideoTaskID != "" || scene.Status != "image_ready" {
			t.Fatalf("continuity-dependent video retained: %+v", scene)
		}
		var cfg models.SceneContinuity
		db.Where("scene_id = ?", scene.ID).First(&cfg)
		if cfg.Status != "source_invalidated" || cfg.SelectedFrameID != nil {
			t.Fatalf("continuity dependency not invalidated: %+v", cfg)
		}
	}
}

func TestDetachedVideoRetrySuccessClearsOldFramesAndInvalidatesContinuity(t *testing.T) {
	db := newTestDBWithNewModels(t)
	if err := db.AutoMigrate(&models.GenerationCandidate{}, &models.FrameCandidate{}, &models.SceneContinuity{}); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "p"}
	db.Create(&project)
	source := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1, Status: "video_ready", ImageFile: "story.png", VideoFile: "old.mp4", VideoInputFile: "old-local.mp4", VideoTaskID: "video-old"}
	dependent := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 2, Status: "video_ready", ImageFile: "next.png", VideoFile: "next.mp4", VideoTaskID: "video-next"}
	db.Create(&source)
	db.Create(&dependent)
	parent := models.GenerationCandidate{ProjectID: project.ID, EntityType: "scene", EntityID: source.ID, MediaType: "video", TaskID: source.VideoTaskID, File: source.VideoFile, IsCurrent: true}
	dependentVideo := models.GenerationCandidate{ProjectID: project.ID, EntityType: "scene", EntityID: dependent.ID, MediaType: "video", TaskID: dependent.VideoTaskID, File: dependent.VideoFile, IsCurrent: true}
	db.Create(&parent)
	db.Create(&dependentVideo)
	frame := models.FrameCandidate{ProjectID: project.ID, SceneID: source.ID, VideoTaskID: source.VideoTaskID, Type: models.FrameCandidateSelected, FrameIndex: 21, ImageFile: "old-tail.png"}
	db.Create(&frame)
	db.Create(&models.SceneContinuity{SceneID: dependent.ID, Mode: models.ContinuityModeContinue, SourceSceneID: &source.ID, SelectedFrameID: &frame.ID, SourceVideoTaskID: source.VideoTaskID, Status: "ready"})
	gpu := 2

	_, err := NewGenerationCandidateService(db).CaptureSceneSuccess(SceneCandidateCapture{
		ProjectID: project.ID, SceneID: source.ID, MediaType: "video", TaskID: "video-retry", File: "remote-retry.mp4",
		VideoInputFile: "retry-local.mp4", VideoGPU: &gpu, ParentCandidateID: &parent.ID, RequireParentFresh: true, FinalizeDerived: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	db.First(&source, source.ID)
	if source.VideoTaskID != "video-retry" || source.VideoInputFile != "retry-local.mp4" || source.Status != "video_ready" {
		t.Fatalf("retry video was not projected: %+v", source)
	}
	var frameCount int64
	db.Model(&models.FrameCandidate{}).Where("scene_id = ?", source.ID).Count(&frameCount)
	if frameCount != 0 {
		t.Fatalf("old frames must be removed before retry frame extraction: %d", frameCount)
	}
	db.First(&dependent, dependent.ID)
	db.First(&dependentVideo, dependentVideo.ID)
	var cfg models.SceneContinuity
	db.Where("scene_id = ?", dependent.ID).First(&cfg)
	if dependent.VideoTaskID != "" || !dependentVideo.Stale || cfg.Status != "source_invalidated" || cfg.SelectedFrameID != nil {
		t.Fatalf("downstream continuity retained stale output: scene=%+v candidate=%+v continuity=%+v", dependent, dependentVideo, cfg)
	}
}

func TestGenerationCandidateStaleCannotBecomeCurrent(t *testing.T) {
	db := newTestDBWithNewModels(t)
	if err := db.AutoMigrate(&models.GenerationCandidate{}); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "p"}
	db.Create(&project)
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1}
	db.Create(&scene)
	svc := NewGenerationCandidateService(db)
	row, _ := svc.CaptureSceneTask(project.ID, scene.ID, "video", "v1", "v.mp4", "prompt", nil, nil, nil, nil)
	if err := MarkSceneCandidatesStale(db, project.ID, scene.ID, "video", "提示词变化"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SelectCurrent(project.ID, row.ID); err == nil {
		t.Fatal("stale candidate selected")
	}
}

func TestCandidateDeleteRejectsCurrent(t *testing.T) {
	db := newTestDBWithNewModels(t)
	if err := db.AutoMigrate(&models.GenerationCandidate{}); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "p"}
	db.Create(&project)
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1}
	db.Create(&scene)
	svc := NewGenerationCandidateService(db)
	row, _ := svc.CaptureSceneTask(project.ID, scene.ID, "image", "img1", "a.png", "prompt", nil, nil, nil, nil)
	// row is current by default after capture
	if err := svc.Delete(project.ID, row.ID); err == nil {
		t.Fatal("current candidate deleted")
	} else if err != ErrCandidateIsCurrent {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCandidateDeleteRejectsWithChildren(t *testing.T) {
	db := newTestDBWithNewModels(t)
	if err := db.AutoMigrate(&models.GenerationCandidate{}, &models.Task{}); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "p"}
	db.Create(&project)
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1}
	db.Create(&scene)
	svc := NewGenerationCandidateService(db)
	// Create current candidate (needed to have at least one current)
	current, _ := svc.CaptureSceneTask(project.ID, scene.ID, "image", "current-task", "current.png", "prompt", nil, nil, nil, nil)
	// Create a non-current candidate
	orphan := models.GenerationCandidate{
		ProjectID: project.ID, EntityType: "scene", EntityID: scene.ID, MediaType: "image",
		TaskID: "orphan-task", File: "orphan.png", IsCurrent: false,
	}
	db.Create(&orphan)
	// Create a child candidate referencing the orphan
	child := models.GenerationCandidate{
		ProjectID: project.ID, EntityType: "scene", EntityID: scene.ID, MediaType: "image",
		TaskID: "child-task", File: "child.png", ParentCandidateID: &orphan.ID, IsCurrent: false,
	}
	db.Create(&child)
	// Cannot delete orphan with child branch
	if err := svc.Delete(project.ID, orphan.ID); err == nil {
		t.Fatal("candidate with children deleted")
	} else if err != ErrCandidateHasChildren {
		t.Fatalf("unexpected error: %v", err)
	}
	// Can delete child without children
	if err := svc.Delete(project.ID, child.ID); err != nil {
		t.Fatalf("child deletion failed: %v", err)
	}
	// Can delete orphan after child is gone
	if err := svc.Delete(project.ID, orphan.ID); err != nil {
		t.Fatalf("orphan deletion failed: %v", err)
	}
	_ = current // silence unused warning
}

func TestCandidateDeleteRejectsRunningTask(t *testing.T) {
	db := newTestDBWithNewModels(t)
	if err := db.AutoMigrate(&models.GenerationCandidate{}, &models.Task{}); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "p"}
	db.Create(&project)
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1}
	db.Create(&scene)
	svc := NewGenerationCandidateService(db)
	// Create current candidate
	current, _ := svc.CaptureSceneTask(project.ID, scene.ID, "video", "current-task", "current.mp4", "prompt", nil, nil, nil, nil)
	// Create a non-current candidate with a running task
	orphan := models.GenerationCandidate{
		ProjectID: project.ID, EntityType: "scene", EntityID: scene.ID, MediaType: "video",
		TaskID: "running-task", File: "orphan.mp4", IsCurrent: false,
	}
	db.Create(&orphan)
	db.Create(&models.Task{TaskID: "running-task", Status: "running"})
	// Cannot delete candidate with running task
	if err := svc.Delete(project.ID, orphan.ID); err == nil {
		t.Fatal("candidate with running task deleted")
	} else if err != ErrCandidateTaskRunning {
		t.Fatalf("unexpected error: %v", err)
	}
	// Completed task is OK to delete
	db.Model(&models.Task{}).Where("task_id = ?", "running-task").Update("status", "success")
	if err := svc.Delete(project.ID, orphan.ID); err != nil {
		t.Fatalf("candidate with completed task deletion failed: %v", err)
	}
	_ = current // silence unused warning
}

func TestCandidateDeleteSuccess(t *testing.T) {
	db := newTestDBWithNewModels(t)
	if err := db.AutoMigrate(&models.GenerationCandidate{}, &models.Task{}); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "p"}
	db.Create(&project)
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1}
	db.Create(&scene)
	svc := NewGenerationCandidateService(db)
	// Create a non-current candidate
	candidate := models.GenerationCandidate{
		ProjectID: project.ID, EntityType: "scene", EntityID: scene.ID, MediaType: "image",
		TaskID: "orphan-task", File: "orphan.png", IsCurrent: false,
	}
	db.Create(&candidate)
	if err := svc.Delete(project.ID, candidate.ID); err != nil {
		t.Fatalf("orphan candidate deletion failed: %v", err)
	}
	var count int64
	db.Model(&models.GenerationCandidate{}).Where("id = ?", candidate.ID).Count(&count)
	if count != 0 {
		t.Fatalf("candidate still exists after deletion")
	}
}

func TestCandidateDeleteNotFound(t *testing.T) {
	db := newTestDBWithNewModels(t)
	if err := db.AutoMigrate(&models.GenerationCandidate{}); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "p"}
	db.Create(&project)
	svc := NewGenerationCandidateService(db)
	err := svc.Delete(project.ID, 9999)
	if err == nil {
		t.Fatal("expected not found error")
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("unexpected error type: %v", err)
	}
}
