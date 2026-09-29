package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"comfyui-console/internal/models"
)

const (
	novelJobChapterAnalysis = "chapter_analysis"
	novelJobArcMerge        = "arc_merge"
	novelJobStoryBible      = "story_bible"
)

type NovelAnalysisService struct {
	db       *gorm.DB
	provider TextProvider
	skills   *SkillService
}

func NewNovelAnalysisService(db *gorm.DB, provider TextProvider, skills *SkillService) *NovelAnalysisService {
	return &NovelAnalysisService{db: db, provider: provider, skills: skills}
}

func (s *NovelAnalysisService) RecoverInterruptedJobs() error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.NovelJob{}).Where("status = ?", "running").Updates(map[string]any{"status": "pending", "error": "interrupted; retry to resume"}).Error; err != nil {
			return err
		}
		return tx.Model(&models.Chapter{}).Where("analysis_status = ?", "running").Update("analysis_status", "pending").Error
	})
}

func novelHash(text string) string {
	h := sha256.Sum256([]byte(text))
	return hex.EncodeToString(h[:])
}

func jobToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func parseJSONObject(raw string, target any) error {
	start, end := strings.Index(raw, "{"), strings.LastIndex(raw, "}")
	if start < 0 || end < start {
		return fmt.Errorf("model output does not contain a JSON object")
	}
	if err := json.Unmarshal([]byte(raw[start:end+1]), target); err != nil {
		return fmt.Errorf("invalid model JSON: %w", err)
	}
	return nil
}

func (s *NovelAnalysisService) newJob(projectID uint, typ string, start, end, total int) (*models.NovelJob, error) {
	var active int64
	if err := s.db.Model(&models.NovelJob{}).Where("project_id = ? AND type = ? AND status IN ?", projectID, typ, []string{"pending", "running"}).Count(&active).Error; err != nil {
		return nil, err
	}
	if active > 0 {
		return nil, fmt.Errorf("a %s job is already active for this project", typ)
	}
	skill, err := s.skills.GetEffectiveSkill(projectID, typ)
	if err != nil {
		return nil, err
	}
	job := &models.NovelJob{ProjectID: projectID, Type: typ, ScopeStart: start, ScopeEnd: end, Status: "running", Total: total, Token: jobToken()}
	if skill != nil {
		job.SkillID, job.SkillVersion = &skill.ID, skill.Version
	}
	return job, s.db.Create(job).Error
}

func (s *NovelAnalysisService) finishJob(job *models.NovelJob, failed int) error {
	now := time.Now()
	job.Progress = 1
	job.FinishedAt = &now
	if failed > 0 {
		job.Status = "failed"
		job.Error = fmt.Sprintf("%d item(s) failed; retry resumes failed/stale items", failed)
	} else {
		job.Status = "success"
	}
	return s.db.Save(job).Error
}

func (s *NovelAnalysisService) AnalyzeChapters(projectID uint, chapterIDs []uint) (*models.NovelJob, error) {
	q := s.db.Where("project_id = ?", projectID)
	if len(chapterIDs) > 0 {
		q = q.Where("id IN ?", chapterIDs)
	} else {
		q = q.Where("analysis_status IN ? OR analysis_hash <> content_hash", []string{"", "pending", "failed", "stale"})
	}
	var chapters []models.Chapter
	if err := q.Order("chapter_order ASC").Find(&chapters).Error; err != nil {
		return nil, err
	}
	if len(chapters) == 0 {
		return nil, fmt.Errorf("no chapters require analysis")
	}
	job, err := s.newJob(projectID, novelJobChapterAnalysis, chapters[0].Order, chapters[len(chapters)-1].Order, len(chapters))
	if err != nil {
		return nil, err
	}
	failed := 0
	for i := range chapters {
		var current models.NovelJob
		if err := s.db.Select("status").First(&current, job.ID).Error; err != nil {
			return job, err
		}
		if current.Status == "cancelled" {
			return job, fmt.Errorf("analysis cancelled")
		}
		if err := s.analyzeChapter(job, &chapters[i]); err != nil {
			failed++
		}
		job.Completed++
		job.Progress = float64(job.Completed) / float64(job.Total)
		_ = s.db.Model(job).Updates(map[string]any{"completed": job.Completed, "progress": job.Progress}).Error
	}
	_ = s.finishJob(job, failed)
	return job, nil
}

func (s *NovelAnalysisService) analyzeChapter(job *models.NovelJob, chapter *models.Chapter) error {
	contentHash := novelHash(chapter.Content)
	version := chapter.AnalysisVersion + 1
	if err := s.db.Model(chapter).Updates(map[string]any{"content_hash": contentHash, "analysis_status": "running", "analysis_error": ""}).Error; err != nil {
		return err
	}
	var previous models.Chapter
	_ = s.db.Where("project_id = ? AND chapter_order < ? AND analysis_status = ?", chapter.ProjectID, chapter.Order, "ready").Order("chapter_order DESC").First(&previous).Error
	aliases, _ := s.confirmedAliases(chapter.ProjectID)
	output, err := s.skills.ChatWithSkill(chapter.ProjectID, models.SkillStageChapterAnalysis, s.provider,
		"Analyze one chapter. Preserve source traceability and output JSON only.", "",
		map[string]string{"chapter_no": strconv.Itoa(chapter.Order), "chapter_title": chapter.Title, "previous_summary": previous.Summary, "aliases": aliases, "chapter_content": chapter.Content})
	if err != nil {
		s.failChapter(chapter.ID, err)
		return err
	}
	var result struct {
		Summary         string `json:"summary"`
		AliasCandidates []struct {
			CanonicalName string `json:"canonical_name"`
			Alias         string `json:"alias"`
		} `json:"alias_candidates"`
	}
	if err := parseJSONObject(output, &result); err != nil || strings.TrimSpace(result.Summary) == "" {
		if err == nil {
			err = fmt.Errorf("analysis summary is required")
		}
		s.failChapter(chapter.ID, err)
		return err
	}
	// Optimistic source lock prevents an old result overwriting edited source.
	res := s.db.Model(&models.Chapter{}).Where("id = ? AND project_id = ? AND content_hash = ?", chapter.ID, chapter.ProjectID, contentHash).Updates(map[string]any{
		"summary": result.Summary, "analysis_json": outputJSON(output), "analysis_status": "ready", "analysis_version": version, "analysis_hash": contentHash, "analysis_error": "",
	})
	if res.Error != nil || res.RowsAffected != 1 {
		return fmt.Errorf("chapter changed while analysis was running")
	}
	for _, candidate := range result.AliasCandidates {
		canonical, alias := strings.TrimSpace(candidate.CanonicalName), strings.TrimSpace(candidate.Alias)
		if canonical == "" || alias == "" || canonical == alias {
			continue
		}
		row := models.CharacterAliasCandidate{ProjectID: chapter.ProjectID, CanonicalName: canonical, Alias: alias, ChapterID: chapter.ID, Status: "pending"}
		_ = s.db.Where("project_id = ? AND alias = ?", chapter.ProjectID, alias).FirstOrCreate(&row).Error
	}
	return s.markDownstreamStale(chapter.ProjectID, chapter.Order)
}

