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

func newLocationDesignFixture(t *testing.T) (*ProjectService, models.Project, models.Asset, models.Asset) {
	t.Helper()
	f := newFrameConvertFixture(t)
	ps := f.ps
	if err := ps.db.AutoMigrate(&models.LocationReferenceDesign{}, &models.LocationReferenceDesignFile{}, &models.Asset{}, &models.AssetVariant{}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"minimax_h3_image_edit.json"} {
		tpl := loadTemplateForTest(t, name)
		if err := ps.db.Create(&tpl).Error; err != nil {
			t.Fatal(err)
		}
	}
	var p models.Project
	if err := ps.db.First(&p, f.source.ProjectID).Error; err != nil {
		t.Fatal(err)
	}
	ps.db.Model(&p).Updates(map[string]any{"style": "国漫写实", "genre": "古典修仙", "aspect_ratio": "16:9"})
	ps.db.First(&p, p.ID)
	loc := models.Asset{ProjectID: p.ID, Kind: AssetKindLocation, Name: "天阙宗山门", Description: "九十九级白玉石阶，两侧青铜灯柱", VisibleText: "天阙宗"}
	ps.db.Create(&loc)
	prop := models.Asset{ProjectID: p.ID, Kind: AssetKindProp, Name: "青铜灯", Description: "高两丈的青铜灯柱"}
	ps.db.Create(&prop)
	ps.db.Create(&models.AssetVariant{ProjectID: p.ID, EntityType: VariantAssetImage, EntityID: loc.ID, File: "loc_current.png", Selected: true})
	for _, file := range []string{"loc_current.png", "scene_ref.png", "hero.png", "prop.png"} {
		if _, _, err := f.upload.SaveFile(fmt.Sprint(p.ID), "image", file, []byte("bytes-"+file)); err != nil {
			t.Fatal(err)
		}
	}
	return ps, p, loc, prop
}

func locationDesignSampleInput() locationDesignInput {
	return locationDesignInput{
		Engine:       AgeStagePortraitQwenEdit,
		Requirements: "黄昏，山门前的石阶上有两个弟子在扫雪",
		References: []models.LocationReferenceDesignFile{
			{File: "scene_ref.png", Slot: locationDesignSlotScene, Presence: locationDesignPresentOptional, Responsibility: "沿用石阶与灯柱的布局和暮色光线"},
			{File: "hero.png", Slot: locationDesignSlotCharacter, Presence: locationDesignPresentRequired, Responsibility: "白衣弟子，站在第三级石阶上"},
			{File: "prop.png", Slot: locationDesignSlotProp, Presence: locationDesignPresentRequired, Responsibility: "青铜灯柱立在画面右侧"},
		},
	}
}

func TestLocationDesignPreviewBindsOrderedReferencesAndKeepsSceneImage(t *testing.T) {
	ps, p, loc, _ := newLocationDesignFixture(t)
	for _, engine := range []string{AgeStagePortraitQwenEdit, AgeStagePortraitH3Edit} {
		token1, token2, token3 := "<image1>", "<image2>", "<image3>"
		paramsKey := "aspect_ratio"
		if engine == AgeStagePortraitH3Edit {
			token1, token2, token3 = "<Picture 1>", "<Picture 2>", "<Picture 3>"
			paramsKey = "width"
		}
		provider := &captureTextProvider{response: token1 + "场景，" + token2 + "白衣弟子，" + token3 + "青铜灯柱，黄昏山门"}
		ps.textProvider = provider
		in := locationDesignSampleInput()
		in.Engine = engine
		row, err := ps.PreviewLocationDesign(p.ID, loc.ID, in)
		if err != nil {
			t.Fatal(err)
		}
		if len(row.References) != 3 || row.References[0].SHA256 != sha256Hex([]byte("bytes-scene_ref.png")) || row.References[2].Position != 3 || row.References[1].Presence != locationDesignPresentRequired {
			t.Fatalf("references not bound in order: %+v", row.References)
		}
		if !strings.Contains(provider.user, token1+"：场景参考") || !strings.Contains(provider.user, token3+"：道具参考，必须出现在画面中") || !strings.Contains(provider.user, "仅作造型与材质参考") {
			t.Fatalf("reference roles not described: %s", provider.user)
		}
		if !strings.Contains(provider.user, "天阙宗山门") || !strings.Contains(provider.user, "黄昏，山门前的石阶上有两个弟子在扫雪") || !strings.Contains(provider.user, "天阙宗") {
			t.Fatalf("scene material missing from request: %s", provider.user)
		}
		if !strings.Contains(provider.system, token1+"…"+token3) {
			t.Fatalf("%s system must state this engine's reference syntax: %s", engine, provider.system)
		}
		if engine == AgeStagePortraitQwenEdit {
			if row.Skill == "" || !strings.Contains(row.Skill, "qwen") {
				t.Fatalf("Qwen edit program skill not used: skill=%q", row.Skill)
			}
		} else if row.Skill != "" {
			t.Fatalf("H3 must not borrow the Qwen edit program skill: %q", row.Skill)
		}
		var params map[string]any
		if err := json.Unmarshal([]byte(row.ParamsJSON), &params); err != nil {
			t.Fatal(err)
		}
		if params[paramsKey] == nil || params["_location_design_template_hash"] == "" {
			t.Fatalf("engine params missing: %s", row.ParamsJSON)
		}
		if engine == AgeStagePortraitH3Edit && (params["width"] != float64(1344) || params["height"] != float64(768)) {
			t.Fatalf("scene canvas must follow project aspect ratio: %s", row.ParamsJSON)
		}
		// 预览只落一份可审阅的记录：不建任务、不动场景当前图与变体。
		var taskCount int64
		ps.db.Model(&models.Task{}).Where("task_id = ?", row.TaskID).Count(&taskCount)
		if row.TaskID != "" || taskCount != 0 || row.ResultVariantID != 0 {
			t.Fatalf("preview started work: %+v", row)
		}
		var saved models.Asset
		ps.db.First(&saved, loc.ID)
		var variants int64
		ps.db.Model(&models.AssetVariant{}).Where("entity_id = ? AND entity_type = ?", loc.ID, VariantAssetImage).Count(&variants)
		if saved.Image != "" || variants != 1 {
			t.Fatalf("preview touched the scene image: %+v variants=%d", saved, variants)
		}
		if engines := ps.locationDesignEngines(); engines[1].Code != AgeStagePortraitH3Edit || engines[1].MaxCount != 100 || !engines[0].Available {
			t.Fatalf("engine capacity not read from templates: %+v", engines)
		}
	}
}

