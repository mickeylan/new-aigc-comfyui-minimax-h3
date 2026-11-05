package service

import (
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

func TestNewestStoryAuthorityIgnoresRetainedDirectorShots(t *testing.T) {
	ps := newTestProjectService(t)
	if err := ps.db.AutoMigrate(&models.Shot{}, &models.Dialogue{}); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "newest story"}
	if err := ps.db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	scene := models.Scene{
		ProjectID: project.ID,
		Content:   "旧剧情：甲在码头打开红伞。",
		Duration:  6,
		ImageFile: "retained.png",
		VideoFile: "retained.mp4",
	}
	if err := ps.db.Create(&scene).Error; err != nil {
		t.Fatal(err)
	}
	oldShot := models.Shot{SceneID: scene.ID, Order: 1, Description: "甲在码头打开红伞", PromptAction: "打开红伞"}
	if err := ps.db.Create(&oldShot).Error; err != nil {
		t.Fatal(err)
	}

	const newest = "新剧情：乙独自在山顶放下竹篮。"
	if err := ps.UpdateScene(&scene, "", newest, "", 6); err != nil {
		t.Fatal(err)
	}
	var saved models.Scene
	if err := ps.db.First(&saved, scene.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !saved.DirectorSourceStale || saved.ImageFile != "retained.png" || saved.VideoFile != "retained.mp4" {
		t.Fatalf("update must stale director source while retaining media: %+v", saved)
	}
	var shotCount int64
	if err := ps.db.Model(&models.Shot{}).Where("scene_id = ?", scene.ID).Count(&shotCount).Error; err != nil || shotCount != 1 {
		t.Fatalf("historical shot should remain, count=%d err=%v", shotCount, err)
	}

	provider := &captureTextProvider{response: "[Shot 1] 乙 sets the bamboo basket down and looks across the summit while the camera slowly pushes in."}
	ps.textProvider = provider
	if _, err := ps.GenerateSceneVideoAction(&saved); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(provider.user, newest) {
		t.Fatalf("request must contain newest story authority: %s", provider.user)
	}
	for _, old := range []string{"甲在码头", "打开红伞"} {
		if strings.Contains(provider.user, old) {
			t.Fatalf("request leaked stale director fact %q: %s", old, provider.user)
		}
	}

	provider.response = "not a valid shot response"
	fallback, err := ps.GenerateSceneVideoAction(&saved)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(fallback, "码头") || strings.Contains(fallback, "红伞") {
		t.Fatalf("invalid-provider fallback must not reuse stale shots: %s", fallback)
	}
}
