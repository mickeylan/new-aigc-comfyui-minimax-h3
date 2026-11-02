package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const sceneBlockingSkillCode = "scene_blocking_analysis"
const maxSceneBlockingBytes = 64 * 1024

// Shared by analysis and downstream image/video prompt writing; advisory only.
const sceneBlockingFacingGuidance = `朝向必须同时说明两套关系：每个人的身体/脸朝向哪个人物或场景方位，以及相对当前镜头为正面、侧面、三分之二侧面或背面；facing填写这两者，gaze单独描述视线，不能用目光方向代替身体朝向。camera说明机位位于互动双方的哪一侧，不能只写平视或中景。抱拳、行礼、交谈、对峙等动作必须明确施动者、互动对象和面向关系；例如甲向乙抱拳，应面向乙而非镜头，侧拍可让双方侧身相向，过肩拍可让乙以背影进入前景、甲朝向乙。不得为了全员露脸让双方同时面向镜头；只有原文明确对镜头表演或当前机位确实如此时才写面对镜头。机位与原文未规定的朝向属于AI建议，不冒充事实。动作起止需描述身体转向与视线变化，切换机位后重新计算画面左右及相对镜头朝向，不机械继承上一镜的正背面。`

type SceneBlockingCharacter struct {
	Name       string `json:"name"`
	Position   string `json:"position"`
	Facing     string `json:"facing"`
	Gaze       string `json:"gaze"`
	Visibility string `json:"visibility"`
}

type SceneBlocking struct {
	Facts       []string                 `json:"facts"`
	Suggestions []string                 `json:"suggestions"`
	Characters  []SceneBlockingCharacter `json:"characters"`
	Camera      string                   `json:"camera"`
	Start       string                   `json:"start"`
	End         string                   `json:"end"`
	Summary     string                   `json:"summary"`
}

func parseSceneBlocking(raw string) (SceneBlocking, error) {
	var out SceneBlocking
	raw = strings.TrimSpace(raw)
	if len(raw) == 0 || len(raw) > maxSceneBlockingBytes {
		return out, fmt.Errorf("站位内容必须为1至%d字节", maxSceneBlockingBytes)
	}
	if strings.HasPrefix(raw, "```") {
		raw = strings.TrimPrefix(raw, "```json")
		raw = strings.TrimPrefix(raw, "```")
		raw = strings.TrimSuffix(strings.TrimSpace(raw), "```")
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return out, fmt.Errorf("站位JSON无效: %w", err)
	}
	if len(out.Characters) > 100 || len(out.Facts) > 100 || len(out.Suggestions) > 100 {
		return out, fmt.Errorf("站位条目过多")
	}
	return out, nil
}

func blockingJSON(out SceneBlocking) string { b, _ := json.Marshal(out); return string(b) }

func blockingDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func blockingAppliedHash(blocking, prompt string) string {
	return blockingDigest(blocking) + ":" + blockingDigest(prompt)
}

// HandleAnalyzeSceneBlocking returns an editable preview and never writes Scene.
func (s *Service) HandleAnalyzeSceneBlocking(c *gin.Context) {
	scene, ok := s.loadScene(c)
	if !ok {
		return
	}
	if s.Skills == nil || s.TextProviderFact == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "AI 文本服务未配置"})
		return
	}
	var previous models.Scene
	prevErr := s.DB.Where("project_id=? AND episode_n=? AND generation=? AND `order` < ?", scene.ProjectID, scene.EpisodeN, scene.Generation, scene.Order).Order("`order` DESC").First(&previous).Error
	if prevErr != nil && prevErr != gorm.ErrRecordNotFound {
		c.JSON(500, gin.H{"error": prevErr.Error()})
		return
	}
	previousText := "无"
	if prevErr == nil {
		previousText = previous.BlockingJSON
	}
	system := `你是影视场面调度助手。只输出合法JSON：{"facts":[],"suggestions":[],"characters":[{"name":"","position":"","facing":"","gaze":"","visibility":""}],"camera":"","start":"","end":"","summary":""}。facts只能摘录场景原文明示事实；suggestions必须明确是AI建议。描述人物站位、机位、朝向、视线、遮挡，以及动作开始/结束空间关系。上一镜站位仅用于连续性建议，不得变成当前镜事实。不得从图片、文件名或参考素材臆测视觉细节，不得新增剧情、人物、道具或语义硬门禁。`
	user := fmt.Sprintf("场景标题：%s\n场景原文：%s\n可见人物：%s\n地点：%s\n上一镜已保存站位（可能为空）：%s", scene.Title, scene.Content, visibleSceneCharacterNames(scene), scene.LocationName, previousText)
	output, err := s.Skills.ChatWithConfiguredOrFallbackSkill(scene.ProjectID, models.SkillStageStoryboard, sceneBlockingSkillCode, s.TextProviderFact, system+"\n"+sceneBlockingFacingGuidance, user, map[string]string{"scene_id": fmt.Sprint(scene.ID), "scene_content": scene.Content, "previous_blocking": previousText})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "AI站位分析失败: " + err.Error()})
		return
	}
	result, err := parseSceneBlocking(output)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "AI返回格式无效: " + err.Error()})
		return
	}
	c.JSON(200, gin.H{"blocking": result, "persisted": false, "provider_id": s.TextProviderFact.Name(), "skill_code": sceneBlockingSkillCode})
}

