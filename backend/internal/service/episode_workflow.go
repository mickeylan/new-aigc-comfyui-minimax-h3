package service

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type CharacterAppearance struct {
	Name        string `json:"name"`
	SceneIDs    []uint `json:"scene_ids"`
	SceneOrders []int  `json:"scene_orders"`
}

type EpisodeContinuityView struct {
	Episode         models.Episode        `json:"episode"`
	PreviousEpisode *models.Episode       `json:"previous_episode,omitempty"`
	PreviousSummary string                `json:"previous_summary"`
	PreviousHook    string                `json:"previous_hook"`
	LastSceneImages []string              `json:"last_scene_images"`
	Appearances     []CharacterAppearance `json:"character_appearances"`
	ProviderID      string                `json:"provider_id"`
	ProviderReady   bool                  `json:"provider_available"`
}

func splitCharacterNames(values ...string) []string {
	seen := map[string]bool{}
	var names []string
	for _, value := range values {
		for _, name := range strings.FieldsFunc(value, func(r rune) bool { return strings.ContainsRune(",，、;；\n", r) }) {
			name = strings.TrimSpace(name)
			if name != "" && !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
	}
	return names
}

func episodeAppearances(db *gorm.DB, projectID uint, episodeN int) ([]CharacterAppearance, error) {
	var scenes []models.Scene
	if err := db.Where("project_id = ? AND episode_n = ?", projectID, episodeN).Order("`order`, id").Find(&scenes).Error; err != nil {
		return nil, err
	}
	byName := map[string]*CharacterAppearance{}
	for _, scene := range scenes {
		for _, name := range splitCharacterNames(scene.VisibleCharacters, scene.VoiceCharacters, scene.Characters) {
			item := byName[name]
			if item == nil {
				item = &CharacterAppearance{Name: name, SceneIDs: []uint{}, SceneOrders: []int{}}
				byName[name] = item
			}
			item.SceneIDs = append(item.SceneIDs, scene.ID)
			item.SceneOrders = append(item.SceneOrders, scene.Order)
		}
	}
	out := make([]CharacterAppearance, 0, len(byName))
	for _, item := range byName {
		out = append(out, *item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *Service) textProviderConfigured() bool {
	switch s.Volc.GetSetting(SettingTextProvider, DefaultTextProvider) {
	case "llama":
		return strings.TrimSpace(s.Volc.GetSetting(SettingLlamaBaseURL, DefaultLlamaBaseURL)) != "" && strings.TrimSpace(s.Volc.GetSetting(SettingLlamaModel, DefaultLlamaModel)) != ""
	case "minimax_coding":
		return strings.TrimSpace(s.Volc.GetSetting(SettingMiniMaxBaseURL, "")) != "" && strings.TrimSpace(s.Volc.GetSetting(SettingMiniMaxModel, DefaultMiniMaxModel)) != "" && strings.TrimSpace(s.Volc.GetSetting(SettingMiniMaxAPIKey, "")) != ""
	default:
		return strings.TrimSpace(s.Volc.GetSetting(SettingVolcAPIKey, "")) != "" && strings.TrimSpace(s.Volc.GetSetting(SettingVolcTextModel, "")) != ""
	}
}

func (s *Service) HandleEpisodeContinuity(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	n, err := strconv.Atoi(c.Param("number"))
	if err != nil || n < 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid episode number"})
		return
	}
	if err := EnsureProjectEpisodes(s.DB, p.ID); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	var episode models.Episode
	if err := s.DB.Where("project_id = ? AND episode_number = ?", p.ID, n).First(&episode).Error; err != nil {
		c.JSON(404, gin.H{"error": "episode not found"})
		return
	}
	if err := refreshEpisodeEditorialStatus(s.DB, &episode); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	view := EpisodeContinuityView{Episode: episode, LastSceneImages: []string{}, ProviderID: s.TextProviderFact.Name(), ProviderReady: s.textProviderConfigured()}
	appearances, err := episodeAppearances(s.DB, p.ID, n)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	view.Appearances = appearances
	if n > 1 {
		var previous models.Episode
		if s.DB.Where("project_id = ? AND episode_number = ?", p.ID, n-1).First(&previous).Error == nil {
			if err := refreshEpisodeEditorialStatus(s.DB, &previous); err != nil {
				c.JSON(500, gin.H{"error": err.Error()})
				return
			}
			view.PreviousEpisode, view.PreviousSummary, view.PreviousHook = &previous, previous.Summary, previous.NextHook
			var scenes []models.Scene
			_ = s.DB.Where("project_id = ? AND episode_n = ? AND image_file <> ''", p.ID, n-1).Order("`order` DESC, id DESC").Limit(3).Find(&scenes).Error
			for i := len(scenes) - 1; i >= 0; i-- {
				view.LastSceneImages = append(view.LastSceneImages, scenes[i].ImageFile)
			}
		}
	}
	c.JSON(http.StatusOK, view)
}

func (s *Service) HandleRegenerateEpisodeContinuity(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	if !s.textProviderConfigured() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "当前文生文 provider 未配置，不能 AI 重生成"})
		return
	}
	n, err := strconv.Atoi(c.Param("number"))
	if err != nil || n < 1 {
		c.JSON(400, gin.H{"error": "invalid episode number"})
		return
	}
	var episode models.Episode
	if err := s.DB.Where("project_id = ? AND episode_number = ?", p.ID, n).First(&episode).Error; err != nil {
		c.JSON(404, gin.H{"error": "episode not found"})
		return
	}
	generation, err := latestEpisodeGeneration(s.DB, p.ID, n)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	var sceneCount int64
	if err := s.DB.Model(&models.Scene{}).Where("project_id = ? AND episode_n = ? AND generation = ?", p.ID, n, generation).Count(&sceneCount).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if sceneCount == 0 {
		c.JSON(409, gin.H{"error": "本集没有场景，不能生成摘要与钩子"})
		return
	}
	source, err := episodeEditorialSnapshot(s.DB, p.ID, n)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	inputHash := fmt.Sprintf("%x", sha256.Sum256(source))
	raw, err := s.TextProviderFact.Chat("只输出严格JSON对象，字段summary和next_hook均为字符串。忠实概括输入，不新增剧情、角色或对白。summary不超过180字；next_hook只描述本集结尾已存在的悬念。", string(source))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	var result struct {
		Summary  string `json:"summary"`
		NextHook string `json:"next_hook"`
	}
	if err := parseJSONObject(raw, &result); err != nil || strings.TrimSpace(result.Summary) == "" {
		c.JSON(http.StatusBadGateway, gin.H{"error": "provider 返回的摘要格式无效"})
		return
	}
	appearances, _ := episodeAppearances(s.DB, p.ID, n)
	appearanceJSON, _ := json.Marshal(appearances)
	updated, err := UpdateProjectEpisode(s.DB, p.ID, n, map[string]any{"summary": result.Summary, "next_hook": result.NextHook, "character_appearances": string(appearanceJSON), "editorial_input_hash": inputHash})
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"episode": updated, "provider_id": s.TextProviderFact.Name()})
}

