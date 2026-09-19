package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
)

// ProjectService 漫剧项目：剧本（文生文）→ 分镜画面（文生图）→ 视频（本地 L40）→ 合并成片
type ProjectService struct {
	cfg            *config.Config
	db             *gorm.DB
	textProvider   TextProvider // 文生文 provider（支持火山 / llama.cpp 等）
	volc           *VolcClient  // 保留用于图片生成
	ali            *AliyunTTS
	tasks          *TaskService
	remote         *RemoteExec
	upload         *UploadManager
	hub            *Hub
	stopped        chan struct{}
	materials      *MaterialService
	skills         *SkillService
	characterLooks *CharacterLookService
	continuity     *ContinuityService
	assetMu        sync.Mutex // 串行化资产参考图任务创建，避免并发重复排队
}

func NewProjectService(cfg *config.Config, db *gorm.DB, textProvider TextProvider, volc *VolcClient, tasks *TaskService, remote *RemoteExec, upload *UploadManager, hub *Hub, materials *MaterialService) *ProjectService {
	return &ProjectService{
		cfg: cfg, db: db, textProvider: textProvider, volc: volc, tasks: tasks,
		remote: remote, upload: upload, hub: hub,
		ali:       NewAliyunTTS(db),
		stopped:   make(chan struct{}),
		materials: materials,
	}
}

func (s *ProjectService) chatWithSkill(projectID uint, stage, system, user string, params map[string]string) (string, error) {
	if s.skills == nil {
		return s.textProvider.Chat(system, user)
	}
	return s.skills.ChatWithSkill(projectID, stage, s.textProvider, system, user, params)
}

func (s *ProjectService) applyPromptSkill(projectID uint, stage, prompt string, params map[string]string) string {
	if s.skills == nil {
		return prompt
	}
	system, addition, skill, err := s.skills.ApplyPrompt(projectID, stage, "", "", params)
	if err != nil || skill == nil {
		return prompt
	}
	extra := strings.TrimSpace(strings.Join([]string{strings.TrimSpace(system), strings.TrimSpace(addition)}, "\n"))
	if extra == "" {
		return prompt
	}
	_ = s.skills.LogSkillUsage(projectID, stage, skill, prompt, len(prompt)+len(extra), 0, true, "")
	return prompt + "\n\n" + extra
}

// ---------- 项目 ----------

// CreateProject 创建项目（仅记录创意，剧本待生成）
func (s *ProjectService) CreateProject(p models.Project) (*models.Project, error) {
	if p.Title == "" {
		p.Title = "未命名项目"
	}
	if p.Status == "" {
		p.Status = "draft"
	}
	if p.AspectRatio == "" {
		p.AspectRatio = "16:9"
	}
	if p.SourceType == "" {
		p.SourceType = models.ProjectSourceOutline // 默认梗概项目
	}
	if err := s.db.Create(&p).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *ProjectService) GetProject(id uint) (*models.Project, []models.Scene, error) {
	var p models.Project
	if err := s.db.First(&p, id).Error; err != nil {
		return nil, nil, err
	}
	var scenes []models.Scene
	s.db.Where("project_id = ?", id).Order("`order`").Find(&scenes)
	return &p, scenes, nil
}

func (s *ProjectService) ListProjects() ([]models.Project, error) {
	var list []models.Project
	s.db.Order("id desc").Find(&list)
	return list, nil
}

// StartPipeline 幂等启动完整生成流水线。实际工作在后台执行，状态持久化后可在重启时恢复。
// stopAfterScript=true 时（创建项目自动触发），流水线生成完创作方案+角色+第一集剧本后停止，后续由人工处理。
func (s *ProjectService) StartPipeline(p *models.Project, episodeN int, stopAfterScript bool) (*models.Project, error) {
	current, err := s.claimPipeline(p, episodeN, stopAfterScript)
	if err != nil {
		return nil, err
	}
	go s.advancePipeline(current.ID)
	return current, nil
}

// claimPipeline 原子认领并持久化本次流水线目标，独立于后台执行，便于重启恢复。
func (s *ProjectService) claimPipeline(p *models.Project, episodeN int, stopAfterScript bool) (*models.Project, error) {
	if episodeN <= 0 {
		episodeN = 1
	}
	startStage := "plan"
	if strings.TrimSpace(p.Plan) != "" {
		startStage = "script"
	}
	active := []string{"plan", "plan_running", "script", "script_running", "images", "videos", "merge"}
	res := s.db.Model(&models.Project{}).Where("id = ? AND COALESCE(pipeline_stage, '') NOT IN ?", p.ID, active).
		Updates(map[string]any{
			"pipeline_stage": startStage, "pipeline_episode": episodeN,
			"auto_generate": true, "stop_after_script": stopAfterScript,
			"status": "producing", "error": "",
		})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, fmt.Errorf("项目生成流程正在进行中")
	}
	var current models.Project
	if err := s.db.First(&current, p.ID).Error; err != nil {
		return nil, err
	}
	return &current, nil
}

// ClaimManualScript 防止手工剧本生成被重复提交。
func (s *ProjectService) ClaimManualScript(p *models.Project) error {
	active := []string{"plan", "plan_running", "script", "script_running", "script_manual", "images", "videos", "merge"}
	res := s.db.Model(&models.Project{}).Where("id = ? AND COALESCE(pipeline_stage, '') NOT IN ?", p.ID, active).
		Updates(map[string]any{"pipeline_stage": "script_manual", "auto_generate": false, "status": "producing", "error": ""})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("项目已有生成任务正在进行")
	}
	return s.db.First(p, p.ID).Error
}

func (s *ProjectService) cancelProjectTasks(projectID uint) {
	if s.tasks == nil {
		return
	}
	var scenes []models.Scene
	if err := s.db.Where("project_id = ?", projectID).Find(&scenes).Error; err != nil {
		return
	}
	for _, sc := range scenes {
		if sc.VideoTaskID != "" {
			if err := s.tasks.CancelTask(sc.VideoTaskID); err != nil && !strings.Contains(err.Error(), "已结束") {
				log.Printf("[project %d] cancel task %s failed: %v", projectID, sc.VideoTaskID, err)
			}
		}
	}
}

func (s *ProjectService) cleanupStaleSceneFiles(projectID uint, scenes []models.Scene) {
	if s.cfg == nil || s.remote == nil || s.upload == nil {
		return
	}
	for _, sc := range scenes {
		paths := make([]string, 0, 2)
		if sc.ImageFile != "" {
			paths = append(paths, filepath.Join(s.upload.InputDir(), fmt.Sprintf("%d", projectID), filepath.Base(sc.ImageFile)))
		}
		if sc.VideoFile != "" && sc.VideoGPU != nil {
			paths = append(paths, filepath.Join(s.cfg.Comfy.ComfyDir, "output_workers", fmt.Sprintf("gpu%d", *sc.VideoGPU), sc.VideoFile))
		}
		for _, path := range paths {
			if _, err := s.remote.Run("rm -f -- " + shellQuote(path)); err != nil {
				log.Printf("[project %d] cleanup stale file %s failed: %v", projectID, path, err)
			}
		}
	}
}

func (s *ProjectService) DeleteProject(id uint) error {
	s.cancelProjectTasks(id)
	var scenesForCleanup []models.Scene
	var mergesForCleanup []models.MergeTask
	s.db.Where("project_id = ?", id).Find(&scenesForCleanup)
	s.db.Where("project_id = ?", id).Find(&mergesForCleanup)
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		var scenes []models.Scene
		if err := tx.Where("project_id = ?", id).Find(&scenes).Error; err != nil {
			return err
		}
		sceneIDs := make([]uint, 0, len(scenes))
		for _, sc := range scenes {
			sceneIDs = append(sceneIDs, sc.ID)
			if sc.VideoTaskID != "" {
				tx.Where("task_id = ?", sc.VideoTaskID).Delete(&models.Event{})
				tx.Where("task_id = ?", sc.VideoTaskID).Delete(&models.Task{})
			}
		}
		if len(sceneIDs) > 0 {
			if err := tx.Where("scene_id IN ? OR source_scene_id IN ?", sceneIDs, sceneIDs).Delete(&models.SceneContinuity{}).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("project_id = ?", id).Delete(&models.FrameCandidate{}).Error; err != nil {
			return err
		}
		if err := tx.Where("project_id = ?", id).Delete(&models.SharedAssetReference{}).Error; err != nil {
			return err
		}
		if err := tx.Where("project_id = ?", id).Delete(&models.Material{}).Error; err != nil {
			return err
		}
		if err := tx.Where("task_id = ?", fmt.Sprintf("%d", id)).Delete(&models.UploadFile{}).Error; err != nil {
			return err
		}
		if err := tx.Where("project_id = ?", id).Delete(&models.Scene{}).Error; err != nil {
			return err
		}
		if err := tx.Where("project_id = ?", id).Delete(&models.Character{}).Error; err != nil {
			return err
		}
		if err := tx.Where("project_id = ?", id).Delete(&models.Asset{}).Error; err != nil {
			return err
		}
		if err := tx.Where("project_id = ?", id).Delete(&models.Dialogue{}).Error; err != nil {
			return err
		}
		if err := tx.Where("project_id = ?", id).Delete(&models.AudioLayer{}).Error; err != nil {
			return err
		}
		if err := tx.Where("project_id = ?", id).Delete(&models.MergeTask{}).Error; err != nil {
			return err
		}
		if err := tx.Where("project_id = ?", id).Delete(&models.ProjectSkillConfig{}).Error; err != nil {
			return err
		}
		if err := tx.Where("project_id = ?", id).Delete(&models.SkillAuditLog{}).Error; err != nil {
			return err
		}
		if err := tx.Where("project_id = ?", id).Delete(&models.ChapterTask{}).Error; err != nil {
			return err
		}
		if err := tx.Where("project_id = ?", id).Delete(&models.Chapter{}).Error; err != nil {
			return err
		}
		for _, model := range []any{&models.StoryArc{}, &models.StoryBible{}, &models.CharacterAliasCandidate{}, &models.NovelJob{}} {
			if err := tx.Where("project_id = ?", id).Delete(model).Error; err != nil {
				return err
			}
		}
		return tx.Delete(&models.Project{}, id).Error
	}); err != nil {
		return err
	}
	// 数据库提交后再清理远程产物；路径仅由数字项目 ID 和数据库记录组成并经过 shell 转义。
	if s.cfg != nil && s.remote != nil && s.upload != nil {
		paths := []string{filepath.Join(s.upload.InputDir(), fmt.Sprintf("%d", id))}
		for _, sc := range scenesForCleanup {
			if sc.VideoFile != "" && sc.VideoGPU != nil {
				paths = append(paths, filepath.Join(s.cfg.Comfy.ComfyDir, "output_workers", fmt.Sprintf("gpu%d", *sc.VideoGPU), sc.VideoFile))
			}
		}
		for _, mt := range mergesForCleanup {
			if mt.OutputFile != "" {
				paths = append(paths, filepath.Join(s.cfg.Comfy.ComfyDir, "output_workers", "gpu0", mt.OutputFile))
			}
		}
		for _, path := range paths {
			if _, err := s.remote.Run("rm -rf -- " + shellQuote(path)); err != nil {
				log.Printf("[project %d] cleanup %s failed: %v", id, path, err)
			}
		}
	}
	return nil
}

// UpdateProject 编辑项目信息（标题/题材/画风/创意）
func (s *ProjectService) UpdateProject(p *models.Project, req models.Project) error {
	updates := map[string]any{"genre": req.Genre, "style": req.Style}
	if req.Title != "" {
		updates["title"] = req.Title
	}
	if req.Genre != "" {
		updates["genre"] = req.Genre
	}
	if req.Style != "" {
		updates["style"] = req.Style
	}
	if req.Synopsis != "" {
		updates["synopsis"] = req.Synopsis
	}
	if req.Audience != "" {
		updates["audience"] = req.Audience
	}
	if req.Tone != "" {
		updates["tone"] = req.Tone
	}
	if req.Ending != "" {
		updates["ending"] = req.Ending
	}
	if req.Episodes > 0 {
		updates["episodes"] = req.Episodes
	}
	if req.AspectRatio != "" {
		updates["aspect_ratio"] = req.AspectRatio
	}
	if len(updates) == 0 {
		return nil
	}
	return s.db.Model(p).Updates(updates).Error
}

var (
	storyboardDialogueTagPattern = regexp.MustCompile(`(?is)<d>.*?</d>`)
	storyboardQuotedTextPattern  = regexp.MustCompile(`[“\"][^”\"]*[”\"]`)
)

func sanitizeStoryboardAIDetail(detail string) string {
	text := storyboardDialogueTagPattern.ReplaceAllString(detail, "")
	text = storyboardQuotedTextPattern.ReplaceAllString(text, "")
	for _, residue := range []string{"正说出那句：", "正说出那句", "正在说出：", "正在说出", "说出台词：", "说出台词", "对白内容：", "对白内容"} {
		text = strings.ReplaceAll(text, residue, "")
	}
	return strings.TrimSpace(strings.ReplaceAll(text, "。。", "。"))
}

// RedesignSceneImagePrompt asks the configured text provider to rebuild a production-ready
// image prompt from authoritative project, character, location and prop data.
func (s *ProjectService) RedesignSceneImagePrompt(sc *models.Scene) (string, error) {
	if s.textProvider == nil {
		return "", fmt.Errorf("文本生成服务未配置")
	}
	var project models.Project
	if err := s.db.First(&project, sc.ProjectID).Error; err != nil {
		return "", err
	}
	assetContext := s.assetContextForScene(sc)
	shotContext := s.sceneShotContext(sc)
	_, referenceLines, explicitReferences := s.selectedSceneReferenceFiles(sc, "krea2")
	if !explicitReferences {
		_, referenceLines = s.sceneImageReferenceFiles(sc)
	}
	referenceContext := "无可用参考图"
	if len(referenceLines) > 0 {
		referenceContext = strings.Join(referenceLines, "\n")
	}
	framingRule := ""
	if len(parseSceneCharacters(sc.Characters)) > 0 {
		framingRule = "\n本场有人物：优先中景、中近景或近景，保证主要人物面部清晰并占据足够像素；避免远景、大远景和人物在画面中过小。只有剧情必须交代宏大空间时才可用全景，但人物脸部仍须清晰可辨。"
	}
	system := `你是 MiniMax H3 SelfLift 分镜图提示词编辑。本任务只设计一个静止分镜画面。
Subject与Picture的真实身份绑定由系统根据实际上传文件生成，你不得生成或改写subject_definitions、summary、retention_analysis、overall_soundscape或non_diegetic_music。
你只输出一段以“[Shot 1]”开头的detailed_description正文：
1. 人物必须使用场景资料中的真实角色名，禁止自行猜测或输出任何<Subject N>编号；系统会在返回后按实际图片顺序确定性转换。
2. 只描述一个明确静止瞬间中的主体位置、动作定格、可见表情、环境、景别、静态机位、构图与光线。
3. 这是静态分镜图：禁止写对白原文、引号台词、<d>标签、“正在说出某句”等语言内容；如剧情含对白，只能描述可见的嘴部状态和表情。
4. 不得描述连续动作、时间推进、推拉摇移跟升降或切镜。
5. 人物身份与造型、场景和道具由Picture及权威资料控制，不得新增或猜测资料中没有的内容。
6. 不得写“素材名”“主体信息”等占位词。只输出正文，不要标题、解释、Markdown或JSON。` + framingRule
	user := fmt.Sprintf("项目：%s\n题材：%s\n画风：%s\n场景标题：%s\n场景剧情：%s\n地点：%s\n道具：%s\n\n本次实际提交的参考图（编号与上传顺序一致）：\n%s\n\n当前镜头设计：\n%s\n\n场景与道具资料：\n%s",
		project.Title, project.Genre, project.Style, sc.Title, sc.Content,
		sc.LocationName, sc.Props, referenceContext, shotContext, assetContext)
	policy, err := NewPromptPolicyService(s.db).Resolve(PromptPolicyContext{ProjectID: sc.ProjectID, SceneID: &sc.ID}, PromptPolicyImagePolish, system)
	if err != nil {
		return "", fmt.Errorf("解析画面提示词策略失败: %w", err)
	}
	output, err := s.textProvider.Chat(policy.Content, user)
	if err != nil {
		return "", fmt.Errorf("AI 重新设计场景提示词失败: %w", err)
	}
	output = strings.TrimSpace(output)
	output = strings.TrimPrefix(output, "```json")
	output = strings.TrimPrefix(output, "```")
	output = strings.TrimSuffix(output, "```")
	output = sanitizeH3ReferencePrompt(strings.TrimSpace(output))
	// 兼容模型仍返回旧六段格式：只采纳镜头正文，真实引用绑定由代码生成。
	if detail := h3PromptSection(output, "detailed_description:"); detail != "" {
		output = detail
	}
	output = strings.TrimSpace(output)
	if !strings.HasPrefix(output, "[Shot 1]") {
		return "", fmt.Errorf("AI 返回的场景提示词必须以 [Shot 1] 开头，请重试")
	}
	if strings.Contains(output, "<Subject ") {
		return "", fmt.Errorf("AI 错误地自行填写了 Subject 编号，请重试；人物必须使用真实角色名")
	}
	output = sanitizeStoryboardAIDetail(output)
	output = useSubjectTags(output, referenceLines)
	for _, placeholder := range []string{"素材名", "已提供的主体信息", "主体信息"} {
		if strings.Contains(output, placeholder) {
			return "", fmt.Errorf("AI 返回内容仍含无意义占位词“%s”，请重试", placeholder)
		}
	}
	if len([]rune(output)) < 20 {
		return "", fmt.Errorf("AI 返回的场景提示词过短，请重试")
	}
	preview := *sc
	preview.ImagePrompt = output
	result := buildH3StoryboardPrompt(&preview, &project, referenceLines)
	for _, placeholder := range []string{"素材名", "已提供的主体信息"} {
		if strings.Contains(result, placeholder) {
			return "", fmt.Errorf("生成的引用绑定仍含无意义占位词“%s”", placeholder)
		}
	}
	return result, nil
}

// ReplaceSceneImage 以人工上传图片替换分镜图，并使所有基于旧图的视频状态失效。
func (s *ProjectService) ReplaceSceneImage(sc *models.Scene, imageFile string) error {
	if sc == nil || sc.ID == 0 || strings.TrimSpace(imageFile) == "" {
		return fmt.Errorf("场景和分镜图不能为空")
	}
	oldImageTaskID, oldVideoTaskID := sc.ImageTaskID, sc.VideoTaskID
	updates := map[string]any{
		"image_file":            filepath.Base(imageFile),
		"image_token":           "",
		"image_task_id":         "",
		"video_task_id":         "",
		"video_file":            "",
		"video_input_file":      "",
		"video_gpu":             nil,
		"video_full_prompt":     "",
		"video_first_frame_img": "",
		"video_last_frame_img":  "",
		"video_retries":         0,
		"status":                "image_ready",
		"error":                 "",
	}
	if err := s.db.Model(&models.Scene{}).Where("id = ? AND project_id = ?", sc.ID, sc.ProjectID).Updates(updates).Error; err != nil {
		return err
	}
	if s.tasks != nil {
		for _, taskID := range []string{oldImageTaskID, oldVideoTaskID} {
			if taskID == "" {
				continue
			}
			if err := s.tasks.CancelTask(taskID); err != nil && !strings.Contains(err.Error(), "已结束") {
				log.Printf("[scene %d] cancel stale task %s failed: %v", sc.ID, taskID, err)
			}
		}
	}
	if s.continuity != nil {
		s.continuity.InvalidateDependents(sc.ID, "来源分镜图已替换，请重新生成视频并选择衔接帧")
	}
	s.updateProjectStatus(sc.ProjectID)
	s.pushProject(nil)
	return nil
}

// UpdateScene 编辑场景文案。修改 image_prompt 会清空已生成画面与视频（需重新生成）；
// 仅修改 content/title 时保留画面、清空已生成视频（需重新生成视频）。
func (s *ProjectService) UpdateScene(sc *models.Scene, title, content, imagePrompt string, duration float64, visual ...string) error {
	updates := map[string]any{}
	durationChanged := duration > 0 && sc.Duration != duration
	if duration > 0 {
		updates["duration"] = duration
	}
	if title != "" {
		updates["title"] = title
	}
	if content != "" {
		updates["content"] = content
	}
	imageChanged := false
	if len(visual) > 0 {
		visualType := normalizeVisualType(visual[0], "")
		updates["visual_type"] = visualType
		imageChanged = visualType != normalizeVisualType(sc.VisualType, "")
	}
	if len(visual) > 1 {
		megaType := ""
		if normalizeVisualType(visual[0], "") == "megastructure" {
			megaType = normalizeMegaType(visual[1])
		}
		updates["mega_type"] = megaType
		imageChanged = imageChanged || megaType != sc.MegaType
	}
	if imagePrompt != "" {
		updates["image_prompt"] = imagePrompt
		imageChanged = imageChanged || sc.ImagePrompt != imagePrompt
	}
	if imageChanged {
		updates["prompt_stale"] = strings.TrimSpace(sc.VideoFullPrompt) != ""
		updates["image_file"] = ""
		updates["video_task_id"] = ""
		updates["video_file"] = ""
		updates["video_input_file"] = ""
		updates["video_gpu"] = nil
		updates["image_retries"] = 0
		updates["video_retries"] = 0
		updates["image_token"] = ""
		updates["status"] = "pending"
		updates["error"] = ""
	} else if (content != "" && sc.Content != content) || durationChanged {
		// 修改正文或时长：保留画面，视频需重新生成；已审核完整提示词需要重新确认。
		updates["prompt_stale"] = strings.TrimSpace(sc.VideoFullPrompt) != ""
		updates["video_task_id"] = ""
		updates["video_file"] = ""
		updates["video_input_file"] = ""
		updates["video_gpu"] = nil
		updates["video_retries"] = 0
		if sc.VideoFile != "" || sc.VideoTaskID != "" {
			updates["status"] = "image_ready"
		}
		updates["error"] = ""
	}
	if len(updates) == 0 {
		return nil
	}
	if err := s.db.Model(sc).Updates(updates).Error; err != nil {
		return err
	}
	if imageChanged {
		if err := MarkSceneCandidatesStale(s.db, sc.ProjectID, sc.ID, "", "场景画面来源或提示词已修改"); err != nil {
			return err
		}
	} else if (content != "" && sc.Content != content) || durationChanged {
		if err := MarkSceneCandidatesStale(s.db, sc.ProjectID, sc.ID, "video", "场景正文或时长已修改"); err != nil {
			return err
		}
	}
	if (imageChanged || (content != "" && sc.Content != content) || durationChanged) && sc.VideoTaskID != "" && s.tasks != nil {
		if err := s.tasks.CancelTask(sc.VideoTaskID); err != nil && !strings.Contains(err.Error(), "已结束") {
			log.Printf("[scene %d] cancel stale video task %s failed: %v", sc.ID, sc.VideoTaskID, err)
		}
	}
	return nil
}

// ---------- 剧本生成（文生文） ----------

type scriptDialogue struct {
	Character  string `json:"character"`
	SpeechType string `json:"speech_type"`
	Text       string `json:"text"`
}

type scriptScene struct {
	Title               string           `json:"title"`
	Content             string           `json:"content"`
	ImagePrompt         string           `json:"image_prompt"`
	Duration            float64          `json:"duration"`
	Characters          []string         `json:"characters"`
	VisibleCharacters   []string         `json:"visible_characters"`
	VoiceCharacters     []string         `json:"voice_characters"`
	MentionedCharacters []string         `json:"mentioned_characters"`
	Location            string           `json:"location"`
	Props               []string         `json:"props"`
	VisualType          string           `json:"visual_type"`
	MegaType            string           `json:"mega_type"`
	Dialogues           []scriptDialogue `json:"dialogues"`
}

type scriptResult struct {
	Script      string        `json:"script"`
	VisualBible string        `json:"visual_bible"`
	Scenes      []scriptScene `json:"scenes"`
}

const scriptSystemPrompt = `你是一位专业的漫剧编剧与分镜师。根据用户提供的故事创意，创作一部完整的漫剧剧本，并拆分为多个分镜场景，供后续"文生图 + 图生视频"流水线使用。
要求：
1. 只输出一个合法的 JSON 对象，不要输出任何解释、Markdown 代码块标记或其它文字。
2. JSON 结构固定为：
{
  "script": "完整剧本正文。每段发声内容使用固定标签：【动作】画面描述、【对白｜角色名】原文、【旁白】原文、【内心独白｜角色名】原文",
  "visual_bible": "主要角色的固定外貌、服装、色彩与全片统一画风；后续每个画面提示词都必须遵守",
  "scenes": [
    {
      "title": "场景1：概括性标题",
      "content": "该场景的视频提示词：描述画面动作、镜头运动（如推近/摇镜）、人物表情与对白，现在时态，1~3 句",
      "image_prompt": "该场景的静态画面提示词（用于文生图）：包含主体人物外貌特征、服装、场景环境、光影氛围、构图与画风描述",
      "duration": 5,
      "characters": ["兼容字段：与 visible_characters 相同"],
      "visible_characters": ["本镜最终画面中实际可见的角色"],
      "voice_characters": ["本镜只发声但不在画面中出现的角色"],
      "mentioned_characters": ["仅在剧情说明、对白或独白中被提及的角色"],
      "location": "该场景地点名（同一地点多场景须用同一名称，保证环境一致；无明确地点则为空字符串）",
      "props": ["该场景出现的关键道具名（同一道具须用同一名称；无则为空数组）"],
      "visual_type": "normal 或 megastructure（仅巨型建筑、巨兽、地质奇观、巨型机械、超现实巨构使用后者）",
      "mega_type": "architecture/creature/geological/mechanical/surreal，非巨构留空",
      "dialogues": [{"character": "角色名", "speech_type": "dialogue", "text": "角色说出的原文"}, {"character": "旁白", "speech_type": "narration", "text": "正文明确写出的旁白原文"}, {"character": "角色名", "speech_type": "monologue", "text": "正文明确写出的内心独白原文"}]
    }
  ]
}
3. 默认按约 180 秒单集规划 20~30 个镜头；每个镜头为 3~15 秒的独立视频片段，所有镜头 duration 之和应在 162~198 秒内。
4. 人物一致性至关重要：同一角色在多个场景出现时，image_prompt 必须重复其外貌特征（发型、服装颜色、体型），且所有场景画风描述保持一致。
5. 必须区分人物用途：visible_characters 只列本镜最终画面中真实可见的人物（包括回忆画面、照片、倒影中确实被画出的角色）；voice_characters 只列画外对白或内心独白的发声者；mentioned_characters 只列剧情说明、对白或独白中被提到但不会出现在画面中的人物。人物名字出现在文字中不等于画面出场。characters 为兼容字段，必须与 visible_characters 完全相同。只有 visible_characters 会要求人物四视图。
6. script 正文优先使用固定格式：【动作】画面描述、【对白｜角色名】原文、【旁白】原文、【内心独白｜角色名】原文。逐场检查故事正文和分镜内容中的明确发声标注，并忠实提取到 dialogues：“旁白/画外音：原文”使用 narration，“角色名内心独白：原文”使用 monologue，“角色名：原文”使用 dialogue。只复制标注后的原文，不改写、不概括、不补充。不得把“他心里疑惑”“气氛压抑”等心理、动作或氛围描写转换成独白或旁白。speech_type 只能是 dialogue、narration 或 monologue；dialogue/monologue 的 character 必须是角色名，narration 的 character 固定为“旁白”。正文和分镜内容均未明确出现可发声内容时必须为空数组。
7. 第一个场景尽量给出大场景/环境交代，后续场景聚焦人物动作与剧情推进。
8. 道具与场景一致性：贯穿剧情的关键道具（信物/武器等）与主要地点必须在 props/location 中用统一名称标出（系统会用同名资产参考图锁定其外观），同一道具/地点在不同场景中名称必须完全相同。`

// GenerateScript 生成分镜剧本：已有创作方案时按方案渲染（两阶段流程），否则直接文生文。
// episodeN 指定当前制作集数（默认 1），只生成该集的分镜场景并替换该集旧场景。
func (s *ProjectService) GenerateScript(p *models.Project, episodeN int) (*models.Project, []models.Scene, error) {
	if episodeN <= 0 {
		episodeN = 1
	}
	var user strings.Builder
	user.WriteString("故事创意：" + p.Synopsis + "\n")
	if p.Genre != "" {
		user.WriteString("题材：" + p.Genre + "\n")
	}
	if p.Style != "" {
		user.WriteString("画风：" + p.Style + "\n")
	}

	system := scriptSystemPrompt
	if strings.TrimSpace(p.Plan) != "" {
		// 阶段 2：依据创作方案渲染分镜场景（人物外观沿用方案中的 trait/style 保持一致）
		user.WriteString("\n\n=== 创作方案 ===\n" + p.Plan + "\n")
		// 注入当前集提示词（用户可在创作方案中修改每集标题与剧情提示词）
		epTitle, epBrief, targetDuration, targetScenes := s.planEpisodeInfo(p, episodeN)
		user.WriteString(fmt.Sprintf("\n=== 当前制作集 ===\n第 %d 集「%s」\n本集剧情提示词：%s\n目标时长：%.0f秒 | 目标镜头：%d个\n", episodeN, epTitle, epBrief, targetDuration, targetScenes))
		user.WriteString("请基于创作方案，重点围绕「当前制作集」的剧情提示词，输出该集的分镜剧本 JSON。")
		system = scriptFromPlanSystemPrompt(targetDuration, targetScenes)
	} else {
		user.WriteString("请按系统要求输出剧本 JSON。")
	}

	raw, err := s.chatWithSkill(p.ID, models.SkillStageStoryboard, system, user.String(), map[string]string{"episode_n": fmt.Sprint(episodeN), "story_prompt": p.Synopsis, "characters": p.Plan})
	if err != nil {
		return nil, nil, err
	}
	return s.generateScriptCore(p, episodeN, raw, resScriptHandler(true))
}

// resScriptHandler 决定落库的剧本正文来源：true 使用 LLM 输出，false 使用用户定稿剧本
func resScriptHandler(useLLM bool) func(string, string) string {
	if useLLM {
		return func(episodePrefix, resScript string) string {
			if episodePrefix != "" {
				return episodePrefix + resScript
			}
			return resScript
		}
	}
	return func(episodePrefix, userScript string) string {
		if episodePrefix != "" {
			return episodePrefix + userScript
		}
		return userScript
	}
}

// GenerateScriptFromText 用户编辑剧本定稿后，重新渲染该集分镜场景（剧本正文以用户文本为准）
func (s *ProjectService) GenerateScriptFromText(p *models.Project, episodeN int, scriptText string) (*models.Project, []models.Scene, error) {
	if strings.TrimSpace(scriptText) == "" {
		return nil, nil, fmt.Errorf("剧本内容不能为空")
	}
	if episodeN <= 0 {
		episodeN = 1
	}
	var user strings.Builder
	user.WriteString("=== 剧本正文（用户已定稿，请勿改写剧情） ===\n" + scriptText + "\n")
	system := scriptSystemPrompt
	if strings.TrimSpace(p.Plan) != "" {
		user.WriteString("\n\n=== 创作方案 ===\n" + p.Plan + "\n")
		// 获取当前集的目标时长和镜头数
		_, _, targetDuration, targetScenes := s.planEpisodeInfo(p, episodeN)
		user.WriteString(fmt.Sprintf("本集目标时长：%.0f秒 | 目标镜头：%d个\n", targetDuration, targetScenes))
		user.WriteString("请保持创作方案中主要角色的人物外貌（trait）与服装（style）一致、全片画风统一，将上面的剧本正文拆分为该集的分镜场景 JSON，输出格式遵循系统要求。")
		system = scriptFromPlanSystemPrompt(targetDuration, targetScenes)
	}

	raw, err := s.chatWithSkill(p.ID, models.SkillStageStoryboard, system, user.String(), map[string]string{"episode_n": fmt.Sprint(episodeN), "story_prompt": scriptText, "characters": p.Plan})
	if err != nil {
		return nil, nil, err
	}
	return s.generateScriptCore(p, episodeN, raw, resScriptHandler(false))
}

