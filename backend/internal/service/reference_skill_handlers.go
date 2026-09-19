package service

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
)

func (s *Service) sceneAssetBible(scene *models.Scene) (string, error) {
	var lines []string
	names := splitCharacterNames(scene.VisibleCharacters, scene.Characters)
	if len(names) > 0 {
		var characters []models.Character
		if err := s.DB.Where("project_id = ? AND name IN ?", scene.ProjectID, names).Order("name, id").Find(&characters).Error; err != nil {
			return "", err
		}
		for _, character := range characters {
			detail := strings.TrimSpace(strings.Join([]string{character.Trait, character.Appearance, character.Style, character.WardrobeDetail}, "；"))
			lines = append(lines, fmt.Sprintf("角色 %s：%s", character.Name, detail))
		}
	}
	assetNames := splitCharacterNames(scene.LocationName, scene.Props)
	if len(assetNames) > 0 {
		var assets []models.Asset
		if err := s.DB.Where("project_id = ? AND name IN ?", scene.ProjectID, assetNames).Order("kind, name, id").Find(&assets).Error; err != nil {
			return "", err
		}
		for _, asset := range assets {
			lines = append(lines, fmt.Sprintf("%s %s：%s", asset.Kind, asset.Name, strings.TrimSpace(asset.Description)))
		}
	}
	if len(lines) == 0 {
		lines = append(lines, fmt.Sprintf("可见角色：%s", strings.Join(names, "、")), "地点："+scene.LocationName, "道具："+scene.Props)
	}
	return strings.Join(lines, "\n"), nil
}

func (s *Service) previousSceneEndState(scene *models.Scene) string {
	var previous models.Scene
	if err := s.DB.Where("project_id = ? AND generation = ? AND episode_n = ? AND `order` < ?", scene.ProjectID, scene.Generation, scene.EpisodeN, scene.Order).
		Order("`order` DESC, id DESC").First(&previous).Error; err != nil {
		return "无（本场景为当前序列首场）"
	}
	if value := strings.TrimSpace(previous.VideoPrompt); value != "" {
		return value
	}
	return strings.TrimSpace(previous.Content)
}

func (s *Service) HandleAssetContinuityReviewDraft(c *gin.Context) {
	scene, ok := s.loadScene(c)
	if !ok {
		return
	}
	var req struct {
		PreviousEndState string `json:"previous_end_state"`
	}
	_ = c.ShouldBindJSON(&req)
	assetBible, err := s.sceneAssetBible(scene)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if strings.TrimSpace(req.PreviousEndState) == "" {
		req.PreviousEndState = s.previousSceneEndState(scene)
	}
	s.runExplicitSkill(c, scene.ProjectID, models.SkillStageStoryboard, "reference-asset-continuity", "只返回连续性审查草稿，不修改或保存任何场景、镜头或资产。", "核对当前场景使用的权威资产并列出冲突，供用户审核。", map[string]string{
		"asset_bible": assetBible, "scene_content": scene.Content, "previous_end_state": req.PreviousEndState,
	})
}

func (s *Service) HandleShotStatePromptDraft(c *gin.Context) {
	project, ok := s.loadProject(c)
	if !ok {
		return
	}
	if !s.textProviderConfigured() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "当前文生文 provider 未配置"})
		return
	}
	shotID, err := strconv.ParseUint(c.Param("shid"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid shot id"})
		return
	}
	var shot models.Shot
	if err := s.DB.Joins("JOIN scenes ON scenes.id = shots.scene_id").Where("shots.id = ? AND scenes.project_id = ?", shotID, project.ID).First(&shot).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "shot not found"})
		return
	}
	var req struct {
		StartState string `json:"start_state"`
		EndState   string `json:"end_state"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	baseSystem := "只输出合法JSON对象，不要Markdown或解释。字段必须且只能包含subject、action、camera、lighting、style，所有值均为非空字符串。不要保存或执行结果。"
	output, err := s.Skills.ChatWithConfiguredOrFallbackSkill(project.ID, models.SkillStageVideoPrompt, "reference-shot-state-prompt", s.TextProviderFact, baseSystem, "生成供用户审核的单镜起止状态提示词草稿。", map[string]string{
		"scene_content": shot.Description, "start_state": req.StartState, "end_state": req.EndState, "duration": fmt.Sprintf("%.1f", shot.Duration),
	})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	draft, err := parseDirectorShotPacket(output)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "单镜状态Skill返回的五段JSON无效: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"draft": draft, "skill_code": "reference-shot-state-prompt", "provider_id": s.TextProviderFact.Name(), "audited": true})
}

func orderedStoryboard(shots []models.Shot) string {
	var lines []string
	for _, shot := range shots {
		line := fmt.Sprintf("镜头%d｜%s｜%s｜%s｜%.1f秒", shot.Order, shot.ShotType, shot.Description, shot.Dialogue, shot.Duration)
		if note := strings.TrimSpace(shot.TransitionNote); note != "" {
			line += "｜转场：" + note
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func (s *Service) HandleCoverageReviewDraft(c *gin.Context) {
	scene, ok := s.loadScene(c)
	if !ok {
		return
	}
	var shots []models.Shot
	if err := s.DB.Where("scene_id = ?", scene.ID).Order("order_num, id").Find(&shots).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if len(shots) == 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "当前场景还没有可审查的镜头"})
		return
	}
	s.runExplicitSkill(c, scene.ProjectID, models.SkillStageReview, "reference-coverage-review", "只返回覆盖度审查草稿，不新增、修改或保存镜头。必须按输入镜头顺序审查。", "检查当前场景的有序镜头覆盖，结果供用户审核。", map[string]string{
		"story_goal": scene.Content, "storyboard": orderedStoryboard(shots),
	})
}
