package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var ErrMusicCueSheetNotFound = errors.New("配乐 cue 不存在")

type MusicCueService struct {
	db           *gorm.DB
	provider     TextProvider
	skills       *SkillService
	dependencies *ArtifactDependencyService
}

func NewMusicCueService(db *gorm.DB, deps ...any) *MusicCueService {
	s := &MusicCueService{db: db}
	for _, dep := range deps {
		switch value := dep.(type) {
		case TextProvider:
			s.provider = value
		case *SkillService:
			s.skills = value
		case *ArtifactDependencyService:
			s.dependencies = value
		}
	}
	return s
}

// MusicCueSheetInput mirrors the editable fields of MusicCueSheet for API consumption.
type MusicCueSheetInput struct {
	EpisodeN          int     `json:"episode_n"`
	SceneID           *uint   `json:"scene_id"`
	ShotID            *uint   `json:"shot_id"`
	Kind              string  `json:"kind"`
	StartTime         float64 `json:"start_time"`
	EndTime           float64 `json:"end_time"`
	Intensity         string  `json:"intensity"`
	Mood              string  `json:"mood"`
	InstrumentNote    string  `json:"instrument_note"`
	UserRequirements  string  `json:"user_requirements"`
	Duration          float64 `json:"duration"`
	Tags              string  `json:"tags"`
	CaptionJSON       string  `json:"caption_json"`
	SourceVersionHash string  `json:"source_version_hash"`
	SkillInvocationID *uint   `json:"skill_invocation_id"`
	RawOutput         string  `json:"raw_output"`
	Enabled           *bool   `json:"enabled"`
	Order             int     `json:"order"`
	Source            string  `json:"source"`
	ReviewStatus      string  `json:"review_status"`
	ReviewReason      string  `json:"review_reason"`
}

var musicCueKinds = map[string]bool{
	"opening": true, "scene": true, "action": true, "emotion": true,
	"transition": true, "ending": true, "bgm": true,
}

var musicCueIntensities = map[string]bool{
	"soft": true, "medium": true, "intense": true, "climax": true,
}

var musicCueMoods = map[string]bool{
	"tension": true, "joyful": true, "mysterious": true,
	"romantic": true, "epic": true, "sad": true, "calm": true,
}

func musicCueIsFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func normalizeMusicCueSheet(projectID uint, in MusicCueSheetInput) (models.MusicCueSheet, error) {
	kind := strings.ToLower(strings.TrimSpace(in.Kind))
	if kind == "" {
		kind = "scene"
	}
	if !musicCueKinds[kind] {
		return models.MusicCueSheet{}, fmt.Errorf("kind 必须是 opening、scene、action、emotion、transition、ending 或 bgm")
	}
	if in.EpisodeN <= 0 {
		return models.MusicCueSheet{}, fmt.Errorf("episode_n 必须大于 0")
	}
	if !musicCueIsFinite(in.StartTime) || !musicCueIsFinite(in.EndTime) || in.StartTime < 0 || in.EndTime <= in.StartTime {
		return models.MusicCueSheet{}, fmt.Errorf("start_time 必须非负且 end_time 必须大于 start_time")
	}
	intensity := strings.ToLower(strings.TrimSpace(in.Intensity))
	if intensity == "" {
		intensity = "medium"
	}
	if !musicCueIntensities[intensity] {
		return models.MusicCueSheet{}, fmt.Errorf("intensity 必须是 soft、medium、intense 或 climax")
	}
	mood := strings.ToLower(strings.TrimSpace(in.Mood))
	if mood != "" && !musicCueMoods[mood] {
		return models.MusicCueSheet{}, fmt.Errorf("mood 必须是 tension、joyful、mysterious、romantic、epic、sad 或 calm")
	}
	duration := in.Duration
	if duration < 0 || !musicCueIsFinite(duration) {
		duration = 0
	}
	captionJSON := strings.TrimSpace(in.CaptionJSON)
	if captionJSON != "" {
		var v any
		if err := json.Unmarshal([]byte(captionJSON), &v); err != nil {
			return models.MusicCueSheet{}, fmt.Errorf("caption_json 必须是合法 JSON")
		}
	}
	source := strings.ToLower(strings.TrimSpace(in.Source))
	if source == "" {
		source = "manual"
	}
	if source != "manual" && source != "ai" {
		return models.MusicCueSheet{}, fmt.Errorf("source 必须是 manual 或 ai")
	}
	reviewStatus := strings.ToLower(strings.TrimSpace(in.ReviewStatus))
	if reviewStatus == "" {
		reviewStatus = "pending"
	}
	if !regexp.MustCompile(`^(pending|draft|approved|rejected)$`).MatchString(reviewStatus) {
		return models.MusicCueSheet{}, fmt.Errorf("review_status 必须是 pending、draft、approved 或 rejected")
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	return models.MusicCueSheet{
		ProjectID: projectID, EpisodeN: in.EpisodeN, SceneID: in.SceneID, ShotID: in.ShotID,
		Kind: kind, StartTime: in.StartTime, EndTime: in.EndTime,
		Intensity: intensity, Mood: mood,
		InstrumentNote: strings.TrimSpace(in.InstrumentNote), UserRequirements: strings.TrimSpace(in.UserRequirements),
		Duration: duration, Tags: strings.TrimSpace(in.Tags),
		CaptionJSON: captionJSON, SourceVersionHash: strings.TrimSpace(in.SourceVersionHash),
		SkillInvocationID: in.SkillInvocationID, RawOutput: strings.TrimSpace(in.RawOutput),
		Enabled: enabled, Order: in.Order, Source: source,
		ReviewStatus: reviewStatus, ReviewReason: strings.TrimSpace(in.ReviewReason),
	}, nil
}

func (s *MusicCueService) validateBindings(cue *models.MusicCueSheet) error {
	if cue.SceneID != nil {
		var scene models.Scene
		if err := s.db.Where("id = ? AND project_id = ? AND episode_n = ?", *cue.SceneID, cue.ProjectID, cue.EpisodeN).First(&scene).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("场景不属于当前项目或集")
			}
			return err
		}
	}
	if cue.ShotID != nil {
		if cue.SceneID == nil {
			return fmt.Errorf("绑定镜头时必须同时绑定场景")
		}
		var count int64
		if err := s.db.Model(&models.Shot{}).Where("id = ? AND scene_id = ?", *cue.ShotID, *cue.SceneID).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return fmt.Errorf("镜头不属于绑定场景")
		}
	}
	return nil
}

