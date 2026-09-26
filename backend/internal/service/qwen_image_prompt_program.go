package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"comfyui-console/internal/models"
)

const (
	QwenImage21T2IProgram           = "qwen-image-2.1-t2i"
	QwenImage21EditProgram          = "qwen-image-2.1-multi-image-edit"
	QwenImage21CharacterCardProgram = "qwen-image-2.1-character-card"
)

var ErrPromptProgramNotFound = errors.New("prompt program not found")

type PromptProgram struct {
	Code          string `json:"code"`
	Name          string `json:"name"`
	TargetModel   string `json:"target_model"`
	TargetMode    string `json:"target_mode"`
	TemplateCode  string `json:"template_code,omitempty"`
	TemplateReady bool   `json:"template_ready"`
	MaxReferences int    `json:"max_references"`
	Description   string `json:"description"`
}

type PromptReference struct {
	Role        string `json:"role"`
	Description string `json:"description"`
}

type PromptProgramInput struct {
	Brief       string            `json:"brief"`
	Context     string            `json:"context"`
	AspectRatio string            `json:"aspect_ratio"`
	References  []PromptReference `json:"references"`
}

type PromptProgramResult struct {
	ProgramCode string `json:"program_code"`
	TargetModel string `json:"target_model"`
	TargetMode  string `json:"target_mode"`
	Prompt      string `json:"prompt"`
	WHRatio     string `json:"wh_ratio"`
	RatioFollow string `json:"ratio_follow,omitempty"`
	Provider    string `json:"provider"`
}

type QwenImagePromptProgramService struct {
	provider TextProvider
	skills   *SkillService
}

func NewQwenImagePromptProgramService(provider TextProvider, skills ...*SkillService) *QwenImagePromptProgramService {
	var skillService *SkillService
	if len(skills) > 0 {
		skillService = skills[0]
	}
	return &QwenImagePromptProgramService{provider: provider, skills: skillService}
}

func (s *QwenImagePromptProgramService) List() []PromptProgram {
	return []PromptProgram{
		{Code: QwenImage21T2IProgram, Name: "Qwen-Image-2.1 文生图提示词", TargetModel: "qwen-image-2.1", TargetMode: "text-to-image", MaxReferences: 0, Description: "把任意长度需求扩写为完整英文观察式画面描述，画幅独立返回，不写进正文。"},
		{Code: QwenImage21EditProgram, Name: "Qwen-Image-2.1 多图编辑提示词", TargetModel: "qwen-image-2.1", TargetMode: "multi-image-edit", MaxReferences: 16, Description: "区分保守编辑与参考造景，按有序 <imageN> 明确画布、身份和每张图的唯一职责。"},
		{Code: QwenImage21CharacterCardProgram, Name: "Qwen-Image-2.1 角色设定卡", TargetModel: "qwen-image-2.1", TargetMode: "character-card", MaxReferences: 0, Description: "生成独立的专业角色设定卡提示词；不修改现有 Krea2 四视图流程。"},
	}
}

func (s *QwenImagePromptProgramService) program(code string) (PromptProgram, error) {
	for _, p := range s.List() {
		if p.Code == code {
			return p, nil
		}
	}
	return PromptProgram{}, ErrPromptProgramNotFound
}

var ratioPattern = regexp.MustCompile(`^[1-9][0-9]*:[1-9][0-9]*$`)

func validatePromptProgramInput(program PromptProgram, in PromptProgramInput) error {
	in.Brief = strings.TrimSpace(in.Brief)
	if in.Brief == "" {
		return fmt.Errorf("brief 不能为空")
	}
	if len([]rune(in.Brief)) > 12000 || len([]rune(in.Context)) > 20000 {
		return fmt.Errorf("输入过长")
	}
	if in.AspectRatio != "" && !ratioPattern.MatchString(strings.TrimSpace(in.AspectRatio)) {
		return fmt.Errorf("aspect_ratio 必须是 W:H")
	}
	if program.TargetMode != "multi-image-edit" && len(in.References) != 0 {
		return fmt.Errorf("%s 程序不接受参考图", program.Name)
	}
	if program.TargetMode == "multi-image-edit" {
		if len(in.References) == 0 || len(in.References) > program.MaxReferences {
			return fmt.Errorf("多图编辑需要 1–%d 张有序参考图", program.MaxReferences)
		}
		for i, ref := range in.References {
			if strings.TrimSpace(ref.Role) == "" {
				return fmt.Errorf("第%d张参考图缺少职责", i+1)
			}
			if len([]rune(ref.Role))+len([]rune(ref.Description)) > 4000 {
				return fmt.Errorf("第%d张参考图说明过长", i+1)
			}
		}
	}
	return nil
}

