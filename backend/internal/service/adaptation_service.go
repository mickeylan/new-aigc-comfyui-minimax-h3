package service

import (
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

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
	ProjectAssetIdentities string            `json:"project_asset_identities,omitempty"`
	EpisodeTitle           string            `json:"episode_title,omitempty"`
	TargetDuration         float64           `json:"target_duration"`
	TargetScenes           int               `json:"target_scenes"`
	AdaptationGoal         string            `json:"adaptation_goal,omitempty"`
	MustKeepEvents         []string          `json:"must_keep_events,omitempty"`
	OptionalEvents         []string          `json:"optional_events,omitempty"`
	OmittedEvents          []string          `json:"omitted_events,omitempty"`
	EndingState            string            `json:"approved_ending_state,omitempty"`
	AdaptationRules        string            `json:"adaptation_rules"`
	WorldRules             string            `json:"world_rules"`
	MainPlot               string            `json:"main_plot"`
	OpeningState           string            `json:"opening_state"`
	PreviousEnding         string            `json:"previous_ending_state"`
	PreviousContent        string            `json:"previous_episode_completed_content,omitempty"`
	NextGoal               string            `json:"next_goal"`
	ChapterAnalyses        []json.RawMessage `json:"chapter_analyses"`
	SourceChapters         []SourceChapter   `json:"source_chapters"`
	DialogueLedger         []SourceDialogue  `json:"source_dialogue_ledger"`
	CompletedDialogues     []SourceDialogue  `json:"previously_completed_source_dialogues,omitempty"`
	AvailableDialogues     []SourceDialogue  `json:"available_source_dialogues,omitempty"`
	ReservedFutureEvents   []string          `json:"reserved_future_events,omitempty"`
	Traces                 []SourceTrace     `json:"source_traces"`
}

// adaptationEventList accepts every shape the planning model has produced for
// must/optional/omitted events: a JSON array, a JSON-encoded string, or plain text.
func adaptationEventList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" || raw == "[]" {
		return nil
	}
	var list []string
	if err := json.Unmarshal([]byte(raw), &list); err == nil {
		out := make([]string, 0, len(list))
		for _, item := range list {
			if item = strings.TrimSpace(item); item != "" {
				out = append(out, item)
			}
		}
		return out
	}
	var wrapped string
	if err := json.Unmarshal([]byte(raw), &wrapped); err == nil {
		return adaptationEventList(wrapped)
	}
	return []string{raw}
}

// Fidelity is judged per clause, not by comparing the whole event string. A faithful
// scene paraphrases an approved event (different verbs, reordered details) while an
// invented scene shares almost nothing. Measuring whole-string trigram overlap made
// correct drafts fail whenever the wording differed.
const (
	mustKeepClausePresent = 0.18 // a clause sharing less than this is truly absent
	mustKeepClauseRatio   = 0.30 // at least a third of the event's clauses must be present
	mustKeepEventFloor    = 0.18 // the event as a whole must still share real wording
	mustKeepBlockRatio    = 0.4  // refuse only when most approved events are absent
)

func eventTrigrams(value string) map[string]struct{} {
	runes := make([]rune, 0, len([]rune(value)))
	for _, r := range strings.ToLower(value) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			runes = append(runes, r)
		}
	}
	out := map[string]struct{}{}
	for i := 0; i+2 < len(runes); i++ {
		out[string(runes[i:i+3])] = struct{}{}
	}
	return out
}

func eventCorpus(scenes []string) map[string]struct{} {
	corpus := map[string]struct{}{}
	for _, scene := range scenes {
		for gram := range eventTrigrams(scene) {
			corpus[gram] = struct{}{}
		}
	}
	return corpus
}

func trigramCoverage(value string, corpus map[string]struct{}) float64 {
	grams := eventTrigrams(value)
	if len(grams) < 4 {
		return 1
	}
	covered := 0
	for gram := range grams {
		if _, ok := corpus[gram]; ok {
			covered++
		}
	}
	return float64(covered) / float64(len(grams))
}

