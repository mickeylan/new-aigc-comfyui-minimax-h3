package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const sceneVideoPromptCompilerVersion = "h3-prompt-v1"

var protectedH3TokenRE = regexp.MustCompile(`(?m)<d>(?s:.*?)</d>|<Picture\s+\d+>|<Subject\s+\d+>|\[Shot\s+\d+\]|\[\d+(?:\.\d+)?s\]|\d{2}:\d{2}(?:\.\d+)?|\b\d+(?:\.\d+)?\s*s\b|\(S\d+\)|</?(?:scenetrans|cutoff)>|^\s*(?:subject_definitions|summary|retention_analysis|detailed_description|overall_soundscape|non_diegetic_music|task|duration|resolution|aspect_ratio|fps|prompt)\s*:`)
var dialogueBlockRE = regexp.MustCompile(`(?s)<d>.*?</d>`)

func protectedH3Tokens(s string) []string { return protectedH3TokenRE.FindAllString(s, -1) }

func validateH3Translation(source, translated string) error {
	if strings.TrimSpace(translated) == "" {
		return fmt.Errorf("翻译结果为空")
	}
	a, b := protectedH3Tokens(source), protectedH3Tokens(translated)
	if len(a) != len(b) {
		return fmt.Errorf("翻译改变了 H3 结构 token")
	}
	for i := range a {
		if a[i] != b[i] {
			return fmt.Errorf("翻译改变了 H3 结构 token 顺序或内容")
		}
	}
	da, db := dialogueBlockRE.FindAllString(source, -1), dialogueBlockRE.FindAllString(translated, -1)
	if len(da) != len(db) {
		return fmt.Errorf("翻译改变了对白块")
	}
	for i := range da {
		if da[i] != db[i] {
			return fmt.Errorf("翻译必须逐字保留 <d>…</d>")
		}
	}
	return nil
}

