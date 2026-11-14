package service

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
)

func TestModelCapabilityLimitsKeepCurrentValues(t *testing.T) {
	for name, got := range map[string]float64{
		"segment": h3SegmentMaxSeconds, "scene video": maxSceneVideoDuration, "native": maxNativeSceneDuration,
		"shot": maxShotDuration, "scene min": minSceneVideoDuration, "long scene": maxLongSceneVideoDuration,
	} {
		want := 15.0
		switch name {
		case "scene min":
			want = 3
		case "long scene":
			want = 60
		}
		if got != want {
			t.Errorf("%s=%v want %v", name, got, want)
		}
	}
	if maxSceneReferenceImages != 9 {
		t.Fatalf("scene reference images=%d", maxSceneReferenceImages)
	}
}

func TestModelCapabilitiesHTTPReflectsConfiguredH3Duration(t *testing.T) {
	db := newTestDB(t)
	if err := db.AutoMigrate(&models.Setting{}); err != nil {
		t.Fatal(err)
	}
	db.Create(&models.Setting{Key: settingH3MaxDurationSeconds, Value: "20"})
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/model-capabilities", (&Service{DB: db}).HandleModelCapabilities)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/model-capabilities", nil))
	var got ModelCapabilityLimits
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	want := ModelCapabilityLimits{ShotMaxSeconds: 15, SceneMinSeconds: 3, SceneMaxSeconds: 60, H3GenerationMaxSeconds: 20, SceneReferenceImages: 9, H3ReferenceImages: 8}
	if w.Code != 200 || got != want {
		t.Fatalf("status=%d got=%+v", w.Code, got)
	}
}
