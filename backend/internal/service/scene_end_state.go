package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	sceneEndStateSchema    = "scene_end_state.v1"
	sceneEndStateSkillCode = "scene-end-state-archivist"
	sceneEndStateFieldMax  = 80
)

// sceneEndStateArchivistSkill extracts the end state. Its wording is user-approved; raise
// Version whenever the prompt changes so InitSystemSkills publishes the new revision.
var sceneEndStateArchivistSkill = models.Skill{
	Name: "人物结尾状态归档", Code: sceneEndStateSkillCode, Operation: sceneEndStateSkillCode, Version: 1,
	Description:    "记录Scene结束瞬间每个人物的姿态、面部、持有物、情绪表现和伤势以及关键道具位置，只作下一场的连续性参考",
	Stage:          models.SkillStageContinuityReview,
	PromptTemplate: "本场角色：{{characters}}\n地点：{{location}}\nScene正文：{{scene_content}}\n各镜头结束状态（按顺序，最后一项最接近本场结尾）：{{shot_end_states}}",
	SystemPrompt:   "你是连续性记录员，只记录本场最后一刻画面里能看见的状态，供下一场衔接参考；不续写、不推测后续剧情。只输出JSON对象，根字段只能是characters和props：characters以角色名为键，值只含posture（身体姿态与位置）、face（面部可见痕迹或表情）、holding（哪只手拿着什么，空手写空手）、emotion（可见的情绪表现，如咬牙、垂目，不写心理分析）、injury（可见外伤及部位）；props以道具名为键，值写其所在位置或持有者。只写输入中明确出现或可以直接看到的事实，没有依据的字段输出空字符串；角色名必须使用本场角色表中的写法；本场结尾已离场的角色不要输出。不要输出Markdown、解释或思考过程。",
	IsSystem:       true, Enabled: true, SortOrder: 20,
}

type sceneEndCharacterState struct {
	Posture string `json:"posture"`
	Face    string `json:"face"`
	Holding string `json:"holding"`
	Emotion string `json:"emotion"`
	Injury  string `json:"injury"`
}

type sceneEndStateData struct {
	Schema     string                            `json:"schema"`
	Characters map[string]sceneEndCharacterState `json:"characters"`
	Props      map[string]string                 `json:"props"`
}

func clipSceneEndField(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if runes := []rune(value); len(runes) > sceneEndStateFieldMax {
		value = string(runes[:sceneEndStateFieldMax])
	}
	return value
}

func normalizeSceneEndState(state sceneEndStateData) sceneEndStateData {
	out := sceneEndStateData{Schema: sceneEndStateSchema, Characters: map[string]sceneEndCharacterState{}, Props: map[string]string{}}
	for name, c := range state.Characters {
		name = strings.TrimSpace(name)
		c = sceneEndCharacterState{clipSceneEndField(c.Posture), clipSceneEndField(c.Face), clipSceneEndField(c.Holding), clipSceneEndField(c.Emotion), clipSceneEndField(c.Injury)}
		if name != "" && c != (sceneEndCharacterState{}) {
			out.Characters[name] = c
		}
	}
	for name, value := range state.Props {
		if name, value = strings.TrimSpace(name), clipSceneEndField(value); name != "" && value != "" {
			out.Props[name] = value
		}
	}
	return out
}

// parseSceneEndStateOutput accepts the documented object form and the common array form
// ([{"name":…}]) models fall back to; anything else is rejected rather than guessed.
func parseSceneEndStateOutput(raw string) (sceneEndStateData, error) {
	var envelope struct {
		Characters json.RawMessage `json:"characters"`
		Props      json.RawMessage `json:"props"`
	}
	if err := json.Unmarshal([]byte(extractJSON(raw)), &envelope); err != nil {
		return sceneEndStateData{}, fmt.Errorf("状态归档输出不是JSON对象: %w", err)
	}
	state := sceneEndStateData{Characters: map[string]sceneEndCharacterState{}, Props: map[string]string{}}
	if len(envelope.Characters) > 0 && string(envelope.Characters) != "null" {
		if json.Unmarshal(envelope.Characters, &state.Characters) != nil {
			var list []struct {
				Name string `json:"name"`
				sceneEndCharacterState
			}
			if err := json.Unmarshal(envelope.Characters, &list); err != nil {
				return sceneEndStateData{}, fmt.Errorf("characters格式无效: %w", err)
			}
			for _, item := range list {
				state.Characters[item.Name] = item.sceneEndCharacterState
			}
		}
	}
	if len(envelope.Props) > 0 && string(envelope.Props) != "null" {
		if json.Unmarshal(envelope.Props, &state.Props) != nil {
			var list []struct{ Name, State string }
			if err := json.Unmarshal(envelope.Props, &list); err != nil {
				return sceneEndStateData{}, fmt.Errorf("props格式无效: %w", err)
			}
			for _, item := range list {
				state.Props[item.Name] = item.State
			}
		}
	}
	return normalizeSceneEndState(state), nil
}

