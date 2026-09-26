package service

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"comfyui-console/internal/models"
)

// ---------- 平台设置 ----------

// HandleGetSettings 读取平台设置（API Key 打码返回）
func (s *Service) HandleGetSettings(c *gin.Context) {
	settings := s.Volc.AllSettings()
	// 添加当前选中的文生文 provider
	settings["text_provider"] = s.Volc.GetSetting(SettingTextProvider, DefaultTextProvider)
	// 添加 llama 配置
	settings["llama_base_url"] = s.Volc.GetSetting(SettingLlamaBaseURL, DefaultLlamaBaseURL)
	settings["llama_model"] = s.Volc.GetSetting(SettingLlamaModel, DefaultLlamaModel)
	settings[SettingMiniMaxBaseURL] = s.Volc.GetSetting(SettingMiniMaxBaseURL, "")
	settings[SettingMiniMaxModel] = s.Volc.GetSetting(SettingMiniMaxModel, DefaultMiniMaxModel)
	c.JSON(200, settings)
}

// HandleUpdateSettings 保存平台设置
func (s *Service) HandleUpdateSettings(c *gin.Context) {
	var req map[string]string
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	if tp, ok := req["text_provider"]; ok && tp != "llama" && tp != "volcano" && tp != "minimax_coding" {
		c.JSON(400, gin.H{"error": "text_provider 仅支持 llama、minimax_coding 或 volcano"})
		return
	}
	s.Volc.UpdateSettings(req)
	log.Printf("[settings] text provider: %s", s.TextProviderFact.Name())
	c.JSON(200, gin.H{"ok": true})
}

