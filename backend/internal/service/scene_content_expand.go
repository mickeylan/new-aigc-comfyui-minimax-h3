package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
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
必须忠于已有事实与前后场。连续性优先级固定为：前一Scene最后Shot的end_state及其主体位置、朝向、手持道具、视线、动作余势 > 前一Scene正文与Dialogue > 当前未保存正文与当前Dialogue > 后一Scene边界。当前正文仍是本镜剧情权威；前镜生产状态只约束本镜如何开始，不能改写当前剧情。已确认上一镜尾帧只约束0.00秒可见状态，不得从图片文件名或参考图猜剧情。后一Scene只能防止提前演出后续事件，绝不能倒灌成当前动作。
把概括词落实为可见的表演因果和连贯动作，例如“虚弱”落实为重心、步幅、短暂停顿与继续行动的选择，而不是增加形容词。围绕角色当前目的、已有阻力、即时反应和结束姿态组织节奏，避免把所有句子写成同速流水账；仅在有助于叙事时提出景别和视线落点。扩展表现过程，不扩展剧情事件；不得凭空增加追兵、障碍、受伤、能力或胜负，不得臆造服装/参考图细节。必须尊重当前单镜时长，不在短镜头塞入一长串动作。dialogues中有明确对白时逐字保留并自然嵌入；当前正文中明确说出的台词也必须保留，不能因为结构化列表缺失就删除。没有明确对白时禁止新增台词。不要输出H3字段、英文视频提示词或Shot JSON。时长容不下时在director_note建议后续拆镜，不改变已有剧情结果。director_note保持简短。`
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
	var previous, next models.Scene
	previousErr := s.DB.Where("project_id = ? AND episode_n = ? AND generation = ? AND `order` = ?", scene.ProjectID, scene.EpisodeN, scene.Generation, scene.Order-1).First(&previous).Error
	if previousErr != nil && previousErr != gorm.ErrRecordNotFound {
		return "", previousErr
	}
	previousFound := previousErr == nil
	nextErr := s.DB.Where("project_id = ? AND episode_n = ? AND generation = ? AND `order` = ?", scene.ProjectID, scene.EpisodeN, scene.Generation, scene.Order+1).First(&next).Error
	if nextErr != nil && nextErr != gorm.ErrRecordNotFound {
		return "", nextErr
	}
	nextFound := nextErr == nil
	var previousShots []models.Shot
	var previousDialogues []models.Dialogue
	if previousFound {
		if err := s.DB.Where("scene_id = ?", previous.ID).Order("order_num, id").Find(&previousShots).Error; err != nil {
			return "", err
		}
		if err := s.DB.Where("project_id = ? AND scene_id = ?", scene.ProjectID, previous.ID).Order("`order`, id").Find(&previousDialogues).Error; err != nil {
			return "", err
		}
		previousDialogues = mergeExplicitSceneSpeech(&previous, previousDialogues)
	}
	var continuity models.SceneContinuity
	continuityErr := s.DB.Where("scene_id = ?", scene.ID).First(&continuity).Error
	if continuityErr != nil && continuityErr != gorm.ErrRecordNotFound {
		return "", continuityErr
	}
	continuityFound := continuityErr == nil
	var selectedFrame models.FrameCandidate
	selectedFrameFound := false
	if continuityFound && continuity.SelectedFrameID != nil {
		frameErr := s.DB.Where("id = ? AND project_id = ?", *continuity.SelectedFrameID, scene.ProjectID).First(&selectedFrame).Error
		if frameErr != nil && frameErr != gorm.ErrRecordNotFound {
			return "", frameErr
		}
		selectedFrameFound = frameErr == nil
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
	fmt.Fprintln(&b, "前镜生产状态（最高连续性约束；本镜开场必须承接，不得重置人物、道具或未完成动作）：")
	if previousFound {
		fmt.Fprintf(&b, "- 前一Scene[%d] %s：%s\n", previous.Order, previous.Title, previous.Content)
		for _, shot := range previousShots {
			fmt.Fprintf(&b, "- 前镜Shot %d（%.1f秒，%s）：开始=%s；可见动作=%s %s；摄影机=%s %s；结束=%s；对白=%s\n", shot.Order, shot.Duration, shot.ShotType, shot.StartState, shot.PromptSubject, shot.PromptAction, shot.CameraAngle, shot.CameraMovement, shot.EndState, shot.Dialogue)
		}
		if len(previousShots) > 0 {
			last := previousShots[len(previousShots)-1]
			fmt.Fprintf(&b, "- 必须直接承接的最终状态：%s；主体/动作：%s %s；镜头：%s %s\n", last.EndState, last.PromptSubject, last.PromptAction, last.CameraAngle, last.CameraMovement)
		}
		for _, d := range previousDialogues {
			fmt.Fprintf(&b, "- 前镜Dialogue[%s|%s] %s\n", d.SpeechType, d.Character, d.Text)
		}
	} else {
		fmt.Fprintln(&b, "- 本集当前版本没有前一Scene。")
	}
	fmt.Fprintln(&b, "本镜0.00秒连续性输入（只约束可见开场状态，不得据此创造剧情）：")
	if selectedFrameFound {
		fmt.Fprintf(&b, "- 已确认采用上一镜尾帧：frame_id=%d，文件=%s，来源Scene=%v，连续性模式=%s，状态=%s。\n", selectedFrame.ID, selectedFrame.ImageFile, continuity.SourceSceneID, continuity.Mode, continuity.Status)
	} else if continuityFound && continuity.Mode != models.ContinuityModeIndependent {
		fmt.Fprintf(&b, "- 已配置连续性模式%s，但尚无有效确认尾帧；只能依据前镜文字生产状态承接。\n", continuity.Mode)
	} else {
		fmt.Fprintln(&b, "- 未配置上一镜尾帧。")
	}
	fmt.Fprintln(&b, "后一Scene剧情边界（只用于防止本镜提前演出后续事件，不得倒灌为本镜动作）：")
	if nextFound {
		fmt.Fprintf(&b, "- 后一Scene[%d] %s：%s\n", next.Order, next.Title, next.Content)
	} else {
		fmt.Fprintln(&b, "- 本集当前版本没有后一Scene。")
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
