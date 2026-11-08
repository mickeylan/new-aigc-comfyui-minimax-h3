package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"comfyui-console/internal/models"
)

const episodeBoundaryInstruction = "分集边界优先于整章覆盖：source_chapters仅为原文查证上下文，不代表本集应演完整章。source_dialogue_ledger仅为本集必保对白；available_source_dialogues为可选原文对白，只能在本集must_keep_events和审核首尾状态范围内使用，禁止为了补齐可选对白重演前集或提前演完后集。previously_completed_source_dialogues是前集已使用内容，禁止补回；reserved_future_events属于后集，禁止提前演出。修复时同样遵守episode_boundary中的这些约束。"

// Required quotes are limited to explicit quotations in the reviewed event list
// when one chapter is deliberately shared between episodes. Other source speech
// remains available for faithful adaptation but is NOT a compulsory whole-chapter
// checklist. This does not rewrite any source text.
func requiredSharedChapterDialogues(available []SourceDialogue, events []string) []SourceDialogue {
	if len(events) == 0 {
		return available
	} // legacy mappings without event allocation
	required := []SourceDialogue{}
	for _, line := range available {
		matched := false
		for _, event := range events {
			for _, delimiters := range [][2]string{{"“", "”"}, {"「", "」"}, {"\"", "\""}} {
				rest := event
				for {
					start := strings.Index(rest, delimiters[0])
					if start < 0 {
						break
					}
					rest = rest[start+len(delimiters[0]):]
					end := strings.Index(rest, delimiters[1])
					if end < 0 {
						break
					}
					quote := sourceQuoteKey(rest[:end])
					rest = rest[end+len(delimiters[1]):]
					if len([]rune(quote)) >= 2 && strings.Contains(sourceQuoteKey(line.Text), quote) {
						matched = true
					}
				}
			}
		}
		if matched {
			required = append(required, line)
		}
	}
	return required
}

func (s *AdaptationService) sharedChapterReservations(projectID uint, episode int, chapters []SourceChapter) (bool, []string, error) {
	var mappings []models.EpisodeAdaptation
	if err := s.db.Where("project_id=? AND episode_n<>?", projectID, episode).Order("episode_n").Find(&mappings).Error; err != nil {
		return false, nil, err
	}
	ids := map[uint]bool{}
	for _, ch := range chapters {
		ids[ch.ChapterID] = true
	}
	shared := false
	reserved := []string{}
	for _, mapping := range mappings {
		var sourceIDs []uint
		if err := json.Unmarshal([]byte(mapping.SourceChapterIDs), &sourceIDs); err != nil {
			continue
		}
		overlap := false
		for _, id := range sourceIDs {
			if ids[id] {
				overlap = true
			}
		}
		if !overlap {
			continue
		}
		shared = true
		if mapping.EpisodeN > episode {
			for _, event := range adaptationEventList(mapping.MustKeepEvents) {
				reserved = append(reserved, fmt.Sprintf("第%d集：%s", mapping.EpisodeN, event))
			}
		}
	}
	return shared, reserved, nil
}

// completedSourceDialogueCounts is occurrence-aware. Only the active generation
// of earlier episodes sharing source chapters can consume a source occurrence.
// Speaker aliases do not create new occurrences of an identical source quote.
func (s *AdaptationService) completedSourceDialogueCounts(projectID uint, episode int, chapters []SourceChapter) (map[string]int, error) {
	counts := map[string]int{}
	if episode <= 1 || len(chapters) == 0 || s.projects == nil {
		return counts, nil
	}
	var earlier []models.EpisodeAdaptation
	if err := s.db.Where("project_id = ? AND episode_n < ?", projectID, episode).Order("episode_n").Find(&earlier).Error; err != nil {
		return nil, err
	}
	chapterIDs := map[uint]bool{}
	for _, ch := range chapters {
		chapterIDs[ch.ChapterID] = true
	}
	for _, ep := range earlier {
		shared := false
		var sourceIDs []uint
		if err := json.Unmarshal([]byte(ep.SourceChapterIDs), &sourceIDs); err != nil {
			return nil, fmt.Errorf("第%d集来源章节无效: %w", ep.EpisodeN, err)
		}
		for _, id := range sourceIDs {
			if chapterIDs[id] {
				shared = true
			}
		}
		if !shared {
			continue
		}
		scenes, err := s.projects.authoritativeEpisodeScenes(projectID, ep.EpisodeN)
		if err != nil {
			return nil, err
		}
		if len(scenes) == 0 {
			continue
		}
		ids := make([]uint, 0, len(scenes))
		for _, scene := range scenes {
			ids = append(ids, scene.ID)
		}
		var dialogues []models.Dialogue
		if err := s.db.Where("project_id = ? AND scene_id IN ?", projectID, ids).Order("scene_id, `order`, id").Find(&dialogues).Error; err != nil {
			return nil, err
		}
		for _, d := range dialogues {
			if key := sourceQuoteKey(d.Text); key != "" {
				counts[key]++
			}
		}
	}
	return counts, nil
}

// Punctuation/whitespace differences are ignored for ownership matching only;
// authoritative source text is never rewritten.
func sourceQuoteKey(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || unicode.IsPunct(r) {
			return -1
		}
		return r
	}, text)
}

func excludeCompletedSourceDialogues(ledger []SourceDialogue, counts map[string]int) ([]SourceDialogue, []SourceDialogue) {
	used := map[string]int{}
	remaining, completed := []SourceDialogue{}, []SourceDialogue{}
	for _, line := range ledger {
		key := sourceQuoteKey(line.Text)
		if key != "" && used[key] < counts[key] {
			completed = append(completed, line)
			used[key]++
		} else {
			remaining = append(remaining, line)
		}
	}
	return remaining, completed
}

// Exhausted speech alone does not rule out later silent action. Conflict requires
// an explicitly quoted mandatory event to demand an already-consumed source line.
// No prose-similarity threshold is used as proof of event ownership.
func (s *AdaptationService) episodeSourceScopeConsumed(projectID uint, episode int, ctx *EpisodeContext) (bool, error) {
	if len(ctx.CompletedDialogues) == 0 || len(ctx.AvailableDialogues) > 0 || len(ctx.MustKeepEvents) == 0 {
		return false, nil
	}
	for _, line := range requiredSharedChapterDialogues(ctx.CompletedDialogues, ctx.MustKeepEvents) {
		if len([]rune(sourceQuoteKey(line.Text))) >= 8 {
			return true, nil
		}
	}
	return false, nil
}

// Long, identical source quotes identify a replay even when all visual prose has
// been paraphrased. Short expressions such as 小心 are not sufficient evidence.
func replayedCompletedSourceDialogues(raw string, completed []SourceDialogue) ([]string, error) {
	doc, err := parseAdaptationScript(raw)
	if err != nil {
		return nil, err
	}
	forbidden := map[string]string{}
	for _, line := range completed {
		if key := sourceQuoteKey(line.Text); len([]rune(key)) >= 12 {
			forbidden[key] = line.Text
		}
	}
	seen := map[string]bool{}
	issues := []string{}
	for i, scene := range doc.Scenes {
		texts := []string{scene.Content}
		for _, d := range scene.Dialogues {
			texts = append(texts, d.Text)
		}
		for _, text := range texts {
			key := sourceQuoteKey(text)
			for quote, original := range forbidden {
				if !seen[quote] && strings.Contains(key, quote) {
					issues = append(issues, fmt.Sprintf("场景%d重演前集已使用的原文对白：%s", i+1, original))
					seen[quote] = true
				}
			}
		}
	}
	return issues, nil
}
