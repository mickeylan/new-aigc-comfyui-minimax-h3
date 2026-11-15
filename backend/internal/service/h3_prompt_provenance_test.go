package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
)

func TestVideoSnapshotCopiesScenePromptProvenance(t *testing.T) {
	db := newTestDBWithNewModels(t)
	if err := db.AutoMigrate(&models.GenerationSnapshot{}); err != nil {
		t.Fatal(err)
	}
	projects := &ProjectService{db: db}
	provenance := `{"source":"combat_skill_repair_1","skills":[{"skill_code":"h3-combat","skill_version":8,"input_sha256":"abc"}]}`
	sc := models.Scene{ProjectID: 1, VideoPromptProvenance: provenance}
	db.Create(&sc)
	tpl := &models.Template{Code: "minimax_h3_ref2v"}
	projects.recordGenerationSnapshot(&sc, "video", &models.Task{TaskID: "v"}, tpl, nil)
	projects.recordGenerationSnapshot(&sc, "image", &models.Task{TaskID: "i"}, tpl, nil)
	var rows []models.GenerationSnapshot
	db.Order("id").Find(&rows)
	if len(rows) != 2 || rows[0].PromptProvenanceJSON != provenance || rows[1].PromptProvenanceJSON != "" {
		t.Fatalf("video snapshot must copy the Scene provenance and image must not: %+v", rows)
	}
}

func TestRegenerateVideoPromptStoresProvenanceOnScene(t *testing.T) {
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"content":"[Shot 1] opening action. [Shot 2] At 00:00.800, second action. [Shot 3] At 00:02.000, third action. [Shot 4] At 00:03.200, fourth action. [Shot 5] At 00:05.000, final action with a door click."}}]}`)
	}))
	defer ai.Close()
	s, db, scene := expandHTTPService(t, ai.URL)
	if err := db.AutoMigrate(&models.Template{}, &models.Episode{}, &models.PromptPolicyOverride{}, &models.GenerationCandidate{}); err != nil {
		t.Fatal(err)
	}
	s.Projects = &ProjectService{db: db, textProvider: s.TextProviderFact}
	scene.Duration = 7
	scene.VideoTemplate = "minimax_h3_first_last"
	scene.Content = "0-0.8秒｜opening\n0.8-2秒｜second\n2-3.2秒｜third\n3.2-5秒｜fourth\n5-7秒｜final"
	if err := db.Save(scene).Error; err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/projects/:id/scenes/:sid/video/prompt/regenerate", s.HandleRegenerateSceneVideoPrompt)
	q := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/projects/%d/scenes/%d/video/prompt/regenerate", scene.ProjectID, scene.ID), strings.NewReader(`{"template":"minimax_h3_first_last"}`))
	q.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, q)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"prompt_provenance"`) {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var saved models.Scene
	db.First(&saved, scene.ID)
	var trace h3PromptTrace
	if err := json.Unmarshal([]byte(saved.VideoPromptProvenance), &trace); err != nil || trace.Source == "" || trace.Skills == nil {
		t.Fatalf("scene provenance=%q err=%v", saved.VideoPromptProvenance, err)
	}
}
