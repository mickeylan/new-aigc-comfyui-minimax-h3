package database

import (
	"testing"

	"comfyui-console/internal/models"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestMigrateLegacyDialogueAudioStateMarksOnlyUnverifiableReadyRows(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Dialogue{}); err != nil {
		t.Fatal(err)
	}
	legacy := models.Dialogue{Status: "ready", AudioFile: "legacy.mp3"}
	verified := models.Dialogue{Status: "ready", AudioFile: "verified.mp3", AudioHash: "hash"}
	pending := models.Dialogue{Status: "pending"}
	db.Create(&legacy)
	db.Create(&verified)
	db.Create(&pending)

	if err := migrateLegacyDialogueAudioState(db); err != nil {
		t.Fatal(err)
	}
	db.First(&legacy, legacy.ID)
	db.First(&verified, verified.ID)
	db.First(&pending, pending.ID)
	if legacy.Status != "pending" || !legacy.AudioStale || legacy.AudioFile != "legacy.mp3" {
		t.Fatalf("legacy row not safely migrated: %+v", legacy)
	}
	if verified.Status != "ready" || verified.AudioStale {
		t.Fatalf("verified row changed: %+v", verified)
	}
	if pending.AudioStale {
		t.Fatalf("existing pending row changed: %+v", pending)
	}
}
