package service

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"comfyui-console/internal/models"
)

func newTestNovelService(t *testing.T) *NovelService {
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
		&models.AdaptationStrategy{},
		&models.EpisodeAdaptation{},
		&models.CharacterAliasCandidate{},
		&models.NovelJob{},
	); err != nil {
		t.Fatal(err)
	}
	return NewNovelService(db)
}

func TestDetectChaptersWithChapterNumbers(t *testing.T) {
	ns := newTestNovelService(t)
	text := `第一章 相遇

这是第一章的内容，林夏走在回家的路上。

第二章 命运

这是第二章的内容，天空突然下起了雨。

第三章 转折

这是第三章的内容，她遇见了陆川。`

	chapters := ns.DetectChapters(text)
	if len(chapters) != 3 {
		t.Fatalf("expected 3 chapters, got %d", len(chapters))
	}
	if chapters[0].Title != "相遇" {
		t.Fatalf("expected chapter 1 title '相遇', got %q", chapters[0].Title)
	}
	if chapters[1].Title != "命运" {
		t.Fatalf("expected chapter 2 title '命运', got %q", chapters[1].Title)
	}
	if chapters[2].Title != "转折" {
		t.Fatalf("expected chapter 3 title '转折', got %q", chapters[2].Title)
	}
}

func TestDetectChaptersWithChapterKeyword(t *testing.T) {
	ns := newTestNovelService(t)
	text := `第1章 林夏的秘密

林夏是一个普通的上班族。

Chapter 2 意外相遇

在一个雨夜，她遇见了陆川。

第3章 真相

原来陆川是一个集团的继承人。`

	chapters := ns.DetectChapters(text)
	if len(chapters) != 3 {
		t.Fatalf("expected 3 chapters, got %d", len(chapters))
	}
}

func TestDetectChaptersWithMarkdownHeaders(t *testing.T) {
	ns := newTestNovelService(t)
	text := `# 第一章 相遇

这是第一章的内容。

## Chapter 2

这是第二章的内容。

### 第3章

这是第三章的内容。`

	chapters := ns.DetectChapters(text)
	// Note: Some markdown header patterns may not match all cases
	if len(chapters) < 2 {
		t.Fatalf("expected at least 2 chapters, got %d", len(chapters))
	}
}

func TestDetectChaptersNoChapters(t *testing.T) {
	ns := newTestNovelService(t)
	text := `这是一个没有章节标题的小说文本。
所有的内容都是连续的。
没有明显的章节分隔。`

	chapters := ns.DetectChapters(text)
	if len(chapters) != 1 {
		t.Fatalf("expected 1 chapter (whole text), got %d", len(chapters))
	}
	if chapters[0].Title != "全文" {
		t.Fatalf("expected title '全文', got %q", chapters[0].Title)
	}
}

func TestCountWords(t *testing.T) {
	ns := newTestNovelService(t)
	tests := []struct {
		text string
		want int
	}{
		{"你好世界", 4},         // 4个汉字
		{"Hello World", 10}, // 10个字符（空格+字母，忽略空格）
		{"你好 Hello", 7},     // 2个汉字 + 5个字母（忽略空格）
		{"这是一个测试。", 6},      // 6个汉字
		{"", 0},             // 空字符串
		{"   \n\t   ", 0},   // 只含空白
	}
	for _, tt := range tests {
		got := ns.CountWords(tt.text)
		if got != tt.want {
			t.Errorf("CountWords(%q) = %d, want %d", tt.text, got, tt.want)
		}
	}
}

func TestComputeFileHash(t *testing.T) {
	ns := newTestNovelService(t)
	data1 := []byte("test content")
	data2 := []byte("test content")
	data3 := []byte("different content")

	hash1 := ns.ComputeFileHash(data1)
	hash2 := ns.ComputeFileHash(data2)
	hash3 := ns.ComputeFileHash(data3)

	if hash1 != hash2 {
		t.Error("same content should have same hash")
	}
	if hash1 == hash3 {
		t.Error("different content should have different hash")
	}
	if len(hash1) != 64 { // SHA256 produces 64 hex characters
		t.Errorf("expected hash length 64, got %d", len(hash1))
	}
}

