package service

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

const (
	BatchStatusDraft           = "draft"
	BatchStatusGenerating      = "generating"
	BatchStatusReview          = "review"
	BatchStatusApproved        = "approved"
	BatchStatusProduced        = "produced"
	BatchStatusCancelled       = "cancelled"
	ReviewStatusPending        = "pending"
	ReviewStatusApproved       = "approved"
	ReviewStatusRejected       = "rejected"
	BatchEpisodeStatusDraft    = "draft"
	BatchEpisodeStatusApproved = "approved"
	BatchEpisodeStatusProduced = "produced"
	DefaultBatchSizeMin        = 5
	DefaultBatchSizeMax        = 20
)

type BatchPlanningService struct {
	db       *gorm.DB
	provider TextProvider
	skills   *SkillService
}

func NewBatchPlanningService(db *gorm.DB, deps ...any) *BatchPlanningService {
	s := &BatchPlanningService{db: db}
	for _, dep := range deps {
		switch v := dep.(type) {
		case TextProvider:
			s.provider = v
		case *SkillService:
			s.skills = v
		}
	}
	return s
}

type BatchCreateInput struct {
	Title        string `json:"title"`
	EpisodeStart int    `json:"episode_start"`
	EpisodeEnd   int    `json:"episode_end"`
	ChapterStart int    `json:"chapter_start"`
	ChapterEnd   int    `json:"chapter_end"`
	ArcRange     string `json:"arc_range"`
}

type BatchGenerationParams struct {
	BatchID, Generation, AdaptationID uint
	SkillStage                        string
	SkillID                           *uint
	SkillVersion                      int
}
type BatchProgress struct {
	BatchID                                                              uint `json:"batch_id"`
	TotalEpisodes, GeneratedEpisodes, ReviewedEpisodes, ProducedEpisodes int
	Progress                                                             float64 `json:"progress"`
}
type PlanningBatchDetail struct {
	Batch    models.PlanningBatch       `json:"batch"`
	Episodes []models.EpisodeAdaptation `json:"episodes"`
	Arcs     []models.StoryArc          `json:"arcs"`
}

func (s *BatchPlanningService) CreateBatch(projectID uint, in BatchCreateInput) (*models.PlanningBatch, error) {
	if strings.TrimSpace(in.Title) == "" {
		return nil, fmt.Errorf("批次标题不能为空")
	}
	if in.EpisodeStart < 1 || in.EpisodeEnd < in.EpisodeStart {
		return nil, fmt.Errorf("集数范围无效")
	}
	count := in.EpisodeEnd - in.EpisodeStart + 1
	if count < DefaultBatchSizeMin || count > DefaultBatchSizeMax {
		return nil, fmt.Errorf("单次规划必须为 %d–%d 集", DefaultBatchSizeMin, DefaultBatchSizeMax)
	}
	if in.ChapterStart == 0 && in.ChapterEnd == 0 {
		in.ChapterStart, in.ChapterEnd = 1, 1
	}
	if in.ChapterStart < 1 || in.ChapterEnd < in.ChapterStart {
		return nil, fmt.Errorf("章节范围无效")
	}
	var project models.Project
	if err := s.db.First(&project, projectID).Error; err != nil {
		return nil, err
	}
	if project.Episodes > 0 && in.EpisodeEnd > project.Episodes {
		return nil, fmt.Errorf("批次结束集 %d 超过全剧预计 %d 集", in.EpisodeEnd, project.Episodes)
	}
	var overlaps int64
	if err := s.db.Model(&models.PlanningBatch{}).Where("project_id = ? AND status <> ? AND episode_start <= ? AND episode_end >= ?", projectID, BatchStatusCancelled, in.EpisodeEnd, in.EpisodeStart).Count(&overlaps).Error; err != nil {
		return nil, err
	}
	if overlaps > 0 {
		return nil, fmt.Errorf("该集数范围与现有批次重叠")
	}
	var last models.PlanningBatch
	var previous *uint
	batchNo := 1
	if err := s.db.Where("project_id = ?", projectID).Order("batch_no DESC").First(&last).Error; err == nil {
		if last.ReviewStatus != ReviewStatusApproved && last.Status != BatchStatusProduced {
			return nil, fmt.Errorf("请先审核上一批次")
		}
		batchNo, previous = last.BatchNo+1, &last.ID
	} else if err != gorm.ErrRecordNotFound {
		return nil, err
	}
	batch := &models.PlanningBatch{ProjectID: projectID, BatchNo: batchNo, Title: strings.TrimSpace(in.Title), EpisodeStart: in.EpisodeStart, EpisodeEnd: in.EpisodeEnd, EpisodeCount: count, TotalEpisodes: count, ChapterStart: in.ChapterStart, ChapterEnd: in.ChapterEnd, ArcRange: strings.TrimSpace(in.ArcRange), Status: BatchStatusDraft, ReviewStatus: ReviewStatusPending, PreviousBatchID: previous, Version: 1}
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(batch).Error; err != nil {
			return err
		}
		// Automatically bind the production batch to the approved analysis window
		// that owns its chapter range. Outline projects and legacy flows remain unbound.
		var window models.AnalysisWindow
		if err := tx.Where("project_id = ? AND chapter_start <= ? AND chapter_end >= ? AND status IN ?", projectID, in.ChapterStart, in.ChapterEnd, []models.AnalysisWindowStatus{models.WindowStatusReady, models.WindowStatusApproved}).Order("window_no DESC").First(&window).Error; err == nil {
			if window.PlanningBatchID != nil && *window.PlanningBatchID != batch.ID {
				return fmt.Errorf("analysis window is already bound to another production batch")
			}
			if err := tx.Model(&window).Update("planning_batch_id", batch.ID).Error; err != nil {
				return err
			}
		} else if err != gorm.ErrRecordNotFound {
			return err
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return batch, nil
}

// RecoverInterruptedBatches resets in-process AI generation left behind by a
// service restart. There is no persistent worker that can resume these calls.
func (s *BatchPlanningService) RecoverInterruptedBatches() error {
	return s.db.Model(&models.PlanningBatch{}).Where("status = ?", BatchStatusGenerating).Updates(map[string]any{
		"status": BatchStatusDraft,
		"error":  "方案生成因服务重启中断，请重新点击生成",
	}).Error
}

func (s *BatchPlanningService) ListBatches(projectID uint) ([]models.PlanningBatch, error) {
	var rows []models.PlanningBatch
	err := s.db.Where("project_id = ?", projectID).Order("batch_no").Find(&rows).Error
	return rows, err
}
func (s *BatchPlanningService) GetBatch(projectID, batchID uint) (*models.PlanningBatch, error) {
	var v models.PlanningBatch
	q := s.db.Where("id = ?", batchID)
	if projectID > 0 {
		q = q.Where("project_id = ?", projectID)
	}
	if err := q.First(&v).Error; err != nil {
		return nil, err
	}
	return &v, nil
}
func (s *BatchPlanningService) GetActiveBatch(projectID uint) (*models.PlanningBatch, error) {
	var v models.PlanningBatch
	err := s.db.Where("project_id = ? AND status NOT IN ?", projectID, []string{BatchStatusApproved, BatchStatusProduced, BatchStatusCancelled}).Order("batch_no DESC").First(&v).Error
	return &v, err
}
func (s *BatchPlanningService) CanCreateNextBatch(projectID uint) (bool, string, error) {
	var v models.PlanningBatch
	err := s.db.Where("project_id = ?", projectID).Order("batch_no DESC").First(&v).Error
	if err == gorm.ErrRecordNotFound {
		return true, "", nil
	}
	if err != nil {
		return false, "", err
	}
	if v.ReviewStatus != ReviewStatusApproved && v.Status != BatchStatusProduced {
		return false, "上一批次尚未审核通过", nil
	}
	return true, "", nil
}

func parseArcNumbers(raw string) []int {
	var out []int
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == '-' || r == '、' || r == ' ' }) {
		if n, e := strconv.Atoi(part); e == nil && n > 0 {
			out = append(out, n)
		}
	}
	return out
}

