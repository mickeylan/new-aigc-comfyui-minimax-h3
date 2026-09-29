package service

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"comfyui-console/internal/models"
)

func newTestNovelAnalysisService(t *testing.T) (*NovelAnalysisService, *gorm.DB) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&models.Project{},
		&models.Chapter{},
		&models.ChapterTask{},
		&models.StoryArc{},
		&models.StoryBible{},
		&models.StoryBibleChange{},
		&models.AdaptationStrategy{},
		&models.EpisodeAdaptation{},
		&models.CharacterAliasCandidate{},
		&models.NovelJob{},
		&models.AnalysisWindow{},
	); err != nil {
		t.Fatal(err)
	}
	// Create minimal services for testing window operations (which don't need AI calls)
	skillSvc := NewSkillService(db)
	svc := NewNovelAnalysisService(db, &mockTextProviderForTest{}, skillSvc)
	return svc, db
}

// mockTextProviderForTest implements TextProvider interface for testing
type mockTextProviderForTest struct{}

func (m *mockTextProviderForTest) Name() string { return "mock" }

func (m *mockTextProviderForTest) Chat(system, user string) (string, error) {
	return "{}", nil
}

func (m *mockTextProviderForTest) HealthCheck(ctx context.Context) error {
	return nil
}

func setupTestProjectWithChapters(t *testing.T, db *gorm.DB, chapterCount int) uint {
	project := models.Project{
		Title:      "测试小说",
		SourceType: models.ProjectSourceNovel,
		Status:     "draft",
	}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}

	for i := 1; i <= chapterCount; i++ {
		chapter := models.Chapter{
			ProjectID: project.ID,
			Order:     i,
			Title:     "章节" + string(rune('0'+i)),
			Content:   "这是第" + string(rune('0'+i)) + "章的内容",
		}
		if err := db.Create(&chapter).Error; err != nil {
			t.Fatal(err)
		}
	}
	return project.ID
}

func TestCreateWindow(t *testing.T) {
	svc, db := newTestNovelAnalysisService(t)
	projectID := setupTestProjectWithChapters(t, db, 30)

	opts := WindowOptions{
		ChapterStart: 1,
		ChapterEnd:   10,
		WindowSize:   10,
	}
	window, err := svc.CreateWindow(projectID, opts)
	if err != nil {
		t.Fatalf("CreateWindow failed: %v", err)
	}
	if window.WindowNo != 1 {
		t.Errorf("expected window_no 1, got %d", window.WindowNo)
	}
	if window.ChapterStart != 1 || window.ChapterEnd != 10 {
		t.Errorf("expected chapter range 1-10, got %d-%d", window.ChapterStart, window.ChapterEnd)
	}
	if window.Status != models.WindowStatusPending {
		t.Errorf("expected status pending, got %s", window.Status)
	}
}

func TestCreateWindowWithContext(t *testing.T) {
	svc, db := newTestNovelAnalysisService(t)
	projectID := setupTestProjectWithChapters(t, db, 30)

	opts := WindowOptions{
		ChapterStart: 5,
		ChapterEnd:   15,
		ContextStart: 1,
		ContextEnd:   20,
		WindowSize:   11,
	}
	window, err := svc.CreateWindow(projectID, opts)
	if err != nil {
		t.Fatalf("CreateWindow failed: %v", err)
	}
	if window.ContextStart != 1 || window.ContextEnd != 20 {
		t.Errorf("expected context range 1-20, got %d-%d", window.ContextStart, window.ContextEnd)
	}
}

func TestCreateWindowInvalidRange(t *testing.T) {
	svc, db := newTestNovelAnalysisService(t)
	projectID := setupTestProjectWithChapters(t, db, 10)

	tests := []struct {
		name string
		opts WindowOptions
	}{
		{"start <= 0", WindowOptions{ChapterStart: 0, ChapterEnd: 5}},
		{"end < start", WindowOptions{ChapterStart: 10, ChapterEnd: 5}},
	}
	for _, tt := range tests {
		_, err := svc.CreateWindow(projectID, tt.opts)
		if err == nil {
			t.Errorf("%s: expected error, got nil", tt.name)
		}
	}
}

