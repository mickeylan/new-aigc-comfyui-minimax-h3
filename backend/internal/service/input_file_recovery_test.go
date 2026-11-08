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

func newInputRecoveryService(t *testing.T) (*TaskService, *models.Character, *models.Scene, string, string) {
	t.Helper()
	db := newTestDB(t)
	if err := db.AutoMigrate(&models.Character{}, &models.Asset{}, &models.CharacterLook{}, &models.AssetVariant{}, &models.Scene{}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	inputDir := filepath.Join(root, "input", "1")
	if err := os.MkdirAll(inputDir, 0o755); err != nil {
		t.Fatal(err)
	}
	character := models.Character{ProjectID: 1, Name: "上官若彤"}
	if err := db.Create(&character).Error; err != nil {
		t.Fatal(err)
	}
	missing := "char_sheet_" + strconv.FormatUint(uint64(character.ID), 10) + "_1793440455009263200.png"
	current := "char_sheet_" + strconv.FormatUint(uint64(character.ID), 10) + "_1790009704714276500.png"
	if err := os.WriteFile(filepath.Join(inputDir, current), []byte("authoritative-sheet-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	character.Sheet = current
	if err := db.Save(&character).Error; err != nil {
		t.Fatal(err)
	}
	variant := models.AssetVariant{ProjectID: 1, EntityType: VariantCharacterSheet, EntityID: character.ID, File: missing}
	if err := db.Create(&variant).Error; err != nil {
		t.Fatal(err)
	}
	refs, _ := json.Marshal([]SceneReferenceSelection{{SourceType: "character", SourceID: character.ID, Variant: "sheet", AssetVariantID: variant.ID, UseKrea2: true, UseH3: true}})
	scene := models.Scene{ProjectID: 1, EpisodeN: 1, Order: 1, ReferenceImagesJSON: string(refs)}
	if err := db.Create(&scene).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewTaskService(&config.Config{Comfy: config.ComfyConfig{ComfyDir: root}}, db, nil, nil, NewHub(), NewRemoteExec(config.RemoteConfig{}))
	return svc, &character, &scene, current, missing
}

func TestEnsureProjectInputRestoresDeletedSheetFromAuthoritativeArt(t *testing.T) {
	svc, character, scene, current, missing := newInputRecoveryService(t)
	if _, err := svc.ensureProjectInputFile(1, missing); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(filepath.Join(svc.cfg.Comfy.ComfyDir, "input", "1", missing))
	if err != nil {
		t.Fatal(err)
	}
	original, _ := os.ReadFile(filepath.Join(svc.cfg.Comfy.ComfyDir, "input", "1", current))
	if string(restored) != string(original) {
		t.Fatal("restored bytes differ from authoritative artwork")
	}
	var got models.Scene
	if err := svc.db.First(&got, scene.ID).Error; err != nil {
		t.Fatal(err)
	}
	var refs []SceneReferenceSelection
	if err := json.Unmarshal([]byte(got.ReferenceImagesJSON), &refs); err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0].AssetVariantID != 0 || refs[0].SourceID != character.ID {
		t.Fatalf("stale variant selection not repaired: %+v", refs)
	}
}

func TestEnsureProjectInputNeverSubstitutesHistoricalVariantSilently(t *testing.T) {
	svc, character, _, current, missing := newInputRecoveryService(t)
	inputDir := filepath.Join(svc.cfg.Comfy.ComfyDir, "input", "1")
	if err := os.Remove(filepath.Join(inputDir, current)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inputDir, "char_sheet_"+strconv.FormatUint(uint64(character.ID), 10)+"_1700000000000000000.png"), []byte("older-look"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := svc.db.Create(&models.AssetVariant{ProjectID: 1, EntityType: VariantCharacterSheet, EntityID: character.ID, File: "char_sheet_" + strconv.FormatUint(uint64(character.ID), 10) + "_1700000000000000000.png"}).Error; err != nil {
		t.Fatal(err)
	}
	_, err := svc.ensureProjectInputFile(1, missing)
	if err == nil || !strings.Contains(err.Error(), "历史版本仍存在") || !strings.Contains(err.Error(), "不会自动替换") {
		t.Fatalf("silent downgrade allowed: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(inputDir, missing)); !os.IsNotExist(statErr) {
		t.Fatal("missing file was created from a historical variant")
	}
}

func TestEnsureProjectInputCopiesIdenticalFileFromAlternateComfyRoot(t *testing.T) {
	svc, _, _, current, missing := newInputRecoveryService(t)
	alternate := t.TempDir()
	alternateProject := filepath.Join(alternate, "input", "1")
	if err := os.MkdirAll(alternateProject, 0o755); err != nil {
		t.Fatal(err)
	}
	// Exact same basename is the same generated file, so copying cannot change identity.
	if err := os.WriteFile(filepath.Join(alternateProject, missing), []byte("same-generated-file"), 0o644); err != nil {
		t.Fatal(err)
	}
	fallbackComfyRoots = []string{alternate}
	t.Cleanup(func() { fallbackComfyRoots = nil })
	if _, err := svc.ensureProjectInputFile(1, missing); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(svc.cfg.Comfy.ComfyDir, "input", "1", missing))
	if err != nil || string(data) != "same-generated-file" {
		t.Fatalf("alternate root copy failed: %v %q", err, data)
	}
	if _, err := os.Stat(filepath.Join(svc.cfg.Comfy.ComfyDir, "input", "1", current)); err != nil {
		t.Fatal("existing authoritative file was disturbed")
	}
}

func TestEnsureProjectInputExplainsMissingOrDisconnectedInputRoot(t *testing.T) {
	svc, _, _, _, missing := newInputRecoveryService(t)
	if err := os.RemoveAll(filepath.Join(svc.cfg.Comfy.ComfyDir, "input")); err != nil {
		t.Fatal(err)
	}
	fallbackComfyRoots = []string{filepath.Join(t.TempDir(), "not-there")}
	t.Cleanup(func() { fallbackComfyRoots = nil })
	_, err := svc.ensureProjectInputFile(1, missing)
	if err == nil || !strings.Contains(err.Error(), "目录不可访问") || !strings.Contains(err.Error(), "comfy_dir") {
		t.Fatalf("unclear root diagnostic: %v", err)
	}
}

func TestEnsureProjectInputRejectsUnknownAndCrossProjectFiles(t *testing.T) {
	svc, _, _, _, _ := newInputRecoveryService(t)
	if _, err := svc.ensureProjectInputFile(1, "unknown_sheet.png"); err == nil {
		t.Fatal("unknown file was accepted")
	}
	other := models.Character{ProjectID: 2, Name: "其他项目角色", Sheet: "char_sheet_77_1.png"}
	if err := svc.db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ensureProjectInputFile(1, "char_sheet_77_1.png"); err == nil || !strings.Contains(err.Error(), "不存在") {
		t.Fatalf("cross-project identity leak: %v", err)
	}
}