// HandleTestText 测试文生文接口（根据当前选中的 provider 测试）
func (s *Service) HandleTestText(c *gin.Context) {
	provider := s.TextProviderFact.Create()
	text, err := provider.Chat("你是连接测试助手。", "请只回复两个字：正常")
	if err != nil {
		c.JSON(500, gin.H{"ok": false, "error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true, "message": fmt.Sprintf("[%s] 文生文连通正常: %s", provider.Name(), strings.TrimSpace(text))})
}

// HandleTestImage 测试文生图接口（仅火山引擎支持）
func (s *Service) HandleTestImage(c *gin.Context) {
	msg, err := s.Volc.TestImage()
	if err != nil {
		c.JSON(500, gin.H{"ok": false, "error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true, "message": "文生图连通正常: " + msg})
}

// HandleTestTTS 测试阿里云 TTS 配音接口
func (s *Service) HandleTestTTS(c *gin.Context) {
	ali := NewAliyunTTS(s.DB)
	msg, err := ali.TestTTS()
	if err != nil {
		c.JSON(500, gin.H{"ok": false, "error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true, "message": "配音连通正常: " + msg})
}

// ---------- 漫剧项目 ----------

func (s *Service) HandleListProjects(c *gin.Context) {
	list, err := s.Projects.ListProjects()
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	// 附加场景统计
	out := make([]gin.H, 0, len(list))
	for _, p := range list {
		var total, ready, videos int64
		s.DB.Where("project_id = ?", p.ID).Count(&total)
		s.DB.Where("project_id = ? AND status = ?", p.ID, "video_ready").Count(&ready)
		s.DB.Where("project_id = ? AND video_file != ''", p.ID).Count(&videos)
		out = append(out, gin.H{
			"project": p, "scene_total": total,
			"scene_video_ready": ready, "scene_videos": videos,
		})
	}
	c.JSON(200, out)
}

func (s *Service) HandleCreateProject(c *gin.Context) {
	var req struct {
		models.Project
		SourceType string `json:"source_type"` // outline(默认)/novel
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	if req.Synopsis == "" {
		c.JSON(400, gin.H{"error": "请填写故事创意"})
		return
	}
	// 设置来源类型（默认梗概）
	if req.SourceType == "novel" {
		req.Project.SourceType = models.ProjectSourceNovel
		req.Project.ImportStatus = models.NovelImportPending
	} else {
		req.Project.SourceType = models.ProjectSourceOutline
	}
	p, err := s.Projects.CreateProject(req.Project)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, p)
}

func (s *Service) HandleGetProject(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid id"})
		return
	}
	p, scenes, err := s.Projects.GetProject(uint(id))
	if err != nil {
		c.JSON(404, gin.H{"error": "project not found"})
		return
	}
	merges, _ := s.Projects.ListMerges(uint(id))
	chars, _ := s.Projects.ListCharacters(uint(id))
	counts, _ := s.Projects.CharacterSceneCounts(uint(id))
	assets, _ := s.Projects.ListAssets(uint(id), "")
	assetCounts, _ := s.Projects.AssetSceneCounts(uint(id))
	var dialogues []models.Dialogue
	s.DB.Where("project_id = ?", uint(id)).Order("scene_id, `order`").Find(&dialogues)
	episodes, _ := ProjectEpisodeHierarchy(s.DB, uint(id))
	c.JSON(200, gin.H{"project": p, "episodes": episodes, "scenes": scenes, "merges": merges, "characters": chars, "character_counts": counts, "assets": assets, "asset_counts": assetCounts, "dialogues": dialogues})
}

func (s *Service) HandleCreateProjectEpisode(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	var episode models.Episode
	if err := c.ShouldBindJSON(&episode); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	created, err := CreateProjectEpisode(s.DB, p.ID, episode)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, created)
}

func (s *Service) HandleUpdateProjectEpisode(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	number, err := strconv.Atoi(c.Param("number"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid episode number"})
		return
	}
	var updates map[string]any
	if err := c.ShouldBindJSON(&updates); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	updated, err := UpdateProjectEpisode(s.DB, p.ID, number, updates)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, updated)
}

func (s *Service) HandleDeleteProjectEpisode(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	number, err := strconv.Atoi(c.Param("number"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid episode number"})
		return
	}
	if err := DeleteProjectEpisode(s.DB, p.ID, number); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Service) HandleGetProjectEpisodes(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	episodes, err := ProjectEpisodeHierarchy(s.DB, p.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, episodes)
}

func (s *Service) HandleDeleteProject(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid id"})
		return
	}
	if err := s.Projects.DeleteProject(uint(id)); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

// HandleUpdateProject 编辑项目信息
func (s *Service) HandleUpdateProject(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	var req models.Project
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	if strings.TrimSpace(req.Synopsis) == "" {
		c.JSON(400, gin.H{"error": "故事创意不能为空"})
		return
	}
	if err := s.Projects.UpdateProject(p, req); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	np, _, _ := s.Projects.GetProject(p.ID)
	c.JSON(200, np)
}

// HandleUpdateScene 编辑场景文案（修改画面提示词会重置画面与视频）
func (s *Service) HandleRedesignScenePrompt(c *gin.Context) {
	sc, ok := s.loadScene(c)
	if !ok {
		return
	}
	var req struct {
		Brief       string                    `json:"brief"`
		ImageEngine string                    `json:"image_engine"`
		Outfits     []OutfitAssignment        `json:"outfits"`
		References  []SceneReferenceSelection `json:"references"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	if strings.TrimSpace(req.Brief) != "" {
		sc.Content = strings.TrimSpace(req.Brief)
	}
	if engine, err := normalizeSceneImageEngine(req.ImageEngine); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	} else {
		sc.ImageEngine = engine
	}
	if err := s.CharacterLooks.AssignSceneOutfits(sc.ProjectID, sc.ID, req.Outfits); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := s.Projects.SaveSceneReferences(sc, req.References); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := s.DB.First(sc, sc.ID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	sc.ImageEngine, _ = normalizeSceneImageEngine(req.ImageEngine)
	prompt, err := s.Projects.RedesignSceneImagePrompt(sc)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"prompt": prompt})
}

type sceneVideoReferenceBinding struct {
	Picture  int    `json:"picture"`
	Subject  *int   `json:"subject"`
	Role     string `json:"role"`
	Identity string `json:"identity"`
	Usage    string `json:"usage"`
	Label    string `json:"label"`
	TaskID   string `json:"task_id"`
	Name     string `json:"name"`
	ImageURL string `json:"image_url"`
}

func sceneVideoReferenceBindings(refs []FileMeta, lines []string) []sceneVideoReferenceBinding {
	out := make([]sceneVideoReferenceBinding, 0, len(refs))
	for i, ref := range refs {
		label := fmt.Sprintf("Reference %d", i+1)
		if i < len(lines) {
			label = strings.TrimSpace(strings.TrimPrefix(lines[i], "-"))
		}
		role, usage := "reference", "外观参考"
		switch {
		case strings.Contains(label, "当前分镜") || strings.Contains(label, "开始画面"):
			role, usage = "storyboard", "起始构图与动作状态"
		case strings.Contains(label, "场景") || strings.Contains(label, "环境"):
			role, usage = "location", "环境与空间参考"
		case strings.Contains(label, "道具"):
			role, usage = "prop", "道具外观参考"
		case strings.Contains(label, "造型"):
			role, usage = "look", "服装、发型与配饰参考"
		case strings.Contains(label, "角色"):
			role, usage = "character", "身份、五官与年龄参考"
		}
		identity := label
		if left := strings.Index(label, "「"); left >= 0 {
			if right := strings.Index(label[left+len("「"):], "」"); right >= 0 {
				identity = label[left+len("「") : left+len("「")+right]
			}
		}
		var subject *int
		if role != "storyboard" {
			value := i + 1
			subject = &value
		}
		out = append(out, sceneVideoReferenceBinding{Picture: i + 1, Subject: subject, Role: role, Identity: identity, Usage: usage, Label: label, TaskID: ref.TaskID, Name: ref.Name, ImageURL: fmt.Sprintf("/api/input/%s/%s", ref.TaskID, ref.Name)})
	}
	return out
}

func (s *Service) sceneVideoPromptContinuity(sc *models.Scene) (*models.SceneContinuity, []FileMeta, []string) {
	refs, lines, cfg := s.Projects.sceneVideoContinuityReferences(sc, fmt.Sprint(sc.ProjectID))
	return cfg, refs, lines
}

func (s *Service) HandleGetSceneVideoPrompt(c *gin.Context) {
	sc, ok := s.loadScene(c)
	if !ok {
		return
	}
	actionPrompt := normalizeVideoActionPrompt(sc.VideoFullPrompt)
	if actionPrompt == "" {
		actionPrompt = normalizeVideoActionPrompt(sc.VideoPrompt)
	}
	hasSavedPrompt := strings.TrimSpace(sc.VideoFullPrompt) != ""
	var project models.Project
	if err := s.DB.First(&project, sc.ProjectID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	continuity, refs, refLines := s.sceneVideoPromptContinuity(sc)
	// 查看/编辑接口只读取用户保存值，绝不自动生成或重建。
	// 参考图变化仅返回校验问题，由用户决定修改或明确点击 AI 重新生成。
	fullPrompt := strings.TrimSpace(sc.VideoFullPrompt)
	promptIssues := []string{}
	if hasSavedPrompt {
		promptIssues = ValidateFullH3PromptForReferences(fullPrompt, refLines)
	}
	width, height := aspectVideoSize(project.AspectRatio, s.Projects.videoResolution())
	template := strings.TrimSpace(sc.VideoTemplate)
	if template == "" {
		template = "minimax_h3_ref2v"
	}
	response := gin.H{
		"prompt": fullPrompt, "full_prompt": fullPrompt, "action_prompt": actionPrompt,
		"has_saved_prompt": hasSavedPrompt, "prompt_stale": sc.PromptStale, "prompt_issues": promptIssues,
		"reference_limit_warning": s.Projects.sceneVideoReferenceWarning(sc, len(refs)),
		"template":                template, "width": width, "height": height, "reference_count": len(refs),
		"duration": normalizeSceneDuration(sc.Duration), "fps": 24, "steps": 20,
		"reference_bindings": sceneVideoReferenceBindings(refs, refLines),
	}
	if continuity != nil {
		response["continuity_mode"] = continuity.Mode
		response["continuity_status"] = continuity.Status
		response["continuity_source_scene_id"] = continuity.SourceSceneID
		if continuity.SelectedFrame != nil {
			response["continuity_frame"] = s.frameResponses([]models.FrameCandidate{*continuity.SelectedFrame})[0]
			if continuity.Mode == models.ContinuityModeBridge {
				response["template"] = "minimax_h3_first_last"
				response["first_frame_img"] = continuity.SelectedFrame.ImageFile
				response["last_frame_img"] = sc.ImageFile
			}
		}
	}
	c.JSON(http.StatusOK, response)
}

func (s *Service) HandleRegenerateSceneVideoPrompt(c *gin.Context) {
	sc, ok := s.loadScene(c)
	if !ok {
		return
	}
	actionPrompt, err := s.Projects.GenerateSceneVideoAction(sc)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	var project models.Project
	if err := s.DB.First(&project, sc.ProjectID).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	dubs := s.Projects.sceneVideoDialogues(sc)
	preview := *sc
	actionPrompt = s.Projects.applySceneShotTimeline(sc, actionPrompt)
	preview.VideoPrompt = actionPrompt
	continuity, refs, lines := s.sceneVideoPromptContinuity(sc)
	var req struct {
		Template string `json:"template"`
	}
	_ = c.ShouldBindJSON(&req)
	template := strings.TrimSpace(req.Template)
	if template == "" {
		template = strings.TrimSpace(sc.VideoTemplate)
	}
	if continuity != nil && continuity.Status == "ready" && continuity.Mode == models.ContinuityModeBridge {
		template = "minimax_h3_first_last"
	}
	if template == "" {
		template = "minimax_h3_ref2v"
	}
	fullPrompt := compileH3PromptForTemplate(template, &preview, &project, dubs, lines)
	var shots []models.Shot
	_ = s.DB.Where("scene_id = ?", sc.ID).Order("order_num, id").Find(&shots).Error
	shots = s.Projects.ensureShotDialogueRanges(sc.ID, shots)
	fullPrompt = normalizeSavedH3Audio(fullPrompt, dubs, lines, shots)
	fullPrompt = applyShotTimeline(fullPrompt, shots, normalizeSceneDuration(sc.Duration))
	if issues := validateGeneratedH3Prompt(fullPrompt, template, normalizeSceneDuration(sc.Duration)); len(issues) > 0 {
		c.JSON(http.StatusBadGateway, gin.H{"error": "生成的视频提示词不符合 MiniMax H3 官方格式: " + strings.Join(issues, "；")})
		return
	}
	response := gin.H{"prompt": fullPrompt, "full_prompt": fullPrompt, "action_prompt": actionPrompt, "reference_count": len(refs), "reference_bindings": sceneVideoReferenceBindings(refs, lines), "template": template}
	if continuity != nil && continuity.SelectedFrame != nil {
		response["continuity_mode"] = continuity.Mode
		response["continuity_frame"] = s.frameResponses([]models.FrameCandidate{*continuity.SelectedFrame})[0]
	}
	c.JSON(200, response)
}

func (s *Service) HandleUploadSceneVideoFrame(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	sc, ok := s.loadScene(c)
	if !ok {
		return
	}
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请选择首帧或尾帧图片"})
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 25<<20))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext != ".png" && ext != ".jpg" && ext != ".jpeg" && ext != ".webp" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "仅支持 PNG/JPG/WebP"})
		return
	}
	kind := "first"
	if c.Query("kind") == "last" {
		kind = "last"
	}
	name := fmt.Sprintf("video_%s_s%d_%d%s", kind, sc.ID, time.Now().UnixNano(), ext)
	path, _, err := s.Upload.SaveFile(fmt.Sprint(p.ID), "image", name, data)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"file": filepath.Base(path), "kind": kind})
}

func (s *Service) HandleUpdateSceneVideoPrompt(c *gin.Context) {
	sc, ok := s.loadScene(c)
	if !ok {
		return
	}
	var req struct {
		Prompt        string `json:"prompt"`          // 用户编辑后的完整 H3 提示词
		Template      string `json:"template"`        // 可选：用户指定视频模板
		FirstFrameImg string `json:"first_frame_img"` // 首尾帧模板的首帧图
		LastFrameImg  string `json:"last_frame_img"`  // 首尾帧模板的尾帧图
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	prompt := strings.TrimSpace(req.Prompt)
	_, _, refLines := s.sceneVideoPromptContinuity(sc)
	dubs := s.Projects.sceneVideoDialogues(sc)
	if isH3KeyframePrompt(prompt) {
		// User visual edits remain authoritative, but spoken content is always rebuilt from
		// structured Dialogue and placed in its persisted Shot interval.
		var shots []models.Shot
		_ = s.DB.Where("scene_id = ?", sc.ID).Order("order_num, id").Find(&shots).Error
		shots = s.Projects.ensureShotDialogueRanges(sc.ID, shots)
		prompt = normalizeSavedH3Audio(prompt, dubs, refLines, shots)
		prompt = applyShotTimeline(prompt, shots, normalizeSceneDuration(sc.Duration))
	}
	template := strings.TrimSpace(req.Template)
	if template == "" {
		template = strings.TrimSpace(sc.VideoTemplate)
	}
	var contractIssues []string
	if template == "minimax_h3_t2v" || template == "minimax_h3_i2v" || template == "minimax_h3_first_last" {
		contractIssues = validateH3KeyframePrompt(prompt, template)
	} else {
		contractIssues = ValidateFullH3PromptForReferences(prompt, refLines)
	}
	if len(contractIssues) > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": strings.Join(contractIssues, "；")})
		return
	}
	// 保存接口只校验并原样持久化用户内容，不重写对白、音频段或视觉正文。
	actionPrompt := normalizeVideoActionPrompt(prompt)
	if isH3KeyframePrompt(prompt) {
		actionPrompt = h3IntegratedDescription(prompt)
	}
	if issues := ValidateVideoPrompt(actionPrompt, sc.Characters, sc.LocationName, sc.Props); len(issues) > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": strings.Join(issues, "；")})
		return
	}
	updates := map[string]any{"video_full_prompt": prompt, "video_prompt": actionPrompt, "prompt_stale": false}
	if strings.TrimSpace(req.Template) != "" {
		updates["video_template"] = strings.TrimSpace(req.Template)
	}
	if strings.TrimSpace(req.FirstFrameImg) != "" {
		updates["video_first_frame_img"] = strings.TrimSpace(req.FirstFrameImg)
	}
	if strings.TrimSpace(req.LastFrameImg) != "" {
		updates["video_last_frame_img"] = strings.TrimSpace(req.LastFrameImg)
	}
	result := s.DB.Model(&models.Scene{}).Where("id = ? AND project_id = ?", sc.ID, sc.ProjectID).Updates(updates)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": result.Error.Error()})
		return
	}
	if prompt != strings.TrimSpace(sc.VideoFullPrompt) || template != strings.TrimSpace(sc.VideoTemplate) {
		if err := MarkSceneCandidatesStale(s.DB, sc.ProjectID, sc.ID, "video", "视频提示词或模板已修改"); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	var saved models.Scene
	if err := s.DB.Select("video_full_prompt", "video_template", "video_first_frame_img", "video_last_frame_img").First(&saved, sc.ID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "video_full_prompt": saved.VideoFullPrompt, "video_template": saved.VideoTemplate, "first_frame_img": saved.VideoFirstFrameImg, "last_frame_img": saved.VideoLastFrameImg})
}

func (s *Service) HandleUpdateScene(c *gin.Context) {
	sc, ok := s.loadScene(c)
	if !ok {
		return
	}
	var req struct {
		Title               string  `json:"title"`
		Content             string  `json:"content"`
		ImagePrompt         string  `json:"image_prompt"`
		ImageEngine         string  `json:"image_engine"`
		VideoPrompt         string  `json:"video_prompt"`
		VisibleCharacters   string  `json:"visible_characters"`
		VoiceCharacters     string  `json:"voice_characters"`
		MentionedCharacters string  `json:"mentioned_characters"`
		Duration            float64 `json:"duration"`
		VisualType          string  `json:"visual_type"`
		MegaType            string  `json:"mega_type"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	if strings.TrimSpace(req.Content) == "" || strings.TrimSpace(req.ImagePrompt) == "" {
		c.JSON(400, gin.H{"error": "场景正文和画面提示词不能为空"})
		return
	}
	if req.Duration < 3 || req.Duration > 15 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "分镜时长必须在 3–15 秒之间"})
		return
	}
	imageEngine, err := normalizeSceneImageEngine(req.ImageEngine)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	currentImageEngine, _ := normalizeSceneImageEngine(sc.ImageEngine)
	engineChanged := imageEngine != currentImageEngine
	sourceChanged := strings.TrimSpace(req.Content) != strings.TrimSpace(sc.Content) || strings.TrimSpace(req.ImagePrompt) != strings.TrimSpace(sc.ImagePrompt) || req.Duration != sc.Duration || strings.TrimSpace(req.VideoPrompt) != strings.TrimSpace(sc.VideoPrompt) || strings.TrimSpace(req.VisibleCharacters) != strings.TrimSpace(sc.VisibleCharacters) || strings.TrimSpace(req.VoiceCharacters) != strings.TrimSpace(sc.VoiceCharacters) || strings.TrimSpace(req.MentionedCharacters) != strings.TrimSpace(sc.MentionedCharacters) || engineChanged
	if err := s.Projects.UpdateScene(sc, req.Title, req.Content, req.ImagePrompt, req.Duration, req.VisualType, req.MegaType); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	roleUpdates := map[string]any{
		"image_engine":         imageEngine,
		"video_prompt":         strings.TrimSpace(req.VideoPrompt),
		"visible_characters":   strings.TrimSpace(req.VisibleCharacters),
		"voice_characters":     strings.TrimSpace(req.VoiceCharacters),
		"mentioned_characters": strings.TrimSpace(req.MentionedCharacters),
		"character_roles_set":  true,
		"characters":           strings.TrimSpace(req.VisibleCharacters),
		"prompt_stale":         sc.PromptStale || (strings.TrimSpace(sc.VideoFullPrompt) != "" && sourceChanged),
	}
	if engineChanged {
		roleUpdates["image_file"], roleUpdates["image_task_id"], roleUpdates["video_file"], roleUpdates["video_input_file"], roleUpdates["video_task_id"] = "", "", "", "", ""
		roleUpdates["status"], roleUpdates["error"] = "pending", ""
	}
	if err := s.DB.Model(sc).Updates(roleUpdates).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	var updated models.Scene
	s.DB.First(&updated, sc.ID)
	c.JSON(200, updated)
}

// HandleUpdatePlanEpisodes 修改创作方案中每集的标题与剧情提示词
func (s *Service) HandleUpdatePlanEpisodes(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	var req struct {
		Episodes []PlanEpisodeUpdate `json:"episodes"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	if len(req.Episodes) == 0 {
		c.JSON(400, gin.H{"error": "episodes 不能为空"})
		return
	}
	np, err := s.Projects.UpdatePlanEpisodes(p, req.Episodes)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"project": np})
}

// HandleGeneratePlan 阶段 1：按 short-drama 方法论生成创作方案
func (s *Service) HandleGeneratePlan(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	if p.Episodes > DefaultBatchSizeMax {
		c.JSON(409, gin.H{"error": fmt.Sprintf("全剧预计 %d 集，禁止一次生成完整方案；请使用滚动规划，按每批 5–20 集生成", p.Episodes), "redirect": fmt.Sprintf("/projects/%d/adaptation", p.ID)})
		return
	}
	if err := s.Projects.ClaimManualScript(p); err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	fresh, err := s.Projects.GeneratePlan(p)
	if err != nil {
		s.Projects.failPipeline(p.ID, err, false)
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"project": fresh})
}

// HandleRenderScriptFromText 用户编辑剧本后重新渲染该集分镜场景（剧本正文以用户文本为准）
func (s *Service) HandleRenderScriptFromText(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	if err := s.Projects.ClaimManualScript(p); err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	var req struct {
		EpisodeN int    `json:"episode_n"`
		Script   string `json:"script"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	p, scenes, err := s.Projects.GenerateScriptFromText(p, req.EpisodeN, req.Script)
	if err != nil {
		s.Projects.failPipeline(p.ID, err, false)
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"project": p, "scenes": scenes})
}

// HandleExpandScript AI 扩写剧本正文（不落库，返回扩写后文本由前端回填）
func (s *Service) HandleExpandScript(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	var req struct {
		EpisodeN int    `json:"episode_n"`
		Script   string `json:"script"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	if strings.TrimSpace(req.Script) == "" {
		c.JSON(400, gin.H{"error": "剧本内容不能为空"})
		return
	}
	if _, err := s.ScriptRevisions.Create(p.ID, req.EpisodeN, "before_script_expand"); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	out, err := s.Projects.ExpandScript(p, req.EpisodeN, req.Script)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"script": out})
}

// HandleGenerateScript 生成剧本（有创作方案时按方案渲染分镜）。
// query 参数 episode_n 指定当前制作集（默认 1），只生成并替换该集分镜场景。
func (s *Service) HandleGenerateScript(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	if err := s.Projects.ClaimManualScript(p); err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	episodeN, _ := strconv.Atoi(c.DefaultQuery("episode_n", "1"))
	p, scenes, err := s.Projects.GenerateScript(p, episodeN)
	if err != nil {
		s.Projects.failPipeline(p.ID, err, false)
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"project": p, "scenes": scenes})
}

// HandleGenerateProject 启动剧本→画面→视频→合并的持久化一键生成流程。
// query auto=true（创建项目自动触发）：生成完创作方案+角色+第一集剧本后停止，后续由人工处理。
func (s *Service) HandleGenerateProject(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	stopAfterScript := c.Query("auto") == "1" || c.Query("auto") == "true"
	episodeN, _ := strconv.Atoi(c.DefaultQuery("episode_n", "1"))
	if episodeN <= 0 {
		episodeN = 1
	}
	current, err := s.Projects.StartPipeline(p, episodeN, stopAfterScript)
	if err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	msg := fmt.Sprintf("第%d集完整生成流程已启动", episodeN)
	if stopAfterScript {
		msg = "已启动自动生成：创作方案、角色与第一集剧本，完成后由人工处理后续制作"
	}
	c.JSON(http.StatusAccepted, gin.H{"ok": true, "project": current, "message": msg})
}

// HandleUpdateSceneLocks explicitly locks or unlocks generation output overwrite protection.
func (s *Service) HandleUpdateSceneLocks(c *gin.Context) {
	sc, ok := s.loadScene(c)
	if !ok {
		return
	}
	var req struct {
		ImageLocked *bool `json:"image_locked"`
		VideoLocked *bool `json:"video_locked"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || (req.ImageLocked == nil && req.VideoLocked == nil) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "至少提供一个锁定状态"})
		return
	}
	updates := map[string]any{}
	if req.ImageLocked != nil {
		updates["image_locked"] = *req.ImageLocked
		sc.ImageLocked = *req.ImageLocked
	}
	if req.VideoLocked != nil {
		updates["video_locked"] = *req.VideoLocked
		sc.VideoLocked = *req.VideoLocked
	}
	if err := s.DB.Model(sc).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, sc)
}

// HandleGenerateSceneImage 生成单个场景画面
func (s *Service) HandleGenerateSceneImage(c *gin.Context) {
	sc, ok := s.loadScene(c)
	if !ok {
		return
	}
	if err := s.Projects.StartSceneImage(sc); err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true, "message": fmt.Sprintf("场景 %d 画面生成中", sc.Order)})
}

// HandleUploadSceneImage 上传分镜图替代 AI 生成（最大 25 MB，PNG/JPG/WebP）
func (s *Service) HandleUploadSceneImage(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	sc, ok := s.loadScene(c)
	if !ok {
		return
	}
	file, _, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(400, gin.H{"error": "file required: " + err.Error()})
		return
	}
	defer file.Close()
	const maxSceneImageBytes = 25 * 1024 * 1024
	data, err := io.ReadAll(io.LimitReader(file, maxSceneImageBytes+1))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if len(data) == 0 {
		c.JSON(400, gin.H{"error": "空文件"})
		return
	}
	if len(data) > maxSceneImageBytes {
		c.JSON(400, gin.H{"error": "文件不超过 25 MB"})
		return
	}
	ext := uploadedSceneImageExt(data)
	if ext == "" {
		c.JSON(400, gin.H{"error": "仅支持有效的 PNG/JPG/WebP 图片"})
		return
	}
	name := fmt.Sprintf("scene_%d_%d%s", sc.ID, time.Now().UnixNano(), ext)
	path, _, err := s.Upload.SaveFile(fmt.Sprintf("%d", p.ID), "image", name, data)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if err := s.Projects.ReplaceSceneImage(sc, filepath.Base(path)); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true, "message": "分镜图已上传，旧视频已失效", "image_file": filepath.Base(path)})
}