func (s *MusicCueService) List(projectID uint, episodeN int) ([]models.MusicCueSheet, error) {
	var cues []models.MusicCueSheet
	q := s.db.Where("project_id = ?", projectID)
	if episodeN > 0 {
		q = q.Where("episode_n = ?", episodeN)
	}
	err := q.Order("episode_n, start_time, `order`, id").Find(&cues).Error
	return cues, err
}

func musicCueSheetUpdates(cue models.MusicCueSheet) map[string]any {
	return map[string]any{
		"episode_n": cue.EpisodeN, "scene_id": cue.SceneID, "shot_id": cue.ShotID,
		"kind": cue.Kind, "start_time": cue.StartTime, "end_time": cue.EndTime,
		"intensity": cue.Intensity, "mood": cue.Mood, "instrument_note": cue.InstrumentNote, "user_requirements": cue.UserRequirements,
		"duration": cue.Duration, "tags": cue.Tags,
		"caption_json": cue.CaptionJSON, "source_version_hash": cue.SourceVersionHash,
		"skill_invocation_id": cue.SkillInvocationID, "raw_output": cue.RawOutput,
		"enabled": cue.Enabled, "order": cue.Order, "source": cue.Source,
		"review_status": cue.ReviewStatus, "review_reason": cue.ReviewReason,
	}
}

func (s *MusicCueService) Create(projectID uint, in MusicCueSheetInput) (*models.MusicCueSheet, error) {
	cue, err := normalizeMusicCueSheet(projectID, in)
	if err != nil {
		return nil, err
	}
	if err := s.validateBindings(&cue); err != nil {
		return nil, err
	}
	cue.ReviewStatus = "draft"
	cue.ReviewReason = ""
	cue.ReviewedAt = nil
	if context, contextErr := s.episodeContext(projectID, cue.EpisodeN); contextErr == nil {
		cue.SourceVersionHash = musicCueSourceHash(context, &cue)
	}
	if err := s.db.Create(&cue).Error; err != nil {
		return nil, err
	}
	// Reload so the returned pointer reflects the DB-persisted state (including auto-generated ID).
	created := &models.MusicCueSheet{}
	if err := s.db.First(created, cue.ID).Error; err != nil {
		return nil, err
	}
	return created, nil
}

func (s *MusicCueService) Update(projectID, id uint, in MusicCueSheetInput) (*models.MusicCueSheet, error) {
	var existing models.MusicCueSheet
	if err := s.db.Where("id = ? AND project_id = ?", id, projectID).First(&existing).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrMusicCueSheetNotFound
		}
		return nil, err
	}
	cue, err := normalizeMusicCueSheet(projectID, in)
	if err != nil {
		return nil, err
	}
	if err := s.validateBindings(&cue); err != nil {
		return nil, err
	}
	cue.ReviewStatus = "draft"
	cue.ReviewReason = ""
	if !existing.Stale {
		if context, contextErr := s.episodeContext(projectID, cue.EpisodeN); contextErr == nil {
			cue.SourceVersionHash = musicCueSourceHash(context, &cue)
		}
	} else {
		cue.SourceVersionHash = existing.SourceVersionHash
	}
	updates := musicCueSheetUpdates(cue)
	updates["reviewed_at"] = nil
	updates["stale"] = existing.Stale
	updates["stale_reason"] = existing.StaleReason
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := markMusicCandidatesStale(tx, projectID, id, "配乐Cue内容已修改"); err != nil {
			return err
		}
		if s.dependencies != nil {
			if _, err := s.dependencies.RecordInvalidation(tx, projectID, ArtifactRef{Kind: "music_cue", ID: id}, "配乐Cue内容已修改"); err != nil {
				return err
			}
		}
		return tx.Model(&existing).Updates(updates).Error
	}); err != nil {
		return nil, err
	}
	if err := s.db.First(&existing, id).Error; err != nil {
		return nil, err
	}
	return &existing, nil
}

func (s *MusicCueService) Delete(projectID, id uint) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if s.dependencies != nil {
			if err := s.dependencies.DeleteArtifactTx(tx, projectID, ArtifactRef{Kind: "music_cue", ID: id}); err != nil {
				return err
			}
		}
		if tx.Migrator().HasTable(&models.MusicCandidate{}) {
			if err := tx.Where("project_id = ? AND music_cue_sheet_id = ?", projectID, id).Delete(&models.MusicCandidate{}).Error; err != nil {
				return err
			}
		}
		result := tx.Where("id = ? AND project_id = ?", id, projectID).Delete(&models.MusicCueSheet{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrMusicCueSheetNotFound
		}
		return nil
	})
}

