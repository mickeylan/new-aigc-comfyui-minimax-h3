package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
)

// Experimental top-view blocking (P2-4). Everything here is off unless the user turns on
// SettingExperimentalTopview; a 3-Scene A/B is required before it may become a default.
const (
	SettingExperimentalTopview = "experimental.topview"

	topviewStructureSkillCode   = "topview-structure"
	topviewEndDispatchSkillCode = "topview-end-dispatch"

	// topviewReferenceLine describes the cached structure image to H3. Wording is user-approved.
	topviewReferenceLine = "场景俯视结构图（只参考空间布局与地标的相对位置，不作为画面内容，视频中不出现俯视视角）"
)

// topviewStructureSkill is an image-edit prompt sent to Qwen Edit as is; topviewEndDispatchSkill
// is an LLM program. Both wordings are user-approved; raise Version whenever they change.
var topviewStructureSkill = models.Skill{
	Name: "场景俯视结构图", Code: topviewStructureSkillCode, Operation: topviewStructureSkillCode, Version: 1,
	Description:  "实验：把场景参考图转成只还原可见区域、不含人物的正俯视结构图，按场景资产缓存（图像编辑提示词）",
	Stage:        models.SkillStageQwenImageEdit,
	SystemPrompt: "把参考图中的场景转换为同一场景的正俯视结构图：相机位于场景正上方垂直向下 90 度拍摄，全程不倾斜，不转成斜俯或立面透视，效果类似掀开屋顶的沙盘或建筑平面图。参考图就是空间边界：只还原参考图中可见的区域，以及能由可见的墙线、门洞、道路、水岸、树线、山体和建筑外轮廓直接闭合出的部分；看不到的相邻房间、院落、街段、门后、楼上楼下、林深处或水域尽头一律不补画，不为对称或完整而复制、延长、扩建。信息不足处按可见边界截断，或留作遮挡、暗区、雾区或树冠覆盖。本场景主体活动区上方的屋顶、亭盖、廊檐要掀开，露出下方连续的地面与家具布局；非主体的周边建筑保留屋顶俯视轮廓作为边框；原本露天的院落、街道、天井保持原样。墙体、隔断、屏风、门框、柱子、栏杆及墙上挂物只画成从上方看到的细边线，不立起、不展开正面；落地的桌案、香炉、神龛按顶部轮廓保留位置。水面按俯视轮廓画出，岸线清楚，水面不可站立。地面、道路、院落和室内地面保持清晰、平整、连续、无遮挡，为后续落位留出空间；家具与车辆只位于可承载地面，尺度与建筑相称。画面中不出现任何人物，不添加文字、标注、箭头或水印。",
	IsSystem:     true, Enabled: true, SortOrder: 33,
}

var topviewEndDispatchSkill = models.Skill{
	Name: "Scene结尾俯视调度", Code: topviewEndDispatchSkillCode, Operation: topviewEndDispatchSkillCode, Version: 1,
	Description:    "实验：推出本Scene结束时白名单人物锚定场景地标的位置，确认后写入人物站位的起止状态",
	Stage:          models.SkillStageStoryboard,
	PromptTemplate: "场景：{{location_name}}\n结尾可见人物（姓名=颜色）：{{visible_characters}}\n上一Scene结尾调度：{{previous_end}}\n\n本Scene正文：\n{{scene_text}}",
	SystemPrompt: withSkillOutputRule("你是影视场面调度师。只根据当前 Scene 正文，推出本 Scene 结束最后一刻各人物在场景中的位置，供俯视调度参考。\n" +
		"1. characters 只能包含「结尾可见人物」白名单中的人物，每人恰好一条，顺序与白名单一致；白名单外的姓名，即使出现在台词、画外音、门外或上一 Scene 中，也不得写入任何字段，也不得改写成路人。\n" +
		"2. marker_color 原样沿用白名单中给出的颜色，不得改色。\n" +
		"3. 若提供上一 Scene 结尾调度，它就是本 Scene 开始时的位置；你只推出本 Scene 正文结束后的位置。正文没有交代移动的人物保持原位。\n" +
		"4. position 必须锚定场景里的实体地标（如「门槛内侧」「石桌东侧」「廊柱旁」「台阶下」），可再补人物间的相对关系；禁止使用画面坐标或构图位置（如「画面左侧」「前景」「三分之一处」「中轴线」），不推断街道朝向。\n" +
		"5. 不写外貌、服饰、颜色、发型等外观信息，外观只由人物参考图决定。\n" +
		"6. 只写正文能推出的内容；无法确定的 posture、facing、holding 写空字符串。\n" +
		`7. 输出合法 JSON：{"summary":"一句话概括最后一刻的站位","characters":[{"name":"","marker_color":"","position":"","posture":"","facing":"","holding":""}],"props":[{"name":"","position":""}]}`),
	IsSystem: true, Enabled: true, SortOrder: 34,
}

