package service

import (
	"comfyui-console/internal/models"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEditingDraftHTTPContracts(t *testing.T) {
	db := newTestDB(t)
	if err := db.AutoMigrate(&models.Project{}, &models.EditingDraft{}, &models.Scene{}, &models.Material{}); err != nil {
		t.Fatal(err)
	}
	project := models.Project{ID: 1}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	svc := &Service{DB: db}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/projects/:id/episodes/:number/editing-draft", svc.HandleGetEditingDraft)
	r.PUT("/projects/:id/episodes/:number/editing-draft", svc.HandleSaveEditingDraft)
	request := func(method, url, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, url, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		return w
	}
	base := "/projects/1/episodes/2/editing-draft"
	if w := request("GET", base, ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"draft":null`) {
		t.Fatalf("empty GET: %d %s", w.Code, w.Body)
	}
	for _, body := range []string{`{"edit_sequence":[]}`, `{"revision":0,"edit_sequence":[],"unknown":1}`, `{"revision":0} {}`} {
		if w := request("PUT", base, body); w.Code != 400 {
			t.Fatalf("invalid body: %d %s", w.Code, w.Body)
		}
	}
	if w := request("PUT", base, `{"revision":0,"edit_sequence":[]}`); w.Code != 200 {
		t.Fatalf("save: %d %s", w.Code, w.Body)
	}
	if w := request("PUT", base, `{"revision":0,"edit_sequence":[]}`); w.Code != 409 {
		t.Fatalf("CAS: %d %s", w.Code, w.Body)
	}
	w := request("GET", base, "")
	var response struct {
		Draft models.EditingDraft `json:"draft"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.Draft.Revision != 1 {
		t.Fatalf("roundtrip: %s %v", w.Body, err)
	}
	if w := request("GET", "/projects/999/episodes/2/editing-draft", ""); w.Code != 404 {
		t.Fatalf("missing project: %d", w.Code)
	}
	if w := request("GET", "/projects/1/episodes/0/editing-draft", ""); w.Code != 400 {
		t.Fatalf("bad episode: %d", w.Code)
	}
}

func TestEditingDraftRevisionOwnershipAndNoSceneMutation(t *testing.T) {
	db := newTestDB(t)
	if err := db.AutoMigrate(&models.EditingDraft{}, &models.Scene{}, &models.Material{}); err != nil {
		t.Fatal(err)
	}
	scene := models.Scene{ProjectID: 1, EpisodeN: 2, Content: "canonical", VideoFile: "original.mp4", Status: "video_ready"}
	db.Create(&scene)
	clips := []editingDraftClip{{mergeEditClip: mergeEditClip{SceneID: &scene.ID}, Duration: 10}}
	draft, err := saveEditingDraft(db, 1, 2, 0, clips)
	if err != nil || draft.Revision != 1 {
		t.Fatalf("create: %+v %v", draft, err)
	}
	if _, err = saveEditingDraft(db, 1, 2, 0, clips); !errors.Is(err, errEditingDraftConflict) {
		t.Fatalf("duplicate creation: %v", err)
	}
	draft, err = saveEditingDraft(db, 1, 2, 1, clips)
	if err != nil || draft.Revision != 2 {
		t.Fatalf("update: %v", err)
	}
	if _, err = saveEditingDraft(db, 1, 2, 1, nil); !errors.Is(err, errEditingDraftConflict) {
		t.Fatalf("stale save: %v", err)
	}
	if _, err = saveEditingDraft(db, 2, 2, 0, clips); err == nil {
		t.Fatal("cross project accepted")
	}
	if _, err = saveEditingDraft(db, 1, 3, 0, clips); err == nil {
		t.Fatal("cross episode accepted")
	}
	var got models.Scene
	db.First(&got, scene.ID)
	if got.Content != scene.Content || got.VideoFile != scene.VideoFile || got.Status != scene.Status {
		t.Fatal("draft mutated production scene")
	}
	var stored models.EditingDraft
	db.First(&stored, draft.ID)
	if stored.Revision != 2 || stored.EditSequence != draft.EditSequence {
		t.Fatal("conflict changed stored draft")
	}
}
