package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"comfyui-console/internal/models"
)

// 资产交互设计：AI 出稿 → 用户指方向 → AI 改稿 → 用户确认后由前端保存。
// 每次只调用一次文本模型；草稿不落库，只有长期方向和派生关系随资产保存。

const (
	assetDesignSceneLimit    = 6
	assetDesignSnippetRunes  = 200
	assetDesignDirectionMax  = 20
	assetDesignDirectionRune = 200
)

// 来源标注只允许这四种，前端按此着色。
var assetDesignSourceTags = map[string]string{
	"沿用": "沿用", "按你的要求": "按你的要求", "AI补充": "AI补充", "与原文不同": "与原文不同",
}

type AssetDesignRequest struct {
	AssetID     uint     `json:"asset_id"`
	Name        string   `json:"name"`
	BaseID      uint     `json:"base_id"`
	Ideas       string   `json:"ideas"`
	Directions  []string `json:"directions"`
	Current     string   `json:"current"`
	Instruction string   `json:"instruction"`
	Selection   string   `json:"selection"`
}

type AssetDesignSource struct {
	Tag  string `json:"tag"`
	Text string `json:"text"`
}

type AssetDesignDraft struct {
	Description string              `json:"description"`
	Sources     []AssetDesignSource `json:"sources"`
	Changes     []string            `json:"changes"`
	Warning     string              `json:"warning,omitempty"`
}

type AssetDesignOption struct {
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

// ParseAssetDesignDirections 把保存的多行方向拆成列表（去空行、去重）。
func ParseAssetDesignDirections(text string) []string {
	return normalizeAssetDesignDirections(strings.Split(text, "\n"))
}

func normalizeAssetDesignDirections(items []string) []string {
	out := make([]string, 0, len(items))
	seen := map[string]bool{}
	for _, item := range items {
		item = strings.TrimSpace(strings.ReplaceAll(item, "\n", " "))
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, truncateRunes(item, assetDesignDirectionRune))
		if len(out) == assetDesignDirectionMax {
			break
		}
	}
	return out
}

func joinAssetDesignLines(items []string) string {
	return strings.Join(normalizeAssetDesignDirections(items), "\n")
}

// assetDesignBase 读取派生来源：必须同项目同类别。
func (s *ProjectService) assetDesignBase(projectID uint, kind string, baseID, selfID uint) (*models.Asset, error) {
	if baseID == 0 {
		return nil, nil
	}
	if baseID == selfID {
		return nil, fmt.Errorf("不能以自身作为派生来源")
	}
	var base models.Asset
	if err := s.db.Where("id = ? AND project_id = ? AND kind = ?", baseID, projectID, kind).First(&base).Error; err != nil {
		return nil, fmt.Errorf("派生来源%s不存在或已删除", AssetKindLabel(kind))
	}
	return &base, nil
}

// validateAssetDerivation 保证来源存在、同类别且不形成派生环。
func (s *ProjectService) validateAssetDerivation(projectID uint, kind string, baseID, selfID uint) error {
	if baseID == 0 {
		return nil
	}
	if _, err := s.assetDesignBase(projectID, kind, baseID, selfID); err != nil {
		return err
	}
	next := baseID
	for i := 0; i < 50 && next != 0; i++ {
		if selfID != 0 && next == selfID {
			return fmt.Errorf("派生关系不能形成循环")
		}
		var a models.Asset
		if err := s.db.Select("id", "derived_from_id").Where("id = ? AND project_id = ?", next, projectID).First(&a).Error; err != nil {
			return nil // 链上来源已删除不影响本次指定
		}
		next = a.DerivedFromID
	}
	return nil
}

