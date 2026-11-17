package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

func TestH3ImagePanoramaTemplateRendersTwoToOneDistilledGraph(t *testing.T) {
	tpl := loadTemplateForTest(t, "minimax_h3_image_panorama.json")
	params := map[string]any{"prompt": "panorama", "width": panoramaGenWidth, "height": panoramaGenHeight, "seed": float64(7), "_task_id": "pano-task"}
	if err := normalizeTemplateFiles(&tpl, params, nil); err != nil {
		t.Fatal(err)
	}
	workflow, err := (&TaskService{}).RenderWorkflow(&tpl, params)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(workflow)
	if strings.Contains(string(raw), "{{") {
		t.Fatalf("unrendered placeholder: %s", raw)
	}
	inputs := func(id string) map[string]any { return workflow[id].(map[string]any)["inputs"].(map[string]any) }
	cond, sampler := inputs("5"), inputs("7")
	if workflow["5"].(map[string]any)["class_type"] != "MiniMaxH3ImageConditioning" || fmt.Sprint(cond["width"]) != "5632" || fmt.Sprint(cond["height"]) != "2816" {
		t.Fatalf("conditioning=%v", workflow["5"])
	}
	if fmt.Sprint(sampler["steps"]) != "6" || fmt.Sprint(sampler["cfg"]) != "1" || sampler["sampler_name"] != "euler" || sampler["scheduler"] != "simple" {
		t.Fatalf("sampler=%v", sampler)
	}
	if inputs("1")["unet_name"] != `minimax-h3-image\minimax_h3_image_pruned_int8_convrot.safetensors` ||
		inputs("2")["clip_name"] != `minimax-h3\qwen3vl_32b_minimax_h3_int8_convrot.safetensors` || inputs("2")["type"] != "minimax" ||
		inputs("3")["vae_name"] != `minimax-h3-image\minimax_h3_image_vae_fp16.safetensors` {
		t.Fatalf("model paths: unet=%v clip=%v vae=%v", inputs("1"), inputs("2"), inputs("3"))
	}
	if inputs("9")["filename_prefix"] != "minimax_h3_image/panorama_pano-task" {
		t.Fatalf("save=%v", inputs("9"))
	}
}

func TestBuildPanoramaPromptKeepsLocationFieldsAndTextRule(t *testing.T) {
	p := &models.Project{Style: "国漫"}
	plain := buildPanoramaPrompt(p, &models.Asset{Kind: AssetKindLocation, Name: "青云山门", Description: "白玉台阶通向朱红山门"})
	for _, want := range []string{"360-degree equirectangular panorama", "Style: 国漫插画风格", "Location: 「青云山门」", "follow exactly: 白玉台阶通向朱红山门", "no captions, no text, no collage", "no circular fisheye frame."} {
		if !strings.Contains(plain, want) {
			t.Fatalf("prompt lacks %q:\n%s", want, plain)
		}
	}
	signed := buildPanoramaPrompt(p, &models.Asset{Kind: AssetKindLocation, Name: "酒楼", VisibleText: `醉"仙"楼`})
	if !strings.Contains(signed, `exactly as written, legible, unchanged and untranslated, with no other text: "醉\"仙\"楼"`) || strings.Contains(signed, "no text") {
		t.Fatalf("visible text rule wrong:\n%s", signed)
	}
}

func newPanoramaFixture(t *testing.T) (frameConvertFixture, models.Asset) {
	t.Helper()
	f := newFrameConvertFixture(t)
	if err := f.ps.db.AutoMigrate(&models.Asset{}, &models.AssetVariant{}); err != nil {
		t.Fatal(err)
	}
	tpl := loadTemplateForTest(t, "minimax_h3_image_panorama.json")
	if err := f.ps.db.Create(&tpl).Error; err != nil {
		t.Fatal(err)
	}
	asset := models.Asset{ProjectID: f.source.ProjectID, Kind: AssetKindLocation, Name: "山门", Description: "石阶", Image: "loc_current.png"}
	f.ps.db.Create(&asset)
	f.ps.db.Create(&models.AssetVariant{ProjectID: asset.ProjectID, EntityType: VariantAssetImage, EntityID: asset.ID, File: "loc_current.png", Selected: true})
	return f, asset
}

