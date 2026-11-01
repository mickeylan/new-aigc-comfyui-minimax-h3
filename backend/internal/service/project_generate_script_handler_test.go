package service

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
)

func generateScriptRequest(s *Service, projectID uint, query, body string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/projects/:id/script", s.HandleGenerateScript)
	path := fmt.Sprintf("/api/projects/%d/script%s", projectID, query)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestGenerateScriptRequiresExplicitConsistentEpisodeBeforeClaim(t *testing.T) {
	db := newTestDB(t)
	if err := db.AutoMigrate(&models.Project{}, &models.Scene{}); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "safe", Status: "script_done", PipelineStage: "", PipelineEpisode: 7}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	svc := &Service{DB: db, Projects: &ProjectService{db: db}}
	for name, request := range map[string]struct {
		query string
		body  string
	}{
		"missing":  {body: `{}`},
		"invalid":  {query: `?episode_n=nope`, body: `{"episode_n":2}`},
		"conflict": {query: `?episode_n=2`, body: `{"episode_n":1}`},
	} {
		t.Run(name, func(t *testing.T) {
			w := generateScriptRequest(svc, project.ID, request.query, request.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			var after models.Project
			if err := db.First(&after, project.ID).Error; err != nil {
				t.Fatal(err)
			}
			if after.PipelineStage != "" || after.PipelineEpisode != 7 || after.Status != "script_done" {
				t.Fatalf("invalid request claimed or mutated project: %+v", after)
			}
		})
	}
}

func TestClaimManualScriptPersistsRequestedEpisode(t *testing.T) {
	db := newTestDB(t)
	if err := db.AutoMigrate(&models.Project{}); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "safe", Status: "script_done"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	service := &ProjectService{db: db}
	if err := service.ClaimManualScript(&project, 2); err != nil {
		t.Fatal(err)
	}
	var after models.Project
	if err := db.First(&after, project.ID).Error; err != nil {
		t.Fatal(err)
	}
	if after.PipelineStage != "script_manual" || after.PipelineEpisode != 2 || after.Status != "producing" {
		t.Fatalf("wrong claimed episode: %+v", after)
	}
}
