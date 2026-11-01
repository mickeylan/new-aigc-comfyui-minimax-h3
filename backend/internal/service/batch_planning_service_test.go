package service

import (
	"comfyui-console/internal/models"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"strings"
	"testing"
)

func newTestBatchService(t *testing.T) *BatchPlanningService {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(&models.Project{}, &models.PlanningBatch{}, &models.BatchEpisode{}, &models.EpisodeAdaptation{}, &models.StoryArc{}, &models.Episode{}, &models.Scene{}, &models.Dialogue{}, &models.Character{}, &models.CharacterAliasCandidate{}, &models.BatchStateSnapshot{}, &models.StoryClue{}, &models.AnalysisWindow{}); err != nil {
		t.Fatal(err)
	}
	if err = db.Create(&models.Project{ID: 1, Title: "Novel", SourceType: models.ProjectSourceNovel, Episodes: 1800}).Error; err != nil {
		t.Fatal(err)
	}
	return NewBatchPlanningService(db)
}

func TestRecoverInterruptedPlanningBatch(t *testing.T) {
	s := newTestBatchService(t)
	batch := models.PlanningBatch{ProjectID: 1, BatchNo: 1, Title: "中断批次", EpisodeStart: 1, EpisodeEnd: 5, EpisodeCount: 5, ChapterStart: 1, ChapterEnd: 5, Status: BatchStatusGenerating}
	if err := s.db.Create(&batch).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.RecoverInterruptedBatches(); err != nil {
		t.Fatal(err)
	}
	if err := s.db.First(&batch, batch.ID).Error; err != nil {
		t.Fatal(err)
	}
	if batch.Status != BatchStatusDraft || batch.Error == "" {
		t.Fatalf("interrupted batch was not recovered: %+v", batch)
	}
}

func TestAdaptationTimelineIssuesRejectsHookThatPredatesEnding(t *testing.T) {
	episode := models.EpisodeAdaptation{
		EpisodeN:     1,
		OpeningState: "苍黄荒地空旷无人，烈日炙烤着杂草、灌木和灰白石块；六七岁的妖狐女童柳乐儿正握着旧拨浪鼓亡命奔逃。",
		EndingState:  "丈许高的灰白巨石被蛛网般裂缝贯穿，随后寸寸爆碎，尘土散尽后显出一名青衣高大男子。",
		Hook:         "火海合围下，女童藏在草丛里的身影即将无处可逃。",
		MustKeepEvents: `[
			"女童握着拨浪鼓在荒地奔逃",
			"三名修士以火云符和引风术焚烧草丛，火海合围迫使女童现身",
			"女童施展木遁后撞上灰白巨石",
			"灰白巨石出现蛛网裂缝并整体爆碎，青衣高大男子现身"
		]`,
	}
	issues := adaptationTimelineIssues(episode)
	if len(issues) == 0 || !strings.Contains(strings.Join(issues, "；"), "钩子时序倒退") {
		t.Fatalf("reported timeline contradiction was not rejected: %v", issues)
	}
	episode.Hook = "尘土尚未落定，三名修士重新握紧法器，青衣男子挡在柳乐儿身前，新一轮对峙一触即发。"
	if issues := adaptationTimelineIssues(episode); len(issues) != 0 {
		t.Fatalf("valid post-ending hook was rejected: %v", issues)
	}
}