func (s *MusicCueService) markCuesAndCandidatesStale(query string, args []any, projectID uint, reason string) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		var ids []uint
		if err := tx.Model(&models.MusicCueSheet{}).Where(query, args...).Pluck("id", &ids).Error; err != nil {
			return err
		}
		if len(ids) > 0 && tx.Migrator().HasTable(&models.MusicCandidate{}) {
			if err := tx.Model(&models.MusicCandidate{}).Where("project_id = ? AND music_cue_sheet_id IN ? AND stale = ?", projectID, ids, false).Updates(map[string]any{"stale": true, "stale_reason": reason, "is_current": false}).Error; err != nil {
				return err
			}
		}
		if s.dependencies != nil {
			for _, id := range ids {
				if _, err := s.dependencies.RecordInvalidation(tx, projectID, ArtifactRef{Kind: "music_cue", ID: id}, reason); err != nil {
					return err
				}
			}
		}
		return tx.Model(&models.MusicCueSheet{}).Where(query, args...).Updates(map[string]any{"stale": true, "stale_reason": reason}).Error
	})
}

// MarkSceneStale invalidates Cue plans and any audio candidates derived from them.
func (s *MusicCueService) MarkSceneStale(projectID uint, sceneID uint, reason string) error {
	return s.markCuesAndCandidatesStale("project_id = ? AND scene_id = ? AND stale = ?", []any{projectID, sceneID, false}, projectID, reason)
}

// MarkEpisodeStale invalidates Cue plans and any audio candidates derived from them.
func (s *MusicCueService) MarkEpisodeStale(projectID uint, episodeN int, reason string) error {
	return s.markCuesAndCandidatesStale("project_id = ? AND episode_n = ? AND stale = ?", []any{projectID, episodeN, false}, projectID, reason)
}

type MusicCuePreflightIssue struct {
	Severity     string `json:"severity"`
	Code         string `json:"code"`
	CueID        uint   `json:"cue_id,omitempty"`
	RelatedCueID uint   `json:"related_cue_id,omitempty"`
	Message      string `json:"message"`
}
type MusicCuePreflightResult struct {
	EpisodeN      int                      `json:"episode_n"`
	TotalDuration float64                  `json:"total_duration"`
	Ready         bool                     `json:"ready"`
	Issues        []MusicCuePreflightIssue `json:"issues"`
}

func (s *MusicCueService) Preflight(projectID uint, episodeN int) (MusicCuePreflightResult, error) {
	result := MusicCuePreflightResult{EpisodeN: episodeN, Ready: true, Issues: []MusicCuePreflightIssue{}}
	if err := s.db.Model(&models.Scene{}).Where("project_id = ? AND episode_n = ?", projectID, episodeN).Select("COALESCE(SUM(duration),0)").Scan(&result.TotalDuration).Error; err != nil {
		return result, err
	}
	cues, err := s.List(projectID, episodeN)
	if err != nil {
		return result, err
	}
	if len(cues) == 0 {
		result.Ready = false
		result.Issues = append(result.Issues, MusicCuePreflightIssue{"error", "empty", 0, 0, "当前Episode尚未创建配乐Cue Sheet"})
		return result, nil
	}
	context, contextErr := s.episodeContext(projectID, episodeN)
	if contextErr != nil {
		return result, contextErr
	}
	for i := range cues {
		cue := &cues[i]
		if !cue.Enabled {
			continue
		}
		if cue.Stale {
			result.Issues = append(result.Issues, MusicCuePreflightIssue{"error", "stale", cue.ID, 0, "Cue已过期"})
		}
		if err := s.validateBindings(cue); err != nil {
			result.Issues = append(result.Issues, MusicCuePreflightIssue{"error", "binding", cue.ID, 0, err.Error()})
		}
		if err := s.validateBindingTimeline(cue); err != nil {
			result.Issues = append(result.Issues, MusicCuePreflightIssue{"error", "binding_timeline", cue.ID, 0, err.Error()})
		}
		if err := s.validateCueTimeline(cue); err != nil {
			result.Issues = append(result.Issues, MusicCuePreflightIssue{"error", "timeline", cue.ID, 0, err.Error()})
		}
		if err := validateMusicCaptionJSON(cue.CaptionJSON); err != nil {
			result.Issues = append(result.Issues, MusicCuePreflightIssue{"error", "caption", cue.ID, 0, err.Error()})
		}
		expectedHash := musicCueSourceHash(context, cue)
		if cue.SourceVersionHash == "" || cue.SourceVersionHash != expectedHash {
			result.Issues = append(result.Issues, MusicCuePreflightIssue{"error", "source", cue.ID, 0, "Cue来源哈希与当前Episode剧本或用户要求不一致"})
		}
		if cue.ReviewStatus != "approved" || cue.ReviewedAt == nil {
			result.Issues = append(result.Issues, MusicCuePreflightIssue{"error", "review", cue.ID, 0, "Cue尚未完成当前版本的人工审核"})
		}
		for j := i + 1; j < len(cues); j++ {
			other := cues[j]
			if other.Enabled && cue.StartTime < other.EndTime && cue.EndTime > other.StartTime {
				result.Issues = append(result.Issues, MusicCuePreflightIssue{"warning", "overlap", cue.ID, other.ID, "Cue时间范围重叠，请人工确认"})
			}
		}
	}
	for _, issue := range result.Issues {
		if issue.Severity == "error" {
			result.Ready = false
			break
		}
	}
	return result, nil
}

