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
	QwenImage21T2IProgram  = "qwen-image-2.1-t2i"
	QwenImage21EditProgram = "qwen-image-2.1-multi-image-edit"
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
		{Code: QwenImage21T2IProgram, Name: "Qwen-Image-2.1 文生图提示词", TargetModel: "qwen-image-2.1", TargetMode: "text-to-image", MaxReferences: 0, Description: "把简短需求扩写为描述最终画面的英文提示词，并独立返回画幅比例。"},
		{Code: QwenImage21EditProgram, Name: "Qwen-Image-2.1 多图编辑提示词", TargetModel: "qwen-image-2.1", TargetMode: "multi-image-edit", MaxReferences: 16, Description: "按有序 <imageN> 绑定每张参考图职责，明确修改项、身份来源、画布与保留项。"},
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
	if program.TargetMode == "text-to-image" && len(in.References) != 0 {
		return fmt.Errorf("文生图程序不接受参考图")
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

const qwenImage21T2ISystem = `You are a Qwen-Image-2.1 text-to-image prompt rewriting expert. Return exactly one JSON object: {"rewritten_prompt":"...","wh_ratio":"W:H"}. The rewritten_prompt is one continuous English paragraph describing the finished image as an observer, never instructions to an AI. Preserve every user-fixed subject, count, named object, colour, position and exact visible text. Put aspect ratio only in wh_ratio. Establish medium, style, subject and background first; then walk the frame using explicit positions; enumerate concrete elements and materials; state lighting source, direction, quality, shadows and highlights; end with exactly one whole-composition sentence. Keep visible text character-for-character inside straight double quotes in its original script. Do not invent readable text. Do not use quality-booster filler such as masterpiece, 8K or award-winning. Output no Markdown or explanation.`

const qwenImage21EditSystem = `You are a Qwen-Image-2.1 multi-image editing prompt rewriting expert. Return exactly one JSON object: {"rewritten_prompt":"...","wh_ratio":"","ratio_follow":"<imageN>"}. Write one continuous actionable paragraph. Edit only attributes explicitly requested, strongly and unmistakably; preserve all untargeted content, identity, exact product design, counts, personal accessories and rendering medium. For two or more images, refer to every image only as <image1>, <image2>, etc., in supplied order; state each image's role and what is taken from it. Support at most sixteen ordered images. Never use 图1, first image or image A. Identify the canvas image when one exists and put it in ratio_follow. For a new composition with no canvas, set ratio_follow to empty and choose wh_ratio. wh_ratio and ratio_follow are mutually exclusive. Never put ratios or resolutions in rewritten_prompt. Do not invent facts absent from the supplied image-role descriptions. Chinese user instruction produces Chinese descriptive prose; English produces English; other languages use English. Exact visible text remains in its required script inside straight double quotes. Output no Markdown or explanation.`

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
	if program.TargetMode == "multi-image-edit" {
		system = qwenImage21EditSystem
	}
	request := buildPromptProgramUser(program, in)
	var raw string
	if s.skills != nil {
		stage := models.SkillStageQwenImageT2I
		if program.TargetMode == "multi-image-edit" {
			stage = models.SkillStageQwenImageEdit
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
