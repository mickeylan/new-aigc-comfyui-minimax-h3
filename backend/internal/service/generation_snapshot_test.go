package service

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
)

func TestGenerationSnapshotHashesInputsAndTracksReferenceChanges(t *testing.T) {
	db := newTestDBWithNewModels(t)
	if err := db.AutoMigrate(&models.GenerationSnapshot{}); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.Storage.AssetDir = t.TempDir()
	projects := &ProjectService{db: db, upload: NewUploadManager(cfg, db, NewRemoteExec(config.RemoteConfig{}))}
	p := models.Project{Title: "p"}
	db.Create(&p)
	sc := models.Scene{ProjectID: p.ID, EpisodeN: 1, Generation: 1, Order: 1}
	db.Create(&sc)
	refDir := filepath.Join(cfg.Storage.AssetDir, fmt.Sprint(p.ID))
	if err := os.MkdirAll(refDir, 0o755); err != nil {
		t.Fatal(err)
	}
	refPath := filepath.Join(refDir, "ref.png")
	if err := os.WriteFile(refPath, []byte("first"), 0o644); err != nil {
		t.Fatal(err)
	}
	tpl := &models.Template{Code: "minimax_h3_ref2v", WorkflowJSON: `{"1":{}}`, InputsJSON: `[]`}
	files := map[string][]FileMeta{"ref_images": {{TaskID: fmt.Sprint(p.ID), Name: "ref.png"}}}

	projects.recordGenerationSnapshot(&sc, "video", &models.Task{TaskID: "t1", Prompt: "韩立拔剑", ParamsJSON: `{"seed":1}`}, tpl, files)
	if err := os.WriteFile(refPath, []byte("second"), 0o644); err != nil {
		t.Fatal(err)
	}
	projects.recordGenerationSnapshot(&sc, "video", &models.Task{TaskID: "t2", Prompt: "韩立拔剑", ParamsJSON: `{"seed":1}`}, tpl, files)

	var rows []models.GenerationSnapshot
	db.Order("id").Find(&rows)
	if len(rows) != 2 {
		t.Fatalf("snapshots=%d", len(rows))
	}
	refs := func(row models.GenerationSnapshot) []generationSnapshotRef {
		var out []generationSnapshotRef
		if err := json.Unmarshal([]byte(row.RefsJSON), &out); err != nil || len(out) != 1 {
			t.Fatalf("refs_json=%s err=%v", row.RefsJSON, err)
		}
		return out
	}
	first, second := refs(rows[0])[0], refs(rows[1])[0]
	if first.Role != "ref_images" || first.File != fmt.Sprintf("%d/ref.png", p.ID) || first.SHA256 != sha256Hex([]byte("first")) {
		t.Fatalf("first ref=%+v", first)
	}
	if second.SHA256 != sha256Hex([]byte("second")) || second.SHA256 == first.SHA256 {
		t.Fatalf("a changed reference image must produce a new hash: %s vs %s", first.SHA256, second.SHA256)
	}
	if rows[0].PromptSHA256 != sha256Hex([]byte("韩立拔剑")) || rows[0].FullPrompt != "韩立拔剑" || rows[0].PromptSHA256 != rows[1].PromptSHA256 {
		t.Fatalf("prompt hash=%s/%s", rows[0].PromptSHA256, rows[1].PromptSHA256)
	}
	if rows[0].TemplateSHA256 != sha256Hex([]byte(tpl.WorkflowJSON+"\x00"+tpl.InputsJSON)) || rows[0].TemplateCode != "minimax_h3_ref2v" || rows[0].ParamsJSON != `{"seed":1}` {
		t.Fatalf("template/params=%+v", rows[0])
	}

	svc := &Service{DB: db}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/projects/:id/scenes/:sid/generation-snapshots", svc.HandleListGenerationSnapshots)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", fmt.Sprintf("/api/projects/%d/scenes/%d/generation-snapshots?kind=video", p.ID, sc.ID), nil))
	var out struct {
		Items []models.GenerationSnapshot `json:"items"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || len(out.Items) != 2 || out.Items[0].TaskID != "t2" {
		t.Fatalf("list status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestGenerationSnapshotFailureDoesNotPanicOrBlock(t *testing.T) {
	db := newTestDBWithNewModels(t)
	// No snapshot table: the write fails and must only be logged.
	projects := &ProjectService{db: db}
	sc := models.Scene{ProjectID: 1}
	projects.recordGenerationSnapshot(&sc, "image", &models.Task{TaskID: "t"}, &models.Template{Code: "x"}, map[string][]FileMeta{"ref_images": {{TaskID: "1", Name: "missing.png"}}})
}