func outputJSON(raw string) string {
	start, end := strings.Index(raw, "{"), strings.LastIndex(raw, "}")
	if start >= 0 && end >= start {
		return raw[start : end+1]
	}
	return raw
}

func (s *NovelAnalysisService) failChapter(id uint, err error) {
	_ = s.db.Model(&models.Chapter{}).Where("id = ?", id).Updates(map[string]any{"analysis_status": "failed", "analysis_error": err.Error()}).Error
}

func (s *NovelAnalysisService) confirmedAliases(projectID uint) (string, error) {
	var rows []models.CharacterAliasCandidate
	if err := s.db.Where("project_id = ? AND status = ?", projectID, "confirmed").Find(&rows).Error; err != nil {
		return "", err
	}
	b, err := json.Marshal(rows)
	return string(b), err
}

func (s *NovelAnalysisService) ListAliases(projectID uint) ([]models.CharacterAliasCandidate, error) {
	var rows []models.CharacterAliasCandidate
	err := s.db.Where("project_id = ?", projectID).Order("id ASC").Find(&rows).Error
	return rows, err
}

func (s *NovelAnalysisService) SetAliasStatus(projectID, id uint, status string) (*models.CharacterAliasCandidate, error) {
	if status != "confirmed" && status != "rejected" && status != "pending" {
		return nil, fmt.Errorf("invalid alias status")
	}
	var row models.CharacterAliasCandidate
	if err := s.db.Where("id = ? AND project_id = ?", id, projectID).First(&row).Error; err != nil {
		return nil, err
	}
	if err := s.db.Model(&row).Update("status", status).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *NovelAnalysisService) GenerateArcs(projectID uint, groupSize int) ([]models.StoryArc, *models.NovelJob, error) {
	return s.GenerateArcsRange(projectID, 0, 0, groupSize)
}

// GenerateArcsRange only requires the selected target range to be ready. This
// unblocks production of the first window while later chapters remain pending.
func (s *NovelAnalysisService) GenerateArcsRange(projectID uint, chapterStart, chapterEnd, groupSize int) ([]models.StoryArc, *models.NovelJob, error) {
	if groupSize == 0 {
		groupSize = 8
	}
	if groupSize < 5 || groupSize > 10 {
		return nil, nil, fmt.Errorf("group_size must be between 5 and 10")
	}
	var chapters []models.Chapter
	query := s.db.Where("project_id = ?", projectID)
	if chapterStart > 0 || chapterEnd > 0 {
		if chapterStart < 1 || chapterEnd < chapterStart {
			return nil, nil, fmt.Errorf("invalid chapter range")
		}
		query = query.Where("chapter_order BETWEEN ? AND ?", chapterStart, chapterEnd)
	}
	if err := query.Order("chapter_order ASC").Find(&chapters).Error; err != nil {
		return nil, nil, err
	}
	if len(chapters) == 0 {
		return nil, nil, fmt.Errorf("project has no chapters")
	}
	for _, chapter := range chapters {
		if chapter.AnalysisStatus != "ready" || chapter.AnalysisHash != chapter.ContentHash {
			return nil, nil, fmt.Errorf("chapters %d-%d must all be current before arc generation", chapters[0].Order, chapters[len(chapters)-1].Order)
		}
	}
	total := (len(chapters) + groupSize - 1) / groupSize
	job, err := s.newJob(projectID, novelJobArcMerge, chapters[0].Order, chapters[len(chapters)-1].Order, total)
	if err != nil {
		return nil, nil, err
	}
	var arcs []models.StoryArc
	var maxArcNo int
	if err := s.db.Model(&models.StoryArc{}).Where("project_id = ?", projectID).Select("COALESCE(MAX(arc_no), 0)").Scan(&maxArcNo).Error; err != nil {
		return nil, job, err
	}
	failed := 0
	for start := 0; start < len(chapters); start += groupSize {
		end := start + groupSize
		if end > len(chapters) {
			end = len(chapters)
		}
		part := chapters[start:end]
		analyses := make([]json.RawMessage, 0, len(part))
		versions := make(map[string]int)
		for _, ch := range part {
			analyses = append(analyses, json.RawMessage(ch.AnalysisJSON))
			versions[strconv.FormatUint(uint64(ch.ID), 10)] = ch.AnalysisVersion
		}
		input, _ := json.Marshal(analyses)
		output, callErr := s.skills.ChatWithSkill(projectID, models.SkillStageArcMerge, s.provider, "Merge only supplied analyses. Output JSON only.", "", map[string]string{"chapter_start": strconv.Itoa(part[0].Order), "chapter_end": strconv.Itoa(part[len(part)-1].Order), "chapter_analyses": string(input)})
		var parsed struct {
			Title   string `json:"title"`
			Summary string `json:"summary"`
		}
		if callErr == nil {
			callErr = parseJSONObject(output, &parsed)
		}
		if callErr != nil || parsed.Summary == "" {
			failed++
			continue
		}
		versionJSON, _ := json.Marshal(versions)
		arc := models.StoryArc{ProjectID: projectID, ArcNo: maxArcNo + len(arcs) + 1, Title: parsed.Title, ChapterStart: part[0].Order, ChapterEnd: part[len(part)-1].Order, Summary: parsed.Summary, AnalysisJSON: outputJSON(output), SourceVersions: string(versionJSON), Status: "draft", Version: 1}
		var old models.StoryArc
		if s.db.Where("project_id = ? AND chapter_start = ? AND chapter_end = ?", projectID, arc.ChapterStart, arc.ChapterEnd).First(&old).Error == nil {
			arc.ID, arc.ArcNo = old.ID, old.ArcNo
			arc.Version = old.Version + 1
		}
		if err := s.db.Save(&arc).Error; err != nil {
			failed++
			continue
		}
		arcs = append(arcs, arc)
		job.Completed++
		job.Progress = float64(job.Completed) / float64(job.Total)
		_ = s.db.Save(job).Error
	}
	_ = s.finishJob(job, failed)
	_ = s.db.Model(&models.StoryBible{}).Where("project_id = ?", projectID).Update("status", "stale").Error
	return arcs, job, nil
}

