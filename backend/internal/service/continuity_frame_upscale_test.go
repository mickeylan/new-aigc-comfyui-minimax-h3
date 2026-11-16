package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"comfyui-console/internal/models"
)

func installVOSR2Template(t *testing.T, f frameConvertFixture) models.Template {
	t.Helper()
	tpl := loadTemplateForTest(t, "vosr2_image_upscale.json")
	if err := f.ps.db.Create(&tpl).Error; err != nil {
		t.Fatal(err)
	}
	return tpl
}

func TestUpscaleTargetSizeKeepsAspect(t *testing.T) {
	for _, c := range []struct{ w, h, ww, wh int }{{1280, 720, 2048, 1152}, {720, 1280, 1152, 2048}, {500, 500, 2048, 2048}} {
		if w, h := upscaleTargetSize(c.w, c.h); w != c.ww || h != c.wh {
			t.Fatalf("%dx%d -> %dx%d, want %dx%d", c.w, c.h, w, h, c.ww, c.wh)
		}
	}
}

func TestFrameUpscaleFactorReachesLongEdgeWithinCap(t *testing.T) {
	for longEdge, want := range map[int]int{1280: 2, 1024: 2, 1025: 2, 832: 3, 640: 4, 64: 4} {
		if got := frameUpscaleFactor(longEdge); got != want {
			t.Fatalf("factor(%d)=%d, want %d", longEdge, got, want)
		}
	}
}

func TestVOSR2TemplateRendersBF16LoaderAndTiledUpscale(t *testing.T) {
	tpl := loadTemplateForTest(t, "vosr2_image_upscale.json")
	params := map[string]any{"upscale": 2, "_task_id": "hd-task"}
	if err := normalizeTemplateFiles(&tpl, params, map[string][]FileMeta{"source_image": {{TaskID: "7", Name: "tail.png"}}}); err != nil {
		t.Fatal(err)
	}
	workflow, err := (&TaskService{}).RenderWorkflow(&tpl, params)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(workflow)
	if strings.Contains(string(encoded), "{{") {
		t.Fatalf("工作流仍有未渲染占位符: %s", encoded)
	}
	node := func(id string) map[string]any { return workflow[id].(map[string]any)["inputs"].(map[string]any) }
	if node("1")["image"] != "7/tail.png" || node("2")["model"] != "VOSR2" || node("2")["dtype"] != "bf16" {
		t.Fatalf("loader inputs: image=%v model=%v", node("1"), node("2"))
	}
	up := node("3")
	if up["upscale"] != "2" || up["seed"] != "42" || up["color_alignment"] != "wavelet" || up["tile_size"] != float64(512) || up["vae_tile_size"] != float64(1024) {
		t.Fatalf("upscale inputs=%v", up)
	}
	if node("4")["filename_prefix"] != "vosr2/upscale_hd-task" {
		t.Fatalf("save inputs=%v", node("4"))
	}
}

func TestStartFrameUpscaleSubmitsVOSR2TaskAndKeepsSource(t *testing.T) {
	f := newFrameConvertFixture(t)
	if _, err := f.ps.StartFrameConversion(f.source.ProjectID, f.source.ID, f.frame.ID, FrameReferenceHD); err == nil || !strings.Contains(err.Error(), TemplateVOSR2Upscale) {
		t.Fatalf("a missing VOSR2 template must be reported, err=%v", err)
	}
	tpl := installVOSR2Template(t, f)
	if _, err := f.ps.frameUpscaleTaskRequest(f.source.ProjectID, "big.png", testPNG(t, 2048, 1152)); err == nil || !strings.Contains(err.Error(), "无需超分") {
		t.Fatalf("a frame already at 2K must be refused, err=%v", err)
	}
	got, err := f.ps.StartFrameConversion(f.source.ProjectID, f.source.ID, f.frame.ID, FrameReferenceHD)
	if err != nil {
		t.Fatal(err)
	}
	var task models.Task
	if err := f.ps.db.Where("task_id = ?", got.ConvertTaskID).First(&task).Error; err != nil {
		t.Fatalf("upscale task not recorded: %v", err)
	}
	var params map[string]any
	json.Unmarshal([]byte(task.ParamsJSON), &params)
	// The 64×36 fixture needs 32x; the generative step is capped and the final resize covers the rest.
	if task.TemplateID != tpl.ID || params["upscale"] != float64(4) || params["model_dtype"] != "bf16" || !strings.Contains(task.InputsJSON, `"source_image"`) || !strings.Contains(task.InputsJSON, "tail.png") {
		t.Fatalf("task template=%d params=%s inputs=%s", task.TemplateID, task.ParamsJSON, task.InputsJSON)
	}
	var saved models.FrameCandidate
	f.ps.db.First(&saved, f.frame.ID)
	if saved.ConvertKind != FrameReferenceHD || saved.ConvertSourceSHA256 == "" || saved.ImageFile != "tail.png" || saved.Source != "extracted" || saved.Type != models.FrameCandidateSelected {
		t.Fatalf("source frame must stay untouched while upscaling: %+v", saved)
	}
	if _, err := f.ps.StartFrameConversion(f.source.ProjectID, f.source.ID, f.frame.ID, FrameReferenceClay); err != errFrameConvertActive {
		t.Fatalf("a second conversion of the same frame must conflict, err=%v", err)
	}
}

