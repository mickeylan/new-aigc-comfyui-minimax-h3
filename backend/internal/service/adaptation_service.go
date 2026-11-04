package service

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"comfyui-console/internal/models"
)

const defaultEpisodeContextChars = 48000

type SourceTrace struct {
	ChapterID uint   `json:"chapter_id"`
	ChapterNo int    `json:"chapter_no"`
	Start     int    `json:"start_offset"`
	End       int    `json:"end_offset"`
	Snippet   string `json:"snippet"`
}

type EpisodeContext struct {
	AdaptationRules string            `json:"adaptation_rules"`
	WorldRules      string            `json:"world_rules"`
	MainPlot        string            `json:"main_plot"`
	OpeningState    string            `json:"opening_state"`
	PreviousEnding  string            `json:"previous_ending_state"`
	PreviousContent string            `json:"previous_episode_completed_content,omitempty"`
	NextGoal        string            `json:"next_goal"`
	ChapterAnalyses []json.RawMessage `json:"chapter_analyses"`
	SourceChapters  []SourceChapter   `json:"source_chapters"`
	DialogueLedger  []SourceDialogue  `json:"source_dialogue_ledger"`
	Traces          []SourceTrace     `json:"source_traces"`
}

type SourceChapter struct {
	ChapterID uint   `json:"chapter_id"`
	ChapterNo int    `json:"chapter_no"`
	Content   string `json:"content"`
}

type SourceDialogue struct {
	ChapterID uint   `json:"chapter_id"`
	ChapterNo int    `json:"chapter_no"`
	Offset    int    `json:"offset"`
	Speaker   string `json:"speaker"`
	Text      string `json:"text"`
}

var (
	sourceQuotePattern       = regexp.MustCompile(`[“\"]([^”\"\n]+)[”\"]`)
	prefixAttributionPattern = regexp.MustCompile(`([\p{Han}A-Za-z0-9·]{1,12})(?:说道|说|道|问道|问|答道|答|喝道|怒道|冷声道|沉声道|笑道|开口道|开口说道)[，,:：\s]*$`)
	postAttributionPattern   = regexp.MustCompile(`^\s*([\p{Han}A-Za-z0-9·]{1,12}?)(?:见此|眼睛[^，。]{0,12}|正气[^，。]{0,12}|略一迟疑|沉吟(?:道)?|面色一沉|说道|说|道|问道|问|答道|答|喝道|怒道)(?:[，,。！？!?]|$)`)
)

func extractSourceDialogues(ch models.Chapter, knownNames ...string) []SourceDialogue {
	matches := sourceQuotePattern.FindAllStringSubmatchIndex(ch.Content, -1)
	out := make([]SourceDialogue, 0, len(matches))
	for _, quote := range matches {
		if !sourceQuoteIsSpeech(ch.Content, quote) {
			continue
		}
		speaker := ""
		beforeStart := quote[0] - 64
		if beforeStart < 0 {
			beforeStart = 0
		}
		if m := prefixAttributionPattern.FindStringSubmatch(ch.Content[beforeStart:quote[0]]); len(m) > 1 {
			speaker = m[1]
		} else {
			afterEnd := quote[1] + 64
			if afterEnd > len(ch.Content) {
				afterEnd = len(ch.Content)
			}
			after := ch.Content[quote[1]:afterEnd]
			// Project character names and confirmed aliases are authoritative. Longest
			// first prevents a short alias from stealing a canonical-name prefix.
			for _, name := range knownNames {
				if strings.HasPrefix(strings.TrimSpace(after), name) {
					speaker = name
					break
				}
			}
			if speaker == "" {
				if m := postAttributionPattern.FindStringSubmatch(after); len(m) > 1 {
					speaker = m[1]
				}
			}
		}
		out = append(out, SourceDialogue{ChapterID: ch.ID, ChapterNo: ch.Order, Offset: quote[2], Speaker: speaker, Text: ch.Content[quote[2]:quote[3]]})
	}
	return out
}

type AdaptationService struct {
	db       *gorm.DB
	provider TextProvider
	skills   *SkillService
	projects *ProjectService
}

func NewAdaptationService(db *gorm.DB, provider TextProvider, skills *SkillService, projects *ProjectService) *AdaptationService {
	return &AdaptationService{db: db, provider: provider, skills: skills, projects: projects}
}

func validateStrategy(v *models.AdaptationStrategy) error {
	modes := map[string]bool{"faithful": true, "cinematic": true, "enhanced": true, "free": true}
	if !modes[v.Mode] {
		return fmt.Errorf("mode must be faithful, cinematic, enhanced, or free")
	}
	if v.ChapterStart < 1 || v.ChapterEnd < v.ChapterStart {
		return fmt.Errorf("invalid chapter range")
	}
	if v.TargetEpisodes < 1 || v.TargetEpisodes > 500 {
		return fmt.Errorf("target_episodes must be between 1 and 500")
	}
	if v.TargetDuration == 0 {
		v.TargetDuration = 180
	}
	if v.TargetScenes == 0 {
		v.TargetScenes = 25
	}
	if v.TargetDuration < 30 || v.TargetDuration > 600 {
		return fmt.Errorf("target_duration must be between 30 and 600 seconds")
	}
	if v.TargetScenes < 1 || v.TargetScenes > 100 {
		return fmt.Errorf("target_scenes must be between 1 and 100")
	}
	for _, raw := range []string{v.MustKeepJSON, v.DroppableJSON} {
		if raw != "" && !json.Valid([]byte(raw)) {
			return fmt.Errorf("event lists must be valid JSON")
		}
	}
	return nil
}

