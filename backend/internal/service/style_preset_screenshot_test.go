package service

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
)

const styleScreenshotReply = `{"name":"冷雾仙侠","summary":"冷青低饱和的写实仙侠质感","shared_style":"写实电影质感，冷青低饱和色调，柔和侧逆光，浅景深","character_style":"细腻肤质，丝绸衣料","scene_style":"","prop_style":"冷铁金属，细微划痕","negative":"卡通，高饱和","warnings":["只有两张截图，夜景光线无法判断"]}`

func styleTestPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 90, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// newStyleScreenshotTestService returns a service whose llama mock records each request body.
func newStyleScreenshotTestService(t *testing.T, vision bool) (*Service, *[]string) {
	t.Helper()
	db := newTestDBWithNewModels(t)
	if err := db.AutoMigrate(&models.Skill{}, &models.ProjectSkillConfig{}, &models.SkillAuditLog{}); err != nil {
		t.Fatal(err)
	}
	if err := NewSkillService(db).InitSystemSkills(); err != nil {
		t.Fatal(err)
	}
	bodies := []string{}
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var buf bytes.Buffer
		buf.ReadFrom(r.Body)
		bodies = append(bodies, buf.String())
		content, _ := json.Marshal(styleScreenshotReply)
		fmt.Fprintf(w, `{"choices":[{"message":{"content":%s}}]}`, content)
	}))
	t.Cleanup(ai.Close)
	settings := map[string]string{SettingTextProvider: "llama", SettingLlamaBaseURL: ai.URL, SettingLlamaModel: "mock"}
	if vision {
		settings[SettingTextVisionEnabled] = "true"
	}
	svc := &Service{DB: db, Skills: NewSkillService(db), TextProviderFact: NewTextProviderFactory(&mockGetter{settings: settings}), StylePresets: NewStylePresetService(db)}
	return svc, &bodies
}

func styleScreenshotRequest(t *testing.T, svc *Service, files ...[]byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for i, data := range files {
		part, _ := form.CreateFormFile("files", fmt.Sprintf("shot%d.png", i+1))
		part.Write(data)
	}
	form.WriteField("project_id", "7")
	form.Close()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/presets/from-screenshots", svc.HandleStylePresetFromScreenshots)
	req := httptest.NewRequest(http.MethodPost, "/api/presets/from-screenshots", &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestStylePresetScreenshotSkillIsSeeded(t *testing.T) {
	db := newTestDB(t)
	if err := NewSkillService(db).InitSystemSkills(); err != nil {
		t.Fatal(err)
	}
	var skill models.Skill
	if err := db.Where("code = ?", stylePresetScreenshotSkillCode).First(&skill).Error; err != nil {
		t.Fatal(err)
	}
	if skill.Version != 1 || !skill.Enabled || skill.SystemPrompt != "你是影视视觉风格分析师。只从参考截图中提取可迁移到本项目画面的视觉语言，供镜头风格提示词使用。" ||
		!strings.HasPrefix(skill.PromptTemplate, "请综合分析随后提供的{{image_count}}张参考截图，提取一份可复用的风格预设。") ||
		!strings.Contains(skill.PromptTemplate, "- 静态截图无法证明运镜，不得描述镜头运动或剪辑节奏。") ||
		!strings.HasSuffix(skill.PromptTemplate, `"prop_style":"","negative":"","warnings":[]}`) {
		t.Fatalf("skill=%+v", skill)
	}
}

func TestTextVisionSwitchGatesImageSending(t *testing.T) {
	svc, bodies := newStyleScreenshotTestService(t, false)
	if svc.TextProviderFact.SupportsImages() || svc.TextProviderFact.Capabilities().FirstFrameGroundedPolish {
		t.Fatal("vision must be off by default")
	}
	if _, err := svc.TextProviderFact.ChatWithImages("s", "u", []TextImage{{MIME: "image/png", Data: []byte("x")}}); err != ErrMultimodalUnsupported {
		t.Fatalf("err=%v", err)
	}
	w := styleScreenshotRequest(t, svc, styleTestPNG(t, 8, 8))
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "multimodal_unsupported") || len(*bodies) != 0 {
		t.Fatalf("status=%d body=%s calls=%d", w.Code, w.Body.String(), len(*bodies))
	}
}

func TestVolcTextAdapterSendsInputImages(t *testing.T) {
	var body string
	ark := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var buf bytes.Buffer
		buf.ReadFrom(r.Body)
		body = buf.String()
		fmt.Fprint(w, `{"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}]}`)
	}))
	defer ark.Close()
	g := &mockGetter{settings: map[string]string{SettingVolcAPIKey: "k", SettingVolcBaseURL: ark.URL, SettingTextVisionEnabled: "true"}}
	out, err := (&VolcTextAdapter{volc: NewVolcClientWithGetter(g)}).ChatWithImages("s", "u", []TextImage{{MIME: "image/jpeg", Data: []byte("abc")}})
	if err != nil || out != "ok" || !strings.Contains(body, `{"type":"input_image","image_url":"data:image/jpeg;base64,YWJj"}`) {
		t.Fatalf("out=%q err=%v body=%s", out, err, body)
	}
}