// MarkApproved validates every cue before atomically approving the Episode.
func (s *MusicCueService) MarkApproved(projectID uint, episodeN int, reviewReason string) error {
	var cues []models.MusicCueSheet
	if err := s.db.Where("project_id = ? AND episode_n = ? AND enabled = ? AND review_status IN ?", projectID, episodeN, true, []string{"pending", "draft"}).Find(&cues).Error; err != nil {
		return err
	}
	if len(cues) == 0 {
		return fmt.Errorf("没有可审核的配乐Cue")
	}
	context, err := s.episodeContext(projectID, episodeN)
	if err != nil {
		return err
	}
	for _, cue := range cues {
		if cue.Stale {
			return fmt.Errorf("Cue #%d 已过期", cue.ID)
		}
		if err := s.validateBindings(&cue); err != nil {
			return fmt.Errorf("Cue #%d: %w", cue.ID, err)
		}
		if err := s.validateBindingTimeline(&cue); err != nil {
			return fmt.Errorf("Cue #%d: %w", cue.ID, err)
		}
		if err := s.validateCueTimeline(&cue); err != nil {
			return fmt.Errorf("Cue #%d: %w", cue.ID, err)
		}
		if err := validateMusicCaptionJSON(cue.CaptionJSON); err != nil {
			return fmt.Errorf("Cue #%d: %w", cue.ID, err)
		}
		if cue.SourceVersionHash == "" || cue.SourceVersionHash != musicCueSourceHash(context, &cue) {
			return fmt.Errorf("Cue #%d 缺少当前剧本来源哈希，或剧本/用户要求已变化", cue.ID)
		}
	}
	now := time.Now()
	return s.db.Model(&models.MusicCueSheet{}).Where("project_id = ? AND episode_n = ? AND enabled = ? AND review_status IN ?", projectID, episodeN, true, []string{"pending", "draft"}).Updates(map[string]any{"review_status": "approved", "review_reason": strings.TrimSpace(reviewReason), "reviewed_at": now}).Error
}

type musicStructuredCaption struct {
	GlobalMetadata string `json:"global_metadata"`
	VocalDetails   string `json:"vocal_details"`
	Arrangement    string `json:"arrangement"`
}

type generatedMusicCue struct {
	SceneID          *uint                  `json:"scene_id"`
	ShotID           *uint                  `json:"shot_id"`
	Kind             string                 `json:"kind"`
	StartTime        float64                `json:"start_time"`
	EndTime          float64                `json:"end_time"`
	Intensity        string                 `json:"intensity"`
	Mood             string                 `json:"mood"`
	InstrumentNote   string                 `json:"instrument_note"`
	UserRequirements string                 `json:"user_requirements"`
	Caption          musicStructuredCaption `json:"caption"`
}

type generatedMusicCueSheet struct {
	Cues []generatedMusicCue `json:"cues"`
}

func renderMusicCaption(raw string) (string, error) {
	if err := validateMusicCaptionJSON(raw); err != nil {
		return "", err
	}
	var caption musicStructuredCaption
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &caption); err != nil {
		return "", err
	}
	return "### Global Metadata\n\n" + strings.TrimSpace(caption.GlobalMetadata) + "\n\n### Vocal Details\n\n" + strings.TrimSpace(caption.VocalDetails) + "\n\n### Arrangement\n\n" + strings.TrimSpace(caption.Arrangement), nil
}

type MusicCueSubmissionItem struct {
	ID                uint    `json:"id"`
	SceneID           *uint   `json:"scene_id,omitempty"`
	ShotID            *uint   `json:"shot_id,omitempty"`
	StartTime         float64 `json:"start_time"`
	EndTime           float64 `json:"end_time"`
	Caption           string  `json:"caption"`
	SourceVersionHash string  `json:"source_version_hash"`
}
type MusicCueSubmissionManifest struct {
	ProjectID uint                     `json:"project_id"`
	EpisodeN  int                      `json:"episode_n"`
	Ready     bool                     `json:"ready"`
	Items     []MusicCueSubmissionItem `json:"items"`
	Note      string                   `json:"note"`
}

func (s *MusicCueService) SubmissionManifest(projectID uint, episodeN int) (MusicCueSubmissionManifest, error) {
	manifest := MusicCueSubmissionManifest{ProjectID: projectID, EpisodeN: episodeN, Items: []MusicCueSubmissionItem{}, Note: "审核清单；本阶段不创建Music 3生成任务或AudioLayer"}
	preflight, err := s.Preflight(projectID, episodeN)
	if err != nil {
		return manifest, err
	}
	if !preflight.Ready {
		return manifest, fmt.Errorf("配乐Cue Sheet尚未通过权威预检")
	}
	cues, err := s.List(projectID, episodeN)
	if err != nil {
		return manifest, err
	}
	for _, cue := range cues {
		if !cue.Enabled {
			continue
		}
		if cue.ReviewStatus != "approved" || cue.ReviewedAt == nil {
			return manifest, fmt.Errorf("Cue #%d尚未完成人工审核", cue.ID)
		}
		caption, err := renderMusicCaption(cue.CaptionJSON)
		if err != nil {
			return manifest, fmt.Errorf("Cue #%d: %w", cue.ID, err)
		}
		manifest.Items = append(manifest.Items, MusicCueSubmissionItem{cue.ID, cue.SceneID, cue.ShotID, cue.StartTime, cue.EndTime, caption, cue.SourceVersionHash})
	}
	if len(manifest.Items) == 0 {
		return manifest, fmt.Errorf("没有可提交的已审核配乐Cue")
	}
	manifest.Ready = true
	return manifest, nil
}

