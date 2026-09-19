package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
	"comfyui-console/internal/service"
)

func TestSharedAssetReferenceRoutesAndMaterialDeleteConflict(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Project{}, &models.Scene{}, &models.Shot{}, &models.Material{}, &models.SharedAssetReference{}); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "project"}
	db.Create(&project)
	material := models.Material{Name: "global", Type: "image", Source: "scene"}
	db.Create(&material)

	svc := &service.Service{
		DB: db, Materials: service.NewMaterialService(nil, db, nil, nil),
		SharedAssetReferences: service.NewSharedAssetReferenceService(db),
	}
	router := NewRouter(&config.Config{}, svc)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		response := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		router.ServeHTTP(response, req)
		return response
	}

	base := fmt.Sprintf("/api/projects/%d/shared-asset-references", project.ID)
	response := request(http.MethodPost, base, fmt.Sprintf(`{"material_id":%d,"mode":"live"}`, material.ID))
	if response.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", response.Code, response.Body.String())
	}
	var ref models.SharedAssetReference
	if err := json.Unmarshal(response.Body.Bytes(), &ref); err != nil {
		t.Fatal(err)
	}
	response = request(http.MethodGet, base, "")
	if response.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", response.Code, response.Body.String())
	}
	response = request(http.MethodGet, fmt.Sprintf("/api/projects/%d/shared-assets/effective", project.ID), "")
	if response.Code != http.StatusOK {
		t.Fatalf("effective status=%d body=%s", response.Code, response.Body.String())
	}
	response = request(http.MethodDelete, fmt.Sprintf("/api/materials/%d", material.ID), "")
	if response.Code != http.StatusConflict {
		t.Fatalf("referenced material delete status=%d body=%s", response.Code, response.Body.String())
	}
	response = request(http.MethodPut, fmt.Sprintf("%s/%d", base, ref.ID), fmt.Sprintf(`{"material_id":%d,"mode":"live"}`, material.ID))
	if response.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", response.Code, response.Body.String())
	}
	response = request(http.MethodDelete, fmt.Sprintf("%s/%d", base, ref.ID), "")
	if response.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", response.Code, response.Body.String())
	}
}