func (s *Service) HandleSaveSceneBlocking(c *gin.Context) {
	scene, ok := s.loadScene(c)
	if !ok {
		return
	}
	body, err := c.GetRawData()
	if err != nil || len(body) == 0 || len(body) > maxSceneBlockingBytes {
		c.JSON(400, gin.H{"error": "站位JSON为空或过大"})
		return
	}
	result, err := parseSceneBlocking(string(body))
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	encoded := blockingJSON(result)
	if err := s.DB.Model(&models.Scene{}).Where("id=? AND project_id=?", scene.ID, scene.ProjectID).Updates(map[string]any{"blocking_json": encoded, "blocking_image_applied_hash": "", "blocking_video_applied_hash": "", "prompt_stale": true}).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"blocking": result, "persisted": true})
}

func (s *Service) HandleInheritSceneBlocking(c *gin.Context) {
	scene, ok := s.loadScene(c)
	if !ok {
		return
	}
	var previous models.Scene
	err := s.DB.Where("project_id=? AND episode_n=? AND generation=? AND `order` < ?", scene.ProjectID, scene.EpisodeN, scene.Generation, scene.Order).Order("`order` DESC").First(&previous).Error
	if err == gorm.ErrRecordNotFound {
		c.JSON(404, gin.H{"error": "同项目同集同版本没有上一镜站位"})
		return
	}
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if strings.TrimSpace(previous.BlockingJSON) == "" {
		c.JSON(404, gin.H{"error": "上一镜尚未保存站位"})
		return
	}
	result, err := parseSceneBlocking(previous.BlockingJSON)
	if err != nil {
		c.JSON(422, gin.H{"error": "上一镜站位无效"})
		return
	}
	// Previous-shot facts are not facts about the current shot.
	for _, fact := range result.Facts {
		result.Suggestions = append(result.Suggestions, "上一镜事实（本镜待确认）："+fact)
	}
	result.Facts = []string{}
	if strings.TrimSpace(result.End) != "" {
		result.Start = result.End
	}
	result.End = ""
	result.Suggestions = append(result.Suggestions, "继承自上一镜，仅作为当前镜开场连续性建议，请编辑确认")
	c.JSON(200, gin.H{"blocking": result, "persisted": false, "source_scene_id": previous.ID})
}

// HandlePreviewSceneBlockingPrompt generates a reviewable prompt without writing Scene or media state.
func (s *Service) HandlePreviewSceneBlockingPrompt(c *gin.Context) {
	scene, ok := s.loadScene(c)
	if !ok {
		return
	}
	if strings.TrimSpace(scene.BlockingJSON) == "" {
		c.JSON(400, gin.H{"error": "请先保存人物站位"})
		return
	}
	kind := c.Param("kind")
	var prompt, actionPrompt string
	var err error
	switch kind {
	case "image":
		prompt, err = s.Projects.RedesignSceneImagePrompt(scene)
	case "video":
		actionPrompt, err = s.Projects.GenerateSceneVideoAction(scene)
		if err == nil {
			var project models.Project
			if err = s.DB.First(&project, scene.ProjectID).Error; err == nil {
				dialogues := s.Projects.sceneVideoDialogues(scene)
				preview := *scene
				actionPrompt = s.Projects.applySceneShotTimeline(scene, actionPrompt)
				preview.VideoPrompt = actionPrompt
				continuity, _, refLines := s.sceneVideoPromptContinuity(scene)
				template := strings.TrimSpace(scene.VideoTemplate)
				if continuity != nil && continuity.Status == "ready" && continuity.Mode == models.ContinuityModeBridge && !sceneExplicitlyRequiresCombatTemplate(scene) {
					template = "minimax_h3_first_last"
				}
				template = enforceSceneModeVideoTemplate(scene, template)
				if template == "" {
					template = "minimax_h3_ref2v"
				}
				prompt = compileH3PromptForTemplate(template, &preview, &project, dialogues, refLines)
				var shots []models.Shot
				_ = s.DB.Where("scene_id = ?", scene.ID).Order("order_num, id").Find(&shots).Error
				shots = s.Projects.ensureShotDialogueRanges(scene.ID, shots)
				prompt = normalizeSavedH3Audio(prompt, dialogues, refLines, shots)
				prompt = applyAuthoritativeSceneShotTimeline(prompt, scene.Content)
				prompt = s.Projects.applySceneShotTimeline(scene, prompt)
			}
		}
	default:
		c.JSON(404, gin.H{"error": "未知提示词类型"})
		return
	}
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"prompt": prompt, "action_prompt": actionPrompt, "kind": kind, "blocking_hash": blockingDigest(scene.BlockingJSON), "persisted": false})
}