const qwenImage21T2ISystem = `You are the Qwen-Image-2.1 text-to-image prompt program. Return exactly one JSON object: {"rewritten_prompt":"...","wh_ratio":"W:H"}. There are no input images. rewritten_prompt is one continuous English paragraph, normally about 20 complete sentences and 400-500 words even for a short brief, observing the finished image in present tense and third person rather than commanding a renderer. Preserve every fixed subject, count, named object, colour, position and exact visible string. Open with orientation, style, an explicit medium noun, subject, background and palette. Walk the frame with 8-14 concrete spatial anchors spanning centre, four sides and corners; enumerate rather than saying several or various; describe observable life stage, pose, materials, texture and occlusion without inventing brands. State lighting source, direction, quality, resulting shadows and highlights in its own sentence. End with exactly one composition overview and nothing after it. Put ratio only in wh_ratio, never in prose. Visible text is character-for-character in straight double quotes and keeps its original script; do not invent uncertain readable text. For RGBA transparency, state transparency at both beginning and end. Never use directives, make sure, masterpiece, best quality, highly detailed, 8K, awards, negative prompts, Markdown or explanation.`

const qwenImage21EditSystem = `You are the Qwen-Image-2.1 image-editing prompt program. Return exactly one JSON object: {"rewritten_prompt":"...","wh_ratio":"","ratio_follow":"<imageN>"}. This always has input images and rewritten_prompt is one continuous actionable paragraph beginning with an operation verb. First decide whether this is a restrained edit of a canvas or a new composition built from referenced subjects. For restrained edits, change only explicitly named attributes strongly and unmistakably, then preserve all untargeted content in one concise positive preservation clause; never redraw preserved identity by describing facial features. For new compositions, actively construct professional scene, composition, lighting and layout while taking identity and supplied objects directly from their assigned references. With one image, do not emit <image1>; say the image naturally. With two or more, cite every supplied image individually and only as ordered <image1>, <image2>, etc.; state which is canvas, what each source provides, and never compress a range or mix roles. Preserve face identity, personal accessories, exact product design, logos, counts and rendering medium unless explicitly targeted. If removal, movement or reveal exposes an area, specify physically consistent fill, perspective, material, edge, shadow and light. Chinese request gives Chinese prose, English gives English, other languages give English. Visible text is a literal character-for-character string in straight double quotes, with language chosen from explicit request, then existing image language, then request language; never invent uncertain text. Use positive target states, not prohibition lists. Outpainting must say outpainting and explain continuation. Canvas edits set ratio_follow; new compositions set wh_ratio. They are mutually exclusive and ratios/resolutions never enter prose. Output no Markdown or explanation.`

const qwenImage21CharacterCardSystem = `You are the Qwen-Image-2.1 professional character-design-card prompt program. Return exactly one JSON object: {"rewritten_prompt":"...","wh_ratio":"W:H"}. This is a separate Qwen text-to-image design-card workflow and never modifies any Krea2 four-view prompt. Output one continuous Chinese prompt paragraph only inside rewritten_prompt, with no headings, numbered list, Markdown, English labels or explanation. Choose a white or light-grey production sheet for modern, school, science-fiction, mecha or contemporary characters, and a dark ink-toned Eastern-fantasy sheet with restrained Chinese ornament for xianxia, wuxia, mythology and historical fantasy. Respect an explicit CG, 3D or live-action mode; otherwise use refined semi-realistic 2.5D CG. Lock one character DNA across every panel: gender, life stage, body silhouette and proportions, face identity, hairstyle and colour, eye structure and colour, skin, complete outfit, footwear, accessories, weapon, unique marks, theme palette, identity and visual medium. Lay out a dominant full-body key art, clean front/side/back views of the exact same design, a large face and head detail, four to six evidence-based details, distinct expression studies, palette/material swatches, and only weapons, equipment or spirit beasts actually present in the brief. Every view preserves identical hair, clothes, shoes, accessories and weapon geometry. For spirit beasts, preserve real animal anatomy before fantasy additions, keep species-appropriate eyes, and separate human and beast details. Keep orthographic views free of effects; effects remain restrained around key art and never obscure design evidence. All visible labels and profile fields are concise Chinese, occupy at most fifteen percent, contain no English or gibberish, and never cover the character. Put the chosen layout ratio only in wh_ratio, never in rewritten_prompt. Output no quality-booster filler such as 8K, masterpiece or best quality.`

