package service

import (
	"testing"

	"comfyui-console/internal/models"
)

func TestAssetVariantSelectValidatesOwnedUploadAndInvalidatesConsumersTransitively(t *testing.T) {
	db := safetyDB(t, &models.UploadFile{}, &models.Asset{}, &models.AssetVariant{})
	project := models.Project{Title: "p", Generation: 1}
	other := models.Project{Title: "other", Generation: 1}
	db.Create(&project)
	db.Create(&other)
	asset := models.Asset{ProjectID: project.ID, Kind: AssetKindProp, Name: "key", Image: "old.png"}
	db.Create(&asset)
	for _, name := range []string{"old.png", "new.png"} {
		db.Create(&models.UploadFile{TaskID: "1", Type: "image", Name: name, Path: "1/" + name, Size: 10})
	}
	// IDs happen to start at one, but ownership must use the persisted project ID.
	db.Model(&models.UploadFile{}).Where("task_id = ?", "1").Update("task_id", project.ID)

	svc := NewAssetVariantService(db)
	old, err := svc.Register(models.AssetVariant{ProjectID: project.ID, EntityType: VariantAssetImage, EntityID: asset.ID, File: "old.png", Selected: true})
	if err != nil || old.ID == 0 {
		t.Fatalf("register old variant: row=%+v err=%v", old, err)
	}
	newVariant, err := svc.Register(models.AssetVariant{ProjectID: project.ID, EntityType: VariantAssetImage, EntityID: asset.ID, File: "new.png"})
	if err != nil {
		t.Fatal(err)
	}

	direct := models.Scene{ProjectID: project.ID, Generation: 1, EpisodeN: 1, Order: 1, Props: "key", ImageFile: "scene.png", VideoFile: "scene.mp4", VideoTaskID: "video-1", Status: "video_ready"}
	dependent := models.Scene{ProjectID: project.ID, Generation: 1, EpisodeN: 1, Order: 2, ImageFile: "dep.png", VideoFile: "dep.mp4", VideoTaskID: "video-2", Status: "video_ready"}
	transitive := models.Scene{ProjectID: project.ID, Generation: 1, EpisodeN: 1, Order: 3, ImageFile: "transitive.png", VideoFile: "transitive.mp4", VideoTaskID: "video-3", Status: "video_ready"}
	unrelated := models.Scene{ProjectID: other.ID, Generation: 1, EpisodeN: 1, Order: 1, Props: "key", ImageFile: "other.png", VideoFile: "other.mp4", Status: "video_ready"}
	for _, scene := range []*models.Scene{&direct, &dependent, &transitive, &unrelated} {
		if err := db.Create(scene).Error; err != nil {
			t.Fatal(err)
		}
	}
	db.Create(&models.SceneContinuity{SceneID: dependent.ID, Mode: models.ContinuityModeContinue, SourceSceneID: &direct.ID, SelectedFrameID: nil, Status: "ready"})
	db.Create(&models.SceneContinuity{SceneID: transitive.ID, Mode: models.ContinuityModeContinue, SourceSceneID: &dependent.ID, SelectedFrameID: nil, Status: "ready"})
	for _, scene := range []models.Scene{direct, dependent, transitive} {
		db.Create(&models.GenerationCandidate{ProjectID: project.ID, EntityType: "scene", EntityID: scene.ID, MediaType: "video", TaskID: scene.VideoTaskID, File: scene.VideoFile, IsCurrent: true})
	}

	if _, err := svc.Select(project.ID, newVariant.ID); err != nil {
		t.Fatal(err)
	}
	db.First(&asset, asset.ID)
	if asset.Image != "new.png" {
		t.Fatalf("asset image not selected: %q", asset.Image)
	}
	for _, id := range []uint{direct.ID, dependent.ID, transitive.ID} {
		var scene models.Scene
		db.First(&scene, id)
		if scene.VideoFile != "" || !scene.PromptStale {
			t.Fatalf("scene %d not invalidated: %+v", id, scene)
		}
		var current int64
		db.Model(&models.GenerationCandidate{}).Where("entity_id = ? AND is_current = ?", id, true).Count(&current)
		if current != 0 {
			t.Fatalf("scene %d retained a current candidate", id)
		}
	}
	db.First(&unrelated, unrelated.ID)
	if unrelated.VideoFile != "other.mp4" {
		t.Fatalf("other project was invalidated: %+v", unrelated)
	}
}

func TestAssetVariantRejectsUnownedFileAndDeletesWithOwner(t *testing.T) {
	db := safetyDB(t, &models.UploadFile{}, &models.Asset{}, &models.AssetVariant{})
	project := models.Project{Title: "p"}
	db.Create(&project)
	asset := models.Asset{ProjectID: project.ID, Kind: AssetKindProp, Name: "key"}
	db.Create(&asset)
	svc := NewAssetVariantService(db)
	if _, err := svc.Register(models.AssetVariant{ProjectID: project.ID, EntityType: VariantAssetImage, EntityID: asset.ID, File: "missing.png"}); err == nil {
		t.Fatal("variant without project-owned upload was accepted")
	}
	db.Create(&models.UploadFile{TaskID: "1", Type: "image", Name: "owned.png", Path: "1/owned.png"})
	db.Model(&models.UploadFile{}).Update("task_id", project.ID)
	row, err := svc.Register(models.AssetVariant{ProjectID: project.ID, EntityType: VariantAssetImage, EntityID: asset.ID, File: "owned.png"})
	if err != nil {
		t.Fatal(err)
	}
	ps := &ProjectService{db: db}
	if err := ps.DeleteAsset(project.ID, asset.ID); err != nil {
		t.Fatal(err)
	}
	var count int64
	db.Model(&models.AssetVariant{}).Where("id = ?", row.ID).Count(&count)
	if count != 0 {
		t.Fatal("owner deletion left asset variant behind")
	}
}