func constrainEpisodeChapterRange(ep *models.EpisodeAdaptation, batch *models.PlanningBatch) {
	// Models sometimes confuse episode numbers with chapter numbers (for example,
	// episode 9 -> chapter 9 in a batch that only covers chapters 1-8). Keep the
	// narrative mapping but clamp provenance to the user-selected source window.
	if ep.ChapterStart < batch.ChapterStart {
		ep.ChapterStart = batch.ChapterStart
	}
	if ep.ChapterStart > batch.ChapterEnd {
		ep.ChapterStart = batch.ChapterEnd
	}
	if ep.ChapterEnd < batch.ChapterStart {
		ep.ChapterEnd = batch.ChapterStart
	}
	if ep.ChapterEnd > batch.ChapterEnd {
		ep.ChapterEnd = batch.ChapterEnd
	}
	if ep.ChapterEnd < ep.ChapterStart {
		ep.ChapterEnd = ep.ChapterStart
	}
}

func fillEpisodeSourceChapterIDs(ep *models.EpisodeAdaptation, chapters []models.Chapter) error {
	if ep.ChapterStart < 1 || ep.ChapterEnd < ep.ChapterStart {
		return fmt.Errorf("invalid chapter range")
	}
	// chapter_start/chapter_end are chapter order numbers, but this field must
	// contain database primary keys. Derive them from project-scoped rows rather
	// than trusting IDs invented by the model.
	ids := make([]uint, 0, ep.ChapterEnd-ep.ChapterStart+1)
	for _, chapter := range chapters {
		if chapter.Order >= ep.ChapterStart && chapter.Order <= ep.ChapterEnd {
			ids = append(ids, chapter.ID)
		}
	}
	expected := ep.ChapterEnd - ep.ChapterStart + 1
	if len(ids) != expected {
		return fmt.Errorf("chapter range %d-%d is not fully covered by ready source chapters", ep.ChapterStart, ep.ChapterEnd)
	}
	raw, _ := json.Marshal(ids)
	ep.SourceChapterIDs = string(raw)
	return nil
}

func normalizeAdaptationJSONWithRepair(provider TextProvider, raw string) (string, string, error) {
	normalized, parseErr := normalizeAdaptationJSON(raw)
	if parseErr == nil {
		return normalized, raw, nil
	}
	repairSystem := `你是严格的JSON语法修复器。只允许修复引号、转义、逗号、冒号和括号等JSON语法错误，不得改写、删减、新增或重新规划任何剧情与分集内容。必须完整保留summary、episodes及每个episode的全部字段和值。只输出一个可被标准JSON.parse解析的JSON对象，不要Markdown、解释或代码围栏。`
	repaired, repairErr := provider.Chat(repairSystem, fmt.Sprintf("解析器错误：%s\n\n待修复原文：\n%s", parseErr.Error(), raw))
	if repairErr != nil {
		return "", raw, fmt.Errorf("批次方案JSON解析失败，自动语法修复调用失败: %v；原始错误: %w", repairErr, parseErr)
	}
	normalized, repairParseErr := normalizeAdaptationJSON(repaired)
	if repairParseErr != nil {
		return "", repaired, fmt.Errorf("批次方案自动修复后仍不是合法JSON: %w", repairParseErr)
	}
	return normalized, repaired, nil
}