// ExpandScript 用 AI 根据已有剧本正文扩写（丰富场景/对白/冲突），返回扩写后文本。
// 不落库，由前端回填编辑框，用户确认后再「保存并 AI 重新生成分镜」。
func (s *ProjectService) ExpandScript(p *models.Project, episodeN int, scriptText string) (string, error) {
	if strings.TrimSpace(scriptText) == "" {
		return "", fmt.Errorf("剧本内容不能为空")
	}
	epTitle, _ := s.planEpisodeOf(p, episodeN)
	if epTitle == "" {
		epTitle = p.Title
	}
	system := "你是专业的漫剧编剧。根据用户提供的本集剧本正文进行扩写：丰富场景环境描写、人物动作与表情、对白与冲突细节，保持原有剧情走向、人物关系与核心冲突不变，节奏更紧凑有张力。只输出扩写后的完整剧本正文（纯文本，按场景分段，含动作描写与对白），不要输出 JSON、Markdown 标记或任何解释。"
	user := fmt.Sprintf("第 %d 集「%s」剧本正文，请扩写：\n\n%s", episodeN, epTitle, scriptText)
	return s.chatWithSkill(p.ID, models.SkillStageStoryboard, system, user, map[string]string{"episode_n": fmt.Sprint(episodeN), "story_prompt": scriptText})
}

// generateScriptCore 解析 LLM 输出并落库（替换指定集的分镜场景）
func (s *ProjectService) generateScriptCore(p *models.Project, episodeN int, raw string, scriptTextFn func(prefix, script string) string) (*models.Project, []models.Scene, error) {
	res, err := parseScriptJSON(raw)
	if err != nil {
		// 小模型可能在长 JSON 中遗漏逗号、混入未转义文本。最多进行两次受控修复：
		// 第二次携带精确解析错误并基于第一次结果修复，避免重复同一种失败。
		candidate := raw
		originalErr := err
		for attempt := 1; attempt <= 2 && err != nil; attempt++ {
			repairSystem := `你是严格的 JSON 语法修复器。只修复 JSON 的引号、转义、逗号、冒号和括号，不改写、删减或新增剧情。必须保留 script、visual_bible、scenes 以及 scenes 内全部字段。输出前逐字符确认可被标准 JSON.parse 解析。只输出一个 JSON 对象，不要 Markdown 或解释。`
			if attempt == 2 {
				repairSystem += "\n上一次修复仍失败，解析器错误是：" + err.Error() + "。请针对该位置修正，不要原样返回。"
			}
			repaired, repairErr := s.textProvider.Chat(repairSystem, candidate)
			if repairErr != nil {
				return nil, nil, fmt.Errorf("剧本解析失败，第%d次自动修复调用失败: %v；原始解析错误: %w", attempt, repairErr, originalErr)
			}
			candidate = repaired
			res, err = parseScriptJSON(candidate)
		}
		if err != nil {
			return nil, nil, fmt.Errorf("剧本解析失败，两次自动修复后仍不是合法 JSON: %w", err)
		}
	}
	_, _, targetDuration, targetScenes := s.planEpisodeInfo(p, episodeN)
	if targetDuration <= 0 {
		targetDuration = 180
	}
	if targetScenes <= 0 {
		targetScenes = 25
	}
	// 本地小模型常给出足量分镜但沿用 5 秒默认值，导致总时长远低于方案预算。
	// 在镜头数足以承载目标时，按原有时长比例确定性重平衡到目标值，不额外消耗一次 LLM 重试。
	rebalanceScriptDurations(res, targetDuration)
	if err := validateScriptResult(res, targetDuration, targetScenes); err != nil {
		return nil, nil, fmt.Errorf("剧本结构不完整（可重试）: %w", err)
	}

	// 新版本落库前停止旧视频任务；旧图片请求使用令牌和版本隔离，无法回写新场景。
	var oldScenes []models.Scene
	s.db.Where("project_id = ? AND episode_n = ?", p.ID, episodeN).Find(&oldScenes)
	s.cancelProjectTasks(p.ID)
	newGeneration := p.Generation + 1

	// 事务写入剧本与场景（仅替换当前集场景）。快照与替换同事务提交，
	// 避免共享工作区中生成失败留下“有修改、无恢复点”的状态。
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if tx.Migrator().HasTable(&models.ScriptRevision{}) {
			if _, err := createScriptRevisionTx(tx, p.ID, episodeN, "before_script_replace", nil); err != nil {
				return err
			}
		}
		for _, old := range oldScenes {
			if old.VideoTaskID != "" {
				if err := tx.Where("task_id = ?", old.VideoTaskID).Delete(&models.Event{}).Error; err != nil {
					return err
				}
				if err := tx.Where("task_id = ?", old.VideoTaskID).Delete(&models.Task{}).Error; err != nil {
					return err
				}
			}
		}
		if len(oldScenes) > 0 {
			oldIDs := make([]uint, 0, len(oldScenes))
			for _, o := range oldScenes {
				oldIDs = append(oldIDs, o.ID)
			}
			if err := tx.Where("project_id = ? AND source = ? AND scene_id IN ?", p.ID, "scene", oldIDs).Delete(&models.Material{}).Error; err != nil {
				return err
			}
			if err := tx.Where("scene_id IN ?", oldIDs).Delete(&models.Dialogue{}).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("project_id = ? AND episode_n = ?", p.ID, episodeN).Delete(&models.Scene{}).Error; err != nil {
			return err
		}
		for i, sc := range res.Scenes {
			visibleNames := sceneVisibleNames(sc)
			scene := models.Scene{
				ProjectID: p.ID, EpisodeN: episodeN, Order: i + 1, Generation: newGeneration,
				Title: sc.Title, Content: sc.Content,
				ImagePrompt: sc.ImagePrompt, Duration: normalizeSceneDuration(sc.Duration), Status: "pending",
				Characters:          joinSceneCharacters(visibleNames),
				VisibleCharacters:   joinSceneCharacters(visibleNames),
				VoiceCharacters:     joinSceneCharacters(sc.VoiceCharacters),
				MentionedCharacters: joinSceneCharacters(sc.MentionedCharacters),
				CharacterRolesSet:   true,
				LocationName:        strings.TrimSpace(sc.Location),
				Props:               joinSceneCharacters(sc.Props),
				VisualType:          normalizeVisualType(sc.VisualType, sc.Title+" "+sc.Content+" "+sc.ImagePrompt),
				MegaType:            normalizeMegaType(sc.MegaType),
			}
			if scene.Title == "" {
				scene.Title = fmt.Sprintf("场景 %d", i+1)
			}
			if err := tx.Create(&scene).Error; err != nil {
				return err
			}
			for j, d := range sc.Dialogues {
				if strings.TrimSpace(d.Text) == "" {
					continue
				}
				speechType, character := normalizeScriptSpeech(d.SpeechType, d.Character)
				if speechType == "" {
					continue
				}
				if err := tx.Create(&models.Dialogue{
					SceneID: scene.ID, ProjectID: p.ID, Order: j + 1,
					Character: character, SpeechType: speechType, Text: strings.TrimSpace(d.Text), Status: "pending",
				}).Error; err != nil {
					return err
				}
			}
		}
		status, pipelineStage := "script_done", ""
		if p.AutoGenerate && !p.StopAfterScript {
			status, pipelineStage = "producing", "images"
		}
		if len(res.Scenes) == 0 {
			status, pipelineStage = "draft", ""
		}
		scriptText := scriptTextFn(episodePrefix(p, episodeN), res.Script)
		// 按集保存剧本：scripts map 只替换当前集
		scripts := map[int]string{}
		if strings.TrimSpace(p.Scripts) != "" {
			_ = json.Unmarshal([]byte(p.Scripts), &scripts)
		}
		scripts[episodeN] = scriptText
		scriptsJSON, _ := json.Marshal(scripts)
		return tx.Model(p).Updates(map[string]any{
			"script": scriptText, "scripts": string(scriptsJSON), "visual_bible": res.VisualBible, "status": status, "error": "", "generation": newGeneration,
			"pipeline_stage": pipelineStage,
		}).Error
	})
	if err != nil {
		return nil, nil, err
	}
	s.cleanupStaleSceneFiles(p.ID, oldScenes)
	// 分镜引用的道具/地点若尚未建卡，自动补建（source=auto，供参考图生成与一致性注入）
	s.upsertAssetsFromScenes(p.ID, res.Scenes)

	s.pushProject(p)
	return s.GetProject(p.ID)
}

// episodePrefix 有创作方案时剧本落库带集号前缀
func episodePrefix(p *models.Project, episodeN int) string {
	if strings.TrimSpace(p.Plan) != "" {
		return fmt.Sprintf("第%d集：", episodeN)
	}
	return ""
}

// planEpisodeOf 从创作方案中提取指定集的标题与剧情提示词
func (s *ProjectService) planEpisodeOf(p *models.Project, n int) (string, string) {
	title, brief, _, _ := s.planEpisodeInfo(p, n)
	return title, brief
}

// planEpisodeInfo 从创作方案中提取指定集的完整信息（标题、剧情提示词、目标时长、目标镜头数）
func (s *ProjectService) planEpisodeInfo(p *models.Project, n int) (string, string, float64, int) {
	if strings.TrimSpace(p.Plan) == "" {
		return "", "", 0, 0
	}
	var plan dramaPlan
	if err := json.Unmarshal([]byte(p.Plan), &plan); err != nil {
		return "", "", 0, 0
	}
	for _, ep := range plan.Episodes {
		if ep.N == n {
			// 兼容旧项目缺失字段：默认 180 秒 / 25 个镜头
			targetDuration := ep.TargetDuration
			if targetDuration <= 0 {
				targetDuration = 180
			}
			targetScenes := ep.TargetScenes
			if targetScenes <= 0 {
				targetScenes = 25
			}
			return ep.Title, ep.Brief, targetDuration, targetScenes
		}
	}
	return "", "", 0, 0
}

// normalizeSceneDuration 归一化镜头时长为 3-15 秒（兼容旧项目 3-8 秒上限）
func normalizeSceneDuration(duration float64) float64 {
	if duration == 0 {
		return 5
	}
	if duration < 3 {
		return 3
	}
	if duration > 15 {
		return 15
	}
	return duration
}

// aspectVideoSize 画幅 + 分辨率档位对应的视频尺寸（i2v，须 step 32 对齐）
// resolution: "480p"/"720p"/"1080p"/"2k"，默认 480p；尺寸按 32 对齐。
func aspectVideoSize(aspect, resolution string) (int, int) {
	w, h := 832, 480 // 测试机默认 480p 横屏
	switch resolution {
	case "720p":
		w, h = 1280, 704
	case "1080p":
		w, h = 1920, 1088
	case "2k":
		w, h = 2560, 1440
	}
	switch aspect {
	case "9:16": // 竖屏：宽高互换
		return h, w
	case "1:1":
		switch resolution {
		case "1080p":
			return 1920, 1920
		case "2k":
			return 2560, 2560
		case "720p":
			return 1024, 1024
		default:
			return 512, 512
		}
	default: // 16:9 横屏
		return w, h
	}
}

// videoResolution 读取平台设置的视频分辨率档位（测试机默认 480p）
func (s *ProjectService) videoResolution() string {
	var st models.Setting
	if err := s.db.Where("key = ?", "video_resolution").First(&st).Error; err == nil && st.Value != "" {
		return st.Value
	}
	return "480p"
}

// aspectImageSize 画幅对应的文生图尺寸（须满足 seedream 5.0 像素数 ≥ 3686400，且比例与视频一致避免首帧变形）
func aspectImageSize(aspect string) string {
	switch aspect {
	case "9:16":
		return "1440x2560"
	case "1:1":
		return "1920x1920"
	default:
		return "2560x1440"
	}
}

// projectImageSize 取项目画幅对应的文生图尺寸
func (s *ProjectService) projectImageSize(projectID uint) string {
	var p models.Project
	if err := s.db.First(&p, projectID).Error; err == nil {
		return aspectImageSize(p.AspectRatio)
	}
	return aspectImageSize("")
}

const maxSceneReferenceImages = 9

func visibleSceneCharacterNames(sc *models.Scene) []string {
	if sc.CharacterRolesSet {
		return parseSceneCharacters(sc.VisibleCharacters)
	}
	return parseSceneCharacters(sc.Characters)
}

// sceneCharacterPortraits 收集场景实际可见角色的参考像（四视图优先，标准像兜底，按出场顺序）
func (s *ProjectService) sceneCharacterPortraits(sc *models.Scene) []models.Character {
	names := visibleSceneCharacterNames(sc)
	if len(names) == 0 {
		return nil
	}
	var chars []models.Character
	s.db.Where("project_id = ? AND name IN ? AND (portrait != '' OR sheet != '')", sc.ProjectID, names).Find(&chars)
	if len(chars) == 0 {
		return nil
	}
	byName := make(map[string]models.Character, len(chars))
	for _, c := range chars {
		byName[c.Name] = c
	}
	ordered := make([]models.Character, 0, len(chars))
	for _, n := range names {
		if c, ok := byName[n]; ok && s.sceneHasCharacter(sc, c.Name) {
			ordered = append(ordered, c)
		}
	}
	return ordered
}

// buildSceneVideoSpec 按 MiniMax H3 图生视频规则组织引用、动作、摄影机、光线、风格和声音。
// templateOverride 为空则按场景是否有首帧图自动选择；非空则强制使用指定模板。
func (s *ProjectService) buildSceneVideoSpec(sc *models.Scene, pid string, templateOverride string) (tplCode, promptText string, files map[string][]FileMeta) {
	var p models.Project
	_ = s.db.First(&p, sc.ProjectID).Error
	dubs := s.sceneVideoDialogues(sc)
	prompt := strings.TrimSpace(sc.VideoFullPrompt)
	compile := func(code string) string {
		if prompt != "" {
			return prompt
		}
		switch code {
		case "minimax_h3_t2v":
			return buildH3T2VAPrompt(sc, &p, dubs)
		case "minimax_h3_i2v":
			return buildH3I2VAPrompt(sc, &p, dubs)
		case "minimax_h3_first_last":
			return buildH3FL2VAPrompt(sc, &p, dubs)
		default:
			return buildMiniMaxH3Prompt(sc, &p, dubs)
		}
	}
	if templateOverride != "" {
		return templateOverride, compile(templateOverride), s.buildVideoFilesForTemplate(sc, pid, templateOverride)
	}
	if sc.ImageFile == "" {
		return "minimax_h3_t2v", compile("minimax_h3_t2v"), nil
	}
	// 默认模式继续使用 Ref2VA；人物、场景与分镜参考图在提交阶段统一装配。
	if prompt == "" {
		_, refLines := s.sceneVideoReferenceFiles(sc, pid)
		prompt = buildMiniMaxH3RefPrompt(sc, &p, dubs, refLines)
	}
	return "minimax_h3_ref2v", prompt, nil
}

// buildVideoFilesForTemplate 根据模板构建所需文件映射；不支持的模板返回 nil。
func (s *ProjectService) buildVideoFilesForTemplate(sc *models.Scene, pid, tplCode string) map[string][]FileMeta {
	switch tplCode {
	case "minimax_h3_i2v", "minimax_h3_storyboard_candidates_selflift":
		firstImg := strings.TrimSpace(sc.VideoFirstFrameImg)
		if firstImg == "" {
			firstImg = sc.ImageFile
		}
		if firstImg == "" {
			return nil
		}
		return map[string][]FileMeta{"first_frame": {{TaskID: pid, Name: firstImg}}}
	case "minimax_h3_first_last":
		files := map[string][]FileMeta{}
		// 首帧：用户明确指定的，否则用分镜图
		firstImg := strings.TrimSpace(sc.VideoFirstFrameImg)
		if firstImg == "" {
			firstImg = sc.ImageFile
		}
		if firstImg != "" {
			files["first_frame"] = []FileMeta{{TaskID: pid, Name: firstImg}}
		}
		// 尾帧：用户明确指定的
		lastImg := strings.TrimSpace(sc.VideoLastFrameImg)
		if lastImg != "" {
			files["last_frame"] = []FileMeta{{TaskID: pid, Name: lastImg}}
		}
		return files
	case "minimax_h3_ref2v", "minimax_h3_ref2v_single", "minimax_h3_t2v":
		return nil
	}
	return nil
}

func defaultSceneVideoAction(sc *models.Scene) string {
	content := strings.TrimSpace(sc.Content)
	if content == "" {
		return "[Shot 1] 主体完成一个连续可见的动作并自然停下。摄影机保持静止。"
	}
	return "[Shot 1] " + content
}

// GenerateSceneVideoAction 用文本模型生成用户可审核编辑的 Ref2VA detailed_description 正文。
func (s *ProjectService) GenerateSceneVideoAction(sc *models.Scene) (string, error) {
	if s.textProvider == nil {
		return "", fmt.Errorf("文本生成服务未配置")
	}
	_, refLines := s.sceneVideoReferenceFiles(sc, fmt.Sprint(sc.ProjectID))
	openingPicture := openingPictureTag(refLines)
	shotContext := s.sceneShotContext(sc)
	dubs := s.sceneVideoDialogues(sc)
	dialogueContext := "无结构化对白；禁止生成说话、旁白或人声"
	if valid := validSceneDialogues(dubs); len(valid) > 0 {
		lines := make([]string, 0, len(valid))
		for _, d := range valid {
			speaker := strings.TrimSpace(d.Character)
			if speaker == "" {
				speaker = "旁白"
			}
			lines = append(lines, speaker+"："+strings.TrimSpace(d.Text))
		}
		dialogueContext = strings.Join(lines, "\n")
	}
	system := fmt.Sprintf(`你是 MiniMax H3 Ref2VA 视频动作编辑。只输出简洁的 detailed_description 正文，不输出字段名、解释、规则或 Markdown。
输出正文使用用户当前使用的语言，保持自然、清晰、易于人工审核和修改。真实对白由系统另行加入，你不得翻译、复述或自行补写对白。
正文是给视频模型执行的镜头指令，不是剧本复述。只保留当前镜头实际可见的主体位置、一个主要动作、必要的表情变化和一种运镜，使用3至5句简短明确的句子。
运镜必须自然融入动作句：明确运动类型；仅在确有意义时写小/大幅度和慢/快速，正常速度与中等幅度省略。固定镜头不得同时出现推进、跟随、摇移、升降、环绕或变焦。
忠实采用Scene与结构化Shot，但不得复述剧情背景、人物关系、前因后果、心理活动、内心想法、氛围解释或观众感受；禁止“仿佛想说什么”“未说出口的问题”“沉默中充满”等文学化语言暗示。
第一句以 [Shot 1] 开头，说明画面可从%s参考状态开始；随后直接写动作与镜头。不得新增角色、动作、对白、道具、地点或剧情。
Dialogue只决定人物是否开口及必要口型时机；对白文本将由系统确定性加入，你不得在正文输出台词、<d>标签或改写台词。无结构化对白时，人物保持闭口，不得描写嘴唇微张、欲言又止或任何说话暗示。
正文中的人物必须使用【场景剧情】和【Shot导演设计】里的真实角色名，禁止自行填写或猜测任何<Subject N>编号。系统会在AI返回后依据实际上传顺序，把真实角色名确定性转换为正确Subject编号。
四视图只负责人物身份与服装，场景图只负责环境；不得从参考图反推剧情，不得复述或猜测外貌、服装、陈设。`, openingPicture)
	user := fmt.Sprintf("目标时长：%.1f秒。只提取执行本镜所必需的信息，不要把以下资料逐段复述进输出。\n\n【场景剧情（仅作事实边界）】\n%s\n\n【Shot导演设计（动作与镜头权威）】\n%s\n\n【结构化对白（仅判断口型时机）】\n%s\n\n【实际参考绑定（仅身份与外观）】\n%s", normalizeSceneDuration(sc.Duration), sc.Content, shotContext, dialogueContext, strings.Join(refLines, "\n"))
	policy, err := NewPromptPolicyService(s.db).Resolve(PromptPolicyContext{ProjectID: sc.ProjectID, SceneID: &sc.ID}, PromptPolicyVideoPolish, system)
	if err != nil {
		return "", fmt.Errorf("解析视频提示词策略失败: %w", err)
	}
	out, err := s.textProvider.Chat(policy.Content, user)
	if err != nil {
		return "", fmt.Errorf("AI 生成视频动作提示词失败: %w", err)
	}
	out = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(out), "```"), "```"))
	if strings.Contains(out, "<Subject ") {
		return "", fmt.Errorf("AI 错误地自行填写了 Subject 编号，请重试；人物必须先使用真实角色名")
	}
	out = stripPromptDialogueNarration(out)
	out = useSubjectTags(out, refLines)
	if !strings.HasPrefix(out, "[Shot 1]") {
		out = "[Shot 1] " + out
	}
	if issues := ValidateVideoPrompt(out, sc.Characters, sc.LocationName, sc.Props); len(issues) > 0 {
		return "", fmt.Errorf("AI 返回的视频动作提示词不合格: %s", strings.Join(issues, "；"))
	}
	return out, nil
}

// buildMiniMaxH3Prompt 按 H3 官方六段结构生成视频提示词。
// subject_definitions / summary / retention_analysis / overall_soundscape / non_diegetic_music
// 由系统确定性生成；detailed_description 来自用户编辑或系统生成。
func openingPictureTag(referenceLines []string) string {
	for _, line := range referenceLines {
		if !strings.Contains(line, "当前分镜画面") && !strings.Contains(line, "衔接帧") && !strings.Contains(line, "确认尾帧") {
			continue
		}
		start := strings.Index(line, "<Picture ")
		if start < 0 {
			continue
		}
		if end := strings.Index(line[start:], ">"); end >= 0 {
			return line[start : start+end+1]
		}
	}
	return fmt.Sprintf("<Picture %d>", len(referenceLines))
}

func subjectTagsToNames(text string, referenceLines []string) string {
	for i, line := range referenceLines {
		marker := "角色「"
		start := strings.Index(line, marker)
		if start < 0 {
			continue
		}
		start += len(marker)
		end := strings.Index(line[start:], "」")
		if end < 0 {
			continue
		}
		name := strings.TrimSpace(line[start : start+end])
		if name != "" {
			text = strings.ReplaceAll(text, fmt.Sprintf("<Subject %d>", i+1), name)
		}
	}
	return text
}

func canonicalVideoAction(prompt string, referenceLines []string) string {
	text := subjectTagsToNames(normalizeVideoActionPrompt(prompt), referenceLines)
	text = stripPromptDialogueNarration(text)
	// 剩余 Subject 指向分镜图、场景或道具，并非角色；旧提示词语义已不可靠。
	if strings.Contains(text, "<Subject ") {
		return ""
	}
	return strings.TrimSpace(text)
}

func useSubjectTags(text string, referenceLines []string) string {
	for i, line := range referenceLines {
		marker := "角色「"
		start := strings.Index(line, marker)
		if start < 0 {
			continue
		}
		start += len(marker)
		end := strings.Index(line[start:], "」")
		if end < 0 {
			continue
		}
		name := strings.TrimSpace(line[start : start+end])
		if name == "" {
			continue
		}
		tag := fmt.Sprintf("<Subject %d>", i+1)
		text = strings.ReplaceAll(text, tag+name, tag)
		text = strings.ReplaceAll(text, name, tag)
		text = strings.ReplaceAll(text, tag+tag, tag)
	}
	return text
}

func normalizeVideoActionPrompt(prompt string) string {
	text := strings.TrimSpace(prompt)
	// 历史版本可能把完整六段提示词误存进 video_prompt，并在每次重建时递归嵌套。
	// 最内层（最后一个）detailed_description 才是用户真正的动作正文。
	if i := strings.LastIndex(strings.ToLower(text), "detailed_description:"); i >= 0 {
		text = strings.TrimSpace(text[i+len("detailed_description:"):])
		lower := strings.ToLower(text)
		end := len(text)
		for _, heading := range []string{"overall_soundscape:", "non_diegetic_music:"} {
			if j := strings.Index(lower, heading); j >= 0 && j < end {
				end = j
			}
		}
		text = strings.TrimSpace(text[:end])
	}
	return text
}

var (
	h3DialogueTagPattern      = regexp.MustCompile(`(?is)<d>.*?</d>`)
	dialogueNarrationPattern  = regexp.MustCompile(`(?:<Subject [0-9]+>|[\p{Han}]{1,12})(?:说道|说|问道|答道)[：:]?\s*`)
	quotedDialoguePattern     = regexp.MustCompile(`[“\"][^”\"]*[”\"]`)
	repeatedShotMarkerPattern = regexp.MustCompile(`(?:\[Shot 1\]\s*){2,}`)
)

func stripPromptDialogueNarration(prompt string) string {
	text := h3DialogueTagPattern.ReplaceAllString(prompt, "")
	text = quotedDialoguePattern.ReplaceAllString(text, "")
	text = dialogueNarrationPattern.ReplaceAllString(text, "")
	text = repeatedShotMarkerPattern.ReplaceAllString(text, "[Shot 1] ")
	return strings.TrimSpace(text)
}

var dialogueStageDirectionPattern = regexp.MustCompile(`[（(\[【][^）)\]】]*(?:动作|动作描写|表情|神态|镜头|画面|运镜|抚摸|轻抚|输送|转身|走向|看向|停顿|沉默|环境|音效)[^）)\]】]*[）)\]】]`)

func spokenDialogueText(text string) string {
	text = strings.TrimSpace(dialogueStageDirectionPattern.ReplaceAllString(text, ""))
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	// 纯舞台提示即使没有括号，也不是可发声台词。
	lower := strings.ToLower(text)
	for _, prefix := range []string{"动作描写", "动作：", "动作:", "镜头描写", "镜头：", "镜头:", "画面描写", "旁白说明", "音效：", "音效:"} {
		if strings.HasPrefix(lower, prefix) {
			return ""
		}
	}
	return text
}

func normalizeScriptSpeech(speechType, character string) (string, string) {
	t := strings.ToLower(strings.TrimSpace(speechType))
	character = strings.TrimSpace(character)
	// 兼容旧数据的显式标签，但绝不把空说话人或普通剧情说明猜成旁白。
	if isNarrationSpeaker(character) {
		t = "narration"
	} else if strings.Contains(character, "独白") && (t == "" || t == "dialogue") {
		t = "monologue"
	} else if t == "" && character != "" {
		t = "dialogue"
	}
	switch t {
	case "narration":
		return t, "旁白"
	case "monologue", "dialogue":
		if character != "" {
			return t, character
		}
	}
	return "", ""
}

var (
	explicitSceneSpeechPattern  = regexp.MustCompile(`(内心独白|旁白|画外音)\s*[:：]\s*["“‘']?`)
	standardSceneSpeechPattern  = regexp.MustCompile(`【(对白|旁白|内心独白)(?:[｜|]([^】]+))?】\s*`)
	quotedThoughtPattern        = regexp.MustCompile(`(?:心想|暗自想道|心中说道|心中想道|默念)\s*[:：]?\s*["“]([^"”]+)["”]`)
	offscreenVoiceSpeechPattern = regexp.MustCompile(`(?:门外|屋外|画外|身后)?[^。！？!?\r\n]{0,12}?(?:传来|响起)([\p{Han}]{2,4})(?:清脆悦耳|清脆|悦耳|温柔|低沉|沙哑|熟悉|陌生|焦急|急促|平静)*的声音(?:说道|说|喊道|叫道)\s*[:：]?\s*["“]([^"”]+)["”]`)
)

func sceneCharacterNameList(characters string) []string {
	parts := strings.FieldsFunc(characters, func(r rune) bool {
		return r == ',' || r == '，' || r == '、' || r == ';' || r == '；' || r == '\n'
	})
	out := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, part := range parts {
		if name := strings.TrimSpace(part); name != "" && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}

func sceneSpeechCharacterNames(sc *models.Scene) []string {
	return sceneCharacterNameList(strings.Join([]string{sc.Characters, sc.VisibleCharacters, sc.VoiceCharacters}, ","))
}

func explicitlyQuotedCharacterSpeech(content string, names []string) []models.Dialogue {
	type foundSpeech struct {
		pos      int
		dialogue models.Dialogue
	}
	found := make([]foundSpeech, 0)
	for _, name := range names {
		// 接受“林采薇说道：\"...\"”“传来林采薇的声音说道：\"...\"”等明确发声写法。
		// 必须同时具备已知角色名、明确说话动词和引号原文，避免把剧情说明猜成对白。
		pattern := regexp.MustCompile(regexp.QuoteMeta(name) + `[^。！？!?\r\n]{0,30}?(?:说道|说|问道|答道|喊道|叫道|开口道)\s*[:：]?\s*["“]([^"”]+)["”]`)
		for _, match := range pattern.FindAllStringSubmatchIndex(content, -1) {
			lead := content[match[0]+len(name) : match[2]]
			shadowed := false
			for _, other := range names {
				if other != name && strings.Contains(lead, other) {
					shadowed = true
					break
				}
			}
			if shadowed {
				continue // a nearer known character owns the speaking verb
			}
			text := strings.TrimSpace(content[match[2]:match[3]])
			if text != "" {
				found = append(found, foundSpeech{pos: match[0], dialogue: models.Dialogue{Character: name, SpeechType: "dialogue", Text: text}})
			}
		}
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].pos < found[j].pos })
	out := make([]models.Dialogue, 0, len(found))
	for _, item := range found {
		duplicate := false
		for _, existing := range out {
			if existing.Character == item.dialogue.Character && existing.Text == item.dialogue.Text {
				duplicate = true
				break
			}
		}
		if !duplicate {
			item.dialogue.Order = len(out) + 1
			out = append(out, item.dialogue)
		}
	}
	return out
}

// explicitSceneSpeech extracts only explicitly labelled narration/monologue text.
// It never promotes ordinary thoughts, mood or action prose into spoken content.
func labelledSpeechText(content string, start, end int) string {
	segment := content[start:end]
	if quoteEnd := strings.IndexAny(segment, `"”'’“`); quoteEnd >= 0 {
		segment = segment[:quoteEnd]
	} else if lineEnd := strings.IndexAny(segment, "\r\n"); lineEnd >= 0 {
		segment = segment[:lineEnd]
	}
	return strings.TrimSpace(strings.TrimRight(strings.TrimSpace(segment), "，,；;"))
}

