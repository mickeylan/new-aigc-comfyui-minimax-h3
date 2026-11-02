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

const sceneDialogueAnalysisSkillCode = "scene_dialogue_analysis"

type SceneDialogueAnalysisItem struct {
	Index       int    `json:"index"`
	Text        string `json:"text"`
	Speaker     string `json:"speaker"`
	Addressee   string `json:"addressee"`
	SpeechType  string `json:"speech_type"`
	TimeSegment string `json:"time_segment"`
	NeedsReview bool   `json:"needs_review"`
}

type SceneDialogueAnalysis struct {
	Items  []SceneDialogueAnalysisItem `json:"items"`
	Source string                      `json:"source"`
}

// extractQuotedDialogue only locates explicitly quoted source spans. It deliberately
// does not guess attribution; slicing by byte offsets preserves source bytes exactly.
func extractQuotedDialogue(content string) []SceneDialogueAnalysisItem {
	var out []SceneDialogueAnalysisItem
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
		}
		pos = closeAt + width
	}
	return out
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
		// AI text is never authoritative. Only metadata is accepted by index.
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

func (s *Service) HandleAnalyzeSceneDialogue(c *gin.Context) {
	scene, ok := s.loadScene(c)
	if !ok {
		return
	}
	original := extractQuotedDialogue(scene.Content)
	analysis := SceneDialogueAnalysis{Items: original, Source: "source_extract"}
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
			analysis.Items = items
			analysis.Source = "ai_preview"
		}
	} else if len(original) > 0 {
		warning = "AI文本服务未配置，已保留全部原文台词待确认"
	}
	// Analysis is a preview. Persist only after the user explicitly applies it.
	c.JSON(http.StatusOK, gin.H{"analysis": analysis, "persisted": false, "warning": warning})
}

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
	original := extractQuotedDialogue(scene.Content)
	var existing []models.Dialogue
	if err := s.DB.Where("project_id=? AND scene_id=?", scene.ProjectID, scene.ID).Order("`order`,id").Find(&existing).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	warnings, applied := []string{}, 0
	validType := map[string]bool{"dialogue": true, "offscreen_dialogue": true, "narration": true, "monologue": true}
	usedExisting := make(map[uint]bool)
	err := s.DB.Transaction(func(tx *gorm.DB) error {
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
			if strings.TrimSpace(item.Speaker) == "" {
				warnings = append(warnings, fmt.Sprintf("台词%d说话人为空，未写入Dialogue", item.Index+1))
				continue
			}
			if !validType[item.SpeechType] {
				warnings = append(warnings, fmt.Sprintf("台词%d的speech_type无效，未写入Dialogue", item.Index+1))
				continue
			}
			found := -1
			for i := range existing {
				if !usedExisting[existing[i].ID] && existing[i].Text == item.Text && existing[i].Character == item.Speaker {
					found = i
					break
				}
			}
			if found < 0 {
				for i := range existing {
					if !usedExisting[existing[i].ID] && existing[i].Text == item.Text {
						found = i
						break
					}
				}
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
		for i := range req.Items {
			if strings.TrimSpace(req.Items[i].Speaker) == "" || req.Items[i].Speaker == "待确认" || req.Items[i].Speaker == "未知" {
				req.Items[i].NeedsReview = true
			}
		}
		req.Source = "ai_reviewed"
		encoded, _ := json.Marshal(struct {
			SceneDialogueAnalysis
			Warnings []string `json:"warnings,omitempty"`
		}{req, warnings})
		return tx.Model(&models.Scene{}).Where("id=?", scene.ID).Update("dialogue_analysis_json", string(encoded)).Error
	})
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"applied": applied, "warnings": warnings})
}