func buildPromptProgramUser(program PromptProgram, in PromptProgramInput) string {
	var b strings.Builder
	b.WriteString("User request:\n" + strings.TrimSpace(in.Brief))
	if c := strings.TrimSpace(in.Context); c != "" {
		b.WriteString("\n\nAdditional fixed context:\n" + c)
	}
	if r := strings.TrimSpace(in.AspectRatio); r != "" {
		b.WriteString("\n\nExplicit output aspect ratio: " + r)
	}
	if program.TargetMode == "multi-image-edit" {
		b.WriteString("\n\nOrdered input image roles (order is binding):")
		for i, ref := range in.References {
			fmt.Fprintf(&b, "\n<image%d>: role=%s", i+1, strings.TrimSpace(ref.Role))
			if d := strings.TrimSpace(ref.Description); d != "" {
				b.WriteString("; visible facts=" + d)
			}
		}
	}
	return b.String()
}

func parseQwenImagePromptResult(raw string, edit bool) (string, string, string, error) {
	var out struct {
		RewrittenPrompt string `json:"rewritten_prompt"`
		WHRatio         string `json:"wh_ratio"`
		RatioFollow     string `json:"ratio_follow"`
	}
	if err := parseJSONObject(raw, &out); err != nil {
		return "", "", "", err
	}
	out.RewrittenPrompt, out.WHRatio, out.RatioFollow = strings.TrimSpace(out.RewrittenPrompt), strings.TrimSpace(out.WHRatio), strings.TrimSpace(out.RatioFollow)
	if out.RewrittenPrompt == "" || strings.Contains(out.RewrittenPrompt, "```") {
		return "", "", "", fmt.Errorf("模型未返回有效 rewritten_prompt")
	}
	if strings.ContainsAny(out.RewrittenPrompt, "\r\n") {
		out.RewrittenPrompt = strings.Join(strings.Fields(out.RewrittenPrompt), " ")
	}
	if out.WHRatio != "" && !ratioPattern.MatchString(out.WHRatio) {
		return "", "", "", fmt.Errorf("模型返回无效 wh_ratio")
	}
	if edit {
		if (out.WHRatio == "") == (out.RatioFollow == "") {
			return "", "", "", fmt.Errorf("wh_ratio 与 ratio_follow 必须且只能填写一个")
		}
		if out.RatioFollow != "" && !regexp.MustCompile(`^<image[1-9][0-9]*>$`).MatchString(out.RatioFollow) {
			return "", "", "", fmt.Errorf("模型返回无效 ratio_follow")
		}
	} else if out.WHRatio == "" {
		return "", "", "", fmt.Errorf("文生图必须返回 wh_ratio")
	}
	return out.RewrittenPrompt, out.WHRatio, out.RatioFollow, nil
}

func (s *QwenImagePromptProgramService) Generate(code string, in PromptProgramInput) (PromptProgramResult, error) {
	program, err := s.program(code)
	if err != nil {
		return PromptProgramResult{}, err
	}
	if err = validatePromptProgramInput(program, in); err != nil {
		return PromptProgramResult{}, err
	}
	if s.provider == nil {
		return PromptProgramResult{}, fmt.Errorf("文本生成服务未配置")
	}
	system := qwenImage21T2ISystem
	switch program.TargetMode {
	case "multi-image-edit":
		system = qwenImage21EditSystem
	case "character-card":
		system = qwenImage21CharacterCardSystem
	}
	request := buildPromptProgramUser(program, in)
	var raw string
	if s.skills != nil {
		stage := models.SkillStageQwenImageT2I
		switch program.TargetMode {
		case "multi-image-edit":
			stage = models.SkillStageQwenImageEdit
		case "character-card":
			stage = models.SkillStageQwenCharacterCard
		}
		raw, err = s.skills.ChatWithConfiguredOrFallbackSkill(0, stage, program.Code, s.provider, "", "", map[string]string{"request": request})
	} else {
		raw, err = s.provider.Chat(system, request)
	}
	if err != nil {
		return PromptProgramResult{}, fmt.Errorf("提示词生成失败: %w", err)
	}
	prompt, ratio, follow, err := parseQwenImagePromptResult(raw, program.TargetMode == "multi-image-edit")
	if err != nil {
		return PromptProgramResult{}, err
	}
	return PromptProgramResult{ProgramCode: program.Code, TargetModel: program.TargetModel, TargetMode: program.TargetMode, Prompt: prompt, WHRatio: ratio, RatioFollow: follow, Provider: s.provider.Name()}, nil
}

func (r PromptProgramResult) MarshalJSON() ([]byte, error) {
	type alias PromptProgramResult
	return json.Marshal(alias(r))
}