func (s *NovelAnalysisService) ListArcs(projectID uint) ([]models.StoryArc, error) {
	var arcs []models.StoryArc
	err := s.db.Where("project_id = ?", projectID).Order("arc_no ASC").Find(&arcs).Error
	return arcs, err
}

func (s *NovelAnalysisService) GenerateBible(projectID uint) (*models.StoryBible, *models.NovelJob, error) {
	var approved models.StoryBible
	if err := s.db.Where("project_id = ? AND status = ?", projectID, "approved").First(&approved).Error; err == nil {
		return nil, nil, fmt.Errorf("approved story bible cannot be overwritten; generate an incremental change from the current analysis window")
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, err
	}
	arcs, err := s.ListArcs(projectID)
	if err != nil || len(arcs) == 0 {
		return nil, nil, fmt.Errorf("story arcs are required")
	}
	for _, arc := range arcs {
		if arc.Status == "stale" || arc.Error != "" {
			return nil, nil, fmt.Errorf("all story arcs must be current")
		}
	}
	job, err := s.newJob(projectID, novelJobStoryBible, arcs[0].ChapterStart, arcs[len(arcs)-1].ChapterEnd, 1)
	if err != nil {
		return nil, nil, err
	}
	input, _ := json.Marshal(arcs)
	output, err := s.skills.ChatWithSkill(projectID, models.SkillStageStoryBible, s.provider, "Synthesize the supplied arcs. Output JSON only.", "", map[string]string{"story_arcs": string(input)})
	var parsed struct {
		Premise       string `json:"premise"`
		WorldRules    any    `json:"world_rules"`
		MainPlot      any    `json:"main_plot"`
		Subplots      any    `json:"subplots"`
		Timeline      any    `json:"timeline"`
		Relationships any    `json:"relationships"`
		ClueLedger    any    `json:"clue_ledger"`
		Locations     any    `json:"locations"`
		Props         any    `json:"props"`
	}
	if err == nil {
		err = parseJSONObject(output, &parsed)
	}
	if err != nil {
		job.Error = err.Error()
		_ = s.finishJob(job, 1)
		return nil, job, err
	}
	marshal := func(v any) string { b, _ := json.Marshal(v); return string(b) }
	versions := map[string]int{}
	for _, arc := range arcs {
		versions[strconv.FormatUint(uint64(arc.ID), 10)] = arc.Version
	}
	source, _ := json.Marshal(versions)
	bible := models.StoryBible{ProjectID: projectID, Premise: parsed.Premise, WorldRules: marshal(parsed.WorldRules), MainPlot: marshal(parsed.MainPlot), Subplots: marshal(parsed.Subplots), TimelineJSON: marshal(parsed.Timeline), RelationshipsJSON: marshal(parsed.Relationships), ClueLedgerJSON: marshal(parsed.ClueLedger), LocationBibleJSON: marshal(parsed.Locations), PropBibleJSON: marshal(parsed.Props), SourceVersions: string(source), Status: "draft", Version: 1}
	var old models.StoryBible
	if s.db.Where("project_id = ?", projectID).First(&old).Error == nil {
		bible.ID = old.ID
		bible.Version = old.Version + 1
		bible.AdaptationRules = old.AdaptationRules
	}
	if err := s.db.Save(&bible).Error; err != nil {
		return nil, job, err
	}
	job.Completed = 1
	_ = s.finishJob(job, 0)
	return &bible, job, nil
}

func bibleCandidateJSON(v models.StoryBible) string {
	payload := map[string]string{"premise": v.Premise, "world_rules": v.WorldRules, "main_plot": v.MainPlot, "subplots": v.Subplots, "timeline_json": v.TimelineJSON, "relationships_json": v.RelationshipsJSON, "clue_ledger_json": v.ClueLedgerJSON, "location_bible_json": v.LocationBibleJSON, "prop_bible_json": v.PropBibleJSON}
	b, _ := json.Marshal(payload)
	return string(b)
}