func TestCreateWindowOverlap(t *testing.T) {
	svc, db := newTestNovelAnalysisService(t)
	projectID := setupTestProjectWithChapters(t, db, 30)

	// Create first window
	opts1 := WindowOptions{ChapterStart: 1, ChapterEnd: 10}
	_, err := svc.CreateWindow(projectID, opts1)
	if err != nil {
		t.Fatal(err)
	}

	// Try to create overlapping window
	opts2 := WindowOptions{ChapterStart: 5, ChapterEnd: 15}
	_, err = svc.CreateWindow(projectID, opts2)
	if err == nil {
		t.Error("expected error for overlapping window")
	}
}

func TestListWindows(t *testing.T) {
	svc, db := newTestNovelAnalysisService(t)
	projectID := setupTestProjectWithChapters(t, db, 50)

	// Create 3 windows
	for i := 0; i < 3; i++ {
		opts := WindowOptions{
			ChapterStart: i*10 + 1,
			ChapterEnd:   (i + 1) * 10,
		}
		_, err := svc.CreateWindow(projectID, opts)
		if err != nil {
			t.Fatal(err)
		}
	}

	windows, err := svc.ListWindows(projectID)
	if err != nil {
		t.Fatal(err)
	}
	if len(windows) != 3 {
		t.Errorf("expected 3 windows, got %d", len(windows))
	}
	// Check ordering
	for i, w := range windows {
		if w.WindowNo != i+1 {
			t.Errorf("expected window_no %d at index %d, got %d", i+1, i, w.WindowNo)
		}
	}
}