func uploadedSceneImageExt(data []byte) string {
	switch {
	case len(data) >= 8 && string(data[:8]) == "\x89PNG\r\n\x1a\n":
		return ".png"
	case len(data) >= 3 && data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff:
		return ".jpg"
	case len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		return ".webp"
	default:
		return ""
	}
}

// HandleGenerateAllImages 一键生成当前集全部画面（query episode_n 过滤，默认全部）
func (s *Service) HandleGenerateAllImages(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	episodeN, _ := strconv.Atoi(c.DefaultQuery("episode_n", "0"))
	n, err := s.Projects.GenerateAllImages(p, episodeN)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true, "message": fmt.Sprintf("已提交 %d 个场景的画面生成", n)})
}

// HandleGenerateSceneVideo 生成单个场景视频（本地 L40）
func (s *Service) HandleGenerateSceneVideo(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	sc, ok := s.loadScene(c)
	if !ok {
		return
	}
	if err := s.Projects.GenerateSceneVideo(p, sc); err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true, "message": fmt.Sprintf("场景 %d 视频任务已创建", sc.Order)})
}

// HandleCancelSceneVideo 停止单个场景视频生成
func (s *Service) HandleCancelSceneVideo(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	sc, ok := s.loadScene(c)
	if !ok {
		return
	}
	if err := s.Projects.CancelSceneVideo(p, sc); err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true, "message": fmt.Sprintf("场景 %d 视频已停止", sc.Order)})
}

