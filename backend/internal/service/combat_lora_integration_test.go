package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
)

func TestCombatTemplateUsesSinglePowerLoader(t *testing.T) {
	tpl := loadTemplateForTest(t, "minimax_h3_ref2v_combat.json")
	var g map[string]any
	if err := json.Unmarshal([]byte(tpl.WorkflowJSON), &g); err != nil {
		t.Fatal(err)
	}
	loras, err := powerLorasFromWorkflow(g)
	if err != nil || len(loras) != 4 {
		t.Fatalf("loras=%v err=%v", loras, err)
	}
	for _, id := range []string{"157", "158", "159"} {
		if _, ok := g[id]; ok {
			t.Fatalf("legacy node %s retained", id)
		}
	}
	if got := g["334"].(map[string]any)["inputs"].(map[string]any)["model"].([]any)[0]; got != "156" {
		t.Fatalf("final model source=%v", got)
	}
}
func TestCombatLoraHTTPInheritanceOwnershipAndFiles(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newTestDBWithNewModels(t)
	_ = db.AutoMigrate(&models.Project{}, &models.Scene{}, &models.Template{})
	tpl := loadTemplateForTest(t, "minimax_h3_ref2v_combat.json")
	db.Create(&tpl)
	p := models.Project{Title: "p"}
	db.Create(&p)
	sc := models.Scene{ProjectID: p.ID, Order: 1, ImageFile: "keep.png", Status: "video_ready"}
	db.Create(&sc)
	svc := &Service{DB: db}
	call := func(method, body string, handler func(*gin.Context)) (int, CombatLoraResolved) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(method, "/", bytes.NewBufferString(body))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(p.ID)}, {Key: "sid", Value: fmt.Sprint(sc.ID)}}
		handler(c)
		var out CombatLoraResolved
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out
	}
	cfg := CombatLoraConfig{Mode: "custom", Loras: []CombatLora{{true, "x.safetensors", .25}, {false, "y.safetensors", -2}}}
	b, _ := json.Marshal(cfg)
	if code, out := call("PATCH", string(b), svc.HandlePatchProjectCombatLoras); code != 200 || out.Files[1] != "y.safetensors" {
		t.Fatalf("status=%d out=%+v", code, out)
	}
	if code, out := call("PATCH", `{"mode":"inherit"}`, svc.HandlePatchSceneCombatLoras); code != 200 || out.Source != "project" || len(out.Config.Loras) != 2 {
		t.Fatalf("status=%d out=%+v", code, out)
	}
	empty, _ := json.Marshal(CombatLoraConfig{Mode: "custom", Loras: []CombatLora{}})
	if code, out := call("PATCH", string(empty), svc.HandlePatchSceneCombatLoras); code != 200 || out.Source != "scene" || len(out.Config.Loras) != 0 {
		t.Fatalf("status=%d out=%+v", code, out)
	}
	var saved models.Scene
	db.First(&saved, sc.ID)
	if saved.ImageFile != "keep.png" || saved.Status != "video_ready" {
		t.Fatalf("unrelated scene fields changed: %+v", saved)
	}
	other := models.Project{Title: "other"}
	db.Create(&other)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("PATCH", "/", bytes.NewBuffer(b))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(other.ID)}, {Key: "sid", Value: fmt.Sprint(sc.ID)}}
	svc.HandlePatchSceneCombatLoras(c)
	if w.Code != 404 {
		t.Fatalf("cross-project status=%d", w.Code)
	}
}
