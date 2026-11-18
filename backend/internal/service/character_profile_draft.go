package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
)

var characterDraftFields = []string{"gender", "appearance", "personality", "background", "relationships", "emotions", "habits", "wardrobe_detail", "lighting_mood", "color_palette"}

type CharacterProfileDraftRequest struct {
	Current        map[string]string `json:"current"`
	Fields         []string          `json:"fields"`
	Instruction    string            `json:"instruction"`
	Selection      string            `json:"selection"`
	SelectionStart *int              `json:"selection_start"`
}

// PreviewProfile returns an ephemeral patch, never changing approval, prompts or images.
func (s *CharacterProfileService) PreviewProfile(ch *models.Character, p *models.Project, req CharacterProfileDraftRequest) (map[string]string, error) {
	if s.textProvider == nil {
		return nil, fmt.Errorf("未配置文本模型")
	}
	if len(req.Fields) == 0 || strings.TrimSpace(req.Instruction) == "" {
		return nil, fmt.Errorf("请选择字段并填写方向")
	}
	allowed := map[string]bool{}
	for _, key := range characterDraftFields {
		allowed[key] = true
	}
	selected := map[string]bool{}
	for _, key := range req.Fields {
		if !allowed[key] || selected[key] {
			return nil, fmt.Errorf("无效或重复的档案字段：%s", key)
		}
		selected[key] = true
	}
	start, end := 0, 0
	var err error
	if req.Selection != "" {
		if len(req.Fields) != 1 {
			return nil, fmt.Errorf("选区修改只能选择一个字段")
		}
		start, end, err = designSelection(req.Current[req.Fields[0]], req.Selection, req.SelectionStart)
		if err != nil {
			return nil, err
		}
	}
	current, _ := json.Marshal(req.Current)
	fields, _ := json.Marshal(req.Fields)
	user := fmt.Sprintf("角色：%s\n身份：%s\n既有外貌：%s\n既有服装：%s\n项目题材：%s\n故事：%s\n当前表单：%s\n仅允许修改字段：%s\n用户方向：%s\n选中片段：%s", ch.Name, ch.Role, ch.Appearance, ch.Style, p.Genre, p.Synopsis, current, fields, req.Instruction, req.Selection)
	system := characterProfileSystemPrompt + "\n本次为档案局部草稿。以上是领域规则，不是本次输出格式。只输出被选字段的 JSON 对象，例如 {\"appearance\":\"新外貌\"}。不得输出其他字段，不得改变姓名、年龄、性别等权威事实；用户明确要求修改的部分除外。新增且无来源的设定必须注明‘AI建议，待确认’，不要臆测参考图细节。"
	user += fmt.Sprintf("\n既有性别：%s\n既有特征：%s\n既有背景：%s\n创作方案：%s", ch.Gender, ch.Trait, ch.Background, p.Plan)
	if authoritativeAdultFemale(ch) {
		system += "\n" + femaleCharacterBeautyDesignContract
	}
	if req.Selection != "" {
		system += "\n本次只输出 {\"replacement\":\"替换选中片段的文字\"}，不要返回完整字段。"
	}
	raw, err := s.textProvider.Chat(system, user)
	if err != nil {
		return nil, fmt.Errorf("档案草稿生成失败: %w", err)
	}
	var patch map[string]string
	if json.Unmarshal([]byte(extractJSON(stripThinkBlocks(raw))), &patch) != nil {
		return nil, fmt.Errorf("AI 未返回有效字段草稿，原档案未改变")
	}
	if req.Selection != "" {
		replacement, ok := patch["replacement"]
		if len(patch) != 1 || !ok || strings.TrimSpace(replacement) == "" {
			return nil, fmt.Errorf("AI 未返回有效选区替换，原档案未改变")
		}
		key := req.Fields[0]
		runes := []rune(req.Current[key])
		return map[string]string{key: string(runes[:start]) + replacement + string(runes[end:])}, nil
	}
	if len(patch) != len(selected) {
		return nil, fmt.Errorf("AI 返回的字段不完整或超出选择，原档案未改变")
	}
	for key, value := range patch {
		if !selected[key] || strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("AI 返回无效字段，原档案未改变")
		}
	}
	return patch, nil
}

func (s *Service) HandlePreviewCharacterProfile(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	ch, ok := s.loadCharacter(c)
	if !ok {
		return
	}
	if ch.ProjectID != p.ID {
		c.JSON(404, gin.H{"error": "角色不属于当前项目"})
		return
	}
	var req CharacterProfileDraftRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "无效请求"})
		return
	}
	patch, err := s.CharacterProfiles.PreviewProfile(ch, p, req)
	if err != nil {
		c.JSON(422, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"patch": patch})
}
