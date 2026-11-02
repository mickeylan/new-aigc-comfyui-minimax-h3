package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
	"comfyui-console/internal/service"
)

func openPromptPolicyRouteTestDB(t *testing.T, migratePolicy bool) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Project{}); err != nil {
		t.Fatal(err)
	}
	if migratePolicy {
		if err := db.AutoMigrate(&models.PromptPolicyOverride{}); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func promptPolicyRouteRequest(db *gorm.DB, path string) *httptest.ResponseRecorder {
	svc := &service.Service{DB: db}
	router := NewRouter(&config.Config{}, svc)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
	return response
}

func TestListPromptPolicyOverridesHTTP(t *testing.T) {
	db := openPromptPolicyRouteTestDB(t, true)
	project := models.Project{Title: "policy test"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	override := models.PromptPolicyOverride{ProjectID: project.ID, PolicyKey: "image_prompt_polish", Content: "keep detail", Version: 1}
	if err := db.Create(&override).Error; err != nil {
		t.Fatal(err)
	}

	base := fmt.Sprintf("/api/projects/%d/prompt-policy-overrides", project.ID)
	response := promptPolicyRouteRequest(db, base)
	if response.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", response.Code, response.Body.String())
	}
	var rows []models.PromptPolicyOverride
	if err := json.Unmarshal(response.Body.Bytes(), &rows); err != nil {
		t.Fatalf("list must return a JSON array: %v body=%s", err, response.Body.String())
	}
	if len(rows) != 1 || rows[0].ID != override.ID {
		t.Fatalf("unexpected rows: %+v", rows)
	}

	response = promptPolicyRouteRequest(db, base+"?policy_key=INVALID%20KEY")
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"error"`) {
		t.Fatalf("invalid policy status=%d body=%s", response.Code, response.Body.String())
	}

	response = promptPolicyRouteRequest(db, "/api/projects/null/prompt-policy-overrides")
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"error"`) {
		t.Fatalf("null project status=%d body=%s", response.Code, response.Body.String())
	}

	response = promptPolicyRouteRequest(db, "/api/projects/999999/prompt-policy-overrides")
	if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), `"error"`) {
		t.Fatalf("missing project status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestListPromptPolicyOverridesMissingTableReturnsDiagnostic500(t *testing.T) {
	db := openPromptPolicyRouteTestDB(t, false)
	project := models.Project{Title: "missing policy table"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}

	response := promptPolicyRouteRequest(db, fmt.Sprintf("/api/projects/%d/prompt-policy-overrides", project.ID))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("missing table status=%d body=%s", response.Code, response.Body.String())
	}
	var diagnostic struct {
		Error  string `json:"error"`
		Code   string `json:"code"`
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &diagnostic); err != nil {
		t.Fatalf("diagnostic must be JSON: %v body=%s", err, response.Body.String())
	}
	if diagnostic.Code != "prompt_policy_list_failed" || diagnostic.Error == "" || diagnostic.Detail == "" {
		t.Fatalf("incomplete diagnostic: %+v", diagnostic)
	}
}
