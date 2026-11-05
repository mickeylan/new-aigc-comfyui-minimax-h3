package service

import (
	"encoding/json"
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

func TestManualMergeRecoversReviewedAndBodyOnlySpeechBeforeClearingAnalysis(t *testing.T) {
	db := newShotMaterializationDB(t)
	if err := db.AutoMigrate(&models.Character{}, &models.CharacterAliasCandidate{}); err != nil {
		t.Fatal(err)
	}
	p := models.Project{Title: "对白来源"}
	db.Create(&p)
	a := models.Scene{ProjectID: p.ID, EpisodeN: 1, Generation: 1, Order: 1, Duration: 5, Content: "门外传来“不要过来！”", VideoFile: "a.mp4"}
	analysis := SceneDialogueAnalysis{Source: "ai_reviewed", ContentHash: sceneDialogueContentHash(a.Content), Items: []SceneDialogueAnalysisItem{{Index: 0, Text: "不要过来！", Speaker: "马脸男子", SpeechType: "offscreen_dialogue"}}}
	encoded, _ := json.Marshal(analysis)
	a.DialogueAnalysisJSON = string(encoded)
	b := models.Scene{ProjectID: p.ID, EpisodeN: 1, Generation: 1, Order: 2, Duration: 6, Content: "燕承回答：“我知道了。”", Characters: "燕承", VideoFile: "b.mp4"}
	db.Create(&models.Character{ProjectID: p.ID, Name: "燕承"})
	for _, sc := range []*models.Scene{&a, &b} {
		if err := db.Create(sc).Error; err != nil {
			t.Fatal(err)
		}
	}
	svc := &Service{DB: db}
	preview, err := svc.PreviewManualSceneMerge(p.ID, []uint{a.ID, b.ID})
	if err != nil {
		t.Fatal(err)
	}
	merged, err := svc.ApplyManualSceneMerge(p.ID, preview.SceneIDs, preview.SourceHash)
	if err != nil {
		t.Fatal(err)
	}
	var rows []models.Dialogue
	db.Where("scene_id=?", merged.ID).Order("`order`,id").Find(&rows)
	if len(rows) != 2 || rows[0].Text != "不要过来！" || rows[0].Character != "马脸男子" || rows[0].SpeechType != "offscreen_dialogue" || rows[1].Text != "我知道了。" || rows[1].Character != "燕承" {
		t.Fatalf("merged source speech lost: %+v", rows)
	}
	got := (&ProjectService{db: db}).sceneVideoDialogues(merged)
	if len(got) != 2 || got[0].Character != "马脸男子" || got[1].Character != "燕承" {
		t.Fatalf("downstream dialogue changed: %+v", got)
	}
	if merged.Content != a.Content+"\n\n"+b.Content {
		t.Fatalf("original narrative changed: %s", merged.Content)
	}
}

func TestManualMergeKeepsDatabaseOnlySpeechInNarrativeAndDoesNotReviveExcludedSpeech(t *testing.T) {
	db := newShotMaterializationDB(t)
	db.AutoMigrate(&models.Character{}, &models.CharacterAliasCandidate{})
	p := models.Project{Title: "已保存对白"}
	db.Create(&p)
	a := models.Scene{ProjectID: p.ID, EpisodeN: 1, Order: 1, Duration: 5, Content: "男子开口。"}
	b := models.Scene{ProjectID: p.ID, EpisodeN: 1, Order: 2, Duration: 6, Content: "告示写着“禁止入内”。"}
	db.Create(&a)
	db.Create(&b)
	db.Create(&models.Dialogue{ProjectID: p.ID, SceneID: a.ID, Order: 1, Character: "甲", SpeechType: "offscreen_dialogue", Text: " 原句，别动！", Source: "ai_reviewed"})
	db.Create(&models.Dialogue{ProjectID: p.ID, SceneID: b.ID, Order: 1, Character: "甲", SpeechType: "dialogue", Text: "禁止入内", Source: "user_excluded"})
	svc := &Service{DB: db}
	preview, err := svc.PreviewManualSceneMerge(p.ID, []uint{a.ID, b.ID})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(preview.Content, "甲从画外说道：“ 原句，别动！”") {
		t.Fatalf("preview omitted stored speech: %s", preview.Content)
	}
	merged, err := svc.ApplyManualSceneMerge(p.ID, preview.SceneIDs, preview.SourceHash)
	if err != nil {
		t.Fatal(err)
	}
	got := (&ProjectService{db: db}).sceneVideoDialogues(merged)
	if len(got) != 1 || got[0].Text != " 原句，别动！" || got[0].SpeechType != "offscreen_dialogue" {
		t.Fatalf("excluded/reviewed speech changed %+v", got)
	}
}
