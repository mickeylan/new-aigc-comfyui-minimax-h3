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
		cue.SourceVersionHash = musicCueSourceHash(context, cue.UserRequirements)
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
	if context, contextErr := s.episodeContext(projectID, cue.EpisodeN); contextErr == nil {
		cue.SourceVersionHash = musicCueSourceHash(context, cue.UserRequirements)
	}
	updates := musicCueSheetUpdates(cue)
	updates["reviewed_at"] = nil
	updates["stale"] = false
	updates["stale_reason"] = ""
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
		if err := s.validateCueTimeline(&cue); err != nil {
			return fmt.Errorf("Cue #%d: %w", cue.ID, err)
		}
		if err := validateMusicCaptionJSON(cue.CaptionJSON); err != nil {
			return fmt.Errorf("Cue #%d: %w", cue.ID, err)
		}
		if cue.SourceVersionHash == "" || cue.SourceVersionHash != musicCueSourceHash(context, cue.UserRequirements) {
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

func musicCueSourceHash(context, requirements string) string {
	hash := sha256.Sum256([]byte(context + "\nUSER_REQUIREMENTS:\n" + strings.TrimSpace(requirements)))
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
	for _, scene := range scenes {
		var shots []models.Shot
		if err := s.db.Where("scene_id = ?", scene.ID).Order("order_num, id").Find(&shots).Error; err != nil {
			return "", err
		}
		var dialogues []models.Dialogue
		if err := s.db.Where("scene_id = ?", scene.ID).Order("`order`, id").Find(&dialogues).Error; err != nil {
			return "", err
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
	baseSystem := `You plan short-drama instrumental score cues and rewrite every cue for MiniMax Music 3. Output strict JSON only. Root field: cues. Each cue has kind,start_time,end_time,intensity,mood,instrument_note,user_requirements,caption. caption must have exactly global_metadata,vocal_details,arrangement. Music is fully instrumental: no lead vocal, spoken voice, narration, chanting, choir, humming, whispering, or vocal-like texture. Arrangement must describe section-by-section musical evolution, not merely list instruments. Preserve explicit user requirements and exclusions as highest priority. Keep dialogue space and avoid wall-to-wall music. Do not create audio or claim generation happened.`
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
			input := MusicCueSheetInput{EpisodeN: episodeN, Kind: item.Kind, StartTime: item.StartTime, EndTime: item.EndTime, Intensity: item.Intensity, Mood: item.Mood, InstrumentNote: item.InstrumentNote, UserRequirements: mergeMusicRequirements(item.UserRequirements, requirements), CaptionJSON: string(caption), Source: "ai", Order: index + 1}
			cue, err := normalizeMusicCueSheet(projectID, input)
			if err != nil {
				return fmt.Errorf("第%d条Cue: %w", index+1, err)
			}
			cue.SourceVersionHash = musicCueSourceHash(context, cue.UserRequirements)
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
	baseSystem := `Rewrite exactly one short-drama instrumental Music 3 cue. Output strict JSON with root field cue. Keep start_time and end_time unchanged. cue has kind,start_time,end_time,intensity,mood,instrument_note,user_requirements,caption. caption has exactly global_metadata,vocal_details,arrangement. Fully instrumental; no speech, singing, choir, chanting, humming, whispering or vocal-like textures. Preserve user requirements as highest priority.`
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
	if err == nil {
		_ = s.db.Model(updated).Update("raw_output", raw).Error
	}
	return updated, err
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
		if cue.SourceVersionHash == "" || cue.SourceVersionHash != musicCueSourceHash(context, cue.UserRequirements) {
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