func (s *NovelAnalysisService) ProposeBibleChange(projectID, windowID uint) (*models.StoryBibleChange, error) {
	window, err := s.GetWindow(projectID, windowID)
	if err != nil {
		return nil, err
	}
	if window.Status != models.WindowStatusReady && window.Status != models.WindowStatusApproved && window.Status != models.WindowStatusCommitted {
		return nil, fmt.Errorf("window analysis must be ready")
	}
	base, err := s.GetBible(projectID)
	if err != nil || base.Status != "approved" {
		return nil, fmt.Errorf("an approved story bible is required before incremental update")
	}
	var arcs []models.StoryArc
	if err := s.db.Where("project_id = ? AND chapter_start >= ? AND chapter_end <= ? AND status <> ?", projectID, window.ChapterStart, window.ChapterEnd, "stale").Order("arc_no").Find(&arcs).Error; err != nil {
		return nil, err
	}
	if len(arcs) == 0 {
		return nil, fmt.Errorf("generate this window's story arcs first")
	}
	arcJSON, _ := json.Marshal(arcs)
	raw, err := s.skills.ChatWithSkill(projectID, models.SkillStageStoryBible, s.provider, "Update the approved story bible using only supplied new arcs. Return one complete candidate JSON. Preserve established facts unless new source explicitly changes them.", "", map[string]string{"approved_story_bible": bibleJSON(*base), "new_story_arcs": string(arcJSON), "chapter_start": strconv.Itoa(window.ChapterStart), "chapter_end": strconv.Itoa(window.ChapterEnd)})
	if err != nil {
		return nil, err
	}
	var candidate models.StoryBible
	if err := parseJSONObject(raw, &candidate); err != nil {
		return nil, err
	}
	versions := map[string]int{}
	for _, arc := range arcs {
		versions[strconv.FormatUint(uint64(arc.ID), 10)] = arc.Version
	}
	source, _ := json.Marshal(versions)
	change := &models.StoryBibleChange{ProjectID: projectID, WindowID: &windowID, BaseVersion: base.Version, CandidateJSON: bibleCandidateJSON(candidate), SourceVersions: string(source), Status: "pending"}
	if err := s.db.Create(change).Error; err != nil {
		return nil, err
	}
	return change, nil
}

func (s *NovelAnalysisService) ListBibleChanges(projectID uint) ([]models.StoryBibleChange, error) {
	var rows []models.StoryBibleChange
	return rows, s.db.Where("project_id = ?", projectID).Order("created_at DESC, id DESC").Find(&rows).Error
}

func (s *NovelAnalysisService) ReviewBibleChange(projectID, changeID uint, approve bool, note string) (*models.StoryBible, error) {
	var result models.StoryBible
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var change models.StoryBibleChange
		if err := tx.Where("id = ? AND project_id = ? AND status = ?", changeID, projectID, "pending").First(&change).Error; err != nil {
			return err
		}
		var base models.StoryBible
		if err := tx.Where("project_id = ?", projectID).First(&base).Error; err != nil {
			return err
		}
		if !approve {
			if err := tx.Model(&change).Updates(map[string]any{"status": "rejected", "review_note": note}).Error; err != nil {
				return err
			}
			result = base
			return nil
		}
		if base.Version != change.BaseVersion {
			return fmt.Errorf("story bible changed after this proposal was generated; regenerate it")
		}
		var values map[string]string
		if err := json.Unmarshal([]byte(change.CandidateJSON), &values); err != nil {
			return err
		}
		updates := map[string]any{"status": "approved", "version": base.Version + 1, "source_versions": change.SourceVersions}
		for _, key := range []string{"premise", "world_rules", "main_plot", "subplots", "timeline_json", "relationships_json", "clue_ledger_json", "location_bible_json", "prop_bible_json"} {
			updates[key] = values[key]
		}
		if err := tx.Model(&base).Updates(updates).Error; err != nil {
			return err
		}
		if err := tx.Model(&change).Updates(map[string]any{"status": "approved", "review_note": note}).Error; err != nil {
			return err
		}
		return tx.First(&result, base.ID).Error
	})
	return &result, err
}

func (s *NovelAnalysisService) GetBible(projectID uint) (*models.StoryBible, error) {
	var bible models.StoryBible
	if err := s.db.Where("project_id = ?", projectID).First(&bible).Error; err != nil {
		return nil, err
	}
	return &bible, nil
}

func (s *NovelAnalysisService) UpdateBible(projectID uint, updates map[string]any) (*models.StoryBible, error) {
	bible, err := s.GetBible(projectID)
	if err != nil {
		return nil, err
	}
	allowed := map[string]bool{"premise": true, "world_rules": true, "main_plot": true, "subplots": true, "timeline_json": true, "relationships_json": true, "clue_ledger_json": true, "location_bible_json": true, "prop_bible_json": true, "adaptation_rules": true}
	clean := map[string]any{}
	for k, v := range updates {
		if !allowed[k] {
			return nil, fmt.Errorf("field %s cannot be updated", k)
		}
		if _, ok := v.(string); !ok {
			return nil, fmt.Errorf("field %s must be a string", k)
		}
		clean[k] = v
	}
	clean["status"] = "draft"
	clean["version"] = bible.Version + 1
	if err := s.db.Model(bible).Updates(clean).Error; err != nil {
		return nil, err
	}
	return s.GetBible(projectID)
}

func (s *NovelAnalysisService) ApproveBible(projectID uint) (*models.StoryBible, error) {
	bible, err := s.GetBible(projectID)
	if err != nil {
		return nil, err
	}
	if bible.Status != "draft" {
		return nil, fmt.Errorf("only a current draft can be approved")
	}
	if err := s.db.Model(bible).Update("status", "approved").Error; err != nil {
		return nil, err
	}
	return s.GetBible(projectID)
}

func (s *NovelAnalysisService) ListJobs(projectID uint) ([]models.NovelJob, error) {
	var jobs []models.NovelJob
	err := s.db.Where("project_id = ?", projectID).Order("created_at DESC").Find(&jobs).Error
	return jobs, err
}

