package service

import (
	"encoding/json"
	"fmt"
	"regexp"
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
7. wardrobe_detail 必须明确写出鞋履。鞋履严格符合故事时代、地域文化、身份和服装：中国古典/修仙/武侠角色使用布靴、皂靴、云头履或绣鞋等中式鞋履；除非故事明确要求，禁止赤脚、现代高跟鞋、运动鞋、皮鞋、日式木屐及跨时代跨文化鞋款。`

// GenerateProfile 使用 LLM 从故事中生成角色详细档案
func (s *CharacterProfileService) GenerateProfile(char *models.Character, project *models.Project, planJSON string) error {
	// 构建用户输入
	var user strings.Builder
	user.WriteString(fmt.Sprintf("角色名：%s\n", char.Name))
	if char.Role != "" {
		user.WriteString(fmt.Sprintf("角色定位：%s\n", char.Role))
	}
	if char.Trait != "" {
		user.WriteString(fmt.Sprintf("已知外貌特征：%s\n", char.Trait))
	}
	if char.Style != "" {
		user.WriteString(fmt.Sprintf("已知服装风格：%s\n", char.Style))
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
	keywords := []string{"岁", "发", "额头", "脸", "眉", "眼", "鼻", "唇", "肤", "耳", "下巴", "痣", "疤", "雀斑"}
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

// GenerateReferencePrompt 只生成脸部身份用的大头贴提示词；服装、鞋履和配饰由 CharacterLook 单独生成。
func (s *CharacterProfileService) GenerateReferencePrompt(char *models.Character, project *models.Project) (string, error) {
	appearance := portraitFaceIdentity(char.Appearance, char.Trait)
	if appearance == "" {
		return "", fmt.Errorf("请先完善角色脸部与发型描述")
	}
	parts := []string{"单人正面大头贴", appearance}
	if project != nil {
		if desc := styleDescriptor(project.Style); desc != "" {
			parts = append(parts, desc)
		}
	}
	parts = append(parts, "头发完整入镜，脸部居中，直视镜头，自然表情，肩部以上构图，纯白背景，柔和均匀光线")
	prompt := strings.Join(parts, "，")
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

func validateCharacterProfile(char *models.Character) error {
	missing := make([]string, 0, 10)
	for label, value := range map[string]string{
		"角色名": char.Name, "身份": char.Role, "外貌描述": char.Appearance, "性格特点": char.Personality,
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

// parseCharacterProfileJSON 解析 LLM 返回的角色档案 JSON
func parseCharacterProfileJSON(raw string) (*characterProfileResult, error) {
	// 尝试提取 JSON（处理 markdown 代码块包裹）
	jsonStr := extractJSON(raw)

	var result characterProfileResult
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil, fmt.Errorf("JSON 解析失败: %w", err)
	}

	// 档案必须足以支撑人物一致性与后续参考像生成。
	if strings.TrimSpace(result.Name) == "" || strings.TrimSpace(result.Role) == "" ||
		strings.TrimSpace(result.Appearance) == "" || strings.TrimSpace(result.Personality) == "" ||
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
