package service

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"

	"comfyui-console/internal/models"
)

// NovelService 处理小说导入、章节识别与操作
type NovelService struct {
	db *gorm.DB
}

// NewNovelService 创建小说服务
func NewNovelService(db *gorm.DB) *NovelService {
	return &NovelService{db: db}
}

// DetectedChapter 识别出的章节结构
type DetectedChapter struct {
	Title   string // 章节标题（可能为空）
	Content string // 章节正文
	Start   int    // 在原文本中的起始位置
	End     int    // 在原文本中的结束位置
}

// chapterPattern 章节识别正则表达式
// 支持：第X章、Chapter X、CHAPTER X、第X回、第X节 等常见格式
var chapterPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?m)^第[一二三四五六七八九十百千零〇\d]+章[　\s]*(.+)?$`),        // 第1章、第12章标题
	regexp.MustCompile(`(?m)^第[一二三四五六七八九十百千零〇\d]+回[　\s]*(.+)?$`),        // 第1回
	regexp.MustCompile(`(?m)^第[一二三四五六七八九十百千零〇\d]+节[　\s]*(.+)?$`),        // 第1节
	regexp.MustCompile(`(?im)^Chapter\s+(\d+)[　\s:]*(.+)?$`),            // Chapter 1
	regexp.MustCompile(`(?im)^CHAPTER\s+(\d+)[　\s:]*(.+)?$`),            // CHAPTER 1
	regexp.MustCompile(`(?im)^第[一二三四五六七八九十百千零〇\d]+卷[　\s]*(.+)?$`),       // 第1卷
	regexp.MustCompile(`(?m)^#{1,3}\s*第?[一二三四五六七八九十百千零〇\d]+[章节回节卷].*$`), // # 第1章、## Chapter 1
	regexp.MustCompile(`(?m)^\\[第\\][一二三四五六七八九十百千零〇\\d\\]+[章回节].*$`),    // Markdown: \[第1章\]
}

// isEmptyLine 判断是否为空行或仅包含空白字符
func isEmptyLine(s string) bool {
	for _, r := range s {
		if r != ' ' && r != '\t' && r != '\r' && r != '\n' {
			return false
		}
	}
	return true
}

// CleanUTF8 处理文本编码问题，返回干净的 UTF-8 文本
func (s *NovelService) CleanUTF8(data []byte) (string, error) {
	// 检查是否已经是有效的 UTF-8
	if utf8.Valid(data) {
		// 移除 BOM (Byte Order Mark)
		text := strings.TrimPrefix(string(data), "\ufeff")
		return text, nil
	}

	return "", fmt.Errorf("文件不是有效的 UTF-8 编码，请先转换为 UTF-8 后重新上传")
}

// DetectChapters 从文本中识别章节
func (s *NovelService) DetectChapters(text string) []DetectedChapter {
	var chapters []DetectedChapter
	textLen := len(text)

	// 预处理：统一换行符
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	lines := strings.Split(text, "\n")
	var currentChapter *DetectedChapter
	var currentContent strings.Builder
	lineOffset := 0

	for _, line := range lines {
		isChapterTitle := false
		var chapterTitle string

		// 检查是否符合章节标题模式
		for _, pattern := range chapterPatterns {
			matches := pattern.FindStringSubmatch(line)
			if len(matches) > 0 {
				isChapterTitle = true
				// matches[0] 是完整匹配，matches[1] 是捕获的标题（如果有）
				if len(matches) > 1 && strings.TrimSpace(matches[1]) != "" {
					chapterTitle = strings.TrimSpace(matches[1])
				} else {
					// 没有捕获到标题，使用默认标题
					chapterTitle = strings.TrimSpace(matches[0])
				}
				break
			}
		}

		if isChapterTitle {
			// 保存上一个章节（如果有内容）
			if currentChapter != nil && currentContent.Len() > 0 {
				currentChapter.Content = strings.TrimSpace(currentContent.String())
				currentChapter.End = lineOffset
				chapters = append(chapters, *currentChapter)
			}

			// 开始新章节
			currentChapter = &DetectedChapter{
				Title: chapterTitle,
				Start: lineOffset,
			}
			currentContent.Reset()
		} else {
			// 添加到当前章节内容
			if currentContent.Len() > 0 || !isEmptyLine(line) {
				if currentContent.Len() > 0 {
					currentContent.WriteString("\n")
				}
				currentContent.WriteString(line)
			}
		}

		// 更新行偏移
		lineOffset += len(line) + 1 // +1 for newline
	}

	// 保存最后一个章节
	if currentChapter != nil && currentContent.Len() > 0 {
		currentChapter.Content = strings.TrimSpace(currentContent.String())
		currentChapter.End = textLen
		chapters = append(chapters, *currentChapter)
	}

	// 如果没有识别到任何章节，将整个文本作为一个章节
	if len(chapters) == 0 && textLen > 0 {
		chapters = append(chapters, DetectedChapter{
			Title:   "全文",
			Content: strings.TrimSpace(text),
			Start:   0,
			End:     textLen,
		})
	}

	return chapters
}