func (s *MusicCueService) CaptionPreview(projectID, id uint) (map[string]any, error) {
	var cue models.MusicCueSheet
	if err := s.db.Where("id = ? AND project_id = ?", id, projectID).First(&cue).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrMusicCueSheetNotFound
		}
		return nil, err
	}
	caption, err := renderMusicCaption(cue.CaptionJSON)
	if err != nil {
		return nil, err
	}
	return map[string]any{"cue_id": cue.ID, "caption": caption, "source_version_hash": cue.SourceVersionHash, "review_status": cue.ReviewStatus, "reviewed_at": cue.ReviewedAt, "stale": cue.Stale}, nil
}

func validateMusicCaptionJSON(raw string) error {
	decoder := json.NewDecoder(strings.NewReader(strings.TrimSpace(raw)))
	decoder.DisallowUnknownFields()
	var caption musicStructuredCaption
	if err := decoder.Decode(&caption); err != nil {
		return fmt.Errorf("Structured Caption JSON 无效或包含非契约字段: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("Structured Caption 只能包含一个JSON对象")
	}
	global, vocal, arrangement := strings.TrimSpace(caption.GlobalMetadata), strings.TrimSpace(caption.VocalDetails), strings.TrimSpace(caption.Arrangement)
	if global == "" || vocal == "" || arrangement == "" {
		return fmt.Errorf("Structured Caption 必须且只能包含 global_metadata、vocal_details 和 arrangement")
	}
	if len(global) > 3000 || len(vocal) > 2000 || len(arrangement) > 8000 {
		return fmt.Errorf("Structured Caption 字段过长")
	}
	lower := strings.ToLower(vocal)
	if !strings.Contains(lower, "instrumental") || strings.Contains(lower, "not instrumental") {
		return fmt.Errorf("短剧配乐必须在 vocal_details 中明确为纯器乐")
	}
	if regexp.MustCompile(`(?i)\b(lead vocal|spoken voice|narration|singing|chanting|humming|whispering)\b`).MatchString(lower) && !regexp.MustCompile(`(?i)\b(no|without|exclude|avoids?)\b`).MatchString(lower) {
		return fmt.Errorf("短剧配乐不得要求人声、对白或吟唱")
	}
	if !regexp.MustCompile(`(?i)(intro|opening|section|development|climax|outro|ending|\b\d+(?:\.\d+)?\s*(?:s|sec|seconds))`).MatchString(arrangement) {
		return fmt.Errorf("arrangement 必须包含可读的分段或时间线")
	}
	return nil
}

func (s *MusicCueService) validateBindingTimeline(cue *models.MusicCueSheet) error {
	if cue.SceneID == nil {
		return nil
	}
	var scenes []models.Scene
	if err := s.db.Where("project_id = ? AND episode_n = ?", cue.ProjectID, cue.EpisodeN).Order("`order`, id").Find(&scenes).Error; err != nil {
		return err
	}
	offset := 0.0
	sceneStart, sceneEnd := -1.0, -1.0
	for _, scene := range scenes {
		if scene.ID == *cue.SceneID {
			sceneStart, sceneEnd = offset, offset+scene.Duration
			break
		}
		offset += scene.Duration
	}
	if sceneStart < 0 {
		return fmt.Errorf("绑定Scene不在当前Episode时间轴")
	}
	boundStart, boundEnd := sceneStart, sceneEnd
	if cue.ShotID != nil {
		var shots []models.Shot
		if err := s.db.Where("scene_id = ?", *cue.SceneID).Order("order_num, id").Find(&shots).Error; err != nil {
			return err
		}
		shotOffset := sceneStart
		found := false
		for _, shot := range shots {
			if shot.ID == *cue.ShotID {
				boundStart, boundEnd = shotOffset, shotOffset+shot.Duration
				found = true
				break
			}
			shotOffset += shot.Duration
		}
		if !found {
			return fmt.Errorf("绑定Shot不在当前Scene时间轴")
		}
	}
	if cue.EndTime <= boundStart || cue.StartTime >= boundEnd {
		return fmt.Errorf("Cue时间范围 %.2f–%.2f 秒与绑定实体 %.2f–%.2f 秒不相交", cue.StartTime, cue.EndTime, boundStart, boundEnd)
	}
	return nil
}

func (s *MusicCueService) validateCueTimeline(cue *models.MusicCueSheet) error {
	var total float64
	if err := s.db.Model(&models.Scene{}).Where("project_id = ? AND episode_n = ?", cue.ProjectID, cue.EpisodeN).Select("COALESCE(SUM(duration),0)").Scan(&total).Error; err != nil {
		return err
	}
	if total <= 0 {
		return fmt.Errorf("当前Episode没有有效时间轴")
	}
	if cue.StartTime < 0 || cue.EndTime > total+0.001 || cue.EndTime <= cue.StartTime {
		return fmt.Errorf("Cue时间范围 %.2f–%.2f 秒超出Episode总时长 %.2f 秒", cue.StartTime, cue.EndTime, total)
	}
	return nil
}

func mergeMusicRequirements(values ...string) string {
	seen := map[string]bool{}
	lines := []string{}
	for _, value := range values {
		for _, line := range strings.Split(strings.ReplaceAll(value, "；", "\n"), "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !seen[line] {
				seen[line] = true
				lines = append(lines, line)
			}
		}
	}
	return strings.Join(lines, "\n")
}