func TestStylePresetFromScreenshotsReturnsUnsavedDraft(t *testing.T) {
	svc, bodies := newStyleScreenshotTestService(t, true)
	w := styleScreenshotRequest(t, svc, styleTestPNG(t, 8, 8), styleTestPNG(t, 10, 6))
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var draft stylePresetDraft
	if err := json.Unmarshal(w.Body.Bytes(), &draft); err != nil {
		t.Fatal(err)
	}
	p := draft.Preset
	if p.Name != "冷雾仙侠" || p.PromptTail != "写实电影质感，冷青低饱和色调，柔和侧逆光，浅景深；人物质感：细腻肤质，丝绸衣料；道具质感：冷铁金属，细微划痕" ||
		p.NegativeTail != "卡通，高饱和" || !strings.Contains(p.Reason, "冷青低饱和的写实仙侠质感") || !strings.Contains(p.Reason, "夜景光线无法判断") || p.ID != 0 {
		t.Fatalf("preset=%+v", p)
	}
	// One request carrying the rendered prompt and both images as image_url parts.
	if len(*bodies) != 1 || strings.Count((*bodies)[0], `"type":"image_url"`) != 2 || !strings.Contains((*bodies)[0], "data:image/png;base64,") ||
		!strings.Contains((*bodies)[0], "随后提供的2张参考截图") {
		t.Fatalf("bodies=%d first=%.300s", len(*bodies), strings.Join(*bodies, ""))
	}
	var presets, audits int64
	svc.DB.Model(&models.StylePreset{}).Where("is_system = ?", false).Count(&presets)
	svc.DB.Model(&models.SkillAuditLog{}).Where("skill_code = ? AND project_id = ? AND success = ?", stylePresetScreenshotSkillCode, 7, true).Count(&audits)
	if presets != 0 || audits != 1 {
		t.Fatalf("the draft must not be saved (presets=%d) and the call must be audited (audits=%d)", presets, audits)
	}
}

func TestStylePresetFromScreenshotsRejectsBadUploads(t *testing.T) {
	svc, bodies := newStyleScreenshotTestService(t, true)
	if w := styleScreenshotRequest(t, svc); w.Code != http.StatusBadRequest {
		t.Fatalf("no files: status=%d", w.Code)
	}
	seven := make([][]byte, 7)
	for i := range seven {
		seven[i] = styleTestPNG(t, 4, 4)
	}
	if w := styleScreenshotRequest(t, svc, seven...); w.Code != http.StatusBadRequest {
		t.Fatalf("seven files: status=%d", w.Code)
	}
	if w := styleScreenshotRequest(t, svc, []byte("GIF89a not a screenshot")); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "PNG、JPEG 或 WebP") {
		t.Fatalf("gif: status=%d body=%s", w.Code, w.Body.String())
	}
	if len(*bodies) != 0 {
		t.Fatalf("rejected uploads must not reach the model, calls=%d", len(*bodies))
	}
}

