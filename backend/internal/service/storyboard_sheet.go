package service

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	storyboardSheetSkillCode = "storyboard-sheet-prompt"
	maxStoryboardAnchors     = 15
)

// storyboardSheetSkill splits a Scene into causal anchors for a review-only pencil storyboard.
// Its wording is user-approved; raise Version whenever the prompt changes so InitSystemSkills
// publishes the new revision.
var storyboardSheetSkill = models.Skill{
	Name: "故事版锚点拆解", Code: storyboardSheetSkillCode, Operation: storyboardSheetSkillCode, Version: 1,
	Description: "把一个Scene拆成1～15个因果锚点，用于生成仅供审阅的铅笔故事版总图",
	Stage:       models.SkillStageStoryboard,
	PromptTemplate: "请把下面这个 Scene 拆成 1～15 个按时间顺序排列的故事版锚点。\n规则：\n" +
		"1. 每个锚点是一个不能再合并的因果瞬间（起因、动作、受力或反应、结果）；按剧情自然提取，不按时长凑数，也不机械地每个 Shot 一格。\n" +
		"2. 每个锚点单独一行，格式为「P1：……」「P2：……」，从 P1 连续编号，不超过 P15。\n" +
		"3. 每个锚点写清：景别与构图、人物在画面中的左右位置与朝向、动作路径或受力点、视线、与下一锚点的衔接。\n" +
		"4. 台词只转成说话瞬间的表演（谁在说、对谁说、神情与口型），画面中不出现文字；台词原文只用于判断说话瞬间，不得改写。\n" +
		"5. 只使用下列 Scene 正文、Shot 与对白中的事实，不得新增人物、道具、术法、伤势或结果。\n" +
		"6. 人物只写名字，不描述外貌与服装，造型以参考图为准。\n\n" +
		"Scene 正文：{{scene_content}}\n已保存 Shot（仅作镜头参考）：{{shots}}\n对白（按顺序）：{{dialogues}}\n出场人物：{{characters}}",
	SystemPrompt: "你是故事版分镜师。把一个 Scene 拆成按时间顺序排列的因果锚点，供绘制一张审阅用的铅笔故事版总图。只输出锚点行，不输出解释、标题或代码块。",
	IsSystem:     true, Enabled: true, SortOrder: 21,
}

// storyboardSheetImagePrompt is the user-approved fixed Qwen prompt. {{n}} and {{grid}} are
// filled by the backend; reference and anchor lines follow it.
const storyboardSheetImagePrompt = "生成一张横向 4:3 的铅笔故事版总图：纯白纸底，黑色与中灰铅笔线稿，细黑格线，按 P1 到 P{{n}} 从左到右、从上到下排成 {{grid}} 宫格，每个锚点只画一格，未占满的格子留白。每格按 16:9 视频画面构图。人物造型以参考图为准，只去色转成线稿，不改发型、服装、配饰与道具。用蓝色实线箭头表示人物与物体的运动方向，红色实线箭头表示攻击或受力方向，每格箭头不超过 3 支，不遮挡脸、手和接触点。画面中不出现任何文字、对白、编号、时间码、标志或水印。"

var (
	errStoryboardSheetActive = errors.New("故事版总图正在生成")
	storyboardAnchorPattern  = regexp.MustCompile(`^[Pp](\d{1,2})\s*[：:]\s*(.+)$`)
)

// parseStoryboardAnchors accepts one "P1：…" anchor per line, numbered consecutively from P1 and
// capped at 15. Anything else is rejected rather than silently dropped, so a panel is never lost.
func parseStoryboardAnchors(raw string) ([]string, error) {
	var anchors []string
	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "```") {
			continue
		}
		m := storyboardAnchorPattern.FindStringSubmatch(line)
		if m == nil {
			return nil, fmt.Errorf("第%d个锚点之后出现非锚点行：%s", len(anchors), line)
		}
		n, _ := strconv.Atoi(m[1])
		if n != len(anchors)+1 {
			return nil, fmt.Errorf("锚点编号必须从P1连续递增，P%d之后出现P%d", len(anchors), n)
		}
		anchors = append(anchors, fmt.Sprintf("P%d：%s", n, strings.TrimSpace(m[2])))
	}
	if len(anchors) == 0 {
		return nil, fmt.Errorf("没有锚点")
	}
	if len(anchors) > maxStoryboardAnchors {
		return nil, fmt.Errorf("锚点不能超过%d个，当前%d个", maxStoryboardAnchors, len(anchors))
	}
	return anchors, nil
}

// storyboardSheetGrid picks the smallest landscape grid (columns×rows) that holds every anchor.
func storyboardSheetGrid(n int) string {
	switch {
	case n <= 4:
		return "2列×2行"
	case n <= 6:
		return "3列×2行"
	case n <= 9:
		return "3列×3行"
	case n <= 12:
		return "4列×3行"
	default:
		return "5列×3行"
	}
}

