package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
)

const sceneContentExpandSkillCode = "scene_content_expand"

type sceneContentExpandResult struct {
	Content      string `json:"content"`
	DirectorNote string `json:"director_note"`
}

// HandleExpandSceneContent creates an editable preview only. It deliberately does not persist
// the scene or touch Shots/media; applying the preview still goes through HandleUpdateScene.
func (s *Service) HandleExpandSceneContent(c *gin.Context) {
	scene, ok := s.loadScene(c)
	if !ok {
		return
	}
	var req struct {
		Content      string `json:"content"`
		Requirements string `json:"requirements"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Content) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "当前场景正文不能为空"})
		return
	}
	if s.Skills == nil || s.TextProviderFact == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "AI 文本服务未配置"})
		return
	}

	draftScene := *scene
	draftScene.Content = strings.TrimSpace(req.Content)
	context, err := s.sceneContentExpandContext(&draftScene)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	const system = `你是影视导演和场面调度师。把用户当前未保存的场景正文扩写成中文、可拍摄的场景剧本过程，而不是视频提示词翻译。只输出合法JSON：{"content":"...","director_note":"..."}。
必须忠于已有事实与前后场：把概括词落实为可见的表演因果和连贯动作，例如“虚弱”落实为重心、步幅、短暂停顿与继续行动的选择，而不是增加形容词。围绕角色当前目的、已有阻力、即时反应和结束姿态组织节奏，避免把所有句子写成同速流水账；仅在有助于叙事时提出景别和视线落点。扩展表现过程，不扩展剧情事件；不得凭空增加追兵、障碍、受伤、能力或胜负，不得臆造服装/参考图细节。必须尊重当前单镜时长，不在短镜头塞入一长串动作。dialogues中有明确对白时逐字保留并自然嵌入；当前正文中明确说出的台词也必须保留，不能因为结构化列表缺失就删除。没有明确对白时禁止新增台词。不要输出H3字段、英文视频提示词或Shot JSON。时长容不下时在director_note建议后续拆镜，不改变已有剧情结果。director_note保持简短。`
	user := fmt.Sprintf("【当前未保存正文】\n%s\n\n【创作要求】\n%s\n\n【同集同版本、角色资产与明确对白上下文】\n%s", strings.TrimSpace(req.Content), strings.TrimSpace(req.Requirements), context)
	output, err := s.Skills.ChatWithConfiguredOrFallbackSkill(scene.ProjectID, models.SkillStageStoryboard, sceneContentExpandSkillCode, s.TextProviderFact, system, user, map[string]string{
		"scene_id": fmt.Sprint(scene.ID), "scene_content": strings.TrimSpace(req.Content), "requirements": strings.TrimSpace(req.Requirements), "context": context,
	})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "AI 导演扩写失败: " + err.Error()})
		return
	}
	result, err := parseSceneContentExpandResult(output)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "AI 返回格式无效: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"content": result.Content, "director_note": result.DirectorNote, "skill_code": sceneContentExpandSkillCode, "provider_id": s.TextProviderFact.Name(), "persisted": false})
}

func (s *Service) sceneContentExpandContext(scene *models.Scene) (string, error) {
	var adjacent []models.Scene
	if err := s.DB.Where("project_id = ? AND episode_n = ? AND generation = ? AND id <> ? AND `order` BETWEEN ? AND ?", scene.ProjectID, scene.EpisodeN, scene.Generation, scene.ID, scene.Order-1, scene.Order+1).Order("`order`").Find(&adjacent).Error; err != nil {
		return "", err
	}
	var characters []models.Character
	if err := s.DB.Where("project_id = ?", scene.ProjectID).Order("id").Find(&characters).Error; err != nil {
		return "", err
	}
	var assets []models.Asset
	if err := s.DB.Where("project_id = ?", scene.ProjectID).Order("kind, id").Find(&assets).Error; err != nil {
		return "", err
	}
	var dialogues []models.Dialogue
	if err := s.DB.Where("project_id = ? AND scene_id = ?", scene.ProjectID, scene.ID).Order("`order`").Find(&dialogues).Error; err != nil {
		return "", err
	}
	dialogues = mergeExplicitSceneSpeech(scene, dialogues)
	var b strings.Builder
	fmt.Fprintf(&b, "当前场景：%s；时长：%.1f秒；文武类型：%s；可见人物：%s；发声人物：%s；地点：%s；道具：%s\n", scene.Title, scene.Duration, scene.SceneMode, visibleSceneCharacterNames(scene), scene.VoiceCharacters, scene.LocationName, scene.Props)
	for _, sc := range adjacent {
		fmt.Fprintf(&b, "相邻场景[%d] %s：%s\n", sc.Order, sc.Title, sc.Content)
	}
	fmt.Fprintln(&b, "当前角色（仅可使用已给事实）：")
	for _, ch := range characters {
		fmt.Fprintf(&b, "- %s（%s）；特征：%s；行为/情绪：%s %s\n", ch.Name, ch.Role, ch.Trait, ch.Emotions, ch.Habits)
	}
	fmt.Fprintln(&b, "当前资产（不得据此臆造未写明细节）：")
	for _, a := range assets {
		fmt.Fprintf(&b, "- %s/%s：%s\n", a.Kind, a.Name, a.Description)
	}
	fmt.Fprintln(&b, "dialogues（必须原字保留；列表为空则不得加台词）：")
	for _, d := range dialogues {
		fmt.Fprintf(&b, "- [%s|%s] %s\n", d.SpeechType, d.Character, d.Text)
	}
	return b.String(), nil
}

func parseSceneContentExpandResult(raw string) (sceneContentExpandResult, error) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "```") {
		raw = strings.TrimPrefix(raw, "```json")
		raw = strings.TrimPrefix(raw, "```")
		raw = strings.TrimSuffix(strings.TrimSpace(raw), "```")
	}
	var result sceneContentExpandResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return result, err
	}
	result.Content, result.DirectorNote = strings.TrimSpace(result.Content), strings.TrimSpace(result.DirectorNote)
	if result.Content == "" {
		return result, fmt.Errorf("content 为空")
	}
	return result, nil
}