func (s *Service) HandleReassignSceneEpisode(c *gin.Context) {
	scene, ok := s.loadScene(c)
	if !ok {
		return
	}
	var req struct {
		EpisodeN int `json:"episode_n"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.EpisodeN < 1 {
		c.JSON(400, gin.H{"error": "episode_n must be positive"})
		return
	}
	var episode models.Episode
	if err := s.DB.Where("project_id = ? AND episode_number = ?", scene.ProjectID, req.EpisodeN).First(&episode).Error; err != nil {
		c.JSON(404, gin.H{"error": "target episode not found"})
		return
	}
	if scene.EpisodeN == req.EpisodeN {
		c.JSON(200, scene)
		return
	}
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		var maxOrder int
		if err := tx.Model(&models.Scene{}).Where("project_id = ? AND generation = ? AND episode_n = ?", scene.ProjectID, scene.Generation, req.EpisodeN).Select("COALESCE(MAX(`order`), 0)").Scan(&maxOrder).Error; err != nil {
			return err
		}
		if err := tx.Model(scene).Updates(map[string]any{"episode_n": req.EpisodeN, "order": maxOrder + 1}).Error; err != nil {
			return err
		}
		if err := markEpisodeEditorialStale(tx, scene.ProjectID, scene.EpisodeN); err != nil {
			return err
		}
		return markEpisodeEditorialStale(tx, scene.ProjectID, req.EpisodeN)
	})
	if err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	s.DB.First(scene, scene.ID)
	c.JSON(200, scene)
}

func (s *Service) runExplicitSkill(c *gin.Context, projectID uint, stage, code, baseSystem, baseUser string, params map[string]string) {
	if !s.textProviderConfigured() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "当前文生文 provider 未配置"})
		return
	}
	output, err := s.Skills.ChatWithConfiguredOrFallbackSkill(projectID, stage, code, s.TextProviderFact, baseSystem, baseUser, params)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"draft": strings.TrimSpace(output), "skill_code": code, "provider_id": s.TextProviderFact.Name(), "audited": true})
}

func (s *Service) HandleVisualBeatDecomposition(c *gin.Context) {
	scene, ok := s.loadScene(c)
	if !ok {
		return
	}
	var req struct {
		TargetDuration float64 `json:"target_duration"`
	}
	_ = c.ShouldBindJSON(&req)
	if req.TargetDuration <= 0 {
		req.TargetDuration = scene.Duration
	}
	s.runExplicitSkill(c, scene.ProjectID, models.SkillStageStoryboard, "director-visual-beat-decomposition", "只输出技能约定的严格JSON，不自动保存或执行。", "生成供用户审核的视觉节拍拆镜草稿。", map[string]string{"story_content": scene.Content, "characters": strings.Join(splitCharacterNames(scene.VisibleCharacters, scene.VoiceCharacters, scene.Characters), "、"), "asset_context": strings.TrimSpace(scene.LocationName + " " + scene.Props), "target_duration": fmt.Sprintf("%.1f", req.TargetDuration)})
}

func (s *Service) HandleFaithfulPromptPolish(c *gin.Context) {
	scene, ok := s.loadScene(c)
	if !ok {
		return
	}
	var req struct {
		OriginalPrompt       string `json:"original_prompt"`
		ImmutableFacts       string `json:"immutable_facts"`
		EditablePresentation string `json:"editable_presentation"`
		Feedback             string `json:"feedback"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if strings.TrimSpace(req.OriginalPrompt) == "" {
		req.OriginalPrompt = scene.ImagePrompt
	}
	if strings.TrimSpace(req.ImmutableFacts) == "" {
		req.ImmutableFacts = scene.Content
	}
	s.runExplicitSkill(c, scene.ProjectID, models.SkillStageImagePrompt, "director-faithful-prompt-polish", "只返回润色草稿，不自动保存。", "忠实最小改动润色，结果交由用户审核。", map[string]string{"original_prompt": req.OriginalPrompt, "immutable_facts": req.ImmutableFacts, "editable_presentation": req.EditablePresentation, "feedback": req.Feedback})
}

func (s *Service) HandleCreativeIntent(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	var req struct {
		Action   string `json:"action"`
		UserGoal string `json:"user_goal"`
		Answers  string `json:"answers"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if req.Action != "questions" && req.Action != "brief" {
		c.JSON(400, gin.H{"error": "action must be questions or brief"})
		return
	}
	s.runExplicitSkill(c, p.ID, models.SkillStageCreativeIntent, "creative-intent-elicitation", "只输出严格JSON；不自动修改项目。", "用户已显式请求创作意图澄清。", map[string]string{"project_context": fmt.Sprintf("标题：%s\n题材：%s\n梗概：%s", p.Title, p.Genre, p.Synopsis), "user_goal": req.UserGoal, "action": req.Action, "answers": req.Answers})
}