func hashString(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

type sceneVideoSourceHash struct {
	Compiler string `json:"compiler"`
	Master   string `json:"master"`
	Scene    struct {
		Content                                                                  string  `json:"content"`
		Duration                                                                 float64 `json:"duration"`
		Mode, References                                                         string
		Identity, Visible, Voice, Mentioned, Location, Props, First, Last, Image string
	} `json:"scene"`
	Template   struct{ Code, Workflow, Inputs string } `json:"template"`
	Files      map[string][]FileMeta                   `json:"files"`
	Shots      []models.Shot                           `json:"shots"`
	Dialogues  []models.Dialogue                       `json:"dialogues"`
	Project    struct{ Style, Genre, Ratio string }    `json:"project"`
	Continuity *models.SceneContinuity                 `json:"continuity,omitempty"`
}

// sceneVideoPromptSourceHash binds the review to the effective compiler inputs, never to VideoFullPrompt.
func (s *Service) sceneVideoPromptSourceHash(sc *models.Scene) (string, error) {
	if s.Projects.continuity != nil {
		if err := s.Projects.continuity.PrepareScene(sc); err != nil {
			return "", err
		}
	}
	var p models.Project
	if err := s.DB.First(&p, sc.ProjectID).Error; err != nil {
		return "", err
	}
	var shots []models.Shot
	if err := s.DB.Where("scene_id = ?", sc.ID).Order("order_num, id").Find(&shots).Error; err != nil {
		return "", err
	}
	shots = s.Projects.ensureShotDialogueRanges(sc.ID, shots)
	template := enforceSceneModeVideoTemplate(sc, sc.VideoTemplate)
	if template == "" {
		template = "minimax_h3_ref2v"
	}
	if hasAuditedCombatShots(shots) {
		template = h3CombatRef2VTemplate
	}
	_, _, files := s.Projects.buildSceneVideoSpec(sc, fmt.Sprint(sc.ProjectID), template)
	if isReviewedAudioVideoTemplate(template) {
		var err error
		files, err = s.Projects.attachReviewedSceneAudio(sc, files)
		if err != nil {
			return "", err
		}
	}
	var tpl models.Template
	if err := s.DB.Where("code = ? AND enabled = ?", template, true).First(&tpl).Error; err != nil {
		return "", fmt.Errorf("视频模板不可用: %w", err)
	}
	continuity, _, _ := s.sceneVideoPromptContinuity(sc)
	v := sceneVideoSourceHash{Compiler: sceneVideoPromptCompilerVersion, Master: sc.VideoPromptMasterZH, Files: files, Shots: shots, Dialogues: s.Projects.sceneVideoDialogues(sc), Continuity: continuity}
	v.Scene.Content, v.Scene.Duration, v.Scene.Mode, v.Scene.References = sc.Content, normalizeSceneDuration(sc.Duration), sc.SceneMode, sc.ReferenceImagesJSON
	v.Scene.Identity, v.Scene.Visible, v.Scene.Voice, v.Scene.Mentioned = sc.Characters, sc.VisibleCharacters, sc.VoiceCharacters, sc.MentionedCharacters
	v.Scene.Location, v.Scene.Props, v.Scene.First, v.Scene.Last, v.Scene.Image = sc.LocationName, sc.Props, sc.VideoFirstFrameImg, sc.VideoLastFrameImg, sc.ImageFile
	v.Template.Code, v.Template.Workflow, v.Template.Inputs = template, tpl.WorkflowJSON, tpl.InputsJSON
	v.Project.Style, v.Project.Genre, v.Project.Ratio = p.Style, p.Genre, p.AspectRatio
	data, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return hashString(string(data)), nil
}

func (s *Service) HandleTranslateSceneVideoPrompt(c *gin.Context) {
	sc, ok := s.loadScene(c)
	if !ok {
		return
	}
	var req struct {
		MasterPromptZH string  `json:"master_prompt_zh"`
		Prompt         string  `json:"prompt"`
		Language       string  `json:"language"`
		Direction      string  `json:"direction"`
		Template       string  `json:"template"`
		FirstFrameImg  *string `json:"first_frame_img"`
		LastFrameImg   *string `json:"last_frame_img"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	source := req.MasterPromptZH
	if source == "" {
		source = req.Prompt
	}
	if source == "" {
		source = sc.VideoPromptMasterZH
	}
	language := strings.ToLower(strings.TrimSpace(req.Language))
	if req.Direction == "to_zh" {
		language = "zh"
	}
	if language == "" {
		language = "en"
	}
	if language != "en" && language != "zh" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "language 仅支持 en 或 zh"})
		return
	}
	if strings.TrimSpace(source) == "" {
		c.JSON(400, gin.H{"error": "prompt 不能为空"})
		return
	}
	if s.TextProviderFact == nil {
		c.JSON(503, gin.H{"error": "文本翻译服务未配置"})
		return
	}
	beforeScene := *sc
	beforeHash, err := s.sceneVideoPromptSourceHash(&beforeScene)
	if err != nil {
		c.JSON(422, gin.H{"error": err.Error()})
		return
	}
	target := "English"
	if language == "zh" {
		target = "Simplified Chinese"
	}
	system := "Translate only natural-language prose in this complete MiniMax H3 protocol prompt into " + target + ". Preserve field names, <Picture N>, <Subject N>, timestamps, tags and their order exactly. Preserve every <d>…</d> block byte-for-byte. Return only the translated prompt."
	translated, err := s.TextProviderFact.Chat(system, source)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	translated = strings.TrimSpace(translated)
	if err := validateH3Translation(source, translated); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	var fresh models.Scene
	if err := s.DB.Where("id = ? AND project_id = ?", sc.ID, sc.ProjectID).First(&fresh).Error; err != nil {
		c.JSON(409, gin.H{"error": "场景已变化，请重新打开预览"})
		return
	}
	afterHash, err := s.sceneVideoPromptSourceHash(&fresh)
	if err != nil || afterHash != beforeHash {
		c.JSON(409, gin.H{"error": "翻译期间源内容已变化，请重新翻译"})
		return
	}
	previewScene := fresh
	if req.MasterPromptZH != "" {
		previewScene.VideoPromptMasterZH = strings.TrimSpace(req.MasterPromptZH)
	}
	if req.Template != "" {
		previewScene.VideoTemplate = strings.TrimSpace(req.Template)
	}
	if req.FirstFrameImg != nil {
		previewScene.VideoFirstFrameImg = strings.TrimSpace(*req.FirstFrameImg)
	}
	if req.LastFrameImg != nil {
		previewScene.VideoLastFrameImg = strings.TrimSpace(*req.LastFrameImg)
	}
	h, err := s.sceneVideoPromptSourceHash(&previewScene)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"preview": translated, "prompt": translated, "language": language, "source_hash": h, "token": hashString(h + "\x00" + translated)})
}

func (s *Service) HandlePutSceneVideoPromptMaster(c *gin.Context) {
	sc, ok := s.loadScene(c)
	if !ok {
		return
	}
	var req struct {
		MasterPromptZH string  `json:"master_prompt_zh"`
		Template       string  `json:"template"`
		FirstFrameImg  *string `json:"first_frame_img"`
		LastFrameImg   *string `json:"last_frame_img"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.MasterPromptZH) == "" {
		c.JSON(400, gin.H{"error": "master_prompt_zh 不能为空"})
		return
	}
	master := strings.TrimSpace(req.MasterPromptZH)
	updates := map[string]any{"video_prompt_master_zh": master}
	if req.Template != "" {
		updates["video_template"] = strings.TrimSpace(req.Template)
	}
	if req.FirstFrameImg != nil {
		updates["video_first_frame_img"] = strings.TrimSpace(*req.FirstFrameImg)
	}
	if req.LastFrameImg != nil {
		updates["video_last_frame_img"] = strings.TrimSpace(*req.LastFrameImg)
	}
	changed := master != sc.VideoPromptMasterZH || (req.Template != "" && strings.TrimSpace(req.Template) != sc.VideoTemplate) || (req.FirstFrameImg != nil && strings.TrimSpace(*req.FirstFrameImg) != sc.VideoFirstFrameImg) || (req.LastFrameImg != nil && strings.TrimSpace(*req.LastFrameImg) != sc.VideoLastFrameImg)
	if changed {
		updates["prompt_stale"] = true
		updates["video_exec_source_hash"], updates["video_exec_confirmed_hash"], updates["video_exec_confirmed_at"] = "", "", nil
	}
	if err := s.DB.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&models.Scene{}).Where("id = ? AND project_id = ? AND updated_at = ?", sc.ID, sc.ProjectID, sc.UpdatedAt).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("场景已变化，请重新打开编辑器")
		}
		if changed && tx.Migrator().HasTable(&models.GenerationCandidate{}) {
			return MarkSceneCandidatesStale(tx, sc.ProjectID, sc.ID, "video", "中文视频主稿或模板已修改")
		}
		return nil
	}); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	var saved models.Scene
	if err := s.DB.First(&saved, sc.ID).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	h, err := s.sceneVideoPromptSourceHash(&saved)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true, "master_prompt_zh": saved.VideoPromptMasterZH, "source_hash": h, "confirmation_cleared": changed})
}