// eventClauses splits an approved event into independently dramatizable beats.
func eventClauses(event string) []string {
	parts := strings.FieldsFunc(event, func(r rune) bool {
		return r == '，' || r == '。' || r == '；' || r == ';' || r == '！' || r == '？' || r == '!' || r == '?' || r == '\n'
	})
	clauses := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); len([]rune(part)) >= 5 {
			clauses = append(clauses, part)
		}
	}
	if len(clauses) == 0 {
		clauses = append(clauses, strings.TrimSpace(event))
	}
	return clauses
}

type mustKeepAssessment struct {
	Event    string
	Coverage float64 // share of event wording realized across the scene corpus
	Clauses  float64 // share of clauses that are not absent
}

// realized means the script actually tells this beat: at least a third of its clauses
// are present and the event as a whole keeps real source wording. Compound events
// routinely lose one clause to paraphrase without losing the beat.
func (a mustKeepAssessment) realized() bool {
	return a.Coverage >= mustKeepEventFloor && a.Clauses >= mustKeepClauseRatio
}

func (a mustKeepAssessment) description() string {
	return fmt.Sprintf("%s（原文措辞覆盖率%0.f%%）", a.Event, a.Coverage*100)
}

func joinMustKeepDescriptions(items []mustKeepAssessment) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, item.description())
	}
	return strings.Join(parts, "；")
}

// sceneCoverageTexts collects every textual surface a scene can dramatize an event
// in: title, story content and structured dialogue. Checking content alone marked
// scenes as missing when the beat was carried by its quoted line.
func sceneCoverageTexts(doc *scriptResult) []string {
	texts := make([]string, 0, len(doc.Scenes)*3)
	for _, scene := range doc.Scenes {
		texts = append(texts, scene.Title, scene.Content)
		for _, dialogue := range scene.Dialogues {
			texts = append(texts, dialogue.Text)
		}
	}
	return texts
}

func assessMustKeepEvents(scenes []string, events []string) []mustKeepAssessment {
	if len(events) == 0 {
		return nil
	}
	corpus := eventCorpus(scenes)
	out := make([]mustKeepAssessment, 0, len(events))
	for _, event := range events {
		assessment := mustKeepAssessment{Event: event, Coverage: trigramCoverage(event, corpus)}
		clauses := eventClauses(event)
		present := 0
		for _, clause := range clauses {
			if trigramCoverage(clause, corpus) >= mustKeepClausePresent {
				present++
			}
		}
		assessment.Clauses = float64(present) / float64(len(clauses))
		out = append(out, assessment)
	}
	return out
}

// missingMustKeepEvents returns approved events the candidate script does not
// dramatize. Parse failures are left to the normal structure validation.
func missingMustKeepEvents(raw string, events []string) []mustKeepAssessment {
	if len(events) == 0 {
		return nil
	}
	doc, err := parseAdaptationScript(raw)
	if err != nil || doc == nil {
		return nil
	}
	missing := []mustKeepAssessment{}
	for _, assessment := range assessMustKeepEvents(sceneCoverageTexts(doc), events) {
		if !assessment.realized() {
			missing = append(missing, assessment)
		}
	}
	return missing
}