// assetDesignSceneSnippets 取资产出场的分镜片段作为设计依据；新派生资产尚未出场时退用来源的分镜。
func (s *ProjectService) assetDesignSceneSnippets(projectID uint, kind string, names ...string) []string {
	var scenes []models.Scene
	if err := s.db.Select("id", "episode_n", "order", "title", "content", "location_name", "props").
		Where("project_id = ? AND (archived = ? OR archived IS NULL)", projectID, false).
		Order("episode_n ASC, `order` ASC, id ASC").Find(&scenes).Error; err != nil {
		return nil
	}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		var out []string
		for _, sc := range scenes {
			matched := false
			if kind == AssetKindLocation {
				matched = strings.TrimSpace(sc.LocationName) == name
			} else {
				for _, pn := range parseSceneCharacters(sc.Props) {
					matched = matched || pn == name
				}
			}
			if !matched {
				continue
			}
			body := strings.Join(strings.Fields(sc.Content), " ")
			out = append(out, fmt.Sprintf("第%d集场景%d「%s」：%s", sc.EpisodeN, sc.Order, strings.TrimSpace(sc.Title), truncateRunes(body, assetDesignSnippetRunes)))
			if len(out) == assetDesignSceneLimit {
				break
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return nil
}

func assetDesignRules(kind string) string {
	if kind == AssetKindLocation {
		return "这是场景：写清空间类型、建筑结构、固定布局、入口出口、前中后景、主要陈设、材质、主辅色、光源方向、时段与氛围；必须是无人物环境设定。"
	}
	return "这是道具：写清类别、形状比例、材质、颜色、纹样、磨损、可识别特征和尺寸参照；必须是无生命单一物件，不得因名称含人物或生物词而拟人化。"
}

const assetDesignOutputContract = `只输出一个 JSON 对象，不要输出 JSON 以外的任何内容：
{"description":"完整中文设定，一段具体描述","sources":[{"tag":"沿用","text":"description 中的原句或短语"}],"changes":["相对基础资产的一条可见变化"]}
sources 逐条标注 description 中的句子或短语来源，tag 只能是：
- 沿用：来自基础资产或已有设定；
- 按你的要求：落实用户的长期方向、本次想法或修改指令；
- AI补充：你新补的细节；
- 与原文不同：与分镜片段描述不一致之处（尽量避免；必须出现时如实标出）。
text 必须逐字摘自 description。changes 只在有基础资产时填写，每条一句可直接执行的画面变化（例如“巨石已碎成散落的碎块”）；没有基础资产时为空数组。`

func (s *ProjectService) assetDesignUserContext(p *models.Project, kind string, req AssetDesignRequest, base *models.Asset) string {
	var b strings.Builder
	fmt.Fprintf(&b, "项目：%s\n题材：%s\n画风：%s\n故事：%s\n资产类型：%s\n名称：%s\n", p.Title, p.Genre, p.Style, p.Synopsis, AssetKindLabel(kind), strings.TrimSpace(req.Name))
	if base != nil {
		desc := strings.TrimSpace(base.Description)
		if desc == "" {
			desc = "（来源没有文字设定，只能依据其名称）"
		}
		fmt.Fprintf(&b, "基础资产（本资产由它派生）：%s「%s」：%s\n", AssetKindLabel(kind), base.Name, desc)
	}
	if directions := normalizeAssetDesignDirections(req.Directions); len(directions) > 0 {
		b.WriteString("用户的长期方向（必须全部体现）：\n")
		for i, d := range directions {
			fmt.Fprintf(&b, "%d. %s\n", i+1, d)
		}
	}
	names := []string{req.Name}
	if base != nil {
		names = append(names, base.Name)
	}
	if snippets := s.assetDesignSceneSnippets(p.ID, kind, names...); len(snippets) > 0 {
		b.WriteString("该资产在分镜中的出场片段（设计依据）：\n")
		for _, line := range snippets {
			b.WriteString("- " + line + "\n")
		}
	}
	return b.String()
}

func parseAssetDesignDraft(output, kind string) (*AssetDesignDraft, error) {
	output = strings.TrimSpace(stripThinkBlocks(output))
	var parsed AssetDesignDraft
	if err := json.Unmarshal([]byte(extractJSON(output)), &parsed); err == nil && strings.TrimSpace(parsed.Description) != "" {
		draft := &AssetDesignDraft{Description: strings.TrimSpace(parsed.Description), Sources: []AssetDesignSource{}, Changes: []string{}}
		if len([]rune(draft.Description)) < 20 {
			return nil, fmt.Errorf("AI 返回的%s描述过短，请重试", AssetKindLabel(kind))
		}
		for _, src := range parsed.Sources {
			tag := assetDesignSourceTags[strings.ReplaceAll(strings.TrimSpace(src.Tag), " ", "")]
			text := strings.TrimSpace(src.Text)
			if tag != "" && text != "" && strings.Contains(draft.Description, text) {
				draft.Sources = append(draft.Sources, AssetDesignSource{Tag: tag, Text: text})
			}
		}
		draft.Changes = normalizeAssetDesignDirections(parsed.Changes)
		return draft, nil
	}
	// 不追加修复调用：把原文当作描述返回，并提示未能标注来源。
	raw := strings.TrimSpace(strings.Trim(output, "`"))
	if len([]rune(raw)) < 20 {
		return nil, fmt.Errorf("AI 返回的%s描述过短，请重试", AssetKindLabel(kind))
	}
	return &AssetDesignDraft{Description: raw, Sources: []AssetDesignSource{}, Changes: []string{}, Warning: "AI 未按格式返回，已直接使用原文，未能标注来源"}, nil
}

func (s *ProjectService) assetDesignPrepare(p *models.Project, kind string, req *AssetDesignRequest) (string, *models.Asset, error) {
	kind, err := normalizeAssetKind(kind)
	if err != nil {
		return "", nil, err
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return "", nil, fmt.Errorf("请先填写%s名称，AI需要名称来识别设计对象", AssetKindLabel(kind))
	}
	if s.textProvider == nil {
		return "", nil, fmt.Errorf("文本生成服务未配置")
	}
	base, err := s.assetDesignBase(p.ID, kind, req.BaseID, req.AssetID)
	if err != nil {
		return "", nil, err
	}
	return kind, base, nil
}

// DraftAssetDesign 生成第一稿：用户想法为空时由 AI 做主。
func (s *ProjectService) DraftAssetDesign(p *models.Project, kind string, req AssetDesignRequest) (*AssetDesignDraft, error) {
	kind, base, err := s.assetDesignPrepare(p, kind, &req)
	if err != nil {
		return nil, err
	}
	system := "你是影视美术设定师，和用户一起设计可长期复用的" + AssetKindLabel(kind) + "权威设定。\n" +
		"严格遵守项目题材、时代、地域文化和画风，不添加人物或剧情，不虚构剧情用途、法术能力、特殊机关、所有者或人物属性。\n" +
		assetDesignRules(kind) + "\n" +
		"用户的长期方向和本次想法优先，必须体现；用户没给想法时由你依据名称、分镜片段与项目上下文做保守设计。\n" +
		"有基础资产时，这是从基础资产派生出的新" + AssetKindLabel(kind) + "：除用户要求和名称明确暗示的变化外，沿用基础资产的结构、材质、色调与画风。\n" +
		assetDesignOutputContract
	user := s.assetDesignUserContext(p, kind, req, base)
	if current := strings.TrimSpace(req.Current); current != "" {
		user += "已有设定（可沿用）：" + current + "\n"
	}
	ideas := strings.TrimSpace(req.Ideas)
	if ideas == "" {
		ideas = "未提供，由你做主"
	}
	user += "用户本次想法：" + ideas
	output, err := s.textProvider.Chat(system, user)
	if err != nil {
		return nil, fmt.Errorf("AI 设计%s失败: %w", AssetKindLabel(kind), err)
	}
	return parseAssetDesignDraft(output, kind)
}

// ReviseAssetDesign 按用户方向修改当前稿，未指出的部分保持原文。
func (s *ProjectService) ReviseAssetDesign(p *models.Project, kind string, req AssetDesignRequest) (*AssetDesignDraft, error) {
	kind, base, err := s.assetDesignPrepare(p, kind, &req)
	if err != nil {
		return nil, err
	}
	current, instruction := strings.TrimSpace(req.Current), strings.TrimSpace(req.Instruction)
	if current == "" {
		return nil, fmt.Errorf("没有可修改的当前稿")
	}
	if instruction == "" {
		return nil, fmt.Errorf("请填写这次要怎么改")
	}
	selection := strings.TrimSpace(req.Selection)
	if selection != "" && !strings.Contains(current, selection) {
		return nil, fmt.Errorf("选中的文字已不在当前稿中，请重新选择")
	}
	system := "你是影视美术设定师，正在按用户的方向修改一份" + AssetKindLabel(kind) + "设定稿。\n" +
		"只改用户这次指出的地方；给了选中片段时只改该片段及必要的衔接词，其余句子逐字保留。不添加人物或剧情。\n" +
		assetDesignRules(kind) + "\n" +
		"修改后输出完整设定。sources 针对修改后的完整 description 标注；本次改动的部分标“按你的要求”。\n" +
		"有基础资产时，changes 是修改后整份设定相对基础资产的完整变化清单（保留之前已有的变化），不是只列本次改动。\n" +
		assetDesignOutputContract
	user := s.assetDesignUserContext(p, kind, req, base) + "当前稿：" + current + "\n"
	if selection != "" {
		user += "用户选中的片段：" + selection + "\n"
	}
	user += "这次的修改方向：" + instruction
	output, err := s.textProvider.Chat(system, user)
	if err != nil {
		return nil, fmt.Errorf("AI 修改%s设定失败: %w", AssetKindLabel(kind), err)
	}
	return parseAssetDesignDraft(output, kind)
}

// AssetDesignOptions 先给 3 个可选方向，帮用户决定往哪走。
func (s *ProjectService) AssetDesignOptions(p *models.Project, kind string, req AssetDesignRequest) ([]AssetDesignOption, error) {
	kind, base, err := s.assetDesignPrepare(p, kind, &req)
	if err != nil {
		return nil, err
	}
	system := "你是影视美术设定师。为这个" + AssetKindLabel(kind) + "提出 3 个明显不同、都符合项目题材画风与分镜片段的设计方向，供用户挑选。\n" +
		"不添加人物或剧情，不虚构剧情用途。必须体现用户已有的长期方向。\n" +
		`只输出 JSON：{"options":[{"title":"不超过12字的方向名","detail":"一两句可见画面特征"}]}，恰好 3 项。`
	user := s.assetDesignUserContext(p, kind, req, base)
	if ideas := strings.TrimSpace(req.Ideas); ideas != "" {
		user += "用户本次想法：" + ideas
	}
	output, err := s.textProvider.Chat(system, user)
	if err != nil {
		return nil, fmt.Errorf("AI 生成%s方向失败: %w", AssetKindLabel(kind), err)
	}
	var parsed struct {
		Options []AssetDesignOption `json:"options"`
	}
	if err := json.Unmarshal([]byte(extractJSON(stripThinkBlocks(output))), &parsed); err != nil {
		return nil, fmt.Errorf("AI 未按格式返回方向，请重试")
	}
	options := make([]AssetDesignOption, 0, 3)
	for _, o := range parsed.Options {
		o.Title, o.Detail = strings.TrimSpace(o.Title), strings.TrimSpace(o.Detail)
		if o.Title != "" && o.Detail != "" {
			options = append(options, o)
		}
	}
	if len(options) == 0 {
		return nil, fmt.Errorf("AI 未按格式返回方向，请重试")
	}
	if len(options) > 3 {
		options = options[:3]
	}
	return options, nil
}