// topviewMarkerColors are assigned in order; a character keeps the colour it had in earlier Scenes.
var topviewMarkerColors = []string{"红", "蓝", "黄", "绿", "紫", "橙", "青", "粉", "白", "黑"}

var (
	errTopviewDisabled     = errors.New("实验功能「俯视调度」未开启，请在设置中打开")
	errTopviewActive       = errors.New("该场景的俯视结构图正在生成")
	topviewScreenWordsRe   = regexp.MustCompile(`画面|前景|后景|中景|三分之一|1/3|中轴线|镜头|构图|画左|画右`)
	topviewAppearanceRe    = regexp.MustCompile(`身穿|穿着|服饰|服装|衣着|发型|发色|外貌|长相`)
	topviewMaxDispatchSize = 32 * 1024
)

type topviewMarker struct {
	Name  string `json:"name"`
	Color string `json:"marker_color"`
}

type topviewDispatchCharacter struct {
	Name     string `json:"name"`
	Color    string `json:"marker_color"`
	Position string `json:"position"`
	Posture  string `json:"posture"`
	Facing   string `json:"facing"`
	Holding  string `json:"holding"`
}

type topviewDispatchProp struct {
	Name     string `json:"name"`
	Position string `json:"position"`
}

type topviewDispatch struct {
	SourceContentHash string                     `json:"source_content_hash,omitempty"`
	Summary           string                     `json:"summary"`
	Characters        []topviewDispatchCharacter `json:"characters"`
	Props             []topviewDispatchProp      `json:"props"`
}

func (s *ProjectService) topviewEnabled() bool {
	return s != nil && s.volc != nil && s.volc.GetSetting(SettingExperimentalTopview, "false") == "true"
}

// parseTopviewDispatch accepts only the documented fields, the whitelisted characters in order
// with their assigned colours, and landmark-anchored text. Anything else is rejected, never repaired.
func parseTopviewDispatch(raw string, markers []topviewMarker) (topviewDispatch, error) {
	var out topviewDispatch
	if len(raw) > topviewMaxDispatchSize {
		return out, fmt.Errorf("调度JSON过大")
	}
	dec := json.NewDecoder(bytes.NewReader([]byte(extractJSON(raw))))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return out, fmt.Errorf("调度输出不是约定的JSON（不允许外观等额外字段）: %w", err)
	}
	if len(out.Characters) != len(markers) {
		return out, fmt.Errorf("调度人物数 %d 与结尾可见人物 %d 不一致", len(out.Characters), len(markers))
	}
	check := func(label, value string) error {
		if w := topviewScreenWordsRe.FindString(value); w != "" {
			return fmt.Errorf("%s使用了画面坐标「%s」，位置必须锚定场景地标", label, w)
		}
		if w := topviewAppearanceRe.FindString(value); w != "" {
			return fmt.Errorf("%s写了外观信息「%s」", label, w)
		}
		return nil
	}
	if err := check("summary", out.Summary); err != nil {
		return out, err
	}
	for i, c := range out.Characters {
		c.Name, c.Color, c.Position = strings.TrimSpace(c.Name), strings.TrimSpace(c.Color), strings.TrimSpace(c.Position)
		if c.Name != markers[i].Name {
			return out, fmt.Errorf("第%d位人物应为「%s」，实际为「%s」", i+1, markers[i].Name, c.Name)
		}
		if c.Color != markers[i].Color {
			return out, fmt.Errorf("「%s」的颜色应为%s，实际为%s", c.Name, markers[i].Color, c.Color)
		}
		if c.Position == "" {
			return out, fmt.Errorf("「%s」缺少位置", c.Name)
		}
		for _, v := range []string{c.Position, c.Posture, c.Facing, c.Holding} {
			if err := check("「"+c.Name+"」", v); err != nil {
				return out, err
			}
		}
		out.Characters[i] = c
	}
	for _, p := range out.Props {
		if err := check("道具「"+p.Name+"」", p.Position); err != nil {
			return out, err
		}
	}
	return out, nil
}

