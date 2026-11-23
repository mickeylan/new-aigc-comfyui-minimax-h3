package service

import (
	"bytes"
	"comfyui-console/internal/models"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"testing"
)

func TestSceneArchiveProjectValidation(t *testing.T) {
	r, s, _ := newSceneArchiveRouter(t)
	if err := s.DB.AutoMigrate(&models.Project{}); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "存档项目"}
	if err := s.DB.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	upload := func(projectID string) int {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		part, err := writer.CreateFormFile("file", "test.json")
		if err != nil {
			t.Fatal(err)
		}
		part.Write([]byte("{}"))
		writer.WriteField("project_id", projectID)
		writer.Close()
		q := httptest.NewRequest("POST", "/api/scene-archives", body)
		q.Header.Set("Content-Type", writer.FormDataContentType())
		w := httptest.NewRecorder()
		r.ServeHTTP(w, q)
		return w.Code
	}
	if code := upload("999999"); code != 400 {
		t.Fatal(code)
	}
	if code := upload(fmt.Sprint(project.ID)); code != 201 {
		t.Fatal(code)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/scene-archives?project_id=0&page=1&page_size=10", nil))
	var result struct{ Total int }
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Total != 0 {
		t.Fatal("unassigned filter leaked", w.Body.String())
	}
}

func TestSceneArchiveManagement(t *testing.T) {
	r, s, _ := newSceneArchiveRouter(t)
	r.PATCH("/api/scene-archives/:id", s.HandleRenameSceneArchive)
	r.DELETE("/api/scene-archives/:id", s.HandleDeleteSceneArchive)
	var first models.SceneArchive
	for i := 0; i < 3; i++ {
		a := models.SceneArchive{Name: fmt.Sprint(i), Size: 2}
		if err := s.DB.Create(&a).Error; err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = a
		}
	}
	if err := os.MkdirAll(s.SceneArchiveDir(), 0755); err != nil {
		t.Fatal(err)
	}
	path := s.sceneArchivePath(first.ID)
	if err := os.WriteFile(path, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	request := func(method, url, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		q := httptest.NewRequest(method, url, bytes.NewBufferString(body))
		q.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, q)
		return w
	}
	w := request("GET", "/api/scene-archives?page=2&page_size=2", "")
	var page struct {
		Items []models.SceneArchive
		Total int
	}
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || page.Total != 3 || len(page.Items) != 1 || page.Items[0].ID != first.ID {
		t.Fatalf("pagination: %d %s", w.Code, w.Body.String())
	}
	for _, query := range []string{"page=0", "page_size=101", "page=x"} {
		if w := request("GET", "/api/scene-archives?"+query, ""); w.Code != 400 {
			t.Fatal(query, w.Code)
		}
	}
	url := fmt.Sprintf("/api/scene-archives/%d", first.ID)
	if w := request("PATCH", url, `{"name":"新名称"}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request("PATCH", url, `{"name":" "}`); w.Code != 400 {
		t.Fatal(w.Code)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "{}" {
		t.Fatal("rename changed bytes", err)
	}
	if w := request("DELETE", url, ""); w.Code != 204 {
		t.Fatal(w.Code)
	}
	if w := request("GET", url, ""); w.Code != 404 {
		t.Fatal(w.Code)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("deleted source bytes", err)
	}
	var recycled models.SceneArchive
	if err := s.DB.Unscoped().First(&recycled, first.ID).Error; err != nil || !recycled.DeletedAt.Valid {
		t.Fatal("not recoverable", err)
	}
	if w := request("PATCH", url, `{"name":"no"}`); w.Code != 404 {
		t.Fatal(w.Code)
	}
	if w := request("DELETE", url, ""); w.Code != 404 {
		t.Fatal(w.Code)
	}
}
