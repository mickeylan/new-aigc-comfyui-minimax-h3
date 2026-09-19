package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
	"comfyui-console/internal/service"
)

func TestModelCatalogRoute(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Template{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Template{
		Name: "image", Code: "image", Enabled: true, InputsJSON: `[]`, WorkflowJSON: `{}`,
	}).Error; err != nil {
		t.Fatal(err)
	}

	router := NewRouter(&config.Config{}, &service.Service{DB: db})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/model-catalog", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if got := response.Body.String(); got != `{"items":[{"name":"image","description":"","mode":"text-to-image","template_code":"image","max_references":0,"supports_first_frame":false,"supports_last_frame":false,"supports_audio":false,"resolutions":[],"duration":null,"default_params":{}}]}` {
		t.Fatalf("unexpected body: %s", got)
	}
}