// CountWords 统计字数（中文按字符计数，英文按单词计数）
func (s *NovelService) CountWords(text string) int {
	words := 0
	for _, r := range text {
		if r >= 0x4E00 && r <= 0x9FA5 { // CJK统一汉字范围
			words++
		} else if r >= 0x3000 && r <= 0x303F { // CJK标点符号
			// 不计入标点
		} else if r >= 0xFF00 && r <= 0xFFEF { // 全角ASCII
			words++
		} else if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' {
			words++
		} else if r == ' ' || r == '\t' || r == '\n' {
			// 不计入空白
		} else {
			words++
		}
	}
	return words
}

// ComputeFileHash 计算文件内容哈希
func (s *NovelService) ComputeFileHash(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// ImportNovel 上传并处理小说文件
func (s *NovelService) ImportNovel(projectID uint, filename string, data []byte) (*models.Project, error) {
	var project models.Project
	if err := s.db.First(&project, projectID).Error; err != nil {
		return nil, fmt.Errorf("project not found: %w", err)
	}

	// 验证项目类型
	if project.SourceType != models.ProjectSourceNovel {
		return nil, fmt.Errorf("project is not a novel project (source_type=%s)", project.SourceType)
	}

	// 检查是否重复上传（通过文件哈希）
	fileHash := s.ComputeFileHash(data)
	if project.NovelFileHash == fileHash && project.ImportStatus == models.NovelImportCompleted {
		return nil, fmt.Errorf("duplicate file: this file has already been imported")
	}

	// 更新导入状态为处理中
	project.ImportStatus = models.NovelImportProcessing
	project.ImportError = ""
	project.NovelFileHash = fileHash
	if err := s.db.Save(&project).Error; err != nil {
		return nil, fmt.Errorf("update project status: %w", err)
	}

	// UTF-8 清洗
	cleanText, cleanErr := s.CleanUTF8(data)
	if cleanErr != nil {
		project.ImportStatus = models.NovelImportFailed
		project.ImportError = cleanErr.Error()
		_ = s.db.Save(&project).Error
		return nil, cleanErr
	}

	// 章节识别
	detectedChapters := s.DetectChapters(cleanText)

	// 统计总字数
	totalWords := 0
	for _, ch := range detectedChapters {
		totalWords += s.CountWords(ch.Content)
	}

	// 增量重导入：相同内容哈希复用分析；仅让变更来源及其下游失效。
	var oldChapters []models.Chapter
	if err := s.db.Where("project_id = ?", projectID).Find(&oldChapters).Error; err != nil {
		return nil, err
	}
	byHash := make(map[string][]models.Chapter)
	for _, old := range oldChapters {
		byHash[old.ContentHash] = append(byHash[old.ContentHash], old)
	}
	changed := len(oldChapters) != len(detectedChapters)
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("project_id = ?", projectID).Delete(&models.Chapter{}).Error; err != nil {
			return err
		}
		for i, ch := range detectedChapters {
			hash := s.ComputeFileHash([]byte(ch.Content))
			chapter := models.Chapter{ProjectID: projectID, Order: i + 1, Title: ch.Title, Content: ch.Content, WordCount: s.CountWords(ch.Content), IdentifiedBy: "rule", ContentHash: hash, AnalysisStatus: "pending"}
			if matches := byHash[hash]; len(matches) > 0 {
				old := matches[0]
				byHash[hash] = matches[1:]
				chapter.ID = old.ID
				chapter.Summary, chapter.AnalysisJSON, chapter.AnalysisStatus = old.Summary, old.AnalysisJSON, old.AnalysisStatus
				chapter.AnalysisVersion, chapter.AnalysisHash, chapter.AnalysisError = old.AnalysisVersion, old.AnalysisHash, old.AnalysisError
			} else {
				changed = true
			}
			if err := tx.Create(&chapter).Error; err != nil {
				return err
			}
		}
		if changed {
			if err := tx.Model(&models.StoryArc{}).Where("project_id = ?", projectID).Update("status", "stale").Error; err != nil {
				return err
			}
			if err := tx.Model(&models.StoryBible{}).Where("project_id = ?", projectID).Update("status", "stale").Error; err != nil {
				return err
			}
			if err := tx.Model(&models.EpisodeAdaptation{}).Where("project_id = ?", projectID).Update("status", "stale").Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		project.ImportStatus = models.NovelImportFailed
		project.ImportError = err.Error()
		_ = s.db.Save(&project).Error
		return nil, fmt.Errorf("replace chapters: %w", err)
	}

	// 更新项目状态
	project.NovelWordCount = totalWords
	project.ImportStatus = models.NovelImportCompleted
	if err := s.db.Save(&project).Error; err != nil {
		return nil, fmt.Errorf("update project: %w", err)
	}

	return &project, nil
}

// ListChapters 获取项目的章节列表
func (s *NovelService) ListChapters(projectID uint) ([]models.Chapter, error) {
	var chapters []models.Chapter
	err := s.db.Where("project_id = ?", projectID).
		Order("chapter_order ASC").
		Find(&chapters).Error
	return chapters, err
}

// GetChapter 获取单个章节
func (s *NovelService) GetChapter(projectID, chapterID uint) (*models.Chapter, error) {
	var chapter models.Chapter
	err := s.db.Where("id = ? AND project_id = ?", chapterID, projectID).First(&chapter).Error
	if err != nil {
		return nil, err
	}
	return &chapter, nil
}

// UpdateChapterTitle 更新章节标题
func (s *NovelService) UpdateChapterTitle(chapter *models.Chapter, newTitle string) error {
	updates := map[string]interface{}{
		"title":           newTitle,
		"manually_edited": true,
	}
	return s.db.Model(chapter).Updates(updates).Error
}

// UpdateChapterOrder 更新章节顺序
func (s *NovelService) UpdateChapterOrder(chapter *models.Chapter, newOrder int) error {
	updates := map[string]interface{}{
		"chapter_order":   newOrder,
		"manually_edited": true,
	}
	return s.db.Model(chapter).Updates(updates).Error
}

// ReorderChapters 批量更新章节顺序
func (s *NovelService) ReorderChapters(projectID uint, chapterOrders map[uint]int) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		for chapterID, order := range chapterOrders {
			if err := tx.Model(&models.Chapter{}).
				Where("id = ? AND project_id = ?", chapterID, projectID).
				Updates(map[string]interface{}{
					"chapter_order":   order,
					"manually_edited": true,
				}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// SplitChapter 拆分章节
func (s *NovelService) SplitChapter(chapter *models.Chapter, splitPoints []int) (*models.ChapterTask, []models.Chapter, error) {
	if len(splitPoints) < 1 {
		return nil, nil, fmt.Errorf("at least one split point is required")
	}

	// 验证拆分点
	content := chapter.Content
	for _, point := range splitPoints {
		if point < 0 || point > len(content) {
			return nil, nil, fmt.Errorf("invalid split point: %d", point)
		}
	}

	var task models.ChapterTask
	var newChapters []models.Chapter

	err := s.db.Transaction(func(tx *gorm.DB) error {
		// 创建拆分任务
		task = models.ChapterTask{
			ProjectID:       chapter.ProjectID,
			TaskType:        models.ChapterTaskSplit,
			Status:          models.ChapterTaskPending,
			SourceChapterID: &chapter.ID,
		}
		if err := tx.Create(&task).Error; err != nil {
			return err
		}

		// 执行拆分
		sort.Ints(splitPoints)
		parts := splitContent(content, splitPoints)

		// 更新任务状态为处理中
		if err := tx.Model(&task).Update("task_status", models.ChapterTaskProcessing).Error; err != nil {
			return err
		}

		// 创建新章节
		resultIDs := make([]string, len(parts))
		baseOrder := chapter.Order

		for i, part := range parts {
			newChapter := models.Chapter{
				ProjectID:      chapter.ProjectID,
				Order:          baseOrder + i,
				Title:          fmt.Sprintf("%s (第%d部分)", chapter.Title, i+1),
				Content:        part,
				WordCount:      s.CountWords(part),
				IdentifiedBy:   "manual",
				ManuallyEdited: true,
				ContentHash:    s.ComputeFileHash([]byte(part)),
				AnalysisStatus: "pending",
			}
			if err := tx.Create(&newChapter).Error; err != nil {
				return err
			}
			newChapters = append(newChapters, newChapter)
			resultIDs[i] = fmt.Sprintf("%d", newChapter.ID)
		}

		// 标记原始章节为已删除（逻辑删除，保留记录）
		if err := tx.Model(&models.Chapter{}).Where("id = ?", chapter.ID).
			Update("manually_edited", true).Error; err != nil {
			return err
		}

		// 删除原始章节
		if err := tx.Delete(chapter).Error; err != nil {
			return err
		}

		// 重新编号后续章节
		if err := tx.Model(&models.Chapter{}).
			Where("project_id = ? AND chapter_order > ?", chapter.ProjectID, baseOrder).
			Update("chapter_order", gorm.Expr("chapter_order + ?", len(parts)-1)).Error; err != nil {
			return err
		}

		// 更新任务状态
		task.Status = models.ChapterTaskCompleted
		task.ResultIDs = strings.Join(resultIDs, ",")
		return tx.Save(&task).Error
	})

	if err != nil {
		return nil, nil, err
	}

	return &task, newChapters, nil
}

// MergeChapters 合并章节
func (s *NovelService) MergeChapters(projectID uint, chapterIDs []uint, newTitle string) (*models.ChapterTask, *models.Chapter, error) {
	if len(chapterIDs) < 2 {
		return nil, nil, fmt.Errorf("at least two chapters are required for merging")
	}

	var chapters []models.Chapter
	if err := s.db.Where("id IN ? AND project_id = ?", chapterIDs, projectID).
		Order("chapter_order ASC").Find(&chapters).Error; err != nil {
		return nil, nil, err
	}

	if len(chapters) != len(chapterIDs) {
		return nil, nil, fmt.Errorf("some chapters not found")
	}

	var task models.ChapterTask
	var mergedChapter *models.Chapter

	err := s.db.Transaction(func(tx *gorm.DB) error {
		// 创建合并任务
		targetIDs := make([]string, len(chapterIDs))
		for i, id := range chapterIDs {
			targetIDs[i] = fmt.Sprintf("%d", id)
		}
		task = models.ChapterTask{
			ProjectID:        projectID,
			TaskType:         models.ChapterTaskMerge,
			Status:           models.ChapterTaskPending,
			TargetChapterIDs: strings.Join(targetIDs, ","),
		}
		if err := tx.Create(&task).Error; err != nil {
			return err
		}

		// 更新任务状态为处理中
		if err := tx.Model(&task).Update("task_status", models.ChapterTaskProcessing).Error; err != nil {
			return err
		}

		// 合并内容
		var contentBuilder strings.Builder
		minOrder := chapters[0].Order
		for i, ch := range chapters {
			if i > 0 {
				contentBuilder.WriteString("\n\n")
			}
			contentBuilder.WriteString(ch.Content)
			if ch.Order < minOrder {
				minOrder = ch.Order
			}
		}

		// 创建合并后的章节
		if newTitle == "" {
			newTitle = fmt.Sprintf("第%d至%d章合并", chapters[0].Order, chapters[len(chapters)-1].Order)
		}
		mergedContent := contentBuilder.String()
		mergedChapter = &models.Chapter{
			ProjectID:      projectID,
			Order:          minOrder,
			Title:          newTitle,
			Content:        mergedContent,
			WordCount:      s.CountWords(mergedContent),
			IdentifiedBy:   "manual",
			ManuallyEdited: true,
			ContentHash:    s.ComputeFileHash([]byte(mergedContent)),
			AnalysisStatus: "pending",
		}
		if err := tx.Create(mergedChapter).Error; err != nil {
			return err
		}

		// 删除被合并的章节
		if err := tx.Where("id IN ?", chapterIDs).Delete(&models.Chapter{}).Error; err != nil {
			return err
		}

		// 重新编号后续章节
		if err := tx.Model(&models.Chapter{}).
			Where("project_id = ? AND chapter_order > ?", projectID, minOrder).
			Update("chapter_order", gorm.Expr("chapter_order - ?", len(chapters)-1)).Error; err != nil {
			return err
		}

		// 更新任务状态
		task.Status = models.ChapterTaskCompleted
		task.ResultIDs = fmt.Sprintf("%d", mergedChapter.ID)
		return tx.Save(&task).Error
	})

	if err != nil {
		return nil, nil, err
	}

	return &task, mergedChapter, nil
}

// GetImportStatus 获取项目导入状态
func (s *NovelService) GetImportStatus(projectID uint) (*models.Project, error) {
	var project models.Project
	if err := s.db.First(&project, projectID).Error; err != nil {
		return nil, err
	}
	return &project, nil
}

// ListChapterTasks 获取章节任务列表
func (s *NovelService) ListChapterTasks(projectID uint, taskType string) ([]models.ChapterTask, error) {
	query := s.db.Where("project_id = ?", projectID)
	if taskType != "" {
		query = query.Where("task_type = ?", taskType)
	}
	var tasks []models.ChapterTask
	err := query.Order("created_at DESC").Find(&tasks).Error
	return tasks, err
}

// splitContent 根据拆分点分割内容
func splitContent(content string, splitPoints []int) []string {
	var parts []string
	prev := 0
	for _, point := range splitPoints {
		parts = append(parts, strings.TrimSpace(content[prev:point]))
		prev = point
	}
	parts = append(parts, strings.TrimSpace(content[prev:]))
	return parts
}
