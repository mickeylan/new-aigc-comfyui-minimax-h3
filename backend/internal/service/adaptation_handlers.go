package service

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"comfyui-console/internal/models"
)

func (s *Service) HandleGetAdaptationStrategy(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	v, err := s.Adaptations.GetStrategy(p.ID)
	if err == gorm.ErrRecordNotFound {
		c.JSON(404, gin.H{"error": "strategy not found"})
		return
	}
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, v)
}
func (s *Service) HandleSaveAdaptationStrategy(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	var req models.AdaptationStrategy
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "invalid request"})
		return
	}
	v, err := s.Adaptations.SaveStrategy(p.ID, req)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, v)
}
func (s *Service) HandleListAdaptations(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	v, err := s.Adaptations.List(p.ID)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"adaptations": v})
}
func (s *Service) HandleGenerateAdaptations(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	v, err := s.Adaptations.Generate(p.ID)
	if err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"adaptations": v})
}
func episodeParam(c *gin.Context) (int, bool) {
	n, e := strconv.Atoi(c.Param("episode"))
	if e != nil || n < 1 {
		c.JSON(400, gin.H{"error": "invalid episode"})
		return 0, false
	}
	return n, true
}
func (s *Service) HandleUpdateAdaptation(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	n, ok := episodeParam(c)
	if !ok {
		return
	}
	var req map[string]any
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "invalid request"})
		return
	}
	v, err := s.Adaptations.Update(p.ID, n, req)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, v)
}
func (s *Service) HandleApproveAdaptations(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	var req struct {
		Episodes []int `json:"episodes"`
	}
	if c.Param("episode") != "" {
		n, o := episodeParam(c)
		if !o {
			return
		}
		req.Episodes = []int{n}
	} else if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "invalid request"})
		return
	}
	if err := s.Adaptations.Approve(p.ID, req.Episodes); err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}
func (s *Service) HandleGenerateAdaptationAssets(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	n, ok := episodeParam(c)
	if !ok {
		return
	}
	if err := s.Projects.SyncEpisodeProductionEntities(p.ID, n); err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	var scenes []models.Scene
	if err := s.DB.Where("project_id = ? AND episode_n = ?", p.ID, n).Order("`order`").Find(&scenes).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	// 角色档案必须同时参考当前生产场景、该集对应小说原文和已审核故事圣经。
	// 仅传 scenes 会丢失原文中的年龄、种族等硬事实，导致模型凭空补全。
	var adaptation models.EpisodeAdaptation
	_ = s.DB.Where("project_id = ? AND episode_n = ?", p.ID, n).First(&adaptation).Error
	var bible models.StoryBible
	_ = s.DB.Where("project_id = ?", p.ID).First(&bible).Error
	var sourceChapterIDs []uint
	_ = json.Unmarshal([]byte(adaptation.SourceChapterIDs), &sourceChapterIDs)
	var chapters []models.Chapter
	chapterQuery := s.DB.Where("project_id = ?", p.ID).Order("chapter_order")
	if len(sourceChapterIDs) > 0 {
		chapterQuery = chapterQuery.Where("id IN ?", sourceChapterIDs)
	} else if adaptation.ChapterStart > 0 && adaptation.ChapterEnd >= adaptation.ChapterStart {
		chapterQuery = chapterQuery.Where("chapter_order BETWEEN ? AND ?", adaptation.ChapterStart, adaptation.ChapterEnd)
	} else {
		chapterQuery = chapterQuery.Where("1 = 0")
	}
	if err := chapterQuery.Find(&chapters).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	profileContext := struct {
		Instruction string                   `json:"instruction"`
		Episode     int                      `json:"episode"`
		Adaptation  models.EpisodeAdaptation `json:"adaptation"`
		StoryBible  models.StoryBible        `json:"story_bible"`
		Chapters    []models.Chapter         `json:"source_chapters"`
		Scenes      []models.Scene           `json:"production_scenes"`
	}{
		Instruction: "小说原文和已审核故事圣经是角色年龄、性别、种族、身份、外貌的权威来源；不得猜测或改写。",
		Episode:     n,
		Adaptation:  adaptation,
		StoryBible:  bible,
		Chapters:    chapters,
		Scenes:      scenes,
	}
	contextJSON, _ := json.Marshal(profileContext)
	var characters []models.Character
	// “AI补全”也负责重新生成尚未审核的草稿/驳回档案。旧逻辑只检查 appearance
	// 是否为空，导致错误但非空的档案永远显示“补全0个”且无法纠正。
	profileIncomplete := `(appearance = '' OR appearance IS NULL OR personality = '' OR personality IS NULL OR background = '' OR background IS NULL OR relationships = '' OR relationships IS NULL OR emotions = '' OR emotions IS NULL OR habits = '' OR habits IS NULL OR wardrobe_detail = '' OR wardrobe_detail IS NULL OR lighting_mood = '' OR lighting_mood IS NULL OR color_palette = '' OR color_palette IS NULL)`
	if err := s.DB.Where("project_id = ? AND (profile_status <> ? OR "+profileIncomplete+")", p.ID, models.ProfileStatusApproved).Find(&characters).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	// 已审核档案通常保持不动；但若其年龄已与本集权威小说原文冲突，也必须进入修复队列。
	var approvedCharacters []models.Character
	if err := s.DB.Where("project_id = ? AND profile_status = ?", p.ID, models.ProfileStatusApproved).Find(&approvedCharacters).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	for i := range approvedCharacters {
		if err := validateProfileAgeAgainstSources(approvedCharacters[i].Appearance, &models.Character{Name: approvedCharacters[i].Name}, string(contextJSON)); err != nil {
			repair := approvedCharacters[i]
			repair.Appearance = "" // 不把已判定错误的年龄再次作为“不可改写事实”发给模型。
			repair.Trait = ""
			characters = append(characters, repair)
		}
	}
	generatedCharacters := 0
	for i := range characters {
		if err := s.CharacterProfiles.GenerateProfile(&characters[i], p, string(contextJSON)); err != nil {
			c.JSON(502, gin.H{"error": fmt.Sprintf("角色「%s」AI档案生成失败: %v", characters[i].Name, err)})
			return
		}
		generatedCharacters++
	}
	var assets []models.Asset
	if err := s.DB.Where("project_id = ? AND (description = '' OR description IS NULL)", p.ID).Find(&assets).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	generatedAssets := 0
	for i := range assets {
		description, err := s.Projects.RedesignAssetDescription(p, assets[i].Kind, assets[i].Name, fmt.Sprintf("依据第%d集已生成场景补充资产设定。场景上下文：%s", n, string(contextJSON)))
		if err != nil {
			c.JSON(502, gin.H{"error": fmt.Sprintf("%s「%s」AI描述生成失败: %v", assets[i].Kind, assets[i].Name, err)})
			return
		}
		if err := s.DB.Model(&assets[i]).Update("description", description).Error; err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		generatedAssets++
	}
	c.JSON(200, gin.H{"ok": true, "characters": generatedCharacters, "assets": generatedAssets})
}