// formatTopviewDispatch renders a dispatch as Blocking start/end text.
func formatTopviewDispatch(d topviewDispatch) string {
	parts := make([]string, 0, len(d.Characters))
	for _, c := range d.Characters {
		item := fmt.Sprintf("%s（%s）：%s", c.Name, c.Color, c.Position)
		for _, extra := range []string{c.Posture, c.Facing, c.Holding} {
			if v := strings.TrimSpace(extra); v != "" {
				item += "，" + v
			}
		}
		parts = append(parts, item)
	}
	text := "【俯视调度】" + strings.Join(parts, "；")
	if v := strings.TrimSpace(d.Summary); v != "" {
		text += "。" + v
	}
	return text
}

// earlierTopviewScenes returns earlier active Scenes of the same episode version that carry a
// confirmed dispatch, nearest first.
func (s *Service) earlierTopviewScenes(scene *models.Scene) []models.Scene {
	var rows []models.Scene
	s.DB.Where("project_id = ? AND episode_n = ? AND generation = ? AND `order` < ? AND (archived = ? OR archived IS NULL) AND COALESCE(topview_dispatch_json, '') <> ''",
		scene.ProjectID, scene.EpisodeN, scene.Generation, scene.Order, false).Order("`order` DESC, id DESC").Find(&rows)
	return rows
}

// topviewMarkers keeps each character's colour from the nearest earlier Scene and gives new
// characters the first unused colour.
func (s *Service) topviewMarkers(scene *models.Scene) ([]topviewMarker, error) {
	names := visibleSceneCharacterNames(scene)
	if len(names) == 0 {
		return nil, fmt.Errorf("本Scene没有可见人物，无需俯视调度")
	}
	if len(names) > len(topviewMarkerColors) {
		return nil, fmt.Errorf("可见人物 %d 位，超过 %d 种标记颜色", len(names), len(topviewMarkerColors))
	}
	known := map[string]string{}
	for _, prev := range s.earlierTopviewScenes(scene) {
		var d topviewDispatch
		if json.Unmarshal([]byte(prev.TopviewDispatchJSON), &d) != nil {
			continue
		}
		for _, c := range d.Characters {
			if _, ok := known[c.Name]; !ok && c.Color != "" {
				known[c.Name] = c.Color
			}
		}
	}
	used := map[string]bool{}
	markers := make([]topviewMarker, len(names))
	for i, name := range names {
		markers[i].Name = name
		if color, ok := known[name]; ok && !used[color] {
			markers[i].Color, used[color] = color, true
		}
	}
	for i := range markers {
		if markers[i].Color != "" {
			continue
		}
		for _, color := range topviewMarkerColors {
			if !used[color] {
				markers[i].Color, used[color] = color, true
				break
			}
		}
	}
	return markers, nil
}

// previousTopviewDispatch returns the previous Scene's confirmed end dispatch while it still
// matches that Scene's current text; a stale one is ignored.
func (s *Service) previousTopviewDispatch(scene *models.Scene) (*topviewDispatch, *models.Scene) {
	var prev models.Scene
	if s.DB.Where("project_id = ? AND episode_n = ? AND generation = ? AND `order` < ? AND (archived = ? OR archived IS NULL)",
		scene.ProjectID, scene.EpisodeN, scene.Generation, scene.Order, false).Order("`order` DESC, id DESC").First(&prev).Error != nil {
		return nil, nil
	}
	var d topviewDispatch
	if strings.TrimSpace(prev.TopviewDispatchJSON) == "" || json.Unmarshal([]byte(prev.TopviewDispatchJSON), &d) != nil ||
		d.SourceContentHash != sceneDialogueContentHash(prev.Content) {
		return nil, &prev
	}
	return &d, &prev
}

func topviewMarkerText(markers []topviewMarker) string {
	parts := make([]string, len(markers))
	for i, m := range markers {
		parts[i] = m.Name + "=" + m.Color
	}
	return strings.Join(parts, "、")
}

// HandleTopviewDispatchDraft returns an editable end-of-Scene dispatch draft; it never writes.
func (s *Service) HandleTopviewDispatchDraft(c *gin.Context) {
	scene, ok := s.loadScene(c)
	if !ok {
		return
	}
	if !s.Projects.topviewEnabled() {
		c.JSON(http.StatusForbidden, gin.H{"error": errTopviewDisabled.Error()})
		return
	}
	if s.Skills == nil || s.TextProviderFact == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "AI 文本服务未配置"})
		return
	}
	markers, err := s.topviewMarkers(scene)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	previousText := "无"
	if prev, _ := s.previousTopviewDispatch(scene); prev != nil {
		previousText = formatTopviewDispatch(*prev)
	}
	output, err := s.Skills.ChatWithConfiguredOrFallbackSkill(scene.ProjectID, models.SkillStageStoryboard, topviewEndDispatchSkillCode, s.TextProviderFact, "", "", map[string]string{
		"location_name": scene.LocationName, "visible_characters": topviewMarkerText(markers), "previous_end": previousText, "scene_text": scene.Content,
	})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "俯视调度推演失败: " + err.Error()})
		return
	}
	dispatch, err := parseTopviewDispatch(output, markers)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "AI返回格式无效: " + err.Error()})
		return
	}
	dispatch.SourceContentHash = sceneDialogueContentHash(scene.Content)
	c.JSON(http.StatusOK, gin.H{"dispatch": dispatch, "markers": markers, "previous_end": previousText, "persisted": false, "skill_code": topviewEndDispatchSkillCode})
}