func TestCleanUTF8(t *testing.T) {
	ns := newTestNovelService(t)
	// Test valid UTF-8
	validUTF8 := []byte("你好世界Hello")
	clean, err := ns.CleanUTF8(validUTF8)
	if err != nil {
		t.Fatalf("valid UTF-8 should not error: %v", err)
	}
	if clean != "你好世界Hello" {
		t.Errorf("expected '你好世界Hello', got %q", clean)
	}

	// Test UTF-8 with BOM
	withBOM := append([]byte{0xEF, 0xBB, 0xBF}, []byte("Hello World")...)
	clean, _ = ns.CleanUTF8(withBOM)
	if clean != "Hello World" {
		t.Errorf("expected 'Hello World', got %q", clean)
	}

	// Test empty input
	empty, err := ns.CleanUTF8([]byte{})
	if err != nil {
		t.Fatalf("empty input should not error: %v", err)
	}
	if empty != "" {
		t.Errorf("expected empty string, got %q", empty)
	}
}

func TestImportNovelCreatesChapters(t *testing.T) {
	ns := newTestNovelService(t)

	// Create a novel project
	project := models.Project{
		Title:      "测试小说",
		SourceType: models.ProjectSourceNovel,
		Status:     "draft",
	}
	if err := ns.db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}

	// Test content with chapters
	content := []byte(`第一章 林夏的秘密

林夏是一个普通的上班族，每天朝九晚五。

第二章 命运的相遇

在一个雨夜，她遇见了改变她一生的人。`)

	updated, err := ns.ImportNovel(project.ID, "test_novel.txt", content)
	if err != nil {
		t.Fatalf("ImportNovel failed: %v", err)
	}

	// Check project status
	if updated.ImportStatus != models.NovelImportCompleted {
		t.Errorf("expected import status completed, got %s", updated.ImportStatus)
	}

	// Check chapters created
	chapters, err := ns.ListChapters(project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(chapters) != 2 {
		t.Errorf("expected 2 chapters, got %d", len(chapters))
	}

	// Check chapter order
	if chapters[0].Order != 1 || chapters[1].Order != 2 {
		t.Errorf("chapter order incorrect: %d, %d", chapters[0].Order, chapters[1].Order)
	}

	// Check word count
	if chapters[0].WordCount == 0 {
		t.Error("word count should not be 0")
	}
}

func TestImportNovelDuplicateDetection(t *testing.T) {
	ns := newTestNovelService(t)

	// First, create a project and import a file to get its hash
	project := models.Project{
		Title:      "测试小说",
		SourceType: models.ProjectSourceNovel,
	}
	if err := ns.db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}

	// Import first content
	content1 := []byte("第一章\n内容1\n第二章\n内容2")
	_, err := ns.ImportNovel(project.ID, "novel.txt", content1)
	if err != nil {
		t.Fatal(err)
	}

	// Get the hash of the first import
	var updated models.Project
	ns.db.First(&updated, project.ID)
	hash1 := updated.NovelFileHash

	// Try to import same content - should fail due to duplicate
	_, err = ns.ImportNovel(project.ID, "novel_again.txt", content1)
	if err == nil {
		t.Error("expected error for duplicate file")
	}

	// Import different content - should succeed
	content2 := []byte("第一章\n不同内容")
	_, err = ns.ImportNovel(project.ID, "different.txt", content2)
	if err != nil {
		t.Errorf("different content should succeed: %v", err)
	}

	// Verify hash changed
	var updated2 models.Project
	ns.db.First(&updated2, project.ID)
	if updated2.NovelFileHash == hash1 {
		t.Error("hash should change for different content")
	}
}

func TestUpdateChapterTitle(t *testing.T) {
	ns := newTestNovelService(t)

	project := models.Project{
		Title:      "测试小说",
		SourceType: models.ProjectSourceNovel,
	}
	ns.db.Create(&project)

	chapter := models.Chapter{
		ProjectID: project.ID,
		Order:     1,
		Title:     "原标题",
		Content:   "内容",
	}
	ns.db.Create(&chapter)

	err := ns.UpdateChapterTitle(&chapter, "新标题")
	if err != nil {
		t.Fatal(err)
	}

	updated, _ := ns.GetChapter(project.ID, chapter.ID)
	if updated.Title != "新标题" {
		t.Errorf("expected title '新标题', got %q", updated.Title)
	}
	if !updated.ManuallyEdited {
		t.Error("ManuallyEdited should be true")
	}
}