func buildStoryboardSheetPrompt(anchors, referenceLines []string) string {
	head := strings.NewReplacer("{{n}}", strconv.Itoa(len(anchors)), "{{grid}}", storyboardSheetGrid(len(anchors))).Replace(storyboardSheetImagePrompt)
	parts := []string{head}
	if len(referenceLines) > 0 {
		parts = append(parts, "参考图："+strings.Join(referenceLines, "；")+"。")
	}
	return strings.Join(append(parts, anchors...), "\n")
}

// storyboardSheetSource returns the Skill inputs and a hash over them, so the UI can tell when a
// sheet no longer matches the Scene it was drawn from.
func (s *Service) storyboardSheetSource(scene *models.Scene) (map[string]string, string, error) {
	var shots []models.Shot
	if err := s.DB.Where("scene_id = ?", scene.ID).Order("order_num, id").Find(&shots).Error; err != nil {
		return nil, "", err
	}
	var dialogues []models.Dialogue
	if err := s.DB.Where("scene_id = ?", scene.ID).Order("`order`, id").Find(&dialogues).Error; err != nil {
		return nil, "", err
	}
	shotLines := make([]string, 0, len(shots))
	for i, shot := range shots {
		shotLines = append(shotLines, fmt.Sprintf("Shot %d（%s秒，%s）：%s", i+1, pacingNumber(shot.Duration), strings.TrimSpace(shot.ShotType), strings.TrimSpace(shot.Description)))
	}
	dialogueLines := make([]string, 0, len(dialogues))
	for i, d := range dialogues {
		speaker := strings.TrimSpace(d.Character)
		if speaker == "" {
			speaker = "（无说话人）"
		}
		dialogueLines = append(dialogueLines, fmt.Sprintf("%d. %s：%s", i+1, speaker, strings.TrimSpace(d.Text)))
	}
	orNone := func(lines []string, sep string) string {
		if len(lines) == 0 {
			return "无"
		}
		return strings.Join(lines, sep)
	}
	params := map[string]string{
		"scene_content": strings.TrimSpace(scene.Content),
		"shots":         orNone(shotLines, "\n"),
		"dialogues":     orNone(dialogueLines, "\n"),
		"characters":    orNone(visibleSceneCharacterNames(scene), "、"),
	}
	hash := sha256Hex([]byte(strings.Join([]string{params["scene_content"], params["shots"], params["dialogues"], params["characters"]}, "\x00")))
	return params, hash, nil
}

// HandleStoryboardSheetAnchors drafts anchors for review. Nothing is saved and no image is generated.
func (s *Service) HandleStoryboardSheetAnchors(c *gin.Context) {
	scene, ok := s.loadScene(c)
	if !ok {
		return
	}
	if s.Skills == nil || s.TextProviderFact == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "AI 文本服务未配置"})
		return
	}
	if strings.TrimSpace(scene.Content) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "本场正文为空，无法拆解故事版锚点"})
		return
	}
	params, hash, err := s.storyboardSheetSource(scene)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	output, err := s.Skills.ChatWithConfiguredOrFallbackSkill(scene.ProjectID, storyboardSheetSkill.Stage, storyboardSheetSkillCode, s.TextProviderFact, "", "", params)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "故事版锚点生成失败: " + err.Error()})
		return
	}
	anchors, err := parseStoryboardAnchors(stripThinkBlocks(output))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "故事版锚点未通过校验：" + err.Error(), "raw_output": output})
		return
	}
	c.JSON(http.StatusOK, gin.H{"anchors": strings.Join(anchors, "\n"), "source_hash": hash, "skill_code": storyboardSheetSkillCode, "persisted": false})
}

// storyboardSheetReferenceFiles uses each visible character's sheet (or portrait) as the sole
// design master; locations and props are left out so the pencil sheet stays readable. A scene
// outfit replaces the character master, matching storyboard image and video references.
func (s *ProjectService) storyboardSheetReferenceFiles(sc *models.Scene) ([]FileMeta, []string) {
	pid := fmt.Sprint(sc.ProjectID)
	var refs []FileMeta
	var lines []string
	outfits := s.sceneOutfitsByCharacter(sc)
	for _, ch := range s.sceneCharacterPortraits(sc) {
		if len(refs) >= maxSceneReferenceImages {
			break
		}
		name, kind := ch.Sheet, "四视图"
		if name == "" {
			name, kind = ch.Portrait, "标准像"
		}
		if outfit, ok := outfits[ch.ID]; ok && outfitReferenceFile(outfit) != "" {
			name, kind = outfit.Sheet, "新形象「"+outfit.Name+"」四视图"
			if name == "" {
				name, kind = outfit.Image, "新形象「"+outfit.Name+"」定妆照"
			}
		}
		if name == "" {
			continue
		}
		refs = append(refs, FileMeta{TaskID: pid, Name: name})
		lines = append(lines, fmt.Sprintf("<image%d>是角色「%s」的%s", len(refs), ch.Name, kind))
	}
	return refs, lines
}