// HandleApplyTopviewDispatch saves a user-confirmed dispatch and writes it into the Scene's
// Blocking: the previous Scene's end becomes start, this dispatch becomes end. Other Blocking
// fields are kept.
func (s *Service) HandleApplyTopviewDispatch(c *gin.Context) {
	scene, ok := s.loadScene(c)
	if !ok {
		return
	}
	if !s.Projects.topviewEnabled() {
		c.JSON(http.StatusForbidden, gin.H{"error": errTopviewDisabled.Error()})
		return
	}
	body, err := c.GetRawData()
	if err != nil || len(body) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "调度JSON为空"})
		return
	}
	markers, err := s.topviewMarkers(scene)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	dispatch, err := parseTopviewDispatch(string(body), markers)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	currentHash := sceneDialogueContentHash(scene.Content)
	if dispatch.SourceContentHash != currentHash {
		c.JSON(http.StatusConflict, gin.H{"error": "场景正文已变化，请重新推演俯视调度"})
		return
	}
	blocking := SceneBlocking{SchemaVersion: 2, Facts: []string{}, Suggestions: []string{}}
	if strings.TrimSpace(scene.BlockingJSON) != "" && sceneBlockingPromptContext(scene) != "" {
		if parsed, err := parseSceneBlocking(scene.BlockingJSON); err == nil {
			blocking = parsed
		}
	}
	if prev, _ := s.previousTopviewDispatch(scene); prev != nil {
		blocking.Start = formatTopviewDispatch(*prev)
	}
	blocking.End = formatTopviewDispatch(dispatch)
	blocking.SourceContentHash = currentHash
	dispatchJSON, _ := json.Marshal(dispatch)
	tx := s.DB.Model(&models.Scene{}).Where("id = ? AND project_id = ? AND content = ?", scene.ID, scene.ProjectID, scene.Content).Updates(map[string]any{
		"topview_dispatch_json": string(dispatchJSON), "blocking_json": blockingJSON(blocking), "blocking_content_hash": currentHash,
		"blocking_image_applied_hash": "", "blocking_video_applied_hash": "", "prompt_stale": true,
	})
	if tx.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": tx.Error.Error()})
		return
	}
	if tx.RowsAffected == 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "场景正文已变化，请重新推演俯视调度"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"dispatch": dispatch, "blocking": blocking, "persisted": true})
}

func topviewCacheValid(a *models.Asset) bool {
	return a != nil && a.Kind == AssetKindLocation && a.Image != "" && a.TopviewImage != "" && a.TopviewSourceImage == a.Image
}

// StartTopviewStructure submits a Qwen Edit task converting a location image into its top-view
// structure map. The result is cached on the asset until its image changes.
func (s *ProjectService) StartTopviewStructure(a *models.Asset, force bool) (bool, error) {
	if !s.topviewEnabled() {
		return false, errTopviewDisabled
	}
	if a.Kind != AssetKindLocation || strings.TrimSpace(a.Image) == "" {
		return false, fmt.Errorf("只有已有参考图的场景资产可以生成俯视结构图")
	}
	if s.sheetTaskActive(a.TopviewTaskID) {
		return false, errTopviewActive
	}
	if topviewCacheValid(a) && !force {
		return true, nil
	}
	if s.tasks == nil {
		return false, fmt.Errorf("俯视结构图生成依赖 ComfyUI 任务服务")
	}
	tpl, err := enabledTemplateByCode(s.db, TemplateQwen21MultiEdit)
	if err != nil {
		return false, err
	}
	prompt := topviewStructureSkill.SystemPrompt
	if s.skills != nil {
		if latest, err := s.skills.latestEnabledSkillByCode(topviewStructureSkillCode); err == nil {
			prompt = latest.SystemPrompt
		}
	}
	params := qwenPortraitParams()
	params["aspect_ratio"] = "1:1 (Square)"
	task, err := s.tasks.CreateTask(CreateTaskReq{TemplateID: tpl.ID, Prompt: prompt, Params: params,
		Files: map[string][]FileMeta{"ref_images": {{TaskID: fmt.Sprint(a.ProjectID), Name: a.Image}}}})
	if err != nil {
		return false, fmt.Errorf("创建俯视结构图任务失败: %w", err)
	}
	// The cache is rebuilt for the image submitted now; the previous structure image is dropped.
	if err := s.db.Model(&models.Asset{}).Where("id = ?", a.ID).Updates(map[string]any{
		"topview_task_id": task.TaskID, "topview_error": "", "topview_image": "", "topview_source_image": a.Image,
	}).Error; err != nil {
		_ = s.tasks.CancelTask(task.TaskID)
		return false, err
	}
	if a.TopviewImage != "" {
		// Dropping the old map changes the references these Scenes would submit.
		s.markTopviewScenesStale(a)
	}
	go func() {
		if err := s.tasks.Execute(task.TaskID); err != nil && !errors.Is(err, errNoFreeGPU) {
			log.Printf("[asset %d] execute topview task %s failed: %v", a.ID, task.TaskID, err)
		}
	}()
	s.pushProject(nil)
	return false, nil
}

