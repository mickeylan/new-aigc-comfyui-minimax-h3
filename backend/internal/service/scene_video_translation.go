package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const sceneVideoPromptCompilerVersion = "h3-prompt-v1"

var dialogueBlockRE = regexp.MustCompile(`(?s)<d>.*?</d>`)

// h3ProtocolTokenRE is the structure vocabulary that must survive translation untouched:
// field names, picture/subject bindings, shot markers, timestamps, speaker ids and
// transition tags. Bare prose durations are excluded because "3秒" and "3 seconds" are
// the same meaning written in two languages.
var h3ProtocolTokenRE = regexp.MustCompile(`(?m)(?s:<d>.*?</d>)|<Picture\s+\d+>|<Subject\s+\d+>|\[Shot\s+\d+\]|\[\d+(?:\.\d+)?s\]|\d{2}:\d{2}(?:\.\d+)?|\(S\d+\)|</?(?:scenetrans|cutoff)>|^[ \t]*(?:subject_definitions|summary|retention_analysis|detailed_description|overall_soundscape|non_diegetic_music|task|duration|resolution|aspect_ratio|fps|prompt)[ \t]*:`)

// h3BracketDurationRE matches the Chinese spelling of a bracketed shot-time marker.
var h3BracketDurationRE = regexp.MustCompile(`\[(\d+(?:\.\d+)?)\s*秒\]`)

// h3MarkerRE matches opaque translation markers, including harmless bracket/space
// drift produced by some models, for example "[ [[T11]]]" or "[[T11]]".
// These markers are internal implementation details and must never reach a saved H3 prompt.
var h3MarkerRE = regexp.MustCompile(`(?:\[\s*){2,3}([TD])\s*(\d+)(?:\s*\]){2,3}`)

// canonicalizeH3Structure renders Chinese spellings of protocol markers in the English
// form, so a Chinese master and its English execution draft expose the same vocabulary.
// Dialogue blocks are left untouched because they must stay byte-identical.
func canonicalizeH3Structure(text string) string {
	var out strings.Builder
	rest := text
	for {
		loc := dialogueBlockRE.FindStringIndex(rest)
		if loc == nil {
			out.WriteString(h3BracketDurationRE.ReplaceAllString(rest, "[$1s]"))
			return out.String()
		}
		out.WriteString(h3BracketDurationRE.ReplaceAllString(rest[:loc[0]], "[$1s]"))
		out.WriteString(rest[loc[0]:loc[1]])
		rest = rest[loc[1]:]
	}
}

// h3ProtocolTokens lists the structure tokens of a prompt in order.
func h3ProtocolTokens(s string) []string {
	return h3ProtocolTokenRE.FindAllString(canonicalizeH3Structure(s), -1)
}

// maskH3Structure hides every structure token and every dialogue block behind opaque
// markers, so the translator only sees natural-language prose and cannot alter the
// protocol. restoreH3Structure puts the original bytes back in place.
func maskH3Structure(source string) (string, []string) {
	canonical := canonicalizeH3Structure(source)
	var tokens []string
	masked := h3ProtocolTokenRE.ReplaceAllStringFunc(canonical, func(token string) string {
		tokens = append(tokens, token)
		kind := "T"
		if strings.HasPrefix(token, "<d>") {
			kind = "D"
		}
		return fmt.Sprintf("[[[%s%d]]]", kind, len(tokens)-1)
	})
	return masked, tokens
}

// restoreH3Structure rewrites markers back to their original content and reports the
// markers the translator dropped or corrupted.
func restoreH3Structure(masked string, tokens []string) (string, []string) {
	var issues []string
	seen := make([]bool, len(tokens))
	next := 0
	out := h3MarkerRE.ReplaceAllStringFunc(masked, func(marker string) string {
		match := h3MarkerRE.FindStringSubmatch(marker)
		index, err := strconv.Atoi(match[2])
		if err != nil || index < 0 || index >= len(tokens) {
			issues = append(issues, "未知标记")
			return marker
		}
		expectedKind := "T"
		if strings.HasPrefix(tokens[index], "<d>") {
			expectedKind = "D"
		}
		// The marker kind is an implementation detail, not user content. Some
		// translators preserve the index but rewrite T/D. The original token is
		// authoritative and is restored by index, so a kind mismatch must never
		// block the preview.
		_ = expectedKind
		if seen[index] {
			issues = append(issues, fmt.Sprintf("标记 %d 重复", index+1))
		} else if index != next {
			issues = append(issues, fmt.Sprintf("标记 %d 乱序", index+1))
		}
		seen[index] = true
		for next < len(tokens) && seen[next] {
			next++
		}
		return tokens[index]
	})
	for i := range tokens {
		if !seen[i] {
			issues = append(issues, fmt.Sprintf("标记 %d 缺失", i+1))
		}
	}
	if strings.Contains(out, "[[[") {
		issues = append(issues, "存在损坏或未知标记")
	}
	return out, issues
}