func explicitSceneSpeech(sc *models.Scene) []models.Dialogue {
	content := strings.TrimSpace(sc.Content)
	if content == "" {
		return nil
	}
	names := sceneSpeechCharacterNames(sc)
	out := make([]models.Dialogue, 0)
	// 原文明确写出“门外传来某人的声音说道”时，直接提取画外说话人。
	// 该句式本身已同时提供姓名、发声动作和引号原文，不依赖Scene角色字段。
	for _, match := range offscreenVoiceSpeechPattern.FindAllStringSubmatchIndex(content, -1) {
		speaker := strings.TrimSpace(content[match[2]:match[3]])
		text := strings.TrimSpace(content[match[4]:match[5]])
		if speaker != "" && text != "" {
			matchedText := content[match[0]:match[1]]
			voiceDescription := "门外的" + speaker
			if strings.Contains(matchedText, "清脆悦耳") {
				voiceDescription += "，声音清脆悦耳"
			} else if strings.Contains(matchedText, "清脆") {
				voiceDescription += "，声音清脆"
			}
			out = append(out, models.Dialogue{Character: speaker, SpeechType: "dialogue", H3VoiceDescription: voiceDescription, Text: text, Order: len(out) + 1})
			known := false
			for _, name := range names {
				if name == speaker {
					known = true
					break
				}
			}
			if !known {
				names = append(names, speaker)
			}
		}
	}
	// Preferred authoring format: 【对白｜角色】、【旁白】、【内心独白｜角色】.
	standard := standardSceneSpeechPattern.FindAllStringSubmatchIndex(content, -1)
	for i, match := range standard {
		kind := content[match[2]:match[3]]
		character := ""
		if match[4] >= 0 {
			character = strings.TrimSpace(content[match[4]:match[5]])
		}
		end := len(content)
		if i+1 < len(standard) {
			end = standard[i+1][0]
		}
		text := labelledSpeechText(content, match[1], end)
		if text == "" {
			continue
		}
		d := models.Dialogue{Character: character, Text: text, Order: len(out) + 1}
		switch kind {
		case "旁白":
			d.Character, d.SpeechType = "旁白", "narration"
		case "内心独白":
			d.SpeechType = "monologue"
		case "对白":
			d.SpeechType = "dialogue"
		}
		if d.SpeechType != "narration" && d.Character == "" && len(names) == 1 {
			d.Character = names[0]
		}
		if d.SpeechType == "narration" || d.Character != "" {
			out = append(out, d)
		}
	}

	// Backward-compatible explicit labels, used only for portions not represented above.
	matches := explicitSceneSpeechPattern.FindAllStringSubmatchIndex(content, -1)
	for i, match := range matches {
		label := content[match[2]:match[3]]
		start := match[1]
		end := len(content)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		text := labelledSpeechText(content, start, end)
		if text == "" {
			continue
		}
		d := models.Dialogue{Text: text, Order: len(out) + 1}
		if label == "旁白" || label == "画外音" {
			d.Character, d.SpeechType = "旁白", "narration"
		} else {
			prefix := content[:match[0]]
			bestPos, speaker := -1, ""
			for _, name := range names {
				if pos := strings.LastIndex(prefix, name); pos > bestPos {
					bestPos, speaker = pos, name
				}
			}
			if speaker == "" && len(names) == 1 {
				speaker = names[0]
			}
			if speaker == "" {
				continue
			}
			d.Character, d.SpeechType = speaker, "monologue"
		}
		out = append(out, d)
	}

	// Conservative natural-language dialogue fallback. It requires a known character,
	// an explicit speaking verb and quoted source text; voice-only characters are included.
	for _, extracted := range explicitlyQuotedCharacterSpeech(content, names) {
		duplicate := false
		for _, existing := range out {
			if existing.SpeechType == "dialogue" && existing.Character == extracted.Character && strings.TrimSpace(existing.Text) == strings.TrimSpace(extracted.Text) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			extracted.Order = len(out) + 1
			out = append(out, extracted)
		}
	}

	// Conservative natural-language thought fallback. A quoted thought and an unambiguous
	// character are both required; unquoted mood/psychology prose is ignored.
	for _, match := range quotedThoughtPattern.FindAllStringSubmatchIndex(content, -1) {
		text := strings.TrimSpace(content[match[2]:match[3]])
		if text == "" {
			continue
		}
		prefix := content[:match[0]]
		bestPos, speaker := -1, ""
		for _, name := range names {
			if pos := strings.LastIndex(prefix, name); pos > bestPos {
				bestPos, speaker = pos, name
			}
		}
		if speaker == "" && len(names) == 1 {
			speaker = names[0]
		}
		if speaker == "" {
			continue
		}
		duplicate := false
		for _, existing := range out {
			if existing.SpeechType == "monologue" && existing.Character == speaker && strings.TrimSpace(existing.Text) == text {
				duplicate = true
				break
			}
		}
		if !duplicate {
			out = append(out, models.Dialogue{Character: speaker, SpeechType: "monologue", Text: text, Order: len(out) + 1})
		}
	}
	return out
}

func mergeExplicitSceneSpeech(sc *models.Scene, dubs []models.Dialogue) []models.Dialogue {
	out := append([]models.Dialogue(nil), dubs...)
	for _, extracted := range explicitSceneSpeech(sc) {
		matched := false
		for i := range out {
			existingType, existingCharacter := normalizeScriptSpeech(out[i].SpeechType, out[i].Character)
			if existingType != extracted.SpeechType || strings.TrimSpace(out[i].Text) != strings.TrimSpace(extracted.Text) {
				continue
			}
			matched = true
			// 场景原文同时具备角色名、明确说话动词和引号原文时，说话人是
			// 确定事实；用它纠正早期模型生成的同文本错误说话人。
			if existingCharacter != extracted.Character {
				out[i].Character = extracted.Character
				out[i].SpeechType = extracted.SpeechType
			}
			if extracted.H3VoiceDescription != "" {
				out[i].H3VoiceDescription = extracted.H3VoiceDescription
			}
			break
		}
		if !matched {
			extracted.Order = len(out) + 1
			out = append(out, extracted)
		}
	}
	return out
}

func sameCharacterNameVariant(a, b string) bool {
	normalize := func(v string) string { return strings.ReplaceAll(strings.TrimSpace(v), "薇", "微") }
	return normalize(a) != "" && normalize(a) == normalize(b)
}

func (s *ProjectService) sceneVideoDialogues(sc *models.Scene) []models.Dialogue {
	var dubs []models.Dialogue
	s.db.Where("scene_id = ?", sc.ID).Order("`order`").Find(&dubs)
	dubs = mergeExplicitSceneSpeech(sc, dubs)
	var characters []models.Character
	s.db.Where("project_id = ?", sc.ProjectID).Find(&characters)
	for i := range dubs {
		if dubs[i].H3VoiceDescription == "" {
			continue
		}
		for _, ch := range characters {
			if !sameCharacterNameVariant(ch.Name, dubs[i].Character) {
				continue
			}
			profile := ch.Role + " " + ch.Appearance + " " + ch.Trait
			if strings.Contains(profile, "女") || strings.Contains(profile, "少女") {
				dubs[i].H3VoiceDescription = strings.Replace(dubs[i].H3VoiceDescription, "门外的"+dubs[i].Character, "门外的年轻女性"+dubs[i].Character, 1)
				if strings.Contains(dubs[i].H3VoiceDescription, "声音清脆悦耳") {
					dubs[i].H3VoiceDescription = strings.Replace(dubs[i].H3VoiceDescription, "声音清脆悦耳", "使用清脆悦耳的女声", 1)
				} else {
					dubs[i].H3VoiceDescription = strings.Replace(dubs[i].H3VoiceDescription, "声音清脆", "使用清脆的女声", 1)
				}
			}
			break
		}
	}
	return dubs
}

func validSceneDialogues(dubs []models.Dialogue) []models.Dialogue {
	valid := make([]models.Dialogue, 0, len(dubs))
	for _, d := range dubs {
		d.SpeechType, d.Character = normalizeScriptSpeech(d.SpeechType, d.Character)
		if d.SpeechType == "" {
			continue
		}
		d.Text = spokenDialogueText(d.Text)
		if d.Text != "" {
			valid = append(valid, d)
		}
	}
	return valid
}

func stripH3DialogueTags(prompt string) string {
	return strings.TrimSpace(h3DialogueTagPattern.ReplaceAllString(prompt, ""))
}

func stripStructuredDialogueFromAction(prompt string, dubs []models.Dialogue) string {
	text := stripPromptDialogueNarration(prompt)
	for _, d := range validSceneDialogues(dubs) {
		text = strings.ReplaceAll(text, strings.TrimSpace(d.Text), "")
	}
	for _, residue := range []string{"随后说出台词", "然后说出台词", "说出台词", "随后说：", "然后说：", "说道：", "“”", `""`} {
		text = strings.ReplaceAll(text, residue, "")
	}
	text = strings.ReplaceAll(text, "。。", "。")
	return strings.TrimSpace(text)
}

func dialogueSpeakerIDs(dubs []models.Dialogue) []int {
	ids := make([]int, len(dubs))
	bySpeaker := map[string]int{}
	next := 1
	for i, d := range dubs {
		key := strings.TrimSpace(d.Character)
		if key == "" {
			key = "__narrator__"
		}
		id, ok := bySpeaker[key]
		if !ok {
			id = next
			bySpeaker[key] = id
			next++
		}
		ids[i] = id
	}
	return ids
}

func h3SoundscapeContract(hasDialogue bool) string {
	if hasDialogue {
		return "安静的环境底噪与画面中明确可见的物理动作声持续存在。对白仅在画面时间线中出现，此处不重复对白文本；不出现其他人声、额外对白、旁白、解说、含混发声或吟唱。"
	}
	return "仅有环境底噪与画面中明确可见的物理动作声。全程无对白、无人声、无旁白、无解说、无含混发声、无说话声或吟唱；所有人物始终闭口。"
}

func isNarrationSpeaker(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	return name == "旁白" || name == "画外音" || name == "voiceover" || name == "narrator"
}

func appendStructuredDialogue(body string, dubs []models.Dialogue, referenceLines []string) string {
	valid := validSceneDialogues(dubs)
	if len(valid) == 0 {
		return strings.TrimSpace(body)
	}
	speakerIDs := dialogueSpeakerIDs(valid)
	for i, d := range valid {
		speakerName := strings.TrimSpace(d.Character)
		speaker := useSubjectTags(speakerName, referenceLines)
		if strings.TrimSpace(d.H3VoiceDescription) != "" {
			speaker = strings.TrimSpace(d.H3VoiceDescription)
		}
		speaker += fmt.Sprintf(" (S%d)", speakerIDs[i])
		text := strings.TrimSpace(d.Text)
		crossShot := strings.Contains(text, "<scenetrans>")
		cutoff := strings.Contains(text, "<cutoff>")
		text = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(text, "<scenetrans>", ""), "<cutoff>", ""))
		switch d.SpeechType {
		case "narration":
			body += " " + speaker + "画外音：<d>[Chinese] " + text + "</d>。"
		case "monologue":
			body += " " + speaker + "内心独白：<d>[Chinese] " + text + "</d>。"
		default:
			body += " " + speaker + "说：<d>[Chinese] " + text + "</d>。"
		}
		if crossShot {
			body += "<scenetrans>"
		}
		if cutoff {
			body += "<cutoff>"
		}
	}
	return strings.TrimSpace(body)
}

func h3TimelineBody(sc *models.Scene, p *models.Project, dubs []models.Dialogue) string {
	body := stripStructuredDialogueFromAction(normalizeVideoActionPrompt(sc.VideoPrompt), dubs)
	if body == "" {
		body = defaultSceneVideoAction(sc)
	}
	if !strings.HasPrefix(body, "[Shot 1]") {
		body = "[Shot 1] " + body
	}
	if p != nil && strings.TrimSpace(p.Style) != "" {
		body = strings.Replace(body, "[Shot 1]", "[Shot 1] "+strings.TrimSpace(p.Style)+"风格。", 1)
	}
	return appendStructuredDialogue(body, dubs, nil)
}

// buildH3T2VAPrompt follows the three-field T2VA contract: no picture-alignment preamble.
func buildH3T2VAPrompt(sc *models.Scene, p *models.Project, dubs []models.Dialogue) string {
	body := h3TimelineBody(sc, p, dubs)
	return "integrated_multimodal_description:\n" + body +
		"\n\noverall_soundscape:\n" + h3SoundscapeContract(len(validSceneDialogues(dubs)) > 0) +
		"\n\nnon_diegetic_music:\nN/A"
}

// buildH3I2VAPrompt binds Picture 1 to the actual 0.00-second first frame.
func buildH3I2VAPrompt(sc *models.Scene, p *models.Project, dubs []models.Dialogue) string {
	body := strings.TrimSpace(strings.TrimPrefix(h3TimelineBody(sc, p, dubs), "[Shot 1]"))
	body = "[Shot 1] 以<Picture 1>作为目标视频0.00秒的实际首帧，完整保持其构图、主体、服装、空间布局、道具、光线与视觉风格。" + body
	return "目标视频的参考图时间对齐：<Picture 1>（来自[Shot 1]）对应目标视频0.00秒。\n\n" +
		"integrated_multimodal_description:\n" + body +
		"\n\noverall_soundscape:\n" + h3SoundscapeContract(len(validSceneDialogues(dubs)) > 0) +
		"\n\nnon_diegetic_music:\nN/A"
}

// buildH3FL2VAPrompt describes one continuous path from Picture 1 to Picture 2.
func buildH3FL2VAPrompt(sc *models.Scene, p *models.Project, dubs []models.Dialogue) string {
	duration := normalizeSceneDuration(sc.Duration)
	body := strings.TrimSpace(strings.TrimPrefix(h3TimelineBody(sc, p, dubs), "[Shot 1]"))
	body = "[Shot 1] 镜头从<Picture 1>确定的状态、构图和空间关系开始。" + body +
		"画面动作连续推进，最终在本镜结束时准确落到<Picture 2>确定的姿态、间距、光线和构图。"
	return fmt.Sprintf("目标视频的参考图时间对齐：<Picture 1>（来自[Shot 1]）对应目标视频0.00秒；<Picture 2>（来自[Shot 1]）对应目标视频%.2f秒。\n\n", duration) +
		"integrated_multimodal_description:\n" + body +
		"\n\noverall_soundscape:\n" + h3SoundscapeContract(len(validSceneDialogues(dubs)) > 0) +
		"\n\nnon_diegetic_music:\nN/A"
}

func compileH3PromptForTemplate(template string, sc *models.Scene, p *models.Project, dubs []models.Dialogue, referenceLines []string) string {
	switch template {
	case "minimax_h3_t2v":
		return buildH3T2VAPrompt(sc, p, dubs)
	case "minimax_h3_i2v":
		return buildH3I2VAPrompt(sc, p, dubs)
	case "minimax_h3_first_last":
		return buildH3FL2VAPrompt(sc, p, dubs)
	default:
		return buildMiniMaxH3RefPrompt(sc, p, dubs, referenceLines)
	}
}

func isH3KeyframePrompt(prompt string) bool {
	lower := strings.ToLower(strings.TrimSpace(prompt))
	return strings.HasPrefix(lower, "目标视频的参考图时间对齐") || strings.HasPrefix(lower, "for the target video") || strings.HasPrefix(lower, "how the reference pictures align") || strings.HasPrefix(lower, "integrated_multimodal_description:")
}

func validateH3KeyframePrompt(prompt, template string) []string {
	text := strings.TrimSpace(prompt)
	issues := []string{}
	for _, heading := range []string{"integrated_multimodal_description:", "overall_soundscape:", "non_diegetic_music:"} {
		if strings.Count(strings.ToLower(text), heading) != 1 {
			issues = append(issues, heading+" 必须且只能出现一次")
		}
	}
	if template == "minimax_h3_i2v" && !strings.HasPrefix(text, "目标视频的参考图时间对齐：<Picture 1>") {
		issues = append(issues, "I2VA 必须以 0.00 秒首帧对齐指令开头")
	}
	if template == "minimax_h3_first_last" && !strings.HasPrefix(text, "目标视频的参考图时间对齐：<Picture 1>") {
		issues = append(issues, "FL2VA 必须以首尾帧时间对齐指令开头")
	}
	return issues
}

// normalizeSavedH3Audio only rebuilds dialogue and soundscape sections. All user-edited
// reference definitions, summary, retention rules, visual action and camera text remain intact.
func normalizeSavedH3Audio(prompt string, dubs []models.Dialogue, referenceLines []string) string {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return ""
	}
	detail := normalizeVideoActionPrompt(prompt)
	detail = stripStructuredDialogueFromAction(detail, dubs)
	if !strings.HasPrefix(detail, "[Shot 1]") {
		detail = "[Shot 1] " + detail
	}
	detail = appendStructuredDialogue(detail, dubs, referenceLines)
	sections := []struct{ heading, body string }{
		{"subject_definitions:", h3PromptSection(prompt, "subject_definitions:")},
		{"summary:", h3PromptSection(prompt, "summary:")},
		{"retention_analysis:", h3PromptSection(prompt, "retention_analysis:")},
		{"detailed_description:", detail},
		{"overall_soundscape:", h3SoundscapeContract(len(validSceneDialogues(dubs)) > 0)},
		{"non_diegetic_music:", h3PromptSection(prompt, "non_diegetic_music:")},
	}
	parts := make([]string, 0, len(sections))
	for _, section := range sections {
		parts = append(parts, section.heading+"\n"+strings.TrimSpace(section.body))
	}
	return strings.Join(parts, "\n\n")
}

func videoAudioContractMatches(fullPrompt string, dubs []models.Dialogue) bool {
	valid := validSceneDialogues(dubs)
	soundscape := h3PromptSection(fullPrompt, "overall_soundscape:")
	if strings.Contains(soundscape, "<d>") || strings.Contains(soundscape, "</d>") {
		return false
	}
	if len(valid) == 0 {
		return !strings.Contains(strings.ToLower(fullPrompt), "<d>") && strings.Contains(soundscape, "全程无对白") && strings.Contains(soundscape, "所有人物始终闭口")
	}
	if !strings.Contains(soundscape, "不出现其他人声") || !strings.Contains(soundscape, "额外对白") {
		return false
	}
	speakerIDs := dialogueSpeakerIDs(valid)
	for i, d := range valid {
		text := strings.TrimSpace(d.Text)
		if !strings.Contains(fullPrompt, fmt.Sprintf("(S%d)", speakerIDs[i])) || (!strings.Contains(fullPrompt, "<d>[Chinese] "+text+"</d>") && !strings.Contains(fullPrompt, "<d>[中文] "+text+"</d>")) {
			return false
		}
	}
	return true
}

func resolveRef2VSubmissionPrompt(sc *models.Scene, p *models.Project, dubs []models.Dialogue, referenceLines []string) string {
	saved := strings.TrimSpace(sc.VideoFullPrompt)
	if saved != "" && len(ValidateFullH3PromptForReferences(saved, referenceLines)) == 0 {
		return saved
	}
	actionPrompt := canonicalVideoAction(saved, referenceLines)
	if actionPrompt == "" {
		actionPrompt = canonicalVideoAction(sc.VideoPrompt, referenceLines)
	}
	preview := *sc
	preview.VideoPrompt = actionPrompt
	return buildMiniMaxH3RefPrompt(&preview, p, dubs, referenceLines)
}

func buildMiniMaxH3RefPrompt(sc *models.Scene, p *models.Project, dubs []models.Dialogue, referenceLines []string) string {
	definitions, retention, subjects := h3StoryboardSubjects(referenceLines)
	body := canonicalVideoAction(sc.VideoPrompt, referenceLines)
	if body == "" {
		body = defaultSceneVideoAction(sc)
	}
	body = strings.TrimSpace(strings.TrimPrefix(body, "[Shot 1]"))
	body = useSubjectTags(body, referenceLines)
	body = "[Shot 1] " + body
	style := ""
	if p != nil {
		style = strings.TrimSpace(p.Style)
	}
	if style != "" {
		body = strings.Replace(body, "[Shot 1]", style+"风格。\n[Shot 1]", 1)
	}
	body = stripStructuredDialogueFromAction(body, dubs)
	for _, line := range referenceLines {
		if strings.Contains(line, "上一镜确认尾帧") {
			startPicture := openingPictureTag([]string{line})
			body = "[Shot 1] 本视频必须从 " + startPicture + " 完整一致的画面开始；" + startPicture + " 定义本镜 0.00 秒画面，不是普通参考图。保持该画面构图、人物位置、姿态、服装和场景状态，随后继续本镜动作。 " + strings.TrimSpace(strings.TrimPrefix(body, "[Shot 1]"))
			break
		}
	}
	validDialogues := validSceneDialogues(dubs)
	soundscape := h3SoundscapeContract(len(validDialogues) > 0)
	body = appendStructuredDialogue(body, validDialogues, referenceLines)
	return "subject_definitions:\n" + strings.Join(definitions, "\n") +
		"\n\nsummary:\n[reference generation] " + strings.Join(subjects, "、") + "提供人物、场景及可选分镜状态参考，在约" + fmt.Sprintf("%.0f", normalizeSceneDuration(sc.Duration)) + "秒内严格执行本镜剧情与Shot设计。" +
		"\n\nretention_analysis:\n" + strings.Join(retention, "\n") +
		"\n\ndetailed_description:\n" + body +
		"\n\noverall_soundscape:\n" + soundscape +
		"\n\nnon_diegetic_music:\nN/A"
}

func buildMiniMaxH3Prompt(sc *models.Scene, p *models.Project, dubs []models.Dialogue) string {
	charStr := strings.TrimSpace(sc.Characters)
	locStr := strings.TrimSpace(sc.LocationName)
	propStr := strings.TrimSpace(sc.Props)
	styleStr := ""
	if p != nil {
		styleStr = strings.TrimSpace(p.Style)
	}

	var buf strings.Builder
	// subject_definitions：首帧是唯一视觉基准，角色/场景/道具均以此为锚点
	buf.WriteString("subject_definitions:\n")
	buf.WriteString("输入首帧是当前镜头的唯一视觉基准。")
	if charStr != "" {
		buf.WriteString(" 人物身份、服装、体态和表情均以输入首帧为准：")
		buf.WriteString(" " + charStr + "。")
	}
	if locStr != "" {
		buf.WriteString(" 场景空间与陈设以输入首帧为准：")
		buf.WriteString(" " + locStr + "。")
	}
	if propStr != "" {
		buf.WriteString(" 道具形态与位置以输入首帧为准：")
		buf.WriteString(" " + propStr + "。")
	}
	if styleStr != "" {
		buf.WriteString(" 视觉风格：")
		buf.WriteString(" " + styleStr + "。")
	}
	buf.WriteString("\n\n")

	// summary
	buf.WriteString("summary:\n")
	buf.WriteString("[video continuation] ")
	buf.WriteString("在约")
	buf.WriteString(fmt.Sprintf("%.0f", normalizeSceneDuration(sc.Duration)))
	buf.WriteString("秒内，")
	buf.WriteString(strings.TrimSpace(sc.Content))
	buf.WriteString("\n\n")

	// retention_analysis
	buf.WriteString("retention_analysis:\n")
	buf.WriteString("输入首帧中的人物面部、服装比例、身体朝向、空间位置、场景结构、道具形态和光线方向必须持续保持。")
	buf.WriteString(" 禁止人物换装、变形、瞬移或增减。")
	buf.WriteString(" 禁止新增未在首帧中出现的人物、场景或道具。 NO text, subtitles, captions, speech bubbles, watermarks, logos, UI, or written characters.")
	buf.WriteString("\n\n")

	// detailed_description：用户可编辑正文；系统壳和首帧约束不可被覆盖。
	buf.WriteString("detailed_description:\n")
	buf.WriteString("[Shot 1] 首先严格保持输入首帧构图、人物位置、服装、道具与场景布局。短暂静止后开始运动。 ")
	body := stripH3DialogueTags(normalizeVideoActionPrompt(sc.VideoPrompt))
	if body == "" {
		body = defaultSceneVideoAction(sc)
	}
	buf.WriteString(body)
	buf.WriteString("。摄影机运动必须写明类型、幅度和速度，且全程只使用一种连续运镜。\n\n")

	// overall_soundscape：结构化 Dialogue 是唯一对白事实来源。
	buf.WriteString("overall_soundscape:\n")
	validDialogues := validSceneDialogues(dubs)
	if len(validDialogues) > 0 {
		speakerIDs := dialogueSpeakerIDs(validDialogues)
		for i, d := range validDialogues {
			speaker := strings.TrimSpace(d.Character)
			if speaker == "" {
				speaker = fmt.Sprintf("The narrator (S%d)", speakerIDs[i])
			} else {
				speaker += fmt.Sprintf(" (S%d)", speakerIDs[i])
			}
			buf.WriteString(speaker + " speaks once with clear articulation, <d>[Chinese] " + strings.TrimSpace(d.Text) + "</d>. The speaker then closes their mouth. ")
		}
		buf.WriteString(" " + h3SoundscapeContract(true))
	} else {
		buf.WriteString(h3SoundscapeContract(false))
	}
	buf.WriteString("\n\n")

	// non_diegetic_music
	buf.WriteString("non_diegetic_music:\n")
	buf.WriteString("N/A")

	return buf.String()
}

// ValidateVideoPrompt 检查用户编辑的 detailed_description 是否符合 H3 契约。
// 返回问题列表，空列表表示通过校验。
func ValidateFullH3Prompt(prompt string) []string {
	text := strings.TrimSpace(prompt)
	if text == "" {
		return []string{"完整提示词不能为空"}
	}
	issues := []string{}
	fields := []string{"subject_definitions:", "summary:", "retention_analysis:", "detailed_description:", "overall_soundscape:", "non_diegetic_music:"}
	last := -1
	lower := strings.ToLower(text)
	for _, field := range fields {
		count := strings.Count(lower, field)
		if count == 0 {
			issues = append(issues, "缺少 "+field)
			continue
		}
		if count != 1 {
			issues = append(issues, field+"只能出现一次，禁止嵌套完整提示词")
		}
		pos := strings.Index(lower, field)
		if pos < last {
			issues = append(issues, "H3字段顺序错误")
			break
		}
		last = pos
	}
	if !strings.Contains(text, "<Picture ") {
		issues = append(issues, "缺少参考图片引用")
	}
	if !strings.Contains(h3PromptSection(text, "detailed_description:"), "[Shot 1]") {
		issues = append(issues, "detailed_description 缺少 [Shot 1]")
	}
	return issues
}

func ValidateFullH3PromptForReferences(prompt string, referenceLines []string) []string {
	issues := ValidateFullH3Prompt(prompt)
	// Ref2VA 中分镜图只是可选参考，不是强制首帧；仅校验提示词引用的 Picture
	// 没有超出实际上传数量，不要求 detailed_description 必须从某张图开始。
	for n := len(referenceLines) + 1; n <= maxSceneReferenceImages; n++ {
		if strings.Contains(prompt, fmt.Sprintf("<Picture %d>", n)) || strings.Contains(prompt, fmt.Sprintf("<Subject %d>", n)) {
			issues = append(issues, fmt.Sprintf("引用了未上传的 Picture/Subject %d", n))
		}
	}
	return issues
}

func ValidateVideoPrompt(detailText, charStr, locStr, propStr string) []string {
	_ = charStr
	_ = locStr
	_ = propStr
	text := strings.TrimSpace(detailText)
	if text == "" {
		return nil // 清空表示恢复系统自动生成
	}
	var issues []string
	for _, kw := range []string{"请确认", "请回复", "请上传", "请选择", "please confirm", "please reply", "```"} {
		if strings.Contains(strings.ToLower(text), strings.ToLower(kw)) {
			issues = append(issues, "请移除无关交互或格式标记："+kw)
		}
	}
	// 用户只编辑动作正文，不能覆盖由系统维护的 H3 契约段。
	for _, heading := range []string{"subject_definitions:", "summary:", "retention_analysis:", "detailed_description:", "overall_soundscape:", "non_diegetic_music:"} {
		if strings.Contains(strings.ToLower(text), heading) {
			issues = append(issues, "这里只填写动作正文，请移除固定字段："+heading)
		}
	}
	lower := strings.ToLower(text)
	staticCamera := strings.Contains(text, "固定机位") || strings.Contains(text, "镜头保持固定") || strings.Contains(text, "摄影机保持静止") || strings.Contains(lower, "static shot") || strings.Contains(lower, "camera remains static")
	movingCamera := strings.Contains(text, "推进") || strings.Contains(text, "跟随") || strings.Contains(text, "摇镜") || strings.Contains(text, "平移") || strings.Contains(text, "环绕") || strings.Contains(text, "变焦") || strings.Contains(lower, "pushes in") || strings.Contains(lower, "tracking shot") || strings.Contains(lower, "pans ") || strings.Contains(lower, "trucks ") || strings.Contains(lower, "arcs ") || strings.Contains(lower, "zooms ")
	if staticCamera && movingCamera {
		issues = append(issues, "运镜冲突：固定镜头不能同时推进、跟随、摇移、环绕或变焦")
	}
	if len([]rune(text)) > 4000 {
		issues = append(issues, "视频动作提示词不能超过 4000 字")
	}
	return issues
}

func (s *ProjectService) sceneVideoContinuityReferences(sc *models.Scene, pid string) ([]FileMeta, []string, *models.SceneContinuity) {
	refs, lines := s.sceneVideoReferenceFiles(sc, pid)
	if s.continuity == nil {
		return refs, lines, nil
	}
	cfg, err := s.continuity.Get(sc.ProjectID, sc.ID)
	if err != nil || cfg == nil || cfg.Status != "ready" || cfg.SelectedFrame == nil || cfg.Mode != models.ContinuityModeContinue {
		return refs, lines, cfg
	}
	// Continue 模式下，当前分镜图与上一镜尾帧互斥：保留人物、造型、场景和道具参考，
	// 但用上一镜选定尾帧替换当前分镜图，避免两张完整构图竞争起始画面。
	replacedStoryboard := false
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.Contains(lines[i], "当前分镜画面") {
			refs[i] = FileMeta{TaskID: pid, Name: cfg.SelectedFrame.ImageFile}
			lines[i] = fmt.Sprintf("- <Picture %d>：上一镜确认尾帧（本镜 0.00 秒唯一开始画面）", i+1)
			replacedStoryboard = true
			break
		}
	}
	if !replacedStoryboard && len(refs) < maxSceneReferenceImages {
		refs = append(refs, FileMeta{TaskID: pid, Name: cfg.SelectedFrame.ImageFile})
		lines = append(lines, fmt.Sprintf("- <Picture %d>：上一镜确认尾帧（本镜 0.00 秒唯一开始画面）", len(refs)))
	}
	for i, line := range lines {
		description := strings.TrimSpace(line)
		if parts := strings.SplitN(description, "：", 2); len(parts) == 2 {
			description = strings.TrimSpace(parts[1])
		}
		lines[i] = fmt.Sprintf("- <Picture %d>：%s", i+1, description)
	}
	return refs, lines, cfg
}

func (s *ProjectService) sceneVideoReferenceWarning(sc *models.Scene, actual int) string {
	desired := 1 // current storyboard frame
	if selected, explicit := parseSceneReferences(sc); explicit {
		desired = 1
		for _, ref := range selected {
			if ref.UseH3 && s.sceneReferenceIsRelevant(sc, ref) {
				desired++
			}
		}
	} else {
		desired += len(s.sceneCharacterPortraits(sc)) + len(s.sceneCharacterOutfits(sc)) + len(s.sceneMatchedAssets(sc))
	}
	if desired > actual {
		return fmt.Sprintf("参考图上限为 %d 张，本镜候选 %d 张，实际提交 %d 张；系统按人物、造型、场景、道具优先级取舍", maxSceneReferenceImages, desired, actual)
	}
	return ""
}

func (s *ProjectService) sceneVideoReferenceFiles(sc *models.Scene, pid string) ([]FileMeta, []string) {
	// 用户验证的业务顺序：人物/造型在前，场景/道具随后，当前分镜图最后。
	// H3不依赖图片权重顺序；这里保持稳定顺序以确保 Picture/Subject 编号可读且不漂移。
	refs := []FileMeta{}
	lines := []string{}
	appendStoryboard := func() {
		if sc.ImageFile == "" || len(refs) >= maxSceneReferenceImages {
			return
		}
		refs = append(refs, FileMeta{TaskID: pid, Name: sc.ImageFile})
		lines = append(lines, fmt.Sprintf("- <Picture %d>：当前分镜画面（可选构图与动作状态参考）", len(refs)))
	}
	if selected, selectedLines, explicit := s.selectedSceneReferenceFiles(sc, "h3"); explicit {
		// 显式选择按Scene人物出场顺序 → 其余人物造型 → 场景 → 道具及其他。
		used := make([]bool, len(selected))
		appendSelected := func(i int) {
			if i < 0 || i >= len(selected) || i >= len(selectedLines) || used[i] || len(refs) >= maxSceneReferenceImages-1 {
				return
			}
			used[i] = true
			refs = append(refs, selected[i])
			description := strings.TrimSpace(selectedLines[i])
			if parts := strings.SplitN(description, "：", 2); len(parts) == 2 {
				description = strings.TrimSpace(parts[1])
			}
			lines = append(lines, fmt.Sprintf("- <Picture %d>：%s", len(refs), description))
		}
		for _, name := range visibleSceneCharacterNames(sc) {
			marker := "角色「" + name + "」"
			for i, line := range selectedLines {
				if strings.Contains(line, marker) {
					appendSelected(i)
				}
			}
		}
		for priority := 0; priority < 3; priority++ {
			for i, line := range selectedLines {
				linePriority := 2
				if strings.Contains(line, "角色「") {
					linePriority = 0
				} else if strings.Contains(line, "场景「") {
					linePriority = 1
				}
				if linePriority == priority {
					appendSelected(i)
				}
			}
		}
		appendStoryboard()
		return refs, lines
	}
	for _, ch := range s.sceneCharacterPortraits(sc) {
		if len(refs) >= maxSceneReferenceImages-1 {
			break
		}
		name, kind := ch.Sheet, "四视图"
		if name == "" {
			name, kind = ch.Portrait, "定妆照"
		}
		if name == "" {
			continue
		}
		refs = append(refs, FileMeta{TaskID: pid, Name: name})
		lines = append(lines, fmt.Sprintf("- <Picture %d>：角色「%s」%s", len(refs), ch.Name, kind))
	}
	for _, outfit := range s.sceneCharacterOutfits(sc) {
		if outfit.Character != nil && !s.sceneHasCharacter(sc, outfit.Character.Name) {
			continue
		}
		if len(refs) >= maxSceneReferenceImages-1 {
			break
		}
		name, kind := outfit.Sheet, "套装四视图"
		if name == "" {
			name, kind = outfit.Image, "竖版套装图"
		}
		if name == "" {
			continue
		}
		charName := "角色"
		if outfit.Character != nil {
			charName = outfit.Character.Name
		}
		refs = append(refs, FileMeta{TaskID: pid, Name: name})
		lines = append(lines, fmt.Sprintf("- <Picture %d>：角色「%s」造型套装「%s」%s", len(refs), charName, outfit.Name, kind))
	}
	for _, a := range s.sceneMatchedAssets(sc) {
		if len(refs) >= maxSceneReferenceImages-1 {
			break
		}
		name, kind := a.Image, "参考图"
		if a.Kind == AssetKindProp && a.Sheet != "" {
			name, kind = a.Sheet, "四视图"
		}
		if name == "" {
			continue
		}
		refs = append(refs, FileMeta{TaskID: pid, Name: name})
		lines = append(lines, fmt.Sprintf("- <Picture %d>：%s「%s」%s", len(refs), AssetKindLabel(a.Kind), a.Name, kind))
	}
	appendStoryboard()
	return refs, lines
}

