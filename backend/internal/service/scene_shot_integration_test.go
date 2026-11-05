package service

import (
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

func TestStoryboardSubmissionExcludesStaticAndNegativeConstraintNoise(t *testing.T) {
	scene := &models.Scene{Content: "角色站在殿内", ImagePrompt: "角色抬头看向门口\n静态候选帧硬约束：五帧候选批次保持同一瞬间、同一姿势。\n负向硬约束：画面中禁止出现现代汽车、水印、额外人物。\n禁止额外人物，禁止字幕水印，禁止口型错位。", NegativePrompt: "现代汽车, 水印"}
	prompt := buildH3StoryboardPrompt(scene, &models.Project{Style: "真人写实"}, []string{"<Picture 1>：角色参考图"})
	if !strings.Contains(prompt, "角色抬头看向门口") {
		t.Fatalf("positive storyboard content missing: %s", prompt)
	}
	for _, forbidden := range []string{"静态候选帧硬约束", "五帧候选批次", "负向硬约束", "现代汽车", "字幕水印", "口型错位"} {
		if strings.Contains(prompt, forbidden) {
			t.Fatalf("storyboard constraint noise retained %q: %s", forbidden, prompt)
		}
	}
}

func TestShotSavePreservesSceneFactsAndDuration(t *testing.T) {
	db := newTestDBWithNewModels(t)
	project := models.Project{Title: "p"}
	db.Create(&project)
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1, Content: "权威剧情", ImagePrompt: "权威起始帧", Duration: 9}
	db.Create(&scene)
	_, err := NewShotService(db).ReplaceShots(scene.ID, []models.Shot{{ShotType: "中景", Duration: 3, Description: "导演动作", PromptAction: "抬手"}})
	if err != nil {
		t.Fatal(err)
	}
	db.First(&scene, scene.ID)
	if scene.Content != "权威剧情" || scene.ImagePrompt != "权威起始帧" || scene.Duration != 9 {
		t.Fatalf("Shot overwrote Scene authority: %+v", scene)
	}
}
