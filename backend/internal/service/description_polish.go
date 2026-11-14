package service

import (
	"fmt"
	"net/http"
	"strings"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
)

const (
	assetSceneDescriptionPolishCode     = "asset-scene-description-polish"
	assetCharacterDescriptionPolishCode = "asset-character-description-polish"
)

// Description polish wording is user-approved; raise Version whenever a prompt changes so
// InitSystemSkills publishes the new revision.
var assetSceneDescriptionPolishSkill = models.Skill{
	Name: "场景描述润色", Code: assetSceneDescriptionPolishCode, Operation: assetSceneDescriptionPolishCode, Version: 1,
	Description:    "保留地点、时代、内外景、时段、用途和出入口，补充层次、材质、光线与氛围证据；必须无人",
	Stage:          models.SkillStageImagePrompt,
	PromptTemplate: "场景名：{{name}}\n原描述：{{description}}",
	SystemPrompt:   "你是美术设定编辑。把用户给的场景描述润色成可直接用于生成场景图的中文描述。原描述里的地点、时代、内景或外景、时段、场所用途和出入口位置必须保留，不得改变或删除。在此基础上补充：前景、中景、远景各有什么；主要材质；主光和辅光的方向与色温；能直接看见的氛围证据（如烟、水汽、积灰、落叶）。画面中不得出现任何人物或人影，不新增原文没有的剧情事件或重要道具。字数120到260字。只输出润色后的描述，不要标题、解释或思考过程。",
	IsSystem:       true, Enabled: true, SortOrder: 40,
}

var assetCharacterDescriptionPolishSkill = models.Skill{
	Name: "角色描述润色", Code: assetCharacterDescriptionPolishCode, Operation: assetCharacterDescriptionPolishCode, Version: 1,
	Description:    "保留姓名、性别、年龄段、身份和服装类型，只增强视觉细节",
	Stage:          models.SkillStageCharacter,
	PromptTemplate: "角色名：{{name}}\n原描述：{{description}}",
	SystemPrompt:   "你是角色设定编辑。把用户给的人物描述润色成可直接用于生成人物设定图的中文描述。姓名、性别、年龄段、身份和服装类型必须保留原样，不得改变。只增强视觉细节：脸型与五官特征、发型发色、体态，服装的面料、颜色、纹样和配饰，以及从外表能看出的气质。不写性格分析、背景故事或剧情。只输出润色后的描述，不要标题、解释或思考过程。",
	IsSystem:       true, Enabled: true, SortOrder: 40,
}

// PolishDescription returns a polished draft only; nothing is saved until the user confirms the
// before/after comparison and saves the form.
func (s *ProjectService) PolishDescription(projectID uint, target, name, description string) (string, error) {
	name, description = strings.TrimSpace(name), strings.TrimSpace(description)
	if name == "" || description == "" {
		return "", fmt.Errorf("名称和原描述都不能为空")
	}
	if s.textProvider == nil || s.skills == nil {
		return "", fmt.Errorf("文本生成服务未配置")
	}
	skill := assetSceneDescriptionPolishSkill
	if target == "character" {
		skill = assetCharacterDescriptionPolishSkill
	} else if target != "scene" {
		return "", fmt.Errorf("不支持的润色对象")
	}
	out, err := s.skills.ChatWithConfiguredOrFallbackSkill(projectID, skill.Stage, skill.Code, s.textProvider, "", "", map[string]string{"name": name, "description": description})
	if err != nil {
		return "", fmt.Errorf("AI 润色失败: %w", err)
	}
	out = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(out), "```"), "```"))
	if out == "" {
		return "", fmt.Errorf("AI 润色返回了空描述")
	}
	return out, nil
}

func (s *Service) HandlePolishDescription(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	var req struct {
		Target      string `json:"target"`
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	polished, err := s.Projects.PolishDescription(p.ID, req.Target, req.Name, req.Description)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"before": strings.TrimSpace(req.Description), "after": polished})
}
