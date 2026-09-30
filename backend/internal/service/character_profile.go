package service

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"comfyui-console/internal/models"
)

// CharacterProfileService 角色档案服务：LLM 生成 + 审核工作流 + 参考像提示词
type CharacterProfileService struct {
	db           *gorm.DB
	textProvider TextProvider
	skills       *SkillService
}

// NewCharacterProfileService 创建角色档案服务
func NewCharacterProfileService(db *gorm.DB, textProvider TextProvider) *CharacterProfileService {
	return &CharacterProfileService{
		db:           db,
		textProvider: textProvider,
	}
}

// characterProfileResult LLM 返回的角色档案结构
type characterProfileResult struct {
	Name           string `json:"name"`
	Role           string `json:"role"`
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

// characterProfileSystemPrompt 生成角色档案的系统提示词
const characterProfileSystemPrompt = `你是一位专业的漫剧角色设计师。根据用户提供的角色信息和故事背景，生成一份详细的结构化角色档案。
要求：
1. 只输出一个合法的 JSON 对象，不要输出任何解释、Markdown 代码块标记或其它文字。
2. JSON 结构固定为：
{
  "name": "角色名（与输入保持一致）",
  "role": "角色身份定位（主角/女主/反派/配角等）",
  "appearance": "详细外貌描述：发型（形状/长度/颜色/质感）、脸型、眉眼（形状/眼神特点）、鼻型、唇形、肤色、身材（高矮/体型/气质）、特殊标记（胎记/疤痕/饰品等）",
  "personality": "性格特点：MBTI 性格类型、核心性格标签（3-5个）、行为模式、情绪表达习惯、与他人相处方式",
  "background": "背景故事：出身背景、成长经历、关键事件、角色动机、个人目标与欲望、内心冲突与矛盾",
  "relationships": "关系图谱：该角色与其他角色的关系描述（亲子/恋人/朋友/敌人等），关系动态变化",
  "emotions": "情绪表达：该角色在喜悦/愤怒/悲伤/惊讶等情绪下的具体表现方式，面部表情和肢体语言特点",
  "habits": "习惯动作：常见小动作（摸头发/托腮等）、口头禅、特殊习惯、紧张/放松时的标志性行为",
  "wardrobe_detail": "完整服装细节：日常服装描述（材质/颜色/款式）、上装、下装、腰带、重要配饰以及明确鞋履（鞋型/材质/颜色）；重要场合造型和随时间变化的造型演变",
  "lighting_mood": "光影氛围：适合该角色的打光风格（柔和/硬朗/戏剧性）、主光源方向、氛围偏好（暖色调/冷色调）",
  "color_palette": "角色色调：主色调、辅色调、点缀色，以及与角色性格的关联"
}
3. 输出的内容应符合漫剧风格，适合后续 AI 生图和视频生成。
4. 每个字段都要有实质性内容，不要为空。
5. appearance 和 wardrobe_detail 要足够详细，能支撑高质量的参考像生成。
6. 如果输入或故事明确给出年龄，appearance 必须在开头原样保留准确年龄（例如“22岁青年女性”），不得用“成熟、资深、威严”等身份语义改变视觉年龄；30岁以下角色应描述符合该年龄的面部骨骼、紧致皮肤和自然妆容，禁止擅自增加法令纹、眼袋、皱纹或中年感。
7. wardrobe_detail 必须明确写出鞋履。鞋履严格符合故事时代、地域文化、身份和服装：中国古典/修仙/武侠角色使用布靴、皂靴、云头履或绣鞋等中式鞋履；除非故事明确要求，禁止赤脚、现代高跟鞋、运动鞋、皮鞋、日式木屐及跨时代跨文化鞋款。
8. 小说原文、已有角色资料和创作方案中的年龄、性别、种族、身份及外貌是不可改写的事实。不得把儿童改成少年或成人，不得把狐女等种族改成普通人，不得自行添加江南、书香门第等来源中不存在的设定。缺少资料时宁可写“原文未明确”，禁止猜测。`

// GenerateProfile 使用 LLM 从故事中生成角色详细档案
func (s *CharacterProfileService) GenerateProfile(char *models.Character, project *models.Project, planJSON string) error {
	// 构建用户输入
	var user strings.Builder
	user.WriteString(fmt.Sprintf("角色名：%s\n", char.Name))
	if char.Role != "" {
		user.WriteString(fmt.Sprintf("角色定位：%s\n", char.Role))
	}
	if char.Appearance != "" {
		user.WriteString(fmt.Sprintf("已有外貌事实（不可改写）：%s\n", char.Appearance))
	}
	if char.Trait != "" {
		user.WriteString(fmt.Sprintf("已有角色特征（不可冲突）：%s\n", char.Trait))
	}
	if char.Style != "" {
		user.WriteString(fmt.Sprintf("已有服装事实（不可改写）：%s\n", char.Style))
	}
	if char.Background != "" {
		user.WriteString(fmt.Sprintf("已有背景事实（不可改写）：%s\n", char.Background))
	}
	if project.Genre != "" {
		user.WriteString(fmt.Sprintf("项目题材：%s\n", project.Genre))
	}
	if project.Style != "" {
		user.WriteString(fmt.Sprintf("项目画风：%s\n", project.Style))
	}

	if project.Synopsis != "" {
		user.WriteString(fmt.Sprintf("\n故事创意：\n%s\n", project.Synopsis))
	}
	if planJSON != "" {
		user.WriteString(fmt.Sprintf("\n创作方案：\n%s\n", planJSON))
	}

	// 调用 LLM
	var raw string
	var err error
	if s.skills != nil {
		raw, err = s.skills.ChatWithSkill(project.ID, models.SkillStageCharacter, s.textProvider, characterProfileSystemPrompt, user.String(), map[string]string{"character_info": user.String()})
	} else {
		raw, err = s.textProvider.Chat(characterProfileSystemPrompt, user.String())
	}
	if err != nil {
		return fmt.Errorf("生成角色档案失败: %w", err)
	}

	// 解析 JSON
	result, err := parseCharacterProfileJSON(raw)
	if err != nil {
		return fmt.Errorf("解析角色档案失败: %w", err)
	}
	if strings.TrimSpace(result.Name) != strings.TrimSpace(char.Name) {
		return fmt.Errorf("角色档案名称不匹配：期望「%s」，模型返回「%s」", char.Name, result.Name)
	}
	if err := validateProfileAgeAgainstSources(result.Appearance, char, planJSON); err != nil {
		return err
	}

	// 更新角色档案
	updates := map[string]any{
		"appearance":       result.Appearance,
		"personality":      result.Personality,
		"background":       result.Background,
		"relationships":    result.Relationships,
		"emotions":         result.Emotions,
		"habits":           result.Habits,
		"wardrobe_detail":  result.WardrobeDetail,
		"lighting_mood":    result.LightingMood,
		"color_palette":    result.ColorPalette,
		"profile_status":   models.ProfileStatusDraft,
		"profile_version":  gorm.Expr("profile_version + 1"),
		"reference_prompt": "",
		"review_note":      "",
		"portrait":         "",
		"portrait_task_id": "",
		"portrait_error":   "",
		"sheet":            "",
		"sheet_task_id":    "",
		"sheet_error":      "",
		"trait":            coalesceField(char.Trait, result.Appearance),
		"style":            coalesceField(char.Style, result.WardrobeDetail),
	}

	return s.db.Model(char).Updates(updates).Error
}

var characterAgePattern = regexp.MustCompile(`(?:年龄(?:为|约|：|:)?\s*)?(\d{1,2})\s*岁`)
var characterAgeRangePattern = regexp.MustCompile(`(\d{1,2})\s*(?:到|至|[-~～—])\s*(\d{1,2})\s*岁`)
var chineseCharacterAgeRangePattern = regexp.MustCompile(`([一二三四五六七八九十两]+)\s*(?:到|至|、|[-~～—])?\s*([一二三四五六七八九十两]+)?\s*岁`)

func chineseAgeNumber(text string) (int, bool) {
	values := map[rune]int{'一': 1, '二': 2, '两': 2, '三': 3, '四': 4, '五': 5, '六': 6, '七': 7, '八': 8, '九': 9}
	runes := []rune(text)
	if len(runes) == 1 {
		value, ok := values[runes[0]]
		return value, ok
	}
	if len(runes) == 2 && runes[0] == '十' {
		value, ok := values[runes[1]]
		return 10 + value, ok
	}
	if len(runes) == 2 && runes[1] == '十' {
		value, ok := values[runes[0]]
		return value * 10, ok
	}
	if len(runes) == 3 && runes[1] == '十' {
		tens, ok1 := values[runes[0]]
		ones, ok2 := values[runes[2]]
		return tens*10 + ones, ok1 && ok2
	}
	return 0, false
}

func explicitCharacterAgeRange(text string) (int, int, bool) {
	if match := characterAgeRangePattern.FindStringSubmatch(text); len(match) == 3 {
		minAge, err1 := strconv.Atoi(match[1])
		maxAge, err2 := strconv.Atoi(match[2])
		if err1 == nil && err2 == nil && minAge > 0 && maxAge >= minAge {
			return minAge, maxAge, true
		}
	}
	if match := characterAgePattern.FindStringSubmatch(text); len(match) >= 2 {
		age, err := strconv.Atoi(match[1])
		if err == nil && age > 0 {
			return age, age, true
		}
	}
	if match := chineseCharacterAgeRangePattern.FindStringSubmatch(text); len(match) >= 2 {
		first, second := match[1], ""
		if len(match) >= 3 {
			second = match[2]
		}
		firstRunes := []rune(first)
		if second == "" && len(firstRunes) == 2 && firstRunes[0] != '十' && firstRunes[1] != '十' {
			minAge, ok1 := chineseAgeNumber(string(firstRunes[0]))
			maxAge, ok2 := chineseAgeNumber(string(firstRunes[1]))
			if ok1 && ok2 && maxAge >= minAge {
				return minAge, maxAge, true
			}
		}
		minAge, ok := chineseAgeNumber(first)
		if ok && minAge > 0 {
			maxAge := minAge
			if second != "" {
				if parsed, valid := chineseAgeNumber(second); valid {
					maxAge = parsed
				}
			}
			if maxAge >= minAge {
				return minAge, maxAge, true
			}
		}
	}
	return 0, 0, false
}

func authoritativeCharacterAgeRange(char *models.Character, sourceContext string) (int, int, bool) {
	name := strings.TrimSpace(char.Name)
	if name == "" {
		return explicitCharacterAgeRange(strings.Join([]string{char.Appearance, char.Trait, char.Background}, "\n"))
	}
	contextRunes := []rune(sourceContext)
	nameRunes := []rune(name)
	for start := 0; start+len(nameRunes) <= len(contextRunes); start++ {
		if string(contextRunes[start:start+len(nameRunes)]) != name {
			continue
		}
		right := start + len(nameRunes) + 120
		if right > len(contextRunes) {
			right = len(contextRunes)
		}
		// 优先角色名之后的直接描述，避免把前一句其他角色的年龄误归给当前角色。
		if minAge, maxAge, ok := explicitCharacterAgeRange(string(contextRunes[start+len(nameRunes) : right])); ok {
			return minAge, maxAge, true
		}
		left := start - 80
		if left < 0 {
			left = 0
		}
		if minAge, maxAge, ok := explicitCharacterAgeRange(string(contextRunes[left:start])); ok {
			return minAge, maxAge, true
		}
	}
	return explicitCharacterAgeRange(strings.Join([]string{char.Appearance, char.Trait, char.Background}, "\n"))
}

func validateProfileAgeAgainstSources(appearance string, char *models.Character, sourceContext string) error {
	minAge, maxAge, hasSourceAge := authoritativeCharacterAgeRange(char, sourceContext)
	generatedMin, generatedMax, hasGeneratedAge := explicitCharacterAgeRange(appearance)
	if hasSourceAge && (!hasGeneratedAge || generatedMin < minAge || generatedMax > maxAge) {
		return fmt.Errorf("角色档案年龄与小说原文冲突：原文为%d到%d岁，生成外貌为「%s」", minAge, maxAge, appearance)
	}
	return nil
}

func characterAgeAnchor(appearance, trait string) string {
	match := characterAgePattern.FindStringSubmatch(appearance + "，" + trait)
	if len(match) < 2 {
		return ""
	}
	age, err := strconv.Atoi(match[1])
	if err != nil || age < 1 || age > 99 {
		return ""
	}
	anchor := fmt.Sprintf("%d岁人物", age)
	return anchor
}

func portraitFaceIdentity(appearance, trait string) string {
	text := strings.TrimSpace(appearance)
	if text == "" {
		text = strings.TrimSpace(trait)
	}
	// 不使用单字“发”，否则“洗得发白”等服装描述会被误判为头发身份特征。
	keywords := []string{"岁", "头发", "发型", "发丝", "发髻", "长发", "短发", "披发", "额头", "脸", "眉", "眼", "鼻", "唇", "肤", "耳", "下巴", "痣", "疤", "雀斑", "胡子", "胡须", "髭", "髯", "络腮胡", "山羊胡"}
	clauses := strings.FieldsFunc(text, func(r rune) bool { return r == '，' || r == '。' || r == '；' || r == '\n' })
	out := make([]string, 0, 12)
	for _, clause := range clauses {
		clause = strings.TrimSpace(clause)
		for _, keyword := range keywords {
			if strings.Contains(clause, keyword) {
				out = append(out, clause)
				break
			}
		}
		if len(out) >= 12 {
			break
		}
	}
	return strings.Join(out, "，")
}

// portraitStyling 只保留肩部头像中实际可见的一套上衣信息。
// 发型已由 portraitFaceIdentity 输出；鞋履、武器、身材、动机和经历不进入标准像提示词。
func truncatePortraitLowerBody(clause string) string {
	clause = strings.TrimSpace(clause)
	cut := len(clause)
	for _, marker := range []string{"腰部", "腰间", "腰下", "下摆", "及膝", "膝下", "下装", "长裤", "裤腿", "半身裙", "长裙", "鞋", "靴", "足踏"} {
		if i := strings.Index(clause, marker); i >= 0 && i < cut {
			cut = i
		}
	}
	return strings.TrimSpace(clause[:cut])
}

func sanitizePortraitHeadshotPrompt(prompt string) string {
	clauses := strings.FieldsFunc(prompt, func(r rune) bool { return r == '，' || r == '。' || r == '；' || r == '\n' })
	kept := make([]string, 0, len(clauses))
	for _, clause := range clauses {
		if upper := truncatePortraitLowerBody(clause); upper != "" {
			kept = append(kept, upper)
		}
	}
	return strings.Join(kept, "，")
}

func portraitStyling(char *models.Character) string {
	wardrobe := strings.TrimSpace(char.WardrobeDetail)
	if wardrobe == "" {
		wardrobe = strings.TrimSpace(char.Style)
	}
	if wardrobe == "" {
		return "简洁合体的中性基础上衣，衣料完整覆盖肩部与胸口"
	}
	for _, marker := range []string{"鹅黄色装扮：", "第二套", "另一套", "工作装束", "工作装：", "重要场合造型", "正式场合", "礼服造型"} {
		if i := strings.Index(wardrobe, marker); i > 0 {
			wardrobe = wardrobe[:i]
		}
	}
	for _, marker := range []string{"第一套：", "日常装扮：", "日常服装："} {
		if i := strings.Index(wardrobe, marker); i >= 0 {
			wardrobe = wardrobe[i+len(marker):]
			break
		}
	}
	clothingWords := "衣衫袍裙领襟袖肩胸布绸丝麻纹色带搭"
	excludedWords := []string{"岁", "男性", "女性", "身形", "面容", "眉", "眼", "鼻", "唇", "皮肤", "发髻", "头发", "发丝", "簪", "鞋", "靴", "足踏", "手持", "武器", "拂尘", "经历", "渴望", "机会", "修为", "性格"}
	kept := make([]string, 0, 5)
	seen := map[string]bool{}
	for _, clause := range strings.FieldsFunc(wardrobe, func(r rune) bool { return r == '，' || r == '。' || r == '；' || r == '\n' }) {
		clause = strings.TrimSpace(clause)
		// 标准像是肩部以上头像。档案常把上衣、腰带、下摆和鞋履写在同一句，
		// 必须在第一个下半身结构词之前截断，避免模型被“腰部/及膝/鞋底”等词诱导成全身照。
		clause = truncatePortraitLowerBody(clause)
		if clause == "" || !strings.ContainsAny(clause, clothingWords) {
			continue
		}
		excluded := false
		for _, word := range excludedWords {
			if strings.Contains(clause, word) {
				excluded = true
				break
			}
		}
		if excluded || seen[clause] {
			continue
		}
		seen[clause] = true
		kept = append(kept, clause)
		if len(kept) >= 5 {
			break
		}
	}
	if len(kept) == 0 {
		return "简洁合体的基础上衣，衣料完整覆盖肩部与胸口"
	}
	style := strings.Join(kept, "，")
	if runes := []rune(style); len(runes) > 180 {
		style = string(runes[:180])
	}
	return style
}

func ageSubject(appearance, trait string) string {
	match := characterAgePattern.FindStringSubmatch(appearance + "，" + trait)
	if len(match) < 2 {
		return ""
	}
	gender := "青年人物"
	text := appearance + trait
	if strings.Contains(text, "女性") || strings.Contains(text, "女子") || strings.Contains(text, "女孩") {
		gender = "青年女性"
	} else if strings.Contains(text, "男性") || strings.Contains(text, "男子") || strings.Contains(text, "男孩") {
		gender = "青年男性"
	}
	return match[1] + "岁" + gender
}

func portraitStoryIdentity(char *models.Character, project *models.Project) string {
	parts := []string{}
	genre, style, synopsis := "", "", ""
	if project != nil {
		genre = strings.TrimSpace(project.Genre)
		style = strings.TrimSpace(project.Style)
		synopsis = strings.TrimSpace(project.Synopsis)
	}
	characterContext := strings.Join([]string{char.Role, char.Appearance, char.Trait, char.Background, char.WardrobeDetail}, " ")
	world := strings.TrimSpace(genre + " " + synopsis + " " + characterContext)
	isCultivation := strings.Contains(world, "修仙") || strings.Contains(world, "仙侠") || strings.Contains(world, "宗门") || strings.Contains(world, "灵气") || strings.ContainsAny(world, "狐妖魔仙灵")
	isHistorical := strings.Contains(world, "武侠") || strings.Contains(world, "古代") || strings.Contains(world, "古装") || strings.Contains(world, "罗衣") || strings.Contains(world, "襦裙")
	if genre != "" {
		parts = append(parts, "题材："+genre)
	} else if isCultivation {
		parts = append(parts, "题材：古风仙侠、东方玄幻")
	} else if isHistorical {
		parts = append(parts, "题材：中国古典幻想")
	}
	if style != "" {
		parts = append(parts, "画风："+style)
	} else if isCultivation {
		parts = append(parts, "画风：古风仙侠人物设定、东方幻想美学")
	}
	if isCultivation {
		parts = append(parts, "中国古典修仙世界，禁止现代造型与现代服饰")
	} else if isHistorical {
		parts = append(parts, "中国古典时代人物，禁止现代造型")
	}
	role := strings.TrimSpace(char.Role)
	if role != "" && role != "主角" && role != "配角" && role != "男主" && role != "女主" {
		parts = append(parts, "身份："+role)
	}
	return strings.Join(parts, "，")
}

// GenerateReferencePrompt 生成脸部身份标准像；必须保留档案中的胡须、伤疤等身份特征，
// 并注入故事世界与角色身份。服装仍只选审核档案中的一套确定妆造。
func (s *CharacterProfileService) GenerateReferencePrompt(char *models.Character, project *models.Project) (string, error) {
	appearance := portraitFaceIdentity(char.Appearance, char.Trait)
	if appearance == "" {
		return "", fmt.Errorf("请先完善角色脸部与发型描述")
	}
	parts := []string{"单人正面大头贴"}
	// 题材与画风放在提示词最前部，避免被后续的长外貌和服装描述淹没。
	if identity := portraitStoryIdentity(char, project); identity != "" {
		parts = append(parts, identity)
	}
	parts = append(parts, appearance)
	parts = append(parts, "本次标准像唯一妆造："+portraitStyling(char))
	if project != nil {
		if desc := styleDescriptor(project.Style); desc != "" {
			parts = append(parts, desc)
		}
	}
	parts = append(parts, "头发、发型与头饰完整入镜，脸部居中，直视镜头，自然表情，肩部以上构图，本次唯一上衣的领口、颜色、材质清楚可见，衣料完整覆盖肩部与胸口，纯白背景，柔和均匀光线")
	prompt := normalizePortraitStyle(strings.Join(parts, "，"), project)
	updates := map[string]any{
		"reference_prompt": prompt,
		"portrait":         "",
		"portrait_task_id": "",
		"portrait_error":   "",
		"sheet":            "",
		"sheet_task_id":    "",
		"sheet_error":      "",
		"profile_version":  gorm.Expr("profile_version + 1"),
	}
	// 参考提示词是由现有档案确定性编译出的派生内容；档案本身未变化时，
	// 不应把刚审核通过的状态退回草稿，否则会形成“审核→生成提示词→再审核”的循环。
	if char.ProfileStatus != models.ProfileStatusApproved {
		updates["profile_status"] = models.ProfileStatusDraft
		updates["review_note"] = ""
	}
	if err := s.db.Model(char).Updates(updates).Error; err != nil {
		return "", err
	}
	return prompt, nil
}

// normalizePortraitStyle 删除与项目主画风直接冲突的泛化尾词，避免同一提示词同时要求动漫与写实。
func normalizePortraitStyle(prompt string, project *models.Project) string {
	if project == nil {
		return prompt
	}
	style := strings.ToLower(strings.TrimSpace(project.Style))
	if strings.Contains(style, "国漫") || strings.Contains(style, "日漫") || strings.Contains(style, "韩漫") || strings.Contains(style, "动画") || strings.Contains(style, "插画") {
		for _, conflicting := range []string{"，写实风格", "，真人写实", "，真实照片风格", "，超写实"} {
			prompt = strings.ReplaceAll(prompt, conflicting, "")
		}
	}
	return prompt
}

func validateCharacterProfile(char *models.Character) error {
	missing := make([]string, 0, 10)
	for label, value := range map[string]string{
		"角色名": char.Name, "外貌描述": char.Appearance, "性格特点": char.Personality,
		"背景故事": char.Background, "关系图谱": char.Relationships, "情绪表达": char.Emotions,
		"习惯动作": char.Habits, "服装细节": char.WardrobeDetail, "光影氛围": char.LightingMood, "角色色调": char.ColorPalette,
	} {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, label)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("角色档案不完整，请补充：%s", strings.Join(missing, "、"))
	}
	if strings.TrimSpace(char.ReferencePrompt) == "" {
		return fmt.Errorf("请先生成或填写参考像提示词")
	}
	return nil
}

