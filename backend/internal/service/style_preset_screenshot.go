package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
	_ "golang.org/x/image/webp"
)

const (
	stylePresetScreenshotSkillCode = "style-preset-from-screenshots"
	minStyleScreenshots            = 1
	maxStyleScreenshots            = 6
	maxStyleScreenshotBytes        = 20 << 20
	// Providers cap a single inline image at about 10MB, so large screenshots are downscaled to a
	// JPEG whose long side is at most this many pixels before they are sent.
	styleScreenshotMaxSide = 1600
)

// stylePresetScreenshotSkill extracts a reusable visual style from reference screenshots. Its
// wording is user-approved; raise Version whenever the prompt changes so InitSystemSkills publishes it.
var stylePresetScreenshotSkill = models.Skill{
	Name: "截图提取风格预设", Code: stylePresetScreenshotSkillCode, Operation: stylePresetScreenshotSkillCode, Version: 1,
	Description: "综合分析参考截图，提取可迁移的视觉风格，生成待确认的风格预设草稿",
	Stage:       models.SkillStageImagePrompt,
	PromptTemplate: "请综合分析随后提供的{{image_count}}张参考截图，提取一份可复用的风格预设。\n字段要求：\n" +
		"1. shared_style：所有画面共用的视觉母版，只写媒介与渲染方式、色彩体系、明暗与光线、材质纹理、景深与画面质感、整体氛围。\n" +
		"2. character_style：人物质感，只写肤质、发丝、服装材质、妆面，不写身份、动作、表情或人数。\n" +
		"3. scene_style：场景质感，只写空间氛围、环境材质、天气、光源与纵深，不写具体地点。\n" +
		"4. prop_style：道具质感，只写材质、工艺、磨损与反光，不写具体道具。\n" +
		"5. negative：与该风格冲突的通用负面约束。\n" +
		"6. warnings：截图不足以判断或你不确定的地方，逐条列出。\n" +
		"硬性规则：\n" +
		"- 不得输出截图中的人名、地名、建筑名、道具名、剧情、文字、字幕、水印或界面元素。\n" +
		"- 静态截图无法证明运镜，不得描述镜头运动或剪辑节奏。\n" +
		"- 判断不了的字段留空字符串，不要编造。\n" +
		"- 用简洁中文短语、逗号分隔，可直接接在镜头风格提示词之后。\n" +
		"- 只输出一个 JSON 对象，不要 Markdown、解释或思考过程。\n" +
		`JSON 格式：{"name":"预设名称（不超过12字）","summary":"一句话风格摘要","shared_style":"","character_style":"","scene_style":"","prop_style":"","negative":"","warnings":[]}`,
	SystemPrompt: "你是影视视觉风格分析师。只从参考截图中提取可迁移到本项目画面的视觉语言，供镜头风格提示词使用。",
	IsSystem:     true, Enabled: true, SortOrder: 22,
}

type styleScreenshotAnalysis struct {
	Name           string   `json:"name"`
	Summary        string   `json:"summary"`
	SharedStyle    string   `json:"shared_style"`
	CharacterStyle string   `json:"character_style"`
	SceneStyle     string   `json:"scene_style"`
	PropStyle      string   `json:"prop_style"`
	Negative       string   `json:"negative"`
	Warnings       []string `json:"warnings"`
}

// stylePresetDraft is returned for review only; nothing is saved until the user posts it to
// /api/presets, and it is never applied to a Shot automatically.
type stylePresetDraft struct {
	Preset   models.StylePreset      `json:"preset"`
	Analysis styleScreenshotAnalysis `json:"analysis"`
}

func parseStyleScreenshotAnalysis(raw string) (styleScreenshotAnalysis, error) {
	var analysis styleScreenshotAnalysis
	if err := json.Unmarshal([]byte(extractJSON(stripThinkBlocks(raw))), &analysis); err != nil {
		return analysis, fmt.Errorf("模型未返回有效的风格 JSON: %w", err)
	}
	trim := func(v *string) { *v = strings.TrimSpace(*v) }
	for _, v := range []*string{&analysis.Name, &analysis.Summary, &analysis.SharedStyle, &analysis.CharacterStyle, &analysis.SceneStyle, &analysis.PropStyle, &analysis.Negative} {
		trim(v)
	}
	warnings := analysis.Warnings[:0]
	for _, w := range analysis.Warnings {
		if w = strings.TrimSpace(w); w != "" {
			warnings = append(warnings, w)
		}
	}
	analysis.Warnings = warnings
	if analysis.SharedStyle == "" && analysis.CharacterStyle == "" && analysis.SceneStyle == "" && analysis.PropStyle == "" {
		return analysis, errors.New("模型没有从截图中提取到任何风格描述")
	}
	return analysis, nil
}

// stylePresetFromAnalysis maps the analysis onto a StylePreset: the shared style plus labelled
// character/scene/prop textures form the prompt tail; empty parts are omitted.
func stylePresetFromAnalysis(a styleScreenshotAnalysis) models.StylePreset {
	parts := []string{}
	if a.SharedStyle != "" {
		parts = append(parts, a.SharedStyle)
	}
	for _, labelled := range []struct{ label, value string }{{"人物质感", a.CharacterStyle}, {"场景质感", a.SceneStyle}, {"道具质感", a.PropStyle}} {
		if labelled.value != "" {
			parts = append(parts, labelled.label+"："+labelled.value)
		}
	}
	reason := a.Summary
	if len(a.Warnings) > 0 {
		reason = strings.TrimSpace(reason + "\n不确定：" + strings.Join(a.Warnings, "；"))
	}
	name := a.Name
	if name == "" {
		name = "截图风格"
	}
	return models.StylePreset{
		Name: name, Category: models.StylePresetCinematic, Subcategory: "截图提取",
		PromptTail: strings.Join(parts, "；"), NegativeTail: a.Negative, Reason: reason, Tags: "截图提取",
	}
}

