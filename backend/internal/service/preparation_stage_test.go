package service

import (
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"comfyui-console/internal/models"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestPreparationDurableSuccessAndOnlyFailedStepRetried(t *testing.T) {
	path := filepath.Join(t.TempDir(), "preparation.db")
	open := func() *gorm.DB {
		db, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		if err != nil {
			t.Fatal(err)
		}
		return db
	}
	db := open()
	if err := db.AutoMigrate(&models.PreparationStage{}); err != nil {
		t.Fatal(err)
	}
	var good, bad int
	runGood := func() (string, error) { good++; return "reviewable draft", nil }
	runBad := func() (string, error) {
		bad++
		if bad == 1 {
			return "", errors.New("fixture unavailable")
		}
		return "repaired draft", nil
	}
	if _, err := preparationStep(db, 1, 2, "video_prompt", "v1", "system", "input", runGood); err != nil {
		t.Fatal(err)
	}
	if _, err := preparationStep(db, 1, 2, "plot_anchor_repair", "v1", "system", "repair", runBad); err == nil {
		t.Fatal("failed step must remain failed")
	}
	sqlDB, _ := db.DB()
	sqlDB.Close()
	// Re-open the actual on-disk database: no in-process output cache is involved.
	db = open()
	defer func() { conn, _ := db.DB(); conn.Close() }()
	result, err := preparationStep(db, 1, 2, "video_prompt", "v1", "system", "input", runGood)
	if err != nil || result != "reviewable draft" || good != 1 {
		t.Fatalf("reuse result=%q calls=%d err=%v", result, good, err)
	}
	if _, err := preparationStep(db, 1, 2, "plot_anchor_repair", "v1", "system", "repair", runBad); err != nil {
		t.Fatal(err)
	}
	var rows []models.PreparationStage
	db.Order("stage").Find(&rows)
	if len(rows) != 2 || bad != 2 {
		t.Fatalf("rows=%+v good=%d bad=%d", rows, good, bad)
	}
	for _, row := range rows {
		if row.Status != "success" {
			t.Fatalf("not successful: %+v", row)
		}
		if row.Stage == "video_prompt" && (row.ReuseCount != 1 || row.Attempts != 1) {
			t.Fatalf("repeated successful call: %+v", row)
		}
		if row.Stage == "plot_anchor_repair" && (row.Attempts != 2 || row.Error != "") {
			t.Fatalf("failed retry evidence: %+v", row)
		}
	}
	if _, err := preparationStep(db, 1, 2, "video_prompt", "v2", "system", "input", runGood); err != nil || good != 2 {
		t.Fatal("new version must not reuse stale success")
	}
}

func TestPreparationConcurrentSameStepCallsOnce(t *testing.T) {
	db := newTestDB(t)
	conn, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	// An SQLite :memory: database exists per physical connection.
	conn.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&models.PreparationStage{}); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := preparationStep(db, 1, 2, "translation_en_chunk_1", "v1", "system", "source", func() (string, error) { calls.Add(1); return "translated", nil })
			if err != nil || out != "translated" {
				t.Errorf("out=%s err=%v", out, err)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("calls=%d", calls.Load())
	}
}

func TestPreparationTranslationHTTPPersistsButNeverAdopts(t *testing.T) {
	f := newTranslationHTTPFixture(t)
	if err := f.db.AutoMigrate(&models.PreparationStage{}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		w := translationRequest(f.svc, "POST", f.p.ID, f.sc.ID, "/translate", `{"language":"en"}`)
		if w.Code != 200 {
			t.Fatalf("status=%d %s", w.Code, w.Body.String())
		}
	}
	if f.calls.Load() != 1 {
		t.Fatalf("successful translation called upstream %d times", f.calls.Load())
	}
	var sc models.Scene
	f.db.First(&sc, f.sc.ID)
	if sc.VideoFullPrompt != "old reviewed prompt" || sc.VideoPromptMasterZH != translationMaster || sc.VideoFile != "old.mp4" {
		t.Fatalf("cache adopted draft or changed media: %+v", sc)
	}
	var row models.PreparationStage
	f.db.First(&row)
	if row.Stage != "translation_en_chunk_1" || row.Status != "success" || row.ReuseCount != 1 {
		t.Fatalf("checkpoint=%+v", row)
	}
}
