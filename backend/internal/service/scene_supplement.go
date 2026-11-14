package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const sceneSupplementSkillCode = "scene-supplement-shot"

// sceneSupplementSkill drafts one inserted Scene. Its wording is user-approved; raise Version
// whenever the prompt changes so InitSystemSkills publishes the new revision.
var sceneSupplementSkill = models.Skill{
	Name: "补镜导演", Code: sceneSupplementSkillCode, Operation: sceneSupplementSkillCode, Version: 1,
	Description:    "补镜描述是唯一剧情来源，相邻场景只作连续性参考；输出待用户确认的新Scene草稿",
	Stage:          models.SkillStageStoryboard,
	PromptTemplate: "前一场（仅作连续性参考）：{{previous_scene}}\n前一场结尾状态：{{previous_end_state}}\n后一场（仅作连续性参考）：{{next_scene}}\n补镜描述（唯一剧情来源）：{{supplement_brief}}",
	SystemPrompt:   pacing(`你是分镜编剧。用户要在两场之间插入一个补镜，补镜描述是这一场唯一的剧情来源。前一场和后一场只用来保持人物位置、服装、伤势、道具和光线的连续，不得续写、复述或提前演出它们的内容。只输出JSON对象：{"title":标题,"content":补镜正文（中文，写清可见的人物动作和画面，{scene_typical_min}到{shot_max}秒能演完）,"duration":秒数,"characters":出场人物数组,"dialogues":[{"character":说话人,"text":台词}]}。台词只能来自补镜描述里明确写出的话，描述里没有台词就输出空数组。地点和时间默认与前一场相同，除非补镜描述另有说明。不要输出Markdown、解释或思考过程。`),
	IsSystem:       true, Enabled: true, SortOrder: 21,
}

var errSceneSupplementStale = errors.New("插入位置附近的场景已变化，请重新生成补镜草稿")

type sceneSupplementDialogue struct {
	Character string `json:"character"`
	Text      string `json:"text"`
}

type sceneSupplementDraft struct {
	Title      string                    `json:"title"`
	Content    string                    `json:"content"`
	Duration   float64                   `json:"duration"`
	Characters []string                  `json:"characters"`
	Dialogues  []sceneSupplementDialogue `json:"dialogues"`
	Location   string                    `json:"location"`
}

// supplementNeighbors loads the active Scene the supplement goes after and the Scene that
// currently follows it (nil at the end of the episode).
func supplementNeighbors(db *gorm.DB, projectID, sceneID uint) (*models.Scene, *models.Scene, error) {
	var anchor models.Scene
	if err := db.Where("id = ? AND project_id = ? AND (archived = ? OR archived IS NULL)", sceneID, projectID, false).First(&anchor).Error; err != nil {
		return nil, nil, fmt.Errorf("插入位置的场景不存在或已归档")
	}
	generation, err := latestEpisodeGeneration(db, projectID, anchor.EpisodeN)
	if err != nil {
		return nil, nil, err
	}
	if anchor.Generation != generation {
		return nil, nil, fmt.Errorf("只能在该集当前版本的场景之后插入补镜")
	}
	var next models.Scene
	err = db.Where("project_id = ? AND generation = ? AND episode_n = ? AND `order` > ? AND (archived = ? OR archived IS NULL)", projectID, anchor.Generation, anchor.EpisodeN, anchor.Order, false).
		Order("`order`, id").First(&next).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &anchor, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	return &anchor, &next, nil
}

// supplementSourceHash pins the insertion point so a draft is never applied after the
// neighbours were reordered, edited or archived.
func supplementSourceHash(anchor, next *models.Scene) string {
	parts := []string{fmt.Sprint(anchor.ID, "/", anchor.Order, "/", anchor.Generation), anchor.Content, anchor.LocationName}
	if next != nil {
		parts = append(parts, fmt.Sprint(next.ID, "/", next.Order), next.Content)
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

func supplementSceneText(scene *models.Scene, empty string) string {
	if scene == nil {
		return empty
	}
	return strings.TrimSpace(scene.Title + "\n" + scene.Content)
}

// supplementPreviousEndState renders the anchor's saved or authored end state as seen by a
// Scene that continues in the same place, so the model can match posture, props and injuries.
func supplementPreviousEndState(db *gorm.DB, anchor *models.Scene) string {
	follower := models.Scene{ProjectID: anchor.ProjectID, LocationName: anchor.LocationName, Characters: anchor.Characters, VisibleCharacters: anchor.VisibleCharacters, CharacterRolesSet: anchor.CharacterRolesSet}
	if block := inheritedSceneStartStateFrom(db, &follower, anchor); block != "" {
		return block
	}
	return "无"
}

func supplementSpeechKey(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			return -1
		}
		return r
	}, text)
}