// restoreH3StructureLenient never turns a paid translation into an HTTP error and
// never hides the model output behind the source prompt. When the model kept the
// marker count but changed marker kinds, indices or order, restore authoritative
// tokens by physical position. If markers are missing or duplicated, preserve the
// best-effort restored model output verbatim and return warnings for review.
func restoreH3StructureLenient(translated string, tokens []string) (string, []string) {
	restored, issues := restoreH3Structure(translated, tokens)
	if len(issues) == 0 {
		return restored, nil
	}
	matches := h3MarkerRE.FindAllStringIndex(translated, -1)
	if len(matches) != len(tokens) {
		return restored, issues
	}
	var out strings.Builder
	start := 0
	for i, match := range matches {
		out.WriteString(translated[start:match[0]])
		out.WriteString(tokens[i])
		start = match[1]
	}
	out.WriteString(translated[start:])
	return out.String(), issues
}

// stripH3CodeFence removes a markdown fence the translator may have wrapped around the prompt.
func stripH3CodeFence(raw string) string {
	text := strings.TrimSpace(raw)
	if !strings.HasPrefix(text, "```") {
		return text
	}
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimPrefix(text, "json")
	text = strings.TrimPrefix(text, "markdown")
	text = strings.TrimPrefix(text, "text")
	text = strings.TrimSuffix(strings.TrimSpace(text), "```")
	return strings.TrimSpace(text)
}

func validateH3Translation(source, translated string) error {
	if strings.TrimSpace(translated) == "" {
		return fmt.Errorf("翻译结果为空")
	}
	a, b := h3ProtocolTokens(source), h3ProtocolTokens(translated)
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
	// Translation is a read-only preview of the request snapshot. Capture its
	// provenance before the model call; later edits must not discard paid output.
	previewScene := *sc
	previewScene.VideoPromptMasterZH = strings.TrimSpace(source)
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
		c.JSON(422, gin.H{"error": err.Error()})
		return
	}
	target := "English"
	if language == "zh" {
		target = "Simplified Chinese"
	}
	// The protocol structure is masked out before the call, so the translator can never
	// rewrite <Picture N>, <Subject N>, timestamps or dialogue blocks. The server restores
	// the exact original bytes afterwards.
	masked, tokens := maskH3Structure(source)
	system := "Translate only the natural-language prose in this complete MiniMax H3 protocol prompt into " + target + ". The tokens [[[Tn]]] and [[[Dn]]] are opaque protocol placeholders: copy every one of them exactly once, in the same order, and never translate, renumber, merge or delete them. Keep all other content byte-identical unless it is natural-language prose. Return only the translated prompt."
	translated, err := s.TextProviderFact.Chat(system, masked)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	translated = stripH3CodeFence(translated)
	translated, markerWarnings := restoreH3StructureLenient(translated, tokens)
	response := gin.H{"preview": translated, "prompt": translated, "language": language, "source_hash": h, "token": hashString(h + "\x00" + translated)}
	if len(markerWarnings) > 0 {
		response["warnings"] = markerWarnings
	}
	c.JSON(200, response)
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
	if h3MarkerRE.MatchString(req.ExecPrompt) {
		_, tokens := maskH3Structure(sc.VideoPromptMasterZH)
		restored, _ := restoreH3StructureLenient(req.ExecPrompt, tokens)
		req.ExecPrompt = restored
		if h3MarkerRE.MatchString(req.ExecPrompt) {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "英文执行稿仍含未还原的内部翻译标记，请重新翻译后再确认"})
			return
		}
	}
	current, err := s.sceneVideoPromptSourceHash(sc)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	now := time.Now()
	execHash := hashString(req.ExecPrompt)
	var result *gorm.DB
	txErr := s.DB.Transaction(func(tx *gorm.DB) error {
		result = tx.Model(&models.Scene{}).Where("id = ? AND project_id = ?", sc.ID, sc.ProjectID).Updates(map[string]any{"video_full_prompt": req.ExecPrompt, "video_exec_source_hash": current, "video_exec_confirmed_hash": execHash, "video_exec_confirmed_at": now, "prompt_stale": false})
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
		c.JSON(404, gin.H{"error": "场景不存在"})
		return
	}
	c.JSON(200, gin.H{"ok": true, "source_hash": current, "exec_hash": execHash, "confirmed_at": now})
}