func (s *NovelAnalysisService) RetryJob(projectID, jobID uint) (*models.NovelJob, error) {
	var old models.NovelJob
	if err := s.db.Where("id = ? AND project_id = ?", jobID, projectID).First(&old).Error; err != nil {
		return nil, err
	}
	if old.Status != "failed" && old.Status != "pending" {
		return nil, fmt.Errorf("job is not retryable")
	}
	switch old.Type {
	case novelJobChapterAnalysis:
		var chapters []models.Chapter
		if err := s.db.Where("project_id = ? AND chapter_order BETWEEN ? AND ? AND (analysis_status IN ? OR analysis_hash <> content_hash)", projectID, old.ScopeStart, old.ScopeEnd, []string{"", "pending", "failed", "stale"}).Order("chapter_order").Find(&chapters).Error; err != nil {
			return nil, err
		}
		ids := make([]uint, len(chapters))
		for i := range chapters {
			ids[i] = chapters[i].ID
		}
		if len(ids) == 0 {
			return nil, fmt.Errorf("no failed or stale chapters remain in the original job range")
		}
		return s.AnalyzeChapters(projectID, ids)
	case novelJobArcMerge:
		_, job, err := s.GenerateArcsRange(projectID, old.ScopeStart, old.ScopeEnd, 8)
		return job, err
	case novelJobStoryBible:
		_, job, err := s.GenerateBible(projectID)
		return job, err
	default:
		return nil, fmt.Errorf("unsupported job type")
	}
}

func (s *NovelAnalysisService) CancelJob(projectID, jobID uint) error {
	res := s.db.Model(&models.NovelJob{}).Where("id = ? AND project_id = ? AND status IN ?", jobID, projectID, []string{"pending", "running"}).Updates(map[string]any{"status": "cancelled", "finished_at": time.Now()})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("job not cancellable")
	}
	return nil
}

func (s *NovelAnalysisService) markDownstreamStale(projectID uint, chapterOrder int) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.StoryArc{}).Where("project_id = ? AND chapter_start <= ? AND chapter_end >= ?", projectID, chapterOrder, chapterOrder).Update("status", "stale").Error; err != nil {
			return err
		}
		if err := tx.Model(&models.EpisodeAdaptation{}).Where("project_id = ? AND chapter_start <= ? AND chapter_end >= ?", projectID, chapterOrder, chapterOrder).Update("status", "stale").Error; err != nil {
			return err
		}
		// Keep the approved bible authoritative. Any pending proposal based on the
		// changed source is invalid and must be regenerated.
		return tx.Model(&models.StoryBibleChange{}).Where("project_id = ? AND status = ?", projectID, "pending").Updates(map[string]any{"status": "stale", "review_note": "source chapter changed"}).Error
	})
}

// -------------------- Analysis Window Management --------------------

// WindowOptions defines parameters for creating an analysis window
type WindowOptions struct {
	ChapterStart   int   // 目标分析章节起点
	ChapterEnd     int   // 目标分析章节终点
	ContextStart   int   // 预读上下文起点（可选，默认 = ChapterStart）
	ContextEnd     int   // 预读上下文终点（可选，默认 = ChapterEnd）
	WindowSize     int   // 窗口大小（可选，默认 10）
	PreviousWindow *uint // 前一窗口 ID（可选）
}

// CreateWindow creates a new analysis window for a project.
// Default window size is 10 chapters if not specified.
func (s *NovelAnalysisService) CreateWindow(projectID uint, opts WindowOptions) (*models.AnalysisWindow, error) {
	if opts.ChapterStart <= 0 || opts.ChapterEnd < opts.ChapterStart {
		return nil, fmt.Errorf("invalid chapter range: start=%d, end=%d", opts.ChapterStart, opts.ChapterEnd)
	}
	if opts.WindowSize <= 0 {
		opts.WindowSize = 10
	}
	if opts.WindowSize > 50 || opts.ChapterEnd-opts.ChapterStart+1 > 50 {
		return nil, fmt.Errorf("analysis window cannot exceed 50 chapters")
	}
	var maxChapter int
	if err := s.db.Model(&models.Chapter{}).Where("project_id = ?", projectID).Select("COALESCE(MAX(chapter_order), 0)").Scan(&maxChapter).Error; err != nil {
		return nil, err
	}
	if maxChapter == 0 || opts.ChapterEnd > maxChapter {
		return nil, fmt.Errorf("chapter range exceeds imported chapters (max %d)", maxChapter)
	}
	// Context is pre-read only. It helps detect an arc crossing the window boundary,
	// but those chapters are not analyzed or committed by this window.
	if opts.ContextStart == 0 {
		opts.ContextStart = opts.ChapterStart
	}
	if opts.ContextEnd == 0 {
		opts.ContextEnd = min(maxChapter, opts.ChapterEnd+5)
	}
	if opts.ContextStart > opts.ChapterStart || opts.ContextEnd < opts.ChapterEnd || opts.ContextEnd > maxChapter {
		return nil, fmt.Errorf("context range must contain the target range and stay within imported chapters")
	}

	// Get next window number
	var maxWindowNo int
	s.db.Model(&models.AnalysisWindow{}).Where("project_id = ?", projectID).Select("COALESCE(MAX(window_no), 0)").Scan(&maxWindowNo)

	// Check for overlapping windows
	var overlap int64
	if err := s.db.Model(&models.AnalysisWindow{}).Where(
		"project_id = ? AND status <> ? AND NOT (chapter_end < ? OR chapter_start > ?)",
		projectID, models.WindowStatusStale, opts.ChapterStart, opts.ChapterEnd,
	).Count(&overlap).Error; err != nil {
		return nil, err
	}
	if overlap > 0 {
		return nil, fmt.Errorf("overlapping window exists for chapter range %d-%d", opts.ChapterStart, opts.ChapterEnd)
	}

	window := &models.AnalysisWindow{
		ProjectID:        projectID,
		WindowNo:         maxWindowNo + 1,
		ChapterStart:     opts.ChapterStart,
		ChapterEnd:       opts.ChapterEnd,
		ContextStart:     opts.ContextStart,
		ContextEnd:       opts.ContextEnd,
		WindowSize:       opts.WindowSize,
		Status:           models.WindowStatusPending,
		ReviewStatus:     "pending",
		PreviousWindowID: opts.PreviousWindow,
		Version:          1,
	}
	if err := s.db.Create(window).Error; err != nil {
		return nil, err
	}
	return window, nil
}

