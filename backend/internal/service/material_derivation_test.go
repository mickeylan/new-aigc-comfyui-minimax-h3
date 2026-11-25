package service

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func materialDerivationFixture(t *testing.T) string {
	t.Helper()
	doc := map[string]any{
		"version": 1, "kind": "studio-script-shot-video", "artifactId": "00000000-0000-4000-8000-000000000001",
		"container": "webm", "codec": "VP9", "width": 1280, "height": 720, "fps": 1, "frameCount": 1,
		"durationMs": 1000, "sourceDurationMs": 1000, "timesMs": []float64{0}, "sourceFingerprint": "scene-test-hash", "mode": "viewport",
		"samplerVersion": "script-shot-timeline-v1", "characterAnimationCatalogRevision": 3,
		"shots":       map[string]any{"version": 1, "shots": []any{map[string]any{"id": "shot-1", "end": 1}}},
		"environment": nil, "walkProof": map[string]any{"policy": "static-xz-reject-v1", "obstacles": []any{}},
	}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestMaterialDerivationHTTPPersistsAndRejectsBeforeWrite(t *testing.T) {
	svc, cfg, root := proxyTestService(t)
	path := filepath.Join(root, "material-provenance.db")
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(&models.Material{}, &models.UploadFile{}); err != nil {
		t.Fatal(err)
	}
	svc.DB = db
	svc.Upload = NewUploadManager(cfg, db, svc.Remote)
	svc.Materials = NewMaterialService(cfg, db, svc.Remote, svc.Upload)
	router := gin.New()
	router.POST("/api/materials", svc.HandleUploadMaterial)
	router.GET("/api/materials", svc.HandleListMaterials)
	upload := func(manifest string) *httptest.ResponseRecorder {
		t.Helper()
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		if err := form.WriteField("type", "video"); err != nil {
			t.Fatal(err)
		}
		if err := form.WriteField("derivation_manifest", manifest); err != nil {
			t.Fatal(err)
		}
		file, err := form.CreateFormFile("file", "previs.webm")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = file.Write([]byte("synthetic-video")); err != nil {
			t.Fatal(err)
		}
		if err = form.Close(); err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest("POST", "/api/materials", &body)
		req.Header.Set("Content-Type", form.FormDataContentType())
		out := httptest.NewRecorder()
		router.ServeHTTP(out, req)
		return out
	}
	if response := upload(`{"version":99}`); response.Code != 400 {
		t.Fatalf("bad upload status=%d body=%s", response.Code, response.Body.String())
	}
	for _, table := range []any{&models.Material{}, &models.UploadFile{}} {
		var count int64
		if err := db.Model(table).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("invalid upload wrote rows: %d %v", count, err)
		}
	}
	files := 0
	_ = filepath.WalkDir(svc.Upload.InputDir(), func(_ string, item os.DirEntry, err error) error {
		if err == nil && !item.IsDir() {
			files++
		}
		return nil
	})
	if files != 0 {
		t.Fatal("invalid manifest wrote files")
	}
	good := materialDerivationFixture(t)
	response := upload(good)
	if response.Code != 200 {
		t.Fatalf("upload status=%d body=%s", response.Code, response.Body.String())
	}
	var mat models.Material
	if err := json.Unmarshal(response.Body.Bytes(), &mat); err != nil {
		t.Fatal(err)
	}
	if mat.DerivationManifest != good || mat.ID == 0 {
		t.Fatal("HTTP response lost manifest")
	}
	if response := upload(""); response.Code != 200 {
		t.Fatal("legacy upload rejected")
	}
	out := httptest.NewRecorder()
	router.ServeHTTP(out, httptest.NewRequest("GET", "/api/materials?type=video", nil))
	var rows []models.Material
	if err := json.Unmarshal(out.Body.Bytes(), &rows); err != nil || len(rows) != 2 || rows[0].DerivationManifest != "" || rows[1].DerivationManifest != good {
		t.Fatalf("list did not retain snapshots: %v", err)
	}
	sql, _ := db.DB()
	if err := sql.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { connection, _ := reopened.DB(); _ = connection.Close() }()
	var saved models.Material
	if err := reopened.First(&saved, mat.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.DerivationManifest != good {
		t.Fatal("reopen lost immutable audit snapshot")
	}
	t.Log("MATERIAL_DERIVATION=PASS upload=200 list=retained reopen=retained invalid=400 writes=0 legacy=200")
}

func TestMaterialDerivationValidation(t *testing.T) {
	good := materialDerivationFixture(t)
	if got, err := validateMaterialDerivation(good, "video"); err != nil || got != good {
		t.Fatalf("valid snapshot lost: %v", err)
	}
	if got, err := validateMaterialDerivation("", "image"); err != nil || got != "" {
		t.Fatal("legacy upload must remain compatible")
	}
	bad := []string{
		`null`, `{}`, good + `{}`, strings.Repeat(" ", maxMaterialDerivationBytes) + good,
		strings.Replace(good, `"version":1`, `"version":99`, 1),
		strings.Replace(good, `"width":1280`, `"width":1`, 1),
		strings.Replace(good, `"timesMs":[0]`, `"timesMs":[1000]`, 1),
		strings.Replace(good, `"frameCount":1`, `"frameCount":2881`, 1),
		strings.Replace(good, `"artifactId":"00000000-0000-4000-8000-000000000001"`, `"artifactId":"not-a-uuid"`, 1),
		strings.Replace(good, `"sourceDurationMs":1000`, `"sourceDurationMs":2000`, 1),
	}
	for i, raw := range bad {
		if _, err := validateMaterialDerivation(raw, "video"); err == nil {
			t.Fatalf("bad snapshot %d accepted", i)
		}
	}
	if _, err := validateMaterialDerivation(good, "image"); err == nil {
		t.Fatal("non-video provenance accepted")
	}
	for _, size := range [][2]int{{1280, 720}, {720, 1280}, {960, 960}, {1152, 768}, {768, 1152}, {1280, 1280}, {720, 720}, {1, 720}, {1280, 721}} {
		var doc map[string]any
		if err := json.Unmarshal([]byte(good), &doc); err != nil {
			t.Fatal(err)
		}
		doc["width"], doc["height"] = size[0], size[1]
		data, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		valid := size[0]*size[1] <= 1280*720 && size != [2]int{720, 720} && size[0] != 1
		got, err := validateMaterialDerivation(string(data), "video")
		if valid && (err != nil || got != string(data)) {
			t.Fatalf("scene frame %v rejected or lost: %v", size, err)
		}
		if !valid && err == nil {
			t.Fatalf("unsupported size %v accepted", size)
		}
	}
	t.Log("MATERIAL_FRAME_SIZES=PASS accepted=5 rejected=4 manifest=retained")
}
