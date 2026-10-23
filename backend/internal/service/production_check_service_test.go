package service

import (
	"testing"

	"comfyui-console/internal/models"
)

func TestProductionCheckIsProjectScopedAndReturnsNoSensitiveContent(t *testing.T) {
	db := safetyDB(t, &models.SceneContinuity{}, &models.MusicCueSheet{})
	project := models.Project{Title: "p"}
	other := models.Project{Title: "other"}
	db.Create(&project)
	db.Create(&other)
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1, Duration: 2, PromptStale: true, Content: "secret-story", ImagePrompt: "secret-prompt"}
	db.Create(&scene)
	otherScene := models.Scene{ProjectID: other.ID, EpisodeN: 1, Order: 1, Duration: 8, Error: "other-project-secret"}
	db.Create(&otherScene)
	dialogue := models.Dialogue{ProjectID: project.ID, SceneID: scene.ID, Text: "secret-dialogue", AudioStale: true}
	db.Create(&dialogue)
	result, err := NewProductionCheckService(db).Check(project.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if result.Ready {
		t.Fatal("stale prompt should block readiness")
	}
	if len(result.Findings) != 5 {
		t.Fatalf("unexpected findings: %+v", result.Findings)
	}
	for _, finding := range result.Findings {
		if finding.EntityID == otherScene.ID {
			t.Fatalf("cross-project finding leaked: %+v", finding)
		}
	}
}
