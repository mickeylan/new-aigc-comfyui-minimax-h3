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

func TestSceneArchiveSceneAssociation(t *testing.T) {
	r, s, _ := newSceneArchiveRouter(t)
	if err := s.DB.AutoMigrate(&models.Project{}, &models.Scene{}); err != nil {
		t.Fatal(err)
	}
	p, other := models.Project{Title: "来源项目"}, models.Project{Title: "其他项目"}
	s.DB.Create(&p)
	s.DB.Create(&other)
	scene := models.Scene{ProjectID: p.ID, Title: "来源场次"}
	archived := models.Scene{ProjectID: p.ID, Archived: true}
	sibling := models.Scene{ProjectID: p.ID, Title: "同项目其他场次"}
	for _, item := range []*models.Scene{&scene, &archived, &sibling} {
		if err := s.DB.Create(item).Error; err != nil {
			t.Fatal(err)
		}
	}
	upload := func(projectID, sceneID string) *httptest.ResponseRecorder {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		part, err := writer.CreateFormFile("file", "source.json")
		if err != nil {
			t.Fatal(err)
		}
		part.Write([]byte(`{"version":7}`))
		writer.WriteField("project_id", projectID)
		writer.WriteField("scene_id", sceneID)
		writer.Close()
		q := httptest.NewRequest("POST", "/api/scene-archives", body)
		q.Header.Set("Content-Type", writer.FormDataContentType())
		w := httptest.NewRecorder()
		r.ServeHTTP(w, q)
		return w
	}
	pid, sid := fmt.Sprint(p.ID), fmt.Sprint(scene.ID)
	for _, pair := range [][2]string{{"", sid}, {pid, "0"}, {pid, "-1"}, {pid, "1.5"}, {pid, "x"}, {pid, " "}, {pid, "999999"}, {pid, fmt.Sprint(archived.ID)}, {fmt.Sprint(other.ID), sid}} {
		if w := upload(pair[0], pair[1]); w.Code != 400 {
			t.Fatalf("invalid source %v: %d %s", pair, w.Code, w.Body.String())
		}
	}
	w := upload(pid, sid)
	var saved models.SceneArchive
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &saved) != nil || saved.SceneID == nil || *saved.SceneID != scene.ID || saved.ProjectID == nil || *saved.ProjectID != p.ID {
		t.Fatalf("association: %d %s", w.Code, w.Body.String())
	}
	for _, pair := range [][2]string{{pid, ""}, {"", ""}, {pid, fmt.Sprint(sibling.ID)}} {
		if w := upload(pair[0], pair[1]); w.Code != 201 {
			t.Fatalf("legacy/sibling upload: %d %s", w.Code, w.Body.String())
		}
	}
	for _, query := range []string{"scene_id=" + sid, "project_id=0&scene_id=" + sid, "project_id=" + fmt.Sprint(other.ID) + "&scene_id=" + sid, "project_id=" + pid + "&scene_id=" + fmt.Sprint(archived.ID), "project_id=" + pid + "&scene_id=0", "project_id=" + pid + "&scene_id=x"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/api/scene-archives?"+query, nil))
		if w.Code != 400 {
			t.Fatalf("invalid filter %s: %d %s", query, w.Code, w.Body.String())
		}
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/scene-archives?project_id="+pid+"&scene_id="+sid+"&page=1&page_size=10", nil))
	var list struct {
		Items []models.SceneArchive
		Total int
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &list) != nil || list.Total != 1 || len(list.Items) != 1 || list.Items[0].ID != saved.ID {
		t.Fatalf("source filter leaked: %d %s", w.Code, w.Body.String())
	}
	data, err := os.ReadFile(s.sceneArchivePath(saved.ID))
	if err != nil || string(data) != `{"version":7}` {
		t.Fatalf("source changed archive: %s %v", data, err)
	}
	if err := s.DB.Delete(&scene).Error; err != nil {
		t.Fatal(err)
	}
	if w := upload(pid, sid); w.Code != 400 {
		t.Fatalf("deleted scene accepted: %d %s", w.Code, w.Body.String())
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