func TestGetWindow(t *testing.T) {
	svc, db := newTestNovelAnalysisService(t)
	projectID := setupTestProjectWithChapters(t, db, 20)

	opts := WindowOptions{ChapterStart: 1, ChapterEnd: 10}
	created, err := svc.CreateWindow(projectID, opts)
	if err != nil {
		t.Fatal(err)
	}

	window, err := svc.GetWindow(projectID, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if window.ID != created.ID {
		t.Errorf("expected window ID %d, got %d", created.ID, window.ID)
	}
}

func TestGetWindowNotFound(t *testing.T) {
	svc, _ := newTestNovelAnalysisService(t)
	_, err := svc.GetWindow(999, 999)
	if err == nil {
		t.Error("expected error for non-existent window")
	}
}

func TestGetCurrentWindow(t *testing.T) {
	svc, db := newTestNovelAnalysisService(t)
	projectID := setupTestProjectWithChapters(t, db, 30)

	// Create first window
	opts1 := WindowOptions{ChapterStart: 1, ChapterEnd: 10}
	window1, err := svc.CreateWindow(projectID, opts1)
	if err != nil {
		t.Fatal(err)
	}

	// Commit first window
	db.Model(window1).Update("status", models.WindowStatusCommitted)

	// Create second window
	opts2 := WindowOptions{ChapterStart: 11, ChapterEnd: 20}
	_, err = svc.CreateWindow(projectID, opts2)
	if err != nil {
		t.Fatal(err)
	}

	current, err := svc.GetCurrentWindow(projectID)
	if err != nil {
		t.Fatal(err)
	}
	if current.ChapterStart != 11 {
		t.Errorf("expected current window start 11, got %d", current.ChapterStart)
	}
}

func TestAdvanceToNextWindow(t *testing.T) {
	svc, db := newTestNovelAnalysisService(t)
	projectID := setupTestProjectWithChapters(t, db, 30)

	// Create and commit first window
	opts := WindowOptions{ChapterStart: 1, ChapterEnd: 10, WindowSize: 10}
	window1, err := svc.CreateWindow(projectID, opts)
	if err != nil {
		t.Fatal(err)
	}
	db.Model(window1).Update("status", models.WindowStatusCommitted)
	db.Model(window1).Update("context_end", 10)

	// Advance to next window
	nextWindow, err := svc.AdvanceToNextWindow(projectID, window1.ID, 10)
	if err != nil {
		t.Fatalf("AdvanceToNextWindow failed: %v", err)
	}
	if nextWindow.ChapterStart != 11 {
		t.Errorf("expected next window start 11, got %d", nextWindow.ChapterStart)
	}
	if nextWindow.ChapterEnd != 20 {
		t.Errorf("expected next window end 20, got %d", nextWindow.ChapterEnd)
	}
	if nextWindow.PreviousWindowID == nil || *nextWindow.PreviousWindowID != window1.ID {
		t.Error("next window should reference previous window")
	}
	if nextWindow.WindowNo != 2 {
		t.Errorf("expected window_no 2, got %d", nextWindow.WindowNo)
	}
}

func TestBoundWindowRequiresApprovedBatchSnapshotBeforeCommit(t *testing.T) {
	svc, db := newTestNovelAnalysisService(t)
	if err := db.AutoMigrate(&models.PlanningBatch{}, &models.BatchStateSnapshot{}); err != nil {
		t.Fatal(err)
	}
	projectID := setupTestProjectWithChapters(t, db, 20)
	window, err := svc.CreateWindow(projectID, WindowOptions{ChapterStart: 1, ChapterEnd: 10})
	if err != nil {
		t.Fatal(err)
	}
	db.Model(window).Update("status", models.WindowStatusApproved)
	batch := models.PlanningBatch{ProjectID: projectID, BatchNo: 1, Title: "第一批", EpisodeStart: 1, EpisodeEnd: 5, ChapterStart: 1, ChapterEnd: 10, Status: BatchStatusApproved, ReviewStatus: ReviewStatusApproved}
	db.Create(&batch)
	if _, err := svc.BindWindowBatch(projectID, window.ID, batch.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CommitWindow(projectID, window.ID); err == nil {
		t.Fatal("commit accepted without approved snapshot")
	}
	db.Create(&models.BatchStateSnapshot{ProjectID: projectID, BatchID: batch.ID, Status: "approved"})
	if _, err := svc.CommitWindow(projectID, window.ID); err != nil {
		t.Fatal(err)
	}
}

func TestAdvanceToNextWindowNoMoreChapters(t *testing.T) {
	svc, db := newTestNovelAnalysisService(t)
	projectID := setupTestProjectWithChapters(t, db, 30)

	// Create and commit window covering all chapters
	opts := WindowOptions{ChapterStart: 21, ChapterEnd: 30}
	window, err := svc.CreateWindow(projectID, opts)
	if err != nil {
		t.Fatal(err)
	}
	db.Model(window).Update("status", models.WindowStatusCommitted)

	// Try to advance past end
	_, err = svc.AdvanceToNextWindow(projectID, window.ID, 10)
	if err == nil {
		t.Error("expected error when no more chapters")
	}
}

func TestAdvanceToNextWindowNotCommitted(t *testing.T) {
	svc, db := newTestNovelAnalysisService(t)
	projectID := setupTestProjectWithChapters(t, db, 20)

	// Create uncommitted window
	opts := WindowOptions{ChapterStart: 1, ChapterEnd: 10}
	window, err := svc.CreateWindow(projectID, opts)
	if err != nil {
		t.Fatal(err)
	}
	// Don't commit

	_, err = svc.AdvanceToNextWindow(projectID, window.ID, 10)
	if err == nil {
		t.Error("expected error when window not committed")
	}
}

func TestGetWindowProgress(t *testing.T) {
	svc, db := newTestNovelAnalysisService(t)
	projectID := setupTestProjectWithChapters(t, db, 10)

	// Create window
	opts := WindowOptions{ChapterStart: 1, ChapterEnd: 10}
	window, err := svc.CreateWindow(projectID, opts)
	if err != nil {
		t.Fatal(err)
	}

	// Mark some chapters as ready
	db.Model(&models.Chapter{}).Where("project_id = ? AND chapter_order <= 5", projectID).
		Update("analysis_status", "ready")

	progress, err := svc.GetWindowProgress(projectID, window.ID)
	if err != nil {
		t.Fatal(err)
	}
	if progress.TotalChapters != 10 {
		t.Errorf("expected 10 total chapters, got %d", progress.TotalChapters)
	}
	if progress.ReadyChapters != 5 {
		t.Errorf("expected 5 ready chapters, got %d", progress.ReadyChapters)
	}
}

func TestIsWindowReady(t *testing.T) {
	svc, db := newTestNovelAnalysisService(t)
	projectID := setupTestProjectWithChapters(t, db, 10)

	opts := WindowOptions{ChapterStart: 1, ChapterEnd: 10}
	window, err := svc.CreateWindow(projectID, opts)
	if err != nil {
		t.Fatal(err)
	}

	// Initially not ready
	ready, _ := svc.IsWindowReady(projectID, window.ID)
	if ready {
		t.Error("window should not be ready initially")
	}

	// Mark all as ready
	db.Model(&models.Chapter{}).Where("project_id = ?", projectID).
		Update("analysis_status", "ready")

	ready, _ = svc.IsWindowReady(projectID, window.ID)
	if !ready {
		t.Error("window should be ready when all chapters analyzed")
	}
}

func TestGetWindowSuggestion(t *testing.T) {
	svc, db := newTestNovelAnalysisService(t)
	projectID := setupTestProjectWithChapters(t, db, 30)

	suggestion, err := svc.GetWindowSuggestion(projectID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if suggestion.SuggestedStart != 1 {
		t.Errorf("expected suggested start 1, got %d", suggestion.SuggestedStart)
	}
	if suggestion.SuggestedEnd != 10 {
		t.Errorf("expected suggested end 10, got %d", suggestion.SuggestedEnd)
	}
	if suggestion.TotalChapters != 30 {
		t.Errorf("expected 30 total chapters, got %d", suggestion.TotalChapters)
	}
	if suggestion.RemainingChapters != 30 {
		t.Errorf("expected 30 remaining chapters, got %d", suggestion.RemainingChapters)
	}
}

func TestGetWindowSuggestionAfterCommit(t *testing.T) {
	svc, db := newTestNovelAnalysisService(t)
	projectID := setupTestProjectWithChapters(t, db, 30)

	// Create and commit first window
	opts := WindowOptions{ChapterStart: 1, ChapterEnd: 10}
	window, err := svc.CreateWindow(projectID, opts)
	if err != nil {
		t.Fatal(err)
	}
	db.Model(window).Update("status", models.WindowStatusCommitted)

	suggestion, err := svc.GetWindowSuggestion(projectID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if suggestion.SuggestedStart != 11 {
		t.Errorf("expected suggested start 11, got %d", suggestion.SuggestedStart)
	}
	if suggestion.RemainingChapters != 20 {
		t.Errorf("expected 20 remaining chapters, got %d", suggestion.RemainingChapters)
	}
}

func TestGetWindowContext(t *testing.T) {
	svc, db := newTestNovelAnalysisService(t)
	projectID := setupTestProjectWithChapters(t, db, 20)

	opts := WindowOptions{
		ChapterStart: 5,
		ChapterEnd:   10,
		ContextStart: 1,
		ContextEnd:   15,
	}
	window, err := svc.CreateWindow(projectID, opts)
	if err != nil {
		t.Fatal(err)
	}

	chapters, err := svc.GetWindowContext(projectID, window.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(chapters) != 15 {
		t.Errorf("expected 15 context chapters, got %d", len(chapters))
	}
	if chapters[0].Order != 1 {
		t.Errorf("expected first chapter order 1, got %d", chapters[0].Order)
	}
}

func TestGetWindowTargetChapters(t *testing.T) {
	svc, db := newTestNovelAnalysisService(t)
	projectID := setupTestProjectWithChapters(t, db, 20)

	opts := WindowOptions{
		ChapterStart: 5,
		ChapterEnd:   10,
		ContextStart: 1,
		ContextEnd:   15,
	}
	window, err := svc.CreateWindow(projectID, opts)
	if err != nil {
		t.Fatal(err)
	}

	chapters, err := svc.GetWindowTargetChapters(projectID, window.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(chapters) != 6 {
		t.Errorf("expected 6 target chapters, got %d", len(chapters))
	}
	if chapters[0].Order != 5 {
		t.Errorf("expected first target chapter order 5, got %d", chapters[0].Order)
	}
}

func TestApproveWindow(t *testing.T) {
	svc, db := newTestNovelAnalysisService(t)
	projectID := setupTestProjectWithChapters(t, db, 10)

	opts := WindowOptions{ChapterStart: 1, ChapterEnd: 10}
	window, err := svc.CreateWindow(projectID, opts)
	if err != nil {
		t.Fatal(err)
	}
	db.Model(window).Update("status", models.WindowStatusReady)

	approved, err := svc.ApproveWindow(projectID, window.ID)
	if err != nil {
		t.Fatal(err)
	}
	if approved.Status != models.WindowStatusApproved {
		t.Errorf("expected status approved, got %s", approved.Status)
	}
	if approved.CommittedAt != nil {
		t.Error("committed_at must only be set after commit")
	}
}

func TestReviewBibleChangeRequiresMatchingApprovedBase(t *testing.T) {
	svc, db := newTestNovelAnalysisService(t)
	projectID := setupTestProjectWithChapters(t, db, 10)
	base := models.StoryBible{ProjectID: projectID, Premise: "旧前提", Status: "approved", Version: 2}
	if err := db.Create(&base).Error; err != nil {
		t.Fatal(err)
	}
	candidate := models.StoryBible{Premise: "新前提", WorldRules: "{}", MainPlot: "{}"}
	change := models.StoryBibleChange{ProjectID: projectID, BaseVersion: 2, CandidateJSON: bibleCandidateJSON(candidate), Status: "pending"}
	if err := db.Create(&change).Error; err != nil {
		t.Fatal(err)
	}
	updated, err := svc.ReviewBibleChange(projectID, change.ID, true, "确认")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Premise != "新前提" || updated.Version != 3 || updated.Status != "approved" {
		t.Fatalf("updated=%+v", updated)
	}
	var saved models.StoryBibleChange
	db.First(&saved, change.ID)
	if saved.Status != "approved" {
		t.Fatalf("change status=%s", saved.Status)
	}
}

func TestGenerateBibleDoesNotOverwriteApprovedVersion(t *testing.T) {
	svc, db := newTestNovelAnalysisService(t)
	projectID := setupTestProjectWithChapters(t, db, 10)
	db.Create(&models.StoryBible{ProjectID: projectID, Premise: "已审核", Status: "approved", Version: 1})
	if _, _, err := svc.GenerateBible(projectID); err == nil {
		t.Fatal("approved bible was overwriteable")
	}
}

func TestApproveWindowNotReady(t *testing.T) {
	svc, db := newTestNovelAnalysisService(t)
	projectID := setupTestProjectWithChapters(t, db, 10)

	opts := WindowOptions{ChapterStart: 1, ChapterEnd: 10}
	window, err := svc.CreateWindow(projectID, opts)
	if err != nil {
		t.Fatal(err)
	}
	// Window is still pending

	_, err = svc.ApproveWindow(projectID, window.ID)
	if err == nil {
		t.Error("expected error approving non-ready window")
	}
}

func TestCommitWindow(t *testing.T) {
	svc, db := newTestNovelAnalysisService(t)
	projectID := setupTestProjectWithChapters(t, db, 10)

	opts := WindowOptions{ChapterStart: 1, ChapterEnd: 10}
	window, err := svc.CreateWindow(projectID, opts)
	if err != nil {
		t.Fatal(err)
	}
	db.Model(window).Update("status", models.WindowStatusApproved)

	committed, err := svc.CommitWindow(projectID, window.ID)
	if err != nil {
		t.Fatal(err)
	}
	if committed.Status != models.WindowStatusCommitted {
		t.Errorf("expected status committed, got %s", committed.Status)
	}
}

func TestCommitWindowNotApproved(t *testing.T) {
	svc, db := newTestNovelAnalysisService(t)
	projectID := setupTestProjectWithChapters(t, db, 10)

	opts := WindowOptions{ChapterStart: 1, ChapterEnd: 10}
	window, err := svc.CreateWindow(projectID, opts)
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.CommitWindow(projectID, window.ID)
	if err == nil {
		t.Error("expected error committing non-approved window")
	}
}