// rebalanceScriptDurations keeps every scene within the supported 3–15 second range
// and preserves relative duration weights while matching the episode budget.
func rebalanceScriptDurations(res *scriptResult, target float64) bool {
	if res == nil || len(res.Scenes) == 0 || target <= 0 {
		return false
	}
	if target < float64(len(res.Scenes))*3 || target > float64(len(res.Scenes))*15 {
		return false
	}
	weights := make([]float64, len(res.Scenes))
	for i, scene := range res.Scenes {
		weights[i] = scene.Duration
		if weights[i] <= 0 || math.IsNaN(weights[i]) || math.IsInf(weights[i], 0) {
			weights[i] = 5
		}
	}
	remaining := target
	active := make(map[int]bool, len(weights))
	for i := range weights {
		active[i] = true
	}
	values := make([]float64, len(weights))
	for len(active) > 0 {
		weightTotal := 0.0
		for i := range active {
			weightTotal += weights[i]
		}
		changed := false
		for i := range active {
			value := remaining * weights[i] / weightTotal
			if value < 3 {
				values[i], remaining, changed = 3, remaining-3, true
				delete(active, i)
			} else if value > 15 {
				values[i], remaining, changed = 15, remaining-15, true
				delete(active, i)
			}
		}
		if !changed {
			for i := range active {
				values[i] = remaining * weights[i] / weightTotal
			}
			break
		}
	}
	// Round to tenths, then put rounding residue on the last scene without breaking bounds.
	total := 0.0
	for i := range values {
		values[i] = math.Round(values[i]*10) / 10
		total += values[i]
	}
	delta := math.Round((target-total)*10) / 10
	for i := len(values) - 1; i >= 0 && math.Abs(delta) >= 0.05; i-- {
		adjusted := values[i] + delta
		if adjusted >= 3 && adjusted <= 15 {
			values[i] = adjusted
			delta = 0
		}
	}
	for i := range res.Scenes {
		res.Scenes[i].Duration = values[i]
	}
	return math.Abs(delta) < 0.05
}

func validateScriptResult(res *scriptResult, targets ...any) error {
	if strings.TrimSpace(res.Script) == "" {
		return fmt.Errorf("缺少完整剧本正文")
	}
	if strings.TrimSpace(res.VisualBible) == "" {
		return fmt.Errorf("缺少角色与画风视觉基准")
	}
	targetDuration, targetScenes := 180.0, 25
	if len(targets) > 0 {
		if v, ok := targets[0].(float64); ok && v > 0 {
			targetDuration = v
		}
	}
	if len(targets) > 1 {
		if v, ok := targets[1].(int); ok && v > 0 {
			targetScenes = v
		}
	}
	// targetScenes 是创作目标而非固定协议。硬下限由单镜最长 15 秒决定；
	// 另保留目标值 60% 的节奏底线，避免把整集压缩成少量超长镜头。
	minScenes := int(math.Ceil(targetDuration / 15))
	paceMin := int(math.Ceil(float64(targetScenes) * 0.6))
	if paceMin > minScenes {
		minScenes = paceMin
	}
	maxScenes := targetScenes + 5
	capacityMax := int(math.Floor(targetDuration / 3))
	if maxScenes > capacityMax {
		maxScenes = capacityMax
	}
	if maxScenes < minScenes {
		maxScenes = minScenes
	}
	if len(res.Scenes) < minScenes || len(res.Scenes) > maxScenes {
		return fmt.Errorf("分镜数量应为 %d~%d 个（目标 %d 个，并满足单镜 3~15 秒），实际为 %d 个", minScenes, maxScenes, targetScenes, len(res.Scenes))
	}
	totalDuration := 0.0
	for i, sc := range res.Scenes {
		if strings.TrimSpace(sc.Content) == "" || strings.TrimSpace(sc.ImagePrompt) == "" {
			return fmt.Errorf("场景 %d 缺少视频或画面提示词", i+1)
		}
		if sc.Duration < 3 || sc.Duration > 15 {
			return fmt.Errorf("场景 %d 时长必须为 3~15 秒", i+1)
		}
		totalDuration += sc.Duration
	}
	minDuration, maxDuration := targetDuration*0.9, targetDuration*1.1
	if totalDuration < minDuration || totalDuration > maxDuration {
		return fmt.Errorf("分镜总时长应为 %.0f~%.0f 秒，实际为 %.0f 秒", minDuration, maxDuration, totalDuration)
	}
	return nil
}

// escapeJSONControlCharsInStrings 只转义 JSON 字符串内部的原始控制字符。
// llama.cpp 小模型常在长剧本字符串中直接输出换行，标准 JSON 要求写成 \\n。
func escapeJSONControlCharsInStrings(data []byte) []byte {
	out := make([]byte, 0, len(data)+32)
	inString, escaped := false, false
	for _, b := range data {
		if !inString {
			out = append(out, b)
			if b == '"' {
				inString = true
			}
			continue
		}
		if escaped {
			out = append(out, b)
			escaped = false
			continue
		}
		switch b {
		case '\\':
			out = append(out, b)
			escaped = true
		case '"':
			out = append(out, b)
			inString = false
		case '\n':
			out = append(out, '\\', 'n')
		case '\r':
			out = append(out, '\\', 'r')
		case '\t':
			out = append(out, '\\', 't')
		default:
			if b < 0x20 {
				out = append(out, []byte(fmt.Sprintf("\\u%04x", b))...)
			} else {
				out = append(out, b)
			}
		}
	}
	return out
}

// removeStrayTextAfterJSONValue 删除模型偶发追加在完整 JSON 值与逗号/括号之间的游离文字。
// 例如角色名中的字可能被错误复制到字符串、数组或数字值之后："雷"雷、[]舒、5秒。
// 仅按标准解析器给出的错误位置处理，并要求游离段后紧邻结构分隔符。
func isJSONValueEnd(b byte) bool {
	return b == '"' || b == '}' || b == ']' || b == 'e' || b == 'l' || (b >= '0' && b <= '9')
}

func removeStrayTextAfterJSONValue(data []byte, parseErr error) ([]byte, bool) {
	var syntaxErr *json.SyntaxError
	if !errors.As(parseErr, &syntaxErr) || syntaxErr.Offset <= 0 {
		return data, false
	}
	errorAt := int(syntaxErr.Offset) - 1
	if errorAt < 0 || errorAt >= len(data) {
		return data, false
	}
	// Offset can point inside a multi-byte rune. Find its first byte without
	// inspecting the rune itself; names and all other Unicode text are equivalent.
	start := errorAt
	for start > 0 && data[start]&0xc0 == 0x80 {
		start--
	}
	previous := start - 1
	for previous >= 0 && (data[previous] == ' ' || data[previous] == '\n' || data[previous] == '\r' || data[previous] == '\t') {
		previous--
	}
	if previous < 0 || !isJSONValueEnd(data[previous]) {
		return data, false
	}
	end := start
	for end < len(data) && data[end] != ',' && data[end] != '}' && data[end] != ']' {
		// 出现新的 JSON 结构符号说明不是单纯的游离文字，拒绝自动删除。
		if data[end] == '"' || data[end] == ':' || data[end] == '{' || data[end] == '[' {
			return data, false
		}
		end++
	}
	if end == start || end >= len(data) {
		return data, false
	}
	cleaned := make([]byte, 0, len(data)-(end-start))
	cleaned = append(cleaned, data[:start]...)
	cleaned = append(cleaned, data[end:]...)
	return cleaned, true
}

// parseScriptJSON 从模型输出中提取 JSON（剥离 markdown 代码块与前后杂文）
func parseScriptJSON(raw string) (*scriptResult, error) {
	text := strings.TrimSpace(raw)
	// 剥离 ```json ... ``` 包裹
	if i := strings.Index(text, "```"); i >= 0 {
		rest := text[i+3:]
		if j := strings.Index(rest, "\n"); j >= 0 {
			rest = rest[j+1:]
		}
		if k := strings.LastIndex(rest, "```"); k >= 0 {
			rest = rest[:k]
		}
		text = strings.TrimSpace(rest)
	}
	// 取第一个 { 到最后一个 }
	start, end := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("输出中未找到 JSON 对象")
	}
	data := escapeJSONControlCharsInStrings([]byte(text[start : end+1]))
	var res scriptResult
	var parseErr, originalParseErr error
	usedCleanup := false
	for attempt := 0; attempt < 8; attempt++ {
		parseErr = json.Unmarshal(data, &res)
		if attempt == 0 {
			originalParseErr = parseErr
		}
		if parseErr == nil {
			break
		}
		cleaned, ok := removeStrayTextAfterJSONValue(data, parseErr)
		if !ok {
			return nil, parseErr
		}
		data = cleaned
		usedCleanup = true
	}
	if parseErr != nil {
		return nil, parseErr
	}
	// If cleanup only made a truncated object parseable, keep the original syntax
	// failure so generateScriptCore asks the model to reconstruct the full schema.
	if usedCleanup && len(res.Scenes) == 0 && originalParseErr != nil {
		return nil, originalParseErr
	}
	if res.Script == "" && len(res.Scenes) == 0 {
		return nil, fmt.Errorf("JSON 中缺少 script/scenes 字段")
	}
	return &res, nil
}

// ---------- 角色资产（Character Bible） ----------
//
// 把角色从创作方案文本提升为独立资产：项目内可复用的人物设定 + 标准参考像。
// 文生图时按场景出场角色注入权威 trait/style，纠偏 LLM 描述漂移，保证跨场景人物一致。

// ListCharacters 项目内角色列表
func (s *ProjectService) ListCharacters(projectID uint) ([]models.Character, error) {
	var list []models.Character
	s.db.Where("project_id = ?", projectID).Order("id").Find(&list)
	return list, nil
}

// CharacterSceneCounts 统计每个角色在场景中的出场次数（按 scene.characters 逗号分隔匹配）
func (s *ProjectService) CharacterSceneCounts(projectID uint) (map[uint]int, error) {
	var chars []models.Character
	if err := s.db.Where("project_id = ?", projectID).Find(&chars).Error; err != nil {
		return nil, err
	}
	var scenes []models.Scene
	s.db.Where("project_id = ?", projectID).Find(&scenes)
	count := map[uint]int{}
	for _, sc := range scenes {
		nameSet := map[string]bool{}
		for _, n := range parseSceneCharacters(sc.Characters) {
			nameSet[n] = true
		}
		if len(nameSet) == 0 {
			continue
		}
		for i := range chars {
			if nameSet[chars[i].Name] {
				count[chars[i].ID]++
			}
		}
	}
	return count, nil
}

// CreateCharacter 手动新建角色
func (s *ProjectService) CreateCharacter(ch models.Character) (*models.Character, error) {
	ch.Name = strings.TrimSpace(ch.Name)
	if ch.Name == "" {
		return nil, fmt.Errorf("角色名不能为空")
	}
	ch.Source = "manual"
	ch.Portrait = "" // 新建无标准像
	if err := s.db.Create(&ch).Error; err != nil {
		return nil, fmt.Errorf("角色名已存在或创建失败: %w", err)
	}
	s.pushProject(nil)
	return &ch, nil
}

// UpdateCharacter 编辑角色（改名需保证项目内唯一）；形象相关字段变化会使旧标准像失效。
func (s *ProjectService) UpdateCharacter(ch *models.Character, req models.Character) error {
	updates := map[string]any{}
	newName := strings.TrimSpace(req.Name)
	if newName != "" && newName != ch.Name {
		var cnt int64
		s.db.Model(&models.Character{}).Where("project_id = ? AND name = ? AND id != ?", ch.ProjectID, newName, ch.ID).Count(&cnt)
		if cnt > 0 {
			return fmt.Errorf("该角色名已存在")
		}
		updates["name"] = newName
	}
	role, trait, style := strings.TrimSpace(req.Role), strings.TrimSpace(req.Trait), strings.TrimSpace(req.Style)
	updates["role"] = role
	updates["trait"] = trait
	updates["style"] = style
	updates["voice"] = strings.TrimSpace(req.Voice) // 预设音色；参考语音（voice_ref/voice_id）由独立接口管理
	effectiveName := ch.Name
	if newName != "" {
		effectiveName = newName
	}
	if effectiveName != ch.Name || role != ch.Role || trait != ch.Trait || style != ch.Style {
		updates["profile_status"] = models.ProfileStatusDraft
		updates["review_note"] = ""
		updates["reference_prompt"] = ""
		updates["portrait"] = ""
		updates["portrait_task_id"] = ""
		updates["portrait_error"] = ""
		updates["sheet"] = ""
		updates["sheet_task_id"] = ""
		updates["sheet_error"] = ""
		updates["profile_version"] = gorm.Expr("profile_version + 1")
	}
	if err := s.db.Model(ch).Updates(updates).Error; err != nil {
		return err
	}
	s.pushProject(nil)
	return nil
}

// DeleteCharacter 删除角色（同时清理远程标准像文件）
func (s *ProjectService) DeleteCharacter(projectID, id uint) error {
	var ch models.Character
	if err := s.db.Where("id = ? AND project_id = ?", id, projectID).First(&ch).Error; err != nil {
		return err
	}
	if ch.Portrait != "" && s.cfg != nil && s.remote != nil && s.upload != nil {
		path := filepath.Join(s.upload.InputDir(), fmt.Sprintf("%d", projectID), ch.Portrait)
		if _, err := s.remote.Run("rm -f -- " + shellQuote(path)); err != nil {
			log.Printf("[character %d] cleanup portrait %s failed: %v", ch.ID, path, err)
		}
	}
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		var outfitIDs, lookIDs []uint
		tx.Model(&models.CharacterOutfit{}).Where("character_id = ?", id).Pluck("id", &outfitIDs)
		tx.Model(&models.CharacterLook{}).Where("character_id = ?", id).Pluck("id", &lookIDs)
		if len(outfitIDs) > 0 {
			if err := tx.Where("outfit_id IN ?", outfitIDs).Delete(&models.CharacterOutfitLook{}).Error; err != nil {
				return err
			}
			if err := tx.Where("outfit_id IN ?", outfitIDs).Delete(&models.SceneCharacterOutfit{}).Error; err != nil {
				return err
			}
			if err := tx.Where("outfit_id IN ?", outfitIDs).Delete(&models.ShotCharacterOutfit{}).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("project_id = ? AND entity_id = ? AND entity_type IN ?", projectID, id, []string{VariantCharacterPortrait, VariantCharacterSheet}).Delete(&models.AssetVariant{}).Error; err != nil {
			return err
		}
		if len(lookIDs) > 0 {
			if err := tx.Where("project_id = ? AND entity_type = ? AND entity_id IN ?", projectID, VariantCharacterLook, lookIDs).Delete(&models.AssetVariant{}).Error; err != nil {
				return err
			}
			if err := tx.Where("look_id IN ?", lookIDs).Delete(&models.SceneCharacterLook{}).Error; err != nil {
				return err
			}
			if err := tx.Where("look_id IN ?", lookIDs).Delete(&models.ShotCharacterLook{}).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("character_id = ?", id).Delete(&models.CharacterOutfit{}).Error; err != nil {
			return err
		}
		if err := tx.Where("character_id = ?", id).Delete(&models.CharacterLook{}).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", id).Delete(&models.Character{}).Error
	}); err != nil {
		return err
	}
	s.pushProject(nil)
	return nil
}

// upsertCharactersFromPlan 从创作方案抽取角色为独立资产。
// 已存在同名：仅补全空字段，保留用户手动编辑与已生成标准像；新角色以 source=auto 入库。
func (s *ProjectService) upsertCharactersFromPlan(p *models.Project, plan *dramaPlan) {
	if plan == nil || len(plan.Characters) == 0 {
		return
	}
	for _, pc := range plan.Characters {
		name := strings.TrimSpace(pc.Name)
		if name == "" {
			continue
		}
		var existing models.Character
		err := s.db.Where("project_id = ? AND name = ?", p.ID, name).First(&existing).Error
		if err == nil {
			updates := map[string]any{}
			if strings.TrimSpace(existing.Role) == "" && strings.TrimSpace(pc.Role) != "" {
				updates["role"] = pc.Role
			}
			if strings.TrimSpace(existing.Trait) == "" && strings.TrimSpace(pc.Trait) != "" {
				updates["trait"] = pc.Trait
			}
			if strings.TrimSpace(existing.Style) == "" && strings.TrimSpace(pc.Style) != "" {
				updates["style"] = pc.Style
			}
			for key, pair := range map[string][2]string{
				"appearance": {existing.Appearance, pc.Appearance}, "personality": {existing.Personality, pc.Personality},
				"background": {existing.Background, pc.Background}, "relationships": {existing.Relationships, pc.Relationships},
				"emotions": {existing.Emotions, pc.Emotions}, "habits": {existing.Habits, pc.Habits},
				"wardrobe_detail": {existing.WardrobeDetail, pc.WardrobeDetail}, "lighting_mood": {existing.LightingMood, pc.LightingMood},
				"color_palette": {existing.ColorPalette, pc.ColorPalette},
			} {
				if strings.TrimSpace(pair[0]) == "" && strings.TrimSpace(pair[1]) != "" {
					updates[key] = pair[1]
				}
			}
			if len(updates) > 0 {
				updates["profile_status"] = models.ProfileStatusDraft
				updates["profile_version"] = gorm.Expr("profile_version + 1")
				updates["reference_prompt"] = ""
				updates["review_note"] = ""
				updates["portrait"] = ""
				updates["portrait_task_id"] = ""
				updates["portrait_error"] = ""
				s.db.Model(&existing).Updates(updates)
			}
			continue
		}
		s.db.Create(&models.Character{
			ProjectID: p.ID, Name: name, Role: pc.Role, Trait: pc.Trait, Style: pc.Style, Source: "auto",
			Appearance: pc.Appearance, Personality: pc.Personality, Background: pc.Background,
			Relationships: pc.Relationships, Emotions: pc.Emotions, Habits: pc.Habits,
			WardrobeDetail: pc.WardrobeDetail, LightingMood: pc.LightingMood, ColorPalette: pc.ColorPalette,
			ProfileStatus: models.ProfileStatusDraft, ProfileVersion: 1,
		})
	}
	s.pushProject(nil)
}

// GenerateAllPortraits 一键生成全部缺少标准像的角色
func (s *ProjectService) GenerateAllPortraits(p *models.Project) (int, error) {
	var chars []models.Character
	s.db.Where("project_id = ? AND portrait = '' AND profile_status = ? AND reference_prompt != ''", p.ID, models.ProfileStatusApproved).Find(&chars)
	for i := range chars {
		if err := s.StartCharacterPortrait(&chars[i]); err != nil {
			return i, err
		}
	}
	return len(chars), nil
}

// StartCharacterPortrait 使用本地 ComfyUI Krea2 模板异步生成角色标准像。
func (s *ProjectService) StartCharacterPortrait(ch *models.Character) error {
	if ch.ProfileStatus != models.ProfileStatusApproved {
		return fmt.Errorf("请先审核通过角色「%s」的详细档案", ch.Name)
	}
	if strings.TrimSpace(ch.ReferencePrompt) == "" {
		return fmt.Errorf("请先为角色「%s」生成或填写参考像提示词", ch.Name)
	}
	if s.tasks == nil {
		return fmt.Errorf("Krea2 生成依赖 ComfyUI 任务服务")
	}
	if ch.PortraitTaskID != "" {
		var active int64
		s.db.Model(&models.Task{}).Where("task_id = ? AND status IN ?", ch.PortraitTaskID, []string{"pending", "queued", "running"}).Count(&active)
		if active > 0 {
			return fmt.Errorf("角色「%s」标准像正在生成", ch.Name)
		}
	}
	var p models.Project
	if err := s.db.First(&p, ch.ProjectID).Error; err != nil {
		return err
	}
	var tpl models.Template
	if err := s.db.Where("code = ? AND enabled = ?", "krea2_character_portrait", true).First(&tpl).Error; err != nil {
		return fmt.Errorf("未找到已启用的 Krea2 角色标准像模板")
	}
	task, err := s.tasks.CreateTask(CreateTaskReq{TemplateID: tpl.ID, Prompt: buildPortraitPrompt(&p, ch)})
	if err != nil {
		return err
	}
	if err := s.db.Model(&models.Character{}).Where("id = ?", ch.ID).Updates(map[string]any{
		"portrait_task_id": task.TaskID, "portrait_error": "",
		"sheet": "", "sheet_task_id": "", "sheet_error": "",
	}).Error; err != nil {
		return err
	}
	go func() { _ = s.tasks.Execute(task.TaskID) }()
	s.pushProject(nil)
	return nil
}

// styleDescriptor 把项目画风转换为强约束描述，用于角色标准像/分镜画面/视频 prompt 统一注入，
// 保证全链路风格一致（真人风格不会被稀释成动漫/插画）。
func styleDescriptor(style string) string {
	s := strings.TrimSpace(style)
	if s == "" {
		return ""
	}
	lower := strings.ToLower(s)
	switch {
	case strings.Contains(lower, "真人") || strings.Contains(lower, "写实") || strings.Contains(lower, "real"):
		return "超写实真人照片风格：真实摄影质感、自然皮肤纹理与光影、人物与场景均为真实照片，严禁任何动漫/卡通/插画/绘画风格"
	case strings.Contains(lower, "国漫"):
		return "国漫插画风格：国风动漫插画质感、线条与色彩统一、角色为动漫形象，严禁真实照片风格"
	case strings.Contains(lower, "日漫"):
		return "日式动漫风格：日式动画插画质感、大眼睛角色、统一线条与上色，严禁真实照片风格"
	case strings.Contains(lower, "韩漫"):
		return "韩漫风格：精致唯美漫画插画、柔和光感、统一画风，严禁真实照片风格"
	case strings.Contains(lower, "3d") || strings.Contains(lower, "三维"):
		return "3D动画电影风格：皮克斯式三维渲染质感、柔和光影、统一材质，严禁真实照片与二维插画风格"
	case strings.Contains(lower, "水墨"):
		return "中国水墨画风格：写意山水笔触、墨色晕染、留白构图，严禁写实照片风格"
	case strings.Contains(lower, "像素"):
		return "像素复古游戏风格：8-bit 像素块质感、统一调色板，严禁写实照片风格"
	case strings.Contains(lower, "赛博") || strings.Contains(lower, "cyber"):
		return "赛博朋克风格：霓虹灯光、未来都市、高对比色调，画面为数字插画质感，严禁真实照片风格"
	default:
		return "全片统一采用「" + s + "」画风：所有角色、场景、光影严格保持该风格一致，严禁混入其他画风"
	}
}

func buildPortraitPrompt(p *models.Project, ch *models.Character) string {
	parts := make([]string, 0, 8)
	if prompt := strings.TrimSpace(ch.ReferencePrompt); prompt != "" {
		// GenerateReferencePrompt 已包含项目画风，避免再次叠加造成影楼化和成熟化。
		parts = append(parts, prompt)
	} else {
		if anchor := characterAgeAnchor(ch.Appearance, ch.Trait); anchor != "" {
			parts = append(parts, ageSubject(ch.Appearance, ch.Trait), anchor)
		}
		if desc := styleDescriptor(p.Style); desc != "" {
			parts = append(parts, desc)
		}
		if appearance := strings.TrimSpace(ch.Appearance); appearance != "" {
			parts = append(parts, "人物外貌必须严格固定："+appearance)
		} else if trait := strings.TrimSpace(ch.Trait); trait != "" {
			parts = append(parts, "人物外貌必须严格固定："+trait)
		}
		parts = append(parts, "本次标准像唯一妆造："+portraitStyling(ch))
		if palette := strings.TrimSpace(ch.ColorPalette); palette != "" {
			parts = append(parts, "角色配色："+palette)
		}
		if lighting := strings.TrimSpace(ch.LightingMood); lighting != "" {
			parts = append(parts, "布光："+lighting)
		}
	}
	parts = append(parts, "单一角色，正面肩部以上头像，头发、发型与头饰完整入镜，直视镜头，自然放松表情，本次唯一上衣的领口、颜色、材质清楚可见，衣料完整覆盖肩部与胸口，纯白干净背景，柔和均匀自然光，居中对称构图，高分辨率角色参考照")
	return normalizePortraitStyle(strings.Join(parts, "，"), p)
}

// characterContextForScene 拼装场景出场角色的权威设定文本，前置注入以纠偏 LLM 描述漂移
func (s *ProjectService) characterContextForScene(sc *models.Scene) string {
	names := parseSceneCharacters(sc.Characters)
	if len(names) == 0 {
		return ""
	}
	var chars []models.Character
	s.db.Where("project_id = ? AND name IN ?", sc.ProjectID, names).Find(&chars)
	if len(chars) == 0 {
		return ""
	}
	byName := make(map[string]models.Character, len(chars))
	for _, c := range chars {
		byName[c.Name] = c
	}
	lines := make([]string, 0, len(names))
	for _, n := range names {
		c, ok := byName[n]
		if !ok {
			continue
		}
		// 注入完整外貌：trait（五官/发型/体型）+ style（默认服装/特征），保证人物一致。
		// 图生图模式下角色标准像已锁定主体形象，文本描述进一步强化五官与服装一致性。
		desc := strings.TrimSpace(c.Trait)
		if st := strings.TrimSpace(c.Style); st != "" {
			if desc != "" {
				desc += "；"
			}
			desc += st
		}
		if desc == "" {
			continue
		}
		lines = append(lines, "- "+c.Name+"："+desc)
	}
	if len(lines) == 0 {
		return ""
	}
	return "【角色设定（必须严格遵守，保证人物一致）】\n" + strings.Join(lines, "\n")
}

func (s *ProjectService) sceneCharacterOutfits(sc *models.Scene) []models.CharacterOutfit {
	var outfits []models.CharacterOutfit
	_ = s.db.Model(&models.CharacterOutfit{}).
		Joins("JOIN scene_character_outfits ON scene_character_outfits.outfit_id = character_outfits.id").
		Where("scene_character_outfits.scene_id = ? AND character_outfits.project_id = ? AND character_outfits.audit_status IN ?", sc.ID, sc.ProjectID, []models.CharacterLookStatus{models.LookStatusApproved, models.LookStatusPublished}).
		Preload("Character").Order("scene_character_outfits.id").Find(&outfits).Error
	return outfits
}

func (s *ProjectService) sceneCharacterLooks(sc *models.Scene, shotRelatedOnly bool) []models.CharacterLook {
	var looks []models.CharacterLook
	q := s.db.Model(&models.CharacterLook{}).
		Joins("JOIN scene_character_looks ON scene_character_looks.look_id = character_looks.id").
		Where("scene_character_looks.scene_id = ? AND character_looks.project_id = ? AND character_looks.audit_status IN ?", sc.ID, sc.ProjectID, []models.CharacterLookStatus{models.LookStatusApproved, models.LookStatusPublished})
	if shotRelatedOnly {
		q = q.Where("character_looks.is_shot_related = ?", true)
	}
	_ = q.Order("scene_character_looks.is_featured DESC, character_looks.priority DESC, character_looks.id").Find(&looks).Error
	return looks
}

func (s *ProjectService) sceneShotContext(sc *models.Scene) string {
	var shots []models.Shot
	if err := s.db.Where("scene_id = ?", sc.ID).Order("order_num").Find(&shots).Error; err != nil || len(shots) == 0 {
		return "未设置结构化镜头；根据场景剧情选择一个最有叙事信息量的起始画面"
	}
	lines := make([]string, 0, len(shots))
	for _, shot := range shots {
		parts := []string{fmt.Sprintf("镜头%d", shot.Order)}
		for _, value := range []string{shot.Description, "景别：" + shot.ShotType, "机位：" + shot.CameraAngle, "运镜意图：" + shot.CameraMovement, "情绪：" + shot.Emotion, "主体：" + shot.PromptSubject, "动作：" + shot.PromptAction, "构图：" + shot.PromptCamera, "光线：" + shot.PromptLighting, "风格：" + shot.PromptStyle} {
			value = strings.TrimSpace(value)
			if value != "" && !strings.HasSuffix(value, "：") {
				parts = append(parts, value)
			}
		}
		lines = append(lines, "- "+strings.Join(parts, "；"))
	}
	return strings.Join(lines, "\n")
}

func (s *ProjectService) characterLookContextForScene(sc *models.Scene) string {
	looks := s.sceneCharacterLooks(sc, false)
	if len(looks) == 0 {
		return ""
	}
	lines := make([]string, 0, len(looks))
	for _, look := range looks {
		desc := strings.TrimSpace(look.Description)
		if desc == "" {
			desc = strings.TrimSpace(look.Prompt)
		}
		if desc != "" {
			lines = append(lines, fmt.Sprintf("- %s「%s」：%s", LookCategoryLabel(look.Category), look.Name, desc))
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return "【当前场景角色造型（必须严格遵守）】\n" + strings.Join(lines, "\n")
}

// buildSceneImagePrompt 拼装场景文生图提示词：强画风约束 → 画风/视觉基准 → 角色设定 → 道具/场景设定 → 当前分镜
func (s *ProjectService) buildSceneImagePrompt(sc *models.Scene) string {
	parts := make([]string, 0, 6)
	var p models.Project
	if err := s.db.First(&p, sc.ProjectID).Error; err == nil {
		if desc := styleDescriptor(p.Style); desc != "" {
			parts = append(parts, desc)
		}
		if p.Style != "" {
			parts = append(parts, "画风："+p.Style)
		}
		if vb := strings.TrimSpace(p.VisualBible); vb != "" {
			parts = append(parts, "全片视觉基准："+vb)
		}
	}
	if charCtx := s.characterContextForScene(sc); charCtx != "" {
		parts = append(parts, charCtx)
		parts = append(parts, "【人物景别硬约束】优先中景、中近景或近景，主要人物面部必须清晰并占据足够像素；避免远景、大远景及人物在画面中过小。仅当剧情必须交代宏大空间时允许全景，但人物脸部仍须清晰可辨，禁止因景别过远造成崩脸或五官模糊")
	}
	if lookCtx := s.characterLookContextForScene(sc); lookCtx != "" {
		parts = append(parts, lookCtx)
	}
	if assetCtx := s.assetContextForScene(sc); assetCtx != "" {
		parts = append(parts, assetCtx)
	}
	if prompt := strings.TrimSpace(sc.ImagePrompt); prompt != "" {
		parts = append(parts, "当前分镜："+prompt)
	}
	if negative := strings.TrimSpace(sc.NegativePrompt); negative != "" {
		parts = append(parts, "【负向硬约束】画面中禁止出现："+negative)
	}
	prompt := strings.Join(parts, "\n")
	params := map[string]string{"original_prompt": prompt, "character_definitions": s.characterContextForScene(sc), "style_requirements": p.Style}
	if normalizeVisualType(sc.VisualType, sc.Title+" "+sc.Content+" "+sc.ImagePrompt) == "megastructure" {
		params["mega_type"] = normalizeMegaType(sc.MegaType)
		return s.applyPromptSkill(sc.ProjectID, models.SkillStageMegastructure, prompt, params)
	}
	return s.applyPromptSkill(sc.ProjectID, models.SkillStageImagePrompt, prompt, params)
}

// sceneImageSize returns full-HD-or-larger storyboard dimensions for the project aspect ratio.
func sceneImageSize(p *models.Project) (int, int) {
	if p != nil {
		switch strings.TrimSpace(p.AspectRatio) {
		case "9:16":
			return 1080, 1920
		case "1:1":
			return 1920, 1920
		}
	}
	return 1920, 1080
}

func normalizeVisualType(value, text string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "megastructure":
		return "megastructure"
	case "normal":
		return "normal"
	}
	lower := strings.ToLower(text)
	for _, keyword := range []string{"巨构", "巨型建筑", "巨塔", "巨城", "天空城", "悬浮要塞", "世界树", "巨型峡谷", "巨浪", "巨兽", "巨龙", "巨神像", "星舰", "巨型机甲", "行星发动机", "megastructure", "colossal", "leviathan"} {
		if strings.Contains(lower, strings.ToLower(keyword)) {
			return "megastructure"
		}
	}
	return "normal"
}

func normalizeMegaType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "architecture", "creature", "geological", "mechanical", "surreal":
		return value
	default:
		return "architecture"
	}
}

