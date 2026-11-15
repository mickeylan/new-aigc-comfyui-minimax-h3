package service

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"comfyui-console/internal/models"
)

// fakeRealESRGAN installs a fake engine directory and replaces realESRGANRun. Each call pops one
// scripted exit; a zero exit writes a 4x PNG to the -o path like the real x4plus model.
func fakeRealESRGAN(t *testing.T, exits []struct {
	code   int
	output string
}) (exe string, calls *[][]string) {
	t.Helper()
	dir := t.TempDir()
	exe = filepath.Join(dir, "realesrgan-ncnn-vulkan.exe")
	os.WriteFile(exe, []byte("fake"), 0o644)
	os.MkdirAll(filepath.Join(dir, "models"), 0o755)
	os.WriteFile(filepath.Join(dir, "models", "realesrgan-x4plus.bin"), []byte("m"), 0o644)
	os.WriteFile(filepath.Join(dir, "models", "realesrgan-x4plus.param"), []byte("m"), 0o644)
	recorded := [][]string{}
	calls = &recorded
	prev := realESRGANRun
	t.Cleanup(func() { realESRGANRun = prev })
	realESRGANRun = func(_ context.Context, _ string, args []string) (string, int, error) {
		recorded = append(recorded, args)
		exit := exits[0]
		if len(exits) > 1 {
			exits = exits[1:]
		}
		if exit.code != 0 {
			return exit.output, exit.code, nil
		}
		in, out := argAfter(args, "-i"), argAfter(args, "-o")
		f, err := os.Open(in)
		if err != nil {
			return err.Error(), 1, nil
		}
		cfg, _, err := image.DecodeConfig(f)
		f.Close()
		if err != nil {
			return err.Error(), 1, nil
		}
		var buf bytes.Buffer
		png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, cfg.Width*4, cfg.Height*4)))
		os.WriteFile(out, buf.Bytes(), 0o644)
		return "", 0, nil
	}
	return exe, calls
}

func argAfter(args []string, flag string) string {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag {
			return args[i+1]
		}
	}
	return ""
}

func okExit() []struct {
	code   int
	output string
} {
	return []struct {
		code   int
		output string
	}{{0, ""}}
}

func TestResolveRealESRGANRequiresConfiguredFileAndModels(t *testing.T) {
	exe, _ := fakeRealESRGAN(t, okExit())
	engine, err := resolveRealESRGAN(exe)
	if err != nil || engine.Exe != exe || engine.ModelDir != filepath.Join(filepath.Dir(exe), "models") {
		t.Fatalf("engine=%+v err=%v", engine, err)
	}
	if _, err := resolveRealESRGAN(filepath.Join(t.TempDir(), "missing.exe")); err == nil || !strings.Contains(err.Error(), "不存在") {
		t.Fatalf("a configured path that does not exist must fail, err=%v", err)
	}
	os.Remove(filepath.Join(filepath.Dir(exe), "models", "realesrgan-x4plus.param"))
	if _, err := resolveRealESRGAN(exe); err == nil || !strings.Contains(err.Error(), "realesrgan-x4plus.param") {
		t.Fatalf("missing model file must fail, err=%v", err)
	}
}

func TestUpscaleImageRetriesSmallerTilesOnlyAfterGPUMemoryFailure(t *testing.T) {
	exe, calls := fakeRealESRGAN(t, []struct {
		code   int
		output string
	}{{1, "vkAllocateMemory failed -2"}, {-1073741819, ""}, {0, ""}}) // -1073741819 == 0xC0000005
	engine, _ := resolveRealESRGAN(exe)
	out, lowMemory, err := upscaleImage(engine, testPNG(t, 640, 360), ".png")
	if err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 3 || argAfter((*calls)[0], "-t") != "0" || argAfter((*calls)[1], "-t") != "256" || argAfter((*calls)[2], "-t") != "128" || !lowMemory {
		t.Fatalf("tile attempts=%v lowMemory=%v", *calls, lowMemory)
	}
	if argAfter((*calls)[2], "-s") != "4" || argAfter((*calls)[2], "-j") != "1:1:1" {
		t.Fatalf("args=%v", (*calls)[2])
	}
	cfg, _, _ := image.DecodeConfig(bytes.NewReader(out))
	if cfg.Width != 2048 || cfg.Height != 1152 {
		t.Fatalf("output %dx%d, want 2048x1152", cfg.Width, cfg.Height)
	}

	exe, calls = fakeRealESRGAN(t, []struct {
		code   int
		output string
	}{{3, "invalid gpu device"}})
	engine, _ = resolveRealESRGAN(exe)
	if _, _, err := upscaleImage(engine, testPNG(t, 640, 360), ".png"); err == nil || !strings.Contains(err.Error(), "退出码 3") || len(*calls) != 1 {
		t.Fatalf("a non-memory failure must not retry, err=%v calls=%d", err, len(*calls))
	}
	if _, _, err := upscaleImage(engine, testPNG(t, 2048, 1152), ".png"); err == nil || !strings.Contains(err.Error(), "无需超分") {
		t.Fatalf("a frame already at 2K must be refused, err=%v", err)
	}
}