func (s *AdaptationService) SaveStrategy(projectID uint, input models.AdaptationStrategy) (*models.AdaptationStrategy, error) {
	input.ID, input.ProjectID = 0, projectID
	if input.Mode == "" {
		input.Mode = "cinematic"
	}
	if err := validateStrategy(&input); err != nil {
		return nil, err
	}
	var old models.AdaptationStrategy
	if err := s.db.Where("project_id = ?", projectID).First(&old).Error; err == nil {
		input.ID, input.Version = old.ID, old.Version+1
	} else if err != gorm.ErrRecordNotFound {
		return nil, err
	} else {
		input.Version = 1
	}
	input.Status = "draft"
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&input).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.EpisodeAdaptation{}).Where("project_id = ?", projectID).Update("status", "stale").Error; err != nil {
			return err
		}
		return tx.Model(&models.StoryBible{}).Where("project_id = ?", projectID).Update("adaptation_rules", strategyJSON(input)).Error
	})
	return &input, err
}

func strategyJSON(v models.AdaptationStrategy) string { b, _ := json.Marshal(v); return string(b) }

func (s *AdaptationService) GetStrategy(projectID uint) (*models.AdaptationStrategy, error) {
	var v models.AdaptationStrategy
	if err := s.db.Where("project_id = ?", projectID).First(&v).Error; err != nil {
		return nil, err
	}
	return &v, nil
}

func (s *AdaptationService) List(projectID uint) ([]models.EpisodeAdaptation, error) {
	var rows []models.EpisodeAdaptation
	err := s.db.Where("project_id = ?", projectID).Order("episode_n ASC").Find(&rows).Error
	return rows, err
}

func validateAdaptation(v *models.EpisodeAdaptation) error {
	if v.EpisodeN < 1 {
		return fmt.Errorf("episode_n must be positive")
	}
	if v.ChapterStart < 1 || v.ChapterEnd < v.ChapterStart {
		return fmt.Errorf("invalid chapter range")
	}
	if v.TargetDuration == 0 {
		v.TargetDuration = 180
	}
	if v.TargetScenes == 0 {
		v.TargetScenes = 25
	}
	if v.TargetDuration < 162 || v.TargetDuration > 198 {
		return fmt.Errorf("target_duration must be within 180 seconds ±10%%")
	}
	if v.TargetScenes < 20 || v.TargetScenes > 30 {
		return fmt.Errorf("target_scenes must be between 20 and 30")
	}
	var ids []uint
	if json.Unmarshal([]byte(v.SourceChapterIDs), &ids) != nil || len(ids) == 0 {
		return fmt.Errorf("source_chapter_ids must be a non-empty JSON array")
	}
	for _, raw := range []string{v.MustKeepEvents, v.OptionalEvents, v.OmittedEvents} {
		if raw != "" && !json.Valid([]byte(raw)) {
			return fmt.Errorf("event fields must be valid JSON")
		}
	}
	return nil
}

func (s *AdaptationService) Generate(projectID uint) ([]models.EpisodeAdaptation, error) {
	var project models.Project
	if err := s.db.First(&project, projectID).Error; err != nil {
		return nil, err
	}
	strategy, err := s.GetStrategy(projectID)
	if err != nil {
		return nil, fmt.Errorf("adaptation strategy is required")
	}
	if strategy.TargetEpisodes > DefaultBatchSizeMax {
		return nil, fmt.Errorf("单次分集规划最多 %d 集；当前要求 %d 集，请改用滚动批次", DefaultBatchSizeMax, strategy.TargetEpisodes)
	}
	var bible models.StoryBible
	if err := s.db.Where("project_id = ?", projectID).First(&bible).Error; err != nil || bible.Status != "approved" {
		return nil, fmt.Errorf("approved story bible is required")
	}
	var analyses []models.Chapter
	if err := s.db.Where("project_id = ? AND chapter_order BETWEEN ? AND ? AND analysis_status = ?", projectID, strategy.ChapterStart, strategy.ChapterEnd, "ready").Order("chapter_order").Find(&analyses).Error; err != nil {
		return nil, err
	}
	if len(analyses) == 0 {
		return nil, fmt.Errorf("no current chapter analyses in strategy range")
	}
	payload, _ := json.Marshal(analyses)
	raw, err := s.skills.ChatWithSkill(projectID, models.SkillStageAdaptationPlan, s.provider, "Map source analyses into episodes. Output JSON only.", "", map[string]string{"strategy": strategyJSON(*strategy), "story_bible": bibleJSON(bible), "chapter_analyses": string(payload)})
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Episodes []models.EpisodeAdaptation `json:"episodes"`
	}
	normalized, normalizeErr := normalizeAdaptationJSON(raw)
	if normalizeErr != nil {
		return nil, normalizeErr
	}
	if err := parseJSONObject(normalized, &parsed); err != nil {
		return nil, err
	}
	if len(parsed.Episodes) != strategy.TargetEpisodes {
		return nil, fmt.Errorf("model returned %d episodes, expected %d", len(parsed.Episodes), strategy.TargetEpisodes)
	}
	seen := map[int]bool{}
	for i := range parsed.Episodes {
		ep := &parsed.Episodes[i]
		ep.ProjectID = projectID
		if ep.EpisodeN == 0 {
			ep.EpisodeN = i + 1
		}
		if ep.TargetDuration == 0 {
			ep.TargetDuration = strategy.TargetDuration
		}
		if ep.TargetScenes == 0 {
			ep.TargetScenes = strategy.TargetScenes
		}
		if seen[ep.EpisodeN] {
			return nil, fmt.Errorf("duplicate episode_n")
		}
		seen[ep.EpisodeN] = true
		if err := validateAdaptation(ep); err != nil {
			return nil, fmt.Errorf("episode %d: %w", ep.EpisodeN, err)
		}
		ep.Status, ep.Version, ep.ContinuityStatus = "draft", 1, "pending"
		ep.SourceDigest, err = s.sourceDigest(projectID, ep.SourceChapterIDs)
		if err != nil {
			return nil, err
		}
	}
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("project_id = ?", projectID).Delete(&models.EpisodeAdaptation{}).Error; err != nil {
			return err
		}
		if err := tx.Create(&parsed.Episodes).Error; err != nil {
			return err
		}
		plan := dramaPlan{Title: bible.Premise, Logline: bible.Premise, Core: bible.MainPlot}
		for _, ep := range parsed.Episodes {
			plan.Episodes = append(plan.Episodes, struct {
				N              int     `json:"n"`
				Title          string  `json:"title"`
				Brief          string  `json:"brief"`
				Hook           string  `json:"hook"`
				Tag            string  `json:"tag"`
				TargetDuration float64 `json:"target_duration"`
				TargetScenes   int     `json:"target_scenes"`
			}{ep.EpisodeN, ep.Title, ep.AdaptationGoal, ep.Hook, "", ep.TargetDuration, ep.TargetScenes})
		}
		b, _ := json.Marshal(plan)
		return tx.Model(&models.Project{}).Where("id = ?", projectID).Updates(map[string]any{"plan": string(b), "episodes": len(parsed.Episodes)}).Error
	})
	return parsed.Episodes, err
}

