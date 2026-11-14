package service

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	shotRefinePatchSchema     = "shot_refine.patch.v1"
	shotRefinePatchSkillCode  = "shot-refine-patch"
	shotRefineVersionEntity   = "scene_shot_refine"
	shotRefineVersionAction   = "refine_patch"
	shotRefineStateRolledBack = "rolled_back"
)

// shotRefinePatchSkill drafts field-level Shot edits. Its wording is user-approved; raise
// Version whenever the prompt changes so InitSystemSkills publishes the new revision.
var shotRefinePatchSkill = models.Skill{
	Name: "镜头补丁精修", Code: shotRefinePatchSkillCode, Operation: shotRefinePatchSkillCode, Version: 1,
	Description:    "按用户要求只修改Shot的导演字段，逐条给出修改理由；台词、时长、顺序不动",
	Stage:          models.SkillStageStoryboard,
	PromptTemplate: "Scene正文：{{scene_content}}\n当前Shot（含可编辑字段）：{{shots_json}}\n修改要求：{{instruction}}",
	SystemPrompt:   `你是镜头精修助手。用户会给出一个Scene的当前Shot列表和一条修改要求。只输出JSON对象：{"schema":"shot_refine.patch.v1","action":"apply"或"unsupported","edits":[{"shot_order":镜头序号,"field":字段名,"replacement":该字段的完整新值,"rationale":{"issue":用户要求指向的问题,"problem":当前写法的不足,"change_reason":为什么这样改,"result_reason":改后的效果}}]}。只能修改这些字段：shot_type、camera_angle、camera_movement、blocking、eye_line、performance、reaction、prompt_action、prompt_camera、prompt_lighting。Shot顺序、时长、对白及其分配、可见人物、道具都已锁定，不得修改，replacement里也不得写入新台词。用户的问句同样当作修改指令执行；要求中涉及锁定内容的部分直接跳过，其余照常修改；只有整条要求都与可编辑字段无关时才返回unsupported且edits为空。action为apply时至少给出一处实质修改。不要输出Markdown、解释或思考过程。`,
	IsSystem:       true, Enabled: true, SortOrder: 21,
}

// shotRefineEditableFields is the only surface a refine patch may touch. Order, duration,
// Dialogue and its ranges stay locked because they drive H3 timing and audio.
var shotRefineEditableFields = []string{"shot_type", "camera_angle", "camera_movement", "blocking", "eye_line", "performance", "reaction", "prompt_action", "prompt_camera", "prompt_lighting"}

func shotRefineFieldValue(shot models.Shot, field string) (string, bool) {
	switch field {
	case "shot_type":
		return shot.ShotType, true
	case "camera_angle":
		return shot.CameraAngle, true
	case "camera_movement":
		return shot.CameraMovement, true
	case "blocking":
		return shot.Blocking, true
	case "eye_line":
		return shot.EyeLine, true
	case "performance":
		return shot.Performance, true
	case "reaction":
		return shot.Reaction, true
	case "prompt_action":
		return shot.PromptAction, true
	case "prompt_camera":
		return shot.PromptCamera, true
	case "prompt_lighting":
		return shot.PromptLighting, true
	}
	return "", false
}

type shotRefineRationale struct {
	Issue        string `json:"issue"`
	Problem      string `json:"problem"`
	ChangeReason string `json:"change_reason"`
	ResultReason string `json:"result_reason"`
}

type shotRefineEdit struct {
	ShotOrder   int                 `json:"shot_order"`
	ShotID      uint                `json:"shot_id"`
	Field       string              `json:"field"`
	Original    string              `json:"original"`
	Replacement string              `json:"replacement"`
	Rationale   shotRefineRationale `json:"rationale"`
}

type shotRefinePatch struct {
	Schema string           `json:"schema"`
	Action string           `json:"action"`
	Edits  []shotRefineEdit `json:"edits"`
}

