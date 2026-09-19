package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"comfyui-console/internal/models"
)

func TestModelCatalogDerivesCurrentTemplateCapabilities(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Template{}); err != nil {
		t.Fatal(err)
	}
	if err := InitSystemTemplates(db); err != nil {
		t.Fatal(err)
	}

	entries, err := NewModelCatalogService(db).List()
	if err != nil {
		t.Fatal(err)
	}
	byCode := make(map[string]ModelCatalogEntry, len(entries))
	for _, entry := range entries {
		byCode[entry.TemplateCode] = entry
	}

	ref := byCode["minimax_h3_ref2v"]
	if ref.Mode != "reference-to-video" || ref.MaxReferences != 9 || !ref.SupportsAudio {
		t.Fatalf("ref2v capabilities = %+v", ref)
	}
	if ref.Duration == nil || value(ref.Duration.Min) != 3 || value(ref.Duration.Max) != 15 || value(ref.Duration.Default) != 5 {
		t.Fatalf("ref2v duration = %+v", ref.Duration)
	}

	firstLast := byCode["minimax_h3_first_last"]
	if firstLast.Mode != "first-last-frame-to-video" || !firstLast.SupportsFirstFrame || !firstLast.SupportsLastFrame {
		t.Fatalf("first/last capabilities = %+v", firstLast)
	}
	if len(firstLast.Resolutions) != 1 || value(firstLast.Resolutions[0].Width.Default) != 1280 || value(firstLast.Resolutions[0].Height.Default) != 704 {
		t.Fatalf("first/last resolutions = %+v", firstLast.Resolutions)
	}
	if firstLast.DefaultParams["fps"] != float64(24) || firstLast.DefaultParams["steps"] != float64(20) {
		t.Fatalf("first/last defaults = %+v", firstLast.DefaultParams)
	}

	t2v := byCode["minimax_h3_t2v"]
	if t2v.Mode != "text-to-video" || t2v.SupportsFirstFrame || t2v.SupportsLastFrame || !t2v.SupportsAudio {
		t.Fatalf("t2v capabilities = %+v", t2v)
	}

	image := byCode["krea2_character_portrait"]
	if image.Mode != "text-to-image" || image.Duration != nil || image.SupportsAudio {
		t.Fatalf("image capabilities = %+v", image)
	}
}

func TestModelCatalogOnlyReturnsEnabledTemplatesAndRejectsBadMetadata(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Template{}); err != nil {
		t.Fatal(err)
	}
	templates := []models.Template{
		{Name: "enabled", Code: "enabled", Enabled: true, InputsJSON: `[]`, WorkflowJSON: `{}`},
		{Name: "disabled", Code: "disabled", Enabled: false, InputsJSON: `[]`, WorkflowJSON: `{}`},
	}
	if err := db.Create(&templates).Error; err != nil {
		t.Fatal(err)
	}
	entries, err := NewModelCatalogService(db).List()
	if err != nil || len(entries) != 1 || entries[0].TemplateCode != "enabled" {
		t.Fatalf("entries=%+v err=%v", entries, err)
	}
	if err := db.Model(&templates[0]).Update("inputs_json", "not-json").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := NewModelCatalogService(db).List(); err == nil {
		t.Fatal("invalid template metadata was silently accepted")
	}
}

func TestHandleListModelCatalogResponse(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Template{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Template{
		Name: "video", Code: "video", Enabled: true,
		InputsJSON:   `[{"key":"duration","type":"float","default":5,"min":3,"max":15}]`,
		WorkflowJSON: `{"1":{"class_type":"AudioVideoNode"}}`,
	}).Error; err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/model-catalog", (&Service{DB: db}).HandleListModelCatalog)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/model-catalog", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Items []ModelCatalogEntry `json:"items"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Items) != 1 || body.Items[0].TemplateCode != "video" || body.Items[0].Mode != "text-to-video" || !body.Items[0].SupportsAudio {
		t.Fatalf("response=%+v", body.Items)
	}
}

func value(number *float64) float64 {
	if number == nil {
		return 0
	}
	return *number
}
