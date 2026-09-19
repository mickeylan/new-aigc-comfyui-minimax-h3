package service

import (
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

func TestStoryboardSubmissionIncludesSceneNegativePrompt(t *testing.T) {
	scene := &models.Scene{Content: "角色站在殿内", ImagePrompt: "角色抬头看向门口", NegativePrompt: "现代汽车, 水印"}
	prompt := buildH3StoryboardPrompt(scene, &models.Project{Style: "真人写实"}, []string{"<Picture 1>：角色参考图"})
	for _, want := range []string{"角色抬头看向门口", "负向硬约束", "现代汽车", "水印"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("submission prompt missing %q: %s", want, prompt)
		}
	}
}

func TestShotSavePreservesSceneFactsAndDuration(t *testing.T) {
	db := newTestDBWithNewModels(t)
	project := models.Project{Title: "p"}
	db.Create(&project)
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1, Content: "权威剧情", ImagePrompt: "权威起始帧", Duration: 9}
	db.Create(&scene)
	_, err := NewShotService(db).ReplaceShots(scene.ID, []models.Shot{{ShotType: "中景", Duration: 2, Description: "导演动作", PromptAction: "抬手"}})
	if err != nil {
		t.Fatal(err)
	}
	db.First(&scene, scene.ID)
	if scene.Content != "权威剧情" || scene.ImagePrompt != "权威起始帧" || scene.Duration != 9 {
		t.Fatalf("Shot overwrote Scene authority: %+v", scene)
	}
}