// HandleGenerateAllVideos 一键生成当前集全部视频（query episode_n 过滤，默认全部）
func (s *Service) HandleGenerateAllVideos(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	episodeN, _ := strconv.Atoi(c.DefaultQuery("episode_n", "0"))
	n, err := s.Projects.GenerateAllVideos(p, episodeN)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true, "message": fmt.Sprintf("已提交 %d 个场景的视频生成", n)})
}

// HandleCreateMerge 合并视频成片
func (s *Service) HandleCreateMerge(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	var req struct {
		SceneIDs       []uint   `json:"scene_ids"`
		Dub            bool     `json:"dub"`       // true=保留原音轨(默认), false=静音
		Subtitles      bool     `json:"subtitles"` // true=烧录字幕(默认), false=不烧录
		NativeVolume   *float64 `json:"native_volume"`
		DialogueVolume *float64 `json:"dialogue_volume"`
		BGMVolume      *float64 `json:"bgm_volume"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	options, err := mergeAudioOptions(req.NativeVolume, req.DialogueVolume, req.BGMVolume)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	mt, err := s.Projects.CreateMergeTaskWithAudio(p, req.SceneIDs, req.Dub, req.Subtitles, options)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, mt)
}

// HandleCreateAllMerges 整剧一键合并（所有含就绪视频的集）
func (s *Service) HandleCreateAllMerges(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	var req struct {
		Dub       bool `json:"dub"`
		Subtitles bool `json:"subtitles"`
	}
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	n, err := s.Projects.CreateAllMerges(p, req.Dub, req.Subtitles)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if n == 0 {
		c.JSON(400, gin.H{"error": "没有可合并的集（每集至少需 2 个视频就绪场景）"})
		return
	}
	c.JSON(200, gin.H{"ok": true, "message": fmt.Sprintf("已为 %d 集启动合并", n)})
}

// HandleListMerges 合并任务列表
func (s *Service) HandleListMerges(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid id"})
		return
	}
	list, err := s.Projects.ListMerges(uint(id))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, list)
}

// ---------- 角色资产 ----------

func (s *Service) HandleListCharacters(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	chars, err := s.Projects.ListCharacters(p.ID)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	counts, _ := s.Projects.CharacterSceneCounts(p.ID)
	c.JSON(200, gin.H{"characters": chars, "counts": counts})
}

func (s *Service) HandleCreateCharacter(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	var req models.Character
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	req.ID = 0
	req.ProjectID = p.ID
	ch, err := s.Projects.CreateCharacter(req)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, ch)
}

func (s *Service) HandleUpdateCharacter(c *gin.Context) {
	ch, ok := s.loadCharacter(c)
	if !ok {
		return
	}
	var req models.Character
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	if err := s.Projects.UpdateCharacter(ch, req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	var updated models.Character
	s.DB.First(&updated, ch.ID)
	c.JSON(200, updated)
}

func (s *Service) HandleDeleteCharacter(c *gin.Context) {
	ch, ok := s.loadCharacter(c)
	if !ok {
		return
	}
	if err := s.Projects.DeleteCharacter(ch.ProjectID, ch.ID); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (s *Service) HandleGenerateCharacterPortrait(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	ch, ok := s.loadCharacter(c)
	if !ok {
		return
	}
	if s.CharacterLooks != nil {
		if _, err := s.CharacterLooks.EnsureDefaultLook(ch, p); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "创建默认造型失败: " + err.Error()})
			return
		}
	}
	// 严格去重：已有任务时优先找回现成图片，绝不自动重复提交 GPU 任务。
	if ch.PortraitTaskID != "" {
		current, err := s.Projects.RecoverCharacterPortrait(ch)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if current.Portrait != "" {
			c.JSON(http.StatusOK, gin.H{"ok": true, "recovered": true, "message": "已找回生成完成的标准像", "portrait": current.Portrait})
			return
		}
		if current.PortraitTaskID != "" {
			c.JSON(http.StatusConflict, gin.H{"error": "标准像任务仍在处理或等待认领，请先点击“检查生成结果”；确认无法恢复后再重置状态"})
			return
		}
		ch = current
	}
	if err := s.Projects.StartCharacterPortrait(ch); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true, "message": fmt.Sprintf("角色「%s」标准像生成中", ch.Name)})
}

func (s *Service) HandleRecoverCharacterPortrait(c *gin.Context) {
	ch, ok := s.loadCharacter(c)
	if !ok {
		return
	}
	current, err := s.Projects.RecoverCharacterPortrait(ch)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	var task *models.Task
	if current.PortraitTaskID != "" {
		var found models.Task
		if s.DB.Where("task_id = ?", current.PortraitTaskID).First(&found).Error == nil {
			task = &found
		}
	}
	c.JSON(200, gin.H{"character": current, "task": task})
}

func (s *Service) HandleResetCharacterPortrait(c *gin.Context) {
	ch, ok := s.loadCharacter(c)
	if !ok {
		return
	}
	if err := s.Projects.ResetCharacterPortraitTask(ch); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	s.Projects.PushProject(nil)
	c.JSON(200, gin.H{"ok": true})
}

// HandleUploadCharacterPortrait 上传角色照片作为标准像（替代文生图）
func (s *Service) HandleUploadCharacterPortrait(c *gin.Context) {
	ch, ok := s.loadCharacter(c)
	if !ok {
		return
	}
	if ch.ProfileStatus != models.ProfileStatusApproved {
		c.JSON(409, gin.H{"error": "请先审核通过角色档案，再上传标准像"})
		return
	}
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(400, gin.H{"error": "file required: " + err.Error()})
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if len(data) == 0 {
		c.JSON(400, gin.H{"error": "空文件"})
		return
	}
	ext := filepath.Ext(header.Filename)
	if ext == "" {
		ext = detectImageExt(data)
	}
	if ext == "" {
		ext = ".jpg"
	}
	name := fmt.Sprintf("char_%d_%d%s", ch.ID, time.Now().UnixNano(), ext)
	path, _, err := s.Upload.SaveFile(fmt.Sprintf("%d", ch.ProjectID), "image", name, data)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if ch.PortraitTaskID != "" && s.Tasks != nil {
		_ = s.Tasks.CancelTask(ch.PortraitTaskID)
	}
	if _, _, err := updateSelectedAsset(s.DB, ch.ProjectID, VariantCharacterPortrait, ch.ID, filepath.Base(path), ch.ReferencePrompt, "manual_upload", map[string]any{"portrait_task_id": "", "portrait_error": ""}, "", nil); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	s.Projects.PushProject(nil)
	c.JSON(200, gin.H{"ok": true, "message": "照片已设为角色标准像", "portrait": filepath.Base(path)})
}

func (s *Service) HandleGenerateCharacterSheet(c *gin.Context) {
	ch, ok := s.loadCharacter(c)
	if !ok {
		return
	}
	if err := s.Projects.StartCharacterSheet(ch); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errSheetActive) {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"ok": true, "message": fmt.Sprintf("角色「%s」四视图任务已提交", ch.Name)})
}

func (s *Service) HandleGenerateAllPortraits(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	n, err := s.Projects.GenerateAllPortraits(p)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true, "message": fmt.Sprintf("已提交 %d 个角色的标准像生成", n)})
}

// loadCharacter 从 :cid 加载角色（校验归属 :id 项目）
func (s *Service) loadCharacter(c *gin.Context) (*models.Character, bool) {
	projectID, err := strconv.Atoi(c.Param("id"))
	if err != nil || projectID <= 0 {
		c.JSON(400, gin.H{"error": "invalid project id"})
		return nil, false
	}
	cid, err := strconv.Atoi(c.Param("cid"))
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid character id"})
		return nil, false
	}
	var ch models.Character
	if err := s.DB.Where("id = ? AND project_id = ?", cid, projectID).First(&ch).Error; err != nil {
		c.JSON(404, gin.H{"error": "character not found"})
		return nil, false
	}
	return &ch, true
}

// ---------- 视觉资产（道具 / 场景） ----------

// assetKindOf 从 :kind 路由段解析资产类别
func assetKindOf(c *gin.Context) (string, bool) {
	kind, err := normalizeAssetKind(c.Param("kind"))
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return "", false
	}
	return kind, true
}

// loadAsset 从 :aid 加载资产（校验类别与归属 :id 项目）
func (s *Service) loadAsset(c *gin.Context) (*models.Asset, bool) {
	projectID, err := strconv.Atoi(c.Param("id"))
	if err != nil || projectID <= 0 {
		c.JSON(400, gin.H{"error": "invalid project id"})
		return nil, false
	}
	kind, ok := assetKindOf(c)
	if !ok {
		return nil, false
	}
	aid, err := strconv.Atoi(c.Param("aid"))
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid asset id"})
		return nil, false
	}
	var a models.Asset
	if err := s.DB.Where("id = ? AND project_id = ? AND kind = ?", aid, projectID, kind).First(&a).Error; err != nil {
		c.JSON(404, gin.H{"error": "asset not found"})
		return nil, false
	}
	return &a, true
}

func (s *Service) HandleListAssets(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	kind := ""
	if k := c.Param("kind"); k != "" {
		if kind, ok = assetKindOf(c); !ok {
			return
		}
	}
	list, err := s.Projects.ListAssets(p.ID, kind)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	counts, _ := s.Projects.AssetSceneCounts(p.ID)
	c.JSON(200, gin.H{"assets": list, "counts": counts})
}

func (s *Service) HandleRedesignAssetDescription(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	kind, ok := assetKindOf(c)
	if !ok {
		return
	}
	var req struct {
		Name  string `json:"name"`
		Brief string `json:"brief"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	description, err := s.Projects.RedesignAssetDescription(p, kind, req.Name, req.Brief)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"description": description})
}