func TestLocationDesignRejectsUnsafeOrAmbiguousReferences(t *testing.T) {
	ps, p, loc, prop := newLocationDesignFixture(t)
	ps.textProvider = &captureTextProvider{response: "<image1><image2><image3>合成"}
	bad := func(mutate func(*locationDesignInput)) string {
		t.Helper()
		in := locationDesignSampleInput()
		mutate(&in)
		_, err := ps.PreviewLocationDesign(p.ID, loc.ID, in)
		if err == nil {
			return ""
		}
		return err.Error()
	}
	if bad(func(in *locationDesignInput) { in.References[0].Slot = "background" }) == "" {
		t.Fatal("unknown slot accepted")
	}
	if bad(func(in *locationDesignInput) { in.References[1].Presence = "" }) == "" {
		t.Fatal("missing presence accepted")
	}
	if bad(func(in *locationDesignInput) { in.References[1].File = "scene_ref.png" }) == "" {
		t.Fatal("duplicated reference accepted")
	}
	if bad(func(in *locationDesignInput) { in.References[0].File = "../scene_ref.png" }) == "" {
		t.Fatal("unsafe path accepted")
	}
	if err := ps.db.Create(&models.UploadFile{TaskID: "999", Type: "image", Name: "foreign.png"}).Error; err != nil {
		t.Fatal(err)
	}
	if bad(func(in *locationDesignInput) { in.References[2].File = "foreign.png" }) == "" {
		t.Fatal("foreign project reference accepted")
	}
	if bad(func(in *locationDesignInput) { in.References = nil }) == "" {
		t.Fatal("empty reference list accepted")
	}
	var tooMany []models.LocationReferenceDesignFile
	for i := 0; i < 17; i++ {
		tooMany = append(tooMany, models.LocationReferenceDesignFile{File: "scene_ref.png", Slot: locationDesignSlotScene, Presence: locationDesignPresentOptional})
	}
	if bad(func(in *locationDesignInput) { in.Requirements = strings.Repeat("长", 4001) }) == "" {
		t.Fatal("oversized requirements accepted")
	}
	if err := validateLocationDesignInput(locationDesignInput{References: tooMany}, 16); err == nil {
		t.Fatal("over-capacity reference list accepted instead of truncated")
	}
	if _, err := ps.PreviewLocationDesign(p.ID, prop.ID, locationDesignSampleInput()); err == nil {
		t.Fatal("prop asset accepted as a scene design target")
	}
}

func TestStartLocationDesignRejectsChangedSceneProfileAndSecondSubmission(t *testing.T) {
	ps, p, loc, _ := newLocationDesignFixture(t)
	ps.textProvider = &captureTextProvider{response: "<image1><image2><image3>合成"}
	row, err := ps.PreviewLocationDesign(p.ID, loc.ID, locationDesignSampleInput())
	if err != nil {
		t.Fatal(err)
	}
	ps.db.Model(&models.Asset{}).Where("id = ?", loc.ID).Update("description", "改成了雪夜")
	if _, err := ps.StartLocationDesign(p.ID, loc.ID, row.ID, row.Prompt); err == nil || !strings.Contains(err.Error(), "场景资料已改变") {
		t.Fatalf("stale profile accepted: %v", err)
	}
	ps.db.Model(&models.Asset{}).Where("id = ?", loc.ID).Update("description", "九十九级白玉石阶，两侧青铜灯柱")
	if _, err := ps.StartLocationDesign(p.ID, loc.ID, row.ID, "缺编号的提示词"); err == nil {
		t.Fatal("prompt without reference tokens accepted")
	}
	ps.db.Model(&models.LocationReferenceDesign{}).Where("id = ?", row.ID).Updates(map[string]any{"task_id": "pending-task"})
	if _, err := ps.StartLocationDesign(p.ID, loc.ID, row.ID, row.Prompt); err == nil || !strings.Contains(err.Error(), "已提交") {
		t.Fatalf("second submission accepted: %v", err)
	}
}

