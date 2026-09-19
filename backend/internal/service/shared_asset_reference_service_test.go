package service

import (
	"errors"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"comfyui-console/internal/models"
)

func newSharedAssetTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&models.Project{}, &models.Scene{}, &models.Shot{}, &models.Material{}, &models.SharedAssetReference{},
	); err != nil {
		t.Fatal(err)
	}
	return db
}

func materialProjectID(id uint) *uint { return &id }

func TestSharedAssetReferenceCRUDValidatesOwnershipAndMode(t *testing.T) {
	db := newSharedAssetTestDB(t)
	project := models.Project{Title: "project"}
	otherProject := models.Project{Title: "other"}
	db.Create(&project)
	db.Create(&otherProject)
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1}
	otherScene := models.Scene{ProjectID: otherProject.ID, EpisodeN: 1, Order: 1}
	db.Create(&scene)
	db.Create(&otherScene)
	shot := models.Shot{SceneID: scene.ID, Order: 1, ShotType: "wide", Duration: 1}
	otherShot := models.Shot{SceneID: otherScene.ID, Order: 1, ShotType: "wide", Duration: 1}
	db.Create(&shot)
	db.Create(&otherShot)
	global := models.Material{Name: "global", Type: "image", Source: "upload", Path: "global.png"}
	foreign := models.Material{Name: "foreign", Type: "image", Source: "upload", ProjectID: materialProjectID(otherProject.ID), Path: "foreign.png"}
	db.Create(&global)
	db.Create(&foreign)

	svc := NewSharedAssetReferenceService(db)
	if refs, err := svc.List(project.ID); err != nil || len(refs) != 0 {
		t.Fatalf("materials must not create implicit references: refs=%+v err=%v", refs, err)
	}
	if _, err := svc.Create(project.ID, SharedAssetReferenceInput{MaterialID: foreign.ID}); err == nil {
		t.Fatal("accepted material owned by another project")
	}
	if _, err := svc.Create(project.ID, SharedAssetReferenceInput{MaterialID: global.ID, SceneID: &otherScene.ID}); err == nil {
		t.Fatal("accepted scene owned by another project")
	}
	if _, err := svc.Create(project.ID, SharedAssetReferenceInput{MaterialID: global.ID, ShotID: &otherShot.ID}); err == nil {
		t.Fatal("accepted shot owned by another project")
	}
	copyRef, err := svc.Create(project.ID, SharedAssetReferenceInput{MaterialID: global.ID, Mode: "copy"})
	if err != nil || copyRef.LocalFile != global.Path {
		t.Fatalf("copy mode did not snapshot material path: ref=%+v err=%v", copyRef, err)
	}
	if err := svc.Delete(project.ID, copyRef.ID); err != nil {
		t.Fatal(err)
	}

	ref, err := svc.Create(project.ID, SharedAssetReferenceInput{
		MaterialID: global.ID, SceneID: &scene.ID, ShotID: &shot.ID, Mode: "live", LocalFile: "ignored.png",
	})
	if err != nil {
		t.Fatal(err)
	}
	if ref.Mode != "live" || ref.LocalFile != "" {
		t.Fatalf("live reference not normalized: %+v", ref)
	}
	if _, err := svc.Create(project.ID, SharedAssetReferenceInput{MaterialID: global.ID, SceneID: &scene.ID, ShotID: &shot.ID}); err == nil {
		t.Fatal("accepted duplicate reference")
	}
	updated, err := svc.Update(project.ID, ref.ID, SharedAssetReferenceInput{
		MaterialID: global.ID, SceneID: &scene.ID, ShotID: &shot.ID, Mode: "copy", LocalFile: "copies/global.png",
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Mode != "copy" || updated.LocalFile != "copies/global.png" {
		t.Fatalf("copy update not persisted: %+v", updated)
	}
	views, err := svc.List(project.ID)
	if err != nil || len(views) != 1 || views[0].Material.ID != global.ID {
		t.Fatalf("list = %+v, err=%v", views, err)
	}
	if err := svc.Delete(otherProject.ID, ref.ID); !errors.Is(err, ErrSharedAssetReferenceNotFound) {
		t.Fatalf("cross-project delete error = %v", err)
	}
	if err := svc.Delete(project.ID, ref.ID); err != nil {
		t.Fatal(err)
	}
	if refs, _ := svc.List(project.ID); len(refs) != 0 {
		t.Fatalf("reference was not deleted: %+v", refs)
	}
}

func TestSharedAssetReferenceEffectiveListIncludesGlobalsAndExplicitProjectRefs(t *testing.T) {
	db := newSharedAssetTestDB(t)
	project := models.Project{Title: "project"}
	db.Create(&project)
	global := models.Material{Name: "global", Type: "image", Source: "upload"}
	projectReferenced := models.Material{Name: "selected", Type: "image", Source: "upload", ProjectID: materialProjectID(project.ID)}
	projectUnreferenced := models.Material{Name: "not-selected", Type: "image", Source: "upload", ProjectID: materialProjectID(project.ID)}
	db.Create(&global)
	db.Create(&projectReferenced)
	db.Create(&projectUnreferenced)

	svc := NewSharedAssetReferenceService(db)
	if _, err := svc.Create(project.ID, SharedAssetReferenceInput{MaterialID: projectReferenced.ID, Mode: "live"}); err != nil {
		t.Fatal(err)
	}
	effective, err := svc.ListEffective(project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(effective) != 2 {
		t.Fatalf("effective assets = %+v", effective)
	}
	if effective[0].Material.ID != global.ID || len(effective[0].References) != 0 {
		t.Fatalf("global asset missing or implicitly referenced: %+v", effective[0])
	}
	if effective[1].Material.ID != projectReferenced.ID || len(effective[1].References) != 1 {
		t.Fatalf("explicit project asset missing: %+v", effective[1])
	}
}

func TestMaterialDeleteBlockedWhileSharedAssetReferenced(t *testing.T) {
	db := newSharedAssetTestDB(t)
	project := models.Project{Title: "project"}
	db.Create(&project)
	material := models.Material{Name: "generated", Type: "image", Source: "scene"}
	db.Create(&material)
	refs := NewSharedAssetReferenceService(db)
	ref, err := refs.Create(project.ID, SharedAssetReferenceInput{MaterialID: material.ID})
	if err != nil {
		t.Fatal(err)
	}
	materials := NewMaterialService(nil, db, nil, nil)
	if err := materials.Delete(material.ID); !errors.Is(err, ErrMaterialReferenced) {
		t.Fatalf("delete error = %v", err)
	}
	if err := db.First(&models.Material{}, material.ID).Error; err != nil {
		t.Fatalf("referenced material was deleted: %v", err)
	}
	if err := refs.Delete(project.ID, ref.ID); err != nil {
		t.Fatal(err)
	}
	if err := materials.Delete(material.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&models.Material{}, material.ID).Error; !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("unreferenced material still exists: %v", err)
	}
}