func bibleJSON(v models.StoryBible) string { b, _ := json.Marshal(v); return string(b) }

func (s *AdaptationService) sourceDigest(projectID uint, rawIDs string) (string, error) {
	var ids []uint
	if err := json.Unmarshal([]byte(rawIDs), &ids); err != nil {
		return "", err
	}
	var rows []models.Chapter
	if err := s.db.Where("project_id = ? AND id IN ?", projectID, ids).Find(&rows).Error; err != nil {
		return "", err
	}
	if len(rows) != len(ids) {
		return "", fmt.Errorf("source chapter does not belong to project")
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Order < rows[j].Order })
	var b strings.Builder
	for _, ch := range rows {
		b.WriteString(strconv.FormatUint(uint64(ch.ID), 10))
		b.WriteByte(':')
		b.WriteString(ch.ContentHash)
		b.WriteByte(';')
	}
	return novelHash(b.String()), nil
}

func (s *AdaptationService) Update(projectID uint, episode int, updates map[string]any) (*models.EpisodeAdaptation, error) {
	var row models.EpisodeAdaptation
	if err := s.db.Where("project_id = ? AND episode_n = ?", projectID, episode).First(&row).Error; err != nil {
		return nil, err
	}
	if row.BatchID != nil {
		var batch models.PlanningBatch
		if err := s.db.First(&batch, *row.BatchID).Error; err != nil {
			return nil, err
		}
		if batch.Status == BatchStatusApproved || batch.Status == BatchStatusProduced {
			return nil, fmt.Errorf("已审核或已生产批次不能修改分集映射")
		}
	}
	allowed := map[string]bool{"title": true, "chapter_start": true, "chapter_end": true, "source_chapter_ids": true, "target_duration": true, "target_scenes": true, "adaptation_goal": true, "must_keep_events": true, "optional_events": true, "omitted_events": true, "opening_state": true, "ending_state": true, "hook": true}
	filtered := map[string]any{}
	for key, value := range updates {
		if allowed[key] {
			filtered[key] = value
		}
	}
	if len(filtered) == 0 {
		return nil, fmt.Errorf("没有可更新字段")
	}
	b, _ := json.Marshal(filtered)
	if err := json.Unmarshal(b, &row); err != nil {
		return nil, err
	}
	row.ProjectID, row.EpisodeN = projectID, episode
	if err := validateAdaptation(&row); err != nil {
		return nil, err
	}
	digest, err := s.sourceDigest(projectID, row.SourceChapterIDs)
	if err != nil {
		return nil, err
	}
	row.SourceDigest, row.Status, row.ContinuityStatus, row.Version = digest, "draft", "pending", row.Version+1
	if err := s.db.Save(&row).Error; err != nil {
		return nil, err
	}
	if row.BatchID != nil {
		_ = s.db.Model(&models.BatchStateSnapshot{}).Where("batch_id = ?", *row.BatchID).Updates(map[string]any{"status": "draft", "approved_at": nil}).Error
	}
	return &row, nil
}

