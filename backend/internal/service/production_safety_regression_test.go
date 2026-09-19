package service

import (
	"encoding/json"
	"strings"
	"testing"

	"comfyui-console/internal/models"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func safetyDB(t *testing.T, extra ...any) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	base := []any{&models.Project{}, &models.Episode{}, &models.Scene{}, &models.Shot{}, &models.Dialogue{}, &models.ScriptRevision{}, &models.GenerationCandidate{}, &models.FrameCandidate{}, &models.SceneContinuity{}, &models.SceneCharacterLook{}, &models.SceneCharacterOutfit{}, &models.ShotCharacterLook{}, &models.ShotCharacterOutfit{}, &models.AudioLayer{}, &models.PromptPolicyOverride{}, &models.SharedAssetReference{}}
	base = append(base, extra...)
	if err := db.AutoMigrate(base...); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestSafeFileSegmentRejectsTraversal(t *testing.T) {
	for _, value := range []string{"../secret", `..\\secret`, "/absolute", `\\server\\share`, `C:\\absolute`, "C:relative", "a/b", `a\\b`, "bad\x00name", "line\nbreak"} {
		if _, err := safeFileSegment(value, "test"); err == nil {
			t.Fatalf("unsafe segment accepted: %q", value)
		}
	}
	if got, err := safeFileSegment("safe-file.png", "test"); err != nil || got != "safe-file.png" {
		t.Fatalf("safe segment rejected: %q %v", got, err)
	}
}

func TestScriptRevisionRestoreDoesNotRewindOtherEpisodesOrGeneration(t *testing.T) {
	db := safetyDB(t)
	project := models.Project{Title: "p", Generation: 3, PipelineStage: "videos", Scripts: `{"1":"old one","2":"keep two"}`}
	db.Create(&project)
	db.Create(&models.Episode{ProjectID: project.ID, Number: 1, Title: "one"})
	db.Create(&models.Scene{ProjectID: project.ID, EpisodeN: 1, Generation: 3, Order: 1, Title: "old"})
	svc := NewScriptRevisionService(db)
	revision, err := svc.Create(project.ID, 1, "manual")
	if err != nil {
		t.Fatal(err)
	}
	db.Model(&project).Updates(map[string]any{"generation": 9, "pipeline_stage": "finished", "scripts": `{"1":"new one","2":"newer two"}`})
	db.Where("project_id = ? AND episode_n = ?", project.ID, 1).Delete(&models.Scene{})
	if _, err := svc.Restore(project.ID, revision.ID); err != nil {
		t.Fatal(err)
	}
	db.First(&project, project.ID)
	if project.Generation != 9 || project.PipelineStage != "finished" {
		t.Fatalf("global project state rewound: %+v", project)
	}
	var restored models.Scene
	if err := db.Where("project_id = ? AND episode_n = ?", project.ID, 1).First(&restored).Error; err != nil || restored.Generation != project.Generation {
		t.Fatalf("restored scene not mapped to current generation: %+v err=%v", restored, err)
	}
	var scripts map[string]string
	_ = json.Unmarshal([]byte(project.Scripts), &scripts)
	if scripts["1"] != "old one" || scripts["2"] != "newer two" {
		t.Fatalf("scripts merged incorrectly: %+v", scripts)
	}
}

func TestScriptRevisionRestorePreservesEpisodeIdentityAndResetsDerivedState(t *testing.T) {
	db := safetyDB(t)
	project := models.Project{Title: "p", Generation: 1}
	db.Create(&project)
	episode := models.Episode{ProjectID: project.ID, Number: 1, Title: "snapshot title", TargetDuration: 90, TargetScenes: 12, Status: "approved", Summary: "old summary", NextHook: "old hook", CharacterAppearances: "old", Version: 4}
	db.Create(&episode)
	db.Create(&models.Scene{ProjectID: project.ID, EpisodeN: 1, Generation: 1, Order: 1, Title: "old"})
	svc := NewScriptRevisionService(db)
	revision, err := svc.Create(project.ID, 1, "manual")
	if err != nil {
		t.Fatal(err)
	}
	db.Model(&episode).Updates(map[string]any{"title": "current", "status": "completed", "summary": "new", "next_hook": "new", "character_appearances": "new", "version": 8})
	if _, err := svc.Restore(project.ID, revision.ID); err != nil {
		t.Fatal(err)
	}
	var got models.Episode
	db.First(&got, episode.ID)
	if got.ID != episode.ID || got.Title != "snapshot title" || got.TargetDuration != 90 || got.TargetScenes != 12 {
		t.Fatalf("episode editorial restore mismatch: %+v", got)
	}
	if got.Status != "draft" || got.Summary != "" || got.NextHook != "" || got.CharacterAppearances != "" || got.Version != 9 {
		t.Fatalf("episode derived state was not reset: %+v", got)
	}
}

func TestCandidateRetryCaptureRejectsParentThatBecameStale(t *testing.T) {
	db := safetyDB(t)
	project := models.Project{Title: "p"}
	db.Create(&project)
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1, ImageFile: "current.png"}
	db.Create(&scene)
	parent := models.GenerationCandidate{ProjectID: project.ID, EntityType: "scene", EntityID: scene.ID, MediaType: "image", TaskID: "parent", File: "current.png", IsCurrent: true, Stale: true}
	db.Create(&parent)
	_, err := NewGenerationCandidateService(db).CaptureSceneSuccess(SceneCandidateCapture{ProjectID: project.ID, SceneID: scene.ID, MediaType: "image", TaskID: "retry", File: "retry.png", ParentCandidateID: &parent.ID, RequireParentFresh: true})
	if err == nil {
		t.Fatal("stale parent retry was promoted")
	}
	db.First(&scene, scene.ID)
	if scene.ImageFile != "current.png" {
		t.Fatalf("current scene output changed: %+v", scene)
	}
}