// ListWindows returns all analysis windows for a project
func (s *NovelAnalysisService) ListWindows(projectID uint) ([]models.AnalysisWindow, error) {
	var windows []models.AnalysisWindow
	err := s.db.Where("project_id = ?", projectID).Order("window_no ASC").Find(&windows).Error
	return windows, err
}

// GetWindow returns a specific analysis window
func (s *NovelAnalysisService) GetWindow(projectID, windowID uint) (*models.AnalysisWindow, error) {
	var window models.AnalysisWindow
	err := s.db.Where("id = ? AND project_id = ?", windowID, projectID).First(&window).Error
	if err != nil {
		return nil, err
	}
	return &window, nil
}

// GetCurrentWindow returns the latest non-committed window for a project
func (s *NovelAnalysisService) GetCurrentWindow(projectID uint) (*models.AnalysisWindow, error) {
	var window models.AnalysisWindow
	err := s.db.Where("project_id = ? AND status NOT IN ?", projectID, []string{"committed", "stale"}).
		Order("window_no DESC").First(&window).Error
	if err != nil {
		return nil, err
	}
	return &window, nil
}

// GetWindowProgress returns the analysis progress for a window
func (s *NovelAnalysisService) GetWindowProgress(projectID, windowID uint) (*WindowProgress, error) {
	window, err := s.GetWindow(projectID, windowID)
	if err != nil {
		return nil, err
	}

	var chapters []models.Chapter
	err = s.db.Where("project_id = ? AND chapter_order BETWEEN ? AND ?", projectID, window.ChapterStart, window.ChapterEnd).
		Order("chapter_order ASC").Find(&chapters).Error
	if err != nil {
		return nil, err
	}

	progress := &WindowProgress{
		WindowID:        window.ID,
		Status:          string(window.Status),
		TotalChapters:   len(chapters),
		ReadyChapters:   0,
		PendingChapters: 0,
		FailedChapters:  0,
	}

	for _, ch := range chapters {
		progress.Chapters = append(progress.Chapters, WindowChapterProgress{ID: ch.ID, Order: ch.Order, Title: ch.Title, Status: ch.AnalysisStatus, Error: ch.AnalysisError})
		switch ch.AnalysisStatus {
		case "ready":
			progress.ReadyChapters++
		case "pending", "running":
			progress.PendingChapters++
		case "failed":
			progress.FailedChapters++
		}
	}

	if len(chapters) > 0 && window.ContextEnd > window.ChapterEnd {
		tail := strings.TrimSpace(chapters[len(chapters)-1].Content)
		continuation := regexp.MustCompile(`(?:未完待续|欲知后事|突然|却见|正当|就在这时|下一刻|门外传来|话音未落)[。！？!?…]*$`).MatchString(tail)
		if continuation {
			progress.Boundary = &WindowBoundaryAdvisory{NeedsExtension: true, SuggestedEnd: min(window.ContextEnd, window.ChapterEnd+3), Reason: "目标窗口末章以明显的未完成事件结束，建议扩展后再生成故事弧"}
		} else {
			progress.Boundary = &WindowBoundaryAdvisory{SuggestedEnd: window.ChapterEnd, Reason: "末章未检测到明显的跨窗未完成事件"}
		}
	}
	return progress, nil
}

// WindowProgress tracks chapter analysis status within a window
type WindowChapterProgress struct {
	ID     uint   `json:"id"`
	Order  int    `json:"order"`
	Title  string `json:"title"`
	Status string `json:"status"`
	Error  string `json:"error"`
}
type WindowBoundaryAdvisory struct {
	NeedsExtension bool   `json:"needs_extension"`
	SuggestedEnd   int    `json:"suggested_end"`
	Reason         string `json:"reason"`
}
type WindowProgress struct {
	WindowID        uint                    `json:"window_id"`
	Status          string                  `json:"status"`
	TotalChapters   int                     `json:"total_chapters"`
	ReadyChapters   int                     `json:"ready_chapters"`
	PendingChapters int                     `json:"pending_chapters"`
	FailedChapters  int                     `json:"failed_chapters"`
	Chapters        []WindowChapterProgress `json:"chapters"`
	Boundary        *WindowBoundaryAdvisory `json:"boundary,omitempty"`
}

// AnalyzeWindow analyses all chapters in a window
func (s *NovelAnalysisService) ExtendWindow(projectID, windowID uint, chapterEnd int) (*models.AnalysisWindow, error) {
	window, err := s.GetWindow(projectID, windowID)
	if err != nil {
		return nil, err
	}
	if window.Status == models.WindowStatusApproved || window.Status == models.WindowStatusCommitted || window.Status == models.WindowStatusStale {
		return nil, fmt.Errorf("reviewed window cannot be extended")
	}
	if chapterEnd <= window.ChapterEnd || chapterEnd > window.ContextEnd {
		return nil, fmt.Errorf("extended end must be after the target and within the pre-read range")
	}
	if err := s.db.Model(window).Updates(map[string]any{"chapter_end": chapterEnd, "window_size": chapterEnd - window.ChapterStart + 1, "status": models.WindowStatusPending, "review_status": "pending"}).Error; err != nil {
		return nil, err
	}
	return s.GetWindow(projectID, windowID)
}