func (s *BatchPlanningService) batchCharacterAuthority(projectID uint, previousEpisode int) (map[string][]string, error) {
	result := map[string][]string{}
	var characters []models.Character
	if err := s.db.Where("project_id = ?", projectID).Order("id").Find(&characters).Error; err != nil {
		return nil, err
	}
	for _, character := range characters {
		name := strings.TrimSpace(character.Name)
		if name != "" {
			result[name] = []string{}
		}
	}
	if s.db.Migrator().HasTable(&models.CharacterAliasCandidate{}) {
		var aliases []models.CharacterAliasCandidate
		if err := s.db.Where("project_id = ?", projectID).Find(&aliases).Error; err != nil {
			return nil, err
		}
		for _, alias := range aliases {
			canonical := strings.TrimSpace(alias.CanonicalName)
			value := strings.TrimSpace(alias.Alias)
			if canonical != "" && value != "" {
				result[canonical] = append(result[canonical], value)
			}
		}
	}
	if previousEpisode > 0 {
		var scenes []models.Scene
		if err := s.db.Where("project_id = ? AND episode_n <= ?", projectID, previousEpisode).Find(&scenes).Error; err != nil {
			return nil, err
		}
		for _, scene := range scenes {
			for _, raw := range []string{scene.Characters, scene.VisibleCharacters, scene.VoiceCharacters, scene.MentionedCharacters} {
				for _, name := range strings.FieldsFunc(raw, func(r rune) bool { return strings.ContainsRune(",，、;；\n", r) }) {
					name = strings.TrimSpace(name)
					if name == "" {
						continue
					}
					if _, ok := result[name]; !ok {
						result[name] = []string{}
					}
				}
			}
			var dialogues []models.Dialogue
			if err := s.db.Where("project_id = ? AND scene_id = ?", projectID, scene.ID).Find(&dialogues).Error; err != nil {
				return nil, err
			}
			for _, dialogue := range dialogues {
				name := strings.TrimSpace(dialogue.Character)
				if name != "" && !isNarrationSpeaker(name) {
					if _, ok := result[name]; !ok {
						result[name] = []string{}
					}
				}
			}
		}
	}
	return result, nil
}

func adaptationPlanningText(ep models.EpisodeAdaptation) string {
	return strings.Join([]string{ep.Title, ep.AdaptationGoal, ep.MustKeepEvents, ep.OpeningState, ep.EndingState, ep.Hook}, "\n")
}

func (s *BatchPlanningService) batchPlanningDuplicateIssues(projectID uint, episodeStart int, episodes []models.EpisodeAdaptation) ([]string, error) {
	var issues []string
	episodeNumber := func(index int) int {
		if episodes[index].EpisodeN > 0 {
			return episodes[index].EpisodeN
		}
		return episodeStart + index
	}
	for i := range episodes {
		current := adaptationPlanningText(episodes[i])
		for j := 0; j < i; j++ {
			if score := scriptSceneSimilarity(current, adaptationPlanningText(episodes[j])); score >= 0.30 {
				issues = append(issues, fmt.Sprintf("第%d集与本批第%d集事件重复（相似度%.0f%%）", episodeNumber(i), episodeNumber(j), score*100))
			}
		}
	}
	for i, episode := range episodes {
		currentEpisode := episodeNumber(i)
		if currentEpisode <= 1 {
			continue
		}
		var previousScenes []models.Scene
		if err := s.db.Where("project_id = ? AND episode_n < ?", projectID, currentEpisode).Order("episode_n, `order`, id").Find(&previousScenes).Error; err != nil {
			return nil, err
		}
		current := adaptationPlanningText(episode)
		for _, scene := range previousScenes {
			prior := strings.Join([]string{scene.Title, scene.Content}, "\n")
			if score := scriptSceneSimilarity(current, prior); score >= 0.38 {
				issues = append(issues, fmt.Sprintf("第%d集规划与已生产第%d集Scene %d「%s」事件重复（相似度%.0f%%）", currentEpisode, scene.EpisodeN, scene.Order, scene.Title, score*100))
			}
		}
	}
	return uniqueStrings(issues), nil
}

func (s *BatchPlanningService) lockedBatchEpisodes(projectID uint, start, end int) (map[int]models.EpisodeAdaptation, error) {
	var adaptations []models.EpisodeAdaptation
	if err := s.db.Where("project_id = ? AND episode_n BETWEEN ? AND ?", projectID, start, end).Find(&adaptations).Error; err != nil {
		return nil, err
	}
	locked := map[int]models.EpisodeAdaptation{}
	for _, adaptation := range adaptations {
		var sceneCount int64
		if err := s.db.Model(&models.Scene{}).Where("project_id = ? AND episode_n = ?", projectID, adaptation.EpisodeN).Count(&sceneCount).Error; err != nil {
			return nil, err
		}
		if sceneCount > 0 || adaptation.Status == "scripted" || adaptation.Status == "reviewed" {
			locked[adaptation.EpisodeN] = adaptation
		}
	}
	return locked, nil
}