func (s *Service) HandleSyncAdaptationAssets(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	n, ok := episodeParam(c)
	if !ok {
		return
	}
	if err := s.Projects.SyncEpisodeProductionEntities(p.ID, n); err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (s *Service) HandleGenerateAdaptationScript(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	n, ok := episodeParam(c)
	if !ok {
		return
	}
	v, err := s.Adaptations.GenerateScript(p.ID, n)
	if err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, v)
}
func (s *Service) HandleBatchGenerateAdaptationScripts(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	var req struct {
		Episodes []int `json:"episodes"`
	}
	if c.ShouldBindJSON(&req) != nil || len(req.Episodes) == 0 {
		c.JSON(400, gin.H{"error": "episodes are required"})
		return
	}
	rows := make([]*models.EpisodeAdaptation, 0, len(req.Episodes))
	for _, n := range req.Episodes {
		v, err := s.Adaptations.GenerateScript(p.ID, n)
		if err != nil {
			c.JSON(409, gin.H{"error": err.Error(), "completed": rows})
			return
		}
		rows = append(rows, v)
	}
	c.JSON(200, gin.H{"adaptations": rows})
}
func (s *Service) HandleReviewAdaptation(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	n, ok := episodeParam(c)
	if !ok {
		return
	}
	var req struct {
		OverrideReason string `json:"override_reason"`
	}
	if c.Request.ContentLength > 0 && c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "invalid request"})
		return
	}
	v, err := s.Adaptations.Review(p.ID, n, req.OverrideReason)
	if err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, v)
}
func (s *Service) HandleBatchReviewAdaptations(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	var req struct {
		Episodes []int `json:"episodes"`
	}
	if c.ShouldBindJSON(&req) != nil || len(req.Episodes) == 0 {
		c.JSON(400, gin.H{"error": "episodes are required"})
		return
	}
	rows := make([]*models.EpisodeAdaptation, 0, len(req.Episodes))
	for _, n := range req.Episodes {
		v, err := s.Adaptations.Review(p.ID, n, "")
		if err != nil {
			c.JSON(409, gin.H{"error": err.Error(), "completed": rows})
			return
		}
		rows = append(rows, v)
	}
	c.JSON(200, gin.H{"adaptations": rows})
}
func (s *Service) HandleAdaptationContext(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	n, ok := episodeParam(c)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(c.Query("max_chars"))
	v, err := s.Adaptations.BuildContext(p.ID, n, limit)
	if err != nil {
		c.JSON(404, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, v)
}
func (s *Service) HandleNovelUsage(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	v, err := s.Adaptations.Usage(p.ID)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, v)
}