// normalizeSceneSupplementDraft enforces the Skill contract deterministically: every line of
// dialogue must be written in the brief, the duration stays inside one H3 generation, and the
// location defaults to the previous Scene. Dialogue longer than the duration is a warning only.
func normalizeSceneSupplementDraft(draft sceneSupplementDraft, brief string, anchor *models.Scene) (sceneSupplementDraft, []string, error) {
	draft.Title = strings.TrimSpace(draft.Title)
	draft.Content = strings.TrimSpace(draft.Content)
	if draft.Content == "" {
		return draft, nil, fmt.Errorf("补镜正文为空")
	}
	if draft.Title == "" {
		draft.Title = "补镜"
	}
	briefKey := supplementSpeechKey(brief)
	dialogues := make([]sceneSupplementDialogue, 0, len(draft.Dialogues))
	for i, d := range draft.Dialogues {
		d.Character, d.Text = strings.TrimSpace(d.Character), strings.TrimSpace(d.Text)
		if d.Text == "" {
			continue
		}
		if key := supplementSpeechKey(d.Text); key == "" || !strings.Contains(briefKey, key) {
			return draft, nil, fmt.Errorf("第%d句台词「%s」不在补镜描述中", i+1, d.Text)
		}
		dialogues = append(dialogues, d)
	}
	draft.Dialogues = dialogues
	names := []string{}
	seen := map[string]bool{}
	for _, name := range draft.Characters {
		if name = strings.TrimSpace(name); name != "" && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	draft.Characters = names
	if strings.TrimSpace(draft.Location) == "" {
		draft.Location = anchor.LocationName
	}
	draft.Location = strings.TrimSpace(draft.Location)
	var warnings []string
	if draft.Duration < minSceneVideoDuration || draft.Duration > maxShotDuration {
		clamped := maxShotDuration
		if draft.Duration < minSceneVideoDuration {
			clamped = minTypicalSceneSeconds
		}
		warnings = append(warnings, fmt.Sprintf("时长%s秒超出%s到%s秒，已调整为%s秒", pacingNumber(draft.Duration), pacingNumber(minSceneVideoDuration), pacingNumber(maxShotDuration), pacingNumber(clamped)))
		draft.Duration = clamped
	}
	items := make([]models.Dialogue, 0, len(dialogues))
	for _, d := range dialogues {
		items = append(items, models.Dialogue{Character: d.Character, SpeechType: "dialogue", Text: d.Text})
	}
	if speech := modelDialoguesMinDuration(items); speech > draft.Duration {
		warnings = append(warnings, fmt.Sprintf("台词预计%.1f秒，超过补镜时长%s秒（不阻止插入）", speech, pacingNumber(draft.Duration)))
	}
	return draft, warnings, nil
}

func parseSceneSupplementDraft(raw string) (sceneSupplementDraft, error) {
	var draft sceneSupplementDraft
	if err := json.Unmarshal([]byte(extractJSON(raw)), &draft); err != nil {
		return draft, fmt.Errorf("返回的不是合法JSON: %w", err)
	}
	return draft, nil
}

// HandleSceneSupplementDraft asks the supplement Skill for a new Scene to insert after :sid and
// returns it for review. Nothing is persisted and no image or video is generated.
func (s *Service) HandleSceneSupplementDraft(c *gin.Context) {
	scene, ok := s.loadScene(c)
	if !ok {
		return
	}
	var req struct {
		Brief string `json:"brief"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Brief) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请填写补镜描述"})
		return
	}
	if s.Skills == nil || s.TextProviderFact == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "AI 文本服务未配置"})
		return
	}
	anchor, next, err := supplementNeighbors(s.DB, scene.ProjectID, scene.ID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	brief := strings.TrimSpace(req.Brief)
	output, err := s.Skills.ChatWithConfiguredOrFallbackSkill(scene.ProjectID, sceneSupplementSkill.Stage, sceneSupplementSkillCode, s.TextProviderFact, "只输出完整合法JSON，不要Markdown或解释。", "根据补镜描述生成一个待插入的场景草稿。", map[string]string{
		"previous_scene":     supplementSceneText(anchor, "无"),
		"previous_end_state": supplementPreviousEndState(s.DB, anchor),
		"next_scene":         supplementSceneText(next, "无（插入位置为本集末尾）"),
		"supplement_brief":   brief,
	})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "补镜生成失败: " + err.Error()})
		return
	}
	draft, err := parseSceneSupplementDraft(output)
	var warnings []string
	if err == nil {
		draft, warnings, err = normalizeSceneSupplementDraft(draft, brief, anchor)
	}
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "补镜草稿未通过校验：" + err.Error(), "raw_output": output})
		return
	}
	c.JSON(http.StatusOK, gin.H{"draft": draft, "warnings": warnings, "source_hash": supplementSourceHash(anchor, next), "skill_code": sceneSupplementSkillCode, "persisted": false})
}

// ApplySceneSupplement inserts the confirmed draft right after the anchor Scene. Later Scenes
// shift by one, the new Scene inherits the anchor's location and episode version, and
// continuity that followed the anchor is invalidated because its predecessor changed.
func (s *Service) ApplySceneSupplement(projectID, sceneID uint, brief, sourceHash string, draft sceneSupplementDraft) (*models.Scene, error) {
	var created models.Scene
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		anchor, next, err := supplementNeighbors(tx, projectID, sceneID)
		if err != nil {
			return err
		}
		if sourceHash == "" || sourceHash != supplementSourceHash(anchor, next) {
			return errSceneSupplementStale
		}
		draft, _, err = normalizeSceneSupplementDraft(draft, brief, anchor)
		if err != nil {
			return err
		}
		var later []models.Scene
		if err := tx.Where("project_id = ? AND generation = ? AND episode_n = ? AND `order` > ? AND (archived = ? OR archived IS NULL)", projectID, anchor.Generation, anchor.EpisodeN, anchor.Order, false).
			Order("`order` DESC, id DESC").Find(&later).Error; err != nil {
			return err
		}
		// Descending order keeps the active-order unique index satisfied during the shift.
		for _, sc := range later {
			if err := tx.Model(&models.Scene{}).Where("id = ?", sc.ID).Update("`order`", sc.Order+1).Error; err != nil {
				return err
			}
		}
		names := joinSceneCharacters(draft.Characters)
		created = models.Scene{
			ProjectID: projectID, EpisodeN: anchor.EpisodeN, Order: anchor.Order + 1, Generation: anchor.Generation,
			Title: draft.Title, Content: draft.Content, Duration: draft.Duration, ImageEngine: anchor.ImageEngine,
			Characters: names, VisibleCharacters: names, LocationName: draft.Location, Status: "pending",
		}
		if err := tx.Create(&created).Error; err != nil {
			return err
		}
		for i, d := range draft.Dialogues {
			row := models.Dialogue{ProjectID: projectID, SceneID: created.ID, Order: i + 1, Character: d.Character, SpeechType: "dialogue", Source: "user", Text: d.Text, Status: "pending"}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		}
		if err := invalidateContinuityDependentsTx(tx, projectID, []uint{anchor.ID}, "补镜插入已改变场景顺序"); err != nil {
			return err
		}
		return renumberSceneTitles(tx, projectID, anchor.Generation, anchor.EpisodeN)
	})
	if err != nil {
		return nil, err
	}
	s.DB.First(&created, created.ID)
	if s.Projects != nil {
		s.Projects.PushProject(nil)
	}
	return &created, nil
}

func (s *Service) HandleApplySceneSupplement(c *gin.Context) {
	scene, ok := s.loadScene(c)
	if !ok {
		return
	}
	var req struct {
		Brief      string               `json:"brief"`
		SourceHash string               `json:"source_hash"`
		Draft      sceneSupplementDraft `json:"draft"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Brief) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	created, err := s.ApplySceneSupplement(scene.ProjectID, scene.ID, strings.TrimSpace(req.Brief), req.SourceHash, req.Draft)
	if errors.Is(err, errSceneSupplementStale) {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, created)
}
