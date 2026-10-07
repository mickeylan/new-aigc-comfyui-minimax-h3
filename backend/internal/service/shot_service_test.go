package service

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"comfyui-console/internal/models"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestResolveShotDialogueFragmentsUsesAuthoritativeRanges(t *testing.T) {
	db := newTestDBWithNewModels(t)
	p := models.Project{Title: "p"}
	db.Create(&p)
	sc := models.Scene{ProjectID: p.ID, EpisodeN: 1, Generation: 1, Order: 1, Duration: 8}
	db.Create(&sc)
	d := models.Dialogue{ProjectID: p.ID, SceneID: sc.ID, Order: 1, Character: "林夏", SpeechType: "dialogue", Text: "你好世界"}
	db.Create(&d)
	shots := []models.Shot{{ID: 1, SceneID: sc.ID, Order: 1, DialogueRanges: []models.ShotDialogueRange{{DialogueID: d.ID, GroupKey: "dialogue-group:1", StartRune: 0, EndRune: 2}}}, {ID: 2, SceneID: sc.ID, Order: 2, DialogueRanges: []models.ShotDialogueRange{{DialogueID: d.ID, GroupKey: "dialogue-group:1", StartRune: 2, EndRune: 4}}}}
	got, err := NewShotService(db).ResolveDialogueFragments(sc.ID, shots)
	if err != nil {
		t.Fatal(err)
	}
	if got[1][0].Text != "你好" || got[2][0].Text != "世界" || got[1][0].SpeakerLabel != "林夏" {
		t.Fatalf("fragments=%+v", got)
	}
}

func TestRebalanceShotDurationsPreservesRatiosAndExactSceneTotal(t *testing.T) {
	shots := []models.Shot{{Duration: 3.0}, {Duration: 5.0}}
	got, err := rebalanceShotDurations(shots, 10)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Duration != 3.8 || got[1].Duration != 6.2 || shotDurationTotal(got) != 10 {
		t.Fatalf("retimed=%+v total=%v", got, shotDurationTotal(got))
	}
	if shots[0].Duration != 3.0 {
		t.Fatal("preview mutated original shots")
	}
}

func newTestDBWithNewModels(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&models.Project{}, &models.Episode{}, &models.Scene{}, &models.Shot{}, &models.PromptVersion{}, &models.StylePreset{},
		&models.Character{}, &models.CharacterLook{}, &models.ShotCharacterLook{},
		&models.CharacterOutfit{}, &models.ShotCharacterOutfit{},
		&models.GenerationCandidate{}, &models.FrameCandidate{}, &models.SceneContinuity{}, &models.Dialogue{},
	); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestShotServiceStoresManualDirectorStructure(t *testing.T) {
	db := newTestDBWithNewModels(t)
	project := models.Project{Title: "test"}
	db.Create(&project)
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1, Content: "权威剧情事实", ImagePrompt: "权威起始帧", Duration: 5}
	db.Create(&scene)
	db.Create(&models.Dialogue{ProjectID: project.ID, SceneID: scene.ID, Order: 1, Character: "侦探", Text: "原来如此"})
	svc := NewShotService(db)
	input := []models.Shot{
		{ActType: models.ShotActSetup, ShotType: "wide", Duration: 3.0, Description: "雨夜建立旧宅", PromptSubject: "雨中的旧宅", PromptCamera: "广角固定", PromptLighting: "冷色月光"},
		{ActType: models.ShotActMidpoint, ShotType: "close_up", CameraAngle: "eye_level", CameraMovement: "dolly_in", Duration: 5.0, Description: "角色发现线索", Dialogue: "原来如此", Emotion: "震惊", PromptSubject: "侦探与信件", PromptAction: "拆开信封", PromptCamera: "近景缓慢推进", PromptLighting: "冷色侧光", PromptStyle: "电影写实"},
	}
	shots, err := svc.ReplaceShots(scene.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	if len(shots) != 2 || shots[1].ActType != models.ShotActMidpoint || shots[1].PromptLighting != "冷色侧光" {
		t.Fatalf("structure lost: %+v", shots)
	}
	db.First(&scene, scene.ID)
	if scene.ShotCount != 2 || scene.Duration != 5 {
		t.Fatalf("scene count/duration changed: %d/%v", scene.ShotCount, scene.Duration)
	}
	if scene.Content != "权威剧情事实" || scene.ImagePrompt != "权威起始帧" || scene.Status != "pending" {
		t.Fatalf("Shot save overwrote Scene authority: %+v", scene)
	}
}

