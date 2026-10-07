package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

func verifiedTestFontRegistry(t *testing.T) *FontRegistry {
	t.Helper()
	dataDir := t.TempDir()
	root := filepath.Join(dataDir, "fonts")
	if err := os.MkdirAll(filepath.Join(root, "licenses"), 0o755); err != nil {
		t.Fatal(err)
	}
	fontPath := filepath.Join(root, "font.ttf")
	if err := os.WriteFile(fontPath, []byte("approved font"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "licenses", "OFL.txt"), []byte("OFL 1.1"), 0o644); err != nil {
		t.Fatal(err)
	}
	hash, err := fileSHA256(fontPath)
	if err != nil {
		t.Fatal(err)
	}
	manifest, _ := json.Marshal([]FontRegistryEntry{{Code: "noto-sans-sc", Family: "Noto Sans SC", File: "font.ttf", SHA256: hash, LicenseID: "OFL-1.1", LicenseFile: "licenses/OFL.txt", CommercialUse: true, Redistribution: true, Embedding: true, SupportsChinese: true, Enabled: true}})
	if err := os.WriteFile(filepath.Join(root, fontManifestFile), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	return NewFontRegistry(dataDir)
}

func TestScreenTextPreflightReportsFinalRenderAndFallback(t *testing.T) {
	db := safetyDB(t, &models.Project{}, &models.Scene{}, &models.ScreenTextCue{})
	project := models.Project{Title: "剧"}
	db.Create(&project)
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1, Duration: 6}
	db.Create(&scene)
	x, y := .8, .2
	cue := models.ScreenTextCue{ProjectID: project.ID, EpisodeN: 1, SceneID: &scene.ID, Kind: "location", Text: "玉霄宫", StartTime: 1, EndTime: 4, WritingMode: "vertical-rl", Anchor: "custom", PositionX: &x, PositionY: &y, StyleCode: "xianxia-location-vertical", Animation: "ink_reveal", Enabled: true, ReviewStatus: "approved"}
	db.Create(&cue)
	result, err := NewScreenTextService(db).Preflight(project.ID, 1, 1080, 1920, verifiedTestFontRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK || !result.FontVerified || len(result.Previews) != 1 {
		t.Fatalf("unexpected preflight: %+v", result)
	}
	preview := result.Previews[0]
	if preview.FontSize != 60 || preview.AnimationRendered != "slow_fade_fallback" || !strings.Contains(preview.ASSEvent, `\pos(864,384)`) || !strings.Contains(preview.ASSEvent, `\fad(500,450)`) {
		t.Fatalf("unexpected preview: %+v", preview)
	}
	if len(result.Issues) != 1 || result.Issues[0].Severity != "warning" || result.Issues[0].Code != "ink_reveal_fallback" {
		t.Fatalf("fallback warning missing: %+v", result.Issues)
	}
}

func TestScreenTextPreflightBlocksUnverifiedFontAndInvalidExistingCue(t *testing.T) {
	db := safetyDB(t, &models.Project{}, &models.Scene{}, &models.ScreenTextCue{})
	project := models.Project{Title: "剧"}
	db.Create(&project)
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1, Duration: 3}
	db.Create(&scene)
	db.Create(&models.ScreenTextCue{ProjectID: project.ID, EpisodeN: 1, SceneID: &scene.ID, Kind: "location", Text: "越界旧数据", StartTime: 0, EndTime: 9, WritingMode: "horizontal-ltr", Anchor: "center", StyleCode: "modern-horizontal-caption", Animation: "none", Enabled: true, ReviewStatus: "approved"})
	result, err := NewScreenTextService(db).Preflight(project.ID, 1, 0, 0, NewFontRegistry(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	if result.OK || result.FontVerified {
		t.Fatalf("unsafe preflight passed: %+v", result)
	}
	codes := map[string]bool{}
	for _, issue := range result.Issues {
		codes[issue.Code] = true
	}
	if !codes["font_unverified"] || !codes["invalid_binding"] {
		t.Fatalf("expected errors missing: %+v", result.Issues)
	}
}
