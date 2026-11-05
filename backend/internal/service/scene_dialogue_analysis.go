package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const sceneDialogueAnalysisSkillCode = "scene_dialogue_analysis"

type SceneDialogueAnalysisItem struct {
	Index       int    `json:"index"`
	Text        string `json:"text"`
	Speaker     string `json:"speaker"`
	Addressee   string `json:"addressee"`
	SpeechType  string `json:"speech_type"`
	TimeSegment string `json:"time_segment"`
	NeedsReview bool   `json:"needs_review"`
	Excluded    bool   `json:"excluded"`
}

type SceneDialogueAnalysis struct {
	Items       []SceneDialogueAnalysisItem `json:"items"`
	Source      string                      `json:"source"`
	ContentHash string                      `json:"content_hash"`
}

func sceneDialogueContentHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func currentSavedDialogueAnalysis(sc *models.Scene) (SceneDialogueAnalysis, bool) {
	var saved SceneDialogueAnalysis
	if sc == nil || json.Unmarshal([]byte(sc.DialogueAnalysisJSON), &saved) != nil || saved.Source != "ai_reviewed" {
		return saved, false
	}
	return saved, saved.ContentHash != "" && saved.ContentHash == sceneDialogueContentHash(sc.Content)
}

// extractQuotedDialogue only locates explicitly quoted source spans. It deliberately
// does not guess attribution; slicing by byte offsets preserves source bytes exactly.
func extractQuotedDialogue(content string) []SceneDialogueAnalysisItem {
	items, _ := extractQuotedDialogueSpans(content)
	return items
}

type sceneDialogueQuoteSpan struct {
	start int
	end   int
}

func extractQuotedDialogueSpans(content string) ([]SceneDialogueAnalysisItem, []sceneDialogueQuoteSpan) {
	var out []SceneDialogueAnalysisItem
	var spans []sceneDialogueQuoteSpan
	for pos := 0; pos < len(content); {
		start, width, curly := -1, 1, false
		if content[pos] == '"' {
			start = pos
		} else if strings.HasPrefix(content[pos:], "“") {
			start, width, curly = pos, len("“"), true
		}
		if start < 0 {
			pos++
			continue
		}
		bodyStart := start + width
		closeAt := -1
		if !curly {
			if n := strings.IndexByte(content[bodyStart:], '"'); n >= 0 {
				closeAt = bodyStart + n
			}
		} else if n := strings.Index(content[bodyStart:], "”"); n >= 0 {
			closeAt = bodyStart + n
		}
		if closeAt < 0 {
			pos = bodyStart
			continue
		}
		text := content[bodyStart:closeAt]
		if text != "" {
			out = append(out, SceneDialogueAnalysisItem{Index: len(out), Text: text, Speaker: "待确认", SpeechType: "dialogue", NeedsReview: true})
			spans = append(spans, sceneDialogueQuoteSpan{start: start, end: closeAt + width})
		}
		pos = closeAt + width
	}
	return out, spans
}

// sceneDialogueGenerationContent returns a generation-only copy of Scene.Content.
// A reviewed exclusion applies only to the exact quoted occurrence and content hash
// that the user reviewed; the persisted Scene.Content is never changed.
func sceneDialogueGenerationContent(sc *models.Scene) string {
	if sc == nil {
		return ""
	}
	saved, current := currentSavedDialogueAnalysis(sc)
	if !current {
		return sc.Content
	}
	items, spans := extractQuotedDialogueSpans(sc.Content)
	excluded := make(map[int]bool)
	for _, item := range saved.Items {
		if item.Excluded && item.Index >= 0 && item.Index < len(items) && item.Text == items[item.Index].Text {
			excluded[item.Index] = true
		}
	}
	if len(excluded) == 0 {
		return sc.Content
	}
	var b strings.Builder
	last := 0
	for i, span := range spans {
		if !excluded[i] {
			continue
		}
		b.WriteString(sc.Content[last:span.start])
		last = span.end
	}
	b.WriteString(sc.Content[last:])
	return b.String()
}

