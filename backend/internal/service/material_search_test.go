package service

import (
	"comfyui-console/internal/models"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"testing"
)

func TestMaterialSearchBeforePagingAndCategory(t *testing.T) {
	db := newMaterialPagingDB(t)
	for i := 0; i < 30; i++ {
		name, path := "other", "a.glb"
		if i == 0 {
			name, path = "Old Table_100%", "A.GLB"
		}
		if i == 1 {
			name, path = "Table world", "a.spz"
		}
		if err := db.Create(&models.Material{Name: name, Type: "3d", Path: path}).Error; err != nil {
			t.Fatal(err)
		}
	}
	svc := &Service{Materials: &MaterialService{db: db}}
	cases := []struct {
		url           string
		count, status int
	}{
		{"/api/materials?type=3d&q=Table&category=model&page=1&page_size=24", 1, 200},
		{"/api/materials?type=3d&q=Table&category=splat", 1, 200},
		{"/api/materials?type=3d&q=100%25", 1, 200},
		{"/api/materials?type=3d&q=_", 1, 200},
		{"/api/materials?category=unknown", 0, 400},
		{"/api/materials?type=3d&q=Table&category=model&page=2&page_size=1", 0, 200},
	}
	for _, tt := range cases {
		t.Run(tt.url, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest("GET", tt.url, nil)
			svc.HandleListMaterials(c)
			if rec.Code != tt.status {
				t.Fatalf("HTTP %d: %s", rec.Code, rec.Body.String())
			}
			if rec.Code == 200 {
				var list []models.Material
				if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
					t.Fatal(err)
				}
				if len(list) != tt.count {
					t.Fatalf("count=%d want=%d", len(list), tt.count)
				}
			}
		})
	}
}