// parseSceneCharacters 解析场景出场角色（逗号分隔）
func parseSceneCharacters(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func sceneVisibleNames(sc scriptScene) []string {
	if len(sc.VisibleCharacters) > 0 {
		return sc.VisibleCharacters
	}
	return sc.Characters
}

// joinSceneCharacters 角色名切片 → 逗号分隔字符串
func joinSceneCharacters(names []string) string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		if n = strings.TrimSpace(n); n != "" {
			out = append(out, n)
		}
	}
	return strings.Join(out, ",")
}

// ---------- 分镜画面生成（文生图） ----------

// GenerateSceneImage 生成单个场景画面：文生图 → 保存到 input/<project_id>/scene_N.png
func (s *ProjectService) GenerateSceneImage(sc *models.Scene) error {
	token, err := s.claimSceneImage(sc)
	if err != nil {
		return err
	}
	return s.generateClaimedSceneImage(sc, token)
}

// StartSceneImage 在返回 API 响应前完成原子占位，避免重复请求都收到成功响应。
func (s *ProjectService) StartSceneImage(sc *models.Scene) error {
	token, err := s.claimSceneImage(sc)
	if err != nil {
		return err
	}
	go func() {
		if err := s.generateClaimedSceneImage(sc, token); err != nil {
			log.Printf("[project %d] scene %d image failed: %v", sc.ProjectID, sc.Order, err)
		}
	}()
	return nil
}

func (s *ProjectService) claimSceneImage(sc *models.Scene) (string, error) {
	token := fmt.Sprintf("%d-%d", sc.ID, time.Now().UnixNano())
	claim := s.db.Model(&models.Scene{}).
		Where("id = ? AND project_id = ? AND generation = ? AND image_token = '' AND status IN ?",
			sc.ID, sc.ProjectID, sc.Generation, []string{"pending", "failed", "image_ready", "video_ready"}).
		Updates(map[string]any{
			"status": "image_pending", "error": "", "image_token": token, "image_task_id": "",
			"image_retries": gorm.Expr("image_retries + 1"),
			"video_task_id": "", "video_file": "", "video_gpu": nil,
		})
	if claim.Error != nil {
		return "", claim.Error
	}
	if claim.RowsAffected == 0 {
		return "", fmt.Errorf("场景 %d 已在生成或状态已变化", sc.Order)
	}
	sc.ImageToken = token
	s.pushProject(nil)
	return token, nil
}

func sanitizeH3ReferencePrompt(prompt string) string {
	replacer := strings.NewReplacer(
		"身着符合角色设定的现代服装，", "",
		"身着符合角色设定的现代服装。", "",
		"身着符合角色设定的服装，", "",
		"身着符合角色设定的服装。", "",
		"穿着与参考图一致的服装，", "",
		"穿着与参考图一致的服装。", "",
		"造型与参考图一致，", "",
		"造型与参考图一致。", "",
	)
	return strings.TrimSpace(replacer.Replace(prompt))
}

func h3IntegratedDescription(prompt string) string {
	lower := strings.ToLower(prompt)
	heading := "integrated_multimodal_description:"
	start := strings.Index(lower, heading)
	if start < 0 {
		return ""
	}
	start += len(heading)
	rest := prompt[start:]
	if end := strings.Index(strings.ToLower(rest), "overall_soundscape:"); end >= 0 {
		rest = rest[:end]
	}
	return strings.TrimSpace(rest)
}

func h3PromptSection(prompt, heading string) string {
	start := strings.Index(prompt, heading)
	if start < 0 {
		return ""
	}
	start += len(heading)
	rest := prompt[start:]
	end := len(rest)
	for _, next := range []string{"subject_definitions:", "summary:", "retention_analysis:", "detailed_description:", "overall_soundscape:", "non_diegetic_music:"} {
		if i := strings.Index(rest, next); i >= 0 && i < end {
			end = i
		}
	}
	return strings.TrimSpace(rest[:end])
}

func h3StoryboardSubjects(referenceLines []string) (definitions, retention, subjectRefs []string) {
	for i, line := range referenceLines {
		n := i + 1
		desc := strings.TrimSpace(strings.TrimPrefix(line, "-"))
		if cut := strings.Index(desc, "："); cut >= 0 {
			desc = strings.TrimSpace(desc[cut+len("："):])
		}
		for _, suffix := range []string{"，人物身份与造型以参考图为准", "，环境与起始构图以参考图为准"} {
			desc = strings.TrimSuffix(desc, suffix)
		}
		if desc == "" {
			desc = fmt.Sprintf("参考素材%d", n)
		}
		definitions = append(definitions, fmt.Sprintf("<Subject %d> 是 <Picture %d> 中的%s。", n, n, desc))
		retention = append(retention, fmt.Sprintf("<Subject %d> (出现在 [Shot 1]): fully_preserved - %s。", n, desc))
		subjectRefs = append(subjectRefs, fmt.Sprintf("<Subject %d>", n))
	}
	return definitions, retention, subjectRefs
}

func buildH3StoryboardPrompt(sc *models.Scene, p *models.Project, referenceLines []string) string {
	definitions, retention, subjects := h3StoryboardSubjects(referenceLines)
	detail := sanitizeH3ReferencePrompt(h3PromptSection(sc.ImagePrompt, "detailed_description:"))
	if detail == "" {
		detail = sanitizeH3ReferencePrompt(strings.TrimSpace(sc.ImagePrompt))
	}
	if detail == "" {
		detail = strings.TrimSpace(sc.Content)
	}
	detail = strings.TrimSpace(strings.TrimPrefix(detail, "[Shot 1]"))
	if negative := strings.TrimSpace(sc.NegativePrompt); negative != "" {
		detail += "\n负向硬约束：画面中禁止出现" + negative + "。"
	}
	style := ""
	if p != nil {
		style = strings.TrimSpace(p.Style)
	}
	summary := "[reference generation] " + strings.Join(subjects, "、") + "共同构成一张目标静止分镜画面。"
	subjectSentence := "画面中的参考主体为" + strings.Join(subjects, "、") + "。"
	// 已保存的提示词可能已经由本函数编译过；再次提交必须保持幂等，避免重复前缀和画风。
	for strings.HasPrefix(detail, subjectSentence) {
		detail = strings.TrimSpace(strings.TrimPrefix(detail, subjectSentence))
	}
	detailPrefix := "[Shot 1] " + subjectSentence
	if style != "" {
		styleLine := "视觉风格：" + style
		for strings.Contains(detail, styleLine) {
			detail = strings.TrimSpace(strings.Replace(detail, styleLine, "", 1))
		}
		detail += "\n" + styleLine
	}
	return "subject_definitions:\n" + strings.Join(definitions, "\n") +
		"\n\nsummary:\n" + summary +
		"\n\nretention_analysis:\n" + strings.Join(retention, "\n") +
		"\n\ndetailed_description:\n" + detailPrefix + detail +
		"\n\noverall_soundscape:\nN/A\n\nnon_diegetic_music:\nN/A"
}

func (s *ProjectService) missingSceneCharacterReferences(sc *models.Scene, refs []FileMeta) []string {
	present := map[string]bool{}
	for _, ref := range refs {
		present[ref.Name] = true
	}
	names := visibleSceneCharacterNames(sc)
	if len(names) == 0 {
		return nil
	}
	var chars []models.Character
	s.db.Where("project_id = ? AND name IN ?", sc.ProjectID, names).Find(&chars)
	byName := map[string]models.Character{}
	for _, ch := range chars {
		byName[ch.Name] = ch
	}
	missing := []string{}
	for _, name := range names {
		ch, ok := byName[name]
		if !ok || !s.sceneHasCharacter(sc, name) {
			continue
		}
		file := ch.Sheet
		if file == "" {
			file = ch.Portrait
		}
		if file == "" || !present[file] {
			missing = append(missing, name)
		}
	}
	return missing
}

func (s *ProjectService) generateClaimedSceneImage(sc *models.Scene, token string) error {
	if s.tasks == nil {
		s.failSceneImage(sc, token, "MiniMax H3 SelfLift 场景图生成依赖 ComfyUI 任务服务")
		return fmt.Errorf("MiniMax H3 SelfLift 场景图生成依赖 ComfyUI 任务服务")
	}

	// 认领后重新读取最新 Scene，避免提交调用方持有的旧提示词或旧参考图快照。
	var latest models.Scene
	if err := s.db.First(&latest, sc.ID).Error; err != nil {
		s.failSceneImage(sc, token, err.Error())
		return err
	}
	sc = &latest
	refs, lines, explicitRefs := s.selectedSceneReferenceFiles(sc, "krea2") // krea2 是旧数据库字段名，此处实际提交给 H3 SelfLift
	if !explicitRefs {
		refs, lines = s.sceneImageReferenceFiles(sc)
	}
	if len(refs) == 0 {
		s.failSceneImage(sc, token, "MiniMax H3 SelfLift 分镜候选至少需要选择一张参考图")
		return fmt.Errorf("MiniMax H3 SelfLift 分镜候选至少需要选择一张参考图")
	}
	if len(refs) > maxSceneReferenceImages {
		refs, lines = refs[:maxSceneReferenceImages], lines[:maxSceneReferenceImages]
	}
	if missing := s.missingSceneCharacterReferences(sc, refs); len(missing) > 0 {
		msg := "当前镜头人物缺少实际上传的四视图或标准像参考：" + strings.Join(missing, "、")
		s.failSceneImage(sc, token, msg)
		return fmt.Errorf("%s", msg)
	}

	templateCode := "minimax_h3_storyboard_candidates_selflift"
	var tpl models.Template
	if err := s.db.Where("code = ? AND enabled = ?", templateCode, true).First(&tpl).Error; err != nil {
		msg := "未找到已启用的 MiniMax H3 分镜候选模板 " + templateCode
		s.failSceneImage(sc, token, msg)
		return fmt.Errorf("%s", msg)
	}
	var p models.Project
	if err := s.db.First(&p, sc.ProjectID).Error; err != nil {
		s.failSceneImage(sc, token, err.Error())
		return err
	}
	// Scene supplies canonical facts; structured Shots are appended as subordinate director instructions.
	compiledScene := *sc
	if shotContext := strings.TrimSpace(s.sceneShotContext(sc)); shotContext != "" {
		compiledScene.ImagePrompt = strings.TrimSpace(compiledScene.ImagePrompt) + "\n【Shot导演设计】\n" + shotContext
	}
	prompt := buildH3StoryboardPrompt(&compiledScene, &p, lines)
	refNames := make([]string, 0, len(refs))
	for i, ref := range refs {
		refNames = append(refNames, fmt.Sprintf("Picture %d=%s/%s", i+1, ref.TaskID, ref.Name))
	}
	log.Printf("[storyboard-submit] project=%d scene=%d template=%s refs=[%s]\nprompt:\n%s", sc.ProjectID, sc.ID, templateCode, strings.Join(refNames, ", "), prompt)
	width, height := sceneImageSize(&p)
	task, err := s.tasks.CreateTask(CreateTaskReq{
		TemplateID:        tpl.ID,
		Prompt:            prompt,
		Params:            map[string]any{"width": width, "height": height, "length": 5, "duration": 0.21, "fps": 24},
		Files:             map[string][]FileMeta{"ref_images": refs},
		ParentCandidateID: sc.ImageCandidateParentID,
	})
	if err != nil {
		s.failSceneImage(sc, token, "创建 MiniMax H3 SelfLift 场景图任务失败: "+err.Error())
		return err
	}
	bound := s.db.Model(&models.Scene{}).
		Where("id = ? AND image_token = ? AND generation = ?", sc.ID, token, sc.Generation).
		Update("image_task_id", task.TaskID)
	if bound.Error != nil || bound.RowsAffected == 0 {
		_ = s.tasks.CancelTask(task.TaskID)
		if bound.Error != nil {
			return bound.Error
		}
		return fmt.Errorf("场景 %d 生成任务已过期", sc.Order)
	}
	go func() {
		if err := s.tasks.Execute(task.TaskID); err != nil && !errors.Is(err, errNoFreeGPU) {
			log.Printf("[project %d] scene %d execute MiniMax H3 SelfLift task %s failed: %v", sc.ProjectID, sc.Order, task.TaskID, err)
		}
	}()
	s.pushProject(nil)
	return nil
}

// sceneImageReferenceFiles uses the same stable order as explicit selection:
// visible characters, their outfits, the location, then plot props.
func (s *ProjectService) sceneImageReferenceFiles(sc *models.Scene) ([]FileMeta, []string) {
	pid := fmt.Sprint(sc.ProjectID)
	refs := make([]FileMeta, 0, maxSceneReferenceImages)
	lines := make([]string, 0, maxSceneReferenceImages)
	assets := s.sceneMatchedAssets(sc)
	for _, ch := range s.sceneCharacterPortraits(sc) {
		if len(refs) >= maxSceneReferenceImages {
			break
		}
		name, kind := ch.Sheet, "四视图"
		if name == "" {
			name, kind = ch.Portrait, "标准像"
		}
		if name == "" {
			continue
		}
		refs = append(refs, FileMeta{TaskID: pid, Name: name})
		lines = append(lines, fmt.Sprintf("- <Picture %d>：角色「%s」%s，人物身份与造型以参考图为准", len(refs), ch.Name, kind))
	}
	for _, outfit := range s.sceneCharacterOutfits(sc) {
		if outfit.Character != nil && !s.sceneHasCharacter(sc, outfit.Character.Name) {
			continue
		}
		if len(refs) >= maxSceneReferenceImages {
			break
		}
		name, kind := outfit.Sheet, "套装四视图"
		if name == "" {
			name, kind = outfit.Image, "竖版套装图"
		}
		if name == "" {
			continue
		}
		charName := "角色"
		if outfit.Character != nil {
			charName = outfit.Character.Name
		}
		refs = append(refs, FileMeta{TaskID: pid, Name: name})
		lines = append(lines, fmt.Sprintf("- <Picture %d>：角色「%s」造型套装「%s」%s", len(refs), charName, outfit.Name, kind))
	}
	for _, a := range assets {
		if a.Kind != AssetKindLocation || a.Image == "" || len(refs) >= maxSceneReferenceImages {
			continue
		}
		refs = append(refs, FileMeta{TaskID: pid, Name: a.Image})
		lines = append(lines, fmt.Sprintf("- <Picture %d>：场景「%s」环境与起始构图以参考图为准", len(refs), a.Name))
		break
	}
	for _, a := range assets {
		if a.Kind == AssetKindLocation {
			continue
		}
		if len(refs) >= maxSceneReferenceImages {
			break
		}
		name, kind := a.Image, "参考图"
		if a.Kind == AssetKindProp && a.Sheet != "" {
			name, kind = a.Sheet, "四视图"
		}
		if name == "" {
			continue
		}
		refs = append(refs, FileMeta{TaskID: pid, Name: name})
		lines = append(lines, fmt.Sprintf("- <Picture %d>：%s「%s」%s", len(refs), AssetKindLabel(a.Kind), a.Name, kind))
	}
	return refs, lines
}

// GenerateAllImages 生成项目内所有 pending 场景画面（限流并发）
func (s *ProjectService) GenerateAllImages(p *models.Project, episodeN int) (int, error) {
	var scenes []models.Scene
	query := s.db.Where("project_id = ? AND status = ?", p.ID, "pending")
	if episodeN > 0 {
		generation, err := latestEpisodeGeneration(s.db, p.ID, episodeN)
		if err != nil {
			return 0, err
		}
		query = query.Where("episode_n = ? AND generation = ?", episodeN, generation)
	}
	query.Order("`order`").Find(&scenes)
	if len(scenes) > 0 {
		s.db.Model(p).Updates(map[string]any{"status": "producing", "error": ""})
	}
	started := 0
	for i := range scenes {
		if err := s.StartSceneImage(&scenes[i]); err != nil {
			log.Printf("[project %d] scene %d image failed: %v", p.ID, scenes[i].Order, err)
			continue
		}
		started++
	}
	return started, nil
}

// scenePortraitRefs 加载场景出场角色的标准像，转为图生图参考图列表（方舟 image 字段，多角色传多张）。
// 返回 (参考图列表, 与参考图顺序一一对应的「参考图 N → 角色名」描述行)，供提示词关联角色。
func (s *ProjectService) scenePortraitRefs(sc *models.Scene) ([]ImageRef, []string, error) {
	chars := s.sceneCharacterPortraits(sc)
	if len(chars) == 0 {
		return nil, nil, nil
	}
	refs := make([]ImageRef, 0, len(chars))
	lines := make([]string, 0, len(chars))
	for _, ch := range chars {
		name := ch.Sheet
		kind := "四视图"
		if name == "" {
			name = ch.Portrait
			kind = "标准像"
		}
		if name == "" || len(refs) >= maxSceneReferenceImages {
			continue
		}
		abs := filepath.Join(s.cfg.Comfy.ComfyDir, "input", fmt.Sprintf("%d", sc.ProjectID), name)
		f, err := s.remote.Open(abs)
		if err != nil {
			log.Printf("[project %d] open character reference %s: %v", sc.ProjectID, abs, err)
			continue
		}
		data, err := io.ReadAll(f)
		f.Close()
		if err != nil {
			continue
		}
		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
		if ext == "jpg" {
			ext = "jpeg"
		}
		if ext != "jpeg" && ext != "png" && ext != "webp" {
			ext = "jpeg"
		}
		refs = append(refs, ImageRef{URL: fmt.Sprintf("data:image/%s;base64,%s", ext, base64.StdEncoding.EncodeToString(data))})
		lines = append(lines, fmt.Sprintf("- 图%d：角色 %s %s（%s）", len(refs), ch.Name, kind, strings.TrimSpace(ch.Trait)))
	}
	return refs, lines, nil
}

func (s *ProjectService) failScene(sc *models.Scene, errMsg string) {
	s.db.Model(sc).Updates(map[string]any{"status": "failed", "error": errMsg})
	s.updateProjectStatus(sc.ProjectID)
	s.pushProject(nil)
}

func (s *ProjectService) failSceneImage(sc *models.Scene, token, errMsg string) {
	// 重新生成失败时保留旧画面（回退 image_ready）；首次生成失败则 failed
	status := "failed"
	if sc.ImageFile != "" {
		status = "image_ready"
	}
	s.db.Model(&models.Scene{}).Where("id = ? AND image_token = ?", sc.ID, token).
		Updates(map[string]any{"status": status, "error": errMsg, "image_token": "", "image_task_id": ""})
	s.updateProjectStatus(sc.ProjectID)
	s.pushProject(nil)
}

// ---------- 场景视频生成（本地 L40 / MiniMax H3 i2v） ----------

// restoreLegacyProjectInput 兼容 Windows 上曾使用 Linux 默认 /opt/comfyUI 时，
// Go 将其落在当前盘符根目录的旧文件。找到后复制到当前配置的 input 目录。
func (s *ProjectService) restoreLegacyProjectInput(projectID uint, name string) error {
	if name == "" || s.upload == nil || s.remote == nil || s.remote.Enabled() {
		return nil
	}
	target := filepath.Join(s.upload.InputDir(), fmt.Sprint(projectID), filepath.Base(name))
	if _, err := os.Stat(target); err == nil {
		return nil
	}
	wd, _ := os.Getwd()
	volume := filepath.VolumeName(wd)
	legacy := filepath.Join(volume+string(os.PathSeparator), "opt", "comfyUI", "input", fmt.Sprint(projectID), filepath.Base(name))
	data, err := os.ReadFile(legacy)
	if err != nil {
		return fmt.Errorf("分镜图片不存在: %s（旧目录 %s 也未找到）", target, legacy)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	return os.WriteFile(target, data, 0o644)
}

// GenerateSceneVideo 创建场景视频任务：首帧图 = 文生图画面，提示词 = 场景正文
func (s *ProjectService) GenerateSceneVideo(p *models.Project, sc *models.Scene) error {
	if sc.ImageFile == "" {
		return fmt.Errorf("场景 %d 尚未生成画面", sc.Order)
	}
	if err := s.restoreLegacyProjectInput(sc.ProjectID, sc.ImageFile); err != nil {
		return err
	}
	var latest models.Scene
	if err := s.db.First(&latest, sc.ID).Error; err != nil {
		return err
	}
	sc = &latest
	if sc.PromptStale && strings.TrimSpace(sc.VideoFullPrompt) != "" {
		return fmt.Errorf("场景 %d 的剧情、人物或参考图已变化，请重新生成或确认视频提示词后再生成", sc.Order)
	}
	if s.continuity != nil {
		if err := s.continuity.PrepareScene(sc); err != nil {
			return err
		}
	}
	videoW, videoH := aspectVideoSize(p.AspectRatio, s.videoResolution())
	pid := fmt.Sprintf("%d", p.ID)
	assemble := func(override string) (string, string, map[string][]FileMeta) {
		code, prompt, files := s.buildSceneVideoSpec(sc, pid, override)
		if code == "minimax_h3_ref2v" || code == "minimax_h3_ref2v_single" {
			// Current storyboard and continuity frames are assembled before capability validation,
			// so the catalog validates the exact request that will be submitted.
			refFiles, refLines, _ := s.sceneVideoContinuityReferences(sc, pid)
			if len(refFiles) > 0 {
				prompt = resolveRef2VSubmissionPrompt(sc, p, s.sceneVideoDialogues(sc), refLines)
				if files == nil {
					files = map[string][]FileMeta{}
				}
				files["ref_images"] = append(files["ref_images"], refFiles...)
			}
		}
		return code, prompt, files
	}
	tplCode, promptText, videoFiles := assemble(sc.VideoTemplate)
	duration := normalizeSceneDuration(sc.Duration)
	catalog := NewModelCatalogService(s.db)
	tpl, selectionErr := catalog.ValidateVideoSelection(tplCode, videoFiles, duration)
	if selectionErr != nil {
		fallbackCode := "minimax_h3_ref2v"
		if tplCode == fallbackCode {
			return selectionErr
		}
		tplCode, promptText, videoFiles = assemble(fallbackCode)
		tpl, selectionErr = catalog.ValidateVideoSelection(tplCode, videoFiles, duration)
		if selectionErr != nil {
			return fmt.Errorf("所选视频模板不可用且回退模板也不可用: %w", selectionErr)
		}
	}
	claim := s.db.Model(&models.Scene{}).
		Where("id = ? AND project_id = ? AND generation = ? AND status IN ? AND image_file != ''",
			sc.ID, p.ID, sc.Generation, []string{"image_ready", "video_ready", "failed"}).
		Updates(map[string]any{"status": "video_creating", "error": ""})
	if claim.Error != nil {
		return claim.Error
	}
	if claim.RowsAffected == 0 {
		return fmt.Errorf("场景 %d 已在生成或状态已变化", sc.Order)
	}
	videoFileNames := []string{}
	for _, key := range []string{"ref_images", "first_frame", "last_frame"} {
		for i, ref := range videoFiles[key] {
			videoFileNames = append(videoFileNames, fmt.Sprintf("%s[%d]=%s/%s", key, i, ref.TaskID, ref.Name))
		}
	}
	log.Printf("[video-submit] project=%d scene=%d template=%s files=[%s]\nprompt:\n%s", sc.ProjectID, sc.ID, tplCode, strings.Join(videoFileNames, ", "), promptText)
	task, err := s.tasks.CreateTask(CreateTaskReq{
		TemplateID: tpl.ID,
		Prompt:     promptText,
		Params: map[string]any{
			"width": videoW, "height": videoH, "duration": normalizeSceneDuration(sc.Duration),
			"steps": 8, "cfg": 1.0, "fps": 24, "seed": -1, "ref_image_size": "match",
		},
		Files:             videoFiles,
		ParentCandidateID: sc.VideoCandidateParentID,
	})
	if err != nil {
		s.failScene(sc, "创建视频任务失败: "+err.Error())
		return err
	}
	updated := s.db.Model(&models.Scene{}).Where("id = ? AND generation = ? AND status = ?", sc.ID, sc.Generation, "video_creating").Updates(map[string]any{
		"video_task_id": task.TaskID, "video_file": "", "video_input_file": "", "video_gpu": nil,
		"status": "video_pending", "error": "",
	})
	if updated.Error != nil || updated.RowsAffected == 0 {
		_ = s.tasks.CancelTask(task.TaskID)
		if updated.Error != nil {
			return updated.Error
		}
		return fmt.Errorf("场景 %d 视频任务已过期", sc.Order)
	}
	if s.continuity != nil {
		s.continuity.InvalidateDependents(sc.ID, "来源视频重新生成，请重新选择衔接帧")
	}
	go func() {
		// 视频生成并发由 TaskService 全局限流（video_concurrency 平台设置），此处直接提交
		if err := s.tasks.Execute(task.TaskID); err != nil {
			log.Printf("[project %d] scene %d video execute failed: %v", p.ID, sc.Order, err)
		}
	}()
	s.pushProject(nil)
	return nil
}

// GenerateAllVideos 为所有画面就绪的场景创建视频任务
func (s *ProjectService) GenerateAllVideos(p *models.Project, episodeN int) (int, error) {
	var scenes []models.Scene
	query := s.db.Where("project_id = ? AND status IN ?", p.ID, []string{"image_ready", "failed"})
	if episodeN > 0 {
		generation, err := latestEpisodeGeneration(s.db, p.ID, episodeN)
		if err != nil {
			return 0, err
		}
		query = query.Where("episode_n = ? AND generation = ?", episodeN, generation)
	}
	query.Order("`order`").Find(&scenes)
	if len(scenes) > 0 {
		s.db.Model(p).Updates(map[string]any{"status": "producing", "error": ""})
	}
	created := 0
	for i := range scenes {
		if err := s.GenerateSceneVideo(p, &scenes[i]); err != nil {
			log.Printf("[project %d] scene %d video failed: %v", p.ID, scenes[i].Order, err)
			continue
		}
		created++
	}
	return created, nil
}

// CancelSceneVideo 停止场景视频生成：取消 ComfyUI 任务并回退到画面就绪（不会自动重试）
func (s *ProjectService) CancelSceneVideo(p *models.Project, sc *models.Scene) error {
	if sc.VideoTaskID != "" {
		_ = s.tasks.CancelTask(sc.VideoTaskID)
	}
	updated := s.db.Model(&models.Scene{}).
		Where("id = ? AND project_id = ? AND generation = ? AND status IN ?",
			sc.ID, p.ID, sc.Generation, []string{"video_creating", "video_pending", "video_running"}).
		Updates(map[string]any{
			"status": "image_ready", "error": "用户停止生成",
			"video_task_id": "", "video_file": "", "video_gpu": nil,
		})
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected == 0 {
		return fmt.Errorf("场景 %d 当前无进行中的视频生成", sc.Order)
	}
	s.updateProjectStatus(p.ID)
	s.pushProject(nil)
	return nil
}

// WatchSceneVideos 后台轮询场景视频任务状态（3s），任务完成后回写场景
func (s *ProjectService) WatchSceneVideos() {
	s.recoverInterruptedProjects()
	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-s.stopped:
				return
			case <-ticker.C:
				s.syncCharacterPortraits()
				s.syncAssetImages()
				if s.characterLooks != nil {
					s.characterLooks.SyncImages()
					s.characterLooks.SyncOutfitImages()
				}
				s.syncSheets()
				s.syncSceneImages()
				s.syncSceneVideos()
				s.syncDetachedCandidateRetries()
				s.advanceAutoPipelines()
			}
		}
	}()
}

func (s *ProjectService) recoverInterruptedProjects() {
	// TaskService recovers durable tasks. Never detach a Scene from a persisted task: the
	// project synchronizer will project its eventual success/failure back to that same Scene.
	s.db.Model(&models.Scene{}).Where("status = ? AND image_task_id = ''", "image_pending").
		Updates(map[string]any{"status": "pending", "image_token": "", "error": "服务重启，画面任务已重新排队"})
	s.db.Model(&models.Scene{}).Where("status = ? AND video_task_id = ''", "video_creating").
		Updates(map[string]any{"status": "image_ready", "error": "服务重启，视频任务未创建，请重试"})
	s.db.Model(&models.Scene{}).Where("status IN ? AND (video_task_id = '' OR NOT EXISTS (SELECT 1 FROM tasks WHERE tasks.task_id = scenes.video_task_id))", []string{"video_pending", "video_running"}).
		Updates(map[string]any{"status": "image_ready", "video_task_id": "", "video_file": "", "video_input_file": "", "video_gpu": nil, "error": "服务重启，视频任务记录缺失，请重试"})
	// Auto-pipeline merges are restartable from immutable SceneOrder. Manual merges have no
	// owner loop after restart, so fail explicitly instead of remaining pending forever.
	s.db.Exec("UPDATE merge_tasks SET status = 'pending', error = '' WHERE status = 'running' AND EXISTS (SELECT 1 FROM projects WHERE projects.id = merge_tasks.project_id AND projects.auto_generate = ? AND projects.pipeline_stage = ?)", true, "merge")
	s.db.Exec("UPDATE merge_tasks SET status = 'failed', error = ? WHERE status = 'running'", "服务重启，合并已中断，请重试")
	s.db.Model(&models.Project{}).Where("pipeline_stage = ?", "script_running").Update("pipeline_stage", "script")
	s.db.Model(&models.Project{}).Where("pipeline_stage = ?", "plan_running").Update("pipeline_stage", "plan")
	s.db.Model(&models.Project{}).Where("pipeline_stage = ?", "script_manual").
		Updates(map[string]any{"pipeline_stage": "", "auto_generate": false, "error": "服务重启，剧本生成已中断，请重试"})
}

func (s *ProjectService) advanceAutoPipelines() {
	var projects []models.Project
	if err := s.db.Where("auto_generate = ? AND pipeline_stage IN ?", true,
		[]string{"plan", "script", "images", "videos", "merge"}).Find(&projects).Error; err != nil {
		return
	}
	for _, p := range projects {
		go s.advancePipeline(p.ID)
	}
}