// completeLocationDesignTask 让假 ComfyUI 在 /view 返回 body，并把任务标成成功。
func completeLocationDesignTask(t *testing.T, ps *ProjectService, taskID string, status string, body []byte, taskErr string) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/view" && r.URL.Query().Get("filename") == "design.png" {
			w.Write(body)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)
	inst := models.Instance{GPUIndex: 0, Host: "127.0.0.1", Port: comfyTestPort(server), Status: "running"}
	ps.db.Create(&inst)
	files, _ := json.Marshal([]map[string]string{{"filename": "design.png", "subfolder": "", "type": "images"}})
	ps.db.Create(&models.Task{TaskID: taskID, Status: status, Error: taskErr, Port: &inst.Port, InstanceID: &inst.ID, ResultFiles: string(files)})
}

func TestSyncLocationDesignsRegistersUnselectedSceneVariant(t *testing.T) {
	ps, _, loc, _ := newLocationDesignFixture(t)
	body := testPNG(t, 64, 36)
	completeLocationDesignTask(t, ps, "loc-design-ok", "success", body, "")
	row := models.LocationReferenceDesign{ProjectID: loc.ProjectID, AssetID: loc.ID, Engine: AgeStagePortraitQwenEdit, Prompt: "<image1>山门", TaskID: "loc-design-ok"}
	ps.db.Create(&row)

	if !ps.syncLocationDesigns() {
		t.Fatal("finished design must be applied")
	}
	var saved models.LocationReferenceDesign
	ps.db.First(&saved, row.ID)
	if saved.TaskID != "" || saved.Error != "" || saved.ResultVariantID == 0 {
		t.Fatalf("design=%+v", saved)
	}
	var variants []models.AssetVariant
	ps.db.Where("entity_id = ? AND entity_type = ? AND provenance LIKE ?", loc.ID, VariantAssetImage, "location_design:%").Find(&variants)
	if len(variants) != 1 || variants[0].Selected || !strings.HasPrefix(variants[0].File, "loc_design_") || variants[0].ID != saved.ResultVariantID {
		t.Fatalf("generated scene not registered as an unselected variant: %+v", variants)
	}
	if !strings.Contains(variants[0].Provenance, "task=loc-design-ok") {
		t.Fatalf("provenance lost: %s", variants[0].Provenance)
	}
	var asset models.Asset
	ps.db.First(&asset, loc.ID)
	var selected models.AssetVariant
	ps.db.Where("entity_id = ? AND entity_type = ? AND selected = ?", loc.ID, VariantAssetImage, true).First(&selected)
	if asset.Image != "" || selected.File != "loc_current.png" {
		t.Fatalf("current scene image was replaced: asset=%+v selected=%s", asset, selected.File)
	}
	if ps.syncLocationDesigns() {
		t.Fatal("a second sync must not register the same output again")
	}
	var count int64
	ps.db.Model(&models.AssetVariant{}).Where("entity_id = ? AND provenance LIKE ?", loc.ID, "location_design:%").Count(&count)
	if count != 1 {
		t.Fatalf("variant registered %d times", count)
	}
}

func TestSyncLocationDesignsReportsFailureWithoutRegistering(t *testing.T) {
	ps, _, loc, _ := newLocationDesignFixture(t)
	completeLocationDesignTask(t, ps, "loc-design-bad", "failed", nil, "CUDA out of memory")
	row := models.LocationReferenceDesign{ProjectID: loc.ProjectID, AssetID: loc.ID, Engine: AgeStagePortraitQwenEdit, Prompt: "<image1>山门", TaskID: "loc-design-bad"}
	ps.db.Create(&row)

	if !ps.syncLocationDesigns() {
		t.Fatal("failed task must be recorded once")
	}
	var saved models.LocationReferenceDesign
	ps.db.First(&saved, row.ID)
	if saved.TaskID != "" || saved.Error != "CUDA out of memory" || saved.ResultVariantID != 0 {
		t.Fatalf("design=%+v", saved)
	}
	var count int64
	ps.db.Model(&models.AssetVariant{}).Where("entity_id = ? AND provenance LIKE ?", loc.ID, "location_design:%").Count(&count)
	if count != 0 {
		t.Fatalf("failed task registered %d variants", count)
	}
}