func TestNormalizeStyleScreenshotDownscalesLargeImages(t *testing.T) {
	img, err := normalizeStyleScreenshot(styleTestPNG(t, 3200, 1000))
	if err != nil {
		t.Fatal(err)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(img.Data))
	if err != nil || format != "jpeg" || img.MIME != "image/jpeg" || cfg.Width != styleScreenshotMaxSide || cfg.Height != 500 {
		t.Fatalf("err=%v format=%s mime=%s size=%dx%d", err, format, img.MIME, cfg.Width, cfg.Height)
	}
	small, err := normalizeStyleScreenshot(styleTestPNG(t, 20, 10))
	if err != nil || small.MIME != "image/png" {
		t.Fatalf("small images pass through unchanged: err=%v mime=%s", err, small.MIME)
	}
}

// gopher-doc.1bpp.lossless.webp from golang.org/x/image testdata.
const styleTestWebP = "UklGRrIBAABXRUJQVlA4TKUBAAAvSsAYAA8w//M///MfeJAkbXvaSG7m8Q3GfYSBJekwQztm/IcZlgwnmWImn2BK7aFmBtnVir6q//8VOkFE/xm4baTIu8c48ArEo6+B3zFKYln3pqClSCKX0begFTAXFOLXHSyF8cCNcZEG4OywuA4KVVfJCiArU7GAgJI8+lJP/OKMT/fBAjevg1cYB7YVkFuWga2lyPi5I0HFy5YTpWIHg0RZpkniRVW9odHAKOwosWuOGdxIyn2OvaCDvhg/we6TwadPBPbqBV58MsLmMJ8yZnOWk8SRz4N+QoyPL+MnamzMvcE1rHNEr91F9GKZPVUcS9w7PhhH36suB9qPeYb/oLk6cuTiJ0wOK3m5h1cKjW6EVZCYMK7dxcKCBdgP9HkKr9gkAO2P8GKZGWVdIAatQa+1IDpt6qyorVwdy01xdW8Jkfk6xjEXmVQQ+HQdFr6OKhIN34dXWq0+0qr6EJSCeeVLH9+gvGTLyqM65PQ44ihzlTXxQKjKbAvshXgir7Lil9w4L2bvMycmjQcqXaMCO6BlY28i+FOLzbfI1vEqxAhotocAAA=="

func TestNormalizeStyleScreenshotReencodesWebPAsJPEG(t *testing.T) {
	data, err := base64.StdEncoding.DecodeString(styleTestWebP)
	if err != nil {
		t.Fatal(err)
	}
	webpCfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	img, err := normalizeStyleScreenshot(data)
	if err != nil {
		t.Fatal(err)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(img.Data))
	if err != nil || format != "jpeg" || img.MIME != "image/jpeg" || cfg.Width != webpCfg.Width || cfg.Height != webpCfg.Height {
		t.Fatalf("err=%v format=%s mime=%s size=%dx%d want %dx%d", err, format, img.MIME, cfg.Width, cfg.Height, webpCfg.Width, webpCfg.Height)
	}
}

func TestParseStyleScreenshotAnalysisRejectsEmptyStyle(t *testing.T) {
	if _, err := parseStyleScreenshotAnalysis(`{"name":"空","shared_style":"","character_style":"","scene_style":"","prop_style":""}`); err == nil {
		t.Fatal("an analysis with no style text must be rejected")
	}
	a, err := parseStyleScreenshotAnalysis("<think>x</think>```json\n" + styleScreenshotReply + "\n```")
	if err != nil || a.Name != "冷雾仙侠" || len(a.Warnings) != 1 {
		t.Fatalf("a=%+v err=%v", a, err)
	}
}