func (s *Service) HandleConfirmSceneVideoPrompt(c *gin.Context) {
	sc, ok := s.loadScene(c)
	if !ok {
		return
	}
	var req struct {
		ExecPrompt string `json:"exec_prompt"`
		Prompt     string `json:"prompt"`
		SourceHash string `json:"source_hash"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	if req.ExecPrompt == "" {
		req.ExecPrompt = req.Prompt
	}
	if strings.TrimSpace(sc.VideoPromptMasterZH) == "" {
		c.JSON(400, gin.H{"error": "master_prompt_zh 不能为空"})
		return
	}
	if err := validateH3Translation(sc.VideoPromptMasterZH, req.ExecPrompt); err != nil {
		c.JSON(422, gin.H{"error": err.Error()})
		return
	}
	current, err := s.sceneVideoPromptSourceHash(sc)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if req.SourceHash == "" || req.SourceHash != current {
		c.JSON(http.StatusConflict, gin.H{"error": "源内容已变化，请重新翻译预览", "source_hash": current})
		return
	}
	_, _, refs := s.sceneVideoPromptContinuity(sc)
	dubs := s.Projects.sceneVideoDialogues(sc)
	var shots []models.Shot
	if err := s.DB.Where("scene_id = ?", sc.ID).Order("order_num, id").Find(&shots).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	tpl := enforceSceneModeVideoTemplate(sc, sc.VideoTemplate)
	if tpl == "" {
		tpl = "minimax_h3_ref2v"
	}
	if hasAuditedCombatShots(shots) {
		tpl = h3CombatRef2VTemplate
	}
	if issues := validateFinalH3Submission(req.ExecPrompt, tpl, normalizeSceneDuration(sc.Duration), refs, dubs, shots); len(issues) > 0 {
		c.JSON(422, gin.H{"error": strings.Join(issues, "；")})
		return
	}
	now := time.Now()
	execHash := hashString(req.ExecPrompt)
	var result *gorm.DB
	txErr := s.DB.Transaction(func(tx *gorm.DB) error {
		result = tx.Model(&models.Scene{}).Where("id = ? AND project_id = ? AND video_prompt_master_zh = ? AND updated_at = ?", sc.ID, sc.ProjectID, sc.VideoPromptMasterZH, sc.UpdatedAt).Updates(map[string]any{"video_full_prompt": req.ExecPrompt, "video_exec_source_hash": current, "video_exec_confirmed_hash": execHash, "video_exec_confirmed_at": now, "prompt_stale": false})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return nil
		}
		if req.ExecPrompt != sc.VideoFullPrompt && tx.Migrator().HasTable(&models.GenerationCandidate{}) {
			return MarkSceneCandidatesStale(tx, sc.ProjectID, sc.ID, "video", "英文视频执行稿已重新确认")
		}
		return nil
	})
	if txErr != nil {
		c.JSON(500, gin.H{"error": txErr.Error()})
		return
	}
	if result.Error != nil {
		c.JSON(500, gin.H{"error": result.Error.Error()})
		return
	}
	if result.RowsAffected != 1 {
		c.JSON(409, gin.H{"error": "源内容已变化，请重试"})
		return
	}
	c.JSON(200, gin.H{"ok": true, "source_hash": current, "exec_hash": execHash, "confirmed_at": now})
}