func (s *ProjectService) syncTopviewStructures() bool {
	var assets []models.Asset
	if s.db.Where("kind = ? AND topview_task_id != ''", AssetKindLocation).Find(&assets).Error != nil {
		return false
	}
	changed := false
	for _, a := range assets {
		updates, ok := s.imageTaskUpdates(a.TopviewTaskID, fmt.Sprintf("topview_%d_%d", a.ID, time.Now().UnixNano()), a.ProjectID,
			"俯视结构图", "topview_image", "topview_task_id", "topview_error")
		if !ok {
			continue
		}
		res := s.db.Model(&models.Asset{}).Where("id = ? AND topview_task_id = ?", a.ID, a.TopviewTaskID).Updates(updates)
		changed = changed || res.RowsAffected > 0
		if file, _ := updates["topview_image"].(string); file != "" && res.RowsAffected > 0 {
			s.markTopviewScenesStale(&a)
		}
	}
	return changed
}

// markTopviewScenesStale flags saved video prompts of Scenes whose location is a, because the
// top-view map they would submit has changed. Like other reference changes this is advisory: the
// saved prompt is kept and the user re-confirms it.
func (s *ProjectService) markTopviewScenesStale(a *models.Asset) {
	if !s.topviewEnabled() {
		return
	}
	var scenes []models.Scene
	if s.db.Where("project_id = ? AND (archived = ? OR archived IS NULL) AND COALESCE(video_full_prompt, '') <> ''", a.ProjectID, false).Find(&scenes).Error != nil {
		return
	}
	for i := range scenes {
		for _, matched := range s.sceneMatchedAssets(&scenes[i]) {
			if matched.ID == a.ID {
				if err := s.db.Model(&models.Scene{}).Where("id = ?", scenes[i].ID).Update("prompt_stale", true).Error; err != nil {
					log.Printf("[asset %d] mark scene %d prompt stale failed: %v", a.ID, scenes[i].ID, err)
				}
				break
			}
		}
	}
}

// sceneTopviewReference returns the cached structure image of the Scene's location, or false
// when the experiment is off or no valid cache exists.
func (s *ProjectService) sceneTopviewReference(sc *models.Scene) (string, bool) {
	if !s.topviewEnabled() {
		return "", false
	}
	for _, a := range s.sceneMatchedAssets(sc) {
		if a.Kind == AssetKindLocation {
			if topviewCacheValid(&a) {
				return a.TopviewImage, true
			}
			return "", false
		}
	}
	return "", false
}

func (s *Service) HandleGenerateTopviewStructure(c *gin.Context) {
	a, ok := s.loadAsset(c)
	if !ok {
		return
	}
	var req struct {
		Force bool `json:"force"`
	}
	_ = c.ShouldBindJSON(&req)
	cached, err := s.Projects.StartTopviewStructure(a, req.Force)
	if err != nil {
		status := http.StatusBadRequest
		switch {
		case errors.Is(err, errTopviewDisabled):
			status = http.StatusForbidden
		case errors.Is(err, errTopviewActive):
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	if cached {
		c.JSON(http.StatusOK, gin.H{"cached": true, "topview_image": a.TopviewImage, "message": "该场景图的俯视结构图已缓存；如需重画请选择重新生成"})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"cached": false, "message": fmt.Sprintf("场景「%s」俯视结构图任务已提交", a.Name)})
}
