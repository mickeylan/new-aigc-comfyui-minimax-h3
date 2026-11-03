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

func TestRegenerateFL2VAFinalizesFiveShotAuthoritativeTimeline(t *testing.T) {
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
	scene.VideoFullPrompt = "confirmed editor text"
	scene.Content = "0-0.8秒｜opening\n0.8-2秒｜second\n2-3.2秒｜third\n3.2-5秒｜fourth\n5-7秒｜final"
	if err := db.Save(scene).Error; err != nil {
		t.Fatal(err)
	}
	// Deliberately stale DB durations must not override explicit Scene.Content cuts.
	for i, duration := range []float64{2, 2.5, 1, 1, .5} {
		if err := db.Create(&models.Shot{SceneID: scene.ID, Order: i + 1, Duration: duration}).Error; err != nil {
			t.Fatal(err)
		}
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/projects/:id/scenes/:sid/video/prompt/regenerate", s.HandleRegenerateSceneVideoPrompt)
	path := fmt.Sprintf("/api/projects/%d/scenes/%d/video/prompt/regenerate", scene.ProjectID, scene.ID)
	q := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"template":"minimax_h3_first_last"}`))
	q.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, q)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var response struct {
		FullPrompt string `json:"full_prompt"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	prompt := response.FullPrompt
	for _, want := range []string{"Picture 2 (from Shot 5)", "[Shot 2] At 00:00.800", "[Shot 3] At 00:02.000", "[Shot 4] At 00:03.200", "[Shot 5] At 00:05.000", "non_diegetic_music:\nN/A"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("final prompt missing %q: %s", want, prompt)
		}
	}
	if strings.Count(prompt, h3FL2VAOpening) != 1 || strings.Count(prompt, h3FL2VALanding) != 1 || strings.Index(prompt, h3FL2VAOpening) > strings.Index(prompt, "[Shot 2]") || strings.Index(prompt, h3FL2VALanding) < strings.Index(prompt, "[Shot 5]") {
		t.Fatalf("frame anchors are not unique body endpoints: %s", prompt)
	}
	var saved models.Scene
	if err := db.First(&saved, scene.ID).Error; err != nil || saved.VideoFullPrompt != "confirmed editor text" {
		t.Fatalf("regenerate changed confirmed text: prompt=%q err=%v", saved.VideoFullPrompt, err)
	}
}
