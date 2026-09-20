package service

import (
	"comfyui-console/internal/models"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"testing"
)

func newTestBatchService(t *testing.T) *BatchPlanningService {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(&models.Project{}, &models.PlanningBatch{}, &models.BatchEpisode{}, &models.EpisodeAdaptation{}, &models.StoryArc{}, &models.Episode{}, &models.BatchStateSnapshot{}, &models.StoryClue{}); err != nil {
		t.Fatal(err)
	}
	if err = db.Create(&models.Project{ID: 1, Title: "Novel", SourceType: models.ProjectSourceNovel, Episodes: 1800}).Error; err != nil {
		t.Fatal(err)
	}
	return NewBatchPlanningService(db)
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
