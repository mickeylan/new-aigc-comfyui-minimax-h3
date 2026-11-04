package service

import (
	"errors"
	"testing"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

func TestRenameCharacterPreservesArtifactsAndRewritesIdentityReferences(t *testing.T) {
	ps := newTestProjectService(t)
	if err := ps.db.AutoMigrate(&models.CharacterAliasCandidate{}, &models.Dialogue{}, &models.Shot{}); err != nil {
		t.Fatal(err)
	}
	p := models.Project{Title: "rename"}
	ps.db.Create(&p)
	ch := models.Character{ProjectID: p.ID, Name: "旧名", Portrait: "portrait.png", Sheet: "sheet.png", PortraitTaskID: "task", ProfileStatus: models.ProfileStatusApproved, ReferencePrompt: "confirmed English prompt"}
	ps.db.Create(&ch)
	refs := `[{"source_type":"character","source_id":1,"variant":"portrait","use_krea2":true,"use_h3":true,"bind_character":"旧名"}]`
	sc := models.Scene{ProjectID: p.ID, Characters: "旧名", VisibleCharacters: "旧名", LocationName: "城门", Props: "剑", ReferenceImagesJSON: refs, ImagePrompt: "keep English image prompt", VideoFullPrompt: "keep English video prompt", BlockingJSON: `{"characters":[{"name":"旧名"}]}`}
	ps.db.Create(&sc)
	shot := models.Shot{SceneID: sc.ID, Order: 1, PromptSubject: "旧名 stands", ListenerCharacter: "旧名"}
	ps.db.Create(&shot)
	d := models.Dialogue{ProjectID: p.ID, SceneID: sc.ID, Character: "旧名", Text: "绝不改台词", AudioFile: "audio.wav", AudioStale: false, Status: "ready"}
	ps.db.Create(&d)

	got, err := ps.RenameCharacter(p.ID, ch.ID, "新名")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != ch.ID || got.Portrait != ch.Portrait || got.Sheet != ch.Sheet || got.PortraitTaskID != ch.PortraitTaskID || got.ProfileStatus != ch.ProfileStatus || got.ReferencePrompt != ch.ReferencePrompt {
		t.Fatalf("artifacts changed: %+v", got)
	}
	ps.db.First(&sc, sc.ID)
	ps.db.First(&shot, shot.ID)
	ps.db.First(&d, d.ID)
	if sc.Characters != "新名" || sc.VisibleCharacters != "新名" || !sc.PromptStale || sc.ImagePrompt != "keep English image prompt" || sc.VideoFullPrompt != "keep English video prompt" {
		t.Fatalf("scene mismatch: %+v", sc)
	}
	if shot.PromptSubject != "新名 stands" || shot.ListenerCharacter != "新名" {
		t.Fatalf("shot mismatch: %+v", shot)
	}
	if d.Character != "新名" || d.Text != "绝不改台词" || d.AudioFile != "audio.wav" || d.AudioStale || d.Status != "ready" {
		t.Fatalf("dialogue/audio changed: %+v", d)
	}
	var alias models.CharacterAliasCandidate
	if err := ps.db.Where("project_id=? AND alias=?", p.ID, "旧名").First(&alias).Error; err != nil || alias.CanonicalName != "新名" || alias.Status != "confirmed" {
		t.Fatalf("alias missing: %+v %v", alias, err)
	}
}

func TestRenameAssetsPreservesMediaAndUpdatesSceneNames(t *testing.T) {
	ps := newTestProjectService(t)
	p := models.Project{Title: "assets"}
	ps.db.Create(&p)
	location := models.Asset{ProjectID: p.ID, Kind: "location", Name: "旧地", Image: "location.png", ImageTaskID: "location-task"}
	ps.db.Create(&location)
	prop := models.Asset{ProjectID: p.ID, Kind: "prop", Name: "旧剑", Image: "sword.png", Sheet: "sword-sheet.png"}
	ps.db.Create(&prop)
	sc := models.Scene{ProjectID: p.ID, LocationName: "旧地", Props: "旧剑", ImageFile: "storyboard.png", VideoFile: "video.mp4", Status: "video_ready"}
	ps.db.Create(&sc)
	if _, err := ps.RenameAsset(p.ID, location.ID, "location", "新地"); err != nil {
		t.Fatal(err)
	}
	if _, err := ps.RenameAsset(p.ID, prop.ID, "prop", "新剑"); err != nil {
		t.Fatal(err)
	}
	ps.db.First(&location, location.ID)
	ps.db.First(&prop, prop.ID)
	ps.db.First(&sc, sc.ID)
	if location.Image != "location.png" || location.ImageTaskID != "location-task" || prop.Image != "sword.png" || prop.Sheet != "sword-sheet.png" || sc.ImageFile != "storyboard.png" || sc.VideoFile != "video.mp4" || sc.Status != "video_ready" {
		t.Fatal("rename changed media")
	}
	if sc.LocationName != "新地" || sc.Props != "新剑" || !sc.PromptStale {
		t.Fatalf("scene names not updated: %+v", sc)
	}
}

func TestRenameRejectsDuplicateAndCrossProjectIDs(t *testing.T) {
	ps := newTestProjectService(t)
	p1, p2 := models.Project{Title: "one"}, models.Project{Title: "two"}
	ps.db.Create(&p1)
	ps.db.Create(&p2)
	one, duplicate, foreign := models.Character{ProjectID: p1.ID, Name: "甲"}, models.Character{ProjectID: p1.ID, Name: "乙"}, models.Character{ProjectID: p2.ID, Name: "丙"}
	ps.db.Create(&one)
	ps.db.Create(&duplicate)
	ps.db.Create(&foreign)
	if _, err := ps.RenameCharacter(p1.ID, one.ID, "乙"); !errors.Is(err, ErrRenameConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
	if _, err := ps.RenameCharacter(p1.ID, foreign.ID, "越权"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected cross-project not found, got %v", err)
	}
	a1, a2 := models.Asset{ProjectID: p1.ID, Kind: "location", Name: "城门", Image: "x.png"}, models.Asset{ProjectID: p1.ID, Kind: "location", Name: "宫殿"}
	ps.db.Create(&a1)
	ps.db.Create(&a2)
	if _, err := ps.RenameAsset(p1.ID, a1.ID, "location", "宫殿"); !errors.Is(err, ErrRenameConflict) {
		t.Fatalf("expected asset conflict, got %v", err)
	}
}