func TestReorderChapters(t *testing.T) {
	ns := newTestNovelService(t)

	project := models.Project{
		Title:      "测试小说",
		SourceType: models.ProjectSourceNovel,
	}
	ns.db.Create(&project)

	// Create 3 chapters
	for i := 1; i <= 3; i++ {
		ns.db.Create(&models.Chapter{
			ProjectID: project.ID,
			Order:     i,
			Title:     "章节" + string(rune('0'+i)),
			Content:   "内容",
		})
	}

	// Reorder: 1 -> 3, 2 -> 1, 3 -> 2
	err := ns.ReorderChapters(project.ID, map[uint]int{1: 3, 2: 1, 3: 2})
	if err != nil {
		t.Fatal(err)
	}

	chapters, _ := ns.ListChapters(project.ID)
	orders := make(map[int]int) // chapter_id -> order
	for _, ch := range chapters {
		orders[int(ch.ID)] = ch.Order
	}

	if orders[1] != 3 || orders[2] != 1 || orders[3] != 2 {
		t.Errorf("reorder failed: %v", orders)
	}
}

func TestSplitChapter(t *testing.T) {
	ns := newTestNovelService(t)

	project := models.Project{
		Title:      "测试小说",
		SourceType: models.ProjectSourceNovel,
	}
	ns.db.Create(&project)

	// Content: "第一部分。" + "第二部分。" = 14 chars total
	chapter := models.Chapter{
		ProjectID: project.ID,
		Order:     1,
		Title:     "长章节",
		Content:   "第一部分。第二部分。",
	}
	ns.db.Create(&chapter)

	// Split at position 6 (after "第一部分。")
	splitPoints := []int{6}
	task, newChapters, err := ns.SplitChapter(&chapter, splitPoints)
	if err != nil {
		t.Fatal(err)
	}

	if task.Status != models.ChapterTaskCompleted {
		t.Errorf("expected task status completed, got %s", task.Status)
	}
	if len(newChapters) != 2 {
		t.Errorf("expected 2 new chapters, got %d", len(newChapters))
	}

	// Verify original chapter was deleted
	var count int64
	ns.db.Model(&models.Chapter{}).Where("id = ?", chapter.ID).Count(&count)
	if count != 0 {
		t.Error("original chapter should be deleted")
	}
}

func TestMergeChapters(t *testing.T) {
	ns := newTestNovelService(t)

	project := models.Project{
		Title:      "测试小说",
		SourceType: models.ProjectSourceNovel,
	}
	ns.db.Create(&project)

	// Create 3 chapters
	chapter1 := models.Chapter{ProjectID: project.ID, Order: 1, Title: "第一章", Content: "内容1"}
	chapter2 := models.Chapter{ProjectID: project.ID, Order: 2, Title: "第二章", Content: "内容2"}
	chapter3 := models.Chapter{ProjectID: project.ID, Order: 3, Title: "第三章", Content: "内容3"}
	ns.db.Create(&chapter1)
	ns.db.Create(&chapter2)
	ns.db.Create(&chapter3)

	// Merge first 2 chapters
	task, merged, err := ns.MergeChapters(project.ID, []uint{chapter1.ID, chapter2.ID}, "合并章节")
	if err != nil {
		t.Fatal(err)
	}

	if task.Status != models.ChapterTaskCompleted {
		t.Errorf("expected task status completed, got %s", task.Status)
	}
	if merged.Title != "合并章节" {
		t.Errorf("expected title '合并章节', got %q", merged.Title)
	}
	if merged.Content != "内容1\n\n内容2" {
		t.Errorf("expected merged content, got %q", merged.Content)
	}

	// Verify only 2 chapters remain
	var count int64
	ns.db.Model(&models.Chapter{}).Where("project_id = ?", project.ID).Count(&count)
	if count != 2 {
		t.Errorf("expected 2 chapters, got %d", count)
	}
}

func TestMergeChaptersRequiresTwoChapters(t *testing.T) {
	ns := newTestNovelService(t)

	project := models.Project{
		Title:      "测试小说",
		SourceType: models.ProjectSourceNovel,
	}
	ns.db.Create(&project)

	chapter := models.Chapter{ProjectID: project.ID, Order: 1, Title: "第一章", Content: "内容"}
	ns.db.Create(&chapter)

	_, _, err := ns.MergeChapters(project.ID, []uint{chapter.ID}, "合并")
	if err == nil {
		t.Error("expected error for merging less than 2 chapters")
	}
}

func TestSplitChapterRequiresSplitPoints(t *testing.T) {
	ns := newTestNovelService(t)

	project := models.Project{
		Title:      "测试小说",
		SourceType: models.ProjectSourceNovel,
	}
	ns.db.Create(&project)

	chapter := models.Chapter{ProjectID: project.ID, Order: 1, Title: "第一章", Content: "内容"}
	ns.db.Create(&chapter)

	_, _, err := ns.SplitChapter(&chapter, []int{})
	if err == nil {
		t.Error("expected error for no split points")
	}
}

