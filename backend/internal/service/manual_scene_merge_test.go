package service

import (
	"errors"
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

func TestManualSceneMergePreservesOriginalsAndCreatesCompleteNewScene(t *testing.T) {
	db := newTestDBWithNewModels(t)
	if err := db.AutoMigrate(&models.SceneContinuity{}, &models.FrameCandidate{}, &models.GenerationCandidate{}); err != nil {
		t.Fatal(err)
	}
	p := models.Project{Title: "合镜"}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	// Scene ID order deliberately differs from editorial order.
	b := models.Scene{ProjectID: p.ID, EpisodeN: 1, Generation: 1, Order: 2, Duration: 6, Content: "乙答道：“好。”", Characters: "乙", VideoFile: "b.mp4", ImageFile: "b.png", Status: "video_ready"}
	a := models.Scene{ProjectID: p.ID, EpisodeN: 1, Generation: 1, Order: 1, Duration: 5, Content: "甲问道：“走吗？”", Characters: "甲", VideoFile: "a.mp4", ImageFile: "a.png", VideoPromptMasterZH: "原始手工稿", Status: "video_ready"}
	c := models.Scene{ProjectID: p.ID, EpisodeN: 1, Generation: 1, Order: 3, Duration: 5, Content: "后续"}
	for _, row := range []*models.Scene{&b, &a, &c} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	da := models.Dialogue{ProjectID: p.ID, SceneID: a.ID, Order: 1, Character: "甲", Text: "走吗？", TimeSegment: "0—3秒", AudioFile: "voice.wav", Status: "ready"}
	dbb := models.Dialogue{ProjectID: p.ID, SceneID: b.ID, Order: 1, Character: "乙", Text: "好。", TimeSegment: "1—3秒", Status: "pending"}
	for _, row := range []*models.Dialogue{&dbb, &da} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	sa := models.Shot{SceneID: a.ID, Order: 1, Duration: 5, Dialogue: "走吗？", DialogueRanges: []models.ShotDialogueRange{{DialogueID: da.ID, GroupKey: "g", StartRune: 0, EndRune: 3}}}
	sb := models.Shot{SceneID: b.ID, Order: 1, Duration: 6, Dialogue: "好。", DialogueRanges: []models.ShotDialogueRange{{DialogueID: dbb.ID, GroupKey: "g", StartRune: 0, EndRune: 2}}}
	for _, row := range []*models.Shot{&sb, &sa} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	svc := &Service{DB: db}
	preview, err := svc.PreviewManualSceneMerge(p.ID, []uint{b.ID, a.ID})
	if err != nil {
		t.Fatal(err)
	}
	if preview.Duration != 11 || preview.Content != a.Content+"\n\n"+b.Content {
		t.Fatalf("wrong preview %+v", preview)
	}
	var before models.Scene
	db.First(&before, a.ID)
	if before.Archived {
		t.Fatal("preview wrote source")
	}
	merged, err := svc.ApplyManualSceneMerge(p.ID, preview.SceneIDs, preview.SourceHash)
	if err != nil {
		t.Fatal(err)
	}
	if merged.ID == a.ID || merged.ID == b.ID || merged.Duration != 11 || merged.VideoFile != "" || merged.ImageFile != "" || merged.VideoPromptMasterZH != "" {
		t.Fatalf("bad merge projection %+v", merged)
	}
	for _, original := range []models.Scene{a, b} {
		var got models.Scene
		if err := db.First(&got, original.ID).Error; err != nil {
			t.Fatal(err)
		}
		if !got.Archived || got.Content != original.Content || got.VideoFile != original.VideoFile || got.ImageFile != original.ImageFile {
			t.Fatalf("source damaged %+v", got)
		}
	}
	var dubs []models.Dialogue
	if err := db.Where("scene_id=?", merged.ID).Order("`order`").Find(&dubs).Error; err != nil {
		t.Fatal(err)
	}
	if len(dubs) != 2 || dubs[0].Text != "走吗？" || dubs[1].Text != "好。" || dubs[1].TimeSegment != "6—8秒" || dubs[0].AudioFile != "voice.wav" {
		t.Fatalf("dialogue order/timing lost %+v", dubs)
	}
	var copied []models.Shot
	db.Where("scene_id=?", merged.ID).Order("order_num").Find(&copied)
	if len(copied) != 2 || copied[0].DialogueRanges[0].DialogueID != dubs[0].ID || copied[1].DialogueRanges[0].DialogueID != dubs[1].ID || copied[0].DialogueRanges[0].GroupKey == copied[1].DialogueRanges[0].GroupKey {
		t.Fatalf("ranges not safely cloned %+v", copied)
	}
	var oldShot models.Shot
	db.First(&oldShot, sa.ID)
	if oldShot.DialogueRanges[0].DialogueID != da.ID {
		t.Fatal("original shot mutated")
	}
	var tail models.Scene
	db.First(&tail, c.ID)
	if tail.Order != 2 || tail.Archived {
		t.Fatalf("tail order wrong %+v", tail)
	}
}

func TestManualSceneMergeRejectsStaleOrInvalidSelectionWithoutWrites(t *testing.T) {
	db := newTestDBWithNewModels(t)
	svc := &Service{DB: db}
	a := models.Scene{ProjectID: 1, EpisodeN: 1, Generation: 1, Order: 1, Duration: 5, Content: "甲"}
	b := models.Scene{ProjectID: 1, EpisodeN: 1, Generation: 1, Order: 2, Duration: 6, Content: "乙"}
	c := models.Scene{ProjectID: 1, EpisodeN: 1, Generation: 1, Order: 3, Duration: 5}
	for _, row := range []*models.Scene{&a, &b, &c} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, ids := range [][]uint{{a.ID, a.ID}, {a.ID, c.ID}} {
		if _, err := svc.PreviewManualSceneMerge(1, ids); !errors.Is(err, ErrSceneMergeInvalid) {
			t.Fatalf("invalid accepted %v", err)
		}
	}
	if _, err := svc.PreviewManualSceneMerge(2, []uint{a.ID, b.ID}); !errors.Is(err, ErrSceneMergeNotFound) {
		t.Fatal(err)
	}
	preview, err := svc.PreviewManualSceneMerge(1, []uint{a.ID, b.ID})
	if err != nil {
		t.Fatal(err)
	}
	db.Model(&b).Update("content", "人工修改")
	if _, err := svc.ApplyManualSceneMerge(1, preview.SceneIDs, preview.SourceHash); !errors.Is(err, ErrSceneMergeStale) {
		t.Fatalf("stale accepted %v", err)
	}
	var active int64
	db.Model(&models.Scene{}).Where("archived=false").Count(&active)
	if active != 3 {
		t.Fatalf("stale merge changed rows %d", active)
	}
	if !strings.Contains(preview.Content, "甲") {
		t.Fatal("preview omitted source")
	}
}
