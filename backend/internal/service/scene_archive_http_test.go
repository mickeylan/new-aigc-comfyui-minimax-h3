package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
)

func newSceneArchiveRouter(t *testing.T) (*gin.Engine, *Service, string) {
	t.Helper()
	db := newTestDB(t)
	if err := db.AutoMigrate(&models.SceneArchive{}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	s := &Service{DB: db, Cfg: &config.Config{Storage: config.StorageConfig{DataDir: root}}}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/scene-archives", s.HandleCreateSceneArchive)
	r.GET("/api/scene-archives", s.HandleListSceneArchives)
	r.GET("/api/scene-archives/:id", s.HandleGetSceneArchive)
	return r, s, root
}

func multipartSceneArchive(t *testing.T, name string, payload []byte) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return body, writer.FormDataContentType()
}

func TestSceneArchiveUploadListAndRead(t *testing.T) {
	r, s, root := newSceneArchiveRouter(t)
	doc := []byte(`{"version":3,"model":{"name":"scene","data":"AAAA"}}`)

	body, contentType := multipartSceneArchive(t, "scene-1764000000000.json", doc)
	w := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/scene-archives", body)
	request.Header.Set("Content-Type", contentType)
	r.ServeHTTP(w, request)
	if w.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, body = %s", w.Code, w.Body.String())
	}
	var created models.SceneArchive
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == 0 || created.Size != int64(len(doc)) || created.Name != "scene-1764000000000.json" {
		t.Fatalf("created = %+v", created)
	}

	// 存档 JSON 必须原样落盘：文件名只用 id，客户端给的名字进不了路径。
	stored, err := os.ReadFile(filepath.Join(root, "scene-archives", fmt.Sprintf("%d.json", created.ID)))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stored, doc) {
		t.Fatalf("stored = %s", stored)
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/scene-archives", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d", w.Code)
	}
	var listed struct {
		Items []models.SceneArchive `json:"items"`
		Total int                   `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if listed.Total != 1 || len(listed.Items) != 1 || listed.Items[0].ID != created.ID {
		t.Fatalf("listed = %+v", listed)
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/scene-archives/%d", created.ID), nil))
	if w.Code != http.StatusOK {
		t.Fatalf("read status = %d, body = %s", w.Code, w.Body.String())
	}
	if !bytes.Equal(w.Body.Bytes(), doc) {
		t.Fatalf("read body = %s", w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Fatalf("content type = %q", got)
	}

	// 项目过滤：挂在项目 7 的存档不该出现在别的项目列表里。
	if err := s.DB.Model(&models.SceneArchive{}).Where("id = ?", created.ID).Update("project_id", 7).Error; err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/scene-archives?project_id=8", nil))
	if err := json.Unmarshal(w.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if listed.Total != 0 {
		t.Fatalf("project filter leaked: %+v", listed)
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/scene-archives?project_id=7", nil))
	if err := json.Unmarshal(w.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if listed.Total != 1 {
		t.Fatalf("project filter missed: %+v", listed)
	}
}

func TestSceneArchiveRejectsEmptyAndMissing(t *testing.T) {
	r, _, _ := newSceneArchiveRouter(t)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/scene-archives", bytes.NewReader(nil)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("empty upload status = %d", w.Code)
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/scene-archives/424242", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing id status = %d", w.Code)
	}
}

func TestSceneArchiveRejectsOversizeBody(t *testing.T) {
	r, _, root := newSceneArchiveRouter(t)
	oversize := bytes.Repeat([]byte("a"), maxSceneArchiveBytes+1)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/scene-archives", bytes.NewReader(oversize)))
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize status = %d, body = %s", w.Code, w.Body.String())
	}
	entries, err := os.ReadDir(filepath.Join(root, "scene-archives"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("oversize upload left files: %v", entries)
	}
}