func (s *BatchPlanningService) GenerateBatchDraft(projectID, batchID uint) (*PlanningBatchDetail, error) {
	detail, err := s.generateBatchDraft(projectID, batchID)
	if err != nil {
		_ = s.db.Model(&models.PlanningBatch{}).Where("id = ? AND project_id = ?", batchID, projectID).Updates(map[string]any{"status": BatchStatusDraft, "error": err.Error()}).Error
	}
	return detail, err
}

func (s *BatchPlanningService) generateBatchDraft(projectID, batchID uint) (*PlanningBatchDetail, error) {
	if s.skills == nil || s.provider == nil {
		return nil, fmt.Errorf("文本生成服务未配置")
	}
	batch, err := s.GetBatch(projectID, batchID)
	if err != nil {
		return nil, err
	}
	if batch.Status != BatchStatusDraft && batch.Status != BatchStatusReview && batch.Status != BatchStatusApproved && batch.Status != BatchStatusProduced {
		return nil, fmt.Errorf("当前批次状态不允许生成")
	}
	var project models.Project
	if err := s.db.First(&project, projectID).Error; err != nil {
		return nil, err
	}
	outlineMode := project.SourceType != models.ProjectSourceNovel
	lockedEpisodes, err := s.lockedBatchEpisodes(projectID, batch.EpisodeStart, batch.EpisodeEnd)
	if err != nil {
		return nil, fmt.Errorf("读取已生产分集锁定状态失败: %w", err)
	}
	var bible models.StoryBible
	if err := s.db.Where("project_id = ? AND status = ?", projectID, "approved").First(&bible).Error; err != nil {
		if !outlineMode {
			return nil, fmt.Errorf("需要先审核通过故事圣经")
		}
		bible = models.StoryBible{ProjectID: projectID, Premise: project.Synopsis, MainPlot: project.Plan, Status: "approved"}
	}
	var chapters []models.Chapter
	if err := s.db.Where("project_id = ? AND chapter_order BETWEEN ? AND ? AND analysis_status = ?", projectID, batch.ChapterStart, batch.ChapterEnd, "ready").Order("chapter_order").Find(&chapters).Error; err != nil {
		return nil, err
	}
	if !outlineMode {
		expected := batch.ChapterEnd - batch.ChapterStart + 1
		if len(chapters) != expected {
			return nil, fmt.Errorf("所选章节尚未全部完成分析：需要%d章，当前仅%d章ready", expected, len(chapters))
		}
		for _, chapter := range chapters {
			if chapter.AnalysisHash == "" || chapter.AnalysisHash != chapter.ContentHash {
				return nil, fmt.Errorf("第%d章分析已过期，请重新分析后再生成批次方案", chapter.Order)
			}
		}
	}
	var arcs []models.StoryArc
	arcNos := parseArcNumbers(batch.ArcRange)
	q := s.db.Where("project_id = ?", projectID)
	if len(arcNos) > 0 {
		q = q.Where("arc_no IN ?", arcNos)
	} else {
		q = q.Where("chapter_start <= ? AND chapter_end >= ?", batch.ChapterEnd, batch.ChapterStart)
	}
	if err := q.Order("arc_no").Find(&arcs).Error; err != nil {
		return nil, err
	}
	if len(arcs) == 0 && !outlineMode {
		return nil, fmt.Errorf("所选章节范围没有故事弧，请先生成并审核故事弧")
	}
	for _, arc := range arcs {
		if arc.ReviewStatus != ReviewStatusApproved {
			return nil, fmt.Errorf("故事弧 %d「%s」尚未审核通过", arc.ArcNo, arc.Title)
		}
	}
	var previous models.EpisodeAdaptation
	_ = s.db.Where("project_id = ? AND episode_n < ?", projectID, batch.EpisodeStart).Order("episode_n DESC").First(&previous).Error
	previousProductionFacts := ""
	if batch.EpisodeStart > 1 {
		previousProductionFacts, err = (&ProjectService{db: s.db}).previousEpisodeProductionFacts(projectID, batch.EpisodeStart)
		if err != nil {
			return nil, fmt.Errorf("读取上一集生产事实失败: %w", err)
		}
	}
	characterAuthority, err := s.batchCharacterAuthority(projectID, batch.EpisodeEnd)
	if err != nil {
		return nil, fmt.Errorf("读取跨集角色权威失败: %w", err)
	}
	lockedProductionFacts := map[int]string{}
	for episodeN := range lockedEpisodes {
		facts, factsErr := (&ProjectService{db: s.db}).previousEpisodeProductionFacts(projectID, episodeN+1)
		if factsErr != nil {
			return nil, fmt.Errorf("读取第%d集生产事实失败: %w", episodeN, factsErr)
		}
		lockedProductionFacts[episodeN] = facts
	}
	var previousSnapshot models.BatchStateSnapshot
	if batch.PreviousBatchID != nil {
		_ = s.db.Where("project_id = ? AND batch_id = ? AND status = ?", projectID, *batch.PreviousBatchID, "approved").First(&previousSnapshot).Error
	}
	var openClues []models.StoryClue
	_ = s.db.Where("project_id = ? AND status IN ?", projectID, []string{"open", "developing"}).Order("clue_key").Find(&openClues).Error
	chapterIDByOrder := map[int]uint{}
	for _, chapter := range chapters {
		chapterIDByOrder[chapter.Order] = chapter.ID
	}
	input := map[string]any{"batch": batch, "story_bible": bible, "story_arcs": arcs, "chapter_analyses": chapters, "chapter_id_by_order": chapterIDByOrder, "previous_ending_state": previous.EndingState, "previous_production_facts": previousProductionFacts, "locked_episodes": lockedEpisodes, "locked_episode_production_facts": lockedProductionFacts, "authoritative_character_names": characterAuthority, "previous_state_snapshot": previousSnapshot, "open_clues": openClues, "required_episode_start": batch.EpisodeStart, "required_episode_end": batch.EpisodeEnd, "required_episode_count": batch.EpisodeCount}
	payload, _ := json.Marshal(input)
	_ = s.db.Model(batch).Updates(map[string]any{"status": BatchStatusGenerating, "error": "", "generation": gorm.Expr("generation + 1")}).Error
	system := fmt.Sprintf("你是长篇故事滚动规划师。只输出JSON对象 {\"summary\":\"批次摘要\",\"episodes\":[EpisodeAdaptation字段]}。必须精确生成%d集，集号从%d连续到%d；每集必须含title、chapter_start、chapter_end、source_chapter_ids(必须使用chapter_id_by_order中的数据库ID组成非空JSON数组；不得填写章节序号)、adaptation_goal、opening_state、ending_state、hook、target_duration(默认180)、target_scenes(默认25)。每集chapter_start和chapter_end必须位于本批次原文章节%d至%d内；集数可以多于章节数，此时允许相邻多集引用同一章，绝不可把集号当成章节号。不得重写已完成批次，必须承接previous_ending_state。previous_production_facts是上一集实际完成的Scene、Dialogue和剧本，是最高排重权威：后续集禁止重演、换措辞重讲、再次揭示或回顾其中已发生的事件、动作、信息、对白目的和情绪节点；必须从其结尾之后产生新推进。同一批次各集也必须事件互斥，后一集不得重复前一集已分配的核心事件。locked_episodes及locked_episode_production_facts是本批次中已经生产的不可变分集，必须原样返回其规划字段，绝对不得重写、重新分配其事件或改变人物名；后续集只能使用尚未被锁定集消费的事件。authoritative_character_names列出跨集权威角色名及别名：同一人物必须始终使用权威名称，不得把“马面青年”等既有角色改成另一个新名字、同义称呼或临时别名；只有原文和故事圣经明确的新登场人物才允许新增名字。", batch.EpisodeCount, batch.EpisodeStart, batch.EpisodeEnd, batch.ChapterStart, batch.ChapterEnd)
	if outlineMode {
		system += " 当前项目来自故事梗概而非小说章节；source_chapter_ids固定输出空数组[]，chapter_start和chapter_end可使用0。"
	}
	raw, err := s.skills.ChatWithSkill(projectID, models.SkillStageAdaptationPlan, s.provider, system, "", map[string]string{"strategy": string(payload), "story_bible": bibleJSON(bible), "chapter_analyses": string(payload)})
	if err != nil {
		s.db.Model(batch).Updates(map[string]any{"status": BatchStatusDraft, "error": err.Error()})
		return nil, err
	}
	var parsed struct {
		Summary  string                     `json:"summary"`
		Episodes []models.EpisodeAdaptation `json:"episodes"`
	}
	normalized, repairedRaw, normalizeErr := normalizeAdaptationJSONWithRepair(s.provider, raw)
	if normalizeErr != nil {
		return nil, normalizeErr
	}
	raw = repairedRaw
	if err := parseJSONObject(normalized, &parsed); err != nil {
		return nil, err
	}
	if len(parsed.Episodes) != batch.EpisodeCount {
		return nil, fmt.Errorf("模型返回%d集，批次要求%d集；未保存", len(parsed.Episodes), batch.EpisodeCount)
	}
	for i := range parsed.Episodes {
		want := batch.EpisodeStart + i
		if locked, ok := lockedEpisodes[want]; ok {
			parsed.Episodes[i] = locked
		}
	}
	if duplicateIssues, duplicateErr := s.batchPlanningDuplicateIssues(projectID, batch.EpisodeStart, parsed.Episodes); duplicateErr != nil {
		return nil, duplicateErr
	} else if len(duplicateIssues) > 0 {
		repairSystem := "你是分集规划排重修复器。只输出完整合法JSON，保持集号、字段结构、来源章节、未重复事实和集数不变。必须重写列出的重复事件，使每集从上一集实际结尾之后产生新的剧情推进；禁止用换措辞、回忆、复述或再次揭示伪装新剧情。"
		lockedFactsJSON, _ := json.Marshal(lockedProductionFacts)
		characterAuthorityJSON, _ := json.Marshal(characterAuthority)
		repairUser := "重复问题：\n- " + strings.Join(duplicateIssues, "\n- ") + "\n\n批次外上一集生产权威：\n" + previousProductionFacts + "\n\n本批次已锁定生产事实：\n" + string(lockedFactsJSON) + "\n\n跨集角色权威：\n" + string(characterAuthorityJSON) + "\n\n待修复规划：\n" + raw
		repaired, repairErr := s.provider.Chat(repairSystem, repairUser)
		if repairErr != nil {
			return nil, fmt.Errorf("批次跨集重复自动修复失败: %w", repairErr)
		}
		normalizedRepair, _, normalizeRepairErr := normalizeAdaptationJSONWithRepair(s.provider, repaired)
		if normalizeRepairErr != nil {
			return nil, normalizeRepairErr
		}
		if err := parseJSONObject(normalizedRepair, &parsed); err != nil {
			return nil, fmt.Errorf("批次跨集重复修复返回非法JSON: %w", err)
		}
		if len(parsed.Episodes) != batch.EpisodeCount {
			return nil, fmt.Errorf("批次跨集重复修复改变集数：得到%d集，要求%d集；未保存", len(parsed.Episodes), batch.EpisodeCount)
		}
		for i := range parsed.Episodes {
			want := batch.EpisodeStart + i
			if locked, ok := lockedEpisodes[want]; ok {
				parsed.Episodes[i] = locked
			}
		}
		remaining, remainingErr := s.batchPlanningDuplicateIssues(projectID, batch.EpisodeStart, parsed.Episodes)
		if remainingErr != nil {
			return nil, remainingErr
		}
		if len(remaining) > 0 {
			return nil, fmt.Errorf("批次方案排重修复后仍重复，未保存: %s", strings.Join(remaining, "；"))
		}
		raw = repaired
	}
	for i := range parsed.Episodes {
		ep := &parsed.Episodes[i]
		want := batch.EpisodeStart + i
		if ep.EpisodeN == 0 {
			ep.EpisodeN = want
		}
		if ep.EpisodeN != want {
			return nil, fmt.Errorf("批次集号不连续：得到%d，应为%d", ep.EpisodeN, want)
		}
		if locked, ok := lockedEpisodes[want]; ok {
			parsed.Episodes[i] = locked
			continue
		}
		ep.ProjectID = projectID
		ep.BatchID = &batch.ID
		if ep.TargetDuration == 0 {
			ep.TargetDuration = 180
		}
		if ep.TargetScenes == 0 {
			ep.TargetScenes = 25
		}
		if outlineMode {
			ep.ChapterStart, ep.ChapterEnd, ep.SourceChapterIDs = 0, 0, "[]"
			if ep.TargetDuration < 162 || ep.TargetDuration > 198 || ep.TargetScenes < 20 || ep.TargetScenes > 30 {
				return nil, fmt.Errorf("第%d集: 时长或镜头数超出范围", ep.EpisodeN)
			}
		} else {
			constrainEpisodeChapterRange(ep, batch)
			if err := fillEpisodeSourceChapterIDs(ep, chapters); err != nil {
				return nil, fmt.Errorf("第%d集: %w", ep.EpisodeN, err)
			}
			if err := validateAdaptation(ep); err != nil {
				return nil, fmt.Errorf("第%d集: %w", ep.EpisodeN, err)
			}
		}
		ep.Status = "draft"
		ep.Version = 1
		ep.ContinuityStatus = "pending"
		if outlineMode {
			ep.SourceDigest = novelHash(project.Synopsis + "\n" + project.Plan)
		} else {
			ep.SourceDigest, err = s.sourceDigest(projectID, ep.SourceChapterIDs)
			if err != nil {
				return nil, err
			}
		}
	}
	snapshot, _ := json.Marshal(map[string]any{"input": input, "output": json.RawMessage(outputJSON(raw))})
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("batch_id = ?", batch.ID).Delete(&models.BatchEpisode{}).Error; err != nil {
			return err
		}
		for episodeN := batch.EpisodeStart; episodeN <= batch.EpisodeEnd; episodeN++ {
			if _, locked := lockedEpisodes[episodeN]; locked {
				continue
			}
			if err := tx.Where("project_id = ? AND episode_n = ?", projectID, episodeN).Delete(&models.EpisodeAdaptation{}).Error; err != nil {
				return err
			}
		}
		for i := range parsed.Episodes {
			_, locked := lockedEpisodes[parsed.Episodes[i].EpisodeN]
			if !locked {
				if err := tx.Create(&parsed.Episodes[i]).Error; err != nil {
					return err
				}
			}
			batchEpisodeStatus := BatchEpisodeStatusDraft
			if locked {
				batchEpisodeStatus = BatchEpisodeStatusProduced
			}
			be := models.BatchEpisode{BatchID: batch.ID, EpisodeAdaptID: parsed.Episodes[i].ID, EpisodeN: parsed.Episodes[i].EpisodeN, BatchOrder: i + 1, Status: batchEpisodeStatus}
			if err := tx.Create(&be).Error; err != nil {
				return err
			}
		}
		return tx.Model(batch).Updates(map[string]any{"status": BatchStatusReview, "review_status": ReviewStatusPending, "summary": parsed.Summary, "generation_json": string(snapshot), "total_episodes": len(parsed.Episodes), "error": ""}).Error
	})
	if err != nil {
		return nil, err
	}
	return s.Detail(projectID, batch.ID)
}

