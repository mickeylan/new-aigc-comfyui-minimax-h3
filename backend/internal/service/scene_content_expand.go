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
	generationContent := sceneDialogueGenerationContent(&draftScene)
	context, err := s.sceneContentExpandContext(&draftScene)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	system := sceneContentExpandSystemPrompt()
	exclusionNote := ""
	if excludedCount := currentSceneDialogueExclusionCount(&draftScene); excludedCount > 0 {
		exclusionNote = fmt.Sprintf("\n用户已排除%d条引号误识别，不得当作发声。", excludedCount)
	}
	user := fmt.Sprintf("【当前未保存正文】\n%s%s\n\n【创作要求】\n%s\n\n【同集同版本、角色资产与明确对白上下文】\n%s", generationContent, exclusionNote, strings.TrimSpace(req.Requirements), context)
	output, err := s.Skills.ChatWithConfiguredOrFallbackSkill(scene.ProjectID, models.SkillStageStoryboard, sceneContentExpandSkillCode, s.TextProviderFact, system, user, map[string]string{
		"scene_id": fmt.Sprint(scene.ID), "scene_content": generationContent, "requirements": strings.TrimSpace(req.Requirements), "context": context,
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

func sceneContentExpandSystemPrompt() string {
	return `你是影视故事编辑。把当前未保存正文扩写为简洁、自然、有动作因果且可拍摄的中文剧情，不是逐秒分镜或视频提示词。只输出合法JSON：{"content":"...","director_note":"..."}。
用户本轮【创作要求】优先级最高；用户明确要求细写、增加表现细节或指定风格时照做，但仍不得篡改既有事实与对白。默认只补足理解剧情所必需的动作衔接和人物即时反应，不为显得丰富而发明微动作、反复停顿、视线游移、重心步幅、结束姿态、景别、机位、运镜、光线、天气、地貌、服装或外观。不要把短句拉成长时间轴，不默认逐秒拆解，不在结尾循环概述或重复动作。
当前正文是本镜剧情权威。前一Scene最后Shot的end_state及其位置、朝向、手持物和未完动作只约束本镜开场；已确认尾帧只约束0.00秒可见状态，不得从文件名或参考图猜剧情。后一Scene只用于避免提前演出，不得倒灌动作。扩写表现过程但不新增剧情事件、人物、物件、环境事实、能力、障碍、伤势、胜负或因果结论；尤其不得把未提供的湿土、斜阳、荒地等环境外观写成事实。
若正文已有时间点、时间段或时间轴，逐字保持这些时点及其对应事件，不增加、细分、平移或补齐切点；没有时间轴时不要创建时间码。dialogues及正文中的明确对白必须原句、原顺序、原次数自然嵌入，并写出已有证据支持的说话人或人物反应；没有明确对白不得新增台词。不要输出H3字段、英文视频提示词或Shot JSON。时长明显容不下时只在简短director_note中提示，不改变正文结果。`
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
		previousDialogues = validSceneDialogues(mergeExplicitSceneSpeech(&previous, previousDialogues))
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
	// Keep context bounded to records named by this scene or its immediate boundaries;
	// this preserves relevant identity facts without sending the full project bible.
	relevanceText := strings.Join([]string{scene.Content, scene.Title, scene.VisibleCharacters, scene.VoiceCharacters, scene.LocationName, scene.Props, previous.Content, next.Content}, "\n")
	var projectCharacters []models.Character
	if err := s.DB.Where("project_id = ?", scene.ProjectID).Order("id").Find(&projectCharacters).Error; err != nil {
		return "", err
	}
	characters := make([]models.Character, 0)
	for _, ch := range projectCharacters {
		if ch.Name != "" && strings.Contains(relevanceText, ch.Name) {
			characters = append(characters, ch)
		}
	}
	var projectAssets []models.Asset
	if err := s.DB.Where("project_id = ?", scene.ProjectID).Order("kind, id").Find(&projectAssets).Error; err != nil {
		return "", err
	}
	assets := make([]models.Asset, 0)
	for _, asset := range projectAssets {
		if asset.Name != "" && strings.Contains(relevanceText, asset.Name) {
			assets = append(assets, asset)
		}
	}
	var dialogues []models.Dialogue
	if err := s.DB.Where("project_id = ? AND scene_id = ?", scene.ProjectID, scene.ID).Order("`order`").Find(&dialogues).Error; err != nil {
		return "", err
	}
	dialogues = validSceneDialogues(mergeExplicitSceneSpeech(scene, dialogues))
	for _, ch := range projectCharacters {
		if ch.Name == "" || containsSceneExpandCharacter(characters, ch.Name) {
			continue
		}
		for _, dialogue := range dialogues {
			if dialogue.Character == ch.Name {
				characters = append(characters, ch)
				break
			}
		}
	}
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

func containsSceneExpandCharacter(characters []models.Character, name string) bool {
	for _, character := range characters {
		if character.Name == name {
			return true
		}
	}
	return false
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
