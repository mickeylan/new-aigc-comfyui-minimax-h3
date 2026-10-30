package service

import (
	"strings"
	"testing"

	"comfyui-console/internal/models"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestParseSceneContentExpandResult(t *testing.T) {
	got, err := parseSceneContentExpandResult("```json\n{\"content\":\"她停步。\",\"director_note\":\"保持近景\"}\n```")
	if err != nil || got.Content != "她停步。" || got.DirectorNote != "保持近景" {
		t.Fatalf("unexpected result: %#v, %v", got, err)
	}
	if _, err := parseSceneContentExpandResult(`{"content":""}`); err == nil {
		t.Fatal("expected empty content error")
	}
}

func TestSceneContentExpandContextIsScopedAndReadOnly(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Scene{}, &models.Character{}, &models.Asset{}, &models.Dialogue{}); err != nil {
		t.Fatal(err)
	}
	current := models.Scene{ProjectID: 1, EpisodeN: 2, Generation: 3, Order: 5, Title: "逃离", Content: "原正文", Duration: 3.5, SceneMode: "文武混合", VisibleCharacters: "柳乐儿", VoiceCharacters: "旁白", LocationName: "荒地", Props: "火把"}
	db.Create(&current)
	db.Create(&models.Scene{ProjectID: 1, EpisodeN: 2, Generation: 3, Order: 4, Content: "同版本前场"})
	db.Create(&models.Scene{ProjectID: 1, EpisodeN: 2, Generation: 4, Order: 6, Content: "错误版本"})
	db.Create(&models.Scene{ProjectID: 2, EpisodeN: 2, Generation: 3, Order: 6, Content: "其他项目"})
	db.Create(&models.Character{ProjectID: 1, Name: "柳乐儿", Habits: "紧张时攥拳"})
	db.Create(&models.Character{ProjectID: 2, Name: "越权角色"})
	db.Create(&models.Asset{ProjectID: 1, Kind: "location", Name: "荒地", Description: "焦土"})
	db.Create(&models.Dialogue{ProjectID: 1, SceneID: current.ID, Order: 1, Character: "柳乐儿", Text: "别过来"})
	s := &Service{DB: db}
	context, err := s.sceneContentExpandContext(&current)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"同版本前场", "柳乐儿", "荒地", "别过来", "时长：3.5秒", "文武类型：文武混合", "发声人物：旁白", "道具：火把"} {
		if !strings.Contains(context, want) {
			t.Errorf("missing %q in %s", want, context)
		}
	}
	for _, unwanted := range []string{"错误版本", "其他项目", "越权角色"} {
		if strings.Contains(context, unwanted) {
			t.Errorf("leaked %q", unwanted)
		}
	}
	var after models.Scene
	db.First(&after, current.ID)
	if after.Content != "原正文" {
		t.Fatalf("context generation persisted scene: %q", after.Content)
	}
}
