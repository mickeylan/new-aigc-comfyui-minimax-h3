package service

import (
	"embed"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"comfyui-console/internal/models"
)

// ============================================================
// short-drama 方法论集成（https://github.com/0xsline/short-drama）
// 两阶段制作：
//   阶段 1 GeneratePlan    —— 按方法论生成创作方案（剧名/三幕/节奏/付费卡点/爽感矩阵/角色/分集目录）
//   阶段 2 GenerateScript  —— 依据创作方案渲染分镜场景（供文生图 + 图生视频流水线）
// ============================================================

//go:embed shortdrama
var shortDramaFS embed.FS

// readRef 读取方法论参考文档（嵌入二进制，按需组装进 prompt）
func readRef(name string) string {
	data, err := shortDramaFS.ReadFile("shortdrama/references/" + name)
	if err != nil {
		return ""
	}
	return string(data)
}

type planCharacter struct {
	Name           string `json:"name"`
	Role           string `json:"role"`
	Arc            string `json:"arc"`
	Trait          string `json:"trait"`
	Style          string `json:"style"`
	Appearance     string `json:"appearance"`
	Personality    string `json:"personality"`
	Background     string `json:"background"`
	Relationships  string `json:"relationships"`
	Emotions       string `json:"emotions"`
	Habits         string `json:"habits"`
	WardrobeDetail string `json:"wardrobe_detail"`
	LightingMood   string `json:"lighting_mood"`
	ColorPalette   string `json:"color_palette"`
}

// 创作方案 JSON 结构（阶段 1 输出）
type dramaPlan struct {
	Title   string `json:"title"`   // 剧名
	Logline string `json:"logline"` // 一句话故事线
	Core    string `json:"core"`    // 核心冲突
	Acts    []struct {
		Name  string `json:"name"` // 幕名：入局/纠缠/决战
		Range string `json:"range"`
		Event string `json:"event"`
	} `json:"acts"` // 三幕结构
	Rhythm       string          `json:"rhythm"`       // 节奏曲线要点
	Paywall      string          `json:"paywall"`      // 付费卡点规划
	Satisfaction string          `json:"satisfaction"` // 爽感矩阵
	Characters   []planCharacter `json:"characters"`   // 主要角色（含外貌，供分镜画面一致）
	Villains     []struct {
		Layer string `json:"layer"` // 小/中/大/隐藏反派
		Name  string `json:"name"`
		Motif string `json:"motif"`
	} `json:"villains"` // 四层反派
	Props []struct {
		Name        string `json:"name"`
		Description string `json:"description"` // 外观描述（形状/材质/颜色/标志性细节）
	} `json:"props"` // 贯穿全剧的关键道具（供分镜画面保持道具一致）
	Locations []struct {
		Name        string `json:"name"`
		Description string `json:"description"` // 环境描述（空间/建筑/光线氛围）
	} `json:"locations"` // 主要场景地点（供分镜画面保持环境一致）
	Episodes []struct {
		N              int     `json:"n"`
		Title          string  `json:"title"`
		Brief          string  `json:"brief"`           // 核心冲突/爽点一句话
		Hook           string  `json:"hook"`            // 钩子类型
		Tag            string  `json:"tag"`             // 🔥关键集 💰付费卡点
		TargetDuration float64 `json:"target_duration"` // 目标总时长（秒），默认 180
		TargetScenes   int     `json:"target_scenes"`   // 目标镜头数，默认 25
	} `json:"episodes"` // 分集目录
}