func TestUpscaleTargetSizeKeepsAspect(t *testing.T) {
	for _, c := range []struct{ w, h, ww, wh int }{{1280, 720, 2048, 1152}, {720, 1280, 1152, 2048}, {500, 500, 2048, 2048}} {
		if w, h := upscaleTargetSize(c.w, c.h); w != c.ww || h != c.wh {
			t.Fatalf("%dx%d -> %dx%d, want %dx%d", c.w, c.h, w, h, c.ww, c.wh)
		}
	}
}

func TestUpscaleFrameAddsHDRowUsedAsOpeningFrame(t *testing.T) {
	f := newFrameConvertFixture(t)
	exe, _ := fakeRealESRGAN(t, okExit())
	t.Setenv("REALESRGAN_PATH", exe)
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
	frame, message, err := f.ps.UpscaleFrame(f.source.ProjectID, f.source.ID, f.frame.ID)
	if err != nil {
		t.Fatal(err)
	}
	if frame.ID != f.frame.ID || !strings.Contains(message, "原尾帧保持不变") {
		t.Fatalf("frame=%+v message=%q", frame, message)
	}
	derived, err := f.ps.continuity.DerivedReference(&f.frame, FrameReferenceHD)
	if err != nil || derived == nil || derived.Type != models.FrameCandidateDerived || derived.DerivedSourceFile != "tail.png" || !strings.HasPrefix(derived.ImageFile, fmt.Sprintf("continuity_s%d_hd_", f.source.ID)) {
		t.Fatalf("derived=%+v err=%v", derived, err)
	}
	data, err := f.ps.readProjectInput(f.source.ProjectID, derived.ImageFile)
	if err != nil {
		t.Fatal(err)
	}
	if cfg, _, _ := image.DecodeConfig(bytes.NewReader(data)); cfg.Width != 2048 || cfg.Height != 1152 {
		t.Fatalf("hd file %dx%d", cfg.Width, cfg.Height)
	}
	var source models.FrameCandidate
	f.ps.db.First(&source, f.frame.ID)
	if source.ImageFile != "tail.png" || source.Source != "extracted" || source.Type != models.FrameCandidateSelected {
		t.Fatalf("source frame changed: %+v", source)
	}
	// Recording the result invalidated the Scene that already chose the hd form.
	if err := f.ps.continuity.PrepareScene(&current); err != nil {
		t.Fatal(err)
	}
	refs, lines, _ := f.ps.sceneVideoContinuityReferences(&current, fmt.Sprint(current.ProjectID))
	if len(refs) != 1 || refs[0].Name != derived.ImageFile || !strings.Contains(lines[0], "唯一开始画面") {
		t.Fatalf("hd frame must stay the opening frame: refs=%+v lines=%+v", refs, lines)
	}
	if tag := openingPictureTag(lines); tag != "<Picture 1>" {
		t.Fatalf("opening tag=%q", tag)
	}
}

func TestHandleUpscaleFrameCandidateConflictsWhileEngineBusy(t *testing.T) {
	f := newFrameConvertFixture(t)
	exe, _ := fakeRealESRGAN(t, okExit())
	t.Setenv("REALESRGAN_PATH", exe)
	svc := &Service{DB: f.ps.db, Projects: f.ps, Continuity: f.ps.continuity}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/projects/:id/scenes/:sid/continuity/frames/:fid/upscale", svc.HandleUpscaleFrameCandidate)
	url := fmt.Sprintf("/api/projects/%d/scenes/%d/continuity/frames/%d/upscale", f.source.ProjectID, f.source.ID, f.frame.ID)
	frameUpscaleMu.Lock()
	w := shotRefineCall(r, http.MethodPost, url, nil)
	frameUpscaleMu.Unlock()
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "另一张图片") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if w := shotRefineCall(r, http.MethodPost, url, nil); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"hd"`) {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}
