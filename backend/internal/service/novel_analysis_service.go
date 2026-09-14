package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
	return s.markDownstreamStale(chapter.ProjectID)
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
	if groupSize == 0 {
		groupSize = 8
	}
	if groupSize < 5 || groupSize > 10 {
		return nil, nil, fmt.Errorf("group_size must be between 5 and 10")
	}
	var chapters []models.Chapter
	if err := s.db.Where("project_id = ?", projectID).Order("chapter_order ASC").Find(&chapters).Error; err != nil {
		return nil, nil, err
	}
	if len(chapters) == 0 {
		return nil, nil, fmt.Errorf("project has no chapters")
	}
	for _, chapter := range chapters {
		if chapter.AnalysisStatus != "ready" {
			return nil, nil, fmt.Errorf("all chapters must be ready before arc generation")
		}
	}
	total := (len(chapters) + groupSize - 1) / groupSize
	job, err := s.newJob(projectID, novelJobArcMerge, chapters[0].Order, chapters[len(chapters)-1].Order, total)
	if err != nil {
		return nil, nil, err
	}
	var arcs []models.StoryArc
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
		arc := models.StoryArc{ProjectID: projectID, ArcNo: len(arcs) + 1, Title: parsed.Title, ChapterStart: part[0].Order, ChapterEnd: part[len(part)-1].Order, Summary: parsed.Summary, AnalysisJSON: outputJSON(output), SourceVersions: string(versionJSON), Status: "draft", Version: 1}
		var old models.StoryArc
		if s.db.Where("project_id = ? AND arc_no = ?", projectID, arc.ArcNo).First(&old).Error == nil {
			arc.ID = old.ID
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
		return s.AnalyzeChapters(projectID, nil)
	case novelJobArcMerge:
		_, job, err := s.GenerateArcs(projectID, 8)
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

func (s *NovelAnalysisService) markDownstreamStale(projectID uint) error {
	if err := s.db.Model(&models.StoryArc{}).Where("project_id = ?", projectID).Update("status", "stale").Error; err != nil {
		return err
	}
	if err := s.db.Model(&models.StoryBible{}).Where("project_id = ?", projectID).Update("status", "stale").Error; err != nil {
		return err
	}
	return s.db.Model(&models.EpisodeAdaptation{}).Where("project_id = ?", projectID).Update("status", "stale").Error
}