// planSystemPrompt 阶段 1 系统提示词：注入方法论要点（精选规则控制 token，不全文注入）
func planSystemPrompt() string {
	genre := readRef("genre-guide.md")
	rhythm := readRef("rhythm-curve.md")
	hooks := readRef("hook-design.md")
	paywall := readRef("paywall-design.md")
	satisfaction := readRef("satisfaction-matrix.md")
	villain := readRef("villain-design.md")

	return `你是专业的微短剧编剧，精通短视频平台爆款短剧创作方法论。请根据用户提供的选题信息，产出完整的创作方案。

创作方法论（必须遵循）：
【题材要点】
` + genre + `
【节奏曲线】
` + rhythm + `
【钩子设计】
` + hooks + `
【付费卡点】
` + paywall + `
【爽感矩阵】
` + satisfaction + `
【反派体系】
` + villain + `

输出要求：
1. 只输出一个合法的 JSON 对象，不要输出任何解释、Markdown 代码块标记或其它文字。
2. JSON 结构固定为：
{
  "title": "剧名（3-8字，有网感）",
  "logline": "一句话故事线",
  "core": "核心冲突",
  "acts": [{"name": "第一幕·入局", "range": "第1-{N}集", "event": "核心事件"}],
  "rhythm": "全剧节奏曲线设计（起势/攀升/风暴/决战配比）",
  "paywall": "付费卡点规划（占全集10-15%，标注卡点集数与悬念设计）",
  "satisfaction": "爽感矩阵配比（打脸/逆袭/甜宠/虐心/悬疑/燃/搞笑/感动）",
  "characters": [{"name":"角色名","role":"身份","arc":"人物弧光","trait":"外貌摘要","style":"服装摘要","appearance":"性别呈现、年龄感、发型发色、脸型、眼神、妆造、眉形、肤色、体型和特殊标记","personality":"核心性格与行为模式","background":"出身、经历、动机和目标","relationships":"与主要角色的关系","emotions":"典型表情和肢体语言","habits":"习惯动作和口头禅","wardrobe_detail":"服装款式、配色、配饰和材质","lighting_mood":"适合角色的光影氛围","color_palette":"主色、辅色和点缀色"}],
  "villains": [{"layer": "小反派/中反派/大反派/隐藏反派", "name": "名字", "motif": "动机与行为模式"}],
  "props": [{"name": "道具名", "description": "关键道具外观（形状/材质/颜色/标志性细节），贯穿全剧反复出现，供画面生成保持一致"}],
  "locations": [{"name": "场景名", "description": "主要场景环境（空间/建筑/陈设/光线氛围），供画面生成保持一致"}],
  "episodes": [{"n": 1, "title": "集标题", "brief": "核心冲突或爽点一句话", "hook": "钩子类型（悬念钩/反转钩/情绪钩/信息钩/危机钩）", "tag": "🔥或💰或空", "target_duration": 180, "target_scenes": 25}]
}
3. episodes 必须覆盖全集数（与用户配置的集数一致），体现三幕节奏；前10集至少3个🔥和2个💰；🔥占比25-35%，💰占比10-15%。每集必须设定 target_duration（约180秒，即3分钟）和 target_scenes（约25个镜头，单镜3-15秒）。
4. 必须从故事创意中识别并生成 2~8 个主要角色（包括主角、关键配角和反派）；每个角色必须完整填写 characters 模板的全部字段。appearance 必须覆盖性别呈现、年龄感、发型发色、脸型、眼神、妆造、眉形、肤色、体型和特殊标记；wardrobe_detail 必须覆盖款式、配色、配饰和材质。角色由用户审核修改后再用于生图，禁止返回空 characters。
5. 必须给出贯穿全剧的关键道具清单 props（2~8 项，如信物/武器/法宝/手机等反复出现、影响剧情的物件）与主要场景清单 locations（2~8 个地点），每项给出具体外观/环境描述；后续分镜只引用这些名称，系统会用它们生成参考图保证道具与场景全剧一致。`
}