func TestCompactBatchPlanningContextAvoidsRawChapterAndFullScenePayloads(t *testing.T) {
	chapters := []models.Chapter{{ID: 7, Order: 1, Title: "狐女", Content: strings.Repeat("原文章节正文", 5000), Summary: strings.Repeat("摘要", 600), AnalysisJSON: strings.Repeat("事件", 4000)}}
	compact := compactBatchPlanningChapters(chapters)
	if len(compact) != 1 || strings.Contains(compact[0].AnalysisJSON, "原文章节正文") {
		t.Fatalf("raw chapter content leaked into planning context: %#v", compact)
	}
	if len(compact[0].Summary) > 1000 || len(compact[0].AnalysisJSON) > 6000 {
		t.Fatalf("planning chapter context exceeded bounds: summary=%d analysis=%d", len(compact[0].Summary), len(compact[0].AnalysisJSON))
	}

	s := newTestBatchService(t)
	if err := s.db.Create(&models.Scene{ProjectID: 1, EpisodeN: 1, Order: 1, Title: "已生产场景", Content: strings.Repeat("完整生产正文", 2000)}).Error; err != nil {
		t.Fatal(err)
	}
	facts, err := s.compactEpisodeProductionFacts(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) > 5000 || !strings.Contains(facts, "已生产场景") {
		t.Fatalf("unexpected compact production facts: bytes=%d text=%q", len(facts), facts)
	}
}