// shotRefineSourceHash fingerprints everything a patch was drafted against, so applying a
// stale suggestion over later manual edits is refused instead of silently merged.
func shotRefineSourceHash(content string, shots []models.Shot) string {
	type sourceShot struct {
		ID       uint              `json:"id"`
		Order    int               `json:"order"`
		Duration float64           `json:"duration"`
		Dialogue string            `json:"dialogue"`
		Fields   map[string]string `json:"fields"`
	}
	rows := make([]sourceShot, 0, len(shots))
	for _, shot := range shots {
		fields := map[string]string{}
		for _, field := range shotRefineEditableFields {
			fields[field], _ = shotRefineFieldValue(shot, field)
		}
		rows = append(rows, sourceShot{shot.ID, shot.Order, shot.Duration, shot.Dialogue, fields})
	}
	data, _ := json.Marshal(struct {
		Content string       `json:"content"`
		Shots   []sourceShot `json:"shots"`
	}{content, rows})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func shotRefinePromptShots(shots []models.Shot) string {
	rows := make([]map[string]any, 0, len(shots))
	for i, shot := range shots {
		row := map[string]any{"shot_order": i + 1, "duration": shot.Duration, "description": shot.Description, "dialogue": shot.Dialogue}
		for _, field := range shotRefineEditableFields {
			row[field], _ = shotRefineFieldValue(shot, field)
		}
		rows = append(rows, row)
	}
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	// Keep <Subject N> tags readable for the model.
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(rows)
	return strings.TrimSpace(buf.String())
}

var shotRefineQuotePattern = regexp.MustCompile(`[「“"『]([^「」“”"『』]+)[」”"』]`)

// checkShotRefineEdits validates every edit; any violation rejects the whole patch. Edits that
// equal the current value are dropped as no-ops. Returned edits carry the current value.
func checkShotRefineEdits(edits []shotRefineEdit, shots []models.Shot, spoken string, requireRationale bool) ([]shotRefineEdit, error) {
	index := map[uint]int{}
	for i, shot := range shots {
		index[shot.ID] = i
	}
	seen := map[string]bool{}
	out := make([]shotRefineEdit, 0, len(edits))
	for i, edit := range edits {
		n := i + 1
		pos, ok := index[edit.ShotID]
		if !ok {
			return nil, fmt.Errorf("第%d处修改指向的镜头不属于本场", n)
		}
		edit.Field = strings.TrimSpace(edit.Field)
		original, ok := shotRefineFieldValue(shots[pos], edit.Field)
		if !ok {
			return nil, fmt.Errorf("第%d处修改了锁定或未知字段%q", n, edit.Field)
		}
		edit.Replacement = strings.TrimSpace(edit.Replacement)
		if edit.Replacement == "" {
			return nil, fmt.Errorf("第%d处修改的新值为空", n)
		}
		key := fmt.Sprintf("%d/%s", edit.ShotID, edit.Field)
		if seen[key] {
			return nil, fmt.Errorf("镜头%d的%s被重复修改", pos+1, edit.Field)
		}
		seen[key] = true
		for _, quote := range shotRefineQuotePattern.FindAllStringSubmatch(edit.Replacement, -1) {
			if text := strings.TrimSpace(quote[1]); !strings.Contains(spoken, text) && !strings.Contains(original, text) {
				return nil, fmt.Errorf("第%d处修改写入了原文没有的台词「%s」", n, text)
			}
		}
		r := edit.Rationale
		if requireRationale && strings.TrimSpace(r.Issue+r.Problem+r.ChangeReason+r.ResultReason) == "" {
			return nil, fmt.Errorf("第%d处修改缺少理由", n)
		}
		if edit.Replacement == strings.TrimSpace(original) {
			continue
		}
		edit.ShotOrder, edit.Original = pos+1, original
		out = append(out, edit)
	}
	return out, nil
}

func parseShotRefinePatch(raw string, shots []models.Shot) (shotRefinePatch, error) {
	var patch shotRefinePatch
	if err := json.Unmarshal([]byte(extractJSON(raw)), &patch); err != nil {
		return patch, fmt.Errorf("返回的不是合法JSON: %w", err)
	}
	if schema := strings.TrimSpace(patch.Schema); schema != "" && schema != shotRefinePatchSchema {
		return patch, fmt.Errorf("schema必须是%s", shotRefinePatchSchema)
	}
	patch.Schema, patch.Action = shotRefinePatchSchema, strings.TrimSpace(patch.Action)
	switch patch.Action {
	case "unsupported":
		patch.Edits = nil
	case "apply":
		for i := range patch.Edits {
			order := patch.Edits[i].ShotOrder
			if order < 1 || order > len(shots) {
				return patch, fmt.Errorf("第%d处修改的镜头序号%d超出1-%d", i+1, order, len(shots))
			}
			patch.Edits[i].ShotID = shots[order-1].ID
		}
	default:
		return patch, fmt.Errorf("action必须是apply或unsupported")
	}
	return patch, nil
}

func (s *Service) shotRefineSource(db *gorm.DB, scene *models.Scene) ([]models.Shot, string, error) {
	var shots []models.Shot
	if err := db.Where("scene_id = ?", scene.ID).Order("order_num, id").Find(&shots).Error; err != nil {
		return nil, "", err
	}
	var dialogues []models.Dialogue
	if err := db.Where("scene_id = ?", scene.ID).Order("`order`, id").Find(&dialogues).Error; err != nil {
		return nil, "", err
	}
	spoken := []string{scene.Content}
	for _, d := range dialogues {
		spoken = append(spoken, d.Text)
	}
	return shots, strings.Join(spoken, "\n"), nil
}

// HandleShotRefinePatchDraft asks the refine Skill for a field-level patch and returns it for
// review. Nothing is persisted here.
func (s *Service) HandleShotRefinePatchDraft(c *gin.Context) {
	scene, ok := s.loadScene(c)
	if !ok {
		return
	}
	var req struct {
		Instruction string `json:"instruction"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Instruction) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请填写修改要求"})
		return
	}
	if s.Skills == nil || s.TextProviderFact == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "AI 文本服务未配置"})
		return
	}
	shots, spoken, err := s.shotRefineSource(s.DB, scene)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if len(shots) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "本场还没有已保存的Shot，请先保存导演镜头"})
		return
	}
	hash := shotRefineSourceHash(scene.Content, shots)
	output, err := s.Skills.ChatWithConfiguredOrFallbackSkill(scene.ProjectID, models.SkillStageStoryboard, shotRefinePatchSkillCode, s.TextProviderFact, "只输出完整合法JSON，不要Markdown或解释。", "按用户要求精修本场镜头。", map[string]string{
		"scene_content": scene.Content, "shots_json": shotRefinePromptShots(shots), "instruction": strings.TrimSpace(req.Instruction),
	})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "镜头精修失败: " + err.Error()})
		return
	}
	patch, err := parseShotRefinePatch(output, shots)
	if err == nil && patch.Action == "apply" {
		patch.Edits, err = checkShotRefineEdits(patch.Edits, shots, spoken, true)
		if err == nil && len(patch.Edits) == 0 {
			err = errors.New("模型没有给出实质修改")
		}
	}
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "精修补丁未通过校验，已整体拒绝：" + err.Error(), "raw_output": output})
		return
	}
	if patch.Edits == nil {
		patch.Edits = []shotRefineEdit{}
	}
	c.JSON(http.StatusOK, gin.H{"patch": patch, "source_hash": hash, "skill_code": shotRefinePatchSkillCode, "persisted": false})
}

var errShotRefineStale = errors.New("场景正文或镜头已在生成建议后变化，请重新生成精修建议")

// applyShotRefineValues writes field values through ShotService.UpdateShot so validation,
// prompt history and scene staleness follow the same path as a manual edit.
func applyShotRefineValues(tx *gorm.DB, edits []shotRefineEdit, value func(shotRefineEdit) string) error {
	updates := map[uint]map[string]any{}
	var order []uint
	for _, edit := range edits {
		if updates[edit.ShotID] == nil {
			updates[edit.ShotID] = map[string]any{}
			order = append(order, edit.ShotID)
		}
		updates[edit.ShotID][edit.Field] = value(edit)
	}
	shots := NewShotService(tx)
	for _, id := range order {
		if _, err := shots.UpdateShot(id, updates[id]); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) HandleApplyShotRefinePatch(c *gin.Context) {
	scene, ok := s.loadScene(c)
	if !ok {
		return
	}
	var req struct {
		SourceHash  string           `json:"source_hash"`
		Instruction string           `json:"instruction"`
		Edits       []shotRefineEdit `json:"edits"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.Edits) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请至少选择一处修改"})
		return
	}
	var version models.PromptVersion
	var edits []shotRefineEdit
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		shots, spoken, err := s.shotRefineSource(tx, scene)
		if err != nil {
			return err
		}
		if shotRefineSourceHash(scene.Content, shots) != strings.TrimSpace(req.SourceHash) {
			return errShotRefineStale
		}
		edits, err = checkShotRefineEdits(req.Edits, shots, spoken, false)
		if err != nil {
			return err
		}
		if len(edits) == 0 {
			return errors.New("所选修改与当前值相同")
		}
		if err := applyShotRefineValues(tx, edits, func(e shotRefineEdit) string { return e.Replacement }); err != nil {
			return err
		}
		content, _ := json.Marshal(edits)
		metadata, _ := json.Marshal(map[string]any{"instruction": strings.TrimSpace(req.Instruction), "source_hash": req.SourceHash, "skill_code": shotRefinePatchSkillCode})
		version = models.PromptVersion{ProjectID: scene.ProjectID, EntityType: shotRefineVersionEntity, EntityID: scene.ID, Content: string(content), Action: shotRefineVersionAction, State: PromptVersionStateApplied, Metadata: string(metadata)}
		return tx.Create(&version).Error
	})
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errShotRefineStale) {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	if s.Projects != nil {
		s.Projects.CancelStaleSceneTasks(scene.ID, scene.ImageTaskID, scene.VideoTaskID)
	}
	c.JSON(http.StatusOK, gin.H{"version_id": version.ID, "applied": len(edits)})
}