func musicCueSourceHash(context string, cue *models.MusicCueSheet) string {
	fingerprint := struct {
		Context          string  `json:"context"`
		SceneID          *uint   `json:"scene_id,omitempty"`
		ShotID           *uint   `json:"shot_id,omitempty"`
		Kind             string  `json:"kind"`
		StartTime        float64 `json:"start_time"`
		EndTime          float64 `json:"end_time"`
		Intensity        string  `json:"intensity"`
		Mood             string  `json:"mood"`
		InstrumentNote   string  `json:"instrument_note"`
		UserRequirements string  `json:"user_requirements"`
		CaptionJSON      string  `json:"caption_json"`
		Enabled          bool    `json:"enabled"`
		Order            int     `json:"order"`
	}{context, cue.SceneID, cue.ShotID, cue.Kind, cue.StartTime, cue.EndTime, cue.Intensity, cue.Mood, cue.InstrumentNote, strings.TrimSpace(cue.UserRequirements), strings.TrimSpace(cue.CaptionJSON), cue.Enabled, cue.Order}
	encoded, _ := json.Marshal(fingerprint)
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:])
}

func trimJSONEnvelope(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	return strings.TrimSpace(raw)
}

func (s *MusicCueService) episodeContext(projectID uint, episodeN int) (string, error) {
	var project models.Project
	if err := s.db.First(&project, projectID).Error; err != nil {
		return "", err
	}
	var scenes []models.Scene
	if err := s.db.Where("project_id = ? AND episode_n = ?", projectID, episodeN).Order("`order`, id").Find(&scenes).Error; err != nil {
		return "", err
	}
	if len(scenes) == 0 {
		return "", fmt.Errorf("第%d集还没有可用于配乐规划的场景", episodeN)
	}
	payload := make([]map[string]any, 0, len(scenes))
	offset := 0.0
	hasShots := s.db.Migrator().HasTable(&models.Shot{})
	hasDialogues := s.db.Migrator().HasTable(&models.Dialogue{})
	for _, scene := range scenes {
		var shots []models.Shot
		if hasShots {
			if err := s.db.Where("scene_id = ?", scene.ID).Order("order_num, id").Find(&shots).Error; err != nil {
				return "", err
			}
		}
		var dialogues []models.Dialogue
		if hasDialogues {
			if err := s.db.Where("scene_id = ?", scene.ID).Order("`order`, id").Find(&dialogues).Error; err != nil {
				return "", err
			}
		}
		payload = append(payload, map[string]any{"scene_id": scene.ID, "scene_order": scene.Order, "episode_start": offset, "duration": scene.Duration, "title": scene.Title, "content": scene.Content, "shots": shots, "dialogues": dialogues})
		offset += scene.Duration
	}
	encoded, err := json.Marshal(map[string]any{"project_title": project.Title, "genre": project.Genre, "style": project.Style, "episode_n": episodeN, "total_duration": offset, "scenes": payload})
	return string(encoded), err
}