func (s *Service) HandleCreateAsset(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	kind, ok := assetKindOf(c)
	if !ok {
		return
	}
	var req models.Asset
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	req.ID = 0
	req.ProjectID = p.ID
	req.Kind = kind
	a, err := s.Projects.CreateAsset(req)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, a)
}

func (s *Service) HandleUpdateAsset(c *gin.Context) {
	a, ok := s.loadAsset(c)
	if !ok {
		return
	}
	var req models.Asset
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	if err := s.Projects.UpdateAsset(a, req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	var updated models.Asset
	s.DB.First(&updated, a.ID)
	c.JSON(200, updated)
}

func (s *Service) HandleDeleteAsset(c *gin.Context) {
	a, ok := s.loadAsset(c)
	if !ok {
		return
	}
	if err := s.Projects.DeleteAsset(a.ProjectID, a.ID); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (s *Service) HandleGenerateAssetImage(c *gin.Context) {
	a, ok := s.loadAsset(c)
	if !ok {
		return
	}
	if err := s.Projects.StartAssetImage(a); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, errAssetImageActive) {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	engine, _ := normalizeAssetImageEngine(a.ImageEngine)
	label := "Krea2"
	if engine == ImageEngineQwen21 {
		label = "Qwen-Image-2.1"
	}
	c.JSON(http.StatusAccepted, gin.H{"ok": true, "message": fmt.Sprintf("%s「%s」%s 参考图任务已提交", AssetKindLabel(a.Kind), a.Name, label)})
}

func (s *Service) HandleGeneratePropSheet(c *gin.Context) {
	a, ok := s.loadAsset(c)
	if !ok {
		return
	}
	if err := s.Projects.StartPropSheet(a); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errSheetActive) {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"ok": true, "message": fmt.Sprintf("道具「%s」四视图任务已提交", a.Name)})
}