func (s *AdaptationService) Approve(projectID uint, episodes []int) error {
	if len(episodes) == 0 {
		return fmt.Errorf("episodes are required")
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		for _, n := range episodes {
			var row models.EpisodeAdaptation
			if err := tx.Where("project_id = ? AND episode_n = ?", projectID, n).First(&row).Error; err != nil {
				return err
			}
			if row.Status != "draft" {
				return fmt.Errorf("episode %d is not an approvable draft", n)
			}
			if err := validateAdaptation(&row); err != nil {
				return err
			}
			d, err := s.sourceDigest(projectID, row.SourceChapterIDs)
			if err != nil || d != row.SourceDigest {
				return fmt.Errorf("episode %d source is stale", n)
			}
			if err := tx.Model(&row).Update("status", "approved").Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func keywords(ep models.EpisodeAdaptation) []string {
	raw := strings.Join([]string{ep.Title, ep.AdaptationGoal, ep.MustKeepEvents, ep.OptionalEvents, ep.Hook}, " ")
	parts := strings.FieldsFunc(raw, func(r rune) bool { return strings.ContainsRune(" ,，。；;：:\"'[]{}()（）\n\t", r) })
	seen := map[string]bool{}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if len([]rune(p)) >= 2 && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

func (s *AdaptationService) BuildContext(projectID uint, episode, maxChars int) (*EpisodeContext, error) {
	if maxChars <= 0 || maxChars > defaultEpisodeContextChars {
		maxChars = defaultEpisodeContextChars
	}
	var ep models.EpisodeAdaptation
	if err := s.db.Where("project_id = ? AND episode_n = ?", projectID, episode).First(&ep).Error; err != nil {
		return nil, err
	}
	var bible models.StoryBible
	if err := s.db.Where("project_id = ?", projectID).First(&bible).Error; err != nil {
		return nil, err
	}
	var ids []uint
	_ = json.Unmarshal([]byte(ep.SourceChapterIDs), &ids)
	var chapters []models.Chapter
	if err := s.db.Where("project_id = ? AND id IN ?", projectID, ids).Order("chapter_order").Find(&chapters).Error; err != nil {
		return nil, err
	}
	ctx := &EpisodeContext{AdaptationRules: bible.AdaptationRules, WorldRules: bible.WorldRules, MainPlot: bible.MainPlot, OpeningState: ep.OpeningState, NextGoal: ep.Hook}
	knownNames, err := s.dialogueSpeakerNames(projectID)
	if err != nil {
		return nil, err
	}
	// The approved episode mapping is the authority: summaries and keyword snippets
	// must never replace its source prose or its explicitly attributed dialogue.
	for _, ch := range chapters {
		ctx.SourceChapters = append(ctx.SourceChapters, SourceChapter{ChapterID: ch.ID, ChapterNo: ch.Order, Content: ch.Content})
		ctx.DialogueLedger = append(ctx.DialogueLedger, extractSourceDialogues(ch, knownNames...)...)
	}
	var prev models.EpisodeAdaptation
	if s.db.Where("project_id = ? AND episode_n = ?", projectID, episode-1).First(&prev).Error == nil {
		ctx.PreviousEnding = prev.EndingState
	}
	if s.projects != nil {
		ctx.PreviousContent, _ = s.projects.previousEpisodeProductionFacts(projectID, episode)
	}
	budget := maxChars - len(ctx.AdaptationRules) - len(ctx.WorldRules) - len(ctx.MainPlot) - len(ctx.OpeningState) - len(ctx.PreviousEnding) - len(ctx.PreviousContent) - len(ctx.NextGoal)
	if budget < 0 {
		budget = 0
	}
	for _, ch := range chapters {
		if ch.AnalysisJSON != "" && budget > 0 {
			a := ch.AnalysisJSON
			if len(a) > budget {
				a = a[:budget]
			}
			ctx.ChapterAnalyses = append(ctx.ChapterAnalyses, json.RawMessage(a))
			budget -= len(a)
		}
	}
	ks := keywords(ep)
	for _, ch := range chapters {
		if budget <= 0 {
			break
		}
		traces := findSnippets(ch, ks, budget)
		for _, t := range traces {
			ctx.Traces = append(ctx.Traces, t)
			budget -= len(t.Snippet)
			if budget <= 0 {
				break
			}
		}
	}
	return ctx, nil
}

func (s *AdaptationService) dialogueSpeakerNames(projectID uint) ([]string, error) {
	var names []string
	if s.db.Migrator().HasTable(&models.Character{}) {
		var characters []models.Character
		if err := s.db.Where("project_id = ?", projectID).Find(&characters).Error; err != nil {
			return nil, err
		}
		for _, character := range characters {
			if name := strings.TrimSpace(character.Name); name != "" {
				names = append(names, name)
			}
		}
	}
	if s.db.Migrator().HasTable(&models.CharacterAliasCandidate{}) {
		var aliases []models.CharacterAliasCandidate
		if err := s.db.Where("project_id = ? AND status = ?", projectID, "confirmed").Find(&aliases).Error; err != nil {
			return nil, err
		}
		for _, alias := range aliases {
			if name := strings.TrimSpace(alias.Alias); name != "" {
				names = append(names, name)
			}
			if name := strings.TrimSpace(alias.CanonicalName); name != "" {
				names = append(names, name)
			}
		}
	}
	sort.SliceStable(names, func(i, j int) bool { return len(names[i]) > len(names[j]) })
	return names, nil
}

func findSnippets(ch models.Chapter, keys []string, budget int) []SourceTrace {
	const radius = 500
	seen := map[int]bool{}
	var out []SourceTrace
	for _, k := range keys {
		from := 0
		for {
			i := strings.Index(ch.Content[from:], k)
			if i < 0 {
				break
			}
			i += from
			start, end := i-radius, i+len(k)+radius
			if start < 0 {
				start = 0
			}
			if end > len(ch.Content) {
				end = len(ch.Content)
			}
			start = paragraphStart(ch.Content, start)
			end = paragraphEnd(ch.Content, end)
			if !seen[start] {
				sn := ch.Content[start:end]
				if len(sn) > budget {
					sn = sn[:budget]
					end = start + len(sn)
				}
				out = append(out, SourceTrace{ch.ID, ch.Order, start, end, sn})
				seen[start] = true
				budget -= len(sn)
				if budget <= 0 {
					return out
				}
			}
			from = i + len(k)
		}
	}
	return out
}
func paragraphStart(s string, n int) int {
	if i := strings.LastIndex(s[:n], "\n\n"); i >= 0 {
		return i + 2
	}
	return 0
}
func paragraphEnd(s string, n int) int {
	if i := strings.Index(s[n:], "\n\n"); i >= 0 {
		return n + i
	}
	return len(s)
}

type DialogueCoverage struct {
	Total       int              `json:"total"`
	Covered     int              `json:"covered"`
	Missing     []SourceDialogue `json:"missing"`
	Unbound     int              `json:"unbound,omitempty"`
	OutOfOrder  int              `json:"out_of_order,omitempty"`
	OrderPassed bool             `json:"order_passed"`
}

func dialogueKey(speaker, text string) string { return speaker + "\x00" + text }

// dialogueCoverage is occurrence-aware and checks the source sequence, rather than
// treating a count map as proof that order passed.
func dialogueCoverage(raw string, ledger []SourceDialogue) DialogueCoverage {
	coverage := DialogueCoverage{Total: len(ledger), Missing: []SourceDialogue{}, OrderPassed: true}
	res, err := parseAdaptationScript(raw)
	if err != nil {
		coverage.Missing = append(coverage.Missing, ledger...)
		coverage.OrderPassed = len(ledger) == 0
		return coverage
	}
	var actual []scriptDialogue
	for _, scene := range res.Scenes {
		actual = append(actual, scene.Dialogues...)
	}
	used := make([]bool, len(actual))
	last := -1
	for _, source := range ledger {
		found := -1
		for i := range actual {
			// An unattributed source quote remains diagnostic data, but any actual
			// speaker carrying the exact source text proves that occurrence survived.
			if !used[i] && actual[i].Text == source.Text && (source.Speaker == "" || actual[i].Character == source.Speaker) {
				found = i
				break
			}
		}
		if found < 0 {
			coverage.Missing = append(coverage.Missing, source)
			continue
		}
		used[found] = true
		coverage.Covered++
		if found < last {
			coverage.OutOfOrder++
			coverage.OrderPassed = false
		} else {
			last = found
		}
	}
	return coverage
}

// normalizeAdaptationDialogue is deliberately local to adaptation persistence: model
// aliases are accepted, but an unknown type is retained for review rather than being
// silently dropped or guessed to be dialogue.
func parseAdaptationScript(raw string) (*scriptResult, error) {
	result, err := parseScriptJSON(raw)
	if err != nil {
		return nil, err
	}
	applyAdaptationDialogueAliases(raw, result)
	for i := range result.Scenes {
		for j := range result.Scenes[i].Dialogues {
			d := &result.Scenes[i].Dialogues[j]
			d.SpeechType, d.Character = normalizeAdaptationDialogue(*d)
		}
	}
	return result, nil
}

func applyAdaptationDialogueAliases(raw string, result *scriptResult) {
	var wire struct {
		Scenes []struct {
			Dialogues []map[string]any `json:"dialogues"`
		} `json:"scenes"`
	}
	if parseJSONObject(raw, &wire) != nil {
		return
	}
	for i := range result.Scenes {
		if i >= len(wire.Scenes) {
			break
		}
		for j := range result.Scenes[i].Dialogues {
			if j >= len(wire.Scenes[i].Dialogues) {
				break
			}
			d := &result.Scenes[i].Dialogues[j]
			field := wire.Scenes[i].Dialogues[j]
			if strings.TrimSpace(d.Character) == "" {
				for _, key := range []string{"character_name", "speaker"} {
					if value, ok := field[key]; ok {
						d.Character = strings.TrimSpace(profileValueText(value))
						if d.Character != "" {
							break
						}
					}
				}
			}
			if strings.TrimSpace(d.SpeechType) == "" {
				if value, ok := field["speechType"]; ok {
					d.SpeechType = strings.TrimSpace(profileValueText(value))
				}
			}
		}
	}
}

func normalizeAdaptationDialogue(dialogue scriptDialogue) (string, string) {
	t := strings.ToLower(strings.TrimSpace(dialogue.SpeechType))
	character := strings.TrimSpace(dialogue.Character)
	aliases := map[string]string{
		"对白": "dialogue", "台词": "dialogue", "对话": "dialogue",
		"旁白": "narration", "画外音": "narration",
		"独白": "monologue", "内心独白": "monologue",
		"画外对白": "offscreen_dialogue",
	}
	if alias, ok := aliases[t]; ok {
		t = alias
	}
	if isNarrationSpeaker(character) {
		t, character = "narration", "旁白"
	} else if t == "" && character != "" {
		t = "dialogue"
	}
	if t == "" {
		t = "unknown"
	}
	return t, character
}

func persistedDialogueCoverage(rows []models.Dialogue, ledger []SourceDialogue) DialogueCoverage {
	coverage := DialogueCoverage{Total: len(ledger), Missing: []SourceDialogue{}, OrderPassed: true}
	used := make([]bool, len(rows))
	last := -1
	for _, row := range rows {
		if strings.TrimSpace(row.Character) == "" {
			coverage.Unbound++
		}
	}
	for _, source := range ledger {
		found := -1
		for i, row := range rows {
			if !used[i] && strings.TrimSpace(row.Text) == source.Text && (source.Speaker == "" || strings.TrimSpace(row.Character) == source.Speaker) {
				found = i
				break
			}
		}
		if found < 0 {
			coverage.Missing = append(coverage.Missing, source)
			continue
		}
		used[found] = true
		coverage.Covered++
		if found < last {
			coverage.OutOfOrder++
			coverage.OrderPassed = false
		} else {
			last = found
		}
	}
	return coverage
}

// restoreDialoguesAlreadyLocatedByContent only repairs a structured-dialogue omission
// when the model itself put the exact source text in a scene. It never invents a scene
// or guesses a narrative position.
func restoreDialoguesAlreadyLocatedByContent(raw string, ledger []SourceDialogue) (string, error) {
	res, err := parseAdaptationScript(raw)
	if err != nil {
		return raw, nil
	}
	// Assign at most one ledger occurrence to each literal occurrence in scene
	// content. This prevents one repeated content line from manufacturing several
	// structured dialogues.
	contentRemaining := make([]map[string]int, len(res.Scenes))
	for i := range res.Scenes {
		contentRemaining[i] = make(map[string]int)
	}
	type locatedDialogue struct {
		source SourceDialogue
		scene  int
	}
	located := make([]locatedDialogue, 0, len(ledger))
	for _, source := range ledger {
		for i := range res.Scenes {
			remaining, seen := contentRemaining[i][source.Text]
			if !seen {
				remaining = strings.Count(res.Scenes[i].Content, source.Text)
			}
			if remaining > 0 {
				contentRemaining[i][source.Text] = remaining - 1
				located = append(located, locatedDialogue{source: source, scene: i})
				break
			}
		}
	}

	usedExisting := make([][]bool, len(res.Scenes))
	ordered := make([][]scriptDialogue, len(res.Scenes))
	for i := range res.Scenes {
		usedExisting[i] = make([]bool, len(res.Scenes[i].Dialogues))
	}
	for _, item := range located {
		scene := &res.Scenes[item.scene]
		found := -1
		for i := range scene.Dialogues {
			if !usedExisting[item.scene][i] && scene.Dialogues[i].Text == item.source.Text {
				found = i
				break
			}
		}
		if found >= 0 {
			dialogue := scene.Dialogues[found]
			usedExisting[item.scene][found] = true
			if item.source.Speaker != "" {
				dialogue.Character = item.source.Speaker
			}
			ordered[item.scene] = append(ordered[item.scene], dialogue)
		} else {
			ordered[item.scene] = append(ordered[item.scene], scriptDialogue{Character: item.source.Speaker, SpeechType: "dialogue", Text: item.source.Text})
		}
	}
	for i := range res.Scenes {
		for j, dialogue := range res.Scenes[i].Dialogues {
			if !usedExisting[i][j] {
				ordered[i] = append(ordered[i], dialogue)
			}
		}
		res.Scenes[i].Dialogues = ordered[i]
	}
	// The generic script DTO does not contain adaptation-only fields such as
	// opening_state/ending_state. Patch scenes into the original object instead
	// of dropping those fields through a lossy DTO round-trip.
	var original map[string]json.RawMessage
	if err := parseJSONObject(raw, &original); err != nil {
		return raw, err
	}
	scenesJSON, err := json.Marshal(res.Scenes)
	if err != nil {
		return raw, err
	}
	original["scenes"] = scenesJSON
	encoded, err := json.Marshal(original)
	return string(encoded), err
}

func structuredDialoguesPreserved(original, candidate string) bool {
	before, err := parseAdaptationScript(original)
	if err != nil {
		return false
	}
	after, err := parseAdaptationScript(candidate)
	if err != nil {
		return false
	}
	remaining := make(map[string]int)
	for _, scene := range after.Scenes {
		for _, dialogue := range scene.Dialogues {
			remaining[dialogueKey(dialogue.Character, dialogue.Text)]++
		}
	}
	for _, scene := range before.Scenes {
		for _, dialogue := range scene.Dialogues {
			key := dialogueKey(dialogue.Character, dialogue.Text)
			if remaining[key] == 0 {
				return false
			}
			remaining[key]--
		}
	}
	return true
}

func improvedRevision(original, candidate string, ledger []SourceDialogue) bool {
	if _, err := parseScriptJSON(candidate); err != nil || !structuredDialoguesPreserved(original, candidate) {
		return false
	}
	before, after := dialogueCoverage(original, ledger), dialogueCoverage(candidate, ledger)
	return after.Covered > before.Covered && after.Covered >= before.Covered
}

func (s *AdaptationService) GenerateScript(projectID uint, episode int) (*models.EpisodeAdaptation, error) {
	var ep models.EpisodeAdaptation
	if err := s.db.Where("project_id = ? AND episode_n = ?", projectID, episode).First(&ep).Error; err != nil {
		return nil, err
	}
	if ep.Status != "approved" && ep.Status != "scripted" && ep.Status != "reviewed" {
		return nil, fmt.Errorf("episode mapping must be approved")
	}
	if episode > 1 {
		var prev models.EpisodeAdaptation
		if err := s.db.Where("project_id = ? AND episode_n = ?", projectID, episode-1).First(&prev).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return nil, fmt.Errorf("请先生成第%d集剧本，再生成第%d集", episode-1, episode)
			}
			return nil, err
		}
		// Regeneration resets continuity review to pending. Review is advisory:
		// the latest completed script, not a stale review flag, is the prerequisite.
		if strings.TrimSpace(prev.Script) == "" {
			return nil, fmt.Errorf("请先生成第%d集剧本，再生成第%d集", episode-1, episode)
		}
	}
	ctx, err := s.BuildContext(projectID, episode, 0)
	if err != nil {
		return nil, err
	}
	input, _ := json.Marshal(ctx)
	system := "只改编提供的上下文并输出JSON。source_chapters是本集审核映射的原文权威，source_dialogue_ledger是按原文顺序提取、仅含明确说话人的对白清单：必须逐字、逐条、按序保留全部清单对白到scenes[].dialogues，不得因180秒或25镜头目标删减；时长和镜头数可为对白保真让步。不得发明原文未提供的咒语；原文仅写念念有词或没有说话时不得创作台词。previous_episode_completed_content是上一集已完成内容的排重权威；本集必须从其结尾后继续，严禁重演、改写后重复、重新介绍或回顾其中已经完成的事件、动作、信息揭示、对白与情感节点。"
	raw, err := s.skills.ChatWithSkill(projectID, models.SkillStageEpisodeAdaptation, s.provider, system, "", map[string]string{"episode_n": strconv.Itoa(episode), "target_duration": fmt.Sprint(ep.TargetDuration), "target_scenes": strconv.Itoa(ep.TargetScenes), "episode_context": string(input)})
	if err != nil {
		return nil, err
	}
	raw, err = s.projects.repairCrossEpisodeDuplicates(projectID, episode, raw)
	if err != nil {
		return nil, err
	}
	raw, err = restoreDialoguesAlreadyLocatedByContent(raw, ctx.DialogueLedger)
	if err != nil {
		return nil, err
	}
	coverage := dialogueCoverage(raw, ctx.DialogueLedger)
	if len(coverage.Missing) > 0 {
		revisionContext, _ := json.Marshal(struct {
			SourceChapters          []SourceChapter  `json:"source_chapters"`
			CurrentStructuredScript json.RawMessage  `json:"current_structured_script"`
			MissingDialogues        []SourceDialogue `json:"missing_dialogues"`
		}{ctx.SourceChapters, json.RawMessage(raw), coverage.Missing})
		revisionSystem := "这是一次且仅一次的原文对白保真修订。输出完整JSON剧本；保持现有视觉字段和已完成剧情不变，把missing_dialogues逐条逐字放回current_structured_script中符合source_chapters剧情顺序的正确既有或必要场景。不得把对白机械追加到末尾，不得猜测空speaker，不得新增原文没有的对白。"
		revised, revisionErr := s.skills.ChatWithSkill(projectID, models.SkillStageEpisodeAdaptation, s.provider, revisionSystem, "", map[string]string{
			"episode_n": strconv.Itoa(episode), "target_duration": fmt.Sprint(ep.TargetDuration), "target_scenes": strconv.Itoa(ep.TargetScenes), "episode_context": string(revisionContext),
		})
		// Invalid, destructive, or non-improving revisions never replace paid output.
		if revisionErr == nil && improvedRevision(raw, revised, ctx.DialogueLedger) {
			raw = revised
			raw, _ = restoreDialoguesAlreadyLocatedByContent(raw, ctx.DialogueLedger)
		}
		coverage = dialogueCoverage(raw, ctx.DialogueLedger)
	}
	var p models.Project
	if err := s.db.First(&p, projectID).Error; err != nil {
		return nil, err
	}
	// The generic renderer may split long dialogue text into video-sized chunks.
	// Source-ledger diagnostics and storage, however, require each source occurrence
	// to remain an exact, auditable record. Render scene structure without dialogue,
	// then persist the authoritative structured dialogue unchanged below.
	structured, parseErr := parseAdaptationScript(raw)
	if parseErr != nil {
		return nil, parseErr
	}
	applyAdaptationDialogueAliases(raw, structured)
	rendered := *structured
	rendered.Scenes = append([]scriptScene(nil), structured.Scenes...)
	for i := range rendered.Scenes {
		rendered.Scenes[i].Dialogues = nil
	}
	renderRaw, marshalErr := json.Marshal(&rendered)
	if marshalErr != nil {
		return nil, marshalErr
	}
	_, _, err = s.projects.generateScriptCore(&p, episode, string(renderRaw), resScriptHandler(true))
	if err != nil {
		return nil, err
	}
	var storedScenes []models.Scene
	if err := s.db.Where("project_id = ? AND episode_n = ? AND archived = ?", projectID, episode, false).Order("`order`").Find(&storedScenes).Error; err != nil {
		return nil, err
	}
	storedSceneIDs := make([]uint, len(storedScenes))
	for i := range storedScenes {
		storedSceneIDs[i] = storedScenes[i].ID
	}
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if len(storedSceneIDs) > 0 {
			if err := tx.Where("project_id = ? AND scene_id IN ?", projectID, storedSceneIDs).Delete(&models.Dialogue{}).Error; err != nil {
				return err
			}
		}
		for i, scene := range structured.Scenes {
			if i >= len(storedScenes) {
				break
			}
			for j, dialogue := range scene.Dialogues {
				speechType, character := normalizeAdaptationDialogue(dialogue)
				if strings.TrimSpace(dialogue.Text) == "" {
					continue
				}
				if err := tx.Create(&models.Dialogue{SceneID: storedScenes[i].ID, ProjectID: projectID, Order: j + 1, Character: character, SpeechType: speechType, Text: strings.TrimSpace(dialogue.Text), Status: "pending"}).Error; err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	var parsed struct {
		Script       string `json:"script"`
		OpeningState any    `json:"opening_state"`
		EndingState  any    `json:"ending_state"`
	}
	_ = parseJSONObject(raw, &parsed)
	state := func(v any, previous string) string {
		if v == nil {
			return previous
		}
		if text, ok := v.(string); ok && (strings.TrimSpace(text) == "" || strings.TrimSpace(text) == "null") {
			return previous
		}
		b, err := json.Marshal(v)
		if err != nil {
			return previous
		}
		return string(b)
	}
	// Report what actually survived persistence in the current, non-archived scenes.
	// Unknown/unbound rows remain reviewable but cannot satisfy a named source quote.
	var persistedDialogues []models.Dialogue
	if len(storedSceneIDs) > 0 {
		if err := s.db.Where("project_id = ? AND scene_id IN ?", projectID, storedSceneIDs).Order("scene_id, `order`").Find(&persistedDialogues).Error; err != nil {
			return nil, err
		}
	}
	coverage = persistedDialogueCoverage(persistedDialogues, ctx.DialogueLedger)
	traces, _ := json.Marshal(ctx.Traces)
	coverageJSON, _ := json.Marshal(coverage)
	ep.Script, ep.OpeningState, ep.EndingState, ep.SourceTracesJSON, ep.DialogueCoverageJSON, ep.Status, ep.ContinuityStatus = parsed.Script, state(parsed.OpeningState, ep.OpeningState), state(parsed.EndingState, ep.EndingState), string(traces), string(coverageJSON), "scripted", "pending"
	if err := s.db.Save(&ep).Error; err != nil {
		return nil, err
	}
	return &ep, nil
}

func (s *AdaptationService) Review(projectID uint, episode int, overrideReason string) (*models.EpisodeAdaptation, error) {
	var ep models.EpisodeAdaptation
	if err := s.db.Where("project_id = ? AND episode_n = ?", projectID, episode).First(&ep).Error; err != nil {
		return nil, err
	}
	if ep.Status != "scripted" {
		return nil, fmt.Errorf("episode must be scripted")
	}
	if strings.TrimSpace(overrideReason) != "" {
		ep.ContinuityStatus, ep.OverrideReason = "overridden", strings.TrimSpace(overrideReason)
		if err := s.db.Save(&ep).Error; err != nil {
			return nil, err
		}
		return &ep, nil
	}
	previous := "{}"
	if episode > 1 {
		var p models.EpisodeAdaptation
		if err := s.db.Where("project_id = ? AND episode_n = ?", projectID, episode-1).First(&p).Error; err == nil {
			previous = p.EndingState
		}
	}
	raw, err := s.skills.ChatWithSkill(projectID, models.SkillStageContinuityReview, s.provider, "Compare states and causal continuity. Output JSON only.", "", map[string]string{"previous_ending": previous, "opening_state": ep.OpeningState, "ending_state": ep.EndingState, "script": ep.Script})
	if err != nil {
		return nil, err
	}
	var result struct {
		Passed bool `json:"passed"`
	}
	if err := parseJSONObject(raw, &result); err != nil {
		return nil, err
	}
	ep.ReviewJSON = outputJSON(raw)
	if result.Passed {
		ep.ContinuityStatus = "passed"
	} else {
		ep.ContinuityStatus = "failed"
	}
	if err := s.db.Save(&ep).Error; err != nil {
		return nil, err
	}
	return &ep, nil
}

type NovelUsageStats struct {
	Calls            int64    `json:"calls"`
	SuccessfulCalls  int64    `json:"successful_calls"`
	InputJobs        int64    `json:"jobs"`
	OutputCharacters int64    `json:"output_characters"`
	DurationMS       int64    `json:"duration_ms"`
	EstimatedCost    *float64 `json:"estimated_cost"`
}

func (s *AdaptationService) Usage(projectID uint) (NovelUsageStats, error) {
	var out NovelUsageStats
	q := s.db.Model(&models.SkillAuditLog{}).Where("project_id = ? AND stage IN ?", projectID, []string{models.SkillStageChapterAnalysis, models.SkillStageArcMerge, models.SkillStageStoryBible, models.SkillStageAdaptationPlan, models.SkillStageEpisodeAdaptation, models.SkillStageContinuityReview})
	if err := q.Count(&out.Calls).Error; err != nil {
		return out, err
	}
	_ = q.Where("success = ?", true).Count(&out.SuccessfulCalls).Error
	var sums struct {
		Output   int64
		Duration int64
	}
	_ = q.Select("COALESCE(SUM(output_length),0) AS output, COALESCE(SUM(duration_ms),0) AS duration").Scan(&sums).Error
	out.OutputCharacters, out.DurationMS = sums.Output, sums.Duration
	_ = s.db.Model(&models.NovelJob{}).Where("project_id = ?", projectID).Count(&out.InputJobs).Error
	return out, nil
}
