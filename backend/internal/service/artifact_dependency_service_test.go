package service

import (
	"testing"

	"comfyui-console/internal/models"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newArtifactDependencyTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.ArtifactDependency{}, &models.ArtifactInvalidationEvent{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestArtifactDependencyGraphIsNoOpWhenDisabled(t *testing.T) {
	svc := NewArtifactDependencyService(newArtifactDependencyTestDB(t), false)
	if err := svc.LinkMusicCandidate(1, 10, 20, "v1"); err != nil {
		t.Fatal(err)
	}
	items, err := svc.Affected(1, ArtifactRef{Kind: "music_cue", ID: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("disabled graph changed behavior: %+v", items)
	}
}

func TestArtifactDependencyGraphCanonicalizesKindsAndRejectsUnknownKinds(t *testing.T) {
	db := newArtifactDependencyTestDB(t)
	svc := NewArtifactDependencyService(db, true)
	if err := svc.Link(1, ArtifactRef{Kind: " music_cue ", ID: 1}, "v1", ArtifactRef{Kind: "music_candidate", ID: 2}); err != nil {
		t.Fatal(err)
	}
	var row models.ArtifactDependency
	if err := db.First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.UpstreamKind != ArtifactKindMusicCue {
		t.Fatalf("kind was not canonicalized: %q", row.UpstreamKind)
	}
	if err := svc.Link(1, ArtifactRef{Kind: "unknown", ID: 3}, "", ArtifactRef{Kind: ArtifactKindMusicCandidate, ID: 4}); err == nil {
		t.Fatal("unknown artifact kind accepted")
	}
}

func TestArtifactDependencyGraphTracksOnlyAffectedDescendants(t *testing.T) {
	db := newArtifactDependencyTestDB(t)
	svc := NewArtifactDependencyService(db, true)
	if err := svc.LinkMusicCandidate(1, 10, 20, "hash-a"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Link(1, ArtifactRef{Kind: "music_candidate", ID: 20}, "", ArtifactRef{Kind: "audio_layer", ID: 30}); err != nil {
		t.Fatal(err)
	}
	if err := svc.LinkMusicCandidate(1, 11, 21, "hash-b"); err != nil {
		t.Fatal(err)
	}
	affected, err := svc.RecordInvalidation(nil, 1, ArtifactRef{Kind: "music_cue", ID: 10}, "cue changed")
	if err != nil {
		t.Fatal(err)
	}
	if len(affected) != 2 || affected[0] != (ArtifactRef{Kind: "audio_layer", ID: 30}) || affected[1] != (ArtifactRef{Kind: "music_candidate", ID: 20}) {
		t.Fatalf("unexpected affected set: %+v", affected)
	}
	var count int64
	if err := db.Model(&models.ArtifactInvalidationEvent{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("expected 2 audit events, got %d", count)
	}
	if err := svc.Link(1, ArtifactRef{Kind: "audio_layer", ID: 30}, "", ArtifactRef{Kind: "music_cue", ID: 10}); err == nil {
		t.Fatal("dependency cycle was accepted")
	}
}