// normalizeStyleScreenshot accepts PNG/JPEG/WebP, re-encodes WebP as JPEG and downscales anything
// larger than styleScreenshotMaxSide to a JPEG.
func normalizeStyleScreenshot(data []byte) (TextImage, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "png" && format != "jpeg" && format != "webp") {
		return TextImage{}, errors.New("仅支持 PNG、JPEG 或 WebP 截图")
	}
	// WebP is always re-encoded so providers only ever receive PNG/JPEG.
	if format != "webp" && max(cfg.Width, cfg.Height) <= styleScreenshotMaxSide && len(data) <= 8<<20 {
		return TextImage{MIME: "image/" + format, Data: data}, nil
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return TextImage{}, fmt.Errorf("截图解码失败: %w", err)
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, downscaleImage(img, styleScreenshotMaxSide), &jpeg.Options{Quality: 88}); err != nil {
		return TextImage{}, err
	}
	return TextImage{MIME: "image/jpeg", Data: out.Bytes()}, nil
}

// downscaleImage box-averages img so its long side is at most maxSide.
func downscaleImage(img image.Image, maxSide int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if max(w, h) <= maxSide {
		return img
	}
	nw, nh := w*maxSide/max(w, h), h*maxSide/max(w, h)
	nw, nh = max(nw, 1), max(nh, 1)
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	for y := 0; y < nh; y++ {
		y0, y1 := b.Min.Y+y*h/nh, b.Min.Y+max((y+1)*h/nh, y*h/nh+1)
		for x := 0; x < nw; x++ {
			x0, x1 := b.Min.X+x*w/nw, b.Min.X+max((x+1)*w/nw, x*w/nw+1)
			var r, g, bl, n uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					cr, cg, cb, _ := img.At(sx, sy).RGBA()
					r, g, bl, n = r+uint64(cr), g+uint64(cg), bl+uint64(cb), n+1
				}
			}
			dst.Set(x, y, color.RGBA64{R: uint16(r / n), G: uint16(g / n), B: uint16(bl / n), A: 0xffff})
		}
	}
	return dst
}

// ChatWithImageSkill runs the named curated skill with images attached and audits the call. The
// audit input hash covers the rendered prompt and every image byte.
func (s *SkillService) ChatWithImageSkill(projectID uint, code string, provider MultimodalTextProvider, params map[string]string, images []TextImage) (string, error) {
	skill, err := s.latestEnabledSkillByCode(code)
	if err != nil {
		return "", err
	}
	system, user := s.AssemblePrompt(skill, params)
	auditInput := user
	for _, img := range images {
		auditInput += "\n" + img.MIME + ":" + string(img.Data)
	}
	started := time.Now()
	output, chatErr := provider.ChatWithImages(system, user, images)
	errText := ""
	if chatErr != nil {
		errText = chatErr.Error()
	}
	auditErr := s.LogSkillUsage(projectID, skill.Stage, skill, auditInput, len(output), time.Since(started).Milliseconds(), chatErr == nil, errText)
	if chatErr != nil {
		return output, chatErr
	}
	if auditErr != nil {
		return "", fmt.Errorf("Skill 审计写入失败: %w", auditErr)
	}
	return output, nil
}

func (s *Service) multimodalUnsupported(c *gin.Context) {
	capability := TextProviderCapability{}
	if s.TextProviderFact != nil {
		capability = s.TextProviderFact.Capabilities()
	}
	c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "当前文本模型未开启看图：请在平台设置中打开「当前文本模型支持看图」", "code": "multimodal_unsupported", "capability": capability})
}

// HandleStylePresetFromScreenshots analyses 1–6 uploaded screenshots and returns an editable
// preset draft. It saves nothing.
func (s *Service) HandleStylePresetFromScreenshots(c *gin.Context) {
	if s.TextProviderFact == nil || !s.TextProviderFact.SupportsImages() {
		s.multimodalUnsupported(c)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxStyleScreenshots*maxStyleScreenshotBytes+uploadMultipartOverhead)
	form, err := c.MultipartForm()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "截图上传失败或总大小超限"})
		return
	}
	files := form.File["files"]
	if len(files) < minStyleScreenshots || len(files) > maxStyleScreenshots {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("请上传 %d–%d 张截图", minStyleScreenshots, maxStyleScreenshots)})
		return
	}
	images := make([]TextImage, 0, len(files))
	for i, header := range files {
		if header.Size > maxStyleScreenshotBytes {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("第%d张截图超过 20MB", i+1)})
			return
		}
		file, err := header.Open()
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		data, err := io.ReadAll(io.LimitReader(file, maxStyleScreenshotBytes+1))
		file.Close()
		if err != nil || len(data) > maxStyleScreenshotBytes {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("第%d张截图读取失败或超过 20MB", i+1)})
			return
		}
		img, err := normalizeStyleScreenshot(data)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("第%d张：%s", i+1, err.Error())})
			return
		}
		images = append(images, img)
	}
	projectID, _ := strconv.Atoi(c.PostForm("project_id"))
	output, err := s.Skills.ChatWithImageSkill(uint(max(projectID, 0)), stylePresetScreenshotSkillCode, s.TextProviderFact, map[string]string{"image_count": strconv.Itoa(len(images))}, images)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	analysis, err := parseStyleScreenshotAnalysis(output)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, stylePresetDraft{Preset: stylePresetFromAnalysis(analysis), Analysis: analysis})
}