// sceneEndStateSourceHash covers everything extraction reads, so editing the Scene text,
// its character roles, or any Shot end state marks a saved state stale.
func sceneEndStateSourceHash(scene *models.Scene, shots []models.Shot) string {
	parts := []string{scene.Content, scene.VisibleCharacters, scene.Characters}
	for _, shot := range shots {
		parts = append(parts, shot.EndState)
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

func (s *Service) sceneEndStateSource(scene *models.Scene) (string, []models.Shot, error) {
	var shots []models.Shot
	if err := s.DB.Where("scene_id = ?", scene.ID).Order("order_num, id").Find(&shots).Error; err != nil {
		return "", nil, err
	}
	return sceneEndStateSourceHash(scene, shots), shots, nil
}

type sceneEndStateView struct {
	Record       *models.SceneEndState `json:"record"`
	State        *sceneEndStateData    `json:"state"`
	Stale        bool                  `json:"stale"`
	SourceSHA256 string                `json:"source_sha256"`
	SkillReady   bool                  `json:"skill_ready"`
}

func (s *Service) loadSceneEndState(scene *models.Scene) (*sceneEndStateView, error) {
	hash, _, err := s.sceneEndStateSource(scene)
	if err != nil {
		return nil, err
	}
	view := &sceneEndStateView{SourceSHA256: hash}
	var row models.SceneEndState
	if result := s.DB.Where("scene_id = ?", scene.ID).Limit(1).Find(&row); result.Error != nil {
		return nil, result.Error
	} else if result.RowsAffected == 0 {
		return view, nil
	}
	var state sceneEndStateData
	if err := json.Unmarshal([]byte(row.StateJSON), &state); err != nil {
		return nil, fmt.Errorf("已保存的结尾状态无法解析: %w", err)
	}
	state = normalizeSceneEndState(state)
	view.Record, view.State, view.Stale = &row, &state, row.SourceSHA256 != hash
	return view, nil
}

func (s *Service) saveSceneEndState(scene *models.Scene, state sceneEndStateData, hash, extractedBy string, skillVersion int) (*models.SceneEndState, error) {
	data, err := json.Marshal(normalizeSceneEndState(state))
	if err != nil {
		return nil, err
	}
	var row models.SceneEndState
	if err := s.DB.Where("scene_id = ?", scene.ID).Limit(1).Find(&row).Error; err != nil {
		return nil, err
	}
	row.ProjectID, row.SceneID, row.StateJSON, row.SourceSHA256, row.ExtractedBy, row.SkillVersion = scene.ProjectID, scene.ID, string(data), hash, extractedBy, skillVersion
	return &row, s.DB.Save(&row).Error
}

var (
	sceneTimeShiftMarkers = []string{"次日", "翌日", "第二天", "隔日", "数日后", "几日后", "三日后", "半月后", "一月后", "半年后", "一年后", "多年后", "清晨", "黎明", "正午", "午后", "黄昏", "傍晚", "入夜", "深夜"}
	sceneFlashbackMarkers = []string{"回忆", "闪回", "梦中", "梦境"}
)

// sceneOpening is where screenplay text announces a time jump or flashback; scanning the
// whole body would mistake dialogue such as "昨夜我梦中…" for a boundary.
func sceneOpening(scene *models.Scene) string {
	content := []rune(strings.TrimSpace(scene.Content))
	if len(content) > 24 {
		content = content[:24]
	}
	return scene.Title + "\n" + string(content)
}

func containsAny(text string, markers []string) bool {
	return slices.ContainsFunc(markers, func(m string) bool { return strings.Contains(text, m) })
}

// sceneBoundaryBreak reports why the current Scene is not a direct continuation of the
// previous one; empty means same space and time, where the whole end state carries over.
func sceneBoundaryBreak(previous, current *models.Scene) string {
	if a, b := strings.TrimSpace(previous.LocationName), strings.TrimSpace(current.LocationName); a != "" && b != "" && a != b {
		return "地点已变化"
	}
	if containsAny(sceneOpening(current), sceneTimeShiftMarkers) {
		return "时段已变化"
	}
	if containsAny(sceneOpening(previous), sceneFlashbackMarkers) != containsAny(sceneOpening(current), sceneFlashbackMarkers) {
		return "回忆或梦境切换"
	}
	return ""
}

// formatInheritedSceneState renders the reference block for the current Scene. Only
// characters visible in it are included; across a boundary only injuries carry over,
// because posture, props in hand and mood are reset by the cut in time or place.
func formatInheritedSceneState(state sceneEndStateData, current *models.Scene, breakReason string) string {
	names := splitCharacterNames(current.VisibleCharacters)
	if len(names) == 0 && !current.CharacterRolesSet {
		names = splitCharacterNames(current.Characters)
	}
	var lines []string
	for _, name := range names {
		c, ok := state.Characters[name]
		if !ok {
			continue
		}
		var fields []string
		add := func(label, value string) {
			if value != "" {
				fields = append(fields, label+" "+value)
			}
		}
		if breakReason == "" {
			add("姿态", c.Posture)
			add("面部", c.Face)
			add("持有", c.Holding)
			add("情绪", c.Emotion)
		}
		add("伤势", c.Injury)
		if len(fields) > 0 {
			lines = append(lines, name+"："+strings.Join(fields, "；"))
		}
	}
	if breakReason == "" {
		for _, name := range slices.Sorted(maps.Keys(state.Props)) {
			lines = append(lines, "道具 "+name+"："+state.Props[name])
		}
	}
	if len(lines) == 0 {
		return ""
	}
	header := "【上一场结束状态（参考·同一时空连续，开场应承接）】"
	if breakReason != "" {
		header = fmt.Sprintf("【上一场结束状态（参考·%s，仅延续伤势）】", breakReason)
	}
	return header + "\n" + strings.Join(lines, "\n") + "\n仅作连续性参考；与本场正文冲突时以本场正文为准。"
}

// inheritedSceneStartState returns the previous Scene's end state prepared for the current
// Scene, or empty when none was saved or it no longer matches the previous Scene.
func (s *Service) inheritedSceneStartState(current, previous *models.Scene) string {
	if previous == nil {
		return ""
	}
	view, err := s.loadSceneEndState(previous)
	if err != nil || view.State == nil || view.Stale {
		return ""
	}
	return formatInheritedSceneState(*view.State, current, sceneBoundaryBreak(previous, current))
}

func (s *Service) sceneEndStateSkillReady() bool {
	_, err := s.Skills.latestEnabledSkillByCode(sceneEndStateSkillCode)
	return err == nil
}

func (s *Service) HandleGetSceneEndState(c *gin.Context) {
	scene, ok := s.loadScene(c)
	if !ok {
		return
	}
	view, err := s.loadSceneEndState(scene)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	view.SkillReady = s.Skills != nil && s.sceneEndStateSkillReady()
	c.JSON(http.StatusOK, view)
}

func (s *Service) HandleSaveSceneEndState(c *gin.Context) {
	scene, ok := s.loadScene(c)
	if !ok {
		return
	}
	var req sceneEndStateData
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	hash, _, err := s.sceneEndStateSource(scene)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if _, err := s.saveSceneEndState(scene, req, hash, "manual", 0); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	s.HandleGetSceneEndState(c)
}

func (s *Service) HandleExtractSceneEndState(c *gin.Context) {
	scene, ok := s.loadScene(c)
	if !ok {
		return
	}
	if !s.textProviderConfigured() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "当前文生文 provider 未配置"})
		return
	}
	skill, err := s.Skills.latestEnabledSkillByCode(sceneEndStateSkillCode)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "人物结尾状态归档 Skill 尚未启用（待确认），目前只能手动填写结尾状态"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	hash, shots, err := s.sceneEndStateSource(scene)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var endStates []string
	for _, shot := range shots {
		if value := strings.TrimSpace(shot.EndState); value != "" {
			endStates = append(endStates, fmt.Sprintf("镜头%d：%s", shot.Order, value))
		}
	}
	if len(endStates) == 0 {
		endStates = append(endStates, "无")
	}
	output, err := s.Skills.ChatWithConfiguredOrFallbackSkill(scene.ProjectID, models.SkillStageContinuityReview, sceneEndStateSkillCode, s.TextProviderFact, "只输出完整合法JSON，不要Markdown或解释。", "记录本场结尾的人物与道具状态。", map[string]string{
		"characters": strings.Join(splitCharacterNames(scene.VisibleCharacters, scene.Characters), "、"), "location": scene.LocationName,
		"scene_content": scene.Content, "shot_end_states": strings.Join(endStates, "\n"),
	})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	state, err := parseSceneEndStateOutput(output)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	var current models.Scene
	if err := s.DB.First(&current, scene.ID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if latest, _, err := s.sceneEndStateSource(&current); err != nil || latest != hash {
		c.JSON(http.StatusConflict, gin.H{"error": "提取期间场景正文或镜头已变化，结果未保存，请重新提取"})
		return
	}
	if _, err := s.saveSceneEndState(scene, state, hash, "llm", skill.Version); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	s.HandleGetSceneEndState(c)
}