// HandleUploadAssetImage 上传图片作为资产参考图（替代文生图）
func (s *Service) HandleUploadAssetImage(c *gin.Context) {
	a, ok := s.loadAsset(c)
	if !ok {
		return
	}
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(400, gin.H{"error": "file required: " + err.Error()})
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if len(data) == 0 {
		c.JSON(400, gin.H{"error": "空文件"})
		return
	}
	ext := filepath.Ext(header.Filename)
	if ext == "" {
		ext = detectImageExt(data)
	}
	if ext == "" {
		ext = ".jpg"
	}
	name := fmt.Sprintf("%s_%d_%d%s", assetFilePrefix(a.Kind), a.ID, time.Now().UnixNano(), ext)
	path, _, err := s.Upload.SaveFile(fmt.Sprintf("%d", a.ProjectID), "image", name, data)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if _, _, err := updateSelectedAsset(s.DB, a.ProjectID, VariantAssetImage, a.ID, filepath.Base(path), a.Description, "manual_upload", map[string]any{
		"image_task_id": "", "image_error": "",
	}, "", nil); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	// 上传结果优先：取消旧生成任务，且同步器的 task-id 条件可阻止旧结果覆盖上传图片。
	if a.ImageTaskID != "" && s.Tasks != nil {
		if err := s.Tasks.CancelTask(a.ImageTaskID); err != nil && !strings.Contains(err.Error(), "已结束") {
			log.Printf("[asset %d] cancel replaced image task %s failed: %v", a.ID, a.ImageTaskID, err)
		}
	}
	s.Projects.PushProject(nil)
	c.JSON(200, gin.H{"ok": true, "message": "图片已设为参考图", "image": filepath.Base(path)})
}

// HandleGenerateAllAssetImages 一键生成该类别全部缺图资产
func (s *Service) HandleGenerateAllAssetImages(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	kind, ok := assetKindOf(c)
	if !ok {
		return
	}
	n, err := s.Projects.GenerateAllAssetImages(p, kind)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"ok": true, "message": fmt.Sprintf("已提交 %d 个%s的 Krea2 参考图任务", n, AssetKindLabel(kind))})
}

// ---------- 角色语音（预设音色 / 参考语音复刻） ----------

// audioMimeOf 音频扩展名 → MIME（音色复刻 data URI 用）
func audioMimeOf(ext string) string {
	switch strings.ToLower(strings.TrimPrefix(ext, ".")) {
	case "wav":
		return "audio/wav"
	case "m4a":
		return "audio/mp4"
	case "aac":
		return "audio/aac"
	default:
		return "audio/mpeg"
	}
}

// HandleUploadCharacterVoice 上传参考语音并注册复刻音色（10~20 秒清晰人声）。
// 注册失败时仍保留音频文件（voice_ref），可用 voice/clone 重试。
func (s *Service) HandleUploadCharacterVoice(c *gin.Context) {
	ch, ok := s.loadCharacter(c)
	if !ok {
		return
	}
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(400, gin.H{"error": "file required: " + err.Error()})
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if len(data) == 0 {
		c.JSON(400, gin.H{"error": "空文件"})
		return
	}
	if len(data) > 10*1024*1024 {
		c.JSON(400, gin.H{"error": "参考语音不能超过 10MB"})
		return
	}
	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext == "" {
		ext = ".mp3"
	}
	switch ext {
	case ".mp3", ".wav", ".m4a", ".aac":
	default:
		c.JSON(400, gin.H{"error": "仅支持 MP3/WAV/M4A/AAC 音频"})
		return
	}
	name := fmt.Sprintf("voice_%d_%d%s", ch.ID, time.Now().UnixNano(), ext)
	path, _, err := s.Upload.SaveFile(fmt.Sprintf("%d", ch.ProjectID), "audio", name, data)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	// 先落文件并清空旧复刻音色，再同步注册新音色
	if err := s.DB.Model(&models.Character{}).Where("id = ?", ch.ID).
		Updates(map[string]any{"voice_ref": filepath.Base(path), "voice_id": "", "voice_model": ""}).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	ali := NewAliyunTTS(s.DB)
	voiceID, cloneErr := ali.CloneVoice(base64.StdEncoding.EncodeToString(data), audioMimeOf(ext),
		fmt.Sprintf("p%dc%d", ch.ProjectID, ch.ID))
	if cloneErr == nil && voiceID != "" {
		if err := s.DB.Model(&models.Character{}).Where("id = ?", ch.ID).
			Updates(map[string]any{"voice_id": voiceID, "voice_model": QwenTTSVCModel}).Error; err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
	}
	_ = invalidateCharacterDialogueAudio(s.DB, ch.ProjectID, ch.Name, "角色音色配置已更新")
	s.Projects.PushProject(nil)
	resp := gin.H{"ok": true, "voice_ref": filepath.Base(path), "voice_id": ""}
	if cloneErr != nil {
		resp["warning"] = "参考语音已保存，但音色注册失败：" + cloneErr.Error() + "（配音时将回退到角色预设音色，可稍后重试注册）"
	} else {
		resp["voice_id"] = voiceID
		resp["message"] = "参考语音已上传并注册为角色复刻音色，该角色配音将保持音色一致"
	}
	c.JSON(200, resp)
}

// HandleCloneCharacterVoice 用已保存的参考语音重试注册复刻音色
func (s *Service) HandleCloneCharacterVoice(c *gin.Context) {
	ch, ok := s.loadCharacter(c)
	if !ok {
		return
	}
	if ch.VoiceRef == "" {
		c.JSON(400, gin.H{"error": "该角色尚未上传参考语音"})
		return
	}
	abs := filepath.Join(s.Upload.InputDir(), fmt.Sprintf("%d", ch.ProjectID), ch.VoiceRef)
	f, err := s.Remote.Open(abs)
	if err != nil {
		c.JSON(404, gin.H{"error": "参考语音文件不存在"})
		return
	}
	data, err := io.ReadAll(f)
	f.Close()
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	ali := NewAliyunTTS(s.DB)
	voiceID, cloneErr := ali.CloneVoice(base64.StdEncoding.EncodeToString(data),
		audioMimeOf(filepath.Ext(ch.VoiceRef)), fmt.Sprintf("p%dc%d", ch.ProjectID, ch.ID))
	if cloneErr != nil {
		c.JSON(500, gin.H{"error": "音色注册失败: " + cloneErr.Error()})
		return
	}
	if err := s.DB.Model(&models.Character{}).Where("id = ?", ch.ID).
		Updates(map[string]any{"voice_id": voiceID, "voice_model": QwenTTSVCModel}).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	_ = invalidateCharacterDialogueAudio(s.DB, ch.ProjectID, ch.Name, "角色复刻音色已更新")
	s.Projects.PushProject(nil)
	c.JSON(200, gin.H{"ok": true, "voice_id": voiceID, "message": "复刻音色注册成功"})
}

