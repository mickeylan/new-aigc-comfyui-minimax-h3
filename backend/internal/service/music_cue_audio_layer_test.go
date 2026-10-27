package service

import (
	"fmt"
	"testing"

	"comfyui-console/internal/models"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newMusicAudioLayerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	musicAudioLayerDBCounter++
	dsn := fmt.Sprintf("file:music_audio_layer_%d?mode=memory&cache=shared", musicAudioLayerDBCounter)
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Project{}, &models.MusicCueSheet{}, &models.MusicCandidate{}, &models.AudioLayer{}); err != nil {
		t.Fatal(err)
	}
	return db
}

var musicAudioLayerDBCounter int

func seedAcceptedMusicCandidate(t *testing.T, db *gorm.DB, projectID, cueID uint, hash, file, fileHash string) models.MusicCandidate {
	t.Helper()
	row := models.MusicCandidate{
		ProjectID: projectID, MusicCueSheetID: cueID, SourceVersionHash: hash,
		Provider: "minimax_music_3_comfyui", Model: "MiniMax Music 3",
		File: file, FileSHA256: fileHash, FileSize: 1024, Duration: 12,
		ReviewStatus: "accepted", ReviewConfirmation: musicCandidateReviewConfirmationV1,
		ReviewSourceHash: hash, ReviewFileSHA256: fileHash,
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	return row
}

func TestProjectLibraryCandidateStaysReusableUntilExplicitEpisodePlacement(t *testing.T) {
	db := newMusicAudioLayerTestDB(t)
	project := models.Project{Title: "p"}
	db.Create(&project)
	cue := models.MusicCueSheet{ProjectID: project.ID, Scope: "library", ThemeCode: "danger", Name: "危机主题", EpisodeN: 0, Kind: "action", StartTime: 0, EndTime: 90, Duration: 90, Enabled: true, ReviewStatus: "approved", SourceVersionHash: "library-source"}
	if err := db.Create(&cue).Error; err != nil {
		t.Fatal(err)
	}
	candidate := seedAcceptedMusicCandidate(t, db, project.ID, cue.ID, "library-source", "danger.flac", "danger-file")
	selection, err := NewMusicCandidateService(db).Select(project.ID, cue.ID, candidate.ID, "library-source", "danger-file")
	if err != nil {
		t.Fatal(err)
	}
	if selection.Layer != nil {
		t.Fatalf("library adoption created timeline layer: %+v", selection.Layer)
	}
	layer, err := placeMusicLibraryTrack(db, project.ID, cue.ID, 3, 12, 48, 0.3, 1, 2, true)
	if err != nil {
		t.Fatal(err)
	}
	if layer.EpisodeN != 3 || layer.Source != audioLayerSourceMusicLibrary || !layer.Loop || layer.StartTime != 12 || layer.EndTime != 48 {
		t.Fatalf("placement=%+v", layer)
	}
}

func TestAdoptingMusicCandidateCreatesBGMAudioLayer(t *testing.T) {
	db := newMusicAudioLayerTestDB(t)
	project := models.Project{Title: "p"}
	db.Create(&project)
	cue := seedApprovedMusicCue(t, db, project.ID, "source-a")
	candidate := seedAcceptedMusicCandidate(t, db, project.ID, cue.ID, "source-a", "music3_cue_1.flac", "file-a")

	svc := NewMusicCandidateService(db)
	selection, err := svc.Select(project.ID, cue.ID, candidate.ID, "source-a", "file-a")
	if err != nil {
		t.Fatal(err)
	}
	if selection.Layer == nil {
		t.Fatal("adopted candidate did not create a BGM audio layer")
	}
	layer := selection.Layer
	if layer.Kind != "bgm" || layer.Source != audioLayerSourceMusicCue {
		t.Fatalf("unexpected derived layer: %+v", layer)
	}
	if layer.File != "music3_cue_1.flac" || layer.StartTime != cue.StartTime || layer.EndTime != cue.EndTime {
		t.Fatalf("derived layer does not follow the cue: %+v", layer)
	}
	if layer.MusicCueSheetID == nil || *layer.MusicCueSheetID != cue.ID {
		t.Fatalf("derived layer lost the cue binding: %+v", layer)
	}
	if layer.Status != "ready" {
		t.Fatalf("derived layer should be ready for mixing: %+v", layer)
	}

	var count int64
	db.Model(&models.AudioLayer{}).Where("project_id = ? AND source = ?", project.ID, audioLayerSourceMusicCue).Count(&count)
	if count != 1 {
		t.Fatalf("expected exactly one derived layer, got %d", count)
	}
}

func TestAdoptingAnotherMusicCandidateReplacesTheDerivedLayer(t *testing.T) {
	db := newMusicAudioLayerTestDB(t)
	project := models.Project{Title: "p"}
	db.Create(&project)
	cue := seedApprovedMusicCue(t, db, project.ID, "source-a")
	first := seedAcceptedMusicCandidate(t, db, project.ID, cue.ID, "source-a", "first.flac", "file-a")
	second := seedAcceptedMusicCandidate(t, db, project.ID, cue.ID, "source-a", "second.flac", "file-b")

	svc := NewMusicCandidateService(db)
	if _, err := svc.Select(project.ID, cue.ID, first.ID, "source-a", "file-a"); err != nil {
		t.Fatal(err)
	}
	selection, err := svc.Select(project.ID, cue.ID, second.ID, "source-a", "file-b")
	if err != nil {
		t.Fatal(err)
	}
	var layers []models.AudioLayer
	db.Where("project_id = ? AND source = ?", project.ID, audioLayerSourceMusicCue).Find(&layers)
	if len(layers) != 1 {
		t.Fatalf("expected the derived layer to be reused, got %d layers", len(layers))
	}
	if layers[0].File != "second.flac" {
		t.Fatalf("derived layer was not replaced: %+v", layers[0])
	}
	if selection.Layer == nil || selection.Layer.ID != layers[0].ID {
		t.Fatalf("selection did not report the derived layer: %+v", selection.Layer)
	}
}

func TestUnselectingMusicCandidateRemovesTheDerivedLayer(t *testing.T) {
	db := newMusicAudioLayerTestDB(t)
	project := models.Project{Title: "p"}
	db.Create(&project)
	cue := seedApprovedMusicCue(t, db, project.ID, "source-a")
	candidate := seedAcceptedMusicCandidate(t, db, project.ID, cue.ID, "source-a", "take.flac", "file-a")

	svc := NewMusicCandidateService(db)
	if _, err := svc.Select(project.ID, cue.ID, candidate.ID, "source-a", "file-a"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Unselect(project.ID, cue.ID, candidate.ID); err != nil {
		t.Fatal(err)
	}
	var count int64
	db.Model(&models.AudioLayer{}).Where("project_id = ? AND source = ?", project.ID, audioLayerSourceMusicCue).Count(&count)
	if count != 0 {
		t.Fatalf("derived layer survived the unselect: %d", count)
	}
	var row models.MusicCandidate
	db.First(&row, candidate.ID)
	if row.IsCurrent {
		t.Fatal("candidate is still marked as adopted")
	}
}

func TestDeletingMusicCueRemovesItsDerivedLayer(t *testing.T) {
	db := newMusicAudioLayerTestDB(t)
	project := models.Project{Title: "p"}
	db.Create(&project)
	cue := seedApprovedMusicCue(t, db, project.ID, "source-a")
	candidate := seedAcceptedMusicCandidate(t, db, project.ID, cue.ID, "source-a", "take.flac", "file-a")

	candidates := NewMusicCandidateService(db)
	if _, err := candidates.Select(project.ID, cue.ID, candidate.ID, "source-a", "file-a"); err != nil {
		t.Fatal(err)
	}
	cues := NewMusicCueService(db)
	if err := cues.Delete(project.ID, cue.ID); err != nil {
		t.Fatal(err)
	}
	var count int64
	db.Model(&models.AudioLayer{}).Where("project_id = ? AND source = ?", project.ID, audioLayerSourceMusicCue).Count(&count)
	if count != 0 {
		t.Fatalf("derived layer survived the cue deletion: %d", count)
	}
}

func TestStaleMusicCueTakesItsDerivedLayerOutOfTheMix(t *testing.T) {
	db := newMusicAudioLayerTestDB(t)
	project := models.Project{Title: "p"}
	db.Create(&project)
	cue := seedApprovedMusicCue(t, db, project.ID, "source-a")
	candidate := seedAcceptedMusicCandidate(t, db, project.ID, cue.ID, "source-a", "take.flac", "file-a")

	candidates := NewMusicCandidateService(db)
	if _, err := candidates.Select(project.ID, cue.ID, candidate.ID, "source-a", "file-a"); err != nil {
		t.Fatal(err)
	}
	cues := NewMusicCueService(db)
	if err := cues.MarkEpisodeStale(project.ID, 1, "剧本已修改"); err != nil {
		t.Fatal(err)
	}
	var layer models.AudioLayer
	db.Where("project_id = ? AND source = ?", project.ID, audioLayerSourceMusicCue).First(&layer)
	if !layer.Stale {
		t.Fatalf("stale cue left its derived layer in the mix: %+v", layer)
	}
	if layer.Status == "ready" {
		t.Fatalf("stale derived layer is still marked ready: %+v", layer)
	}
}

func TestAdoptingMusicCandidateReportsOverlappingManualBGMLayer(t *testing.T) {
	db := newMusicAudioLayerTestDB(t)
	project := models.Project{Title: "p"}
	db.Create(&project)
	cue := seedApprovedMusicCue(t, db, project.ID, "source-a")
	candidate := seedAcceptedMusicCandidate(t, db, project.ID, cue.ID, "source-a", "take.flac", "file-a")
	manual := models.AudioLayer{ProjectID: project.ID, EpisodeN: 1, Kind: "bgm", Name: "整集氛围底乐", File: "bed.wav", StartTime: 0, EndTime: 180, Volume: 0.4, Status: "ready", Source: audioLayerSourceManual}
	if err := db.Create(&manual).Error; err != nil {
		t.Fatal(err)
	}

	svc := NewMusicCandidateService(db)
	selection, err := svc.Select(project.ID, cue.ID, candidate.ID, "source-a", "file-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(selection.OverlappingBGMLayers) != 1 || selection.OverlappingBGMLayers[0].ID != manual.ID {
		t.Fatalf("manual BGM overlap was not reported: %+v", selection.OverlappingBGMLayers)
	}
	var kept models.AudioLayer
	db.First(&kept, manual.ID)
	if kept.Muted || kept.Volume != 0.4 || kept.Status != "ready" {
		t.Fatalf("manual BGM layer must stay untouched: %+v", kept)
	}
}

func TestManualAudioLayersCannotClaimDerivedSource(t *testing.T) {
	db := newMusicAudioLayerTestDB(t)
	project := models.Project{Title: "p"}
	db.Create(&project)
	layer, err := NewAudioLayerService(db).Create(project.ID, AudioLayerInput{EpisodeN: 1, Kind: "bgm", Name: "手工", File: "manual.flac", StartTime: 0, EndTime: 10})
	if err != nil {
		t.Fatal(err)
	}
	if layer.Source != audioLayerSourceManual {
		t.Fatalf("client authored layer must be manual, got %q", layer.Source)
	}
	if layer.MusicCueSheetID != nil || layer.MusicCandidateID != nil {
		t.Fatalf("client authored layer must not bind to a cue: %+v", layer)
	}
}
