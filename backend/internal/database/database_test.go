package database

import (
	"path/filepath"
	"testing"

	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestInitConfiguresSQLiteForConcurrentServiceWorkload(t *testing.T) {
	cfg := config.Default()
	cfg.Storage.DBPath = filepath.Join(t.TempDir(), "console.db")
	db, err := Init(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
	})
	var journalMode, synchronous string
	var busyTimeout, foreignKeys int
	if err := db.Raw("PRAGMA journal_mode").Scan(&journalMode).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Raw("PRAGMA synchronous").Scan(&synchronous).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Raw("PRAGMA busy_timeout").Scan(&busyTimeout).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Raw("PRAGMA foreign_keys").Scan(&foreignKeys).Error; err != nil {
		t.Fatal(err)
	}
	if journalMode != "wal" || synchronous != "1" || busyTimeout != 10000 || foreignKeys != 1 {
		t.Fatalf("unexpected pragmas: journal=%q synchronous=%q busy=%d foreign_keys=%d", journalMode, synchronous, busyTimeout, foreignKeys)
	}
	sqlDB, _ := db.DB()
	if sqlDB.Stats().MaxOpenConnections != 4 {
		t.Fatalf("max open connections=%d", sqlDB.Stats().MaxOpenConnections)
	}
}

func TestInitCreatesOperationalPollingIndexes(t *testing.T) {
	cfg := config.Default()
	cfg.Storage.DBPath = filepath.Join(t.TempDir(), "console.db")
	db, err := Init(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB, _ := db.DB(); _ = sqlDB.Close() })
	for model, indexes := range map[any][]string{
		&models.Task{}:  {"idx_task_status_updated", "idx_task_status_template"},
		&models.Scene{}: {"idx_scenes_status", "idx_scenes_video_task_id"},
	} {
		for _, index := range indexes {
			if !db.Migrator().HasIndex(model, index) {
				t.Fatalf("missing operational index %s", index)
			}
		}
	}
}

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