// HandleClearCharacterVoice 清除角色语音配置（预设音色与参考语音）
func (s *Service) HandleClearCharacterVoice(c *gin.Context) {
	ch, ok := s.loadCharacter(c)
	if !ok {
		return
	}
	if ch.VoiceRef != "" && s.Remote != nil && s.Upload != nil {
		path := filepath.Join(s.Upload.InputDir(), fmt.Sprintf("%d", ch.ProjectID), ch.VoiceRef)
		if _, err := s.Remote.Run("rm -f -- " + shellQuote(path)); err != nil {
			log.Printf("[character %d] cleanup voice %s failed: %v", ch.ID, path, err)
		}
	}
	if err := s.DB.Model(&models.Character{}).Where("id = ?", ch.ID).
		Updates(map[string]any{"voice": "", "voice_ref": "", "voice_id": "", "voice_model": ""}).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	_ = invalidateCharacterDialogueAudio(s.DB, ch.ProjectID, ch.Name, "角色音色配置已清除")
	s.Projects.PushProject(nil)
	c.JSON(200, gin.H{"ok": true, "message": "角色语音配置已清除"})
}

// ---------- 对白配音与字幕 ----------

// HandleEditorData 剪辑台数据：该集场景（含视频时长/顺序）+ 全部对白 + 字幕时间轴
func (s *Service) HandleEditorData(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	episodeN, _ := strconv.Atoi(c.DefaultQuery("episode_n", "1"))
	if episodeN <= 0 {
		episodeN = 1
	}
	data, err := s.Projects.EditorData(p, episodeN)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, data)
}

// HandleUpdateDialogue 编辑单条对白（文本/音色），编辑后需重新合成配音
func (s *Service) HandleUpdateDialogue(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	did, err := strconv.Atoi(c.Param("did"))
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid dialogue id"})
		return
	}
	var req DialogueUpdateInput
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	d, err := s.Projects.UpdateDialogueFields(p, uint(did), req)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, d)
}

// HandleRedubDialogue 重新合成单条对白配音
func (s *Service) HandleRedubDialogue(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	did, err := strconv.Atoi(c.Param("did"))
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid dialogue id"})
		return
	}
	var d models.Dialogue
	if err := s.DB.Where("id = ? AND project_id = ?", did, p.ID).First(&d).Error; err != nil {
		c.JSON(404, gin.H{"error": "对白不存在"})
		return
	}
	if err := s.Projects.StartDialogueTTS(&d); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true, "message": "配音重新合成已提交"})
}

// HandleReorderScenes 调整场景顺序（时间轴拖拽排序）
func (s *Service) HandleReorderScenes(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	var req struct {
		SceneIDs []uint `json:"scene_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	if err := s.Projects.ReorderScenes(p, req.SceneIDs); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

// HandleUpdateSceneDuration 调整场景目标时长（秒，影响时间轴与合并）
func (s *Service) HandleUpdateSceneDuration(c *gin.Context) {
	sc, ok := s.loadScene(c)
	if !ok {
		return
	}
	var req struct {
		Duration float64 `json:"duration"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	if req.Duration < 3 || req.Duration > 15 {
		c.JSON(400, gin.H{"error": "时长需在 3~15 秒之间"})
		return
	}
	if err := s.DB.Model(&models.Scene{}).Where("id = ?", sc.ID).Update("duration", req.Duration).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true, "duration": req.Duration})
}

func (s *Service) HandleListSceneDialogues(c *gin.Context) {
	sc, ok := s.loadScene(c)
	if !ok {
		return
	}
	list, err := s.Projects.ListSceneDialogues(sc.ID)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, list)
}

func (s *Service) HandleGenerateSceneDub(c *gin.Context) {
	sc, ok := s.loadScene(c)
	if !ok {
		return
	}
	n, err := s.Projects.GenerateSceneDubs(sc)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true, "message": fmt.Sprintf("已提交 %d 条对白配音", n)})
}

func (s *Service) HandleGenerateProjectDub(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	n, err := s.Projects.GenerateProjectDubs(p)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true, "message": fmt.Sprintf("已提交 %d 条对白配音", n)})
}

func episodeNumberParam(c *gin.Context) (int, bool) {
	n, err := strconv.Atoi(c.Param("number"))
	if err != nil || n <= 0 {
		c.JSON(400, gin.H{"error": "invalid episode number"})
		return 0, false
	}
	return n, true
}

// HandleGenerateEpisodeDub submits stale dialogue only unless stale_only=false is explicit.
func (s *Service) HandleGenerateEpisodeDub(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	number, ok := episodeNumberParam(c)
	if !ok {
		return
	}
	staleOnly := c.DefaultQuery("stale_only", "true") != "false"
	n, err := s.Projects.GenerateEpisodeDubs(p, number, staleOnly)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(202, gin.H{"ok": true, "count": n, "stale_only": staleOnly, "message": fmt.Sprintf("已提交 %d 条过期对白配音", n)})
}

func (s *Service) HandleEpisodeDubPreview(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	number, ok := episodeNumberParam(c)
	if !ok {
		return
	}
	data, err := s.Projects.EpisodeDubPreview(p, number)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, data)
}

func dialogueIDParam(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("did"), 10, 32)
	if err != nil || id == 0 {
		c.JSON(400, gin.H{"error": "invalid dialogue id"})
		return 0, false
	}
	return uint(id), true
}

func (s *Service) HandleApplyDialoguePreview(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	did, ok := dialogueIDParam(c)
	if !ok {
		return
	}
	d, err := s.Projects.ApplyDialoguePreview(p, did)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, d)
}

func (s *Service) HandleRevertDialogueAudio(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	did, ok := dialogueIDParam(c)
	if !ok {
		return
	}
	d, err := s.Projects.RevertDialogueAudio(p, did)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, d)
}