func (s *MusicCueService) DraftEpisode(projectID uint, episodeN int, requirements string) ([]models.MusicCueSheet, error) {
	if s.provider == nil || s.skills == nil {
		return nil, fmt.Errorf("配乐提示词生成服务未配置")
	}
	context, err := s.episodeContext(projectID, episodeN)
	if err != nil {
		return nil, err
	}
	var approvedCount int64
	if err := s.db.Model(&models.MusicCueSheet{}).Where("project_id = ? AND episode_n = ? AND review_status = ?", projectID, episodeN, "approved").Count(&approvedCount).Error; err != nil {
		return nil, err
	}
	if approvedCount > 0 {
		return nil, fmt.Errorf("当前Episode已有%d条已审核Cue；为保护审核结果，请逐条编辑使其回到草稿或删除后再重新生成整套方案", approvedCount)
	}
	baseSystem := `You plan short-drama instrumental score cues and rewrite every cue for MiniMax Music 3. Output strict JSON only. Root field: cues. Each cue has scene_id,shot_id,kind,start_time,end_time,intensity,mood,instrument_note,user_requirements,caption. Bind scene_id and shot_id only when the cue clearly belongs to that authoritative timeline entity; otherwise use null. caption must have exactly global_metadata,vocal_details,arrangement. Music is fully instrumental: no lead vocal, spoken voice, narration, chanting, choir, humming, whispering, or vocal-like texture. Arrangement must describe section-by-section musical evolution, not merely list instruments. Preserve explicit user requirements and exclusions as highest priority. Keep dialogue space and avoid wall-to-wall music. Do not create audio or claim generation happened.`
	baseUser := "Authoritative episode timeline JSON:\n" + context + "\n\nUser music ideas and requirements:\n" + strings.TrimSpace(requirements)
	raw, err := s.skills.ChatWithConfiguredOrFallbackSkill(projectID, models.SkillStageMusicCueSheet, "music-cue-sheet-generate", s.provider, baseSystem, baseUser, map[string]string{"episode_context": context, "user_requirements": requirements})
	if err != nil {
		return nil, err
	}
	var generated generatedMusicCueSheet
	if err := json.Unmarshal([]byte(trimJSONEnvelope(raw)), &generated); err != nil {
		return nil, fmt.Errorf("AI配乐方案JSON解析失败: %w", err)
	}
	if len(generated.Cues) == 0 {
		return nil, fmt.Errorf("AI未返回任何配乐Cue")
	}
	rows := make([]models.MusicCueSheet, 0, len(generated.Cues))
	err = s.db.Transaction(func(tx *gorm.DB) error {
		var replacedIDs []uint
		if err := tx.Model(&models.MusicCueSheet{}).Where("project_id = ? AND episode_n = ? AND source = ? AND review_status <> ?", projectID, episodeN, "ai", "approved").Pluck("id", &replacedIDs).Error; err != nil {
			return err
		}
		for _, id := range replacedIDs {
			if s.dependencies != nil {
				if err := s.dependencies.DeleteArtifactTx(tx, projectID, ArtifactRef{Kind: ArtifactKindMusicCue, ID: id}); err != nil {
					return err
				}
			}
		}
		if len(replacedIDs) > 0 && tx.Migrator().HasTable(&models.MusicCandidate{}) {
			if err := tx.Where("project_id = ? AND music_cue_sheet_id IN ?", projectID, replacedIDs).Delete(&models.MusicCandidate{}).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("project_id = ? AND episode_n = ? AND source = ? AND review_status <> ?", projectID, episodeN, "ai", "approved").Delete(&models.MusicCueSheet{}).Error; err != nil {
			return err
		}
		for index, item := range generated.Cues {
			caption, _ := json.Marshal(item.Caption)
			if err := validateMusicCaptionJSON(string(caption)); err != nil {
				return fmt.Errorf("第%d条Cue: %w", index+1, err)
			}
			input := MusicCueSheetInput{EpisodeN: episodeN, SceneID: item.SceneID, ShotID: item.ShotID, Kind: item.Kind, StartTime: item.StartTime, EndTime: item.EndTime, Intensity: item.Intensity, Mood: item.Mood, InstrumentNote: item.InstrumentNote, UserRequirements: mergeMusicRequirements(item.UserRequirements, requirements), CaptionJSON: string(caption), Source: "ai", Order: index + 1}
			cue, err := normalizeMusicCueSheet(projectID, input)
			if err != nil {
				return fmt.Errorf("第%d条Cue: %w", index+1, err)
			}
			if err := NewMusicCueService(tx).validateBindings(&cue); err != nil {
				return fmt.Errorf("第%d条Cue: %w", index+1, err)
			}
			cue.SourceVersionHash = musicCueSourceHash(context, &cue)
			if err := NewMusicCueService(tx).validateBindingTimeline(&cue); err != nil {
				return fmt.Errorf("第%d条Cue: %w", index+1, err)
			}
			if err := NewMusicCueService(tx).validateCueTimeline(&cue); err != nil {
				return fmt.Errorf("第%d条Cue: %w", index+1, err)
			}
			cue.RawOutput = raw
			cue.ReviewStatus = "draft"
			if err := tx.Create(&cue).Error; err != nil {
				return err
			}
			rows = append(rows, cue)
		}
		return nil
	})
	return rows, err
}

func (s *MusicCueService) RegenerateCue(projectID, id uint) (*models.MusicCueSheet, error) {
	if s.provider == nil || s.skills == nil {
		return nil, fmt.Errorf("配乐提示词生成服务未配置")
	}
	var current models.MusicCueSheet
	if err := s.db.Where("id = ? AND project_id = ?", id, projectID).First(&current).Error; err != nil {
		return nil, ErrMusicCueSheetNotFound
	}
	context, err := s.episodeContext(projectID, current.EpisodeN)
	if err != nil {
		return nil, err
	}
	currentJSON, _ := json.Marshal(current)
	baseSystem := `Rewrite exactly one short-drama instrumental Music 3 cue. Output strict JSON with root field cue. Keep start_time and end_time unchanged. cue has scene_id,shot_id,kind,start_time,end_time,intensity,mood,instrument_note,user_requirements,caption. Preserve the current scene_id and shot_id. caption has exactly global_metadata,vocal_details,arrangement. Fully instrumental; no speech, singing, choir, chanting, humming, whispering or vocal-like textures. Preserve user requirements as highest priority.`
	baseUser := "Episode timeline:\n" + context + "\n\nCurrent cue:\n" + string(currentJSON)
	raw, err := s.skills.ChatWithConfiguredOrFallbackSkill(projectID, models.SkillStageMusicCueSheet, "music-cue-sheet-generate", s.provider, baseSystem, baseUser, map[string]string{"episode_context": context, "user_requirements": current.UserRequirements})
	if err != nil {
		return nil, err
	}
	var wrapper struct {
		Cue generatedMusicCue `json:"cue"`
	}
	if err := json.Unmarshal([]byte(trimJSONEnvelope(raw)), &wrapper); err != nil {
		return nil, fmt.Errorf("AI配乐Cue JSON解析失败: %w", err)
	}
	item := wrapper.Cue
	if item.StartTime != current.StartTime || item.EndTime != current.EndTime {
		return nil, fmt.Errorf("AI不得修改Cue时间范围")
	}
	caption, _ := json.Marshal(item.Caption)
	if err := validateMusicCaptionJSON(string(caption)); err != nil {
		return nil, err
	}
	updated, err := s.Update(projectID, id, MusicCueSheetInput{EpisodeN: current.EpisodeN, SceneID: current.SceneID, ShotID: current.ShotID, Kind: item.Kind, StartTime: current.StartTime, EndTime: current.EndTime, Intensity: item.Intensity, Mood: item.Mood, InstrumentNote: item.InstrumentNote, UserRequirements: current.UserRequirements, Duration: current.Duration, Tags: current.Tags, CaptionJSON: string(caption), Source: "ai", Order: current.Order, Enabled: &current.Enabled})
	if err != nil {
		return nil, err
	}
	context, err = s.episodeContext(projectID, current.EpisodeN)
	if err != nil {
		return nil, err
	}
	updated.Stale = false
	updated.StaleReason = ""
	updated.SourceVersionHash = musicCueSourceHash(context, updated)
	updated.RawOutput = raw
	if err := s.db.Model(updated).Updates(map[string]any{"stale": false, "stale_reason": "", "source_version_hash": updated.SourceVersionHash, "raw_output": raw}).Error; err != nil {
		return nil, err
	}
	return updated, nil
}

func (s *MusicCueService) Review(projectID, id uint, approve bool, reason string) (*models.MusicCueSheet, error) {
	var cue models.MusicCueSheet
	if err := s.db.Where("id = ? AND project_id = ?", id, projectID).First(&cue).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrMusicCueSheetNotFound
		}
		return nil, err
	}
	if approve {
		if cue.Stale {
			return nil, fmt.Errorf("配乐Cue已过期，请重新生成或编辑")
		}
		if !cue.Enabled {
			return nil, fmt.Errorf("已禁用的配乐Cue不能审核通过")
		}
		if err := s.validateBindings(&cue); err != nil {
			return nil, err
		}
		if err := s.validateBindingTimeline(&cue); err != nil {
			return nil, err
		}
		if err := s.validateCueTimeline(&cue); err != nil {
			return nil, err
		}
		if err := validateMusicCaptionJSON(cue.CaptionJSON); err != nil {
			return nil, err
		}
		context, err := s.episodeContext(projectID, cue.EpisodeN)
		if err != nil {
			return nil, err
		}
		if cue.SourceVersionHash == "" || cue.SourceVersionHash != musicCueSourceHash(context, &cue) {
			return nil, fmt.Errorf("缺少当前剧本来源哈希，或剧本/用户要求已变化，请重新生成配乐提示词")
		}
		cue.ReviewStatus = "approved"
	} else {
		if strings.TrimSpace(reason) == "" {
			return nil, fmt.Errorf("拒绝时必须填写原因")
		}
		cue.ReviewStatus = "rejected"
	}
	now := time.Now()
	cue.ReviewReason = strings.TrimSpace(reason)
	cue.ReviewedAt = &now
	if err := s.db.Save(&cue).Error; err != nil {
		return nil, err
	}
	return &cue, nil
}