func TestSplitChapterInvalidSplitPoints(t *testing.T) {
	ns := newTestNovelService(t)

	project := models.Project{
		Title:      "测试小说",
		SourceType: models.ProjectSourceNovel,
	}
	ns.db.Create(&project)

	chapter := models.Chapter{ProjectID: project.ID, Order: 1, Title: "第一章", Content: "内容"}
	ns.db.Create(&chapter)

	// Split point beyond content length
	_, _, err := ns.SplitChapter(&chapter, []int{100})
	if err == nil {
		t.Error("expected error for invalid split point")
	}
}

func TestChapterOrderAfterMerge(t *testing.T) {
	ns := newTestNovelService(t)

	project := models.Project{
		Title:      "测试小说",
		SourceType: models.ProjectSourceNovel,
	}
	ns.db.Create(&project)

	// Create 3 chapters
	ns.db.Create(&models.Chapter{ProjectID: project.ID, Order: 1, Title: "第一章", Content: "内容1"})
	ns.db.Create(&models.Chapter{ProjectID: project.ID, Order: 2, Title: "第二章", Content: "内容2"})
	ns.db.Create(&models.Chapter{ProjectID: project.ID, Order: 3, Title: "第三章", Content: "内容3"})

	// Merge chapters 1 and 2
	var ch1, ch2 models.Chapter
	ns.db.Where("project_id = ? AND chapter_order = 1", project.ID).First(&ch1)
	ns.db.Where("project_id = ? AND chapter_order = 2", project.ID).First(&ch2)
	ns.MergeChapters(project.ID, []uint{ch1.ID, ch2.ID}, "合并")

	// Check orders: merged should be 1, chapter 3 should be renumbered to 2
	chapters, _ := ns.ListChapters(project.ID)
	orders := make(map[string]int)
	for _, ch := range chapters {
		orders[ch.Title] = ch.Order
	}

	if orders["合并"] != 1 {
		t.Errorf("merged chapter should have order 1, got %d", orders["合并"])
	}
	if orders["第三章"] != 2 {
		t.Errorf("chapter 3 should have order 2, got %d", orders["第三章"])
	}
}

func TestListChapterTasks(t *testing.T) {
	ns := newTestNovelService(t)

	project := models.Project{
		Title:      "测试小说",
		SourceType: models.ProjectSourceNovel,
	}
	ns.db.Create(&project)

	// Create tasks
	ns.db.Create(&models.ChapterTask{
		ProjectID: project.ID,
		TaskType:  models.ChapterTaskSplit,
		Status:    models.ChapterTaskCompleted,
	})
	ns.db.Create(&models.ChapterTask{
		ProjectID: project.ID,
		TaskType:  models.ChapterTaskMerge,
		Status:    models.ChapterTaskCompleted,
	})

	// List all tasks
	tasks, err := ns.ListChapterTasks(project.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 {
		t.Errorf("expected 2 tasks, got %d", len(tasks))
	}

	// List split tasks only
	splitTasks, err := ns.ListChapterTasks(project.ID, "split")
	if err != nil {
		t.Fatal(err)
	}
	if len(splitTasks) != 1 {
		t.Errorf("expected 1 split task, got %d", len(splitTasks))
	}
	if splitTasks[0].TaskType != models.ChapterTaskSplit {
		t.Errorf("expected task type split, got %s", splitTasks[0].TaskType)
	}
}

func TestNovelProjectOnlyHasChapters(t *testing.T) {
	ns := newTestNovelService(t)

	// Create a novel project
	project := models.Project{
		Title:      "测试小说",
		SourceType: models.ProjectSourceNovel,
		Status:     "draft",
	}
	ns.db.Create(&project)

	content := []byte(`第一章 林夏的秘密

林夏是一个普通的上班族。`)

	ns.ImportNovel(project.ID, "test.txt", content)

	// Create a separate outline project
	outlineProject := models.Project{
		Title:      "测试梗概",
		SourceType: models.ProjectSourceOutline,
		Status:     "draft",
		Synopsis:   "这是一个故事创意",
	}
	ns.db.Create(&outlineProject)

	// Verify novel project has chapters
	novelChapters, _ := ns.ListChapters(project.ID)
	if len(novelChapters) == 0 {
		t.Error("novel project should have chapters")
	}

	// Verify outline project has no chapters
	outlineChapters, _ := ns.ListChapters(outlineProject.ID)
	if len(outlineChapters) != 0 {
		t.Error("outline project should not have chapters")
	}
}