// HandleRollbackShotRefinePatch restores the values an applied patch replaced. It refuses
// when any patched field was edited again afterwards, rather than overwrite that edit.
func (s *Service) HandleRollbackShotRefinePatch(c *gin.Context) {
	scene, ok := s.loadScene(c)
	if !ok {
		return
	}
	versionID, err := strconv.ParseUint(c.Param("vid"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid version id"})
		return
	}
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		var version models.PromptVersion
		if err := tx.Where("id = ? AND project_id = ? AND entity_type = ? AND entity_id = ?", versionID, scene.ProjectID, shotRefineVersionEntity, scene.ID).First(&version).Error; err != nil {
			return err
		}
		if version.State != PromptVersionStateApplied {
			return errors.New("该次精修已撤销")
		}
		var edits []shotRefineEdit
		if err := json.Unmarshal([]byte(version.Content), &edits); err != nil {
			return fmt.Errorf("精修记录已损坏: %w", err)
		}
		shots, _, err := s.shotRefineSource(tx, scene)
		if err != nil {
			return err
		}
		byID := map[uint]models.Shot{}
		for _, shot := range shots {
			byID[shot.ID] = shot
		}
		for _, edit := range edits {
			shot, ok := byID[edit.ShotID]
			current, known := shotRefineFieldValue(shot, edit.Field)
			if !ok || !known || strings.TrimSpace(current) != edit.Replacement {
				return fmt.Errorf("镜头%d的%s在精修后又被修改或已删除，无法自动撤销", edit.ShotOrder, edit.Field)
			}
		}
		if err := applyShotRefineValues(tx, edits, func(e shotRefineEdit) string { return e.Original }); err != nil {
			return err
		}
		return tx.Model(&version).Update("state", shotRefineStateRolledBack).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "精修记录不存在"})
		return
	} else if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	if s.Projects != nil {
		s.Projects.CancelStaleSceneTasks(scene.ID, scene.ImageTaskID, scene.VideoTaskID)
	}
	c.JSON(http.StatusOK, gin.H{"rolled_back": true})
}

func (s *Service) HandleListShotRefinePatches(c *gin.Context) {
	scene, ok := s.loadScene(c)
	if !ok {
		return
	}
	var versions []models.PromptVersion
	if err := s.DB.Where("project_id = ? AND entity_type = ? AND entity_id = ?", scene.ProjectID, shotRefineVersionEntity, scene.ID).Order("id DESC").Limit(10).Find(&versions).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	items := make([]gin.H, 0, len(versions))
	for _, version := range versions {
		var edits []shotRefineEdit
		_ = json.Unmarshal([]byte(version.Content), &edits)
		var metadata struct {
			Instruction string `json:"instruction"`
		}
		_ = json.Unmarshal([]byte(version.Metadata), &metadata)
		items = append(items, gin.H{"id": version.ID, "state": version.State, "instruction": metadata.Instruction, "edits": edits, "created_at": version.CreatedAt})
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}