// GenerateBatchPlan keeps the original service contract for callers that only need
// persistence scaffolding. Production AI generation uses GenerateBatchDraft.
func (s *BatchPlanningService) GenerateBatchPlan(batchID uint, params BatchGenerationParams) (*BatchProgress, error) {
	batch, err := s.GetBatch(0, batchID)
	if err != nil {
		return nil, err
	}
	generated := 0
	err = s.db.Transaction(func(tx *gorm.DB) error {
		for episodeN := batch.EpisodeStart; episodeN <= batch.EpisodeEnd; episodeN++ {
			var adaptation models.EpisodeAdaptation
			err := tx.Where("project_id = ? AND episode_n = ?", batch.ProjectID, episodeN).First(&adaptation).Error
			if err == gorm.ErrRecordNotFound {
				adaptation = models.EpisodeAdaptation{ProjectID: batch.ProjectID, EpisodeN: episodeN, BatchID: &batch.ID, Status: "draft", Version: 1}
				if err := tx.Create(&adaptation).Error; err != nil {
					return err
				}
			} else if err != nil {
				return err
			} else if err := tx.Model(&adaptation).Update("batch_id", batch.ID).Error; err != nil {
				return err
			}
			link := models.BatchEpisode{BatchID: batch.ID, EpisodeAdaptID: adaptation.ID, EpisodeN: episodeN, BatchOrder: episodeN - batch.EpisodeStart + 1, Status: BatchEpisodeStatusDraft}
			if err := tx.Where("batch_id = ? AND episode_n = ?", batch.ID, episodeN).FirstOrCreate(&link).Error; err != nil {
				return err
			}
			generated++
		}
		return tx.Model(batch).Updates(map[string]any{"status": BatchStatusReview, "generation": params.Generation, "total_episodes": generated}).Error
	})
	if err != nil {
		return nil, err
	}
	return &BatchProgress{BatchID: batch.ID, TotalEpisodes: generated, GeneratedEpisodes: generated, Progress: 1}, nil
}

