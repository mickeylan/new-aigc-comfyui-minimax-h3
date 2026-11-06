package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"strings"
	"time"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
)

const maxSceneAdvisoryReports = 20

type SceneAdvisoryReport struct {
	ID         string                 `json:"id"`
	SourceHash string                 `json:"source_hash"`
	CreatedAt  time.Time              `json:"created_at"`
	Provider   string                 `json:"provider,omitempty"`
	Origin     string                 `json:"origin"`
	Result     SceneVisualCheckResult `json:"result"`
}

func parseSceneAdvisoryReports(raw string) ([]SceneAdvisoryReport, error) {
	if strings.TrimSpace(raw) == "" {
		return []SceneAdvisoryReport{}, nil
	}
	var out []SceneAdvisoryReport
	err := json.Unmarshal([]byte(raw), &out)
	return out, err
}

func randomReportID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return hex.EncodeToString(b), nil
}

func (s *Service) HandleSaveSceneAdvisoryReport(c *gin.Context) {
	scene, ok := s.loadScene(c)
	if !ok {
		return
	}
	var req struct {
		SourceHash  string                 `json:"source_hash"`
		Status      string                 `json:"status"`
		Reason      string                 `json:"reason"`
		Checks      []SceneVisualCheckItem `json:"checks"`
		Uncertainty []string               `json:"uncertainty"`
	}
	dec := json.NewDecoder(io.LimitReader(c.Request.Body, 256<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil || dec.Decode(&struct{}{}) != io.EOF {
		c.JSON(400, gin.H{"error": "invalid report"})
		return
	}
	result := SceneVisualCheckResult{Status: req.Status, Reason: req.Reason, Checks: req.Checks, Uncertainty: req.Uncertainty}
	if err := validateSceneVisualCheck(result); err != nil {
		c.JSON(422, gin.H{"error": err.Error()})
		return
	}
	_, imageHash, _, err := s.currentStoryboardImageSnapshot(scene)
	if err != nil || req.SourceHash != sceneVisualSourceHash(scene, imageHash) {
		c.JSON(409, gin.H{"error": "report source is no longer current"})
		return
	}
	reports, err := parseSceneAdvisoryReports(scene.AdvisoryReportsJSON)
	if err != nil {
		c.JSON(409, gin.H{"error": "saved report history is unreadable"})
		return
	}
	id, err := randomReportID()
	if err != nil {
		c.JSON(500, gin.H{"error": "cannot create report"})
		return
	}
	provider := ""
	if s.TextProviderFact != nil {
		provider = s.TextProviderFact.Name()
	}
	report := SceneAdvisoryReport{ID: id, SourceHash: req.SourceHash, CreatedAt: time.Now().UTC(), Provider: provider, Origin: "user_saved", Result: result}
	reports = append(reports, report)
	if len(reports) > maxSceneAdvisoryReports {
		reports = reports[len(reports)-maxSceneAdvisoryReports:]
	}
	raw, _ := json.Marshal(reports)
	q := s.DB.Model(&models.Scene{}).Where("id = ? AND project_id = ? AND content = ? AND image_file = ? AND blocking_json = ? AND COALESCE(advisory_reports_json, '') = ?", scene.ID, scene.ProjectID, scene.Content, scene.ImageFile, scene.BlockingJSON, scene.AdvisoryReportsJSON).UpdateColumn("advisory_reports_json", string(raw))
	if q.Error != nil {
		c.JSON(500, gin.H{"error": "保存检查记录失败"})
		return
	}
	if q.RowsAffected != 1 {
		c.JSON(409, gin.H{"error": "正文、图片、关系或记录已变化，请重新检查后保存"})
		return
	}
	c.JSON(201, report)
}

func (s *Service) HandleGetSceneAdvisoryReports(c *gin.Context) {
	scene, ok := s.loadScene(c)
	if !ok {
		return
	}
	reports, err := parseSceneAdvisoryReports(scene.AdvisoryReportsJSON)
	unreadable := err != nil
	current := ""
	if _, h, _, e := s.currentStoryboardImageSnapshot(scene); e == nil {
		current = sceneVisualSourceHash(scene, h)
	}
	items := make([]gin.H, 0, len(reports))
	for _, r := range reports {
		items = append(items, gin.H{"report": r, "stale": current == "" || r.SourceHash != current})
	}
	c.JSON(200, gin.H{"items": items, "current_source_hash": current, "unreadable": unreadable})
}

func reportByID(reports []SceneAdvisoryReport, id string) (SceneAdvisoryReport, bool) {
	for _, r := range reports {
		if r.ID == id {
			return r, true
		}
	}
	return SceneAdvisoryReport{}, false
}
func imagePromptHash(v string) string { h := sha256.Sum256([]byte(v)); return hex.EncodeToString(h[:]) }

func buildSceneAdvisoryCorrectionPrompt(prompt string, report SceneAdvisoryReport) (string, error) {
	var suggestions []string
	for _, check := range report.Result.Checks {
		suggestions = append(suggestions, "- "+strings.TrimSpace(check.Suggestion))
	}
	if len(suggestions) == 0 {
		return "", nil
	}
	advice := "用户保存检查建议（仅修正画面，不扩写剧情）：\n" + strings.Join(suggestions, "\n")
	trimmed := strings.TrimSpace(prompt)
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, "```") {
		return "", io.ErrUnexpectedEOF
	}
	lower := strings.ToLower(prompt)
	detail := strings.Index(lower, "detailed_description:")
	sound := strings.Index(lower, "overall_soundscape:")
	if detail >= 0 && sound > detail {
		return strings.TrimRight(prompt[:sound], " \n") + "\n" + advice + "\n\n" + prompt[sound:], nil
	}
	// Qwen image prompts are plain text; preserving the entire original and adding a delimited constraint is safe.
	if detail < 0 && sound < 0 && strings.TrimSpace(prompt) != "" {
		return strings.TrimSpace(prompt) + "\n\n【用户保存检查建议】\n" + strings.Join(suggestions, "\n"), nil
	}
	return "", io.ErrUnexpectedEOF
}

func (s *Service) loadAdvisoryReport(c *gin.Context) (*models.Scene, SceneAdvisoryReport, bool) {
	scene, ok := s.loadScene(c)
	if !ok {
		return nil, SceneAdvisoryReport{}, false
	}
	reports, err := parseSceneAdvisoryReports(scene.AdvisoryReportsJSON)
	if err != nil {
		c.JSON(409, gin.H{"error": "saved report history is unreadable"})
		return nil, SceneAdvisoryReport{}, false
	}
	r, found := reportByID(reports, c.Param("rid"))
	if !found {
		c.JSON(404, gin.H{"error": "report not found"})
		return nil, r, false
	}
	return scene, r, true
}

func (s *Service) HandlePreviewSceneAdvisoryCorrection(c *gin.Context) {
	scene, report, ok := s.loadAdvisoryReport(c)
	if !ok {
		return
	}
	_, h, _, err := s.currentStoryboardImageSnapshot(scene)
	if err != nil || sceneVisualSourceHash(scene, h) != report.SourceHash {
		c.JSON(409, gin.H{"error": "report source is stale"})
		return
	}
	proposed, err := buildSceneAdvisoryCorrectionPrompt(scene.ImagePrompt, report)
	if err != nil || proposed == "" {
		c.JSON(422, gin.H{"error": "prompt format cannot be safely corrected"})
		return
	}
	c.JSON(200, gin.H{"report_id": report.ID, "source_hash": report.SourceHash, "prompt_hash": imagePromptHash(scene.ImagePrompt), "original_prompt": scene.ImagePrompt, "proposed_prompt": proposed})
}

func (s *Service) HandleApplySceneAdvisoryCorrection(c *gin.Context) {
	scene, report, ok := s.loadAdvisoryReport(c)
	if !ok {
		return
	}
	var req struct {
		Prompt     string `json:"prompt"`
		SourceHash string `json:"source_hash"`
		PromptHash string `json:"prompt_hash"`
	}
	dec := json.NewDecoder(io.LimitReader(c.Request.Body, (128<<10)+1))
	dec.DisallowUnknownFields()
	if dec.Decode(&req) != nil || dec.Decode(&struct{}{}) != io.EOF || len(req.Prompt) > 128<<10 || strings.TrimSpace(req.Prompt) == "" {
		c.JSON(400, gin.H{"error": "invalid prompt"})
		return
	}
	activeTask := false
	if scene.ImageTaskID != "" {
		var count int64
		if err := s.DB.Model(&models.Task{}).Where("task_id = ? AND status IN ?", scene.ImageTaskID, []string{"pending", "queued", "running"}).Count(&count).Error; err != nil {
			c.JSON(500, gin.H{"error": "无法确认图片任务状态"})
			return
		}
		activeTask = count > 0
	}
	if scene.ImageToken != "" || scene.Status == "image_pending" || activeTask {
		c.JSON(409, gin.H{"error": "image generation is active"})
		return
	}
	_, h, _, err := s.currentStoryboardImageSnapshot(scene)
	if err != nil || req.SourceHash != report.SourceHash || sceneVisualSourceHash(scene, h) != report.SourceHash || req.PromptHash != imagePromptHash(scene.ImagePrompt) {
		c.JSON(409, gin.H{"error": "correction preview is stale"})
		return
	}
	q := s.DB.Model(&models.Scene{}).Where("id=? AND project_id=? AND content=? AND image_prompt=? AND image_file=? AND blocking_json=? AND COALESCE(advisory_reports_json,'')=? AND COALESCE(image_token,'') = '' AND status <> ? AND COALESCE(image_task_id,'') = ?", scene.ID, scene.ProjectID, scene.Content, scene.ImagePrompt, scene.ImageFile, scene.BlockingJSON, scene.AdvisoryReportsJSON, "image_pending", scene.ImageTaskID).Updates(map[string]any{"image_prompt": req.Prompt, "prompt_stale": true})
	if q.Error != nil {
		c.JSON(500, gin.H{"error": "保存修正提示词失败"})
		return
	}
	if q.RowsAffected != 1 {
		c.JSON(409, gin.H{"error": "场景或图片生成任务已变化，请重新预览"})
		return
	}
	c.JSON(200, gin.H{"image_prompt": req.Prompt, "prompt_stale": true})
}