// repairMissingMustKeepEvents performs exactly one source-grounded repair. It never
// invents replacement plot: the model must use the approved chapters and events.
func (s *AdaptationService) repairMissingMustKeepEvents(projectID uint, episode int, raw string, ctx *EpisodeContext, missing []string) (string, error) {
	contextJSON, err := json.Marshal(struct {
		SourceChapters    []SourceChapter  `json:"source_chapters"`
		DialogueLedger    []SourceDialogue `json:"source_dialogue_ledger"`
		MustKeepEvents    []string         `json:"must_keep_events"`
		OmittedEvents     []string         `json:"omitted_events"`
		MissingEvents     []string         `json:"missing_events"`
		ApprovedEnding    string           `json:"approved_ending_state"`
		CurrentScriptJSON json.RawMessage  `json:"current_structured_script"`
		EpisodeBoundary   *EpisodeContext  `json:"episode_boundary"`
	}{ctx.SourceChapters, ctx.DialogueLedger, ctx.MustKeepEvents, ctx.OmittedEvents, missing, ctx.EndingState, json.RawMessage(raw), ctx})
	if err != nil {
		return "", err
	}
	system := "这是一次且仅一次的忠实度修复。输出完整合法JSON剧本。必须把missing_events逐项改写为可拍摄场景，并按source_chapters还原其人物、动作、法器、术法与结果。禁止新增原著没有的战斗、技能、法器、人物或台词；原著中沉默的角色必须保持沉默；禁止用其他冲突替换缺失事件；禁止出现omitted_events。保留现有视觉字段和已逐字覆盖的结构化对白。"
	repaired, err := s.skills.ChatWithSkill(projectID, models.SkillStageEpisodeAdaptation, s.provider, system+"\n"+episodeBoundaryInstruction, "", map[string]string{
		"episode_n": strconv.Itoa(episode), "target_duration": fmt.Sprint(ctx.TargetDuration), "target_scenes": strconv.Itoa(ctx.TargetScenes), "episode_context": string(contextJSON),
	})
	if err != nil {
		return "", err
	}
	preserved, ok := revisionRootFieldsPreserved(raw, repaired)
	if !ok {
		return "", fmt.Errorf("忠实度修复返回的结构不完整")
	}
	if _, err := parseAdaptationScript(preserved); err != nil {
		return "", fmt.Errorf("忠实度修复返回非法JSON: %w", err)
	}
	return preserved, nil
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
	ctx := &EpisodeContext{
		EpisodeTitle:    ep.Title,
		TargetDuration:  ep.TargetDuration,
		TargetScenes:    ep.TargetScenes,
		AdaptationGoal:  ep.AdaptationGoal,
		MustKeepEvents:  adaptationEventList(ep.MustKeepEvents),
		OptionalEvents:  adaptationEventList(ep.OptionalEvents),
		OmittedEvents:   adaptationEventList(ep.OmittedEvents),
		EndingState:     ep.EndingState,
		AdaptationRules: bible.AdaptationRules,
		WorldRules:      bible.WorldRules,
		MainPlot:        bible.MainPlot,
		OpeningState:    ep.OpeningState,
		NextGoal:        ep.Hook,
	}
	// An approved mapping may encode its constraints as opaque JSON wrappers.
	// Always normalize them into readable lists so the prompt cannot silently drop
	// the events the user reviewed and approved.
	ctx.OpeningState = strings.TrimSpace(strings.Trim(ctx.OpeningState, `"`))
	ctx.EndingState = strings.TrimSpace(strings.Trim(ctx.EndingState, `"`))
	registry, err := loadProductionIdentities(s.db, projectID)
	if err != nil {
		return nil, err
	}
	ctx.ProjectAssetIdentities = registry.promptCatalogue()
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
	counts, err := s.completedSourceDialogueCounts(projectID, episode, ctx.SourceChapters)
	if err != nil {
		return nil, err
	}
	ctx.DialogueLedger, ctx.CompletedDialogues = excludeCompletedSourceDialogues(ctx.DialogueLedger, counts)
	ctx.AvailableDialogues = append([]SourceDialogue(nil), ctx.DialogueLedger...)
	shared, reserved, err := s.sharedChapterReservations(projectID, episode, ctx.SourceChapters)
	if err != nil {
		return nil, err
	}
	ctx.ReservedFutureEvents = reserved
	if shared {
		ctx.DialogueLedger = requiredSharedChapterDialogues(ctx.AvailableDialogues, ctx.MustKeepEvents)
	}
	var prev models.EpisodeAdaptation
	if s.db.Where("project_id = ? AND episode_n = ?", projectID, episode-1).First(&prev).Error == nil {
		ctx.PreviousEnding = prev.EndingState
	}
	if s.projects != nil {
		ctx.PreviousContent, _ = s.projects.previousEpisodeProductionFacts(projectID, episode)
	}
	budget := maxChars - len(ctx.AdaptationRules) - len(ctx.WorldRules) - len(ctx.MainPlot) - len(ctx.OpeningState) - len(ctx.PreviousEnding) - len(ctx.PreviousContent) - len(ctx.NextGoal) -
		len(ctx.AdaptationGoal) - len(ctx.EndingState) - len(strings.Join(ctx.MustKeepEvents, "")) - len(strings.Join(ctx.OptionalEvents, "")) - len(strings.Join(ctx.OmittedEvents, ""))
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
	Total                   int              `json:"total"`
	Covered                 int              `json:"covered"`
	Missing                 []SourceDialogue `json:"missing"`
	Unbound                 int              `json:"unbound,omitempty"`
	OutOfOrder              int              `json:"out_of_order,omitempty"`
	OrderPassed             bool             `json:"order_passed"`
	StoryPendingSceneOrders []int            `json:"story_pending_scene_orders,omitempty"`
	// MustKeepWarnings lists approved events that the draft only partially realized.
	// They are advisory: the episode is still faithful to the approved source scope.
	MustKeepWarnings []string `json:"must_keep_warnings,omitempty"`
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

func storyPendingSceneOrders(raw string) []int {
	res, err := parseAdaptationScript(raw)
	if err != nil {
		return nil
	}
	var pending []int
	for i, scene := range res.Scenes {
		content := scene.Content
		missing := strings.Contains(content, "【对白待确认】") || strings.Contains(content, "【对白｜") || strings.Contains(content, "【画外对白｜")
		for _, dialogue := range scene.Dialogues {
			if strings.TrimSpace(dialogue.Text) != "" && !strings.Contains(content, dialogue.Text) {
				missing = true
			}
		}
		if missing {
			pending = append(pending, i+1)
		}
	}
	return pending
}

func revisionRootFieldsPreserved(original, candidate string) (string, bool) {
	var before, after map[string]json.RawMessage
	if parseJSONObject(original, &before) != nil || parseJSONObject(candidate, &after) != nil {
		return candidate, false
	}
	for key, value := range before {
		if strings.HasPrefix(key, "source_") {
			got, exists := after[key]
			if !exists || string(got) != string(value) {
				return candidate, false
			}
		}
	}
	for _, key := range []string{"opening_state", "ending_state"} {
		if value, exists := before[key]; exists {
			if got, present := after[key]; !present || string(got) == "null" || strings.Trim(string(got), "\"") == "" {
				after[key] = value
			}
		}
	}
	encoded, err := json.Marshal(after)
	return string(encoded), err == nil
}

func improvedRevision(original, candidate string, ledger []SourceDialogue) bool {
	if _, err := parseScriptJSON(candidate); err != nil || !structuredDialoguesPreserved(original, candidate) {
		return false
	}
	before, after := dialogueCoverage(original, ledger), dialogueCoverage(candidate, ledger)
	bodyImproved := len(storyPendingSceneOrders(candidate)) < len(storyPendingSceneOrders(original))
	return after.Covered >= before.Covered && (after.Covered > before.Covered || bodyImproved)
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
	consumed, err := s.episodeSourceScopeConsumed(projectID, episode, ctx)
	if err != nil {
		return nil, err
	}
	if consumed {
		return nil, fmt.Errorf("第%d集映射与前集实际剧情冲突：来源对白已在前集全部使用，当前必保事件又要求重复使用其中的原文台词；不会再次生成相同章节。须先审核调整剩余分集事件范围，现有剧本和媒体未修改", episode)
	}
	input, _ := json.Marshal(ctx)
	system := "只改编提供的上下文并输出JSON。source_chapters是本集审核映射的原文权威，source_dialogue_ledger是按原文顺序提取的对白清单：必须逐字、逐条、按序保留到scenes[].dialogues，并依据原文把是谁说、画外空间位置和发生时间自然融入同场content；content必须自然包含引号原句且归属时序与dialogues完全一致，禁止台词清单、末尾追加标签和【对白待确认】。不得因180秒或25镜头目标删减或硬凑镜头。未知说话者只保留原文明示的描述性身份与offscreen类型，不按别名猜人；人名后置归属必须保留，多角色歧义保持未明多人物语音。不得发明原文未提供的咒语或台词：原文中沉默的角色必须保持沉默，原文没有的旁白、内心独白和对白一律不得新增。must_keep_events是已审核的必保留事件，必须逐项落地为可见场景；omitted_events禁止出现；approved_ending_state必须在本集结尾达到。previous_episode_completed_content是上一集已完成内容的排重权威；本集必须从其结尾后的第一个原文瞬间开始，严禁重演、改写后重复、重新介绍或回顾其中已经完成的事件、动作、信息揭示、对白与情感节点，尤其禁止重新描述上一集已经拍过的巨石爆裂、人物现身或对峙。"
	raw, err := s.skills.ChatWithSkill(projectID, models.SkillStageEpisodeAdaptation, s.provider, system+"\n"+episodeBoundaryInstruction, "", map[string]string{"episode_n": strconv.Itoa(episode), "target_duration": fmt.Sprint(ep.TargetDuration), "target_scenes": strconv.Itoa(ep.TargetScenes), "episode_context": string(input)})
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
	pendingOrders := storyPendingSceneOrders(raw)
	coverage.StoryPendingSceneOrders = pendingOrders
	if len(coverage.Missing) > 0 || len(pendingOrders) > 0 {
		current, _ := parseAdaptationScript(raw)
		type integrationScene struct {
			SceneOrder int              `json:"scene_order"`
			Content    string           `json:"content"`
			Dialogues  []scriptDialogue `json:"dialogues"`
		}
		var requiring []integrationScene
		if current != nil {
			for _, order := range pendingOrders {
				if order > 0 && order <= len(current.Scenes) {
					scene := current.Scenes[order-1]
					requiring = append(requiring, integrationScene{order, scene.Content, scene.Dialogues})
				}
			}
		}
		revisionContext, _ := json.Marshal(struct {
			SourceChapters                  []SourceChapter    `json:"source_chapters"`
			CurrentStructuredScript         json.RawMessage    `json:"current_structured_script"`
			MissingDialogues                []SourceDialogue   `json:"missing_dialogues"`
			ScenesRequiringStoryIntegration []integrationScene `json:"scenes_requiring_story_integration"`
			EpisodeBoundary                 *EpisodeContext    `json:"episode_boundary"`
		}{ctx.SourceChapters, json.RawMessage(raw), coverage.Missing, requiring, ctx})
		revisionSystem := "这是一次且仅一次的原文对白覆盖与正文整合修订。输出完整JSON剧本；保持现有视觉字段、源字段、结构化对白和已完成剧情不变。把missing_dialogues逐字放回正确场景，同时依据source_chapters把scenes_requiring_story_integration中的对白原句、明确归属、画外位置和时序自然融入content。正文不得成为台词清单，不得末尾追加标签或【对白待确认】，不得猜测未明说话者。"
		revised, revisionErr := s.skills.ChatWithSkill(projectID, models.SkillStageEpisodeAdaptation, s.provider, revisionSystem+"\n"+episodeBoundaryInstruction, "", map[string]string{
			"episode_n": strconv.Itoa(episode), "target_duration": fmt.Sprint(ep.TargetDuration), "target_scenes": strconv.Itoa(ep.TargetScenes), "episode_context": string(revisionContext),
		})
		if revisionErr == nil {
			if preserved, ok := revisionRootFieldsPreserved(raw, revised); ok && improvedRevision(raw, preserved, ctx.DialogueLedger) {
				raw = preserved
				raw, _ = restoreDialoguesAlreadyLocatedByContent(raw, ctx.DialogueLedger)
			}
		}
		coverage = dialogueCoverage(raw, ctx.DialogueLedger)
		coverage.StoryPendingSceneOrders = storyPendingSceneOrders(raw)
	}
	// Fidelity gate. A draft that misses most approved events means the model told a
	// different story and must never overwrite the episode. A draft that realizes the
	// story but words one or two beats differently is accepted and recorded as a
	// warning, so ordinary paraphrase cannot deadlock production.
	if missing := missingMustKeepEvents(raw, ctx.MustKeepEvents); len(missing) > 0 {
		events := make([]string, 0, len(missing))
		for _, item := range missing {
			events = append(events, item.Event)
		}
		repaired, repairErr := s.repairMissingMustKeepEvents(projectID, episode, raw, ctx, events)
		if repairErr == nil {
			if remaining := missingMustKeepEvents(repaired, ctx.MustKeepEvents); len(remaining) <= len(missing) {
				raw, missing = repaired, remaining
			}
		}
		if len(missing) > 0 {
			if ratio := float64(len(missing)) / float64(len(ctx.MustKeepEvents)); ratio > mustKeepBlockRatio {
				return nil, fmt.Errorf("本集剧本偏离已审核原著：%d/%d 项必须保留事件未落地（%s）；未覆盖现有剧本",
					len(missing), len(ctx.MustKeepEvents), joinMustKeepDescriptions(missing))
			}
			for _, item := range missing {
				coverage.MustKeepWarnings = append(coverage.MustKeepWarnings, item.description())
			}
			log.Printf("[adaptation] project=%d episode=%d must_keep warnings=%d/%d", projectID, episode, len(missing), len(ctx.MustKeepEvents))
		}
	}
	// Last gate, after ALL model revisions. A dialogue-integration pass must not
	// undo an earlier dedup pass and persist a replay of the previous episode.
	finalScript, err := parseAdaptationScript(raw)
	if err != nil {
		return nil, err
	}
	issues, err := s.projects.crossEpisodeDuplicateIssues(projectID, episode, finalScript)
	if err != nil {
		return nil, err
	}
	replay, err := replayedCompletedSourceDialogues(raw, ctx.CompletedDialogues)
	if err != nil {
		return nil, err
	}
	issues = append(issues, replay...)
	if len(issues) > 0 {
		return nil, fmt.Errorf("第%d集最终稿重演已完成剧情，未保存：%s", episode, strings.Join(issues, "；"))
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
		// Scene.Content is the readable story and the director's source, not
		// merely a visual synopsis. Keep the exact speech here as well as in
		// the authoritative dialogue rows written below.
		rendered.Scenes[i].Content = sceneStoryContentWithDialogues(structured.Scenes[i].Content, structured.Scenes[i].Dialogues)
		rendered.Scenes[i].Dialogues = nil
	}
	if strings.TrimSpace(rendered.Script) == "" {
		var story strings.Builder
		for i, scene := range rendered.Scenes {
			fmt.Fprintf(&story, "【场景%d｜%s】\n%s\n\n", i+1, scene.Title, scene.Content)
		}
		rendered.Script = story.String()
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
	if strings.TrimSpace(parsed.Script) == "" {
		parsed.Script = rendered.Script
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
	coverage.StoryPendingSceneOrders = storyPendingSceneOrders(raw)
	traces, _ := json.Marshal(ctx.Traces)
	coverageJSON, _ := json.Marshal(coverage)
	// Generated states are evidence in the review report, never replacements for
	// approved planning boundaries used by subsequent episodes.
	generatedStates, err := json.Marshal(map[string]any{"generated_opening_state": parsed.OpeningState, "generated_ending_state": parsed.EndingState})
	if err != nil {
		return nil, err
	}
	ep.ReviewJSON = string(generatedStates)
	// Legacy mappings with no boundary may acquire their first generated state;
	// an existing reviewed boundary is never overwritten on regeneration.
	fillMissingState := func(current string, generated any) string {
		if strings.TrimSpace(current) != "" && strings.TrimSpace(current) != "null" {
			return current
		}
		if generated == nil {
			return current
		}
		encoded, err := json.Marshal(generated)
		if err != nil {
			return current
		}
		return string(encoded)
	}
	ep.OpeningState = fillMissingState(ep.OpeningState, parsed.OpeningState)
	ep.EndingState = fillMissingState(ep.EndingState, parsed.EndingState)
	ep.Script, ep.SourceTracesJSON, ep.DialogueCoverageJSON, ep.Status, ep.ContinuityStatus = parsed.Script, string(traces), string(coverageJSON), "scripted", "pending"
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