func TestApplyFrameUpscaleResizesToLongEdgeAndStaysOpeningFrame(t *testing.T) {
	f := newFrameConvertFixture(t)
	pid := fmt.Sprint(f.source.ProjectID)
	current := models.Scene{ProjectID: f.source.ProjectID, EpisodeN: 1, Generation: 1, Order: 2, ImageFile: "story.png", StoryboardVideoRole: "start"}
	f.ps.db.Create(&current)
	if _, err := f.ps.continuity.Configure(current.ProjectID, current.ID, ConfigureContinuityRequest{Mode: models.ContinuityModeBridge, SourceSceneID: f.source.ID, FrameID: f.frame.ID, ReferenceForm: FrameReferenceHD}); err == nil {
		t.Fatal("bridge mode must reject the hd reference form")
	}
	if _, err := f.ps.continuity.Configure(current.ProjectID, current.ID, ConfigureContinuityRequest{Mode: models.ContinuityModeContinue, SourceSceneID: f.source.ID, FrameID: f.frame.ID, ReferenceForm: FrameReferenceHD}); err != nil {
		t.Fatal(err)
	}
	f.ps.db.First(&current, current.ID)
	if err := f.ps.continuity.PrepareScene(&current); err == nil || !strings.Contains(err.Error(), "尚无高清") {
		t.Fatalf("submission without an hd result must be blocked, err=%v", err)
	}
	f.upload.SaveFile(pid, "image", "same.png", testPNG(t, 64, 36))
	if err := f.ps.applyFrameConversion(claimedFrame(t, f, FrameReferenceHD), "same.png"); err == nil || !strings.Contains(err.Error(), "没有比原尾帧") {
		t.Fatalf("a result that is not larger must be discarded, err=%v", err)
	}
	f.upload.SaveFile(pid, "image", "vosr2.png", testPNG(t, 256, 144))
	if err := f.ps.applyFrameConversion(claimedFrame(t, f, FrameReferenceHD), "vosr2.png"); err != nil {
		t.Fatal(err)
	}
	derived, err := f.ps.continuity.DerivedReference(&f.frame, FrameReferenceHD)
	if err != nil || derived == nil || derived.Type != models.FrameCandidateDerived || derived.DerivedSourceFile != "tail.png" || derived.ImageFile != "vosr2.png" {
		t.Fatalf("derived=%+v err=%v", derived, err)
	}
	data, err := f.ps.readProjectInput(f.source.ProjectID, derived.ImageFile)
	if err != nil {
		t.Fatal(err)
	}
	if cfg, _, _ := image.DecodeConfig(bytes.NewReader(data)); cfg.Width != 2048 || cfg.Height != 1152 {
		t.Fatalf("hd file %dx%d, want 2048x1152", cfg.Width, cfg.Height)
	}
	var source models.FrameCandidate
	f.ps.db.First(&source, f.frame.ID)
	if source.ImageFile != "tail.png" || source.Source != "extracted" || source.Type != models.FrameCandidateSelected {
		t.Fatalf("source frame changed: %+v", source)
	}
	if err := f.ps.continuity.PrepareScene(&current); err != nil {
		t.Fatal(err)
	}
	refs, lines, _ := f.ps.sceneVideoContinuityReferences(&current, pid)
	if len(refs) != 1 || refs[0].Name != derived.ImageFile || !strings.Contains(lines[0], "唯一开始画面") {
		t.Fatalf("hd frame must stay the opening frame: refs=%+v lines=%+v", refs, lines)
	}
	if tag := openingPictureTag(lines); tag != "<Picture 1>" {
		t.Fatalf("opening tag=%q", tag)
	}
}

func TestHandleConvertFrameCandidateAcceptsHDKind(t *testing.T) {
	f := newFrameConvertFixture(t)
	installVOSR2Template(t, f)
	svc := &Service{DB: f.ps.db, Projects: f.ps, Continuity: f.ps.continuity}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/projects/:id/scenes/:sid/continuity/frames/:fid/convert", svc.HandleConvertFrameCandidate)
	url := fmt.Sprintf("/api/projects/%d/scenes/%d/continuity/frames/%d/convert", f.source.ProjectID, f.source.ID, f.frame.ID)
	w := shotRefineCall(r, http.MethodPost, url, map[string]string{"kind": FrameReferenceHD})
	if w.Code != http.StatusAccepted || !strings.Contains(w.Body.String(), `"convert_kind":"hd"`) || !strings.Contains(w.Body.String(), "尾帧高清任务已提交") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if w := shotRefineCall(r, http.MethodPost, url, map[string]string{"kind": FrameReferenceHD}); w.Code != http.StatusConflict {
		t.Fatalf("an in-flight upscale must conflict, status=%d body=%s", w.Code, w.Body.String())
	}
}