func (s *BatchPlanningService) sourceDigest(projectID uint, raw string) (string, error) {
	var ids []uint
	if json.Unmarshal([]byte(raw), &ids) != nil || len(ids) == 0 {
		return "", fmt.Errorf("source_chapter_ids必须是非空JSON数组")
	}
	var rows []models.Chapter
	if err := s.db.Where("project_id = ? AND id IN ?", projectID, ids).Find(&rows).Error; err != nil {
		return "", err
	}
	if len(rows) != len(ids) {
		return "", fmt.Errorf("存在不属于项目的章节")
	}
	var b strings.Builder
	for _, r := range rows {
		b.WriteString(strconv.Itoa(int(r.ID)))
		b.WriteString(":")
		b.WriteString(r.ContentHash)
	}
	return novelHash(b.String()), nil
}

func (s *BatchPlanningService) Detail(projectID, batchID uint) (*PlanningBatchDetail, error) {
	batch, err := s.GetBatch(projectID, batchID)
	if err != nil {
		return nil, err
	}
	var eps []models.EpisodeAdaptation
	s.db.Where("project_id = ? AND batch_id = ?", projectID, batchID).Order("episode_n").Find(&eps)
	var arcs []models.StoryArc
	arcNos := parseArcNumbers(batch.ArcRange)
	if len(arcNos) > 0 {
		s.db.Where("project_id = ? AND arc_no IN ?", projectID, arcNos).Order("arc_no").Find(&arcs)
	}
	return &PlanningBatchDetail{Batch: *batch, Episodes: eps, Arcs: arcs}, nil
}