func (s *ProjectService) advancePipeline(projectID uint) {
	var p models.Project
	if err := s.db.First(&p, projectID).Error; err != nil || !p.AutoGenerate {
		return
	}
	episodeN := p.PipelineEpisode
	if episodeN <= 0 {
		episodeN = 1
	}
	switch p.PipelineStage {
	case "plan":
		claim := s.db.Model(&models.Project{}).Where("id = ? AND pipeline_stage = ?", p.ID, "plan").
			Update("pipeline_stage", "plan_running")
		if claim.Error != nil || claim.RowsAffected == 0 {
			return
		}
		p.PipelineStage = "plan_running"
		fresh, err := s.GeneratePlan(&p)
		if err != nil {
			s.failPipeline(p.ID, err, true)
			return
		}
		// 角色仅生成可审核的档案草稿，不在用户审核前自动生成人像；道具/场景参考图仍可异步准备。
		s.GenerateAllAssetImages(fresh, "")
		s.db.Model(&models.Project{}).Where("id = ? AND pipeline_stage = ?", p.ID, "plan_running").Update("pipeline_stage", "script")
		go s.advancePipeline(fresh.ID)
	case "script":
		claim := s.db.Model(&models.Project{}).Where("id = ? AND pipeline_stage = ?", p.ID, "script").
			Update("pipeline_stage", "script_running")
		if claim.Error != nil || claim.RowsAffected == 0 {
			return
		}
		p.PipelineStage = "script_running"
		if _, _, err := s.GenerateScript(&p, episodeN); err != nil {
			s.failPipeline(p.ID, err, true)
			return
		}
		if p.StopAfterScript {
			// 创建项目自动触发：只生成第一集剧本，后续画面/视频/合并由人工处理
			s.db.Model(&models.Project{}).Where("id = ?", p.ID).Updates(map[string]any{
				"pipeline_stage": "", "auto_generate": false,
				"status": "script_done", "error": "",
			})
			s.pushProject(nil)
			return
		}
		go s.advancePipeline(p.ID)
	case "images":
		generation, err := latestEpisodeGeneration(s.db, p.ID, episodeN)
		if err != nil {
			s.failPipeline(p.ID, err, true)
			return
		}
		// 分镜画面依赖角色标准像；在画面生成前执行审核门控，避免先生成无角色参考的废图。
		var unapproved, missingPortraits int64
		s.db.Model(&models.Character{}).Where("project_id = ? AND profile_status != ?", p.ID, models.ProfileStatusApproved).Count(&unapproved)
		s.db.Model(&models.Character{}).Where("project_id = ? AND portrait = ''", p.ID).Count(&missingPortraits)
		if unapproved > 0 || missingPortraits > 0 {
			s.failPipeline(p.ID, fmt.Errorf("角色资产尚未就绪：%d 个档案待审核，%d 个角色缺少标准像；请完成审核和标准像生成后重试", unapproved, missingPortraits), true)
			return
		}
		var scenes []models.Scene
		if err := s.db.Where("project_id = ? AND episode_n = ? AND generation = ?", p.ID, episodeN, generation).Order("`order`").Find(&scenes).Error; err != nil || len(scenes) == 0 {
			s.failPipeline(p.ID, fmt.Errorf("没有可生成的分镜场景"), true)
			return
		}
		// 道具/场景参考图优先于分镜画面：分镜图生图需要资产参考图锁定道具与环境外观
		if s.pendingSceneAssetRefs(p.ID, episodeN, generation) > 0 {
			s.GenerateAllAssetImages(&p, "")
			return
		}
		// 文生图失败自动重试一次；达到上限后停止流水线并保留逐场景重试入口。
		s.db.Model(&models.Scene{}).Where("project_id = ? AND episode_n = ? AND generation = ? AND status = ? AND image_file = '' AND image_retries < ?",
			p.ID, episodeN, generation, "failed", 2).Updates(map[string]any{"status": "pending", "error": ""})
		var failed int64
		s.db.Model(&models.Scene{}).Where("project_id = ? AND episode_n = ? AND generation = ? AND status = ?", p.ID, episodeN, generation, "failed").Count(&failed)
		if failed > 0 {
			s.failPipeline(p.ID, fmt.Errorf("%d 个分镜画面生成失败，请修正后重试", failed), true)
			return
		}
		var waiting int64
		s.db.Model(&models.Scene{}).Where("project_id = ? AND episode_n = ? AND generation = ? AND status IN ?", p.ID, episodeN, generation,
			[]string{"pending", "image_pending"}).Count(&waiting)
		if waiting > 0 {
			_, _ = s.GenerateAllImages(&p, episodeN)
			return
		}
		s.db.Model(&models.Project{}).Where("id = ? AND pipeline_stage = ?", p.ID, "images").Update("pipeline_stage", "videos")
		go s.advancePipeline(p.ID)
	case "videos":
		generation, err := latestEpisodeGeneration(s.db, p.ID, episodeN)
		if err != nil {
			s.failPipeline(p.ID, err, true)
			return
		}
		// 角色标准像必须经过“档案审核 → 提示词确认 → 人工触发生成”，自动流水线不得绕过审核。
		var pendingPortraits int64
		s.db.Model(&models.Character{}).Where("project_id = ? AND portrait = ''", p.ID).Count(&pendingPortraits)
		if pendingPortraits > 0 {
			s.failPipeline(p.ID, fmt.Errorf("仍有 %d 个角色缺少已审核的标准像，请审核角色档案并生成标准像后重试", pendingPortraits), true)
			return
		}
		// 确保道具/场景参考图就绪（分镜画面一致性依赖）
		var pendingAssets int64
		s.db.Model(&models.Asset{}).Where("project_id = ? AND image = ''", p.ID).Count(&pendingAssets)
		if pendingAssets > 0 {
			s.GenerateAllAssetImages(&p, "")
			return
		}
		var failed int64
		s.db.Model(&models.Scene{}).Where("project_id = ? AND episode_n = ? AND generation = ? AND status = ?", p.ID, episodeN, generation, "failed").Count(&failed)
		if failed > 0 {
			s.failPipeline(p.ID, fmt.Errorf("%d 个场景视频生成失败，请修正后重试", failed), true)
			return
		}
		var ready, total, waiting int64
		s.db.Model(&models.Scene{}).Where("project_id = ? AND episode_n = ? AND generation = ?", p.ID, episodeN, generation).Count(&total)
		s.db.Model(&models.Scene{}).Where("project_id = ? AND episode_n = ? AND generation = ? AND status = ?", p.ID, episodeN, generation, "video_ready").Count(&ready)
		if total > 0 && ready == total {
			// 触发对白配音（异步，不阻塞合并）
			s.GenerateProjectDubs(&p)
			s.db.Model(&models.Project{}).Where("id = ? AND pipeline_stage = ?", p.ID, "videos").Update("pipeline_stage", "merge")
			go s.advancePipeline(p.ID)
			return
		}
		s.db.Model(&models.Scene{}).Where("project_id = ? AND episode_n = ? AND generation = ? AND status IN ?", p.ID, episodeN, generation,
			[]string{"video_creating", "video_pending", "video_running"}).Count(&waiting)
		if waiting == 0 {
			_, _ = s.GenerateAllVideos(&p, episodeN)
		}
	case "merge":
		generation, err := latestEpisodeGeneration(s.db, p.ID, episodeN)
		if err != nil {
			s.failPipeline(p.ID, err, true)
			return
		}
		var mt models.MergeTask
		err = s.db.Where("project_id = ? AND episode_n = ? AND generation = ?", p.ID, episodeN, generation).Order("id desc").First(&mt).Error
		if err == gorm.ErrRecordNotFound {
			var scenes []models.Scene
			if err := s.db.Where("project_id = ? AND episode_n = ? AND generation = ? AND status = ?", p.ID, episodeN, generation, "video_ready").Order("`order`").Find(&scenes).Error; err != nil {
				s.failPipeline(p.ID, err, true)
				return
			}
			ids := make([]uint, len(scenes))
			for i := range scenes {
				ids[i] = scenes[i].ID
			}
			if _, err := s.CreateMergeTask(&p, ids, true, true); err != nil {
				s.failPipeline(p.ID, err, true)
			}
			return
		}
		if err != nil {
			s.failPipeline(p.ID, err, true)
			return
		}
		switch mt.Status {
		case "pending":
			var scenes []models.Scene
			ids := sceneIDsFromStrings(strings.Split(mt.SceneOrder, ","))
			if err := s.db.Where("project_id = ? AND id IN ?", p.ID, ids).Order("`order`").Find(&scenes).Error; err != nil || len(scenes) != len(ids) {
				s.failPipeline(p.ID, fmt.Errorf("合并场景快照不可用"), true)
				return
			}
			go s.runAudioMerge(&p, &mt, scenes, true, true)
		case "failed":
			s.failPipeline(p.ID, fmt.Errorf("合并成片失败: %s", mt.Error), true)
		case "success":
			s.db.Model(&models.Project{}).Where("id = ?", p.ID).Updates(map[string]any{
				"status": "finished", "pipeline_stage": "finished", "auto_generate": false, "error": "",
			})
		}
	}
}

func (s *ProjectService) failPipeline(projectID uint, err error, auto bool) {
	updates := map[string]any{"error": err.Error(), "auto_generate": false}
	if auto {
		updates["status"] = "failed"
		updates["pipeline_stage"] = "failed"
	} else {
		updates["pipeline_stage"] = ""
		// 手工重新生成失败时保留原有分镜的可用状态。
		var count int64
		s.db.Model(&models.Scene{}).Where("project_id = ?", projectID).Count(&count)
		if count == 0 {
			updates["status"] = "failed"
		}
	}
	s.db.Model(&models.Project{}).Where("id = ?", projectID).Updates(updates)
	s.pushProject(nil)
}

// completeSceneCandidate publishes a successful normal or detached task and performs all
// downstream invalidation atomically with the Scene projection. Video frame extraction is
// deliberately started only after that transaction commits.
func (s *ProjectService) completeSceneCandidate(in SceneCandidateCapture) (*models.GenerationCandidate, error) {
	in.FinalizeDerived = true
	candidate, err := NewGenerationCandidateService(s.db).CaptureSceneSuccess(in)
	if err != nil {
		return nil, err
	}
	if in.MediaType == "video" && s.continuity != nil {
		go func(projectID, sceneID uint) {
			if _, err := s.continuity.ExtractFrameCandidates(projectID, sceneID, DefaultCandidateFrameCount); err != nil {
				log.Printf("[continuity] scene %d extract frames failed: %v", sceneID, err)
			}
		}(in.ProjectID, in.SceneID)
	}
	return candidate, nil
}

func (s *ProjectService) syncSceneImages() {
	var scenes []models.Scene
	if err := s.db.Where("status = ? AND image_task_id != ''", "image_pending").Find(&scenes).Error; err != nil {
		return
	}
	for i := range scenes {
		sc := &scenes[i]
		var task models.Task
		if err := s.db.Where("task_id = ?", sc.ImageTaskID).First(&task).Error; err != nil {
			s.failSceneImage(sc, sc.ImageToken, "Krea2 分镜任务不存在，请重新生成")
			continue
		}
		switch task.Status {
		case "failed", "cancelled":
			msg := strings.TrimSpace(task.Error)
			if msg == "" {
				msg = task.Status
			}
			s.failSceneImage(sc, sc.ImageToken, msg)
		case "success":
			file, _ := resultImageOf(&task)
			if file == "" || task.Port == nil || s.tasks == nil || s.upload == nil {
				s.failSceneImage(sc, sc.ImageToken, "MiniMax H3 分镜候选任务成功但未返回可用帧")
				continue
			}
			subfolder, filename := filepath.ToSlash(filepath.Dir(file)), filepath.Base(file)
			if subfolder == "." {
				subfolder = ""
			}
			data, err := NewComfyClient(s.tasks.comfyHostForPort(*task.Port), *task.Port).DownloadOutput(filename, subfolder, "output")
			if err != nil {
				s.failSceneImage(sc, sc.ImageToken, "读取 MiniMax H3 分镜候选帧失败: "+err.Error())
				continue
			}
			ext := filepath.Ext(file)
			if ext == "" {
				ext = detectImageExt(data)
			}
			name := fmt.Sprintf("scene_g%d_%d_%d%s", sc.Generation, sc.Order, time.Now().UnixNano(), ext)
			path, _, err := s.upload.SaveFile(fmt.Sprint(sc.ProjectID), "image", name, data)
			if err != nil {
				s.failSceneImage(sc, sc.ImageToken, "保存 Krea2 分镜图片失败: "+err.Error())
				continue
			}
			candidate, captureErr := s.completeSceneCandidate(SceneCandidateCapture{
				ProjectID: sc.ProjectID, SceneID: sc.ID, MediaType: "image", TaskID: task.TaskID,
				File: filepath.Base(path), Prompt: task.Prompt, References: task.InputsJSON,
				Params: task.ParamsJSON, Provenance: map[string]any{"template_id": task.TemplateID, "template_name": task.TemplateName, "port": task.Port, "gpu": task.GPUIndex, "result_files": task.ResultFiles},
				ParentCandidateID: task.ParentCandidateID, ExpectedTaskID: task.TaskID,
			})
			if captureErr != nil {
				if !strings.Contains(captureErr.Error(), "已过期") {
					log.Printf("[candidate] capture image scene=%d task=%s failed: %v", sc.ID, task.TaskID, captureErr)
				}
				continue
			}
			if candidate != nil {
				s.updateProjectStatus(sc.ProjectID)
				s.pushProject(nil)
			}
		}
	}
}

// RecoverCharacterPortrait 立即协调一个角色的标准像任务并尝试回写结果。
func (s *ProjectService) RecoverCharacterPortrait(ch *models.Character) (*models.Character, error) {
	if strings.TrimSpace(ch.PortraitTaskID) == "" {
		var current models.Character
		if err := s.db.First(&current, ch.ID).Error; err != nil {
			return nil, err
		}
		return &current, nil
	}
	// 先扫描磁盘，避免 task 记录缺失时 sync 清掉唯一可用于认领图片的 task_id。
	if err := s.recoverCharacterPortraitFromDisk(ch); err != nil {
		return nil, err
	}
	var diskCurrent models.Character
	if err := s.db.First(&diskCurrent, ch.ID).Error; err == nil && diskCurrent.Portrait != "" {
		return &diskCurrent, nil
	}
	if s.tasks != nil {
		s.tasks.RefreshTaskResult(ch.PortraitTaskID)
	}
	s.syncCharacterPortraits()
	var current models.Character
	if err := s.db.First(&current, ch.ID).Error; err != nil {
		return nil, err
	}
	if current.Portrait == "" && current.PortraitTaskID != "" {
		if err := s.recoverCharacterPortraitFromDisk(&current); err != nil {
			return nil, err
		}
		_ = s.db.First(&current, ch.ID).Error
	}
	return &current, nil
}

// recoverCharacterPortraitFromDisk 不依赖任务表/history/WS，直接认领服务器上已生成的图片。
func (s *ProjectService) recoverCharacterPortraitFromDisk(ch *models.Character) error {
	if s.tasks == nil || s.remote == nil || s.upload == nil || ch.PortraitTaskID == "" {
		return nil
	}
	roots := []string{filepath.Join(s.cfg.Comfy.ComfyDir, "output")}
	for gpu := 0; gpu < s.cfg.Comfy.GPUCount; gpu++ {
		roots = append(roots, filepath.Join(s.cfg.Comfy.ComfyDir, "output_workers", fmt.Sprintf("gpu%d", gpu)))
	}
	for _, root := range roots {
		for _, file := range s.tasks.findTaskOutputs(root, ch.PortraitTaskID) {
			if file["type"] != "images" {
				continue
			}
			source := filepath.Join(root, filepath.FromSlash(file["subfolder"]), file["filename"])
			reader, err := s.remote.Open(source)
			if err != nil {
				continue
			}
			data, readErr := io.ReadAll(reader)
			_ = reader.Close()
			if readErr != nil || len(data) == 0 {
				continue
			}
			ext := filepath.Ext(file["filename"])
			if ext == "" {
				ext = detectImageExt(data)
			}
			if ext == "" {
				ext = ".png"
			}
			name := fmt.Sprintf("char_%d_%d%s", ch.ID, time.Now().UnixNano(), ext)
			path, _, err := s.upload.SaveFile(fmt.Sprint(ch.ProjectID), "image", name, data)
			if err != nil {
				return err
			}
			res := s.db.Model(&models.Character{}).Where("id = ? AND portrait_task_id = ?", ch.ID, ch.PortraitTaskID).Updates(map[string]any{"portrait": filepath.Base(path), "portrait_task_id": "", "portrait_error": ""})
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected > 0 {
				s.pushProject(nil)
				return nil
			}
		}
	}
	return nil
}

// ResetCharacterPortraitTask 清除卡死的任务绑定，不删除已存在的标准像。
func (s *ProjectService) ResetCharacterPortraitTask(ch *models.Character) error {
	if ch.PortraitTaskID != "" && s.tasks != nil {
		_ = s.tasks.CancelTask(ch.PortraitTaskID)
	}
	return s.db.Model(&models.Character{}).Where("id = ? AND project_id = ?", ch.ID, ch.ProjectID).
		Updates(map[string]any{"portrait_task_id": "", "portrait_error": ""}).Error
}

func (s *ProjectService) syncCharacterPortraits() {
	var chars []models.Character
	if err := s.db.Where("portrait_task_id != ''").Find(&chars).Error; err != nil {
		return
	}
	changed := false
	for i := range chars {
		ch := &chars[i]
		// 即使任务记录丢失，也先用角色保存的 task_id 扫描实际输出。
		_ = s.recoverCharacterPortraitFromDisk(ch)
		var refreshed models.Character
		_ = s.db.First(&refreshed, ch.ID).Error
		if refreshed.Portrait != "" || refreshed.PortraitTaskID == "" {
			changed = true
			continue
		}
		var task models.Task
		if err := s.db.Where("task_id = ?", ch.PortraitTaskID).First(&task).Error; err != nil {
			s.db.Model(ch).Where("portrait_task_id = ?", ch.PortraitTaskID).Updates(map[string]any{"portrait_task_id": "", "portrait_error": "标准像任务记录不存在，且未在输出目录找到图片"})
			changed = true
			continue
		}
		// 即使本地误标失败/取消，也先查history和输出目录找回实际已生成的图片。
		needsRefresh := task.Status != "success" || strings.TrimSpace(task.ResultFiles) == "" || task.ResultFiles == "[]"
		if s.tasks != nil && needsRefresh {
			s.tasks.RefreshTaskResult(task.TaskID)
			_ = s.db.Where("task_id = ?", ch.PortraitTaskID).First(&task).Error
		}
		switch task.Status {
		case "failed", "cancelled":
			msg := task.Error
			if msg == "" {
				msg = task.Status
			}
			s.db.Model(ch).Where("portrait_task_id = ?", task.TaskID).Updates(map[string]any{"portrait_task_id": "", "portrait_error": msg})
			changed = true
		case "success":
			file, _ := resultImageOf(&task)
			if file == "" || task.Port == nil || s.upload == nil {
				s.db.Model(ch).Where("portrait_task_id = ?", task.TaskID).Updates(map[string]any{"portrait_task_id": "", "portrait_error": "Krea2 任务成功但未返回可用图片"})
				changed = true
				continue
			}
			subfolder, filename := filepath.ToSlash(filepath.Dir(file)), filepath.Base(file)
			if subfolder == "." {
				subfolder = ""
			}
			client := NewComfyClient(s.tasks.comfyHostForPort(*task.Port), *task.Port)
			data, downloadErr := client.DownloadOutput(filename, subfolder, "output")
			if downloadErr != nil {
				s.db.Model(ch).Where("portrait_task_id = ?", task.TaskID).Update("portrait_error", "读取 Krea2 图片失败: "+downloadErr.Error())
				changed = true
				continue
			}
			name := fmt.Sprintf("char_%d_%d%s", ch.ID, time.Now().UnixNano(), filepath.Ext(file))
			path, _, saveErr := s.upload.SaveFile(fmt.Sprint(ch.ProjectID), "image", name, data)
			if saveErr != nil {
				continue
			}
			_, applied, applyErr := updateSelectedAsset(s.db, ch.ProjectID, VariantCharacterPortrait, ch.ID, filepath.Base(path), task.Prompt, "krea2_task:"+task.TaskID, map[string]any{"portrait_task_id": "", "portrait_error": ""}, "portrait_task_id", task.TaskID)
			if applyErr != nil {
				log.Printf("[character %d] apply portrait result failed: %v", ch.ID, applyErr)
				continue
			}
			changed = changed || applied
		}
	}
	if changed {
		s.pushProject(nil)
	}
}

func resultImageOf(task *models.Task) (string, *int) {
	var files []map[string]string
	if json.Unmarshal([]byte(task.ResultFiles), &files) != nil {
		return "", nil
	}
	for _, f := range files {
		name := f["filename"]
		if f["type"] == "images" && !isVideoExt(name) {
			if f["subfolder"] != "" {
				return f["subfolder"] + "/" + name, task.GPUIndex
			}
			return name, task.GPUIndex
		}
	}
	return "", nil
}

func (s *ProjectService) syncSceneVideos() {
	var scenes []models.Scene
	if err := s.db.Where("status IN ?", []string{"video_pending", "video_running"}).
		Find(&scenes).Error; err != nil || len(scenes) == 0 {
		return
	}
	changed := false
	for i := range scenes {
		sc := &scenes[i]
		if sc.VideoTaskID == "" {
			s.db.Model(sc).Updates(map[string]any{"status": "image_ready", "error": "视频任务缺失"})
			changed = true
			continue
		}
		var task models.Task
		if err := s.db.Where("task_id = ?", sc.VideoTaskID).First(&task).Error; err != nil {
			continue
		}
		switch task.Status {
		case "pending":
			// Startup task recovery owns execution; retain the binding.
		case "running", "queued":
			if sc.Status != "video_running" {
				s.db.Model(sc).Updates(map[string]any{"status": "video_running"})
				changed = true
			}
		case "success":
			file, gpu := resultVideoOf(&task)
			if file == "" || gpu == nil || task.Port == nil || s.upload == nil {
				s.retryOrFailVideo(sc, &task, "任务成功但未返回视频文件、端口或 GPU 信息")
				changed = true
				continue
			}
			subfolder, filename := filepath.ToSlash(filepath.Dir(file)), filepath.Base(file)
			if subfolder == "." {
				subfolder = ""
			}
			data, err := NewComfyClient(s.tasks.comfyHostForPort(*task.Port), *task.Port).DownloadOutput(filename, subfolder, "output")
			if err != nil {
				s.retryOrFailVideo(sc, &task, "下载生成视频失败: "+err.Error())
				changed = true
				continue
			}
			localName := fmt.Sprintf("scene_video_g%d_%d_%d%s", sc.Generation, sc.Order, time.Now().UnixNano(), filepath.Ext(file))
			localPath, _, err := s.upload.SaveFile(fmt.Sprint(sc.ProjectID), "video", localName, data)
			if err != nil {
				s.retryOrFailVideo(sc, &task, "保存生成视频失败: "+err.Error())
				changed = true
				continue
			}
			_, captureErr := s.completeSceneCandidate(SceneCandidateCapture{
				ProjectID: sc.ProjectID, SceneID: sc.ID, MediaType: "video", TaskID: task.TaskID,
				File: file, VideoInputFile: filepath.Base(localPath), VideoGPU: gpu, Prompt: task.Prompt,
				References: task.InputsJSON, Params: task.ParamsJSON,
				Provenance:        map[string]any{"template_id": task.TemplateID, "template_name": task.TemplateName, "port": task.Port, "gpu": task.GPUIndex, "result_files": task.ResultFiles},
				ParentCandidateID: task.ParentCandidateID, ExpectedTaskID: task.TaskID,
			})
			if captureErr != nil {
				if !strings.Contains(captureErr.Error(), "已过期") {
					log.Printf("[candidate] capture video scene=%d task=%s failed: %v", sc.ID, task.TaskID, captureErr)
				}
				continue
			}
			changed = true
		case "failed", "cancelled":
			msg := task.Error
			if msg == "" {
				msg = task.Status
			}
			s.retryOrFailVideo(sc, &task, msg)
			changed = true
		}
	}
	if changed {
		s.pushProject(nil)
		s.updateProjectStatusForAll()
	}
}

// syncDetachedCandidateRetries publishes successful explicit retries. Unlike a branch, a retry
// is not bound to Scene while running, so the current output remains usable until this succeeds.
func (s *ProjectService) syncDetachedCandidateRetries() {
	if s.tasks == nil || s.upload == nil {
		return
	}
	var tasks []models.Task
	if err := s.db.Where("parent_candidate_id IS NOT NULL AND status = ?", "success").Find(&tasks).Error; err != nil {
		return
	}
	for i := range tasks {
		task := &tasks[i]
		var already int64
		if s.db.Model(&models.GenerationCandidate{}).Where("task_id = ?", task.TaskID).Count(&already).Error != nil || already > 0 {
			continue
		}
		// Branches and automatic task retries are Scene-bound and handled by the normal synchronizers.
		var bound int64
		if s.db.Model(&models.Scene{}).Where("image_task_id = ? OR video_task_id = ?", task.TaskID, task.TaskID).Count(&bound).Error != nil || bound > 0 {
			continue
		}
		var parent models.GenerationCandidate
		if err := s.db.First(&parent, *task.ParentCandidateID).Error; err != nil || parent.Stale || parent.EntityType != "scene" {
			continue
		}
		capture := SceneCandidateCapture{
			ProjectID: parent.ProjectID, SceneID: parent.EntityID, MediaType: parent.MediaType,
			TaskID: task.TaskID, Prompt: task.Prompt, References: task.InputsJSON, Params: task.ParamsJSON,
			Provenance:        map[string]any{"source": "candidate_retry", "template_id": task.TemplateID, "template_name": task.TemplateName, "port": task.Port, "gpu": task.GPUIndex, "result_files": task.ResultFiles},
			ParentCandidateID: task.ParentCandidateID, RequireParentFresh: true,
		}
		if parent.MediaType == "image" {
			file, _ := resultImageOf(task)
			if file == "" || task.Port == nil {
				continue
			}
			subfolder, filename := filepath.ToSlash(filepath.Dir(file)), filepath.Base(file)
			if subfolder == "." {
				subfolder = ""
			}
			data, err := NewComfyClient(s.tasks.comfyHostForPort(*task.Port), *task.Port).DownloadOutput(filename, subfolder, "output")
			if err != nil {
				continue
			}
			ext := filepath.Ext(file)
			if ext == "" {
				ext = detectImageExt(data)
			}
			name := fmt.Sprintf("scene_retry_%d_%d%s", parent.EntityID, time.Now().UnixNano(), ext)
			path, _, err := s.upload.SaveFile(fmt.Sprint(parent.ProjectID), "image", name, data)
			if err != nil {
				continue
			}
			capture.File = filepath.Base(path)
		} else if parent.MediaType == "video" {
			file, gpu := resultVideoOf(task)
			if file == "" || gpu == nil || task.Port == nil {
				continue
			}
			subfolder, filename := filepath.ToSlash(filepath.Dir(file)), filepath.Base(file)
			if subfolder == "." {
				subfolder = ""
			}
			data, err := NewComfyClient(s.tasks.comfyHostForPort(*task.Port), *task.Port).DownloadOutput(filename, subfolder, "output")
			if err != nil {
				continue
			}
			name := fmt.Sprintf("scene_video_retry_%d_%d%s", parent.EntityID, time.Now().UnixNano(), filepath.Ext(file))
			path, _, err := s.upload.SaveFile(fmt.Sprint(parent.ProjectID), "video", name, data)
			if err != nil {
				continue
			}
			capture.File, capture.VideoInputFile, capture.VideoGPU = file, filepath.Base(path), gpu
		} else {
			continue
		}
		if _, err := s.completeSceneCandidate(capture); err == nil {
			s.pushProject(nil)
		}
	}
}

func (s *ProjectService) retryOrFailVideo(sc *models.Scene, failedTask *models.Task, msg string) {
	if sc.VideoRetries < 2 && s.tasks != nil && failedTask != nil {
		retries := sc.VideoRetries + 1
		retry, err := s.tasks.CloneTaskSnapshot(failedTask.TaskID, failedTask.ParentCandidateID)
		if err == nil {
			bound := s.db.Model(&models.Scene{}).
				Where("id = ? AND video_task_id = ?", sc.ID, failedTask.TaskID).
				Updates(map[string]any{"status": "video_pending", "video_retries": retries, "error": "", "video_task_id": retry.TaskID, "video_file": "", "video_input_file": "", "video_gpu": nil})
			if bound.Error == nil && bound.RowsAffected == 1 {
				go func() { _ = s.tasks.Execute(retry.TaskID) }()
				log.Printf("[project %d] scene %d video 失败自动重试 %d/2: %s", sc.ProjectID, sc.Order, retries, msg)
				return
			}
			_ = s.db.Delete(retry).Error
		}
	}
	s.db.Model(sc).Updates(map[string]any{
		"status": "failed", "error": "视频生成失败: " + msg,
	})
}

// updateProjectStatusForAll 扫描含活动场景的项目并推进状态
func (s *ProjectService) updateProjectStatusForAll() {
	var projects []models.Project
	if err := s.db.Where("status IN ?", []string{"draft", "script_done", "producing", "ready", "failed"}).
		Find(&projects).Error; err != nil {
		return
	}
	for _, p := range projects {
		s.updateProjectStatus(p.ID)
	}
}

// updateProjectStatus 根据场景完成度推进项目状态：
// 全部视频就绪 → ready；有正在进行的制作 → producing；全部失败 → failed
func (s *ProjectService) updateProjectStatus(projectID uint) {
	var scenes []models.Scene
	if err := s.db.Where("project_id = ?", projectID).Find(&scenes).Error; err != nil || len(scenes) == 0 {
		return
	}
	ready, failed, active := 0, 0, 0
	for _, sc := range scenes {
		switch sc.Status {
		case "video_ready":
			ready++
		case "failed":
			failed++
		default:
			active++
		}
	}
	status := ""
	switch {
	case ready == len(scenes):
		status = "ready"
	case failed == len(scenes):
		status = "failed"
	case active > 0 || ready > 0:
		status = "producing"
	}
	if status != "" {
		s.db.Model(&models.Project{}).Where("id = ?", projectID).
			Where("status != ? AND COALESCE(pipeline_stage, '') != ?", "finished", "failed").Update("status", status)
	}
}

// resultVideoOf 从任务结果中提取第一个视频文件（subfolder/filename）与 GPU
// isVideoExt 判断视频扩展名（minimax SaveVideo 输出 type 标为 images，但文件实为 mp4）
func isVideoExt(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".mp4", ".webm", ".mov", ".mkv", ".gif":
		return true
	}
	return false
}

// resultVideoOf 从任务结果中提取第一个视频文件（subfolder/filename）与 GPU。
// minimax H3 的 SaveVideo 在 ComfyUI outputs 里 type 标为 "images"（文件实为 mp4），故按扩展名兼容。
func resultVideoOf(task *models.Task) (string, *int) {
	var files []map[string]string
	if err := json.Unmarshal([]byte(task.ResultFiles), &files); err != nil {
		return "", nil
	}
	for _, f := range files {
		// ComfyUI 的不同视频保存节点会把 mp4 放在 videos、images 或 gifs 字段；
		// 文件扩展名才是稳定信号，不能按输出字段类型排除有效视频。
		if f["type"] == "videos" || isVideoExt(f["filename"]) {
			sub, name := f["subfolder"], f["filename"]
			if sub != "" {
				return sub + "/" + name, task.GPUIndex
			}
			return name, task.GPUIndex
		}
	}
	return "", nil
}

// ---------- 视频合并（ffmpeg concat） ----------

type MergeAudioOptions struct {
	NativeVolume   float64
	DialogueVolume float64
	BGMVolume      float64
	DialogueMix    bool
}

func mergeAudioOptions(native, dialogue, bgm *float64) (MergeAudioOptions, error) {
	options := MergeAudioOptions{NativeVolume: 1, DialogueVolume: 1, BGMVolume: 1, DialogueMix: dialogue != nil}
	for value, target := range map[*float64]*float64{native: &options.NativeVolume, dialogue: &options.DialogueVolume, bgm: &options.BGMVolume} {
		if value != nil {
			if *value < 0 || *value > 4 {
				return options, fmt.Errorf("合并音量需在 0~4")
			}
			*target = *value
		}
	}
	return options, nil
}

type sceneVideoFingerprint struct {
	SceneID        uint   `json:"scene_id"`
	TaskID         string `json:"task_id"`
	File           string `json:"file"`
	GPU            int    `json:"gpu"`
	VideoInputFile string `json:"input_file"`
}

type mergeDialogueFingerprint struct {
	ID, SceneID                        uint
	Order                              int
	Character, SpeechType, Text, Voice string
	H3VoiceDescription                 string
	Position, Offset, Speed, Pitch     float64
	Volume                             float64
	Emotion, Delivery, AudioFile       string
	AudioHash, Status                  string
	AudioStale                         bool
}

type mergeAudioLayerFingerprint struct {
	ID, ProjectID              uint
	EpisodeN                   int
	SceneID                    *uint
	Kind, File                 string
	StartTime, EndTime, Volume float64
	FadeIn, FadeOut            float64
	Loop, Muted, Stale         bool
	Status                     string
}