// StartStoryboardSheet submits the confirmed anchors to Qwen-Image-2.1. The result is stored on a
// StoryboardSheet row for review only and never becomes an H3 reference.
func (s *ProjectService) StartStoryboardSheet(sc *models.Scene, rawAnchors, sourceHash string) (*models.StoryboardSheet, error) {
	anchors, err := parseStoryboardAnchors(rawAnchors)
	if err != nil {
		return nil, err
	}
	if s.tasks == nil {
		return nil, fmt.Errorf("故事版总图生成依赖 ComfyUI 任务服务")
	}
	var latest models.StoryboardSheet
	if err := s.db.Where("scene_id = ?", sc.ID).Order("id DESC").First(&latest).Error; err == nil && s.sheetTaskActive(latest.TaskID) {
		return nil, errStoryboardSheetActive
	}
	refs, lines := s.storyboardSheetReferenceFiles(sc)
	tpl, err := enabledTemplateByCode(s.db, qwenTemplateCode(len(refs) > 0))
	if err != nil {
		return nil, err
	}
	prompt := buildStoryboardSheetPrompt(anchors, lines)
	params := qwenPortraitParams()
	params["aspect_ratio"] = "4:3 (Standard)"
	params["seed"] = -1
	task, err := s.tasks.CreateTask(CreateTaskReq{TemplateID: tpl.ID, Prompt: prompt, Params: params, Files: map[string][]FileMeta{"ref_images": refs}})
	if err != nil {
		return nil, fmt.Errorf("创建故事版总图任务失败: %w", err)
	}
	skillVersion := 0
	if s.skills != nil {
		if skill, err := s.skills.latestEnabledSkillByCode(storyboardSheetSkillCode); err == nil {
			skillVersion = skill.Version
		}
	}
	row := models.StoryboardSheet{ProjectID: sc.ProjectID, SceneID: sc.ID, Anchors: strings.Join(anchors, "\n"), FullPrompt: prompt,
		TemplateCode: tpl.Code, SourceSHA256: sourceHash, SkillVersion: skillVersion, TaskID: task.TaskID}
	if err := s.db.Create(&row).Error; err != nil {
		_ = s.tasks.CancelTask(task.TaskID)
		return nil, err
	}
	go func() {
		if err := s.tasks.Execute(task.TaskID); err != nil && !errors.Is(err, errNoFreeGPU) {
			log.Printf("[scene %d] execute storyboard sheet task %s failed: %v", sc.ID, task.TaskID, err)
		}
	}()
	s.pushProject(nil)
	return &row, nil
}

func (s *ProjectService) syncStoryboardSheets() bool {
	var rows []models.StoryboardSheet
	if !s.db.Migrator().HasTable(&models.StoryboardSheet{}) || s.db.Where("task_id != ''").Find(&rows).Error != nil {
		return false
	}
	changed := false
	for _, row := range rows {
		updates, ok := s.imageTaskUpdates(row.TaskID, fmt.Sprintf("storyboard_sheet_%d_%d", row.SceneID, time.Now().UnixNano()), row.ProjectID,
			"故事版总图", "image", "task_id", "error")
		if !ok {
			continue
		}
		res := s.db.Model(&models.StoryboardSheet{}).Where("id = ? AND task_id = ?", row.ID, row.TaskID).Updates(updates)
		changed = changed || res.RowsAffected > 0
	}
	return changed
}

func (s *Service) HandleGenerateStoryboardSheet(c *gin.Context) {
	scene, ok := s.loadScene(c)
	if !ok {
		return
	}
	var req struct {
		Anchors    string `json:"anchors"`
		SourceHash string `json:"source_hash"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Anchors) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请先填写或生成故事版锚点"})
		return
	}
	sourceHash := req.SourceHash
	if sourceHash == "" {
		if _, hash, err := s.storyboardSheetSource(scene); err == nil {
			sourceHash = hash
		}
	}
	row, err := s.Projects.StartStoryboardSheet(scene, req.Anchors, sourceHash)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errStoryboardSheetActive) {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"sheet": row, "message": "故事版总图任务已提交（仅供审阅，不进入视频生成）"})
}

// HandleListStoryboardSheets returns the newest sheets first together with the current source
// hash, so the UI can mark sheets drawn from an older version of the Scene.
func (s *Service) HandleListStoryboardSheets(c *gin.Context) {
	scene, ok := s.loadScene(c)
	if !ok {
		return
	}
	var rows []models.StoryboardSheet
	if err := s.DB.Where("scene_id = ?", scene.ID).Order("id DESC").Limit(10).Find(&rows).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	_, hash, err := s.storyboardSheetSource(scene)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": rows, "source_hash": hash})
}
