package service

import (
	"testing"

	"comfyui-console/internal/models"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newShotMaterializationDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Project{}, &models.Scene{}, &models.Shot{}, &models.Dialogue{}, &models.SceneContinuity{}, &models.FrameCandidate{}, &models.GenerationCandidate{}, &models.SceneCharacterLook{}, &models.ShotCharacterLook{}, &models.CharacterOutfit{}, &models.SceneCharacterOutfit{}, &models.ShotCharacterOutfit{}, &models.PromptPolicyOverride{}, &models.SharedAssetReference{}, &models.Material{}, &models.AudioLayer{}, &models.PromptVersion{}, &models.Episode{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestMaterializeShotsCreatesNativeScenesAndPreservesDialogue(t *testing.T) {
	db := newShotMaterializationDB(t)
	project := models.Project{Title: "p", Generation: 1}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	before := models.Scene{ProjectID: project.ID, EpisodeN: 1, Generation: 1, Order: 1, Title: "before", Content: "before", Duration: 5}
	_ = db.Create(&before).Error
	source := models.Scene{ProjectID: project.ID, EpisodeN: 1, Generation: 1, Order: 2, Title: "桃花林", Content: "姐妹对话", ImagePrompt: "桃花林", Duration: 15, LocationName: "梵心桃花林", Characters: "若彤,若琳", ReferenceImagesJSON: `[]`}
	if err := db.Create(&source).Error; err != nil {
		t.Fatal(err)
	}
	after := models.Scene{ProjectID: project.ID, EpisodeN: 1, Generation: 1, Order: 3, Title: "after", Content: "after", Duration: 5, ImageFile: "keep.png"}
	_ = db.Create(&after).Error
	d1 := models.Dialogue{ProjectID: project.ID, SceneID: source.ID, Order: 1, Character: "若彤", SpeechType: "dialogue", Text: "姐姐，你还好吗？", Status: "ready", AudioFile: "old.wav"}
	d2 := models.Dialogue{ProjectID: project.ID, SceneID: source.ID, Order: 2, Character: "若琳", SpeechType: "dialogue", Text: "我很好。"}
	_ = db.Create(&d1).Error
	_ = db.Create(&d2).Error
	shots := []models.Shot{
		{SceneID: source.ID, Order: 1, ActType: models.ShotActSetup, ShotType: "近景", Duration: 4, Description: "若彤询问", Dialogue: "姐姐，", PromptSubject: "若彤", PromptAction: "询问", PromptCamera: "近景", PromptLighting: "夕阳", PromptStyle: "写实"},
		{SceneID: source.ID, Order: 2, ActType: models.ShotActRising, ShotType: "反应", Duration: 5, Description: "若琳反应，若彤画外音", Dialogue: "你还好吗？", PromptSubject: "若琳", PromptAction: "聆听", PromptCamera: "特写", PromptLighting: "夕阳", PromptStyle: "写实"},
		{SceneID: source.ID, Order: 3, ActType: models.ShotActResolution, ShotType: "双人", Duration: 4, Description: "若琳回答", Dialogue: "我很好。", PromptSubject: "姐妹", PromptAction: "回答", PromptCamera: "双人", PromptLighting: "夕阳", PromptStyle: "写实"},
	}
	for i := range shots {
		if err := db.Create(&shots[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	created, err := NewShotService(db).Materialize(project.ID, source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(created) != 1 || created[0].Duration != 13 || created[0].ShotCount != 3 {
		t.Fatalf("created=%+v", created)
	}
	var scenes []models.Scene
	if err := db.Where("project_id=?", project.ID).Order("`order`").Find(&scenes).Error; err != nil {
		t.Fatal(err)
	}
	if len(scenes) != 3 || scenes[0].ID != before.ID || scenes[2].ID != after.ID || scenes[2].ImageFile != "keep.png" {
		t.Fatalf("scenes=%+v", scenes)
	}
	var childShots []models.Shot
	if err := db.Where("scene_id = ?", created[0].ID).Order("order_num").Find(&childShots).Error; err != nil || len(childShots) != 3 {
		t.Fatalf("child shots=%+v err=%v", childShots, err)
	}
	var joined string
	for _, sc := range created {
		if sc.Duration < 3 || sc.Duration > 15 {
			t.Fatalf("duration=%v", sc.Duration)
		}
		var ds []models.Dialogue
		_ = db.Where("scene_id=?", sc.ID).Order("`order`").Find(&ds).Error
		for _, d := range ds {
			joined += d.Text
			if d.AudioFile != "" || !d.AudioStale {
				t.Fatalf("audio not reset: %+v", d)
			}
		}
	}
	if joined != "姐姐，你还好吗？我很好。" {
		t.Fatalf("joined=%q", joined)
	}
}

func TestGroupNativeShotsPacksAdjacentShotsUpToFifteenSeconds(t *testing.T) {
	durations := []float64{4, 3, 3, 5, 5, 4, 5, 4, 4, 3, 4, 5, 4}
	shots := make([]models.Shot, len(durations))
	fragments := make([][]dialogueFragment, len(durations))
	for i, duration := range durations {
		shots[i] = models.Shot{ID: uint(i + 1), Duration: duration}
	}
	groups := groupNativeShots(shots, fragments)
	if len(groups) != 4 {
		t.Fatalf("groups=%d", len(groups))
	}
	want := []float64{15, 14, 15, 9}
	for i := range groups {
		if groups[i].Duration != want[i] {
			t.Fatalf("group %d duration=%v want=%v", i, groups[i].Duration, want[i])
		}
	}
}

func TestPreviewMaterializationRejectsDialogueDrift(t *testing.T) {
	db := newShotMaterializationDB(t)
	p := models.Project{Title: "p", Generation: 1}
	_ = db.Create(&p).Error
	sc := models.Scene{ProjectID: p.ID, EpisodeN: 1, Generation: 1, Order: 1, Title: "s"}
	_ = db.Create(&sc).Error
	_ = db.Create(&models.Dialogue{ProjectID: p.ID, SceneID: sc.ID, Order: 1, Character: "甲", SpeechType: "dialogue", Text: "原文"}).Error
	for i, text := range []string{"改写", ""} {
		_ = db.Create(&models.Shot{SceneID: sc.ID, Order: i + 1, ActType: models.ShotActSetup, ShotType: "近景", Duration: 3, Description: "镜头", Dialogue: text}).Error
	}
	if _, err := NewShotService(db).PreviewMaterialization(p.ID, sc.ID); err == nil {
		t.Fatal("dialogue drift accepted")
	}
}