// ApproveProfile 审核通过角色档案
func (s *CharacterProfileService) ApproveProfile(char *models.Character, note string) error {
	if err := validateCharacterProfile(char); err != nil {
		return err
	}
	updates := map[string]any{
		"profile_status": models.ProfileStatusApproved,
		"review_note":    note,
	}
	return s.db.Model(char).Updates(updates).Error
}

// RejectProfile 驳回角色档案
func (s *CharacterProfileService) RejectProfile(char *models.Character, reason string) error {
	updates := map[string]any{
		"profile_status": models.ProfileStatusRejected,
		"review_note":    reason,
	}
	return s.db.Model(char).Updates(updates).Error
}

// ResetToDraft 将角色档案重置为草稿状态
func (s *CharacterProfileService) ResetToDraft(char *models.Character) error {
	updates := map[string]any{
		"profile_status": models.ProfileStatusDraft,
		"review_note":    "",
	}
	return s.db.Model(char).Updates(updates).Error
}

// UpdateProfile 手动更新角色档案字段
func (s *CharacterProfileService) UpdateProfile(char *models.Character, req models.Character) error {
	// PUT 表示完整替换审核表单，允许用户显式清空错误字段。
	updates := map[string]any{
		"appearance": strings.TrimSpace(req.Appearance), "personality": strings.TrimSpace(req.Personality),
		"background": strings.TrimSpace(req.Background), "relationships": strings.TrimSpace(req.Relationships),
		"emotions": strings.TrimSpace(req.Emotions), "habits": strings.TrimSpace(req.Habits),
		"wardrobe_detail": strings.TrimSpace(req.WardrobeDetail), "lighting_mood": strings.TrimSpace(req.LightingMood),
		"color_palette": strings.TrimSpace(req.ColorPalette), "reference_prompt": strings.TrimSpace(req.ReferencePrompt),
	}

	updates["profile_version"] = gorm.Expr("profile_version + 1")
	// 任意档案修改都需重新审核，且清除旧审核意见。
	updates["profile_status"] = models.ProfileStatusDraft
	updates["review_note"] = ""
	updates["portrait"] = ""
	updates["portrait_task_id"] = ""
	updates["portrait_error"] = ""
	updates["sheet"] = ""
	updates["sheet_task_id"] = ""
	updates["sheet_error"] = ""

	return s.db.Model(char).Updates(updates).Error
}

