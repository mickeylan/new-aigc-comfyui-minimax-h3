package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var ErrMusicCueSheetNotFound = errors.New("配乐 cue 不存在")

type MusicCueService struct {
	db       *gorm.DB
	provider TextProvider
	skills   *SkillService
}

func NewMusicCueService(db *gorm.DB, deps ...any) *MusicCueService {
	s := &MusicCueService{db: db}
	for _, dep := range deps {
		switch value := dep.(type) {
		case TextProvider:
			s.provider = value
		case *SkillService:
			s.skills = value
		}
	}
	return s
}

// MusicCueSheetInput mirrors the editable fields of MusicCueSheet for API consumption.
type MusicCueSheetInput struct {
	Scope             string  `json:"scope"`
	ThemeCode         string  `json:"theme_code"`
	Name              string  `json:"name"`
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

const music3OrientalInstrumentalContract = `A Chinese-language screen-music sensibility is mandatory at the level of emotion and musical storytelling. Chinese pop, lyrical Mandopop-style instrumental writing, guofeng pop, East Asian fantasy scoring, classical crossover, rock or jazz inflection, orchestra, piano, acoustic instruments, Chinese instruments, and subtle electronic textures are all allowed when they fit the episode's story environment. Prioritize a memorable lyrical theme, graceful emotional contour, clear dramatic pacing, restrained space under dialogue, and natural development across the episode. Do not impose any fixed scale, fixed mode, or fixed instrument list. Reject only mismatched generic Hollywood-trailer bombast or anonymous Western-fantasy fanfare that ignores the story; do not reject pop, rock, jazz, or electronic elements merely by genre name. The delivered candidate must be instrumental: no audible singing, English vocal, spoken word, narration, chanting, choir, humming, whispering, breath vocal, vocal pad, or vocal-like texture; a deterministic vocal-separation stage removes the Vocals stem after generation.`

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
	scope := strings.ToLower(strings.TrimSpace(in.Scope))
	if scope == "" {
		if in.EpisodeN == 0 {
			scope = "library"
		} else {
			scope = "episode"
		}
	}
	if scope != "library" && scope != "episode" {
		return models.MusicCueSheet{}, fmt.Errorf("scope 必须是 library 或 episode")
	}
	if scope == "episode" && in.EpisodeN <= 0 {
		return models.MusicCueSheet{}, fmt.Errorf("episode_n 必须大于 0")
	}
	if scope == "library" {
		in.EpisodeN = 0
		in.SceneID = nil
		in.ShotID = nil
		if strings.TrimSpace(in.Name) == "" || strings.TrimSpace(in.ThemeCode) == "" {
			return models.MusicCueSheet{}, fmt.Errorf("项目配乐库曲目必须填写 name 和 theme_code")
		}
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
		ProjectID: projectID, Scope: scope, ThemeCode: strings.TrimSpace(in.ThemeCode), Name: strings.TrimSpace(in.Name),
		EpisodeN: in.EpisodeN, SceneID: in.SceneID, ShotID: in.ShotID,
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
	// Listing is read-only. A policy/compiler version change must never hide or
	// mutate an already saved plan during refresh or process restart. Plans become
	// stale only when their authoritative source changes or the user explicitly
	// regenerates/replaces them.
	var cues []models.MusicCueSheet
	q := s.db.Where("project_id = ? AND enabled = ? AND stale = ?", projectID, true, false)
	if episodeN > 0 {
		q = q.Where("episode_n = ?", episodeN)
	}
	err := q.Order("episode_n, start_time, `order`, id").Find(&cues).Error
	return cues, err
}

func musicCueSheetUpdates(cue models.MusicCueSheet) map[string]any {
	return map[string]any{
		"scope": cue.Scope, "theme_code": cue.ThemeCode, "name": cue.Name,
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
	if context, contextErr := s.contextForCue(&cue); contextErr == nil {
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
		if err := markMusicCueAudioLayerStale(tx, projectID, id, "配乐Cue内容已修改，需要重新采用候选"); err != nil {
			return err
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
		if tx.Migrator().HasTable(&models.MusicCandidate{}) {
			if err := tx.Where("project_id = ? AND music_cue_sheet_id = ?", projectID, id).Delete(&models.MusicCandidate{}).Error; err != nil {
				return err
			}
		}
		if err := deleteMusicCueAudioLayer(tx, projectID, id); err != nil {
			return err
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
		for _, cueID := range ids {
			if err := markMusicCueAudioLayerStale(tx, projectID, cueID, reason); err != nil {
				return err
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
	if err := s.db.Where("project_id = ? AND episode_n = ? AND review_status IN ?", projectID, episodeN, []string{"pending", "draft"}).Find(&cues).Error; err != nil {
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
		if err := validateMusicCaptionJSON(cue.CaptionJSON); err != nil {
			return fmt.Errorf("Cue #%d: %w", cue.ID, err)
		}
		if cue.SourceVersionHash != "" && cue.SourceVersionHash != musicCueSourceHash(context, cue.UserRequirements) {
			return fmt.Errorf("Cue #%d 的剧本或用户要求已变化", cue.ID)
		}
	}
	now := time.Now()
	return s.db.Model(&models.MusicCueSheet{}).Where("project_id = ? AND episode_n = ? AND review_status IN ?", projectID, episodeN, []string{"pending", "draft"}).Updates(map[string]any{"review_status": "approved", "review_reason": strings.TrimSpace(reviewReason), "reviewed_at": now}).Error
}

type musicStructuredCaption struct {
	GlobalMetadata string `json:"global_metadata"`
	VocalDetails   string `json:"vocal_details"`
	Arrangement    string `json:"arrangement"`
	LyricsInput    string `json:"lyrics_input"`
}

func musicCaptionValueText(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := musicCaptionValueText(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "; ")
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			if text := musicCaptionValueText(typed[key]); text != "" {
				parts = append(parts, strings.TrimSpace(key)+": "+text)
			}
		}
		return strings.Join(parts, "; ")
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(typed)
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func (caption *musicStructuredCaption) UnmarshalJSON(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	caption.GlobalMetadata = musicCaptionValueText(raw["global_metadata"])
	caption.VocalDetails = musicCaptionValueText(raw["vocal_details"])
	caption.Arrangement = musicCaptionValueText(raw["arrangement"])
	caption.LyricsInput = musicCaptionValueText(raw["lyrics_input"])
	return nil
}

type generatedMusicCue struct {
	Name             string                 `json:"name"`
	ThemeCode        string                 `json:"theme_code"`
	Duration         float64                `json:"duration"`
	Tags             string                 `json:"tags"`
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

func musicCaptionHasChineseScreenIdentity(caption musicStructuredCaption) bool {
	text := strings.ToLower(caption.GlobalMetadata + "\n" + caption.Arrangement)
	markers := []string{"chinese screen", "chinese-language", "mandopop", "guofeng", "east asian fantasy", "wuxia", "xianxia", "华语", "国风", "中国影视", "东方幻想", "武侠", "仙侠", "古风", "都市华语"}
	for _, marker := range markers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func musicCaptionDirectionCount(caption musicStructuredCaption) int {
	text := strings.ToLower(caption.GlobalMetadata + "\n" + caption.Arrangement)
	groups := [][]string{
		{"mandopop", "华语流行", "华语影视抒情"},
		{"guofeng pop", "古风流行", "国风流行"},
		{"east asian fantasy", "东方幻想", "仙侠影视"},
		{"modern chinese urban", "现代都市华语", "都市华语"},
		{"wuxia action", "武侠动作"},
		{"chinese classical crossover", "中国古典跨界", "古典跨界"},
	}
	count := 0
	for _, group := range groups {
		for _, marker := range group {
			if strings.Contains(text, marker) {
				count++
				break
			}
		}
	}
	return count
}

var musicCaptionClauseSeparator = regexp.MustCompile(`[。；;.!?！？\n]+`)

func musicCaptionClauseIsNegated(clause string) bool {
	clause = strings.ToLower(strings.TrimSpace(clause))
	negations := []string{"no ", "not ", "without ", "avoid ", "exclude ", "excluding ", "prohibit", "forbid", "never ", "禁止", "不要", "避免", "排除", "拒绝", "不使用", "不采用", "不得", "不能", "无任何"}
	for _, negation := range negations {
		if strings.Contains(clause, negation) {
			return true
		}
	}
	return false
}

func musicCaptionHasAffirmativeMarker(text string, markers []string) bool {
	for _, clause := range musicCaptionClauseSeparator.Split(strings.ToLower(text), -1) {
		if musicCaptionClauseIsNegated(clause) {
			continue
		}
		for _, marker := range markers {
			if strings.Contains(clause, marker) {
				return true
			}
		}
	}
	return false
}

func musicCaptionHasForbiddenWesternStyle(caption musicStructuredCaption) bool {
	text := caption.GlobalMetadata + "\n" + caption.Arrangement
	markers := []string{"hollywood trailer", "trailer music", "heroic brass ostinato", "western fantasy fanfare", "generic western fantasy"}
	return musicCaptionHasAffirmativeMarker(text, markers)
}

func musicCaptionHasVocalContent(caption musicStructuredCaption) bool {
	if strings.TrimSpace(caption.LyricsInput) != "" {
		return true
	}
	text := caption.GlobalMetadata + "\n" + caption.Arrangement
	positiveVocal := []string{"lead vocal", "singer", "singing", "sung", "choir", "chanting", "humming", "whisper vocal", "spoken word", "english lyrics", "vocal melody"}
	return musicCaptionHasAffirmativeMarker(text, positiveVocal)
}

func validateMusicCaptionJSON(raw string) error {
	var caption musicStructuredCaption
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &caption); err != nil {
		return fmt.Errorf("Structured Caption JSON 无效: %w", err)
	}
	if strings.TrimSpace(caption.GlobalMetadata) == "" || strings.TrimSpace(caption.VocalDetails) == "" || strings.TrimSpace(caption.Arrangement) == "" {
		return fmt.Errorf("Structured Caption 必须包含 global_metadata、vocal_details 和 arrangement")
	}
	if !strings.Contains(strings.ToLower(caption.VocalDetails), "instrumental") {
		return fmt.Errorf("短剧配乐必须在 vocal_details 中明确为纯器乐")
	}
	if !musicCaptionHasChineseScreenIdentity(caption) {
		return fmt.Errorf("整集配乐必须明确一个符合故事环境的华语屏幕音乐方向")
	}
	if musicCaptionDirectionCount(caption) > 1 {
		return fmt.Errorf("整集配乐只能选择一个主音乐方向，禁止把华语流行、古风流行、东方幻想、都市或武侠等方向混成折中风格")
	}
	if musicCaptionHasForbiddenWesternStyle(caption) {
		return fmt.Errorf("整集配乐包含与故事环境不符的好莱坞预告片或泛西方奇幻套路")
	}
	if musicCaptionHasVocalContent(caption) {
		return fmt.Errorf("整集配乐的lyrics_input必须为空，Caption不得肯定要求人声、演唱、合唱、吟唱、哼唱或说话内容")
	}
	return nil
}

func musicCueSourceHash(context, requirements string) string {
	hash := sha256.Sum256([]byte("music3-calibrated-sections-duration-v10\n" + context + "\nUSER_REQUIREMENTS:\n" + strings.TrimSpace(requirements)))
	return "music3-calibrated-sections-duration-v10:" + hex.EncodeToString(hash[:])
}

func trimJSONEnvelope(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)
	start, end := strings.Index(raw, "{"), strings.LastIndex(raw, "}")
	if start >= 0 && end > start {
		return strings.TrimSpace(raw[start : end+1])
	}
	return raw
}

func parseGeneratedMusicCueSheet(raw string) (*generatedMusicCueSheet, error) {
	data := escapeJSONControlCharsInStrings([]byte(trimJSONEnvelope(raw)))
	var generated generatedMusicCueSheet
	if err := json.Unmarshal(data, &generated); err != nil {
		return nil, err
	}
	return &generated, nil
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

func (s *MusicCueService) projectMusicContext(projectID uint) (string, error) {
	var project models.Project
	if err := s.db.First(&project, projectID).Error; err != nil {
		return "", err
	}
	var scenes []models.Scene
	if err := s.db.Where("project_id = ?", projectID).Order("episode_n, `order`, id").Limit(120).Find(&scenes).Error; err != nil {
		return "", err
	}
	type sceneBrief struct {
		EpisodeN int    `json:"episode_n"`
		Title    string `json:"title"`
		Content  string `json:"content"`
	}
	briefs := make([]sceneBrief, 0, len(scenes))
	for _, scene := range scenes {
		content := []rune(strings.TrimSpace(scene.Content))
		if len(content) > 300 {
			content = content[:300]
		}
		briefs = append(briefs, sceneBrief{EpisodeN: scene.EpisodeN, Title: scene.Title, Content: string(content)})
	}
	encoded, err := json.Marshal(map[string]any{
		"project_title": project.Title, "genre": project.Genre, "style": project.Style,
		"synopsis": project.Synopsis, "planned_episodes": project.Episodes, "representative_scenes": briefs,
	})
	return string(encoded), err
}

func (s *MusicCueService) contextForCue(cue *models.MusicCueSheet) (string, error) {
	if cue.Scope == "library" || cue.EpisodeN == 0 {
		return s.projectMusicContext(cue.ProjectID)
	}
	return s.episodeContext(cue.ProjectID, cue.EpisodeN)
}

// RewriteCaption previews a single Cue caption. It intentionally performs no persistence.
func (s *MusicCueService) RewriteCaption(projectID, cueID uint, requirements, currentCaption string) (string, error) {
	if s.provider == nil || s.skills == nil {
		return "", fmt.Errorf("配乐提示词生成服务未配置")
	}
	var cue models.MusicCueSheet
	if err := s.db.Where("id = ? AND project_id = ?", cueID, projectID).First(&cue).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", ErrMusicCueSheetNotFound
		}
		return "", err
	}
	context, err := s.contextForCue(&cue)
	if err != nil {
		return "", err
	}
	system := `Rewrite exactly one existing MiniMax Music 3 structured caption. Return one strict JSON object with only global_metadata, vocal_details, arrangement, lyrics_input. Preserve the Cue's scope, timing, ownership and dramatic function; do not plan or alter other cues. The saved original requirements remain authoritative, and the current request refines them. The current unsaved caption is the editing source. Write natural English and preserve explicit exclusions. ` + music3OrientalInstrumentalContract + ` Follow the music-caption-rewriter three-section contract. lyrics_input must be an empty string. Do not create audio or claim persistence.`
	if strings.TrimSpace(currentCaption) == "" {
		currentCaption = cue.CaptionJSON
	}
	cueContext := fmt.Sprintf("Cue %d; scope=%s; episode=%d; kind=%s; episode-relative range=%.2f–%.2f seconds; mood=%s; intensity=%s", cue.ID, cue.Scope, cue.EpisodeN, cue.Kind, cue.StartTime, cue.EndTime, cue.Mood, cue.Intensity)
	user := "Authoritative project context JSON:\n" + context + "\n\nTarget cue (do not change):\n" + cueContext + "\n\nSaved original user requirements:\n" + cue.UserRequirements + "\n\nCurrent rewrite requirements:\n" + strings.TrimSpace(requirements) + "\n\nCurrent unsaved caption JSON:\n" + strings.TrimSpace(currentCaption)
	raw, err := s.skills.ChatWithConfiguredOrFallbackSkill(projectID, models.SkillStageMusicCueSheet, "music-cue-sheet-generate", s.provider, system, user, map[string]string{"episode_context": context, "user_requirements": cue.UserRequirements + "\n" + requirements})
	if err != nil {
		return "", err
	}
	var caption musicStructuredCaption
	if err := json.Unmarshal(escapeJSONControlCharsInStrings([]byte(trimJSONEnvelope(raw))), &caption); err != nil {
		return "", fmt.Errorf("AI改写Caption JSON解析失败: %w", err)
	}
	caption.LyricsInput = ""
	encoded, err := json.Marshal(caption)
	if err != nil {
		return "", err
	}
	if err := validateMusicCaptionJSON(string(encoded)); err != nil {
		return "", err
	}
	return string(encoded), nil
}

func validateGeneratedMusicCueCoverage(cues []generatedMusicCue, total float64) error {
	if total <= 0 {
		return fmt.Errorf("Episode总时长无效")
	}
	if total > maxMusic3SegmentSeconds && len(cues) != 2 {
		return fmt.Errorf("%.1f秒Episode必须拆成恰好两段Music 3 Cue", total)
	}
	if total <= maxMusic3SegmentSeconds && len(cues) != 1 {
		return fmt.Errorf("%.1f秒Episode应生成一条整集Music 3 Cue", total)
	}
	cursor := 0.0
	for i, cue := range cues {
		if math.Abs(cue.StartTime-cursor) > 0.05 {
			return fmt.Errorf("第%d段起点%.2f未连续承接%.2f", i+1, cue.StartTime, cursor)
		}
		duration := cue.EndTime - cue.StartTime
		if duration < 5 || duration > maxMusic3SegmentSeconds {
			return fmt.Errorf("第%d段时长%.2f秒超出5–300秒", i+1, duration)
		}
		cursor = cue.EndTime
	}
	if math.Abs(cursor-total) > 0.05 {
		return fmt.Errorf("Music 3分段只覆盖到%.2f秒，Episode为%.2f秒", cursor, total)
	}
	return nil
}

func (s *MusicCueService) DraftEpisode(projectID uint, episodeN int, requirements string) ([]models.MusicCueSheet, error) {
	if s.provider == nil || s.skills == nil {
		return nil, fmt.Errorf("配乐提示词生成服务未配置")
	}
	context, err := s.episodeContext(projectID, episodeN)
	if err != nil {
		return nil, err
	}
	baseSystem := `You plan one continuous whole-episode instrumental score and rewrite it for MiniMax Music 3. Output strict JSON only. Root field: cues. Each cue has kind,start_time,end_time,intensity,mood,instrument_note,user_requirements,caption. caption must have global_metadata,vocal_details,arrangement,lyrics_input. Cover the complete Episode timeline contiguously from 0 to total_duration. If total_duration is at most 300 seconds, return exactly one cue. If it exceeds 300 seconds, return exactly two contiguous cues split near a semantic turning point; each cue must be 5–300 seconds. Follow the bundled MiniMax Music 3 music-caption-rewriter contract. First choose exactly one primary family that best fits the Episode: lyrical Mandopop-style instrumental, guofeng pop instrumental, East Asian fantasy screen score, modern Chinese urban score, wuxia action score, or Chinese classical crossover. Do not blend multiple directions into an ambiguous hybrid. Write all caption fields in natural English, approximately 250–450 English words total. Do not output database-like key:value dumps, scene numbers, character names, plot summaries, sword moves, or second-by-second timestamps. Arrangement must use musical sections such as Intro, Development, Climax and Outro and describe concrete instrument entrances, exits, groove, texture and transitions. Both Episode segments must share that one direction, the same musical theme, overall timbral identity, BPM range and production identity, while the second segment develops rather than restarts the theme. instrument_note must remain empty unless the current user requirements explicitly name an instrument; never invent a core instrumentation mandate. ` + music3OrientalInstrumentalContract + ` lyrics_input must be the empty string. Do not output section tags or lyric words. Arrangement must describe section-by-section musical evolution and leave restrained dynamic space under dialogue. Preserve explicit user requirements and exclusions as highest priority. Do not imitate or name a copyrighted soundtrack or composer; express only high-level musical qualities. Do not create audio or claim generation happened.`
	baseUser := "Authoritative episode timeline JSON:\n" + context + "\n\nUser music ideas and requirements:\n" + strings.TrimSpace(requirements)
	raw, err := s.skills.ChatWithConfiguredOrFallbackSkill(projectID, models.SkillStageMusicCueSheet, "music-cue-sheet-generate", s.provider, baseSystem, baseUser, map[string]string{"episode_context": context, "user_requirements": requirements})
	if err != nil {
		return nil, err
	}
	generated, parseErr := parseGeneratedMusicCueSheet(raw)
	if parseErr != nil {
		repairSystem := baseSystem + ` The previous response was malformed. Repair syntax and schema only. Preserve every cue's meaning, times, musical identity and exclusions. Return one strict JSON object and nothing else.`
		repairUser := "Exact JSON parse error:\n" + parseErr.Error() + "\n\nMalformed response to repair:\n" + raw
		repaired, repairErr := s.skills.ChatWithConfiguredOrFallbackSkill(projectID, models.SkillStageMusicCueSheet, "music-cue-sheet-generate", s.provider, repairSystem, repairUser, map[string]string{"episode_context": context, "user_requirements": requirements})
		if repairErr != nil {
			return nil, fmt.Errorf("AI配乐方案JSON解析失败: %v；自动修复失败: %w", parseErr, repairErr)
		}
		generated, parseErr = parseGeneratedMusicCueSheet(repaired)
		if parseErr != nil {
			return nil, fmt.Errorf("AI配乐方案自动修复后仍无法解析: %w", parseErr)
		}
		raw = repaired
	}
	if len(generated.Cues) == 0 {
		return nil, fmt.Errorf("AI未返回任何配乐Cue")
	}
	var contextMeta struct {
		TotalDuration float64 `json:"total_duration"`
	}
	if err := json.Unmarshal([]byte(context), &contextMeta); err != nil {
		return nil, fmt.Errorf("读取Episode总时长失败: %w", err)
	}
	if err := validateGeneratedMusicCueCoverage(generated.Cues, contextMeta.TotalDuration); err != nil {
		return nil, err
	}
	rows := make([]models.MusicCueSheet, 0, len(generated.Cues))
	err = s.db.Transaction(func(tx *gorm.DB) error {
		var replacedIDs []uint
		if err := tx.Model(&models.MusicCueSheet{}).Where("project_id = ? AND episode_n = ? AND source = ? AND enabled = ?", projectID, episodeN, "ai", true).Pluck("id", &replacedIDs).Error; err != nil {
			return err
		}
		if len(replacedIDs) > 0 && tx.Migrator().HasTable(&models.MusicCandidate{}) {
			if err := tx.Model(&models.MusicCandidate{}).Where("project_id = ? AND music_cue_sheet_id IN ? AND stale = ?", projectID, replacedIDs, false).
				Updates(map[string]any{"stale": true, "stale_reason": "用户重新生成了AI配乐方案", "is_current": false}).Error; err != nil {
				return err
			}
		}
		if len(replacedIDs) > 0 {
			if err := tx.Model(&models.MusicCueSheet{}).Where("project_id = ? AND id IN ?", projectID, replacedIDs).
				Updates(map[string]any{"enabled": false, "stale": true, "stale_reason": "已被用户重新生成的AI配乐方案替换"}).Error; err != nil {
				return err
			}
		}
		for index, item := range generated.Cues {
			// The model may return song section tags despite the instrumental contract.
			// Empty lyrics deterministically before validation and persistence so the exact
			// reviewed Cue matches the no-lyrics execution request.
			item.Caption.LyricsInput = ""
			caption, _ := json.Marshal(item.Caption)
			if err := validateMusicCaptionJSON(string(caption)); err != nil {
				return fmt.Errorf("第%d条Cue: %w", index+1, err)
			}
			input := MusicCueSheetInput{EpisodeN: episodeN, Kind: item.Kind, StartTime: item.StartTime, EndTime: item.EndTime, Intensity: item.Intensity, Mood: item.Mood, InstrumentNote: "", UserRequirements: strings.TrimSpace(requirements), CaptionJSON: string(caption), Source: "ai", Order: index + 1}
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

func (s *MusicCueService) ListLibrary(projectID uint) ([]models.MusicCueSheet, error) {
	var rows []models.MusicCueSheet
	err := s.db.Where("project_id = ? AND scope = ? AND enabled = ? AND stale = ?", projectID, "library", true, false).
		Order("`order`, id").Find(&rows).Error
	return rows, err
}

func (s *MusicCueService) DraftProjectLibrary(projectID uint, requirements string) ([]models.MusicCueSheet, error) {
	if s.provider == nil || s.skills == nil {
		return nil, fmt.Errorf("配乐提示词生成服务未配置")
	}
	context, err := s.projectMusicContext(projectID)
	if err != nil {
		return nil, err
	}
	system := `Plan a reusable project-level score library for one complete drama. Output strict JSON with root field cues. Return 5 to 8 distinct reusable themes, not an episode timeline. Each cue must contain name,theme_code,duration,tags,kind,intensity,mood,instrument_note,user_requirements,caption. theme_code must be a stable lowercase snake_case identifier. duration must be 45 to 180 seconds. kind must be bgm, action, emotion, opening, ending or transition. Cover the project's recurring dramatic functions such as protagonist identity, relationships, mystery, environment, danger, action, warmth or comedy only when supported by the project context. Avoid redundant themes. Each caption has global_metadata,vocal_details,arrangement,lyrics_input and follows the bundled MiniMax Music 3 music-caption-rewriter contract: natural English, one coherent primary music family, concrete musical sections and instrument lifecycles, no plot retelling, scene timestamps, character names or database-style key:value dumps. Each track is instrumental; lyrics_input is empty. Tracks must share a coherent overall sonic identity while remaining clearly distinguishable by dramatic function. Do not imitate copyrighted works or named composers.`
	user := "Authoritative project context JSON:\n" + context + "\n\nUser requirements:\n" + strings.TrimSpace(requirements)
	raw, err := s.skills.ChatWithConfiguredOrFallbackSkill(projectID, models.SkillStageMusicCueSheet, "music-cue-sheet-generate", s.provider, system, user, map[string]string{"episode_context": context, "user_requirements": requirements})
	if err != nil {
		return nil, err
	}
	generated, err := parseGeneratedMusicCueSheet(raw)
	if err != nil {
		return nil, fmt.Errorf("AI项目配乐库JSON解析失败: %w", err)
	}
	if len(generated.Cues) < 5 || len(generated.Cues) > 8 {
		return nil, fmt.Errorf("项目配乐库必须包含5–8个可复用主题，AI返回%d个", len(generated.Cues))
	}
	seen := map[string]bool{}
	rows := make([]models.MusicCueSheet, 0, len(generated.Cues))
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.MusicCueSheet{}).Where("project_id = ? AND scope = ? AND source = ? AND enabled = ?", projectID, "library", "ai", true).
			Updates(map[string]any{"enabled": false, "stale": true, "stale_reason": "已被用户重新生成的项目配乐库替换"}).Error; err != nil {
			return err
		}
		for index, item := range generated.Cues {
			code := strings.ToLower(strings.TrimSpace(item.ThemeCode))
			if !regexp.MustCompile(`^[a-z][a-z0-9_]{2,63}$`).MatchString(code) || seen[code] {
				return fmt.Errorf("第%d条曲目theme_code无效或重复", index+1)
			}
			seen[code] = true
			if item.Duration < 45 || item.Duration > 180 {
				return fmt.Errorf("第%d条曲目duration必须为45–180秒", index+1)
			}
			item.Caption.LyricsInput = ""
			caption, _ := json.Marshal(item.Caption)
			if err := validateMusicCaptionJSON(string(caption)); err != nil {
				return fmt.Errorf("第%d条曲目: %w", index+1, err)
			}
			input := MusicCueSheetInput{Scope: "library", ThemeCode: code, Name: strings.TrimSpace(item.Name), EpisodeN: 0,
				Kind: item.Kind, StartTime: 0, EndTime: item.Duration, Duration: item.Duration,
				Intensity: item.Intensity, Mood: item.Mood, InstrumentNote: item.InstrumentNote,
				UserRequirements: strings.TrimSpace(requirements), Tags: item.Tags, CaptionJSON: string(caption), Source: "ai", Order: index + 1}
			cue, err := normalizeMusicCueSheet(projectID, input)
			if err != nil {
				return fmt.Errorf("第%d条曲目: %w", index+1, err)
			}
			cue.SourceVersionHash = musicCueSourceHash(context, requirements)
			cue.RawOutput, cue.ReviewStatus = raw, "draft"
			if err := tx.Create(&cue).Error; err != nil {
				return err
			}
			rows = append(rows, cue)
		}
		return nil
	})
	return rows, err
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
		if err := validateMusicCaptionJSON(cue.CaptionJSON); err != nil {
			return nil, err
		}
		context, err := s.contextForCue(&cue)
		if err != nil {
			return nil, err
		}
		if cue.SourceVersionHash != "" && cue.SourceVersionHash != musicCueSourceHash(context, cue.UserRequirements) {
			return nil, fmt.Errorf("剧本或用户要求已变化，请重新生成配乐提示词")
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

func (s *Service) HandleRewriteMusicCueSheet(c *gin.Context) {
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
		Requirements string `json:"requirements"`
		CaptionJSON  string `json:"caption_json"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	caption, err := s.MusicCues.RewriteCaption(p.ID, uint(id), body.Requirements, body.CaptionJSON)
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, ErrMusicCueSheetNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"caption_json": caption})
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

func (s *Service) HandleListMusicLibrary(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	rows, err := s.MusicCues.ListLibrary(p.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, rows)
}

func (s *Service) HandleDraftMusicLibrary(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	var body struct {
		Requirements string `json:"requirements"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	rows, err := s.MusicCues.DraftProjectLibrary(p.ID, body.Requirements)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"tracks": rows})
}

func (s *Service) HandlePlaceMusicLibraryTrack(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	cueID, err := strconv.ParseUint(c.Param("cid"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid library track id"})
		return
	}
	var body struct {
		EpisodeN    int     `json:"episode_n"`
		StartTime   float64 `json:"start_time"`
		EndTime     float64 `json:"end_time"`
		Volume      float64 `json:"volume"`
		SourceStart float64 `json:"source_start"`
		FadeIn      float64 `json:"fade_in"`
		FadeOut     float64 `json:"fade_out"`
		Loop        bool    `json:"loop"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	var layer *models.AudioLayer
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		var placeErr error
		layer, placeErr = placeMusicLibraryTrack(tx, p.ID, uint(cueID), body.EpisodeN, body.StartTime, body.EndTime, body.SourceStart, body.Volume, body.FadeIn, body.FadeOut, body.Loop)
		return placeErr
	})
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, layer)
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