// HandleEpisodeSRT 生成并下载该集 SRT 字幕（含 UTF-8 BOM 兼容 Windows 播放器）
func (s *Service) HandleEpisodeSRT(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	episodeN, _ := strconv.Atoi(c.DefaultQuery("episode_n", "1"))
	if episodeN <= 0 {
		episodeN = 1
	}
	srt, count := s.Projects.EpisodeSRT(p, episodeN)
	if count == 0 {
		c.JSON(400, gin.H{"error": "该集暂无对白字幕（需先生成含对白的剧本）"})
		return
	}
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="episode_%d.srt"`, episodeN))
	c.Data(200, "text/plain; charset=utf-8", []byte(srt))
}

// ---------- 素材库 ----------

// HandleListMaterials 素材列表（?type=image|video|audio&project_id=N）
func (s *Service) HandleListMaterials(c *gin.Context) {
	ftype := c.Query("type")
	var projectID *uint
	if v := c.Query("project_id"); v != "" {
		id, err := strconv.ParseUint(v, 10, 32)
		if err == nil {
			pid := uint(id)
			projectID = &pid
		}
	}
	list, err := s.Materials.List(ftype, projectID)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, list)
}

// HandleUploadMaterial 上传素材到素材库 (multipart: file/type/project_id)
func (s *Service) HandleUploadMaterial(c *gin.Context) {
	ftype := c.PostForm("type")
	if ftype == "" {
		ftype = "image"
	}
	var projectID *uint
	if v := c.PostForm("project_id"); v != "" {
		if id, err := strconv.ParseUint(v, 10, 32); err == nil {
			pid := uint(id)
			projectID = &pid
		}
	}
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(400, gin.H{"error": "file required"})
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	mat, err := s.Materials.SaveUpload(projectID, ftype, header.Filename, data)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, mat)
}

// HandleDeleteMaterial 删除素材
func (s *Service) HandleDeleteMaterial(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid id"})
		return
	}
	if err := s.Materials.Delete(uint(id)); err != nil {
		if errors.Is(err, ErrMaterialReferenced) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

// HandleInputFile 场景首帧图预览：/api/input/:taskid/*path → ComfyUI input 目录
func (s *Service) HandleInputFile(c *gin.Context) {
	taskID := c.Param("taskid")
	path := c.Param("path")
	if strings.Contains(path, "..") || strings.Contains(taskID, "..") {
		c.JSON(400, gin.H{"error": "invalid path"})
		return
	}
	full := s.Upload.InputDir() + "/" + taskID + "/" + path
	handleRemoteFile(c, s.Remote, full)
}

// loadProject 从 :id 加载项目
func (s *Service) loadProject(c *gin.Context) (*models.Project, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid id"})
		return nil, false
	}
	var p models.Project
	if err := s.DB.First(&p, id).Error; err != nil {
		c.JSON(404, gin.H{"error": "project not found"})
		return nil, false
	}
	return &p, true
}

// loadScene 从 :sid 加载场景
func (s *Service) loadScene(c *gin.Context) (*models.Scene, bool) {
	projectID, err := strconv.Atoi(c.Param("id"))
	if err != nil || projectID <= 0 {
		c.JSON(400, gin.H{"error": "invalid project id"})
		return nil, false
	}
	id, err := strconv.Atoi(c.Param("sid"))
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid scene id"})
		return nil, false
	}
	var sc models.Scene
	if err := s.DB.Where("id = ? AND project_id = ?", id, projectID).First(&sc).Error; err != nil {
		c.JSON(404, gin.H{"error": "scene not found"})
		return nil, false
	}
	return &sc, true
}

// handleRemoteFile 通用远程文件响应（校验存在 + MIME）
// 容器已挂载共享目录时优先本地直读（避免 SSH/SFTP 回连宿主机），并支持 ETag/304 缓存。
func handleRemoteFile(c *gin.Context, remote *RemoteExec, full string) {
	// 本地直读：容器挂载共享目录时文件即为本地路径，避免 SSH 回连
	if fi, err := os.Stat(full); err == nil && fi.Mode().IsRegular() {
		serveLocalFile(c, full, fi)
		return
	}
	size, err := remote.Size(full)
	if err != nil {
		c.JSON(404, gin.H{"error": "file not found"})
		return
	}
	f, err := remote.Open(full)
	if err != nil {
		c.JSON(404, gin.H{"error": "file not found"})
		return
	}
	defer f.Close()
	c.Header("Content-Type", mimeTypeOf(full))
	c.Header("Content-Length", strconv.FormatInt(size, 10))
	c.Status(http.StatusOK)
	buf := make([]byte, 64*1024)
	for {
		n, rerr := f.Read(buf)
		if n > 0 {
			if _, werr := c.Writer.Write(buf[:n]); werr != nil {
				return
			}
		}
		if rerr != nil {
			return
		}
	}
}

// serveLocalFile 本地文件响应：ETag/304 缓存 + Range 支持
func serveLocalFile(c *gin.Context, full string, fi os.FileInfo) {
	etag := fmt.Sprintf("\"%x-%x\"", fi.Size(), fi.ModTime().UnixNano())
	c.Header("ETag", etag)
	c.Header("Cache-Control", "public, max-age=3600")
	if inm := c.GetHeader("If-None-Match"); inm != "" && inm == etag {
		c.Status(http.StatusNotModified)
		return
	}
	c.Header("Content-Type", mimeTypeOf(full))
	c.Header("Accept-Ranges", "bytes")
	size := fi.Size()
	if rng := c.GetHeader("Range"); rng != "" {
		start, end, ok := parseRange(rng, size)
		if !ok {
			c.Status(http.StatusRequestedRangeNotSatisfiable)
			c.Header("Content-Range", fmt.Sprintf("bytes */%d", size))
			return
		}
		f, err := os.Open(full)
		if err != nil {
			c.JSON(404, gin.H{"error": "file not found"})
			return
		}
		defer f.Close()
		if _, err := f.Seek(start, io.SeekStart); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		c.Status(http.StatusPartialContent)
		c.Header("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, size))
		c.Header("Content-Length", strconv.FormatInt(end-start+1, 10))
		_, _ = io.CopyN(c.Writer, f, end-start+1)
		return
	}
	f, err := os.Open(full)
	if err != nil {
		c.JSON(404, gin.H{"error": "file not found"})
		return
	}
	defer f.Close()
	c.Header("Content-Length", strconv.FormatInt(size, 10))
	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, f)
}

// ---------- 角色档案（LumxAI 风格结构化角色提示词 + 审核工作流） ----------

// HandleGenerateCharacterProfile 使用 LLM 为角色生成完整的结构化档案
func (s *Service) HandleGenerateCharacterProfile(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	ch, ok := s.loadCharacter(c)
	if !ok {
		return
	}

	if err := s.CharacterProfiles.GenerateProfile(ch, p, p.Plan); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	// 刷新角色数据
	var updated models.Character
	s.DB.First(&updated, ch.ID)
	s.Projects.PushProject(nil)
	c.JSON(200, gin.H{
		"character":      updated,
		"profile_fields": s.CharacterProfiles.GetProfileFields(&updated),
		"message":        fmt.Sprintf("角色「%s」档案已生成", ch.Name),
	})
}

// HandleGenerateReferencePrompt 单独生成角色的参考像提示词
func (s *Service) HandleGenerateReferencePrompt(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	ch, ok := s.loadCharacter(c)
	if !ok {
		return
	}

	prompt, err := s.CharacterProfiles.GenerateReferencePrompt(ch, p)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if s.CharacterLooks != nil {
		if _, err := s.CharacterLooks.EnsureDefaultLook(ch, p); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "创建默认造型失败: " + err.Error()})
			return
		}
	}

	// 刷新角色数据
	var updated models.Character
	s.DB.First(&updated, ch.ID)
	s.Projects.PushProject(nil)
	c.JSON(200, gin.H{
		"character": updated,
		"prompt":    prompt,
		"message":   fmt.Sprintf("角色「%s」参考像提示词已生成", ch.Name),
	})
}

// HandleUpdateCharacterProfile 手动更新角色档案字段
func (s *Service) HandleUpdateCharacterProfile(c *gin.Context) {
	ch, ok := s.loadCharacter(c)
	if !ok {
		return
	}
	var req models.Character
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}

	if err := s.CharacterProfiles.UpdateProfile(ch, req); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	var updated models.Character
	s.DB.First(&updated, ch.ID)
	s.Projects.PushProject(nil)
	c.JSON(200, gin.H{
		"character":      updated,
		"profile_fields": s.CharacterProfiles.GetProfileFields(&updated),
	})
}

// HandleApproveCharacterProfile 审核通过角色档案
func (s *Service) HandleApproveCharacterProfile(c *gin.Context) {
	ch, ok := s.loadCharacter(c)
	if !ok {
		return
	}
	var req struct {
		Note string `json:"note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		req.Note = ""
	}

	if err := s.CharacterProfiles.ApproveProfile(ch, req.Note); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}

	var updated models.Character
	s.DB.First(&updated, ch.ID)
	s.Projects.PushProject(nil)
	c.JSON(200, gin.H{
		"character": updated,
		"message":   fmt.Sprintf("角色「%s」档案已审核通过", ch.Name),
	})
}

// HandleRejectCharacterProfile 驳回角色档案
func (s *Service) HandleRejectCharacterProfile(c *gin.Context) {
	ch, ok := s.loadCharacter(c)
	if !ok {
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "请提供驳回原因"})
		return
	}
	if strings.TrimSpace(req.Reason) == "" {
		c.JSON(400, gin.H{"error": "驳回原因不能为空"})
		return
	}

	if err := s.CharacterProfiles.RejectProfile(ch, req.Reason); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	var updated models.Character
	s.DB.First(&updated, ch.ID)
	s.Projects.PushProject(nil)
	c.JSON(200, gin.H{
		"character": updated,
		"message":   fmt.Sprintf("角色「%s」档案已驳回，请根据意见修改后重新提交审核", ch.Name),
	})
}

// HandleResetCharacterProfile 将角色档案重置为草稿状态
func (s *Service) HandleResetCharacterProfile(c *gin.Context) {
	ch, ok := s.loadCharacter(c)
	if !ok {
		return
	}

	if err := s.CharacterProfiles.ResetToDraft(ch); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	var updated models.Character
	s.DB.First(&updated, ch.ID)
	s.Projects.PushProject(nil)
	c.JSON(200, gin.H{
		"character": updated,
		"message":   fmt.Sprintf("角色「%s」档案已重置为草稿状态", ch.Name),
	})
}

var _ = gorm.ErrRecordNotFound
