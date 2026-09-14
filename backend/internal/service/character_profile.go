package service

import (
	"encoding/json"
	"fmt"
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
  "wardrobe_detail": "服装细节：日常服装描述（材质/颜色/款式）、重要场合造型、随时间变化的造型演变、重要配饰",
  "lighting_mood": "光影氛围：适合该角色的打光风格（柔和/硬朗/戏剧性）、主光源方向、氛围偏好（暖色调/冷色调）",
  "color_palette": "角色色调：主色调、辅色调、点缀色，以及与角色性格的关联"
}
3. 输出的内容应符合漫剧风格，适合后续 AI 生图和视频生成。
4. 每个字段都要有实质性内容，不要为空。
5. appearance 和 wardrobe_detail 要足够详细，能支撑高质量的参考像生成。`

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
		"trait":            coalesceField(char.Trait, result.Appearance),
		"style":            coalesceField(char.Style, result.WardrobeDetail),
	}

	return s.db.Model(char).Updates(updates).Error
}

// GenerateReferencePrompt 按审核后的结构化档案编译稳定的单人参考像提示词。
// 这里采用 LumxAI 的人物维度与设定图规范，但保持单人单视图，避免后续参考图模型误判为多人。
func (s *CharacterProfileService) GenerateReferencePrompt(char *models.Character, project *models.Project) (string, error) {
	appearance := strings.TrimSpace(char.Appearance)
	if appearance == "" {
		appearance = strings.TrimSpace(char.Trait)
	}
	wardrobe := strings.TrimSpace(char.WardrobeDetail)
	if wardrobe == "" {
		wardrobe = strings.TrimSpace(char.Style)
	}
	if appearance == "" || wardrobe == "" {
		return "", fmt.Errorf("请先完善角色外貌与服装档案")
	}
	parts := []string{"人物角色标准参考像", "单一角色「" + char.Name + "」"}
	if char.Role != "" {
		parts = append(parts, "身份气质："+char.Role)
	}
	parts = append(parts, "外貌必须精确一致："+appearance, "服饰款式、配色、配饰与材质必须精确一致："+wardrobe)
	if char.ColorPalette != "" {
		parts = append(parts, "角色固定配色："+char.ColorPalette)
	}
	if project != nil {
		if desc := styleDescriptor(project.Style); desc != "" {
			parts = append(parts, desc)
		}
	}
	if char.LightingMood != "" {
		parts = append(parts, "影棚布光："+char.LightingMood)
	}
	parts = append(parts, "正面半身头像，直视镜头，中性自然表情，肩颈端正，纯白干净背景，影棚级柔和布光，居中对称构图，高分辨率，电影级质感", "禁止多人、分屏、拼图、三视图、复杂背景、文字、水印、畸形五官和畸形肢体")
	prompt := strings.Join(parts, "，")
	if err := s.db.Model(char).Updates(map[string]any{
		"reference_prompt": prompt,
		"profile_status":   models.ProfileStatusDraft,
		"review_note":      "",
		"portrait":         "",
		"portrait_task_id": "",
		"portrait_error":   "",
		"profile_version":  gorm.Expr("profile_version + 1"),
	}).Error; err != nil {
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
