package service

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"comfyui-console/internal/models"
)

func TestReferenceBindingsDetectReplacementMutationAndMissingFile(t *testing.T) {
	f := newFaceCoverFixture(t)
	selected, _ := parseSceneReferences(&f.scene)
	if err := f.ps.SaveSceneReferences(&f.scene, selected); err != nil {
		t.Fatal(err)
	}
	f.ps.db.First(&f.scene, f.scene.ID)
	bound, _ := parseSceneReferences(&f.scene)
	if len(bound) != 1 || bound[0].BindingVersion != 1 || bound[0].BoundSHA256 != sha256Hex(f.original) {
		t.Fatalf("binding=%+v", bound)
	}
	if err := f.ps.validateSceneReferenceBindings(&f.scene); err != nil {
		t.Fatal(err)
	}
	// Same filename, different bytes: neither image nor video may silently submit it.
	if _, _, err := f.upload.SaveFile(fmt.Sprint(f.scene.ProjectID), "image", "street.png", []byte("changed bytes")); err != nil {
		t.Fatal(err)
	}
	if err := f.ps.validateSceneReferenceBindings(&f.scene); err == nil {
		t.Fatal("changed bytes accepted")
	}
	if err := f.ps.GenerateSceneImage(&f.scene); err == nil {
		t.Fatal("image generation did not reject invalid bound bytes")
	}
	if err := f.ps.GenerateSceneVideo(&models.Project{}, &f.scene); err == nil {
		t.Fatal("video generation did not reject invalid bound bytes")
	}
	if err := f.ps.SaveSceneReferences(&f.scene, bound); err == nil {
		t.Fatal("old binding silently rebound on save")
	}
	// Explicitly selecting anew records the current version, not the obsolete digest.
	selected[0].BindingVersion = 0
	if err := f.ps.SaveSceneReferences(&f.scene, selected); err != nil {
		t.Fatal(err)
	}
	f.ps.db.First(&f.scene, f.scene.ID)
	bound, _ = parseSceneReferences(&f.scene)
	if bound[0].BoundSHA256 != sha256Hex([]byte("changed bytes")) {
		t.Fatal("explicit rebind not persisted")
	}
	if err := f.ps.db.Model(&f.location).Update("image", "new.png").Error; err != nil {
		t.Fatal(err)
	}
	if err := f.ps.validateSceneReferenceBindings(&f.scene); err == nil {
		t.Fatal("source pointer replacement accepted")
	}
	if err := f.ps.db.Model(&f.location).Update("image", "street.png").Error; err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(f.upload.ResolveReadPath(fmt.Sprint(f.scene.ProjectID), "street.png")); err != nil {
		t.Fatal(err)
	}
	if err := f.ps.validateSceneReferenceBindings(&f.scene); err == nil {
		t.Fatal("missing file accepted")
	}
}

func TestReferenceSnapshotV2PreservesDuplicatePositions(t *testing.T) {
	f := newFaceCoverFixture(t)
	if err := f.ps.db.AutoMigrate(&models.GenerationSnapshot{}); err != nil {
		t.Fatal(err)
	}
	files := map[string][]FileMeta{"ref_images": {{TaskID: fmt.Sprint(f.scene.ProjectID), Name: "street.png"}, {TaskID: fmt.Sprint(f.scene.ProjectID), Name: "street.png"}}}
	f.ps.recordGenerationSnapshot(&f.scene, "video", &models.Task{TaskID: "fixture", Prompt: "reviewed"}, &models.Template{Code: "fixture"}, files)
	var row models.GenerationSnapshot
	if err := f.ps.db.First(&row).Error; err != nil {
		t.Fatal(err)
	}
	var refs []generationSnapshotRef
	if err := json.Unmarshal([]byte(row.RefsJSON), &refs); err != nil {
		t.Fatal(err)
	}
	if len(refs) != 2 || refs[0].Order != 1 || refs[1].Order != 2 || refs[0].SchemaVersion != 2 || refs[0].SourceID != f.location.ID || refs[1].SourceType != "asset" || refs[0].SHA256 != refs[1].SHA256 {
		t.Fatalf("refs=%+v", refs)
	}
}