func currentSceneDialogueExclusionCount(sc *models.Scene) int {
	saved, current := currentSavedDialogueAnalysis(sc)
	if !current {
		return 0
	}
	items := extractQuotedDialogue(sc.Content)
	count := 0
	for _, item := range saved.Items {
		if item.Excluded && item.Index >= 0 && item.Index < len(items) && item.Text == items[item.Index].Text {
			count++
		}
	}
	return count
}

func parseDialogueAI(raw string, original []SceneDialogueAnalysisItem) ([]SceneDialogueAnalysisItem, error) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "```") {
		raw = strings.TrimPrefix(raw, "```json")
		raw = strings.TrimPrefix(raw, "```")
		raw = strings.TrimSuffix(strings.TrimSpace(raw), "```")
	}
	var payload struct {
		Items []SceneDialogueAnalysisItem `json:"items"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, err
	}
	byIndex := make(map[int]SceneDialogueAnalysisItem, len(payload.Items))
	for _, item := range payload.Items {
		byIndex[item.Index] = item
	}
	out := append([]SceneDialogueAnalysisItem(nil), original...)
	validType := map[string]bool{"dialogue": true, "offscreen_dialogue": true, "narration": true, "monologue": true}
	for i := range out {
		ai, ok := byIndex[i]
		if !ok {
			continue
		}
		// AI text and exclusion are never authoritative. Only metadata is accepted by index.
		out[i].Excluded = false
		out[i].Speaker = strings.TrimSpace(ai.Speaker)
		if out[i].Speaker == "" {
			out[i].Speaker = "待确认"
		}
		out[i].Addressee = strings.TrimSpace(ai.Addressee)
		if validType[ai.SpeechType] {
			out[i].SpeechType = ai.SpeechType
		}
		out[i].TimeSegment = strings.TrimSpace(ai.TimeSegment)
		out[i].NeedsReview = out[i].Speaker == "待确认" || out[i].Speaker == "未知"
	}
	return out, nil
}

func mergeSavedReviewedSceneDialogue(sc *models.Scene, dubs []models.Dialogue) []models.Dialogue {
	if sc == nil || strings.TrimSpace(sc.DialogueAnalysisJSON) == "" {
		return dubs
	}
	saved, current := currentSavedDialogueAnalysis(sc)
	if saved.Source != "ai_reviewed" || (saved.ContentHash != "" && !current) {
		return dubs
	}
	original := extractQuotedDialogue(sc.Content)
	byIndex := make(map[int]SceneDialogueAnalysisItem, len(saved.Items))
	for _, item := range saved.Items {
		if !item.Excluded && item.Index >= 0 && item.Index < len(original) && item.Text == original[item.Index].Text {
			byIndex[item.Index] = item
		}
	}
	out := append([]models.Dialogue(nil), dubs...)
	used := make([]bool, len(out))
	for index, source := range original {
		item, ok := byIndex[index]
		if !ok {
			continue
		}
		found := -1
		for i := range out {
			typeName, _ := normalizeScriptSpeech(out[i].SpeechType, out[i].Character)
			if !used[i] && out[i].Text == source.Text && typeName != "" {
				found = i
				break
			}
		}
		if found >= 0 {
			used[found] = true
			continue
		}
		speaker := strings.TrimSpace(item.Speaker)
		speechType := strings.TrimSpace(item.SpeechType)
		unknown := speaker == "" || speaker == "待确认" || speaker == "未知"
		if unknown {
			speaker, speechType = "待确认", "offscreen_dialogue"
		} else if normalized, normalizedSpeaker := normalizeScriptSpeech(speechType, speaker); normalized != "" {
			speechType, speaker = normalized, normalizedSpeaker
		} else {
			speechType = "dialogue"
		}
		out = append(out, models.Dialogue{SceneID: sc.ID, ProjectID: sc.ProjectID, Order: len(out) + 1, Character: speaker, Addressee: strings.TrimSpace(item.Addressee), SpeechType: speechType, TimeSegment: strings.TrimSpace(item.TimeSegment), Text: source.Text, Source: "ai_reviewed", Status: "pending", Speed: 1, Volume: 1})
		used = append(used, true)
	}
	return out
}

func (s *Service) HandleAnalyzeSceneDialogue(c *gin.Context) {
	scene, ok := s.loadScene(c)
	if !ok {
		return
	}
	original := extractQuotedDialogue(scene.Content)
	analysis := SceneDialogueAnalysis{Items: original, Source: "source_extract", ContentHash: sceneDialogueContentHash(scene.Content)}
	if saved, current := currentSavedDialogueAnalysis(scene); current {
		for _, reviewed := range saved.Items {
			if reviewed.Excluded && reviewed.Index >= 0 && reviewed.Index < len(analysis.Items) && reviewed.Text == analysis.Items[reviewed.Index].Text {
				analysis.Items[reviewed.Index].Excluded = true
			}
		}
	}
	warning := ""
	if len(original) > 0 && s.Skills != nil && s.TextProviderFact != nil {
		indexed, _ := json.Marshal(original)
		system := `你是对白归属分析助手。只输出合法JSON对象{"items":[...]}; 每项必须保留输入index，只分析speaker、addressee、speech_type(dialogue/offscreen_dialogue/narration/monologue)、time_segment。禁止输出或改写台词text；无法确定speaker填“待确认”。不得丢弃任何index。`
		user := fmt.Sprintf("场景原文：%s\n候选台词（text是只读原文）：%s", scene.Content, indexed)
		raw, err := s.Skills.ChatWithConfiguredOrFallbackSkill(scene.ProjectID, models.SkillStageStoryboard, sceneDialogueAnalysisSkillCode, s.TextProviderFact, system, user, map[string]string{"scene_content": scene.Content, "dialogue_items": string(indexed)})
		if err != nil {
			warning = "AI归属分析失败，已保留全部原文台词：" + err.Error()
		} else if items, parseErr := parseDialogueAI(raw, original); parseErr != nil {
			warning = "AI返回格式无效，已保留全部原文台词：" + parseErr.Error()
		} else {
			for i := range items {
				items[i].Excluded = analysis.Items[i].Excluded
			}
			analysis.Items = items
			analysis.Source = "ai_preview"
		}
	} else if len(original) > 0 {
		warning = "AI文本服务未配置，已保留全部原文台词待确认"
	}
	// Analysis is a preview. Persist only after the user explicitly applies it.
	c.JSON(http.StatusOK, gin.H{"analysis": analysis, "persisted": false, "warning": warning})
}

var errSceneDialogueConflict = errors.New("场景正文已变化，请重新分析对白")

func (s *Service) HandleApplySceneDialogue(c *gin.Context) {
	scene, ok := s.loadScene(c)
	if !ok {
		return
	}
	var req SceneDialogueAnalysis
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "对白审阅数据无效"})
		return
	}
	hasExclusion := false
	for _, item := range req.Items {
		hasExclusion = hasExclusion || item.Excluded
	}
	if hasExclusion && req.ContentHash == "" {
		c.JSON(http.StatusConflict, gin.H{"error": "排除对白需要重新分析场景"})
		return
	}
	warnings, applied := []string{}, 0
	validType := map[string]bool{"dialogue": true, "offscreen_dialogue": true, "narration": true, "monologue": true}
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		var current models.Scene
		if err := tx.Where("id=? AND project_id=?", scene.ID, scene.ProjectID).First(&current).Error; err != nil {
			return err
		}
		contentHash := sceneDialogueContentHash(current.Content)
		if req.ContentHash != "" && req.ContentHash != contentHash {
			return errSceneDialogueConflict
		}
		original := extractQuotedDialogue(current.Content)
		var existing []models.Dialogue
		if err := tx.Where("project_id=? AND scene_id=?", current.ProjectID, current.ID).Order("`order`,id").Find(&existing).Error; err != nil {
			return err
		}
		usedExisting := make(map[uint]bool)
		maxOrder := 0
		for _, d := range existing {
			if d.Order > maxOrder {
				maxOrder = d.Order
			}
		}
		for _, item := range req.Items {
			if item.Index < 0 || item.Index >= len(original) || item.Text != original[item.Index].Text {
				warnings = append(warnings, fmt.Sprintf("索引%d原文不匹配，未应用", item.Index))
				continue
			}
			ordinal := 0
			for i := 0; i < item.Index; i++ {
				if original[i].Text == item.Text {
					ordinal++
				}
			}
			found, seen := -1, 0
			for i := range existing {
				if existing[i].Text == item.Text {
					if seen == ordinal {
						found = i
						break
					}
					seen++
				}
			}
			if item.Excluded {
				if found >= 0 {
					usedExisting[existing[found].ID] = true
					if err := tx.Model(&models.Dialogue{}).Where("id=?", existing[found].ID).Update("source", "user_excluded").Error; err != nil {
						return err
					}
				} else {
					maxOrder++
					tombstone := models.Dialogue{ProjectID: current.ProjectID, SceneID: current.ID, Order: maxOrder, Character: strings.TrimSpace(item.Speaker), SpeechType: "unknown", Text: item.Text, Source: "user_excluded", Status: "pending", Speed: 1, Volume: 1}
					if tombstone.Character == "待确认" || tombstone.Character == "未知" {
						tombstone.Character = ""
					}
					if err := tx.Create(&tombstone).Error; err != nil {
						return err
					}
					existing = append(existing, tombstone)
					usedExisting[tombstone.ID] = true
				}
				applied++
				continue
			}
			if strings.TrimSpace(item.Speaker) == "" {
				warnings = append(warnings, fmt.Sprintf("台词%d说话人为空，未写入Dialogue", item.Index+1))
				continue
			}
			if !validType[item.SpeechType] {
				warnings = append(warnings, fmt.Sprintf("台词%d的speech_type无效，未写入Dialogue", item.Index+1))
				continue
			}
			if found >= 0 {
				d := existing[found]
				usedExisting[d.ID] = true
				// Applying reviewed analysis explicitly confirms this source line's metadata.
				// Only produced audio remains protected from a silent voice mismatch.
				if d.Character != item.Speaker && (d.AudioFile != "" || d.Status == "ready" || d.AudioHash != "" || d.AudioTaskID != "") {
					warnings = append(warnings, fmt.Sprintf("台词%d已有音频或审核结果，为避免声音错配未覆盖说话人", item.Index+1))
					continue
				}
				updates := map[string]any{"character": item.Speaker, "addressee": item.Addressee, "speech_type": item.SpeechType, "time_segment": item.TimeSegment, "source": "ai_reviewed"}
				if err := tx.Model(&models.Dialogue{}).Where("id=?", d.ID).Updates(updates).Error; err != nil {
					return err
				}
				applied++
			} else {
				maxOrder++
				d := models.Dialogue{ProjectID: scene.ProjectID, SceneID: scene.ID, Order: maxOrder, Character: item.Speaker, Addressee: item.Addressee, SpeechType: item.SpeechType, TimeSegment: item.TimeSegment, Text: item.Text, Source: "ai_reviewed", Status: "pending", Speed: 1, Volume: 1}
				if err := tx.Create(&d).Error; err != nil {
					return err
				}
				existing = append(existing, d)
				usedExisting[d.ID] = true
				applied++
			}
		}
		if applied > 0 {
			// Dialogue review can add rows or change the authoritative dialogue grouping.
			// Keep shot/media work, but invalidate coordinate metadata derived from the
			// previous dialogue version so prompt generation cannot silently omit lines.
			if err := tx.Model(&models.Shot{}).Where("scene_id = ?", scene.ID).Update("dialogue_ranges_json", "[]").Error; err != nil {
				return err
			}
		}
		for i := range req.Items {
			if !req.Items[i].Excluded && (strings.TrimSpace(req.Items[i].Speaker) == "" || req.Items[i].Speaker == "待确认" || req.Items[i].Speaker == "未知") {
				req.Items[i].NeedsReview = true
			}
		}
		req.Source = "ai_reviewed"
		req.ContentHash = contentHash
		encoded, _ := json.Marshal(struct {
			SceneDialogueAnalysis
			Warnings []string `json:"warnings,omitempty"`
		}{req, warnings})
		updates := map[string]any{"dialogue_analysis_json": string(encoded)}
		if applied > 0 {
			updates["prompt_stale"] = true
		}
		result := tx.Model(&models.Scene{}).Where("id=? AND content=? AND updated_at=?", current.ID, current.Content, current.UpdatedAt).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errSceneDialogueConflict
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, errSceneDialogueConflict) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"applied": applied, "warnings": warnings})
}