// scriptFromPlanSystemPrompt 阶段 2 系统提示词：依据创作方案渲染分镜场景
// targetDuration: 目标总时长（秒），默认 180
// targetScenes: 目标镜头数，默认 25
func scriptFromPlanSystemPrompt(targetDuration float64, targetScenes int) string {
	// 兼容旧项目缺失字段
	if targetDuration <= 0 {
		targetDuration = 180
	}
	if targetScenes <= 0 {
		targetScenes = 25
	}
	// 单镜时长范围：3-15 秒
	minSceneDur, maxSceneDur := 3, 15

	return `你是专业的漫剧编剧与分镜师。根据给定的创作方案，把故事转化为一条适合"文生图 + 图生视频"流水线的分镜序列。
要求：
1. 只输出一个合法的 JSON 对象，不要输出任何解释、Markdown 代码块标记或其它文字。
2. JSON 结构固定为：
{
  "script": "本集/本段剧本正文。每段发声内容使用固定标签：【动作】画面描述、【对白｜角色名】原文、【旁白】原文、【内心独白｜角色名】原文",
  "visual_bible": "汇总创作方案中主要角色的固定外貌、服装、色彩与全片统一画风",
  "scenes": [
    {
      "title": "场景1：概括性标题",
      "content": "该场景的视频提示词：描述画面动作、镜头运动（如推近/摇镜）、人物表情与对白，现在时态，1~3 句",
      "image_prompt": "该场景的静态画面提示词（用于文生图）：包含主体人物外貌特征、服装、场景环境、光影氛围、构图与画风描述",
      "duration": 5,
      "characters": ["兼容字段：与 visible_characters 相同"],
      "visible_characters": ["本镜最终画面中实际可见的角色"],
      "voice_characters": ["本镜只发声但不在画面中出现的角色"],
      "mentioned_characters": ["仅在剧情说明、对白或独白中被提及的角色"],
      "location": "该场景地点名（须与创作方案 locations 中的名称完全一致；无明确地点则为空字符串）",
      "props": ["该场景出现的关键道具名（与创作方案 props 中的名称完全一致；无则为空数组）"],
      "dialogues": [{"character": "角色名", "speech_type": "dialogue", "text": "角色说出的原文"}, {"character": "旁白", "speech_type": "narration", "text": "正文明确写出的旁白原文"}, {"character": "角色名", "speech_type": "monologue", "text": "正文明确写出的内心独白原文"}]
    }
  ]
}
【时长约束（必须严格遵循）】
- 总时长预算：约 ` + fmt.Sprintf("%.0f", targetDuration) + ` 秒
- 镜头数要求：` + fmt.Sprintf("%d", targetScenes) + ` 个镜头
- 单镜时长范围：` + fmt.Sprintf("%d", minSceneDur) + `~` + fmt.Sprintf("%d", maxSceneDur) + ` 秒（根据对白长度与动作复杂度灵活调整）
- 所有镜头时长之和应尽量接近总时长预算（允许 ±10% 偏差）
3. 人物一致性至关重要：同一角色在多个场景出现时，image_prompt 必须严格沿用创作方案中该角色的 trait（外貌特征）与 style（服装造型），且所有场景画风描述保持一致。
4. 必须区分人物用途：visible_characters 只列本镜最终画面中真实可见的人物；voice_characters 只列画外对白或内心独白的发声者；mentioned_characters 只列剧情说明、对白或独白中被提到但不会出现在画面中的人物。名字出现在文字中不等于画面出场。characters 为兼容字段，必须与 visible_characters 完全相同；只有 visible_characters 会要求人物四视图。
5. script 正文优先使用固定格式：【动作】画面描述、【对白｜角色名】原文、【旁白】原文、【内心独白｜角色名】原文。逐场检查故事正文和分镜内容中的明确发声标注，并忠实提取到 dialogues：“旁白/画外音：原文”使用 narration，“角色名内心独白：原文”使用 monologue，“角色名：原文”使用 dialogue。只复制标注后的原文，不改写、不概括、不补充。不得把“他心里疑惑”“气氛压抑”等心理、动作或氛围描写转换成独白或旁白。speech_type 只能是 dialogue、narration 或 monologue；dialogue/monologue 的 character 必须是角色名，narration 的 character 固定为“旁白”。正文和分镜内容均未明确出现可发声内容时必须为空数组。
6. 第一个场景尽量给出大场景/环境交代，后续场景聚焦人物动作与剧情推进。
7. 剧情节奏参考创作方案中的节奏曲线：开头要有钩子，中段冲突升级，结尾留悬念。
8. 道具与场景一致性：每个场景的 location 与 props 名称必须完全取自创作方案的 locations/props 清单（系统会用同名资产参考图锁定画面中该场景环境与道具外观），不得随意改名；只有确属剧情新出现的道具才允许新名称。`
}