// HandleApplySceneBlockingPrompt only persists the reviewed prompt and its exact blocking/prompt association.
func (s *Service) HandleApplySceneBlockingPrompt(c *gin.Context) {
	scene, ok := s.loadScene(c)
	if !ok {
		return
	}
	var req struct {
		Prompt       string `json:"prompt"`
		BlockingHash string `json:"blocking_hash"`
	}
	if c.ShouldBindJSON(&req) != nil || strings.TrimSpace(req.Prompt) == "" {
		c.JSON(400, gin.H{"error": "提示词不能为空"})
		return
	}
	blockingMatches := strings.TrimSpace(scene.BlockingJSON) != "" && req.BlockingHash == blockingDigest(scene.BlockingJSON)
	kind := c.Param("kind")
	updates := map[string]any{}
	response := gin.H{"prompt": req.Prompt, "kind": kind, "applied": blockingMatches}
	switch kind {
	case "image":
		updates["image_prompt"] = req.Prompt
		if blockingMatches {
			updates["blocking_image_applied_hash"] = blockingAppliedHash(scene.BlockingJSON, req.Prompt)
		} else {
			updates["blocking_image_applied_hash"] = ""
			response["warning"] = "人物站位已变化；提示词仍已保存，但未标记为已应用当前站位"
		}
	case "video":
		actionPrompt := normalizeVideoActionPrompt(req.Prompt)
		if isH3KeyframePrompt(req.Prompt) {
			actionPrompt = h3IntegratedDescription(req.Prompt)
		}
		updates["video_full_prompt"] = req.Prompt
		updates["video_prompt"] = actionPrompt
		updates["prompt_stale"] = false
		if blockingMatches {
			updates["blocking_video_applied_hash"] = blockingAppliedHash(scene.BlockingJSON, req.Prompt)
		} else {
			updates["blocking_video_applied_hash"] = ""
			response["warning"] = "人物站位已变化；完整执行稿仍已保存，但未标记为已应用当前站位"
		}
		if strings.TrimSpace(scene.VideoPromptMasterZH) != "" {
			current, err := s.sceneVideoPromptSourceHash(scene)
			if err != nil {
				c.JSON(500, gin.H{"error": err.Error()})
				return
			}
			now := time.Now()
			updates["video_exec_source_hash"] = current
			updates["video_exec_confirmed_hash"] = hashString(req.Prompt)
			updates["video_exec_confirmed_at"] = now
			response["source_hash"] = current
			response["exec_hash"] = hashString(req.Prompt)
			response["confirmed_at"] = now
		}
		response["action_prompt"] = actionPrompt
	default:
		c.JSON(404, gin.H{"error": "未知提示词类型"})
		return
	}
	if err := s.DB.Model(&models.Scene{}).Where("id=? AND project_id=?", scene.ID, scene.ProjectID).Updates(updates).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, response)
}

func sceneBlockingPromptContext(sc *models.Scene) string {
	if sc == nil || strings.TrimSpace(sc.BlockingJSON) == "" {
		return ""
	}
	if _, err := parseSceneBlocking(sc.BlockingJSON); err != nil {
		return ""
	}
	return "\n\n【用户已保存的人物站位（事实/AI建议已分栏；作为空间连续性上下文，不得覆盖Scene剧情，不得从引用图片补细节）】\n" + sc.BlockingJSON + "\n" + sceneBlockingFacingGuidance
}
