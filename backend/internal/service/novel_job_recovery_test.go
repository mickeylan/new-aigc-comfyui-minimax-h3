package service

import (
	"strings"
	"testing"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

func TestRecoverInterruptedNovelJobsMakesThemRetryable(t *testing.T) {
	svc, db := newTestNovelAnalysisService(t)
	projectID := setupTestProjectWithChapters(t, db, 2)
	job := models.NovelJob{ProjectID: projectID, Type: novelJobChapterAnalysis, Status: "running", Total: 2}
	if err := db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}
	window := models.AnalysisWindow{ProjectID: projectID, WindowNo: 1, ChapterStart: 1, ChapterEnd: 2, ContextStart: 1, ContextEnd: 2, WindowSize: 2, Status: models.WindowStatusAnalysing}
	if err := db.Create(&window).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.Chapter{}).Where("project_id = ? AND chapter_order = ?", projectID, 1).Update("analysis_status", "running").Error; err != nil {
		t.Fatal(err)
	}

	if err := svc.RecoverInterruptedJobs(); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&job, job.ID).Error; err != nil {
		t.Fatal(err)
	}
	if job.Status != "failed" || job.FinishedAt == nil || !strings.Contains(job.Error, "restart") {
		t.Fatalf("job was not made retryable: %+v", job)
	}
	if err := db.First(&window, window.ID).Error; err != nil {
		t.Fatal(err)
	}
	if window.Status != models.WindowStatusPending {
		t.Fatalf("window remained blocked: %+v", window)
	}
	var chapter models.Chapter
	if err := db.Where("project_id = ? AND chapter_order = ?", projectID, 1).First(&chapter).Error; err != nil {
		t.Fatal(err)
	}
	if chapter.AnalysisStatus != "pending" {
		t.Fatalf("chapter remained blocked: %+v", chapter)
	}
}

func migrateNovelJobSkillTables(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.AutoMigrate(&models.Skill{}, &models.ProjectSkillConfig{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Skill{Name: "章节分析", Code: novelJobChapterAnalysis, Operation: novelJobChapterAnalysis, Stage: novelJobChapterAnalysis, Version: 1, IsSystem: true, Enabled: true}).Error; err != nil {
		t.Fatal(err)
	}
}

func TestNewNovelJobRetiresOrphanedPendingJob(t *testing.T) {
	svc, db := newTestNovelAnalysisService(t)
	migrateNovelJobSkillTables(t, db)
	projectID := setupTestProjectWithChapters(t, db, 1)
	old := models.NovelJob{ProjectID: projectID, Type: novelJobChapterAnalysis, Status: "pending", Total: 1}
	if err := db.Create(&old).Error; err != nil {
		t.Fatal(err)
	}

	created, err := svc.newJob(projectID, novelJobChapterAnalysis, 1, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if created.Status != "running" {
		t.Fatalf("new job not running: %+v", created)
	}
	if err := db.First(&old, old.ID).Error; err != nil {
		t.Fatal(err)
	}
	if old.Status != "failed" || old.FinishedAt == nil {
		t.Fatalf("orphaned pending job still blocks work: %+v", old)
	}
}

func TestNewNovelJobStillRejectsActuallyRunningJob(t *testing.T) {
	svc, db := newTestNovelAnalysisService(t)
	migrateNovelJobSkillTables(t, db)
	projectID := setupTestProjectWithChapters(t, db, 1)
	if err := db.Create(&models.NovelJob{ProjectID: projectID, Type: novelJobChapterAnalysis, Status: "running", Total: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.newJob(projectID, novelJobChapterAnalysis, 1, 1, 1); err == nil {
		t.Fatal("concurrent running job was accepted")
	}
}
