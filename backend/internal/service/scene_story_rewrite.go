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

type sceneStoryDialogue struct {
	ID         uint   `json:"id"`
	Speaker    string `json:"speaker"`
	SpeechType string `json:"speech_type"`
	Text       string `json:"text"`
}

type sceneStoryRewriteResult struct {
	Content   string               `json:"content"`
	Dialogues []sceneStoryDialogue `json:"dialogues"`
}

// HandlePreviewSceneStory rewrites one scene as natural story prose. It is deliberately read-only.
func (s *Service) HandlePreviewSceneStory(c *gin.Context) {
	scene, ok := s.loadScene(c)
	if !ok {
		return
	}
	var req struct {
		Content string `json:"content"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2<<20)
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Content) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "当前场景正文不能为空"})
		return
	}
	if s.Skills == nil || s.TextProviderFact == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "AI 文本服务未配置"})
		return
	}
	var dialogues []models.Dialogue
	if err := s.DB.Where("project_id = ? AND scene_id = ?", scene.ProjectID, scene.ID).Order("`order`, id").Find(&dialogues).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	chapters, err := s.sceneStorySourceChaptersDB(s.DB, scene)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	evidence, err := sceneStoryEvidence(dialogues, chapters)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	sourceHash, err := sceneStorySourceHash(scene, dialogues, chapters)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	const system = `你是影视故事编辑。把当前场景正文、已保存对白与给定小说原文证据自然融合成一段连贯中文叙事。只输出普通JSON对象：{"content":"...","dialogues":[{"id":1,"speaker":"...","speech_type":"...","text":"..."}]}，不要Markdown。
音效只写成非人声声响，可将历史上误标为对白的音效提议为sound_effect，不得要求任何角色念出音效。已保存的speech_type、speaker可能是待修复错误，不得把它们当作原文事实。只取本场已分配台词，证据中的其他台词属于前后边界不得引入本场。
正文应自然交代原文语境中的说话人、画外位置和动作时序，不得输出【原文语境】、【对白】、【待确认】等机械标签，不得在正文末尾附对白清单或元数据。每句已保存对白必须逐字、按原顺序及原出现次数写入正文，不得新增任何引号内台词。dialogues必须逐项保留输入id和text。身份只能来自输入证据；原文仅说明后方远处男子时，写“远处追兵男子”且speech_type为offscreen_dialogue，不得猜成某个已知角色、儿童或虬髯人物。普通运镜措辞不构成语义硬限制。`
	user := fmt.Sprintf("当前正文：\n%s\n\n已保存对白（顺序权威）：\n%s\n\n本集原文匹配段落（只作事实依据）：\n%s", strings.TrimSpace(req.Content), mustJSON(dialoguesForStory(dialogues)), evidence)
	output, err := s.Skills.ChatWithConfiguredOrFallbackSkill(scene.ProjectID, models.SkillStageStoryboard, sceneContentExpandSkillCode, s.TextProviderFact, system, user, map[string]string{"scene_id": fmt.Sprint(scene.ID), "source_hash": sourceHash, "scene_content": strings.TrimSpace(req.Content), "context": evidence, "requirements": "自然整合原文对白、明确归属及动作时序，只预览，不写库"})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "AI 场景叙事改写失败: " + err.Error()})
		return
	}
	result, err := parseSceneStoryRewrite(output)
	if err == nil {
		err = validateSceneStoryRewrite(result, dialogues, chapters)
	}
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "AI 场景叙事结果无效: " + err.Error()})
		return
	}
	warnings := sceneStoryRewriteWarnings(result, dialogues)
	c.JSON(200, gin.H{"content": result.Content, "dialogues": result.Dialogues, "source_hash": sourceHash, "warnings": warnings, "persisted": false})
}

func (s *Service) sceneStorySourceChapters(scene *models.Scene) ([]models.Chapter, error) {
	return s.sceneStorySourceChaptersDB(s.DB, scene)
}

func (s *Service) sceneStorySourceChaptersDB(db *gorm.DB, scene *models.Scene) ([]models.Chapter, error) {
	var ep models.EpisodeAdaptation
	if err := db.Where("project_id = ? AND episode_n = ?", scene.ProjectID, scene.EpisodeN).First(&ep).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("本集没有原文改编范围")
		}
		return nil, err
	}
	var ids []uint
	rawIDs := strings.TrimSpace(ep.SourceChapterIDs)
	if rawIDs == "" {
		return nil, fmt.Errorf("本集 source_chapter_ids 为空")
	}
	if err := json.Unmarshal([]byte(rawIDs), &ids); err != nil {
		return nil, fmt.Errorf("本集 source_chapter_ids 无效: %w", err)
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("本集 source_chapter_ids 为空")
	}
	seen := make(map[uint]bool, len(ids))
	for _, id := range ids {
		if id == 0 || seen[id] {
			return nil, fmt.Errorf("本集 source_chapter_ids 包含无效或重复ID: %d", id)
		}
		seen[id] = true
	}
	var chapters []models.Chapter
	if err := db.Where("project_id = ? AND id IN ?", scene.ProjectID, ids).Order("chapter_order").Find(&chapters).Error; err != nil {
		return nil, err
	}
	if len(chapters) != len(ids) {
		return nil, fmt.Errorf("本集 source_chapter_ids 存在缺失或非本项目章节: 期望%d，找到%d", len(ids), len(chapters))
	}
	if ep.ChapterStart > 0 && ep.ChapterEnd >= ep.ChapterStart {
		for _, ch := range chapters {
			if ch.Order < ep.ChapterStart || ch.Order > ep.ChapterEnd {
				return nil, fmt.Errorf("章节%d(order=%d)超出本集映射范围%d-%d", ch.ID, ch.Order, ep.ChapterStart, ep.ChapterEnd)
			}
		}
	}
	return chapters, nil
}

const sceneStoryEvidenceMaxBytes = 100000

type sceneStoryEvidenceCandidate struct {
	chapterID uint
	order     int
	offset    int
	context   string
	speaker   string
}

func sceneStoryEvidence(ds []models.Dialogue, chapters []models.Chapter) (string, error) {
	counts := make(map[string]int)
	for _, d := range ds {
		if d.SpeechType != "sound_effect" {
			counts[d.Text]++
		}
	}
	used := make(map[string]int)
	var parts []string
	total := 0
	for _, d := range ds {
		if d.SpeechType == "sound_effect" {
			continue
		}
		candidates := sceneStoryFindEvidenceCandidates(d, chapters)
		if len(candidates) == 0 {
			return "", fmt.Errorf("已保存对白未在本集原文中逐字找到: %s", d.Text)
		}
		// A reviewed speaker is useful evidence only when the source context says so.
		if speaker := strings.TrimSpace(d.Character); speaker != "" {
			var matched []sceneStoryEvidenceCandidate
			for _, candidate := range candidates {
				if strings.Contains(candidate.context, speaker) {
					candidate.speaker = speaker
					matched = append(matched, candidate)
				}
			}
			if len(matched) > 0 {
				candidates = matched
			}
		}
		// Repeated lines assigned to this scene consume different source occurrences.
		// A single line keeps every candidate because scenes carry no source offset.
		if counts[d.Text] > 1 {
			i := used[d.Text]
			used[d.Text]++
			if i < len(candidates) {
				candidates = candidates[i : i+1]
			}
		}
		var blocks []string
		for _, candidate := range candidates {
			speaker := candidate.speaker
			if speaker == "" {
				speaker = "原文上下文未唯一确认"
			}
			blocks = append(blocks, fmt.Sprintf("候选 chapter_id=%d order=%d source_offset=%d speaker_attribution=%s\n%s",
				candidate.chapterID, candidate.order, candidate.offset, speaker, candidate.context))
		}
		if len(blocks) > 1 {
			blocks = append([]string{"警告：原文有多个逐字候选，当前场景没有source_offset，不得随意选定唯一出处。"}, blocks...)
		}
		part := fmt.Sprintf("对白 id=%d text=%q\n%s", d.ID, d.Text, strings.Join(blocks, "\n\n"))
		if len(parts) > 0 {
			total += len("\n\n")
		}
		total += len(part)
		if total > sceneStoryEvidenceMaxBytes {
			return "", fmt.Errorf("原文证据上下文超过%d字节限制，请缩小本集章节映射或明确对白出处", sceneStoryEvidenceMaxBytes)
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, "\n\n"), nil
}

func sceneStoryFindEvidenceCandidates(d models.Dialogue, chapters []models.Chapter) []sceneStoryEvidenceCandidate {
	var out []sceneStoryEvidenceCandidate
	for _, ch := range chapters {
		for _, match := range sourceQuotePattern.FindAllStringSubmatchIndex(ch.Content, -1) {
			if match[2] < 0 || ch.Content[match[2]:match[3]] != d.Text {
				continue
			}
			start, end := sourceParagraphBounds(ch.Content, match[0], match[1])
			// Only a directly preceding attribution paragraph is allowed to flow in.
			if start > 0 {
				previousEnd := start
				for previousEnd > 0 && (ch.Content[previousEnd-1] == '\n' || ch.Content[previousEnd-1] == '\r') {
					previousEnd--
				}
				previousStart, _ := sourceParagraphBounds(ch.Content, previousEnd, previousEnd)
				previous := strings.TrimSpace(ch.Content[previousStart:previousEnd])
				if strings.HasSuffix(previous, "：") || strings.HasSuffix(previous, ":") {
					start = previousStart
				}
			}
			// Keep at most two immediate reactions, but never cross into later dialogue.
			for n := 0; n < 2; n++ {
				next := skipParagraphBreaks(ch.Content, end)
				if next >= len(ch.Content) {
					break
				}
				_, nextEnd := sourceParagraphBounds(ch.Content, next, next)
				if sourceQuotePattern.MatchString(ch.Content[next:nextEnd]) {
					break
				}
				end = nextEnd
			}
			out = append(out, sceneStoryEvidenceCandidate{chapterID: ch.ID, order: ch.Order, offset: match[0], context: strings.TrimSpace(ch.Content[start:end])})
		}
	}
	return out
}

func dialoguesForStory(ds []models.Dialogue) []sceneStoryDialogue {
	out := make([]sceneStoryDialogue, 0, len(ds))
	for _, d := range ds {
		out = append(out, sceneStoryDialogue{d.ID, d.Character, d.SpeechType, d.Text})
	}
	return out
}
func mustJSON(v any) string { b, _ := json.Marshal(v); return string(b) }
func parseSceneStoryRewrite(raw string) (sceneStoryRewriteResult, error) {
	var r sceneStoryRewriteResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &r); err != nil {
		return r, err
	}
	r.Content = strings.TrimSpace(r.Content)
	if r.Content == "" {
		return r, fmt.Errorf("content为空")
	}
	return r, nil
}
func validateSceneStoryRewrite(r sceneStoryRewriteResult, ds []models.Dialogue, sources ...[]models.Chapter) error {
	if strings.Contains(r.Content, "【待确认】") || strings.Contains(r.Content, "【对白") || strings.Contains(r.Content, "【原文语境】") {
		return fmt.Errorf("正文含机械标签")
	}
	if len(r.Dialogues) != len(ds) {
		return fmt.Errorf("dialogues数量改变")
	}
	allowedTypes := map[string]bool{"dialogue": true, "narration": true, "monologue": true, "offscreen_dialogue": true, "sound_effect": true, "unknown": true}
	expectedQuotes := make([]string, 0, len(ds))
	for i, d := range ds {
		p := r.Dialogues[i]
		if p.ID != d.ID || p.Text != d.Text {
			return fmt.Errorf("第%d句对白id或原文改变", i+1)
		}
		if !allowedTypes[p.SpeechType] {
			return fmt.Errorf("第%d句speech_type不受支持: %s", i+1, p.SpeechType)
		}
		if p.SpeechType == "sound_effect" && d.SpeechType != "sound_effect" {
			// Reclassification must have source evidence, never a loophole for
			// deleting speech. Every matched occurrence must be non-human sound.
			found, speech := false, false
			for _, chapters := range sources {
				for _, chapter := range chapters {
					for _, quote := range sourceQuotePattern.FindAllStringSubmatchIndex(chapter.Content, -1) {
						if chapter.Content[quote[2]:quote[3]] == d.Text {
							found = true
							speech = speech || sourceQuoteIsSpeech(chapter.Content, quote)
						}
					}
				}
			}
			if !found || speech {
				return fmt.Errorf("第%d句不能在缺少明确音效依据时改成音效", i+1)
			}
		}
		if p.SpeechType != "sound_effect" {
			expectedQuotes = append(expectedQuotes, d.Text)
		}
	}
	quotes := sourceQuotePattern.FindAllStringSubmatch(r.Content, -1)
	if len(quotes) != len(expectedQuotes) {
		return fmt.Errorf("正文引号台词数量与对白不一致")
	}
	for i, q := range quotes {
		if q[1] != expectedQuotes[i] {
			return fmt.Errorf("第%d句引号台词不匹配或顺序改变", i+1)
		}
	}
	return nil
}
