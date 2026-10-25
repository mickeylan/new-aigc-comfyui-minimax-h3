package service

import (
	"strings"
	"testing"

	"comfyui-console/internal/models"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newMusicCandidateTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Project{}, &models.MusicCueSheet{}, &models.MusicCandidate{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func seedApprovedMusicCue(t *testing.T, db *gorm.DB, projectID uint, hash string) models.MusicCueSheet {
	t.Helper()
	cue := models.MusicCueSheet{ProjectID: projectID, EpisodeN: 1, Kind: "emotion", StartTime: 0, EndTime: 12, Intensity: "medium", ReviewStatus: "approved", SourceVersionHash: hash, Enabled: true}
	if err := db.Create(&cue).Error; err != nil {
		t.Fatal(err)
	}
	return cue
}

func seedMusicCandidate(t *testing.T, db *gorm.DB, projectID, cueID uint, hash, fileHash string) models.MusicCandidate {
	t.Helper()
	row := models.MusicCandidate{ProjectID: projectID, MusicCueSheetID: cueID, SourceVersionHash: hash, Provider: "future_adapter", Model: "unknown", File: "music/cue.wav", FileSHA256: fileHash, FileSize: 100, Duration: 12, ReviewStatus: "pending"}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	return row
}

func TestMusicCandidateListStalesPreDemucsCandidates(t *testing.T) {
	db := newMusicCandidateTestDB(t)
	project := models.Project{Title: "music"}
	db.Create(&project)
	cue := seedApprovedMusicCue(t, db, project.ID, "source")
	row := models.MusicCandidate{ProjectID: project.ID, MusicCueSheetID: cue.ID, SourceVersionHash: cue.SourceVersionHash, Provider: "minimax_music_3_comfyui_oriental_v2", FileSHA256: strings.Repeat("a", 64), FileSize: 10, File: "old.flac"}
	db.Create(&row)
	rows, err := NewMusicCandidateService(db).List(project.ID, cue.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || !rows[0].Stale || rows[0].IsCurrent || !strings.Contains(rows[0].StaleReason, "空lyrics加Vocals分离v4") {
		t.Fatalf("legacy candidate was not invalidated: %+v", rows)
	}
}

func TestMusicCandidateRequiresMatchingApprovedCueAndPlaybackEvidence(t *testing.T) {
	db := newMusicCandidateTestDB(t)
	project := models.Project{Title: "p"}
	db.Create(&project)
	cue := seedApprovedMusicCue(t, db, project.ID, "source-a")
	row := seedMusicCandidate(t, db, project.ID, cue.ID, "source-a", "file-a")
	svc := NewMusicCandidateService(db)
	if _, err := svc.Review(project.ID, cue.ID, row.ID, "accepted", "", "wrong", "file-a"); err == nil {
		t.Fatal("mismatched source hash was accepted")
	}
	accepted, err := svc.Review(project.ID, cue.ID, row.ID, "accepted", "", "source-a", "file-a")
	if err != nil {
		t.Fatal(err)
	}
	if accepted.ReviewStatus != "accepted" || accepted.ReviewConfirmation != musicCandidateReviewConfirmationV1 {
		t.Fatalf("review evidence missing: %+v", accepted)
	}
}

func TestMusicCandidateSelectsOneCurrentAndProtectsItFromDelete(t *testing.T) {
	db := newMusicCandidateTestDB(t)
	project := models.Project{Title: "p"}
	db.Create(&project)
	cue := seedApprovedMusicCue(t, db, project.ID, "source-a")
	first := seedMusicCandidate(t, db, project.ID, cue.ID, "source-a", "file-a")
	second := seedMusicCandidate(t, db, project.ID, cue.ID, "source-a", "file-b")
	svc := NewMusicCandidateService(db)
	for _, row := range []models.MusicCandidate{first, second} {
		if _, err := svc.Review(project.ID, cue.ID, row.ID, "accepted", "", "source-a", row.FileSHA256); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.Select(project.ID, cue.ID, first.ID, "source-a", "file-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Select(project.ID, cue.ID, second.ID, "source-a", "file-b"); err != nil {
		t.Fatal(err)
	}
	var current int64
	if err := db.Model(&models.MusicCandidate{}).Where("music_cue_sheet_id = ? AND is_current = ?", cue.ID, true).Count(&current).Error; err != nil {
		t.Fatal(err)
	}
	if current != 1 {
		t.Fatalf("expected one current candidate, got %d", current)
	}
	if err := svc.Delete(project.ID, cue.ID, second.ID); err == nil {
		t.Fatal("current candidate was deleted")
	}
	if err := svc.Delete(project.ID, cue.ID, first.ID); err != nil {
		t.Fatal(err)
	}
}

func TestEditingMusicCueInvalidatesCandidates(t *testing.T) {
	db := newMusicCandidateTestDB(t)
	project := models.Project{Title: "p"}
	db.Create(&project)
	cue := seedApprovedMusicCue(t, db, project.ID, "source-a")
	row := seedMusicCandidate(t, db, project.ID, cue.ID, "source-a", "file-a")
	svc := NewMusicCueService(db)
	_, err := svc.Update(project.ID, cue.ID, MusicCueSheetInput{EpisodeN: 1, Kind: "emotion", StartTime: 0, EndTime: 10, Intensity: "soft", CaptionJSON: `{"global_metadata":"x","vocal_details":"Fully instrumental","arrangement":"intro then outro"}`, Source: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.First(&row, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !row.Stale || row.IsCurrent {
		t.Fatalf("candidate was not invalidated: %+v", row)
	}
}