// GeneratePlan 阶段 1：按 short-drama 方法论生成创作方案（存 project.plan）
func (s *ProjectService) GeneratePlan(p *models.Project) (*models.Project, error) {
	var user strings.Builder
	user.WriteString("故事创意：" + p.Synopsis + "\n")
	if p.Genre != "" {
		user.WriteString("题材：" + p.Genre + "\n")
	}
	if p.Audience != "" {
		user.WriteString("目标受众：" + p.Audience + "\n")
	}
	if p.Tone != "" {
		user.WriteString("故事基调：" + p.Tone + "\n")
	}
	if p.Ending != "" {
		user.WriteString("结局类型：" + p.Ending + "\n")
	}
	if p.Episodes > 0 {
		user.WriteString(fmt.Sprintf("总集数：%d 集\n", p.Episodes))
	}
	if p.Style != "" {
		user.WriteString("画风：" + p.Style + "\n")
	}
	user.WriteString("请按系统要求输出创作方案 JSON。")

	raw, err := s.chatWithSkill(p.ID, models.SkillStagePlan, planSystemPrompt(), user.String(), map[string]string{"project_info": user.String(), "episode_count": fmt.Sprint(p.Episodes)})
	if err != nil {
		return nil, err
	}
	res, err := parsePlanJSON(raw)
	if err != nil {
		return nil, fmt.Errorf("方案解析失败（可重试）: %w", err)
	}
	if err := s.ensurePlanCharacters(p, res); err != nil {
		return nil, err
	}
	planJSON, _ := json.Marshal(res)
	status := "plan_done"
	if p.AutoGenerate {
		status = "producing"
	}
	if err := s.db.Model(p).Updates(map[string]any{
		"plan": string(planJSON), "status": status, "error": "",
	}).Error; err != nil {
		return nil, err
	}
	s.pushProject(nil)
	var fresh models.Project
	if err := s.db.First(&fresh, p.ID).Error; err != nil {
		return nil, err
	}
	// 抽取创作方案中的角色为独立资产（保留已编辑角色与标准像）
	s.upsertCharactersFromPlan(&fresh, res)
	// 抽取关键道具与主要场景为独立资产（跨分镜一致性参考图）
	s.upsertAssetsFromPlan(&fresh, res)
	return &fresh, nil
}