func (s *NovelAnalysisService) AnalyzeWindow(projectID, windowID uint) (*models.NovelJob, error) {
	window, err := s.GetWindow(projectID, windowID)
	if err != nil {
		return nil, err
	}
	if window.Status == models.WindowStatusCommitted {
		return nil, fmt.Errorf("window is already committed")
	}

	// Collect chapter IDs in the window
	var chapters []models.Chapter
	err = s.db.Where("project_id = ? AND chapter_order BETWEEN ? AND ?", projectID, window.ChapterStart, window.ChapterEnd).
		Order("chapter_order ASC").Find(&chapters).Error
	if err != nil {
		return nil, err
	}
	if len(chapters) == 0 {
		return nil, fmt.Errorf("no chapters in window range %d-%d", window.ChapterStart, window.ChapterEnd)
	}

	chapterIDs := make([]uint, 0, len(chapters))
	for _, chapter := range chapters {
		if chapter.AnalysisStatus != "ready" || chapter.AnalysisHash != chapter.ContentHash {
			chapterIDs = append(chapterIDs, chapter.ID)
		}
	}
	if len(chapterIDs) == 0 {
		if err := s.db.Model(window).Update("status", models.WindowStatusReady).Error; err != nil {
			return nil, err
		}
		return &models.NovelJob{ProjectID: projectID, Type: novelJobChapterAnalysis, ScopeStart: window.ChapterStart, ScopeEnd: window.ChapterEnd, Status: "success", Progress: 1}, nil
	}
	if err := s.db.Model(window).Updates(map[string]any{"status": models.WindowStatusAnalysing, "review_status": "pending"}).Error; err != nil {
		return nil, err
	}
	job, err := s.AnalyzeChapters(projectID, chapterIDs)
	if err != nil {
		_ = s.db.Model(window).Update("status", models.WindowStatusPending).Error
		return nil, err
	}
	ready, readyErr := s.IsWindowReady(projectID, windowID)
	if readyErr != nil {
		return job, readyErr
	}
	status := models.WindowStatusPending
	if ready {
		status = models.WindowStatusReady
	}
	if err := s.db.Model(window).Update("status", status).Error; err != nil {
		return job, err
	}
	return job, nil
}

// ApproveWindow approves an analysis window after review
func (s *NovelAnalysisService) ApproveWindow(projectID, windowID uint) (*models.AnalysisWindow, error) {
	window, err := s.GetWindow(projectID, windowID)
	if err != nil {
		return nil, err
	}
	if window.Status != models.WindowStatusReady {
		return nil, fmt.Errorf("window must be in ready status, current: %s", window.Status)
	}

	updates := map[string]any{
		"status":        models.WindowStatusApproved,
		"review_status": "approved",
	}
	if err := s.db.Model(window).Updates(updates).Error; err != nil {
		return nil, err
	}
	return s.GetWindow(projectID, windowID)
}

// CommitWindow marks a window as committed (immutable)
func (s *NovelAnalysisService) BindWindowBatch(projectID, windowID, batchID uint) (*models.AnalysisWindow, error) {
	window, err := s.GetWindow(projectID, windowID)
	if err != nil {
		return nil, err
	}
	if window.Status == models.WindowStatusCommitted || window.Status == models.WindowStatusStale {
		return nil, fmt.Errorf("committed or stale window cannot be rebound")
	}
	var batch models.PlanningBatch
	if err := s.db.Where("id = ? AND project_id = ?", batchID, projectID).First(&batch).Error; err != nil {
		return nil, err
	}
	if batch.ChapterStart < window.ChapterStart || batch.ChapterEnd > window.ChapterEnd {
		return nil, fmt.Errorf("production batch chapter range must stay inside the analysis window")
	}
	if window.PlanningBatchID != nil && *window.PlanningBatchID != batchID {
		return nil, fmt.Errorf("analysis window is already bound to another batch")
	}
	if err := s.db.Model(window).Update("planning_batch_id", batchID).Error; err != nil {
		return nil, err
	}
	return s.GetWindow(projectID, windowID)
}

func (s *NovelAnalysisService) requireWindowProductionComplete(projectID uint, window *models.AnalysisWindow) error {
	if window.PlanningBatchID == nil {
		return nil
	}
	var batch models.PlanningBatch
	if err := s.db.Where("id = ? AND project_id = ?", *window.PlanningBatchID, projectID).First(&batch).Error; err != nil {
		return fmt.Errorf("bound production batch is unavailable")
	}
	if batch.ReviewStatus != ReviewStatusApproved && batch.Status != BatchStatusProduced {
		return fmt.Errorf("production batch must be approved before completing this analysis window")
	}
	var snapshot models.BatchStateSnapshot
	if err := s.db.Where("project_id = ? AND batch_id = ? AND status = ?", projectID, batch.ID, "approved").First(&snapshot).Error; err != nil {
		return fmt.Errorf("approve the production batch state snapshot before completing this analysis window")
	}
	return nil
}

func (s *NovelAnalysisService) CommitWindow(projectID, windowID uint) (*models.AnalysisWindow, error) {
	window, err := s.GetWindow(projectID, windowID)
	if err != nil {
		return nil, err
	}
	if window.Status != models.WindowStatusApproved {
		return nil, fmt.Errorf("window must be approved before committing, current: %s", window.Status)
	}
	if err := s.requireWindowProductionComplete(projectID, window); err != nil {
		return nil, err
	}

	now := time.Now()
	if err := s.db.Model(window).Updates(map[string]any{"status": models.WindowStatusCommitted, "committed_at": &now}).Error; err != nil {
		return nil, err
	}
	return s.GetWindow(projectID, windowID)
}