func TestShotServicePersistsExplicitDialogueRangesAcrossCuts(t *testing.T) {
	db := newTestDBWithNewModels(t)
	project := models.Project{Title: "ranges"}
	db.Create(&project)
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1}
	db.Create(&scene)
	dialogue := models.Dialogue{ProjectID: project.ID, SceneID: scene.ID, Order: 1, Character: "姐姐", Text: "相信姐姐，姐姐不会让你去。"}
	db.Create(&dialogue)
	shots, err := NewShotService(db).ReplaceShots(scene.ID, []models.Shot{{ShotType: "close", Duration: 4, Dialogue: "相信姐姐，"}, {ShotType: "reaction", Duration: 5, Dialogue: "姐姐不会让你去。"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(shots[0].DialogueRanges) != 1 || shots[0].DialogueRanges[0].DialogueID != dialogue.ID || shots[0].DialogueRanges[0].StartRune != 0 {
		t.Fatalf("first range=%+v", shots[0].DialogueRanges)
	}
	if !shots[0].ContinuesToNext || !shots[1].ContinuesFromPrevious {
		t.Fatalf("continuity flags missing: %+v %+v", shots[0], shots[1])
	}
	if shots[0].DialogueRanges[0].EndRune != shots[1].DialogueRanges[0].StartRune {
		t.Fatalf("ranges are not contiguous: %+v", shots)
	}
}

func TestReplaceShotsRejectsStaleDialogueSnapshotWithoutMutation(t *testing.T) {
	db := newTestDBWithNewModels(t)
	project := models.Project{Title: "dialogue snapshot"}
	db.Create(&project)
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1}
	db.Create(&scene)
	dialogue := models.Dialogue{ProjectID: project.ID, SceneID: scene.ID, Order: 1, Character: "姐姐", SpeechType: "dialogue", Text: "原始对白"}
	db.Create(&dialogue)
	svc := NewShotService(db)
	initial, err := svc.ReplaceShots(scene.ID, []models.Shot{{ShotType: "close", Duration: 3, Dialogue: "原始对白", Description: "原始镜头"}})
	if err != nil {
		t.Fatal(err)
	}
	_, snapshot, err := loadSceneDialogueSnapshot(db, scene.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&dialogue).Updates(map[string]any{"text": "修改对白", "order": 2}).Error; err != nil {
		t.Fatal(err)
	}
	_, err = svc.ReplaceShotsWithDialogueSnapshot(scene.ID, []models.Shot{{ShotType: "wide", Duration: 3, Dialogue: "修改对白", Description: "不应写入"}}, snapshot)
	if !errors.Is(err, ErrDialogueSnapshotConflict) {
		t.Fatalf("error=%v", err)
	}
	var saved []models.Shot
	if err := db.Where("scene_id = ?", scene.ID).Order("order_num, id").Find(&saved).Error; err != nil {
		t.Fatal(err)
	}
	if len(saved) != 1 || saved[0].ID != initial[0].ID || saved[0].Description != "原始镜头" || saved[0].Dialogue != "原始对白" {
		t.Fatalf("stale save mutated shots: %+v", saved)
	}
}

func TestShotServicePreservesSceneNegativeConstraints(t *testing.T) {
	db := newTestDBWithNewModels(t)
	project := models.Project{Title: "negative constraints"}
	db.Create(&project)
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1, NegativePrompt: "水印, 现代汽车"}
	db.Create(&scene)
	svc := NewShotService(db)
	if _, err := svc.ReplaceShots(scene.ID, []models.Shot{{ShotType: "wide", Duration: 3}}); err != nil {
		t.Fatal(err)
	}
	db.First(&scene, scene.ID)
	if scene.NegativePrompt != "水印, 现代汽车" {
		t.Fatalf("scene negative constraints were overwritten: %q", scene.NegativePrompt)
	}
	if _, err := svc.ReplaceShots(scene.ID, []models.Shot{{ShotType: "close", Duration: 3, NegativePrompt: "畸形手指"}}); err != nil {
		t.Fatal(err)
	}
	db.First(&scene, scene.ID)
	if !strings.Contains(scene.NegativePrompt, "水印, 现代汽车") || !strings.Contains(scene.NegativePrompt, "畸形手指") {
		t.Fatalf("shot constraints were not merged safely: %q", scene.NegativePrompt)
	}
}

