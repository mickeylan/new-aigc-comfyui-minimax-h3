package service

import (
	"encoding/json"
	"fmt"
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
	NextGoal        string            `json:"next_goal"`
	ChapterAnalyses []json.RawMessage `json:"chapter_analyses"`
	Traces          []SourceTrace     `json:"source_traces"`
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
	var prev models.EpisodeAdaptation
	if s.db.Where("project_id = ? AND episode_n = ?", projectID, episode-1).First(&prev).Error == nil {
		ctx.PreviousEnding = prev.EndingState
	}
	budget := maxChars - len(ctx.AdaptationRules) - len(ctx.WorldRules) - len(ctx.MainPlot) - len(ctx.OpeningState) - len(ctx.PreviousEnding) - len(ctx.NextGoal)
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

func (s *AdaptationService) GenerateScript(projectID uint, episode int) (*models.EpisodeAdaptation, error) {
	var ep models.EpisodeAdaptation
	if err := s.db.Where("project_id = ? AND episode_n = ?", projectID, episode).First(&ep).Error; err != nil {
		return nil, err
	}
	if ep.Status != "approved" {
		return nil, fmt.Errorf("episode mapping must be approved")
	}
	if episode > 1 {
		var prev models.EpisodeAdaptation
		if err := s.db.Where("project_id = ? AND episode_n = ?", projectID, episode-1).First(&prev).Error; err != nil || (prev.ContinuityStatus != "passed" && prev.ContinuityStatus != "overridden") {
			return nil, fmt.Errorf("previous episode continuity gate is not cleared")
		}
	}
	ctx, err := s.BuildContext(projectID, episode, 0)
	if err != nil {
		return nil, err
	}
	input, _ := json.Marshal(ctx)
	raw, err := s.skills.ChatWithSkill(projectID, models.SkillStageEpisodeAdaptation, s.provider, "Adapt only supplied context. Output JSON only.", "", map[string]string{"episode_n": strconv.Itoa(episode), "target_duration": fmt.Sprint(ep.TargetDuration), "target_scenes": strconv.Itoa(ep.TargetScenes), "episode_context": string(input)})
	if err != nil {
		return nil, err
	}
	var p models.Project
	if err := s.db.First(&p, projectID).Error; err != nil {
		return nil, err
	}
	_, _, err = s.projects.generateScriptCore(&p, episode, raw, resScriptHandler(true))
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Script       string `json:"script"`
		OpeningState any    `json:"opening_state"`
		EndingState  any    `json:"ending_state"`
	}
	_ = parseJSONObject(raw, &parsed)
	state := func(v any) string { b, _ := json.Marshal(v); return string(b) }
	traces, _ := json.Marshal(ctx.Traces)
	ep.Script, ep.OpeningState, ep.EndingState, ep.SourceTracesJSON, ep.Status, ep.ContinuityStatus = parsed.Script, state(parsed.OpeningState), state(parsed.EndingState), string(traces), "scripted", "pending"
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