type mergeInputSnapshot struct {
	Version           int                          `json:"version"`
	EpisodeGeneration uint                         `json:"episode_generation"`
	Videos            []sceneVideoFingerprint      `json:"videos"`
	Dialogues         []mergeDialogueFingerprint   `json:"dialogues"`
	AudioLayers       []mergeAudioLayerFingerprint `json:"audio_layers"`
	Dub               bool                         `json:"dub"`
	Subtitles         bool                         `json:"subtitles"`
	NativeVolume      float64                      `json:"native_volume"`
	DialogueVolume    float64                      `json:"dialogue_volume"`
	BGMVolume         float64                      `json:"bgm_volume"`
	DialogueMix       bool                         `json:"dialogue_mix"`
}

func fingerprintSceneVideo(scene models.Scene) (sceneVideoFingerprint, error) {
	if scene.VideoGPU == nil {
		return sceneVideoFingerprint{}, fmt.Errorf("场景 %d 的视频 GPU 信息缺失", scene.Order)
	}
	return sceneVideoFingerprint{SceneID: scene.ID, TaskID: scene.VideoTaskID, File: scene.VideoFile, GPU: *scene.VideoGPU, VideoInputFile: scene.VideoInputFile}, nil
}

func (s *ProjectService) createMergeTaskRecord(p *models.Project, sceneIDs []uint, dub, subtitles bool, audio MergeAudioOptions) (*models.MergeTask, []models.Scene, error) {
	if len(sceneIDs) < 2 {
		return nil, nil, fmt.Errorf("请至少选择 2 个场景进行合并")
	}
	seen := make(map[uint]struct{}, len(sceneIDs))
	for _, id := range sceneIDs {
		if id == 0 {
			return nil, nil, fmt.Errorf("场景 ID 无效")
		}
		if _, exists := seen[id]; exists {
			return nil, nil, fmt.Errorf("场景 ID %d 重复", id)
		}
		seen[id] = struct{}{}
	}

	var mt models.MergeTask
	var scenes []models.Scene
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var current models.Project
		if err := tx.First(&current, p.ID).Error; err != nil {
			return err
		}
		episodeN := 0
		fingerprints := make([]sceneVideoFingerprint, 0, len(sceneIDs))
		for _, id := range sceneIDs {
			var scene models.Scene
			if err := tx.Where("id = ? AND project_id = ?", id, current.ID).First(&scene).Error; err != nil {
				return fmt.Errorf("场景 %d 不属于项目", id)
			}
			if scene.Status != "video_ready" || scene.VideoFile == "" || scene.VideoGPU == nil {
				return fmt.Errorf("场景 %d 的视频尚未完成，请先生成视频", scene.Order)
			}
			currentEpisode := normalizeRevisionEpisode(scene.EpisodeN)
			if episodeN == 0 {
				episodeN = currentEpisode
			} else if currentEpisode != episodeN {
				return fmt.Errorf("不能合并不同集的场景：第%d集与第%d集", episodeN, currentEpisode)
			}
			fingerprint, err := fingerprintSceneVideo(scene)
			if err != nil {
				return err
			}
			fingerprints = append(fingerprints, fingerprint)
			scenes = append(scenes, scene)
		}
		episodeGeneration, err := latestEpisodeGeneration(tx, current.ID, episodeN)
		if err != nil {
			return err
		}
		for _, scene := range scenes {
			if scene.Generation != episodeGeneration {
				return fmt.Errorf("场景 %d 不属于第%d集当前版本", scene.ID, episodeN)
			}
		}
		var active int64
		if err := tx.Model(&models.MergeTask{}).Where("project_id = ? AND episode_n = ? AND status IN ?", current.ID, episodeN, []string{"pending", "running"}).Count(&active).Error; err != nil {
			return err
		}
		if active > 0 {
			return fmt.Errorf("第%d集已有进行中的合并任务，请等待完成", episodeN)
		}
		snapshot, err := s.captureMergeInputSnapshot(tx, current.ID, episodeN, episodeGeneration, fingerprints, sceneIDs, dub, subtitles, audio)
		if err != nil {
			return err
		}
		encoded, err := json.Marshal(fingerprints)
		if err != nil {
			return err
		}
		ids := make([]string, len(sceneIDs))
		for i, id := range sceneIDs {
			ids[i] = strconv.FormatUint(uint64(id), 10)
		}
		mt = models.MergeTask{ProjectID: current.ID, EpisodeN: episodeN, Title: fmt.Sprintf("第%d集 · %s", episodeN, current.Title), SceneOrder: strings.Join(ids, ","), SceneVideoFingerprints: string(encoded), InputSnapshot: string(snapshot), Status: "pending", Generation: episodeGeneration, RequestedDub: dub, RequestedSubtitles: subtitles, NativeVolume: audio.NativeVolume, DialogueVolume: audio.DialogueVolume, BGMVolume: audio.BGMVolume, DialogueMix: audio.DialogueMix}
		return tx.Create(&mt).Error
	})
	if err != nil {
		return nil, nil, err
	}
	return &mt, scenes, nil
}

func (s *ProjectService) validateMergeInputs(tx *gorm.DB, mt *models.MergeTask) ([]models.Scene, error) {
	var persisted models.MergeTask
	if err := tx.Where("id = ? AND project_id = ?", mt.ID, mt.ProjectID).First(&persisted).Error; err != nil {
		return nil, err
	}
	mt = &persisted
	episodeGeneration, err := latestEpisodeGeneration(tx, mt.ProjectID, mt.EpisodeN)
	if err != nil {
		return nil, err
	}
	if episodeGeneration != mt.Generation {
		return nil, fmt.Errorf("合并任务已过期：第%d集版本已变化", mt.EpisodeN)
	}
	var fingerprints []sceneVideoFingerprint
	if err := json.Unmarshal([]byte(mt.SceneVideoFingerprints), &fingerprints); err != nil || len(fingerprints) == 0 {
		return nil, fmt.Errorf("合并任务缺少有效的视频输入快照")
	}
	orderedIDs := sceneIDsFromStrings(strings.Split(mt.SceneOrder, ","))
	if len(orderedIDs) != len(fingerprints) {
		return nil, fmt.Errorf("合并任务的视频输入快照不完整")
	}
	scenes := make([]models.Scene, 0, len(fingerprints))
	for i, expected := range fingerprints {
		if expected.SceneID != orderedIDs[i] {
			return nil, fmt.Errorf("合并任务的视频输入顺序已损坏")
		}
		var scene models.Scene
		if err := tx.Where("id = ? AND project_id = ? AND generation = ? AND episode_n = ?", expected.SceneID, mt.ProjectID, mt.Generation, mt.EpisodeN).First(&scene).Error; err != nil {
			return nil, fmt.Errorf("合并任务已过期：场景 %d 已变化", expected.SceneID)
		}
		actual, err := fingerprintSceneVideo(scene)
		if err != nil || actual != expected || scene.Status != "video_ready" {
			return nil, fmt.Errorf("合并任务已过期：场景 %d 的视频已变化", expected.SceneID)
		}
		scenes = append(scenes, scene)
	}
	actualSnapshot, err := s.captureMergeInputSnapshot(tx, mt.ProjectID, mt.EpisodeN, mt.Generation, fingerprints, orderedIDs, mt.RequestedDub, mt.RequestedSubtitles, MergeAudioOptions{NativeVolume: mt.NativeVolume, DialogueVolume: mt.DialogueVolume, BGMVolume: mt.BGMVolume, DialogueMix: mt.DialogueMix})
	if err != nil {
		return nil, err
	}
	if mt.InputSnapshot == "" || !bytes.Equal([]byte(mt.InputSnapshot), actualSnapshot) {
		return nil, fmt.Errorf("合并任务已过期：对白、音频层、字幕或合并设置已变化")
	}
	return scenes, nil
}

func (s *ProjectService) completeMergeIfCurrent(mt *models.MergeTask, outputFile string, subtitles bool) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if _, err := s.validateMergeInputs(tx, mt); err != nil {
			return err
		}
		result := tx.Model(&models.MergeTask{}).Where("id = ? AND project_id = ? AND generation = ? AND status = ?", mt.ID, mt.ProjectID, mt.Generation, "running").Updates(map[string]any{
			"status": "success", "output_file": outputFile, "subtitle": subtitles, "error": "",
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("合并任务状态已变化，结果已隔离")
		}
		return nil
	})
}

// CreateMergeTask preserves the pre-audio-layer defaults for internal and legacy callers.
func (s *ProjectService) CreateMergeTask(p *models.Project, sceneIDs []uint, dub, subtitles bool) (*models.MergeTask, error) {
	return s.CreateMergeTaskWithAudio(p, sceneIDs, dub, subtitles, MergeAudioOptions{NativeVolume: 1, DialogueVolume: 1, BGMVolume: 1})
}

func (s *ProjectService) CreateMergeTaskWithAudio(p *models.Project, sceneIDs []uint, dub, subtitles bool, audio MergeAudioOptions) (*models.MergeTask, error) {
	// All production entry points share the mix-aware implementation so ready audio layers,
	// dialogue and native audio cannot be silently ignored by one endpoint.
	return s.createAudioMergeTask(p, sceneIDs, dub, subtitles, audio)
}

func (s *ProjectService) mediaPath(parts ...string) string {
	if s.remote != nil && s.remote.Enabled() {
		all := append([]string{filepath.ToSlash(s.cfg.Comfy.ComfyDir)}, parts...)
		return path.Join(all...)
	}
	all := append([]string{s.cfg.Comfy.ComfyDir}, parts...)
	return filepath.Join(all...)
}

func (s *ProjectService) audioLayerMediaPath(projectID uint, file string) (string, error) {
	clean := filepath.Clean(strings.TrimSpace(file))
	if clean == "." || filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
		return "", fmt.Errorf("音频层文件路径无效")
	}
	return s.mediaPath("input", strconv.FormatUint(uint64(projectID), 10), clean), nil
}

func sceneIDsFromStrings(values []string) []uint {
	ids := make([]uint, 0, len(values))
	for _, value := range values {
		id, err := strconv.ParseUint(value, 10, 32)
		if err == nil && id > 0 {
			ids = append(ids, uint(id))
		}
	}
	return ids
}

func localFFmpegPath() (string, error) {
	if p, err := exec.LookPath("ffmpeg"); err == nil {
		return p, nil
	}
	for _, p := range []string{`D:\tools\ffmpeg\bin\ffmpeg.exe`, `C:\ffmpeg\bin\ffmpeg.exe`} {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("Windows 本机未找到 ffmpeg.exe，请将其加入 PATH")
}

func runLocalProgram(program string, args []string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, program, args...)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return output.String(), fmt.Errorf("命令执行超时")
	}
	if err != nil {
		return output.String(), fmt.Errorf("%s: %w: %s", program, err, strings.TrimSpace(output.String()))
	}
	return output.String(), nil
}

// runMerge 后台执行合并：Windows 本机直接执行 ffmpeg.exe；Linux/SSH 使用远端 shell。
// dub=true 保留各场景 H3 原音轨（对白已在生成时同步进视频），subtitles=true 按场景时长均分生成 SRT 字幕并烧录进画面。
func (s *ProjectService) runMerge(p *models.Project, mt *models.MergeTask, dub, subtitles bool) {
	claim := s.db.Model(&models.MergeTask{}).Where("id = ? AND project_id = ? AND generation = ? AND status = ?", mt.ID, mt.ProjectID, mt.Generation, "pending").Update("status", "running")
	if claim.Error != nil || claim.RowsAffected == 0 {
		return
	}
	if _, err := s.validateMergeInputs(s.db, mt); err != nil {
		s.failMerge(mt, p, err.Error())
		return
	}

	ids := strings.Split(mt.SceneOrder, ",")
	inputs := make([]string, 0, len(ids))
	inputPaths := make([]string, 0, len(ids))
	filters := make([]string, 0, len(ids))
	audioCount := 0
	videoDurs := make([]sceneVideo, 0, len(ids))
	scenes := make([]models.Scene, 0, len(ids))
	for i, idStr := range ids {
		id, _ := strconv.Atoi(idStr)
		var sc models.Scene
		if err := s.db.Where("id = ? AND project_id = ? AND generation = ? AND episode_n = ?", id, mt.ProjectID, mt.Generation, mt.EpisodeN).First(&sc).Error; err != nil || sc.VideoFile == "" || sc.VideoGPU == nil {
			s.failMerge(mt, p, fmt.Sprintf("场景 %d 视频信息缺失", id))
			return
		}
		abs := s.mediaPath("output_workers", fmt.Sprintf("gpu%d", *sc.VideoGPU), filepath.FromSlash(sc.VideoFile))
		if (s.remote == nil || !s.remote.Enabled()) && sc.VideoInputFile != "" {
			// 本机优先使用已下载的项目视频副本。切换过运行目录时副本可能不在
			// 当前 ComfyUI input 下，此时从原任务的 /view 重新下载恢复。
			abs = filepath.Join(s.upload.InputDir(), fmt.Sprint(sc.ProjectID), filepath.FromSlash(sc.VideoInputFile))
			if _, err := os.Stat(abs); err != nil {
				var task models.Task
				if dbErr := s.db.Where("task_id = ?", sc.VideoTaskID).First(&task).Error; dbErr != nil || task.Port == nil {
					s.failMerge(mt, p, fmt.Sprintf("场景 %d 的本地视频副本不存在，且原任务不可恢复", sc.Order))
					return
				}
				subfolder, filename := filepath.ToSlash(filepath.Dir(sc.VideoFile)), filepath.Base(sc.VideoFile)
				if subfolder == "." {
					subfolder = ""
				}
				data, downloadErr := NewComfyClient(s.tasks.comfyHostForPort(*task.Port), *task.Port).DownloadOutput(filename, subfolder, "output")
				if downloadErr != nil {
					s.failMerge(mt, p, "重新下载场景视频失败: "+downloadErr.Error())
					return
				}
				if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
					s.failMerge(mt, p, err.Error())
					return
				}
				if err := os.WriteFile(abs, data, 0o644); err != nil {
					s.failMerge(mt, p, "恢复场景视频失败: "+err.Error())
					return
				}
			}
		}
		inputs = append(inputs, "-i", shellQuote(abs))
		inputPaths = append(inputPaths, abs)
		hasAudio, err := s.remoteFileHasAudio(abs)
		if err != nil {
			s.failMerge(mt, p, err.Error())
			return
		}
		if hasAudio {
			audioCount++
		}
		filters = append(filters, fmt.Sprintf("[%d:v]", i))
		dur := normalizeSceneDuration(sc.Duration)
		if mi, err := s.remote.ProbeMedia(abs); err == nil && mi.Duration > 0 {
			dur = mi.Duration
		}
		videoDurs = append(videoDurs, sceneVideo{abs: abs, dur: dur})
		scenes = append(scenes, sc)
	}

	// Audio-layer and dialogue inputs are opt-in additions. The legacy path remains byte-for-byte
	// equivalent at default volumes when no ready layer exists and dialogue_volume is omitted.
	var layers []models.AudioLayer
	_ = s.db.Where("project_id = ? AND episode_n = ? AND status = ? AND muted = ? AND file != ''", p.ID, mt.EpisodeN, "ready", false).
		Order("start_time, id").Find(&layers).Error
	audioInputIndex := len(inputPaths)
	layerInputIndexes := make([]int, 0, len(layers))
	validLayers := make([]models.AudioLayer, 0, len(layers))
	for _, layer := range layers {
		abs, err := s.audioLayerMediaPath(p.ID, layer.File)
		if err != nil {
			s.failMerge(mt, p, err.Error())
			return
		}
		inputs = append(inputs, "-i", shellQuote(abs))
		inputPaths = append(inputPaths, abs)
		layerInputIndexes = append(layerInputIndexes, audioInputIndex)
		audioInputIndex++
		validLayers = append(validLayers, layer)
	}
	var dialogueSegments []dubSegment
	if mt.DialogueMix && mt.DialogueVolume > 0 {
		segments, _, ready, err := s.buildDubTimeline(p, mt.EpisodeN, sceneIDsFromStrings(ids), videoDurs)
		if err != nil {
			s.failMerge(mt, p, err.Error())
			return
		}
		if ready {
			for _, segment := range segments {
				if segment.AudioAbs == "" {
					continue
				}
				inputs = append(inputs, "-i", shellQuote(segment.AudioAbs))
				inputPaths = append(inputPaths, segment.AudioAbs)
				dialogueSegments = append(dialogueSegments, segment)
				audioInputIndex++
			}
		}
	}

	outName := fmt.Sprintf("merged/%s_merged_%d.mp4", projectFileTag(p), mt.ID)
	outAbs := s.mediaPath("output_workers", "gpu0", filepath.FromSlash(outName))
	srtName := strings.TrimSuffix(outName, ".mp4") + ".srt"
	srtAbs := s.mediaPath("output_workers", "gpu0", filepath.FromSlash(srtName))

	// 1) 生成 SRT 字幕：按场景顺序与时长均分对白（无 TTS 依赖）
	sceneIDs := make([]uint, len(ids))
	for i, idStr := range ids {
		id, _ := strconv.Atoi(idStr)
		sceneIDs[i] = uint(id)
	}
	if subtitles {
		if err := s.writeMergeSRT(p, mt, srtAbs, scenes); err != nil {
			s.failMerge(mt, p, "生成字幕失败: "+err.Error())
			return
		}
	}

	// 2) 组装 ffmpeg：视频 concat（保留原音轨）+ 烧录字幕
	// 视频流 concat
	concatHead := ""
	for i := range ids {
		concatHead += fmt.Sprintf("[%d:v]", i)
	}
	ffilters := []string{concatHead + fmt.Sprintf("concat=n=%d:v=1:a=0[vc]", len(ids))}
	// 统一缩放到首个场景尺寸（兼容不同分辨率）
	scale := ""
	if len(ids) > 0 {
		if mi, err := s.remote.ProbeMedia(videoDurs[0].abs); err == nil && mi.Width > 0 && mi.Height > 0 {
			scale = fmt.Sprintf(",scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2", mi.Width, mi.Height, mi.Width, mi.Height)
		}
	}
	// subtitles filter 烧录 SRT（仅 subtitles 开关开启时）；不使用单引号（避免与 shellQuote 冲突），转义特殊字符
	subFilter := ""
	if subtitles {
		subFilter = fmt.Sprintf(",subtitles=filename=%s:force_style=FontName\\=Noto Sans CJK SC\\,FontSize\\=14\\,MarginV\\=28", escapeFilterPath(srtAbs))
	}
	ffilters = append(ffilters, fmt.Sprintf("[vc]fps=24%s%s[v]", scale, subFilter))

	mapArgs := `-map "[v]"`
	if dub && audioCount == len(ids) && audioCount > 0 {
		// 所有场景都有原音轨（H3 同步人声）：音频一并 concat 保留
		audioHead := ""
		for i := range ids {
			audioHead += fmt.Sprintf("[%d:a]", i)
		}
		ffilters = append(ffilters, audioHead+fmt.Sprintf("concat=n=%d:v=0:a=1[a]", len(ids)))
		mapArgs = `-map "[v]" -map "[a]" -c:a aac -b:a 192k`
	}
	filterComplex := strings.Join(ffilters, ";")

	if s.remote == nil || !s.remote.Enabled() {
		if err := os.MkdirAll(filepath.Dir(outAbs), 0o755); err != nil {
			s.failMerge(mt, p, err.Error())
			return
		}
		ffmpeg, err := localFFmpegPath()
		if err != nil {
			s.failMerge(mt, p, err.Error())
			return
		}
		args := []string{"-y"}
		for _, input := range inputPaths {
			args = append(args, "-i", input)
		}
		args = append(args, "-filter_complex", filterComplex, "-map", "[v]")
		if dub && audioCount == len(ids) && audioCount > 0 {
			args = append(args, "-map", "[a]", "-c:a", "aac", "-b:a", "192k")
		}
		args = append(args, "-c:v", "libx264", "-crf", "18", "-preset", "medium", "-pix_fmt", "yuv420p", "-movflags", "+faststart", outAbs)
		if _, err := runLocalProgram(ffmpeg, args, 30*time.Minute); err != nil {
			s.failMerge(mt, p, err.Error())
			return
		}
	} else {
		cmd := fmt.Sprintf("%s; mkdir -p %s; $FF -y %s -filter_complex %s %s -c:v libx264 -crf 18 -preset medium -pix_fmt yuv420p -movflags +faststart %s", ffmpegResolveCmd, shellQuote(path.Dir(outAbs)), strings.Join(inputs, " "), shellQuote(filterComplex), mapArgs, shellQuote(outAbs))
		log.Printf("[merge %d] %s", mt.ID, cmd)
		if _, err := s.remote.RunTimeout(cmd, 30*time.Minute); err != nil {
			s.failMerge(mt, p, err.Error())
			return
		}
	}
	if err := s.completeMergeIfCurrent(mt, outName, subtitles); err != nil {
		s.failMerge(mt, p, "合并结果已隔离: "+err.Error())
		return
	}
	s.finishMergeProject(p)
	log.Printf("[merge %d] 完成: %s", mt.ID, outName)
}

// writeMergeSRT 按场景顺序/时长均分对白生成 SRT 字幕（无 TTS 依赖，字幕对齐场景时间轴）
func (s *ProjectService) writeMergeSRT(p *models.Project, mt *models.MergeTask, srtAbs string, scenes []models.Scene) error {
	var sb strings.Builder
	idx, global := 0, 0.0
	for _, sc := range scenes {
		var dubs []models.Dialogue
		s.db.Where("scene_id = ?", sc.ID).Order("`order`").Find(&dubs)
		dur := normalizeSceneDuration(sc.Duration)
		if sc.VideoFile != "" && sc.VideoGPU != nil {
			abs := s.mediaPath("output_workers", fmt.Sprintf("gpu%d", *sc.VideoGPU), filepath.FromSlash(sc.VideoFile))
			if mi, err := s.remote.ProbeMedia(abs); err == nil && mi.Duration > 0 {
				dur = mi.Duration
			}
		}
		if len(dubs) == 0 {
			global += dur
			continue
		}
		seg := dur / float64(len(dubs))
		for _, d := range dubs {
			if strings.TrimSpace(d.Text) == "" {
				continue
			}
			idx++
			start := global + seg
			sb.WriteString(fmt.Sprintf("%d\n%s --> %s\n", idx,
				formatSRTTime(start), formatSRTTime(start+seg)))
			line := strings.TrimSpace(d.Text)
			if strings.TrimSpace(d.Character) != "" {
				line = d.Character + "：" + line
			}
			sb.WriteString(line + "\n\n")
			global = start + seg
		}
	}
	data := []byte{0xEF, 0xBB, 0xBF}
	data = append(data, []byte(sb.String())...)
	if err := s.remote.WriteFile(srtAbs, data, 0o644); err != nil {
		return err
	}
	log.Printf("[merge %d] 字幕已生成: %s (%d 条)", mt.ID, srtAbs, idx)
	return nil
}

// finishMergeProject 成片完成后标记项目收尾
func (s *ProjectService) finishMergeProject(p *models.Project) {
	s.db.Model(&models.Project{}).Where("id = ? AND generation = ?", p.ID, p.Generation).Updates(map[string]any{
		"status": "finished", "pipeline_stage": "finished", "auto_generate": false,
	})
	s.pushProject(p)
}

// sceneVideo 场景视频本地信息（合并时间轴计算用）
type sceneVideo struct {
	abs string
	dur float64
}

// dubSegment 单句对白在成片全局时间轴上的位置
type dubSegment struct {
	DialogueID uint
	SceneID    uint
	Start      float64 // 全局秒
	End        float64
	Character  string
	Text       string
	AudioAbs   string // TTS 音频绝对路径（空表示无音频）
	SourceDur  float64
	Speed      float64
	Pitch      float64
	Volume     float64
}

// buildDubTimeline 构建该集对白时间轴：
// 场景按顺序排布（起点 = 前序视频时长累加），场景内对白按序排列（句间 0.35s 间隔）。
// 全部对白均 ready 时返回 dubReady=true；存在未合成对白时返回 false（回退原音轨合并）。
func (s *ProjectService) buildDubTimeline(p *models.Project, episodeN int, sceneIDs []uint, videoDurs []sceneVideo) ([]dubSegment, float64, bool, error) {
	var dubs []models.Dialogue
	if err := s.db.Where("scene_id IN ?", sceneIDs).Order("scene_id, `order`").Find(&dubs).Error; err != nil {
		return nil, 0, false, err
	}
	audible := dubs[:0]
	for _, d := range dubs {
		if strings.TrimSpace(d.Text) != "" {
			audible = append(audible, d)
		}
	}
	dubs = audible
	if len(dubs) == 0 {
		return nil, 0, false, nil
	}
	// Dialogue mixing is explicit: never silently publish stale or mismatched speech.
	for _, d := range dubs {
		if d.Status != "ready" || d.AudioStale || strings.TrimSpace(d.AudioFile) == "" || d.AudioHash == "" || d.AudioHash != s.effectiveDialogueAudioHash(d) {
			return nil, 0, false, fmt.Errorf("对白 %d 的音频未就绪或已过期，请先重新合成", d.ID)
		}
	}
	byScene := map[uint][]models.Dialogue{}
	sceneOrder := map[uint]int{}
	for i, sid := range sceneIDs {
		sceneOrder[sid] = i
	}
	for _, d := range dubs {
		byScene[d.SceneID] = append(byScene[d.SceneID], d)
	}

	const gap = 0.35
	segs := make([]dubSegment, 0, len(dubs))
	global := 0.0
	sceneDur := make([]float64, len(sceneIDs))
	for i := range sceneIDs {
		if i < len(videoDurs) {
			sceneDur[i] = videoDurs[i].dur
		}
	}
	for i, sid := range sceneIDs {
		list := byScene[sid]
		sceneStart := global
		if len(list) == 0 {
			global += sceneDur[i]
			continue
		}
		cursor := sceneStart + 0.4
		for _, d := range list {
			if d.Position > 0 {
				cursor = sceneStart + d.Position
			}
			start := math.Max(sceneStart, cursor+d.Offset)
			audioAbs := ""
			sourceDur := 2.0 // 无音频时的兜底时长
			if d.AudioFile != "" {
				audioAbs = filepath.Join(s.cfg.Comfy.ComfyDir, "input", fmt.Sprintf("%d", p.ID), d.AudioFile)
				if ad, err := s.remoteMediaDuration(audioAbs); err == nil && ad > 0 {
					sourceDur = ad
				}
			}
			speed := dialogueSpeed(d)
			dur := sourceDur / speed
			segs = append(segs, dubSegment{
				DialogueID: d.ID, SceneID: d.SceneID,
				Start: start, End: start + dur,
				Character: d.Character, Text: d.Text, AudioAbs: audioAbs,
				SourceDur: sourceDur, Speed: speed, Pitch: d.Pitch, Volume: dialogueVolume(d),
			})
			cursor = start + dur + gap
		}
		if cursor > sceneStart+sceneDur[i] {
			global = cursor // 对白超出场景视频时长时顺延时间轴
		} else {
			global = sceneStart + sceneDur[i]
		}
	}
	return segs, global, true, nil
}

// remoteMediaDuration 远程探测音频/视频时长（秒），用 ffmpeg -i 解析
func (s *ProjectService) remoteMediaDuration(abs string) (float64, error) {
	cmd := fmt.Sprintf(`%s; $FF -i %s 2>&1 | grep -oP 'Duration: \K[0-9:.]+' | head -1`, ffmpegResolveCmd, shellQuote(abs))
	out, err := s.remote.RunTimeout(cmd, 30*time.Second)
	if err != nil {
		return 0, err
	}
	t := strings.TrimSpace(out)
	if t == "" {
		return 0, fmt.Errorf("无法解析时长: %s", abs)
	}
	parts := strings.Split(t, ":")
	if len(parts) != 3 {
		return 0, fmt.Errorf("时长格式异常: %s", t)
	}
	h, _ := strconv.ParseFloat(parts[0], 64)
	m, _ := strconv.ParseFloat(parts[1], 64)
	sec, _ := strconv.ParseFloat(parts[2], 64)
	return h*3600 + m*60 + sec, nil
}

// escapeFilterPath ffmpeg filter 内路径转义（冒号/引号/逗号）
func escapeFilterPath(p string) string {
	// FFmpeg filter 参数有自己的转义规则，与宿主 Shell 无关。统一斜杠后
	// 转义 Windows 盘符冒号及 filter 分隔符；整个值用单引号包裹。
	p = filepath.ToSlash(p)
	p = strings.ReplaceAll(p, "'", `\'`)
	p = strings.ReplaceAll(p, ":", `\:`)
	p = strings.ReplaceAll(p, ",", `\,`)
	p = strings.ReplaceAll(p, "[", `\[`)
	p = strings.ReplaceAll(p, "]", `\]`)
	return "'" + p + "'"
}

// writeEpisodeSRT 按真实音频时间轴写出 SRT 字幕（UTF-8 BOM 兼容 Windows）
func (s *ProjectService) writeEpisodeSRT(mt *models.MergeTask, srtAbs string, segs []dubSegment) error {
	var sb strings.Builder
	idx := 0
	for _, seg := range segs {
		if strings.TrimSpace(seg.Text) == "" {
			continue
		}
		idx++
		sb.WriteString(fmt.Sprintf("%d\n", idx))
		sb.WriteString(fmt.Sprintf("%s --> %s\n", formatSRTTime(seg.Start), formatSRTTime(seg.End)))
		line := strings.TrimSpace(seg.Text)
		if strings.TrimSpace(seg.Character) != "" {
			line = seg.Character + "：" + line
		}
		sb.WriteString(line + "\n\n")
	}
	data := []byte{0xEF, 0xBB, 0xBF}
	data = append(data, []byte(sb.String())...)
	if err := s.remote.WriteFile(srtAbs, data, 0o644); err != nil {
		return err
	}
	log.Printf("[merge %d] 字幕已生成: %s (%d 条)", mt.ID, srtAbs, idx)
	return nil
}

const ffmpegResolveCmd = `FF=$(command -v ffmpeg 2>/dev/null); if [ -z $FF ]; then for PY in python /opt/miniconda3/envs/comfyenv/bin/python /opt/miniconda3/envs/wan22/bin/python; do FF=$($PY -c 'import imageio_ffmpeg,sys;sys.stdout.write(imageio_ffmpeg.get_ffmpeg_exe())' 2>/dev/null); [ -n $FF ] && break; done; fi; if [ -z $FF ]; then for F in /usr/bin/ffmpeg /usr/local/bin/ffmpeg /opt/miniconda3/envs/*/lib/python3.*/site-packages/imageio_ffmpeg/binaries/ffmpeg-*; do [ -x $F ] && { FF=$F; break; }; done; fi; [ -z $FF ] && FF=ffmpeg`