func (s *BatchPlanningService) ApproveBatch(projectID, batchID uint, reviewerID uint, notes string) (*models.PlanningBatch, error) {
	batch, err := s.GetBatch(projectID, batchID)
	if err != nil {
		return nil, err
	}
	if batch.Status != BatchStatusReview {
		return nil, fmt.Errorf("只有待审核批次可以通过")
	}
	var eps []models.EpisodeAdaptation
	if err := s.db.Where("project_id = ? AND batch_id = ?", projectID, batchID).Order("episode_n").Find(&eps).Error; err != nil {
		return nil, err
	}
	if len(eps) != batch.EpisodeCount {
		return nil, fmt.Errorf("批次分集不完整")
	}
	now := time.Now()
	err = s.db.Transaction(func(tx *gorm.DB) error {
		for _, ep := range eps {
			if err := tx.Model(&models.EpisodeAdaptation{}).Where("id = ?", ep.ID).Update("status", "approved").Error; err != nil {
				return err
			}
			var durable models.Episode
			e := tx.Where("project_id = ? AND episode_number = ?", projectID, ep.EpisodeN).First(&durable).Error
			if e == gorm.ErrRecordNotFound {
				durable = models.Episode{ProjectID: projectID, Number: ep.EpisodeN, Title: ep.Title, TargetDuration: ep.TargetDuration, TargetScenes: ep.TargetScenes, Status: "planned", Version: 1}
				if err := tx.Create(&durable).Error; err != nil {
					return err
				}
			} else if e != nil {
				return e
			} else if err := tx.Model(&durable).Updates(map[string]any{"title": ep.Title, "target_duration": ep.TargetDuration, "target_scenes": ep.TargetScenes, "status": "planned", "version": gorm.Expr("version + 1")}).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&models.BatchEpisode{}).Where("batch_id = ?", batchID).Update("status", BatchEpisodeStatusApproved).Error; err != nil {
			return err
		}
		updates := map[string]any{"status": BatchStatusApproved, "review_status": ReviewStatusApproved, "reviewer_notes": notes, "reviewed_at": &now, "can_roll_next": true, "approved_count": len(eps)}
		if reviewerID > 0 {
			updates["reviewer_id"] = reviewerID
		}
		if err := tx.Model(batch).Updates(updates).Error; err != nil {
			return err
		}
		if batch.PreviousBatchID != nil {
			return tx.Model(&models.PlanningBatch{}).Where("id = ?", *batch.PreviousBatchID).Update("next_batch_id", batch.ID).Error
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.GetBatch(projectID, batchID)
}
func (s *BatchPlanningService) RejectBatch(projectID, batchID, reviewerID uint, notes string) (*models.PlanningBatch, error) {
	batch, err := s.GetBatch(projectID, batchID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	u := map[string]any{"status": BatchStatusDraft, "review_status": ReviewStatusRejected, "reviewer_notes": notes, "reviewed_at": &now, "can_roll_next": false}
	if reviewerID > 0 {
		u["reviewer_id"] = reviewerID
	}
	if err := s.db.Model(batch).Updates(u).Error; err != nil {
		return nil, err
	}
	return s.GetBatch(projectID, batchID)
}
func (s *BatchPlanningService) GetBatchProgress(batchID uint) (*BatchProgress, error) {
	b, err := s.GetBatch(0, batchID)
	if err != nil {
		return nil, err
	}
	var a, p int64
	s.db.Model(&models.BatchEpisode{}).Where("batch_id = ? AND status = ?", batchID, BatchEpisodeStatusApproved).Count(&a)
	s.db.Model(&models.BatchEpisode{}).Where("batch_id = ? AND status = ?", batchID, BatchEpisodeStatusProduced).Count(&p)
	g := 0
	if b.Status == BatchStatusReview || b.Status == BatchStatusApproved || b.Status == BatchStatusProduced {
		g = b.EpisodeCount
	}
	return &BatchProgress{BatchID: batchID, TotalEpisodes: b.EpisodeCount, GeneratedEpisodes: g, ReviewedEpisodes: int(a), ProducedEpisodes: int(p), Progress: float64(g) / float64(b.EpisodeCount)}, nil
}
func (s *BatchPlanningService) LinkStoryArcsToBatch(batchID uint, arcIDs []uint) error {
	return s.db.Model(&models.StoryArc{}).Where("id IN ?", arcIDs).Update("batch_id", batchID).Error
}
func (s *BatchPlanningService) GetBatchesByArc(projectID uint, arcNo int) ([]models.PlanningBatch, error) {
	var arc models.StoryArc
	if err := s.db.Where("project_id = ? AND arc_no = ?", projectID, arcNo).First(&arc).Error; err != nil {
		return nil, err
	}
	if arc.BatchID == nil {
		return []models.PlanningBatch{}, nil
	}
	var out []models.PlanningBatch
	err := s.db.Where("project_id = ? AND id = ?", projectID, *arc.BatchID).Find(&out).Error
	return out, err
}
func (s *BatchPlanningService) GetBatchEpisodes(batchID uint) ([]models.BatchEpisode, error) {
	var out []models.BatchEpisode
	err := s.db.Where("batch_id = ?", batchID).Order("batch_order").Find(&out).Error
	return out, err
}
func (s *BatchPlanningService) ApproveBatchEpisode(batchID uint, episodeN int, reviewerID uint, notes string) error {
	now := time.Now()
	return s.db.Model(&models.BatchEpisode{}).Where("batch_id = ? AND episode_n = ?", batchID, episodeN).Updates(map[string]any{"status": BatchEpisodeStatusApproved, "reviewer_id": reviewerID, "review_notes": notes, "reviewed_at": &now}).Error
}
func (s *BatchPlanningService) MarkEpisodeProduced(batchID uint, episodeN int) error {
	return s.db.Model(&models.BatchEpisode{}).Where("batch_id = ? AND episode_n = ?", batchID, episodeN).Update("status", BatchEpisodeStatusProduced).Error
}
func (s *BatchPlanningService) GetBatchForEpisode(projectID uint, episodeN int) (*models.PlanningBatch, error) {
	var b models.PlanningBatch
	err := s.db.Where("project_id = ? AND episode_start <= ? AND episode_end >= ? AND status <> ?", projectID, episodeN, episodeN, BatchStatusCancelled).First(&b).Error
	return &b, err
}
func (s *BatchPlanningService) GetBatchContinuityState(batchID uint) (string, error) {
	b, err := s.GetBatch(0, batchID)
	if err != nil || b.PreviousBatchID == nil {
		return "", err
	}
	var ep models.EpisodeAdaptation
	err = s.db.Where("batch_id = ?", *b.PreviousBatchID).Order("episode_n DESC").First(&ep).Error
	if err == gorm.ErrRecordNotFound {
		return "", nil
	}
	return ep.EndingState, err
}
func (s *BatchPlanningService) ValidateBatchContinuity(batchID uint) (bool, string, error) {
	b, err := s.GetBatch(0, batchID)
	if err != nil {
		return false, "", err
	}
	if b.PreviousBatchID == nil {
		return true, "", nil
	}
	var prev, first models.EpisodeAdaptation
	if err = s.db.Where("batch_id = ?", *b.PreviousBatchID).Order("episode_n DESC").First(&prev).Error; err != nil {
		return false, "上一批次末集不存在", nil
	}
	if err = s.db.Where("batch_id = ?", batchID).Order("episode_n").First(&first).Error; err != nil {
		return false, "当前批次首集不存在", nil
	}
	if prev.EndingState != "" && first.OpeningState != "" && prev.EndingState != first.OpeningState {
		return false, "前批结束状态与本批开始状态不一致", nil
	}
	return true, "", nil
}
func (s *BatchPlanningService) CalculateBatchPlan(projectID uint, total, min, max int) ([]map[string]int, error) {
	if min <= 0 {
		min = DefaultBatchSizeMin
	}
	if max <= 0 {
		max = DefaultBatchSizeMax
	}
	if total < min || min > max {
		return nil, fmt.Errorf("总集数必须不少于单批最小集数")
	}
	var out []map[string]int
	start := 1
	no := 1
	for start <= total {
		count := max
		if total-start+1 < count {
			count = total - start + 1
		}
		if count < min && len(out) > 0 {
			out[len(out)-1]["episode_end"] = total
			out[len(out)-1]["count"] += count
			break
		}
		out = append(out, map[string]int{"batch_no": no, "episode_start": start, "episode_end": start + count - 1, "count": count})
		start += count
		no++
	}
	return out, nil
}