// GetProfileFields 获取角色档案的所有扩展字段（用于审核界面）
func (s *CharacterProfileService) GetProfileFields(char *models.Character) map[string]string {
	return map[string]string{
		"name":             char.Name,
		"role":             char.Role,
		"appearance":       char.Appearance,
		"personality":      char.Personality,
		"background":       char.Background,
		"relationships":    char.Relationships,
		"emotions":         char.Emotions,
		"habits":           char.Habits,
		"wardrobe_detail":  char.WardrobeDetail,
		"lighting_mood":    char.LightingMood,
		"color_palette":    char.ColorPalette,
		"reference_prompt": char.ReferencePrompt,
		"trait":            char.Trait,
		"style":            char.Style,
	}
}

// --- 内部辅助函数 ---

var characterProfileStringFields = []string{
	"name", "role", "appearance", "personality", "background", "relationships", "emotions", "habits",
	"wardrobe_detail", "lighting_mood", "color_palette",
}

// profileValueText 将模型偶尔返回的数组/对象稳定压平成可编辑文本，避免一个字段类型漂移
// 导致整份角色档案丢失。对象键排序保证结果可复现。
func profileValueText(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := profileValueText(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "；")
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			if text := profileValueText(typed[key]); text != "" {
				parts = append(parts, key+"："+text)
			}
		}
		return strings.Join(parts, "；")
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func repairCharacterProfileKeySeparators(jsonStr string) string {
	for _, field := range characterProfileStringFields {
		// 仅修复固定 schema 字段在键后误写逗号的已知模型漂移："relationships", {...}
		pattern := regexp.MustCompile(`("` + regexp.QuoteMeta(field) + `"\s*),\s*(["{\[])`)
		jsonStr = pattern.ReplaceAllString(jsonStr, `${1}:$2`)
	}
	return jsonStr
}