func TestEpisodeSRTAppliesPersistedDialogueOffset(t *testing.T) {
	db := safetyDB(t)
	project := models.Project{Title: "p"}
	db.Create(&project)
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1, Duration: 4}
	db.Create(&scene)
	db.Create(&models.Dialogue{ProjectID: project.ID, SceneID: scene.ID, Order: 1, Text: "line", Offset: 1.5, Speed: 1})
	srt, count := (&ProjectService{db: db}).EpisodeSRT(&project, 1)
	if count != 1 || !strings.Contains(srt, "00:00:01,500 --> 00:00:05,500") {
		t.Fatalf("persisted timing missing from SRT: %q", srt)
	}
}

func TestSynthesizeEmptyDialogueCASStoresPreviousHash(t *testing.T) {
	db := safetyDB(t)
	project := models.Project{Title: "p"}
	db.Create(&project)
	d := models.Dialogue{ProjectID: project.ID, Text: "", Status: "synthesizing", AudioFile: "old.mp3", AudioHash: "old-hash", AudioToken: "current-token", AudioRevision: 2}
	db.Create(&d)
	ps := &ProjectService{db: db, upload: &UploadManager{}}
	if err := ps.synthesizeDialogue(&d, "stale-token", "new-hash"); err == nil {
		t.Fatal("stale CAS unexpectedly succeeded")
	}
	if err := ps.synthesizeDialogue(&d, "current-token", "new-hash"); err != nil {
		t.Fatal(err)
	}
	db.First(&d, d.ID)
	if d.PreviousAudioHash != "old-hash" || d.AudioHash != "new-hash" || d.AudioRevision != 3 {
		t.Fatalf("empty dialogue audio history mismatch: %+v", d)
	}
}

func TestApplyDialoguePreviewRejectsStaleAudio(t *testing.T) {
	db := safetyDB(t)
	project := models.Project{Title: "p"}
	db.Create(&project)
	scene := models.Scene{ProjectID: project.ID, Order: 1}
	db.Create(&scene)
	d := models.Dialogue{ProjectID: project.ID, SceneID: scene.ID, Text: "new", Voice: "v", AudioFile: "old.mp3", AudioHash: "old-hash", AudioStale: true}
	db.Create(&d)
	ps := &ProjectService{db: db}
	if _, err := ps.ApplyDialoguePreview(&project, d.ID); err == nil {
		t.Fatal("stale audio was approved")
	}
}

func TestPlaygroundPromoteRejectsTraversal(t *testing.T) {
	db := safetyDB(t, &models.PlaygroundRun{}, &models.Task{}, &models.Material{})
	task := models.Task{TaskID: "t", Status: "success", ResultFiles: `[{"filename":"secret.txt","subfolder":"../../../outside","type":"images"}]`}
	db.Create(&task)
	run := models.PlaygroundRun{TaskID: task.TaskID, Status: "success", Result: json.RawMessage(task.ResultFiles)}
	db.Create(&run)
	if _, err := NewPlaygroundService(db, nil, nil).Promote(run.ID, 0); err == nil {
		t.Fatal("traversal result was promoted")
	}
}