func TestStartLocationPanoramaSubmitsTextOnlyTaskAndConflictsWhileActive(t *testing.T) {
	f, asset := newPanoramaFixture(t)
	if err := f.ps.StartLocationPanorama(&asset); err != nil {
		t.Fatal(err)
	}
	var saved models.Asset
	f.ps.db.First(&saved, asset.ID)
	var task models.Task
	if err := f.ps.db.Where("task_id = ?", saved.PanoramaTaskID).First(&task).Error; err != nil {
		t.Fatalf("panorama task not recorded: %v", err)
	}
	if !strings.Contains(task.ParamsJSON, `"width":5632`) || !strings.Contains(task.ParamsJSON, `"height":2816`) || !strings.Contains(task.Prompt, "Location: 「山门」") || strings.Contains(task.InputsJSON, "loc_current.png") {
		t.Fatalf("params=%s inputs=%s prompt=%s", task.ParamsJSON, task.InputsJSON, task.Prompt)
	}
	if saved.Image != "loc_current.png" || saved.ImageTaskID != "" {
		t.Fatalf("current reference touched: %+v", saved)
	}

	busy := models.Asset{ProjectID: asset.ProjectID, Kind: AssetKindLocation, Name: "河岸", PanoramaTaskID: "active-pano"}
	f.ps.db.Create(&busy)
	f.ps.db.Create(&models.Task{TaskID: "active-pano", Status: "running"})
	if err := f.ps.StartLocationPanorama(&busy); err != errPanoramaActive {
		t.Fatalf("an active panorama must conflict, err=%v", err)
	}
	if err := f.ps.StartLocationPanorama(&models.Asset{Kind: AssetKindProp, Name: "剑"}); err == nil {
		t.Fatal("props must not get panoramas")
	}
}

// completePanoramaTask marks taskID successful on a fake ComfyUI serving body as its output image.
func completePanoramaTask(t *testing.T, f frameConvertFixture, taskID string, body []byte) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/view" && r.URL.Query().Get("filename") == "pano.png" && r.URL.Query().Get("subfolder") == "minimax_h3_image" {
			w.Write(body)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)
	inst := models.Instance{GPUIndex: 0, Host: "127.0.0.1", Port: comfyTestPort(server), Status: "running"}
	f.ps.db.Create(&inst)
	files, _ := json.Marshal([]map[string]string{{"filename": "pano.png", "subfolder": "minimax_h3_image", "type": "images"}})
	f.ps.db.Create(&models.Task{TaskID: taskID, Status: "success", Port: &inst.Port, InstanceID: &inst.ID, ResultFiles: string(files)})
}

func TestSyncLocationPanoramasSplitsOnceIntoUnselectedVariants(t *testing.T) {
	f, asset := newPanoramaFixture(t)
	completePanoramaTask(t, f, "pano-ok", encodePNG(t, quadrantPanorama(2048, 1024)))
	f.ps.db.Model(&asset).Update("panorama_task_id", "pano-ok")

	if !f.ps.syncLocationPanoramas() {
		t.Fatal("finished panorama must be applied")
	}
	var saved models.Asset
	f.ps.db.First(&saved, asset.ID)
	if saved.PanoramaTaskID != "" || saved.PanoramaError != "" || !strings.HasPrefix(saved.PanoramaImage, "pano_") || saved.Image != "loc_current.png" {
		t.Fatalf("asset=%+v", saved)
	}
	var views []models.AssetVariant
	f.ps.db.Where("entity_id = ? AND provenance LIKE ?", asset.ID, "panorama:%").Order("id").Find(&views)
	if len(views) != panoramaDefaultView || views[0].Selected || views[0].Provenance != fmt.Sprintf("panorama:%s;yaw=0;fov=75", saved.PanoramaImage) {
		t.Fatalf("views=%+v", views)
	}
	var selected models.AssetVariant
	f.ps.db.Where("entity_id = ? AND selected = ?", asset.ID, true).First(&selected)
	if selected.File != "loc_current.png" {
		t.Fatalf("selected variant changed to %q", selected.File)
	}
	if f.ps.syncLocationPanoramas() {
		t.Fatal("a second sync must not split again")
	}
	var count int64
	f.ps.db.Model(&models.AssetVariant{}).Where("entity_id = ? AND provenance LIKE ?", asset.ID, "panorama:%").Count(&count)
	if count != panoramaDefaultView {
		t.Fatalf("views registered %d times", count)
	}
}

func TestSyncLocationPanoramasKeepsImageWhenSplitFails(t *testing.T) {
	f, asset := newPanoramaFixture(t)
	// A 21:9 canvas is what the model returns if it ignores the 2:1 request.
	completePanoramaTask(t, f, "pano-wide", encodePNG(t, quadrantPanorama(2520, 1080)))
	f.ps.db.Model(&asset).Update("panorama_task_id", "pano-wide")

	f.ps.syncLocationPanoramas()
	var saved models.Asset
	f.ps.db.First(&saved, asset.ID)
	if saved.PanoramaImage == "" || saved.PanoramaTaskID != "" || !strings.Contains(saved.PanoramaError, "自动拆视角失败") || !strings.Contains(saved.PanoramaError, "不是标准 ERP") {
		t.Fatalf("asset=%+v", saved)
	}
	var count int64
	f.ps.db.Model(&models.AssetVariant{}).Where("entity_id = ? AND provenance LIKE ?", asset.ID, "panorama:%").Count(&count)
	if count != 0 {
		t.Fatalf("failed split registered %d views", count)
	}
}