// ensurePlanCharacters 在方案模型漏掉角色时，使用同一个文本 provider 专门分析故事并补齐角色。
// 这使“创建项目”始终先由 AI 建立可审核的角色草稿，而不是要求用户从零手工录入。
func (s *ProjectService) ensurePlanCharacters(p *models.Project, plan *dramaPlan) error {
	valid := make([]planCharacter, 0, len(plan.Characters))
	for _, ch := range plan.Characters {
		ch.Name = strings.TrimSpace(ch.Name)
		ch.Role = strings.TrimSpace(ch.Role)
		ch.Arc = strings.TrimSpace(ch.Arc)
		ch.Trait = strings.TrimSpace(ch.Trait)
		ch.Style = strings.TrimSpace(ch.Style)
		if completePlanCharacter(ch) {
			valid = append(valid, ch)
		}
	}
	if len(valid) >= 2 {
		plan.Characters = valid
		return nil
	}

	system := `你是影视角色设定师。请分析用户提供的故事内容，生成供用户审核和修改的主要角色草稿。只输出合法 JSON，不要输出 Markdown 或解释。格式：{"characters":[{"name":"角色名","role":"主角/关键配角/反派及身份","arc":"人物目标、冲突与成长弧线","trait":"稳定外貌摘要","style":"固定服装摘要","appearance":"性别呈现、年龄感、发型发色、脸型、眼神、妆造、眉形、肤色、体型和特殊标记","personality":"核心性格与行为模式","background":"出身、经历、动机和目标","relationships":"与主要角色的关系","emotions":"典型表情和肢体语言","habits":"习惯动作和口头禅","wardrobe_detail":"服装款式、配色、配饰和材质","lighting_mood":"适合角色的光影氛围","color_palette":"主色、辅色和点缀色"}]}。必须生成 2~8 个角色并完整填写全部字段，覆盖主角、关键配角和反派。`
	user := "故事内容：" + p.Synopsis
	if p.Genre != "" {
		user += "\n题材：" + p.Genre
	}
	if p.Style != "" {
		user += "\n画风：" + p.Style
	}
	if plan.Core != "" {
		user += "\n核心冲突：" + plan.Core
	}
	raw, err := s.chatWithSkill(p.ID, models.SkillStageCharacter, system, user, map[string]string{"character_info": user})
	if err != nil {
		return fmt.Errorf("AI 角色分析失败: %w", err)
	}
	var result struct {
		Characters []planCharacter `json:"characters"`
	}
	text := strings.TrimSpace(raw)
	start, end := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if start < 0 || end <= start || json.Unmarshal([]byte(text[start:end+1]), &result) != nil {
		return fmt.Errorf("AI 角色分析结果不是合法 JSON，请重试")
	}
	plan.Characters = nil
	for _, ch := range result.Characters {
		ch.Name = strings.TrimSpace(ch.Name)
		ch.Role = strings.TrimSpace(ch.Role)
		ch.Arc = strings.TrimSpace(ch.Arc)
		ch.Trait = strings.TrimSpace(ch.Trait)
		ch.Style = strings.TrimSpace(ch.Style)
		if completePlanCharacter(ch) {
			plan.Characters = append(plan.Characters, ch)
		}
	}
	if len(plan.Characters) < 2 {
		return fmt.Errorf("AI 未能从故事中识别出至少 2 个完整角色，请重试生成创作方案")
	}
	return nil
}

func completePlanCharacter(ch planCharacter) bool {
	return strings.TrimSpace(ch.Name) != "" && strings.TrimSpace(ch.Role) != "" &&
		strings.TrimSpace(ch.Trait) != "" && strings.TrimSpace(ch.Style) != "" &&
		strings.TrimSpace(ch.Appearance) != "" && strings.TrimSpace(ch.Personality) != "" &&
		strings.TrimSpace(ch.Background) != "" && strings.TrimSpace(ch.Relationships) != "" &&
		strings.TrimSpace(ch.Emotions) != "" && strings.TrimSpace(ch.Habits) != "" &&
		strings.TrimSpace(ch.WardrobeDetail) != "" && strings.TrimSpace(ch.LightingMood) != "" &&
		strings.TrimSpace(ch.ColorPalette) != ""
}

// PlanEpisodeUpdate 每集可编辑字段（标题 / 剧情提示词 / 目标时长 / 目标镜头数）
type PlanEpisodeUpdate struct {
	N              int     `json:"n"`
	Title          string  `json:"title"`
	Brief          string  `json:"brief"`
	TargetDuration float64 `json:"target_duration"` // 目标总时长（秒），默认 180
	TargetScenes   int     `json:"target_scenes"`   // 目标镜头数，默认 25
}