func TestShotServiceReplacePreservesIDsAndRejectsForeignShot(t *testing.T) {
	db := newTestDBWithNewModels(t)
	project := models.Project{Title: "test"}
	db.Create(&project)
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1}
	otherScene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 2}
	db.Create(&scene)
	db.Create(&otherScene)
	svc := NewShotService(db)
	initial, err := svc.ReplaceShots(scene.ID, []models.Shot{
		{ShotType: "wide", Duration: 3, Description: "first"},
		{ShotType: "close", Duration: 3, Description: "second"},
	})
	if err != nil {
		t.Fatal(err)
	}
	firstID, secondID := initial[0].ID, initial[1].ID
	updated, err := svc.ReplaceShots(scene.ID, []models.Shot{
		{ID: secondID, ShotType: "close", Duration: 3, Description: "second updated"},
		{ID: firstID, ShotType: "wide", Duration: 3, Description: "first updated"},
		{ShotType: "medium", Duration: 3, Description: "new"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated) != 3 || updated[0].ID != secondID || updated[1].ID != firstID || updated[2].ID == 0 {
		t.Fatalf("shot IDs/order were not preserved: %+v", updated)
	}
	foreign, err := svc.ReplaceShots(otherScene.ID, []models.Shot{{ShotType: "wide", Duration: 3}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ReplaceShots(scene.ID, []models.Shot{{ID: foreign[0].ID, ShotType: "wide", Duration: 3}}); err == nil || !strings.Contains(err.Error(), "不属于当前场景") {
		t.Fatalf("foreign shot ID accepted: %v", err)
	}
	remaining, _ := svc.GetSceneShots(scene.ID)
	if len(remaining) != 3 || remaining[0].ID != secondID {
		t.Fatalf("failed replacement mutated existing shots: %+v", remaining)
	}
}

func TestShotServiceReplaceIsTransactionalAndValidates(t *testing.T) {
	db := newTestDBWithNewModels(t)
	scene := models.Scene{ProjectID: 1, EpisodeN: 1, Order: 1}
	db.Create(&scene)
	svc := NewShotService(db)
	_, err := svc.ReplaceShots(scene.ID, []models.Shot{{ShotType: "wide", Duration: 3}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.ReplaceShots(scene.ID, []models.Shot{{ShotType: "", Duration: 3}})
	if err == nil {
		t.Fatal("expected validation error")
	}
	shots, _ := svc.GetSceneShots(scene.ID)
	if len(shots) != 1 || shots[0].ShotType != "wide" {
		t.Fatalf("existing shots changed: %+v", shots)
	}
}

func TestShotServiceSaveRecordsCanonicalDeduplicatedManualPrompt(t *testing.T) {
	db := newTestDBWithNewModels(t)
	project := models.Project{Title: "prompt history"}
	db.Create(&project)
	scene := models.Scene{ProjectID: project.ID, Order: 1}
	db.Create(&scene)
	svc := NewShotService(db)

	shots, err := svc.ReplaceShots(scene.ID, []models.Shot{{
		ShotType: "close", Duration: 3, PromptSubject: "  face  ", PromptAction: "turns\nslowly",
		PromptCamera: "close-up", PromptLighting: "moonlight", PromptStyle: "film",
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := "face\nturns slowly\nclose-up\nmoonlight\nfilm"
	var versions []models.PromptVersion
	db.Where("project_id = ? AND entity_type = ? AND entity_id = ?", project.ID, "shot", shots[0].ID).Find(&versions)
	if len(versions) != 1 || versions[0].Action != "manual" || versions[0].Content != want {
		t.Fatalf("manual prompt versions = %+v, want content %q", versions, want)
	}

	if _, err := svc.ReplaceShots(scene.ID, shots); err != nil {
		t.Fatal(err)
	}
	var count int64
	db.Model(&models.PromptVersion{}).Where("project_id = ? AND entity_type = ? AND entity_id = ?", project.ID, "shot", shots[0].ID).Count(&count)
	if count != 1 {
		t.Fatalf("same prompt produced %d versions", count)
	}

	if _, err := svc.UpdateShot(shots[0].ID, map[string]any{"prompt_style": "noir"}); err != nil {
		t.Fatal(err)
	}
	db.Model(&models.PromptVersion{}).Where("project_id = ? AND entity_type = ? AND entity_id = ?", project.ID, "shot", shots[0].ID).Count(&count)
	if count != 2 {
		t.Fatalf("changed prompt produced %d versions", count)
	}
}

func TestShotServiceRemovalCleansAssociationsAndRetainsPromptHistory(t *testing.T) {
	db := newTestDBWithNewModels(t)
	project := models.Project{Title: "cleanup"}
	db.Create(&project)
	scene := models.Scene{ProjectID: project.ID, Order: 1}
	db.Create(&scene)
	svc := NewShotService(db)
	shots, err := svc.ReplaceShots(scene.ID, []models.Shot{
		{ShotType: "wide", Duration: 3, PromptSubject: "first"},
		{ShotType: "close", Duration: 3, PromptSubject: "second"},
	})
	if err != nil {
		t.Fatal(err)
	}
	character := models.Character{ProjectID: project.ID, Name: "actor"}
	if err := db.Create(&character).Error; err != nil {
		t.Fatal(err)
	}
	for i, shot := range shots {
		look := models.CharacterLook{ProjectID: project.ID, CharacterID: character.ID, Name: fmt.Sprintf("look-%d", i)}
		outfit := models.CharacterOutfit{ProjectID: project.ID, CharacterID: character.ID, Name: fmt.Sprintf("outfit-%d", i)}
		if err := db.Create(&look).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&outfit).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&models.ShotCharacterLook{ShotID: shot.ID, LookID: look.ID}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&models.ShotCharacterOutfit{ShotID: shot.ID, CharacterID: character.ID, OutfitID: outfit.ID}).Error; err != nil {
			t.Fatal(err)
		}
	}

	if _, err := svc.ReplaceShots(scene.ID, []models.Shot{shots[0]}); err != nil {
		t.Fatal(err)
	}
	assertShotReferences := func(shotID uint, wantAssociations, wantVersions int64) {
		t.Helper()
		var looks, outfits, versions int64
		db.Model(&models.ShotCharacterLook{}).Where("shot_id = ?", shotID).Count(&looks)
		db.Model(&models.ShotCharacterOutfit{}).Where("shot_id = ?", shotID).Count(&outfits)
		db.Model(&models.PromptVersion{}).Where("project_id = ? AND entity_type = 'shot' AND entity_id = ?", project.ID, shotID).Count(&versions)
		if looks != wantAssociations || outfits != wantAssociations || versions != wantVersions {
			t.Fatalf("shot %d refs: looks=%d outfits=%d versions=%d", shotID, looks, outfits, versions)
		}
	}
	assertShotReferences(shots[1].ID, 0, 1)
	assertShotReferences(shots[0].ID, 1, 1)

	if err := svc.DeleteShot(shots[0].ID); err != nil {
		t.Fatal(err)
	}
	assertShotReferences(shots[0].ID, 0, 1)
}

func TestShotServiceDeleteUpdatesCount(t *testing.T) {
	db := newTestDBWithNewModels(t)
	scene := models.Scene{ProjectID: 1, EpisodeN: 1, Order: 1}
	db.Create(&scene)
	svc := NewShotService(db)
	shots, _ := svc.ReplaceShots(scene.ID, []models.Shot{{ShotType: "medium", Duration: 3}})
	if err := svc.DeleteShot(shots[0].ID); err != nil {
		t.Fatal(err)
	}
	db.First(&scene, scene.ID)
	if scene.ShotCount != 0 {
		t.Fatalf("shot count=%d", scene.ShotCount)
	}
}

func TestValidateShotEnforces3To15Seconds(t *testing.T) {
	tests := []struct {
		duration float64
		wantErr  bool
	}{
		{2.9, true},
		{3.0, false},
		{8.0, false},
		{15.0, false},
		{15.1, true},
		{0, true},
		{-1, true},
	}
	for _, tt := range tests {
		shot := models.Shot{ShotType: "wide", Duration: tt.duration}
		err := validateShot(&shot)
		if (err != nil) != tt.wantErr {
			t.Errorf("validateShot(duration=%.1f) error=%v, wantErr=%v", tt.duration, err, tt.wantErr)
		}
	}
}

func TestAssignShotDialogueRangesRejectsMismatchedText(t *testing.T) {
	db := newTestDBWithNewModels(t)
	project := models.Project{Title: "mismatch"}
	db.Create(&project)
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1}
	db.Create(&scene)
	db.Create(&models.Dialogue{ProjectID: project.ID, SceneID: scene.ID, Order: 1, Character: "甲", Text: "你好世界"})
	// Shot dialogue concatenates to "你好" but structured dialogue is "你好世界" — mismatch.
	shots := []models.Shot{{ShotType: "wide", Duration: 3, Dialogue: "你好"}, {ShotType: "close", Duration: 3, Dialogue: ""}}
	err := newDBTx(db, scene.ID, shots)
	if err == nil || !strings.Contains(err.Error(), "不一致") {
		t.Fatalf("expected mismatch error, got: %v", err)
	}
}

func newDBTx(db *gorm.DB, sceneID uint, shots []models.Shot) error {
	var result error
	db.Transaction(func(tx *gorm.DB) error {
		result = assignShotDialogueRanges(tx, sceneID, shots)
		return result
	})
	return result
}

func TestShotActionTimelineValidationAndPersistence(t *testing.T) {
	db := newTestDBWithNewModels(t)
	project := models.Project{Title: "timeline"}
	db.Create(&project)
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1}
	db.Create(&scene)
	timeline := []models.ShotActionTimelineEntry{
		{Start: 0, End: 4, Subject: "女主", Action: "走到桌边", State: "停在桌边", Camera: "固定中景"},
		{Start: 4, End: 8, Subject: "女主", Action: "拿起信件展开", State: "视线落在信上", Camera: "缓慢推近"},
	}
	shots, err := NewShotService(db).ReplaceShots(scene.ID, []models.Shot{{ShotType: "中景", Duration: 8, ActionTimeline: timeline}})
	if err != nil {
		t.Fatal(err)
	}
	if len(shots) != 1 || len(shots[0].ActionTimeline) != 2 || shots[0].ActionTimeline[1].End != 8 {
		t.Fatalf("timeline not persisted: %+v", shots)
	}
	bad := shots[0]
	bad.ActionTimeline[1].Start = 5
	if err := validateShot(&bad); err == nil || !strings.Contains(err.Error(), "连续") {
		t.Fatalf("gap must be rejected: %v", err)
	}
}

func TestRebalanceShotDurationsScalesActionTimeline(t *testing.T) {
	shots := []models.Shot{{Duration: 6, ActionTimeline: []models.ShotActionTimelineEntry{
		{Start: 0, End: 3, Subject: "甲", Action: "起身", State: "站立", Camera: "固定"},
		{Start: 3, End: 6, Subject: "甲", Action: "走向门口", State: "抵达门口", Camera: "跟随"},
	}}}
	got, err := rebalanceShotDurations(shots, 12)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].ActionTimeline[0].End != 6 || got[0].ActionTimeline[1].Start != 6 || got[0].ActionTimeline[1].End != 12 {
		t.Fatalf("timeline was not scaled: %+v", got[0].ActionTimeline)
	}
}