func (s *ProjectService) remoteFileHasAudio(filePath string) (bool, error) {
	if s.remote == nil || !s.remote.Enabled() {
		ffmpeg, err := localFFmpegPath()
		if err != nil {
			return false, err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-i", filePath)
		output, _ := cmd.CombinedOutput() // ffmpeg -i 没有输出目标时通常返回非零，仍可解析流信息。
		if ctx.Err() == context.DeadlineExceeded {
			return false, fmt.Errorf("检测场景音轨超时")
		}
		return strings.Contains(string(output), "Audio:"), nil
	}
	cmd := fmt.Sprintf(`%s; if ! command -v $FF >/dev/null 2>&1 && [ ! -x $FF ]; then echo unknown; elif $FF -i %s 2>&1 | grep -q 'Audio:'; then echo yes; else echo no; fi`, ffmpegResolveCmd, shellQuote(filePath))
	out, err := s.remote.RunTimeout(cmd, 30*time.Second)
	if err != nil {
		return false, fmt.Errorf("检测场景音轨失败: %w", err)
	}
	switch strings.TrimSpace(out) {
	case "yes":
		return true, nil
	case "no":
		return false, nil
	default:
		return false, fmt.Errorf("无法找到 ffmpeg，不能安全检测并保留音轨")
	}
}

func projectFileTag(p *models.Project) string {
	// 纯数字安全标签，避免文件名注入
	now := time.Now().Format("20060102")
	return fmt.Sprintf("p%d_%s", p.ID, now)
}

func (s *ProjectService) failMerge(mt *models.MergeTask, p *models.Project, errMsg string) {
	s.db.Model(&models.MergeTask{}).
		Where("id = ? AND project_id = ? AND generation = ? AND status IN ?", mt.ID, mt.ProjectID, mt.Generation, []string{"pending", "running"}).
		Updates(map[string]any{"status": "failed", "output_file": "", "error": errMsg})
	s.pushProject(p)
	log.Printf("[merge %d] 失败: %s", mt.ID, errMsg)
}

func (s *ProjectService) ListMerges(projectID uint) ([]models.MergeTask, error) {
	var list []models.MergeTask
	s.db.Where("project_id = ?", projectID).Order("id desc").Find(&list)
	return list, nil
}

// CreateAllMerges 整剧一键合并：对所有含就绪视频的集各创建一个合并任务
func (s *ProjectService) CreateAllMerges(p *models.Project, dub, subtitles bool) (int, error) {
	var episodes []int
	if err := s.db.Model(&models.Scene{}).Where("project_id = ?", p.ID).
		Distinct("episode_n").Order("episode_n").Pluck("episode_n", &episodes).Error; err != nil {
		return 0, err
	}
	count := 0
	var failures []error
	for _, ep := range episodes {
		generation, err := latestEpisodeGeneration(s.db, p.ID, ep)
		if err != nil {
			failures = append(failures, fmt.Errorf("第%d集: %w", ep, err))
			continue
		}
		var scenes []models.Scene
		if err := s.db.Where("project_id = ? AND generation = ? AND episode_n = ? AND status = ?", p.ID, generation, ep, "video_ready").
			Order("`order`, id").Find(&scenes).Error; err != nil {
			failures = append(failures, fmt.Errorf("第%d集: %w", ep, err))
			continue
		}
		if len(scenes) < 2 {
			continue
		}
		ids := make([]uint, len(scenes))
		for i := range scenes {
			ids[i] = scenes[i].ID
		}
		if _, err := s.CreateMergeTask(p, ids, dub, subtitles); err != nil {
			failures = append(failures, fmt.Errorf("第%d集: %w", ep, err))
			continue
		}
		count++
	}
	return count, errors.Join(failures...)
}

// ---------- 对白配音与字幕（Dialogue） ----------

// ListSceneDialogues 场景对白列表
func (s *ProjectService) ListSceneDialogues(sceneID uint) ([]models.Dialogue, error) {
	var list []models.Dialogue
	s.db.Where("scene_id = ?", sceneID).Order("`order`").Find(&list)
	return list, nil
}

func editorSceneVideoURL(projectID uint, sc *models.Scene) string {
	if sc.VideoInputFile != "" {
		return fmt.Sprintf("/api/input/%d/%s", projectID, sc.VideoInputFile)
	}
	if sc.VideoFile != "" && sc.VideoGPU != nil {
		return fmt.Sprintf("/api/output/%d/%s", *sc.VideoGPU, sc.VideoFile)
	}
	return ""
}

// EditorData 剪辑台数据：场景（含视频时长与资源 URL）+ 对白 + 按真实音频对齐的字幕时间轴
func (s *ProjectService) EditorData(p *models.Project, episodeN int) (map[string]any, error) {
	generation, err := latestEpisodeGeneration(s.db, p.ID, episodeN)
	if err != nil {
		return nil, err
	}
	var scenes []models.Scene
	if err := s.db.Where("project_id = ? AND episode_n = ? AND generation = ?", p.ID, episodeN, generation).
		Order("`order`").Find(&scenes).Error; err != nil {
		return nil, err
	}
	type editorScene struct {
		models.Scene
		VideoURL  string   `json:"video_url"`
		ImageURL  string   `json:"image_url"`
		VideoDur  float64  `json:"video_dur"`
		AudioURLs []string `json:"audio_urls,omitempty"`
	}
	outScenes := make([]editorScene, 0, len(scenes))
	sceneIDs := make([]uint, 0, len(scenes))
	for _, sc := range scenes {
		es := editorScene{Scene: sc}
		// 完成同步后会把视频复制到项目 input 目录。该副本跨 GPU、跨 ComfyUI
		// 实例均可访问，剪辑台应与项目详情一样优先使用它，而不是原 worker 输出。
		if sc.VideoInputFile != "" {
			es.VideoURL = editorSceneVideoURL(p.ID, &sc)
			abs := filepath.Join(s.upload.InputDir(), fmt.Sprint(p.ID), filepath.FromSlash(sc.VideoInputFile))
			if mi, err := s.remote.ProbeMedia(abs); err == nil && mi.Duration > 0 {
				es.VideoDur = mi.Duration
			}
		} else if sc.VideoFile != "" && sc.VideoGPU != nil {
			es.VideoURL = editorSceneVideoURL(p.ID, &sc)
			abs := s.mediaPath("output_workers", fmt.Sprintf("gpu%d", *sc.VideoGPU), filepath.FromSlash(sc.VideoFile))
			if mi, err := s.remote.ProbeMedia(abs); err == nil && mi.Duration > 0 {
				es.VideoDur = mi.Duration
			}
		}
		if sc.ImageFile != "" {
			es.ImageURL = fmt.Sprintf("/api/input/%d/%s", p.ID, sc.ImageFile)
		}
		outScenes = append(outScenes, es)
		sceneIDs = append(sceneIDs, sc.ID)
	}

	var dubs []models.Dialogue
	if len(sceneIDs) > 0 {
		s.db.Where("scene_id IN ?", sceneIDs).Order("scene_id, `order`").Find(&dubs)
	}
	for i := range dubs {
		if dubs[i].AudioFile != "" {
			dubs[i].AudioFile = fmt.Sprintf("/api/input/%d/%s", p.ID, dubs[i].AudioFile)
		}
		if dubs[i].PreviousAudioFile != "" {
			dubs[i].PreviousAudioFile = fmt.Sprintf("/api/input/%d/%s", p.ID, dubs[i].PreviousAudioFile)
		}
	}

	// 字幕时间轴（真实音频对齐；配音未就绪时按场景时长均分占位）
	// 注意：dubs 的 AudioFile 已转为 URL，时间轴探测需用原名（在调用前保存）
	subs := s.EditorSubtitleTimelineRaw(p, scenes, dubs)

	return map[string]any{
		"project":   p,
		"scenes":    outScenes,
		"dialogues": dubs,
		"subtitles": subs,
	}, nil
}

// EditorSubtitleTimelineRaw 计算该集字幕时间轴（秒），供剪辑台展示：
// 对白配音 ready 时按真实音频时长排布；否则按场景时长均分占位。
func (s *ProjectService) EditorSubtitleTimelineRaw(p *models.Project, scenes []models.Scene, dubs []models.Dialogue) []map[string]any {
	byScene := map[uint][]models.Dialogue{}
	for _, d := range dubs {
		byScene[d.SceneID] = append(byScene[d.SceneID], d)
	}
	ready := true
	for _, d := range dubs {
		if d.Status != "ready" {
			ready = false
			break
		}
	}
	out := make([]map[string]any, 0, len(dubs))
	global := 0.0
	sceneDur := make([]float64, len(scenes))
	for i, sc := range scenes {
		d := normalizeSceneDuration(sc.Duration)
		if sc.VideoFile != "" && sc.VideoGPU != nil {
			abs := s.mediaPath("output_workers", fmt.Sprintf("gpu%d", *sc.VideoGPU), filepath.FromSlash(sc.VideoFile))
			if mi, err := s.remote.ProbeMedia(abs); err == nil && mi.Duration > 0 {
				d = mi.Duration
			}
		}
		sceneDur[i] = d
	}
	const gap = 0.35
	for i, sc := range scenes {
		list := byScene[sc.ID]
		if len(list) == 0 {
			global += sceneDur[i]
			continue
		}
		cursor := global + 0.4
		if !ready {
			seg := sceneDur[i] / float64(len(list))
			for _, d := range list {
				if d.Position > 0 {
					cursor = global + d.Position
				}
				start := math.Max(global, cursor+d.Offset)
				dur := seg / dialogueSpeed(d)
				out = append(out, map[string]any{
					"dialogue_id": d.ID, "scene_id": d.SceneID, "scene_order": sc.Order,
					"character": d.Character, "text": d.Text,
					"start": start, "end": start + dur, "status": d.Status, "offset": d.Offset,
				})
				cursor = start + dur
			}
			global += sceneDur[i]
			continue
		}
		for _, d := range list {
			if d.Position > 0 {
				cursor = global + d.Position
			}
			start := math.Max(global, cursor+d.Offset)
			dur := 2.0
			if d.AudioFile != "" {
				// AudioFile 可能为 URL（/api/input/pid/name），取文件名后拼真实路径
				name := filepath.Base(strings.TrimPrefix(d.AudioFile, "/api/input/"))
				abs := filepath.Join(s.cfg.Comfy.ComfyDir, "input", fmt.Sprintf("%d", p.ID), name)
				if ad, err := s.remoteMediaDuration(abs); err == nil && ad > 0 {
					dur = ad
				}
			}
			dur /= dialogueSpeed(d)
			out = append(out, map[string]any{
				"dialogue_id": d.ID, "scene_id": d.SceneID, "scene_order": sc.Order,
				"character": d.Character, "text": d.Text,
				"start": start, "end": start + dur, "status": d.Status, "offset": d.Offset,
			})
			cursor = start + dur + gap
		}
		if cursor > global+sceneDur[i] {
			global = cursor
		} else {
			global += sceneDur[i]
		}
	}
	return out
}

type DialogueUpdateInput struct {
	Text               *string  `json:"text"`
	Voice              *string  `json:"voice"`
	Character          *string  `json:"character"`
	SpeechType         *string  `json:"speech_type"`
	Position           *float64 `json:"position"`
	Offset             *float64 `json:"offset"`
	Speed              *float64 `json:"speed"`
	Pitch              *float64 `json:"pitch"`
	Volume             *float64 `json:"volume"`
	Emotion            *string  `json:"emotion"`
	Delivery           *string  `json:"delivery"`
	H3VoiceDescription *string  `json:"h3_voice_description"`
}

func dialogueSpeed(d models.Dialogue) float64 {
	if d.Speed <= 0 {
		return 1
	}
	return d.Speed
}

func dialogueVolume(d models.Dialogue) float64 {
	if d.Volume <= 0 && d.ID == 0 {
		return 1
	}
	return d.Volume
}

// dialogueAudioHash identifies all persisted inputs which determine a QA audio rendition.
func dialogueAudioHash(d models.Dialogue) string {
	raw := fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%.4f\x00%.4f\x00%.4f\x00%s\x00%s",
		strings.TrimSpace(d.Text), strings.TrimSpace(d.Character), strings.TrimSpace(d.SpeechType), strings.TrimSpace(d.Voice),
		dialogueSpeed(d), d.Pitch, dialogueVolume(d), strings.TrimSpace(d.Emotion), strings.TrimSpace(d.Delivery))
	return fmt.Sprintf("%x", sha256.Sum256([]byte(raw)))
}

func (s *ProjectService) effectiveDialogueAudioHash(d models.Dialogue) string {
	voice, voiceID, model := s.dubVoiceFor(&d)
	raw := dialogueAudioHash(d) + "\x00" + voice + "\x00" + voiceID + "\x00" + model
	return fmt.Sprintf("%x", sha256.Sum256([]byte(raw)))
}

// UpdateDialogue keeps the legacy service API while the HTTP API uses patch-like pointer fields.
func (s *ProjectService) UpdateDialogue(p *models.Project, did uint, text, voice, character, speechType string) (*models.Dialogue, error) {
	input := DialogueUpdateInput{}
	if text != "" {
		input.Text = &text
	}
	if voice != "" {
		input.Voice = &voice
	}
	if character != "" {
		input.Character = &character
	}
	if speechType != "" {
		input.SpeechType = &speechType
	}
	return s.UpdateDialogueFields(p, did, input)
}

// UpdateDialogueFields persists timeline/H3 metadata and invalidates synthesized audio only
// when a field that affects speech output changes.
func (s *ProjectService) UpdateDialogueFields(p *models.Project, did uint, input DialogueUpdateInput) (*models.Dialogue, error) {
	var d models.Dialogue
	if err := s.db.Where("id = ? AND project_id = ?", did, p.ID).First(&d).Error; err != nil {
		return nil, fmt.Errorf("对白不存在")
	}
	updates := map[string]any{}
	audioChanged, promptChanged := false, false
	if input.Text != nil && *input.Text != d.Text {
		updates["text"], audioChanged, promptChanged = strings.TrimSpace(*input.Text), true, true
	}
	if input.Voice != nil && *input.Voice != d.Voice {
		updates["voice"], audioChanged = strings.TrimSpace(*input.Voice), true
	}
	character := d.Character
	if input.Character != nil {
		character = strings.TrimSpace(*input.Character)
		if character != d.Character {
			updates["character"], audioChanged, promptChanged = character, true, true
		}
	}
	if input.SpeechType != nil {
		normalized, normalizedCharacter := normalizeScriptSpeech(*input.SpeechType, character)
		if normalized == "" {
			return nil, fmt.Errorf("发声类型必须是 dialogue、narration 或 monologue")
		}
		if normalized != d.SpeechType || normalizedCharacter != d.Character {
			updates["speech_type"], updates["character"] = normalized, normalizedCharacter
			audioChanged, promptChanged = true, true
		}
	}
	if input.Position != nil {
		if *input.Position < 0 {
			return nil, fmt.Errorf("position 不可为负数")
		}
		if *input.Position != d.Position {
			updates["position"] = *input.Position
		}
	}
	if input.Offset != nil {
		if *input.Offset < -60 || *input.Offset > 60 {
			return nil, fmt.Errorf("offset 需在 -60~60 秒之间")
		}
		if *input.Offset != d.Offset {
			updates["offset"] = *input.Offset
		}
	}
	if input.Speed != nil {
		if *input.Speed < 0.5 || *input.Speed > 2 {
			return nil, fmt.Errorf("speed 需在 0.5~2 之间")
		}
		if *input.Speed != dialogueSpeed(d) {
			updates["speed"], audioChanged = *input.Speed, true
		}
	}
	if input.Pitch != nil {
		if *input.Pitch < -12 || *input.Pitch > 12 {
			return nil, fmt.Errorf("pitch 需在 -12~12 半音之间")
		}
		if *input.Pitch != d.Pitch {
			updates["pitch"], audioChanged = *input.Pitch, true
		}
	}
	if input.Volume != nil {
		if *input.Volume < 0 || *input.Volume > 4 {
			return nil, fmt.Errorf("volume 需在 0~4 之间")
		}
		if *input.Volume != dialogueVolume(d) {
			updates["volume"], audioChanged = *input.Volume, true
		}
	}
	if input.Emotion != nil && strings.TrimSpace(*input.Emotion) != d.Emotion {
		updates["emotion"], audioChanged = strings.TrimSpace(*input.Emotion), true
	}
	if input.Delivery != nil && strings.TrimSpace(*input.Delivery) != d.Delivery {
		updates["delivery"], audioChanged = strings.TrimSpace(*input.Delivery), true
	}
	if input.H3VoiceDescription != nil && strings.TrimSpace(*input.H3VoiceDescription) != d.H3VoiceDescription {
		updates["h3_voice_description"] = strings.TrimSpace(*input.H3VoiceDescription)
		promptChanged = true
	}
	if audioChanged {
		// Keep the current file available for QA/revert; stale-only dubbing will replace it.
		updates["status"], updates["error"], updates["audio_token"] = "pending", "", ""
		updates["audio_stale"], updates["audio_stale_reason"] = true, "配音输入或 QA 参数已修改"
	}
	if err := s.db.Model(&d).Updates(updates).Error; err != nil {
		return nil, err
	}
	if promptChanged {
		_ = s.db.Model(&models.Scene{}).Where("id = ? AND video_full_prompt != ''", d.SceneID).Update("prompt_stale", true).Error
	}
	s.pushProject(p)
	s.db.First(&d, did)
	return &d, nil
}

// ReorderScenes 调整场景顺序：scene_ids 必须是当前版本同一集的完整场景集合。
func (s *ProjectService) ReorderScenes(p *models.Project, sceneIDs []uint) error {
	if len(sceneIDs) < 2 {
		return fmt.Errorf("至少需要 2 个场景")
	}
	seen := make(map[uint]struct{}, len(sceneIDs))
	for _, sid := range sceneIDs {
		if sid == 0 {
			return fmt.Errorf("场景 ID 无效")
		}
		if _, exists := seen[sid]; exists {
			return fmt.Errorf("场景 ID %d 重复", sid)
		}
		seen[sid] = struct{}{}
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		var current models.Project
		if err := tx.First(&current, p.ID).Error; err != nil {
			return err
		}
		var selected []models.Scene
		if err := tx.Where("id IN ? AND project_id = ?", sceneIDs, current.ID).Find(&selected).Error; err != nil {
			return err
		}
		if len(selected) != len(sceneIDs) {
			return fmt.Errorf("场景列表包含不属于项目当前版本的场景")
		}
		episodeN := selected[0].EpisodeN
		for _, scene := range selected[1:] {
			if scene.EpisodeN != episodeN {
				return fmt.Errorf("不能重排不同集的场景")
			}
		}
		generation, err := latestEpisodeGeneration(tx, current.ID, episodeN)
		if err != nil {
			return err
		}
		for _, scene := range selected {
			if scene.Generation != generation {
				return fmt.Errorf("场景列表包含不属于该集当前版本的场景")
			}
		}
		var expectedIDs []uint
		if err := tx.Model(&models.Scene{}).Where("project_id = ? AND generation = ? AND episode_n = ?", current.ID, generation, episodeN).Pluck("id", &expectedIDs).Error; err != nil {
			return err
		}
		if len(expectedIDs) != len(sceneIDs) {
			return fmt.Errorf("scene_ids 必须包含该集当前版本的全部场景")
		}
		for _, sid := range expectedIDs {
			if _, exists := seen[sid]; !exists {
				return fmt.Errorf("scene_ids 必须包含该集当前版本的全部场景")
			}
		}

		for i, sid := range sceneIDs {
			result := tx.Model(&models.Scene{}).Where("id = ? AND project_id = ? AND generation = ? AND episode_n = ?", sid, current.ID, generation, episodeN).
				Update("`order`", -1000000000-i)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return fmt.Errorf("场景 %d 在重排期间发生变化", sid)
			}
		}
		for i, sid := range sceneIDs {
			result := tx.Model(&models.Scene{}).Where("id = ? AND project_id = ? AND generation = ? AND episode_n = ?", sid, current.ID, generation, episodeN).
				Update("`order`", i+1)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return fmt.Errorf("场景 %d 在重排期间发生变化", sid)
			}
		}
		return nil
	})
}

// dubVoiceFor 对白音色解析（优先级）：单条对白覆盖 → 角色复刻音色 → 角色预设音色 → 平台角色映射/默认。
// 返回 (预设音色, 复刻音色ID, 复刻合成模型)；复刻音色 ID 非空时用 VC 合成。
func (s *ProjectService) dubVoiceFor(d *models.Dialogue) (string, string, string) {
	if strings.TrimSpace(d.Voice) != "" {
		return d.Voice, "", ""
	}
	if name := strings.TrimSpace(d.Character); name != "" {
		var ch models.Character
		if err := s.db.Where("project_id = ? AND name = ?", d.ProjectID, name).First(&ch).Error; err == nil {
			if ch.VoiceID != "" {
				model := ch.VoiceModel
				if model == "" {
					model = QwenTTSVCModel
				}
				return "", ch.VoiceID, model
			}
			if ch.Voice != "" {
				return ch.Voice, "", ""
			}
		}
	}
	if s.ali != nil {
		return s.ali.Config().VoiceFor(d.Character), "", ""
	}
	return "", "", ""
}

// StartDialogueTTS 异步合成单条对白（占位防并发）
func (s *ProjectService) StartDialogueTTS(d *models.Dialogue) error {
	token := fmt.Sprintf("tts-%d-%d", d.ID, time.Now().UnixNano())
	claim := s.db.Model(&models.Dialogue{}).Where("id = ? AND status <> ?", d.ID, "synthesizing").
		Updates(map[string]any{"status": "synthesizing", "audio_token": token, "error": ""})
	if claim.Error != nil {
		return claim.Error
	}
	if claim.RowsAffected == 0 {
		return fmt.Errorf("对白已在合成或已完成")
	}
	// Refetch after claiming so synthesis and its hash use the latest persisted controls.
	if err := s.db.First(d, d.ID).Error; err != nil {
		return err
	}
	hash := s.effectiveDialogueAudioHash(*d)
	go func() {
		if err := s.synthesizeDialogue(d, token, hash); err != nil {
			log.Printf("[dialogue %d] tts failed: %v", d.ID, err)
		}
	}()
	return nil
}

func (s *ProjectService) synthesizeDialogue(d *models.Dialogue, token, inputHash string) error {
	if s.upload == nil {
		s.db.Model(&models.Dialogue{}).Where("id = ? AND audio_token = ?", d.ID, token).Updates(map[string]any{"status": "failed", "error": "存储未配置"})
		return fmt.Errorf("存储未配置")
	}
	if strings.TrimSpace(d.Text) == "" {
		result := s.db.Model(&models.Dialogue{}).Where("id = ? AND audio_token = ?", d.ID, token).Updates(map[string]any{
			"status": "ready", "audio_file": "", "previous_audio_file": d.AudioFile, "previous_audio_hash": d.AudioHash,
			"audio_revision": d.AudioRevision + 1, "audio_hash": inputHash, "audio_token": "",
			"audio_stale": false, "audio_stale_reason": "", "error": "",
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("对白合成结果已过期")
		}
		return nil
	}
	// 后期配音：阿里云 TTS（DashScope）；角色绑定参考语音时用复刻音色合成
	var data []byte
	var err error
	voice, voiceID, vcModel := s.dubVoiceFor(d)
	if s.ali != nil && s.ali.Config().Configured() {
		if voiceID != "" {
			data, err = s.ali.TextToSpeechVC(d.Text, voiceID, vcModel)
		} else {
			data, err = s.ali.TextToSpeech(d.Text, d.Character, voice)
		}
	} else {
		err = fmt.Errorf("TTS 未配置（请到平台设置配置阿里云语音服务）")
	}
	if err != nil {
		s.db.Model(&models.Dialogue{}).Where("id = ? AND audio_token = ?", d.ID, token).Updates(map[string]any{"status": "failed", "error": err.Error()})
		return err
	}
	name := fmt.Sprintf("dub_%d_%d.mp3", d.ID, time.Now().UnixNano())
	path, _, err := s.upload.SaveFile(fmt.Sprintf("%d", d.ProjectID), "audio", name, data)
	if err != nil {
		s.db.Model(&models.Dialogue{}).Where("id = ? AND audio_token = ?", d.ID, token).Updates(map[string]any{"status": "failed", "error": err.Error()})
		return err
	}
	result := s.db.Model(&models.Dialogue{}).Where("id = ? AND audio_token = ?", d.ID, token).Updates(map[string]any{
		"status": "ready", "audio_file": filepath.Base(path), "previous_audio_file": d.AudioFile, "previous_audio_hash": d.AudioHash,
		"audio_revision": d.AudioRevision + 1, "audio_hash": inputHash, "audio_token": "",
		"audio_stale": false, "audio_stale_reason": "", "error": "",
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("对白合成结果已过期")
	}
	s.pushProject(nil)
	return nil
}

// GenerateSceneDubs 合成场景全部待合成对白
func (s *ProjectService) GenerateSceneDubs(sc *models.Scene) (int, error) {
	var dubs []models.Dialogue
	s.db.Where("scene_id = ? AND status IN ?", sc.ID, []string{"pending", "failed"}).Find(&dubs)
	for i := range dubs {
		_ = s.StartDialogueTTS(&dubs[i])
	}
	return len(dubs), nil
}

// GenerateProjectDubs 一键合成项目全部待合成对白
func (s *ProjectService) GenerateProjectDubs(p *models.Project) (int, error) {
	var dubs []models.Dialogue
	s.db.Where("project_id = ? AND status IN ?", p.ID, []string{"pending", "failed"}).Find(&dubs)
	for i := range dubs {
		_ = s.StartDialogueTTS(&dubs[i])
	}
	return len(dubs), nil
}

// GenerateEpisodeDubs submits only stale/mismatched dialogue by default, scoped through
// the episode's project-owned scenes so dialogue IDs from another project cannot leak in.
func (s *ProjectService) GenerateEpisodeDubs(p *models.Project, episodeN int, staleOnly bool) (int, error) {
	generation, err := latestEpisodeGeneration(s.db, p.ID, episodeN)
	if err != nil {
		return 0, err
	}
	var sceneIDs []uint
	if err := s.db.Model(&models.Scene{}).Where("project_id = ? AND episode_n = ? AND generation = ?", p.ID, episodeN, generation).Pluck("id", &sceneIDs).Error; err != nil {
		return 0, err
	}
	if len(sceneIDs) == 0 {
		return 0, nil
	}
	var dubs []models.Dialogue
	if err := s.db.Where("project_id = ? AND scene_id IN ?", p.ID, sceneIDs).Order("scene_id, `order`").Find(&dubs).Error; err != nil {
		return 0, err
	}
	count := 0
	for i := range dubs {
		d := &dubs[i]
		stale := d.AudioStale || d.AudioFile == "" || d.AudioHash == "" || d.AudioHash != s.effectiveDialogueAudioHash(*d)
		if staleOnly && !stale {
			continue
		}
		if d.Status == "synthesizing" {
			continue
		}
		if stale {
			_ = s.db.Model(d).Updates(map[string]any{"status": "pending", "audio_stale": true, "audio_stale_reason": "配音输入摘要已变化"}).Error
		}
		if err := s.StartDialogueTTS(d); err == nil {
			count++
		}
	}
	return count, nil
}

// EpisodeDubPreview returns the effective QA timeline and project-owned dialogue state.
func (s *ProjectService) EpisodeDubPreview(p *models.Project, episodeN int) (map[string]any, error) {
	data, err := s.EditorData(p, episodeN)
	if err != nil {
		return nil, err
	}
	return map[string]any{"episode_n": episodeN, "dialogues": data["dialogues"], "timeline": data["subtitles"]}, nil
}

// ApplyDialoguePreview approves the current persisted controls for the current audio rendition.
// Providers which do not accept these controls retain them for deterministic preview/merge filters.
func (s *ProjectService) ApplyDialoguePreview(p *models.Project, did uint) (*models.Dialogue, error) {
	var d models.Dialogue
	if err := s.db.Where("id = ? AND project_id = ?", did, p.ID).First(&d).Error; err != nil {
		return nil, fmt.Errorf("对白不存在")
	}
	if strings.TrimSpace(d.AudioFile) == "" {
		return nil, fmt.Errorf("当前对白没有可应用的音频")
	}
	currentHash := s.effectiveDialogueAudioHash(d)
	if d.AudioHash == "" || d.AudioHash != currentHash || d.AudioStale {
		return nil, fmt.Errorf("试听音频与当前文本、音色或表演参数不一致，请先重新合成")
	}
	if err := s.db.Model(&d).Updates(map[string]any{"audio_stale": false, "audio_stale_reason": "", "status": "ready", "error": ""}).Error; err != nil {
		return nil, err
	}
	s.db.First(&d, d.ID)
	return &d, nil
}

// RevertDialogueAudio swaps current and previous audio without deleting either revision.
func (s *ProjectService) RevertDialogueAudio(p *models.Project, did uint) (*models.Dialogue, error) {
	var d models.Dialogue
	if err := s.db.Where("id = ? AND project_id = ?", did, p.ID).First(&d).Error; err != nil {
		return nil, fmt.Errorf("对白不存在")
	}
	if strings.TrimSpace(d.PreviousAudioFile) == "" {
		return nil, fmt.Errorf("没有可回退的上一版音频")
	}
	current, previous := d.AudioFile, d.PreviousAudioFile
	currentHash, previousHash := d.AudioHash, d.PreviousAudioHash
	if previousHash == "" {
		return nil, fmt.Errorf("上一版音频缺少输入摘要，不能安全发布")
	}
	stale := previousHash != s.effectiveDialogueAudioHash(d)
	reason := ""
	if stale {
		reason = "上一版音频与当前对白输入不一致"
	}
	if err := s.db.Model(&d).Updates(map[string]any{"audio_file": previous, "previous_audio_file": current, "audio_hash": previousHash, "previous_audio_hash": currentHash, "audio_revision": d.AudioRevision + 1, "status": "ready", "audio_stale": stale, "audio_stale_reason": reason}).Error; err != nil {
		return nil, err
	}
	s.db.First(&d, d.ID)
	return &d, nil
}

// EpisodeSRT 生成指定集的 SRT 字幕（按场景顺序、场内对白均分时长），返回字幕正文与条数
func (s *ProjectService) EpisodeSRT(p *models.Project, episodeN int) (string, int) {
	generation, err := latestEpisodeGeneration(s.db, p.ID, episodeN)
	if err != nil {
		return "", 0
	}
	var scenes []models.Scene
	s.db.Where("project_id = ? AND episode_n = ? AND generation = ?", p.ID, episodeN, generation).Order("`order`").Find(&scenes)
	if len(scenes) == 0 {
		return "", 0
	}
	sceneIDs := make([]uint, 0, len(scenes))
	for _, sc := range scenes {
		sceneIDs = append(sceneIDs, sc.ID)
	}
	var dubs []models.Dialogue
	s.db.Where("scene_id IN ?", sceneIDs).Order("scene_id, `order`").Find(&dubs)
	byScene := map[uint][]models.Dialogue{}
	for _, d := range dubs {
		byScene[d.SceneID] = append(byScene[d.SceneID], d)
	}
	var sb strings.Builder
	idx, sceneStart, total := 0, 0.0, 0
	for _, sc := range scenes {
		list := byScene[sc.ID]
		total += len(list)
		sceneDuration := normalizeSceneDuration(sc.Duration)
		if len(list) == 0 {
			sceneStart += sceneDuration
			continue
		}
		seg := sceneDuration / float64(len(list))
		cursor := sceneStart
		for _, d := range list {
			if d.Position > 0 {
				cursor = sceneStart + d.Position
			}
			start := math.Max(sceneStart, cursor+d.Offset)
			end := start + seg/dialogueSpeed(d)
			idx++
			writeSRTEntry(&sb, idx, start, end, d.Character, d.Text)
			cursor = end
		}
		sceneStart += sceneDuration
	}
	return sb.String(), total
}

// writeSRTEntry 写一条 SRT 字幕块
func writeSRTEntry(sb *strings.Builder, idx int, start, end float64, character, text string) {
	sb.WriteString(fmt.Sprintf("%d\n", idx))
	sb.WriteString(fmt.Sprintf("%s --> %s\n", formatSRTTime(start), formatSRTTime(end)))
	line := strings.TrimSpace(text)
	if strings.TrimSpace(character) != "" {
		line = character + "：" + line
	}
	sb.WriteString(line + "\n\n")
}

// formatSRTTime 秒 → SRT 时间码 HH:MM:SS,mmm
func formatSRTTime(sec float64) string {
	if sec < 0 {
		sec = 0
	}
	totalMs := int(sec * 1000)
	h := totalMs / 3600000
	m := (totalMs % 3600000) / 60000
	se := (totalMs % 60000) / 1000
	ms := totalMs % 1000
	return fmt.Sprintf("%02d:%02d:%02d,%03d", h, m, se, ms)
}

// ---------- 进度推送 ----------

func (s *ProjectService) pushProject(p *models.Project) {
	// 项目状态变化由前端在轮询/刷新时获取，这里广播轻量通知
	if s.hub != nil {
		s.hub.Broadcast("project_update", map[string]any{"t": time.Now().UnixMilli()})
	}
}

// PushProject 供其他组件触发项目状态广播
func (s *ProjectService) PushProject(p *models.Project) {
	s.pushProject(p)
}

func (s *ProjectService) Stop() {
	close(s.stopped)
}

// detectImageExt 根据魔数判断图片扩展名
func detectImageExt(data []byte) string {
	switch {
	case len(data) >= 8 && data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47:
		return ".png"
	case len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF:
		return ".jpg"
	case len(data) >= 12 && data[0] == 'R' && data[1] == 'I' && data[2] == 'F' && data[3] == 'F' && data[8] == 'W' && data[9] == 'E' && data[10] == 'B' && data[11] == 'P':
		return ".webp"
	default:
		return ".png"
	}
}