// UpdatePlanEpisodes 修改创作方案中分集目录的标题与剧情提示词（按集号 n 匹配）。
// 保存后重新生成剧本，修改后的提示词将生效。
func (s *ProjectService) UpdatePlanEpisodes(p *models.Project, updates []PlanEpisodeUpdate) (*models.Project, error) {
	if strings.TrimSpace(p.Plan) == "" {
		return nil, fmt.Errorf("暂无创作方案，请先生成创作方案")
	}
	var plan dramaPlan
	if err := json.Unmarshal([]byte(p.Plan), &plan); err != nil {
		return nil, fmt.Errorf("创作方案解析失败: %w", err)
	}
	byN := map[int]PlanEpisodeUpdate{}
	for _, u := range updates {
		if u.N > 0 {
			byN[u.N] = u
		}
	}
	changed := false
	for i := range plan.Episodes {
		u, ok := byN[plan.Episodes[i].N]
		if !ok {
			continue
		}
		if u.Title != "" && u.Title != plan.Episodes[i].Title {
			plan.Episodes[i].Title = u.Title
			changed = true
		}
		if u.Brief != "" && u.Brief != plan.Episodes[i].Brief {
			plan.Episodes[i].Brief = u.Brief
			changed = true
		}
		// 更新目标时长（0 表示未修改，保留旧值）
		if u.TargetDuration > 0 && u.TargetDuration != plan.Episodes[i].TargetDuration {
			plan.Episodes[i].TargetDuration = u.TargetDuration
			changed = true
		}
		// 更新目标镜头数（0 表示未修改，保留旧值）
		if u.TargetScenes > 0 && u.TargetScenes != plan.Episodes[i].TargetScenes {
			plan.Episodes[i].TargetScenes = u.TargetScenes
			changed = true
		}
	}
	if !changed {
		return nil, fmt.Errorf("没有需要更新的分集内容")
	}
	planJSON, _ := json.Marshal(plan)
	if err := s.db.Model(p).Update("plan", string(planJSON)).Error; err != nil {
		return nil, err
	}
	s.pushProject(nil)
	var fresh models.Project
	if err := s.db.First(&fresh, p.ID).Error; err != nil {
		return nil, err
	}
	return &fresh, nil
}

var planScalarStringFields = map[string]bool{
	"title": true, "logline": true, "core": true, "rhythm": true, "paywall": true, "satisfaction": true,
	"name": true, "range": true, "event": true, "role": true, "arc": true, "trait": true, "style": true,
	"appearance": true, "personality": true, "background": true, "relationships": true, "emotions": true,
	"habits": true, "wardrobe_detail": true, "lighting_mood": true, "color_palette": true,
	"layer": true, "motif": true, "description": true, "brief": true, "hook": true, "tag": true,
}

// normalizePlanScalarStrings 兼容本地小模型把字符串字段输出成单元素或多元素数组。
// 数组内容按顺序合并，避免因 title/logline 等字段类型漂移导致整个方案丢失。
func normalizePlanScalarStrings(data []byte) ([]byte, error) {
	var root any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	var walk func(any)
	walk = func(value any) {
		switch node := value.(type) {
		case map[string]any:
			for key, child := range node {
				if planScalarStringFields[key] {
					node[key] = planScalarString(child)
				} else {
					walk(child)
				}
			}
		case []any:
			for _, child := range node {
				walk(child)
			}
		}
	}
	walk(root)
	return json.Marshal(root)
}

func planScalarString(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if part := strings.TrimSpace(planScalarString(item)); part != "" {
				parts = append(parts, part)
			}
		}
		return strings.Join(parts, "；")
	default:
		encoded, _ := json.Marshal(v)
		return string(encoded)
	}
}

// parsePlanJSON 从模型输出中提取创作方案 JSON（剥离 markdown 包裹与杂文）
func parsePlanJSON(raw string) (*dramaPlan, error) {
	text := strings.TrimSpace(raw)
	if i := strings.Index(text, "```"); i >= 0 {
		rest := text[i+3:]
		if j := strings.Index(rest, "\n"); j >= 0 {
			rest = rest[j+1:]
		}
		if k := strings.LastIndex(rest, "```"); k >= 0 {
			rest = rest[:k]
		}
		text = strings.TrimSpace(rest)
	}
	start, end := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("输出中未找到 JSON 对象")
	}
	data := []byte(text[start : end+1])
	data, err := normalizePlanScalarStrings(data)
	if err != nil {
		return nil, err
	}
	var res dramaPlan
	if err := json.Unmarshal(data, &res); err != nil {
		return nil, err
	}
	if res.Logline == "" || len(res.Episodes) == 0 {
		return nil, fmt.Errorf("方案缺少必要字段（logline/episodes）")
	}
	return &res, nil
}