func TestLockedBatchEpisodesPreserveProducedAdaptations(t *testing.T) {
	s := newTestBatchService(t)
	produced := models.EpisodeAdaptation{ProjectID: 1, EpisodeN: 1, Title: "已生产第一集", Status: "scripted", Script: "权威剧本"}
	draft := models.EpisodeAdaptation{ProjectID: 1, EpisodeN: 2, Title: "待重规划第二集", Status: "approved"}
	if err := s.db.Create(&produced).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.db.Create(&draft).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.db.Create(&models.Scene{ProjectID: 1, EpisodeN: 1, Order: 1, Title: "权威Scene", Content: "已经发生"}).Error; err != nil {
		t.Fatal(err)
	}
	locked, err := s.lockedBatchEpisodes(1, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if locked[1].ID != produced.ID || locked[1].Script != "权威剧本" {
		t.Fatalf("produced episode not locked: %#v", locked)
	}
	if _, ok := locked[2]; ok {
		t.Fatalf("unproduced approved episode must remain replannable: %#v", locked[2])
	}
}

func TestBatchCharacterAuthorityIncludesProducedNamesAndConfirmedAliases(t *testing.T) {
	s := newTestBatchService(t)
	if err := s.db.AutoMigrate(&models.Character{}, &models.CharacterAliasCandidate{}, &models.Scene{}, &models.Dialogue{}); err != nil {
		t.Fatal(err)
	}
	if err := s.db.Create(&models.Character{ProjectID: 1, Name: "马面青年"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.db.Create(&models.CharacterAliasCandidate{ProjectID: 1, CanonicalName: "马面青年", Alias: "马脸青年", Status: "confirmed"}).Error; err != nil {
		t.Fatal(err)
	}
	scene := models.Scene{ProjectID: 1, EpisodeN: 1, Order: 1, VisibleCharacters: "马面青年", VoiceCharacters: "柳乐儿"}
	if err := s.db.Create(&scene).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.db.Create(&models.Dialogue{ProjectID: 1, SceneID: scene.ID, Character: "柳乐儿", Text: "快走"}).Error; err != nil {
		t.Fatal(err)
	}
	authority, err := s.batchCharacterAuthority(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := authority["马面青年"]; !ok || !strings.Contains(strings.Join(authority["马面青年"], ","), "马脸青年") {
		t.Fatalf("missing canonical alias authority: %#v", authority)
	}
	if _, ok := authority["柳乐儿"]; !ok {
		t.Fatalf("missing produced dialogue character: %#v", authority)
	}
}

func TestBatchPlanningDuplicateIssuesUsePreviousProducedScenesAndCurrentBatch(t *testing.T) {
	s := newTestBatchService(t)
	if err := s.db.AutoMigrate(&models.Scene{}); err != nil {
		t.Fatal(err)
	}
	prior := models.Scene{ProjectID: 1, EpisodeN: 1, Order: 1, Title: "桃花林重逢", Content: "舒寒以元婴之躯归来，与上官若琳在桃花林重逢并揭示身份。"}
	if err := s.db.Create(&prior).Error; err != nil {
		t.Fatal(err)
	}
	episodes := []models.EpisodeAdaptation{
		{EpisodeN: 2, Title: "元婴归来", AdaptationGoal: "舒寒以元婴之躯归来，在桃花林与上官若琳重逢并揭示身份。"},
		{EpisodeN: 3, Title: "再次重逢", AdaptationGoal: "舒寒元婴归来，在桃花林和上官若琳重逢，公开自己的身份。"},
	}
	issues, err := s.batchPlanningDuplicateIssues(1, 2, episodes)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(issues, "\n")
	if !strings.Contains(joined, "已生产第1集Scene 1") || !strings.Contains(joined, "第3集与本批第2集") {
		t.Fatalf("missing production/intra-batch duplicate issues: %v", issues)
	}
}

func TestPlanningBatchSupportsOutlineProjects(t *testing.T) {
	s := newTestBatchService(t)
	if err := s.db.Model(&models.Project{}).Where("id = ?", 1).Update("source_type", models.ProjectSourceOutline).Error; err != nil {
		t.Fatal(err)
	}
	batch, err := s.CreateBatch(1, BatchCreateInput{Title: "梗概第一批", EpisodeStart: 1, EpisodeEnd: 10, ChapterStart: 1, ChapterEnd: 1})
	if err != nil {
		t.Fatalf("outline project should support rolling batches: %v", err)
	}
	if batch.EpisodeCount != 10 {
		t.Fatalf("unexpected batch: %+v", batch)
	}
}

func TestPlanningBatchAutoBindsOwningAnalysisWindow(t *testing.T) {
	s := newTestBatchService(t)
	window := models.AnalysisWindow{ProjectID: 1, WindowNo: 1, ChapterStart: 1, ChapterEnd: 10, ContextStart: 1, ContextEnd: 15, Status: models.WindowStatusApproved}
	if err := s.db.Create(&window).Error; err != nil {
		t.Fatal(err)
	}
	batch, err := s.CreateBatch(1, BatchCreateInput{Title: "第一批", EpisodeStart: 1, EpisodeEnd: 5, ChapterStart: 1, ChapterEnd: 10})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.db.First(&window, window.ID).Error; err != nil {
		t.Fatal(err)
	}
	if window.PlanningBatchID == nil || *window.PlanningBatchID != batch.ID {
		t.Fatalf("window not bound: %+v", window)
	}
}

func TestPlanningBatchRequiresFiveToTwentyEpisodes(t *testing.T) {
	s := newTestBatchService(t)
	if _, e := s.CreateBatch(1, BatchCreateInput{Title: "small", EpisodeStart: 1, EpisodeEnd: 4, ChapterStart: 1, ChapterEnd: 3}); e == nil {
		t.Fatal("expected small batch rejection")
	}
	if _, e := s.CreateBatch(1, BatchCreateInput{Title: "large", EpisodeStart: 1, EpisodeEnd: 21, ChapterStart: 1, ChapterEnd: 10}); e == nil {
		t.Fatal("expected large batch rejection")
	}
	b, e := s.CreateBatch(1, BatchCreateInput{Title: "第一批", EpisodeStart: 1, EpisodeEnd: 10, ChapterStart: 1, ChapterEnd: 8, ArcRange: "1-2"})
	if e != nil {
		t.Fatal(e)
	}
	if b.EpisodeCount != 10 || b.ChapterStart != 1 || b.ChapterEnd != 8 {
		t.Fatalf("unexpected batch: %+v", b)
	}
}
func TestPlanningBatchRejectsOverlapAndBlocksNextUntilReview(t *testing.T) {
	s := newTestBatchService(t)
	b, e := s.CreateBatch(1, BatchCreateInput{Title: "第一批", EpisodeStart: 1, EpisodeEnd: 10, ChapterStart: 1, ChapterEnd: 8})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.CreateBatch(1, BatchCreateInput{Title: "overlap", EpisodeStart: 8, EpisodeEnd: 15, ChapterStart: 9, ChapterEnd: 15}); e == nil {
		t.Fatal("expected overlap rejection")
	}
	can, _, e := s.CanCreateNextBatch(1)
	if e != nil || can {
		t.Fatal("unreviewed batch must block rolling")
	}
	if _, e = s.GenerateBatchPlan(b.ID, BatchGenerationParams{Generation: 1}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ApproveBatch(1, b.ID, 0, "ok"); e != nil {
		t.Fatal(e)
	}
	can, _, e = s.CanCreateNextBatch(1)
	if e != nil || !can {
		t.Fatal("approved batch should allow rolling")
	}
	next, e := s.CreateBatch(1, BatchCreateInput{Title: "第二批", EpisodeStart: 11, EpisodeEnd: 20, ChapterStart: 9, ChapterEnd: 16})
	if e != nil {
		t.Fatal(e)
	}
	if next.PreviousBatchID == nil || *next.PreviousBatchID != b.ID {
		t.Fatal("previous batch lineage missing")
	}
}

func TestApprovePlanningBatchPreservesProducedEpisodeStatusAndScript(t *testing.T) {
	s := newTestBatchService(t)
	batch, err := s.CreateBatch(1, BatchCreateInput{Title: "第一批", EpisodeStart: 1, EpisodeEnd: 5, ChapterStart: 1, ChapterEnd: 5})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.GenerateBatchPlan(batch.ID, BatchGenerationParams{Generation: 1}); err != nil {
		t.Fatal(err)
	}
	var first models.EpisodeAdaptation
	if err := s.db.Where("project_id = ? AND episode_n = 1", 1).First(&first).Error; err != nil {
		t.Fatal(err)
	}
	first.Status, first.Script, first.ContinuityStatus = "scripted", "已有剧本", "passed"
	if err := s.db.Save(&first).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.db.Create(&models.Scene{ProjectID: 1, EpisodeN: 1, Order: 1, Title: "已生产"}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err = s.ApproveBatch(1, batch.ID, 0, "approved"); err != nil {
		t.Fatal(err)
	}
	if err := s.db.First(&first, first.ID).Error; err != nil {
		t.Fatal(err)
	}
	if first.Status != "scripted" || first.Script != "已有剧本" {
		t.Fatalf("approval downgraded produced episode: %+v", first)
	}
	var link models.BatchEpisode
	if err := s.db.Where("batch_id = ? AND episode_n = 1", batch.ID).First(&link).Error; err != nil {
		t.Fatal(err)
	}
	if link.Status != BatchEpisodeStatusProduced {
		t.Fatalf("produced batch episode status lost: %+v", link)
	}
}

func TestApprovePlanningBatchCreatesDurableEpisodes(t *testing.T) {
	s := newTestBatchService(t)
	b, e := s.CreateBatch(1, BatchCreateInput{Title: "第一批", EpisodeStart: 1, EpisodeEnd: 5, ChapterStart: 1, ChapterEnd: 5})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.GenerateBatchPlan(b.ID, BatchGenerationParams{Generation: 1}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ApproveBatch(1, b.ID, 0, "approved"); e != nil {
		t.Fatal(e)
	}
	var count int64
	s.db.Model(&models.Episode{}).Where("project_id = ?", 1).Count(&count)
	if count != 5 {
		t.Fatalf("expected 5 durable episodes, got %d", count)
	}
}
func TestCalculateRollingBatchPlan(t *testing.T) {
	s := newTestBatchService(t)
	rows, e := s.CalculateBatchPlan(1, 45, 5, 20)
	if e != nil {
		t.Fatal(e)
	}
	if len(rows) != 3 || rows[0]["count"] != 20 || rows[2]["count"] != 5 {
		t.Fatalf("unexpected split: %+v", rows)
	}
	if _, e = s.CalculateBatchPlan(1, 3, 5, 20); e == nil {
		t.Fatal("expected minimum rejection")
	}
}
func TestGenerateSnapshotRequiresApprovedMappingsAndScripts(t *testing.T) {
	s := newTestBatchService(t)
	s.provider = &mockTextProviderForTest{}
	s.skills = NewSkillService(s.db)
	batch, err := s.CreateBatch(1, BatchCreateInput{Title: "一", EpisodeStart: 1, EpisodeEnd: 5, ChapterStart: 1, ChapterEnd: 5})
	if err != nil {
		t.Fatal(err)
	}
	s.db.Model(batch).Update("status", BatchStatusApproved)
	for n := 1; n <= 5; n++ {
		s.db.Create(&models.EpisodeAdaptation{ProjectID: 1, EpisodeN: n, BatchID: &batch.ID, Status: "approved"})
	}
	if _, err := s.GenerateSnapshot(1, batch.ID); err == nil || !strings.Contains(err.Error(), "尚缺") {
		t.Fatalf("missing scripts were not explained: %v", err)
	}
}

func TestRollingStateSnapshotAndClueLifecycle(t *testing.T) {
	s := newTestBatchService(t)
	batch, _ := s.CreateBatch(1, BatchCreateInput{Title: "一", EpisodeStart: 1, EpisodeEnd: 5, ChapterStart: 1, ChapterEnd: 5})
	snapshot, err := s.SaveSnapshot(1, batch.ID, RollingStateInput{CharacterStatesJSON: `{"韩立":{"cultivation":"练气"}}`, RelationshipsJSON: `{}`, WorldStateJSON: `{"location":"七玄门"}`, ClueStateJSON: `[{"clue_key":"掌天瓶","title":"神秘小瓶","status":"open","opened_episode":1}]`})
	if err != nil {
		t.Fatal(err)
	}
	s.db.Create(&models.EpisodeAdaptation{ProjectID: 1, EpisodeN: 1, BatchID: &batch.ID, OpeningState: "", EndingState: ""})
	approved, err := s.ReviewSnapshot(1, batch.ID, true, "")
	if err != nil {
		t.Fatal(err)
	}
	if approved.Status != "approved" {
		t.Fatal("snapshot not approved")
	}
	var clue models.StoryClue
	if err = s.db.Where("project_id = ? AND clue_key = ?", 1, "掌天瓶").First(&clue).Error; err != nil {
		t.Fatal(err)
	}
	if _, err = s.TransitionClue(1, clue.ID, "resolved", 5, false); err != nil {
		t.Fatal(err)
	}
	if _, err = s.TransitionClue(1, clue.ID, "open", 6, false); err == nil {
		t.Fatal("resolved clue reopened without override")
	}
	if snapshot.Version != 1 {
		t.Fatalf("unexpected snapshot version %d", snapshot.Version)
	}
}

func TestBatchContinuityUsesPreviousEndingState(t *testing.T) {
	s := newTestBatchService(t)
	first, _ := s.CreateBatch(1, BatchCreateInput{Title: "一", EpisodeStart: 1, EpisodeEnd: 5, ChapterStart: 1, ChapterEnd: 5})
	s.db.Model(first).Updates(map[string]any{"status": BatchStatusApproved, "review_status": ReviewStatusApproved})
	s.db.Create(&models.EpisodeAdaptation{ProjectID: 1, EpisodeN: 5, BatchID: &first.ID, EndingState: "境界筑基"})
	second, e := s.CreateBatch(1, BatchCreateInput{Title: "二", EpisodeStart: 6, EpisodeEnd: 10, ChapterStart: 6, ChapterEnd: 10})
	if e != nil {
		t.Fatal(e)
	}
	s.db.Create(&models.EpisodeAdaptation{ProjectID: 1, EpisodeN: 6, BatchID: &second.ID, OpeningState: "境界筑基"})
	ok, msg, e := s.ValidateBatchContinuity(second.ID)
	if e != nil || !ok {
		t.Fatalf("expected continuity, %s %v", msg, e)
	}
	s.db.Model(&models.EpisodeAdaptation{}).Where("batch_id = ? AND episode_n = ?", second.ID, 6).Update("opening_state", "结丹")
	ok, _, _ = s.ValidateBatchContinuity(second.ID)
	if ok {
		t.Fatal("expected mismatch")
	}
}