// ---- HTTP handlers ----

func (s *Service) HandleListMusicCueSheets(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	episodeN, _ := strconv.Atoi(c.Query("episode_n"))
	cues, err := s.MusicCues.List(p.ID, episodeN)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, cues)
}

func (s *Service) HandleCreateMusicCueSheet(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	var input MusicCueSheetInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	cue, err := s.MusicCues.Create(p.ID, input)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, cue)
}

func (s *Service) HandleUpdateMusicCueSheet(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	id, err := strconv.ParseUint(c.Param("cid"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid music cue sheet id"})
		return
	}
	var input MusicCueSheetInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	cue, err := s.MusicCues.Update(p.ID, uint(id), input)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, ErrMusicCueSheetNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, cue)
}

func (s *Service) HandleDeleteMusicCueSheet(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	id, err := strconv.ParseUint(c.Param("cid"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid music cue sheet id"})
		return
	}
	if err := s.MusicCues.Delete(p.ID, uint(id)); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, ErrMusicCueSheetNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Service) HandleDraftMusicCueSheets(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	var body struct {
		EpisodeN     int    `json:"episode_n"`
		Requirements string `json:"requirements"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.EpisodeN <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "episode_n 必须大于 0"})
		return
	}
	cues, err := s.MusicCues.DraftEpisode(p.ID, body.EpisodeN, body.Requirements)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"cues": cues})
}

func (s *Service) HandleMusicCueSubmissionManifest(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	episodeN, err := strconv.Atoi(c.Query("episode_n"))
	if err != nil || episodeN < 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "episode_n 必须大于 0"})
		return
	}
	manifest, err := s.MusicCues.SubmissionManifest(p.ID, episodeN)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, manifest)
}

func (s *Service) HandleMusicCueCaptionPreview(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	id, err := strconv.ParseUint(c.Param("cid"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid music cue id"})
		return
	}
	preview, err := s.MusicCues.CaptionPreview(p.ID, uint(id))
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, ErrMusicCueSheetNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, preview)
}

func (s *Service) HandleRegenerateMusicCueSheet(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	id, err := strconv.ParseUint(c.Param("cid"), 10, 32)
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid music cue id"})
		return
	}
	cue, err := s.MusicCues.RegenerateCue(p.ID, uint(id))
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, ErrMusicCueSheetNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, cue)
}

func (s *Service) HandleReviewMusicCueSheet(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	id, err := strconv.ParseUint(c.Param("cid"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid music cue sheet id"})
		return
	}
	var body struct {
		Action string `json:"action"`
		Reason string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || (body.Action != "approve" && body.Action != "reject") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "action 必须是 approve 或 reject"})
		return
	}
	cue, err := s.MusicCues.Review(p.ID, uint(id), body.Action == "approve", body.Reason)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, ErrMusicCueSheetNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, cue)
}

func (s *Service) HandleMusicCuePreflight(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	episodeN, err := strconv.Atoi(c.Query("episode_n"))
	if err != nil || episodeN < 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "episode_n 必须大于 0"})
		return
	}
	result, err := s.MusicCues.Preflight(p.ID, episodeN)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (s *Service) HandleBatchApproveMusicCueSheets(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	var body struct {
		EpisodeN     int    `json:"episode_n"`
		ReviewReason string `json:"review_reason"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.EpisodeN <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "episode_n 必须大于 0"})
		return
	}
	if err := s.MusicCues.MarkApproved(p.ID, body.EpisodeN, body.ReviewReason); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