// AdvanceToNextWindow creates the next window based on the previous one
func (s *NovelAnalysisService) AdvanceToNextWindow(projectID, currentWindowID uint, nextWindowSize int) (*models.AnalysisWindow, error) {
	current, err := s.GetWindow(projectID, currentWindowID)
	if err != nil {
		return nil, err
	}
	if current.Status != models.WindowStatusCommitted {
		return nil, fmt.Errorf("current window must be committed first, current: %s", current.Status)
	}
	if err := s.requireWindowProductionComplete(projectID, current); err != nil {
		return nil, err
	}

	if nextWindowSize <= 0 {
		nextWindowSize = 10
	}

	// Get total chapters for the project
	var maxChapter int
	s.db.Model(&models.Chapter{}).Where("project_id = ?", projectID).Select("COALESCE(MAX(chapter_order), 0)").Scan(&maxChapter)
	if maxChapter == 0 {
		return nil, fmt.Errorf("no chapters found for project")
	}

	// Next window starts after current
	nextStart := current.ChapterEnd + 1
	if nextStart > maxChapter {
		return nil, fmt.Errorf("no more chapters after chapter %d", current.ChapterEnd)
	}

	// Calculate next window end
	nextEnd := nextStart + nextWindowSize - 1
	if nextEnd > maxChapter {
		nextEnd = maxChapter
	}

	// Carry a small reviewed tail and pre-read five chapters beyond the target.
	contextStart := max(1, nextStart-2)
	contextEnd := min(maxChapter, nextEnd+5)

	opts := WindowOptions{
		ChapterStart:   nextStart,
		ChapterEnd:     nextEnd,
		ContextStart:   contextStart,
		ContextEnd:     contextEnd,
		WindowSize:     nextWindowSize,
		PreviousWindow: &currentWindowID,
	}
	return s.CreateWindow(projectID, opts)
}

// GetWindowContext retrieves chapter analyses for the context range of a window
func (s *NovelAnalysisService) GetWindowContext(projectID, windowID uint) ([]models.Chapter, error) {
	window, err := s.GetWindow(projectID, windowID)
	if err != nil {
		return nil, err
	}

	var chapters []models.Chapter
	err = s.db.Where("project_id = ? AND chapter_order BETWEEN ? AND ?", projectID, window.ContextStart, window.ContextEnd).
		Order("chapter_order ASC").Find(&chapters).Error
	return chapters, err
}

// GetWindowTargetChapters returns only the target chapters (not context) for a window
func (s *NovelAnalysisService) GetWindowTargetChapters(projectID, windowID uint) ([]models.Chapter, error) {
	window, err := s.GetWindow(projectID, windowID)
	if err != nil {
		return nil, err
	}

	var chapters []models.Chapter
	err = s.db.Where("project_id = ? AND chapter_order BETWEEN ? AND ?", projectID, window.ChapterStart, window.ChapterEnd).
		Order("chapter_order ASC").Find(&chapters).Error
	return chapters, err
}

// IsWindowReady returns true if all target chapters in the window are analyzed
func (s *NovelAnalysisService) IsWindowReady(projectID, windowID uint) (bool, error) {
	window, err := s.GetWindow(projectID, windowID)
	if err != nil {
		return false, err
	}

	var notReady int64
	err = s.db.Model(&models.Chapter{}).Where(
		"project_id = ? AND chapter_order BETWEEN ? AND ? AND analysis_status NOT IN ?",
		projectID, window.ChapterStart, window.ChapterEnd, []string{"ready"},
	).Count(&notReady).Error
	if err != nil {
		return false, err
	}
	return notReady == 0, nil
}

// GetWindowSuggestion suggests the next window based on analysis progress
func (s *NovelAnalysisService) GetWindowSuggestion(projectID uint, windowSize int) (*WindowSuggestion, error) {
	if windowSize <= 0 {
		windowSize = 10
	}
	if windowSize > 50 {
		windowSize = 50
	}

	// Get the last committed or current window
	var lastWindow models.AnalysisWindow
	err := s.db.Where("project_id = ?", projectID).Order("window_no DESC").First(&lastWindow).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return nil, err
	}

	// Get total chapters
	var maxChapter int
	s.db.Model(&models.Chapter{}).Where("project_id = ?", projectID).Select("COALESCE(MAX(chapter_order), 0)").Scan(&maxChapter)
	if maxChapter == 0 {
		return nil, fmt.Errorf("no chapters found for project")
	}

	suggestion := &WindowSuggestion{
		SuggestedStart:        1,
		SuggestedEnd:          min(windowSize, maxChapter),
		SuggestedContextStart: 1,
		SuggestedContextEnd:   min(windowSize+5, maxChapter),
		TotalChapters:         maxChapter,
		RemainingChapters:     maxChapter,
	}

	if err == nil && lastWindow.ID > 0 {
		// There's an existing window
		if lastWindow.Status == models.WindowStatusCommitted {
			suggestion.SuggestedStart = lastWindow.ChapterEnd + 1
			suggestion.SuggestedEnd = suggestion.SuggestedStart + windowSize - 1
			if suggestion.SuggestedEnd > maxChapter {
				suggestion.SuggestedEnd = maxChapter
			}
			suggestion.RemainingChapters = maxChapter - lastWindow.ChapterEnd
			suggestion.SuggestedContextStart = max(1, suggestion.SuggestedStart-2)
			suggestion.SuggestedContextEnd = min(maxChapter, suggestion.SuggestedEnd+5)
		} else {
			// Return current window info
			suggestion.CurrentWindowID = &lastWindow.ID
			suggestion.SuggestedStart = lastWindow.ChapterStart
			suggestion.SuggestedEnd = lastWindow.ChapterEnd
			suggestion.SuggestedContextStart = lastWindow.ContextStart
			suggestion.SuggestedContextEnd = lastWindow.ContextEnd
		}
	}

	// Calculate how many more windows needed
	suggestion.EstimatedWindows = (suggestion.RemainingChapters + windowSize - 1) / windowSize

	return suggestion, nil
}

// WindowSuggestion provides guidance for creating the next window
type WindowSuggestion struct {
	CurrentWindowID       *uint `json:"current_window_id,omitempty"`
	SuggestedStart        int   `json:"suggested_start"`
	SuggestedEnd          int   `json:"suggested_end"`
	SuggestedContextStart int   `json:"suggested_context_start"`
	SuggestedContextEnd   int   `json:"suggested_context_end"`
	TotalChapters         int   `json:"total_chapters"`
	RemainingChapters     int   `json:"remaining_chapters"`
	EstimatedWindows      int   `json:"estimated_windows"`
}
