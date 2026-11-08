package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
)

type inputRecoveryFixture struct {
	svc       *TaskService
	character *models.Character
	scene     *models.Scene
	current   string
	missing   string
	task      *models.Task
}

func newInputRecoveryFixture(t *testing.T) *inputRecoveryFixture {
	t.Helper()
	db := newTestDB(t)
	if err := db.AutoMigrate(&models.Character{}, &models.Asset{}, &models.CharacterLook{}, &models.AssetVariant{}, &models.Scene{}, &models.Task{}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	assetRoot := filepath.Join(root, "data", "assets")
	inputDir := filepath.Join(assetRoot, "1")
	if err := os.MkdirAll(inputDir, 0o755); err != nil {
		t.Fatal(err)
	}
	character := models.Character{ProjectID: 1, Name: "上官若彤"}
	if err := db.Create(&character).Error; err != nil {
		t.Fatal(err)
	}
	suffix := strconv.FormatUint(uint64(character.ID), 10)
	missing := "char_sheet_" + suffix + "_1793440455009263200.png"
	current := "char_sheet_" + suffix + "_1790009704714276500.png"
	if err := os.WriteFile(filepath.Join(inputDir, current), []byte("authoritative-sheet-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	character.Sheet = current
	if err := db.Save(&character).Error; err != nil {
		t.Fatal(err)
	}
	// The sheet was produced by a Krea2 task; its original output is still in ComfyUI output.
	task := models.Task{TaskID: "20261005-210050-sheet", TemplateName: "krea2_character_sheet", Status: "success",
		ResultFiles: `[{"filename":"character_sheet_20261005-210050_00001_.png","subfolder":"krea2","type":"images"}]`}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.AssetVariant{ProjectID: 1, EntityType: VariantCharacterSheet, EntityID: character.ID, File: missing, Selected: true, Provenance: "krea2_task:" + task.TaskID}).Error; err != nil {
		t.Fatal(err)
	}
	refs, _ := json.Marshal([]SceneReferenceSelection{{SourceType: "character", SourceID: character.ID, Variant: "sheet", AssetVariantID: 1, UseKrea2: true, UseH3: true}})
	scene := models.Scene{ProjectID: 1, EpisodeN: 1, Order: 1, ReferenceImagesJSON: string(refs)}
	if err := db.Create(&scene).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewTaskService(&config.Config{Comfy: config.ComfyConfig{ComfyDir: filepath.Join(root, "ComfyUI")}, Storage: config.StorageConfig{DataDir: filepath.Join(root, "data")}}, db, nil, nil, NewHub(), NewRemoteExec(config.RemoteConfig{}))
	return &inputRecoveryFixture{svc, &character, &scene, current, missing, &task}
}

func TestEnsureProjectInputRedownloadsRecordedComfyOutput(t *testing.T) {
	f := newInputRecoveryFixture(t)
	requested := []string{}
	f.svc.outputDownloader = func(task *models.Task) ([]byte, error) {
		file, _ := resultImageOf(task)
		requested = append(requested, file)
		return []byte("krea2-original-sheet"), nil
	}
	if _, err := f.svc.ensureProjectInputFile(1, f.missing); err != nil {
		t.Fatal(err)
	}
	if len(requested) != 1 || requested[0] != "krea2/character_sheet_20261005-210050_00001_.png" {
		t.Fatalf("recorded output path not used: %v", requested)
	}
	restored, err := os.ReadFile(filepath.Join(f.svc.cfg.AssetsRoot(), "1", f.missing))
	if err != nil || string(restored) != "krea2-original-sheet" {
		t.Fatalf("missing file was not restored from recorded output: %v %q", err, restored)
	}
	var got models.Scene
	if err := f.svc.db.First(&got, f.scene.ID).Error; err != nil {
		t.Fatal(err)
	}
	var refs []SceneReferenceSelection
	if err := json.Unmarshal([]byte(got.ReferenceImagesJSON), &refs); err != nil {
		t.Fatal(err)
	}
	// The restored bytes are that variant's own artwork, so its selection stays valid.
	if len(refs) != 1 || refs[0].AssetVariantID != 1 || refs[0].SourceID != f.character.ID {
		t.Fatalf("exact-output restore should keep the variant selection: %+v", refs)
	}
	if _, err := os.Stat(filepath.Join(f.svc.cfg.AssetsRoot(), "1", f.current)); err != nil {
		t.Fatal("existing authoritative file was disturbed")
	}
}

func TestEnsureProjectInputFallsBackToCurrentAuthoritativeArtWithoutTask(t *testing.T) {
	f := newInputRecoveryFixture(t)
	if err := f.svc.db.Where("provenance = ?", "krea2_task:"+f.task.TaskID).Delete(&models.AssetVariant{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.svc.db.Create(&models.AssetVariant{ProjectID: 1, EntityType: VariantCharacterSheet, EntityID: f.character.ID, File: f.missing}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.ensureProjectInputFile(1, f.missing); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(filepath.Join(f.svc.cfg.AssetsRoot(), "1", f.missing))
	if err != nil || string(restored) != "authoritative-sheet-bytes" {
		t.Fatalf("authoritative copy not used: %v %q", err, restored)
	}
}

func TestEnsureProjectInputNeverSubstitutesHistoricalVariantSilently(t *testing.T) {
	f := newInputRecoveryFixture(t)
	inputDir := filepath.Join(f.svc.cfg.AssetsRoot(), "1")
	if err := os.Remove(filepath.Join(inputDir, f.current)); err != nil {
		t.Fatal(err)
	}
	f.svc.outputDownloader = func(*models.Task) ([]byte, error) {
		return nil, os.ErrNotExist
	}
	_, err := f.svc.ensureProjectInputFile(1, f.missing)
	if err == nil || !strings.Contains(err.Error(), "重新生成") {
		t.Fatalf("silent downgrade allowed: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(inputDir, f.missing)); !os.IsNotExist(statErr) {
		t.Fatal("missing file was created from a historical variant")
	}
}

func TestEnsureProjectInputReportsUnavailableRecordedOutput(t *testing.T) {
	f := newInputRecoveryFixture(t)
	// Task row is gone: recovery must say so instead of scanning local folders.
	if err := f.svc.db.Where("task_id = ?", f.task.TaskID).Delete(&models.Task{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(f.svc.cfg.AssetsRoot(), "1", f.current)); err != nil {
		t.Fatal(err)
	}
	_, err := f.svc.ensureProjectInputFile(1, f.missing)
	if err == nil || !strings.Contains(err.Error(), "任务记录已不存在") {
		t.Fatalf("missing diagnostic: %v", err)
	}
}

func TestEnsureProjectInputExplainsMissingOrDisconnectedInputRoot(t *testing.T) {
	f := newInputRecoveryFixture(t)
	if err := os.RemoveAll(f.svc.cfg.AssetsRoot()); err != nil {
		t.Fatal(err)
	}
	_, err := f.svc.ensureProjectInputFile(1, f.missing)
	if err == nil || !strings.Contains(err.Error(), "目录不可访问") || !strings.Contains(err.Error(), "asset_dir") {
		t.Fatalf("unclear root diagnostic: %v", err)
	}
}

func TestEnsureProjectInputRejectsUnknownAndCrossProjectFiles(t *testing.T) {
	f := newInputRecoveryFixture(t)
	if _, err := f.svc.ensureProjectInputFile(1, "unknown_sheet.png"); err == nil {
		t.Fatal("unknown file was accepted")
	}
	other := models.Character{ProjectID: 2, Name: "其他项目角色", Sheet: "char_sheet_77_1.png"}
	if err := f.svc.db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.ensureProjectInputFile(1, "char_sheet_77_1.png"); err == nil || !strings.Contains(err.Error(), "不存在") {
		t.Fatalf("cross-project identity leak: %v", err)
	}
}