func normalizeCharacterProfileScalarStrings(jsonStr string) (string, error) {
	jsonStr = repairCharacterProfileKeySeparators(jsonStr)
	var object map[string]any
	if err := json.Unmarshal([]byte(jsonStr), &object); err != nil {
		return "", err
	}
	for _, field := range characterProfileStringFields {
		if value, ok := object[field]; ok {
			object[field] = profileValueText(value)
		}
	}
	normalized, err := json.Marshal(object)
	if err != nil {
		return "", err
	}
	return string(normalized), nil
}

// parseCharacterProfileJSON 解析 LLM 返回的角色档案 JSON
func parseCharacterProfileJSON(raw string) (*characterProfileResult, error) {
	// 尝试提取 JSON（处理 markdown 代码块包裹）
	jsonStr := extractJSON(raw)
	normalized, err := normalizeCharacterProfileScalarStrings(jsonStr)
	if err != nil {
		return nil, fmt.Errorf("JSON 解析失败: %w", err)
	}

	var result characterProfileResult
	if err := json.Unmarshal([]byte(normalized), &result); err != nil {
		return nil, fmt.Errorf("JSON 解析失败: %w", err)
	}

	// 档案必须足以支撑人物一致性与后续参考像生成。
	if strings.TrimSpace(result.Name) == "" || strings.TrimSpace(result.Appearance) == "" || strings.TrimSpace(result.Personality) == "" ||
		strings.TrimSpace(result.Background) == "" || strings.TrimSpace(result.Relationships) == "" ||
		strings.TrimSpace(result.Emotions) == "" || strings.TrimSpace(result.Habits) == "" ||
		strings.TrimSpace(result.WardrobeDetail) == "" || strings.TrimSpace(result.LightingMood) == "" ||
		strings.TrimSpace(result.ColorPalette) == "" {
		return nil, fmt.Errorf("角色档案字段不完整")
	}

	return &result, nil
}

// extractJSON 从可能包含 markdown 代码块的文本中提取 JSON
func extractJSON(raw string) string {
	raw = strings.TrimSpace(raw)
	start, end := strings.Index(raw, "{"), strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return raw
	}
	return strings.TrimSpace(raw[start : end+1])
}

// coalesceField 如果目标字段为空则使用备选值
func coalesceField(primary, fallback string) string {
	if strings.TrimSpace(primary) != "" {
		return primary
	}
	return fallback
}
