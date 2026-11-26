package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
)

type studioExecutionClip struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Duration float64 `json:"duration"`
}
type studioExecutionAsset struct {
	ID    string                `json:"id"`
	Name  string                `json:"name"`
	Clips []studioExecutionClip `json:"clips"`
}
type studioExecutionObject struct {
	ID       string    `json:"id"`
	AssetID  string    `json:"asset_id"`
	Name     string    `json:"name"`
	Position []float64 `json:"position"`
	Rotation []float64 `json:"rotation"`
	Scale    []float64 `json:"scale"`
}
type studioExecutionContext struct {
	Revision string                  `json:"revision"`
	Assets   []studioExecutionAsset  `json:"assets"`
	Objects  []studioExecutionObject `json:"objects"`
}
type studioExecutionSource struct {
	Scene            models.Scene                    `json:"scene"`
	Shots            []models.Shot                   `json:"shots"`
	Dialogues        []models.Dialogue               `json:"dialogues"`
	DialogueTimeline studioExecutionDialogueTimeline `json:"dialogue_timeline"`
}
type studioExecutionBinding struct {
	Character string `json:"character"`
	ObjectID  string `json:"object_id"`
}
type studioExecutionCamera struct {
	Position []float64 `json:"position"`
	Target   []float64 `json:"target"`
	FOV      float64   `json:"fov"`
}
type studioExecutionMotion struct {
	ObjectID string    `json:"object_id"`
	ClipID   string    `json:"clip_id"`
	Start    []float64 `json:"start"`
	End      []float64 `json:"end"`
	Duration float64   `json:"duration"`
}
type studioExecutionShot struct {
	ShotID      uint                   `json:"shot_id"`
	Duration    float64                `json:"duration"`
	Camera      *studioExecutionCamera `json:"camera"`
	Motion      *studioExecutionMotion `json:"motion"`
	Execution   *studioExecutionTracks `json:"execution,omitempty"`
	Evidence    []string               `json:"evidence"`
	Assumptions []string               `json:"assumptions"`
	Blockers    []string               `json:"blockers"`
}
type studioExecutionPlan struct {
	SourceHash      string                   `json:"source_hash"`
	ContextRevision string                   `json:"context_revision"`
	Objects         []studioExecutionObject  `json:"objects"`
	Bindings        []studioExecutionBinding `json:"bindings"`
	Shots           []studioExecutionShot    `json:"shots"`
	Assumptions     []string                 `json:"assumptions"`
	Blockers        []string                 `json:"blockers"`
}

func studioExecutionVector(v []float64, min, max float64) bool {
	if len(v) != 3 {
		return false
	}
	for _, n := range v {
		if !previsFinite(n, min, max) {
			return false
		}
	}
	return true
}
func studioExecutionTexts(values []string, required bool) bool {
	if values == nil || len(values) > 64 || (required && len(values) == 0) {
		return false
	}
	for _, s := range values {
		if strings.TrimSpace(s) == "" || len(s) > 8000 {
			return false
		}
	}
	return true
}
func validateStudioExecutionContext(ctx studioExecutionContext) error {
	if strings.TrimSpace(ctx.Revision) == "" || len(ctx.Revision) > 200 || len(ctx.Assets) == 0 || len(ctx.Assets) > 16384 || ctx.Objects == nil || len(ctx.Objects) > 4096 {
		return fmt.Errorf("执行目录或修订无效")
	}
	assets := map[string]bool{}
	for _, a := range ctx.Assets {
		if a.ID == "" || len(a.ID) > 160 || a.Name == "" || len(a.Name) > 500 || assets[a.ID] || a.Clips == nil || len(a.Clips) > 256 {
			return fmt.Errorf("素材身份或动作目录无效")
		}
		assets[a.ID] = true
		clips := map[string]bool{}
		for _, clip := range a.Clips {
			if clip.ID == "" || len(clip.ID) > 160 || clip.Name == "" || len(clip.Name) > 500 || clips[clip.ID] || !previsFinite(clip.Duration, 0.001, 86400) {
				return fmt.Errorf("动作资源无效")
			}
			clips[clip.ID] = true
		}
	}
	ids := map[string]bool{}
	for _, o := range ctx.Objects {
		if err := validateStudioExecutionObject(o, assets, ids); err != nil {
			return err
		}
	}
	return nil
}
func validateStudioExecutionObject(o studioExecutionObject, assets, ids map[string]bool) error {
	if o.ID == "" || len(o.ID) > 160 || o.Name == "" || len(o.Name) > 500 || ids[o.ID] || !assets[o.AssetID] || !studioExecutionVector(o.Position, -1000, 1000) || !studioExecutionVector(o.Rotation, -math.Pi*2, math.Pi*2) || !studioExecutionVector(o.Scale, 0.001, 1000) {
		return fmt.Errorf("布景对象或真实素材引用无效")
	}
	ids[o.ID] = true
	return nil
}
func validateStudioExecutionPlan(plan *studioExecutionPlan, source studioExecutionSource, ctx studioExecutionContext, sourceHash string) error {
	if plan.SourceHash != sourceHash || plan.ContextRevision != ctx.Revision {
		return fmt.Errorf("模型来源或目录修订不匹配")
	}
	if plan.Objects == nil || len(plan.Objects) > 4096 || plan.Bindings == nil || len(plan.Bindings) > 128 || len(plan.Shots) != len(source.Shots) || !studioExecutionTexts(plan.Assumptions, false) || !studioExecutionTexts(plan.Blockers, false) {
		return fmt.Errorf("执行草稿结构或镜头覆盖无效")
	}
	assets := map[string]bool{}
	assetByID := map[string]studioExecutionAsset{}
	objects := map[string]studioExecutionObject{}
	ids := map[string]bool{}
	for _, a := range ctx.Assets {
		assets[a.ID] = true
		assetByID[a.ID] = a
	}
	for _, o := range ctx.Objects {
		objects[o.ID] = o
	}
	for _, o := range plan.Objects {
		if err := validateStudioExecutionObject(o, assets, ids); err != nil {
			return err
		}
		if existing, ok := objects[o.ID]; ok && existing.AssetID != o.AssetID {
			return fmt.Errorf("不能替换已有对象素材")
		}
		objects[o.ID] = o
	}
	characters := map[string]bool{}
	usedObjects := map[string]bool{}
	visible := source.Scene.VisibleCharacters
	if !source.Scene.CharacterRolesSet {
		visible = source.Scene.Characters
	}
	for _, b := range plan.Bindings {
		if b.Character == "" || characters[b.Character] || usedObjects[b.ObjectID] || objects[b.ObjectID].ID == "" {
			return fmt.Errorf("人物绑定重复或引用不存在")
		}
		allowed := false
		for _, name := range strings.FieldsFunc(visible, func(r rune) bool { return strings.ContainsRune(",，、;；\n", r) }) {
			if strings.TrimSpace(name) == b.Character {
				allowed = true
			}
		}
		if !allowed {
			return fmt.Errorf("不得把未列为可见的角色放入画面：%s", b.Character)
		}
		characters[b.Character] = true
		usedObjects[b.ObjectID] = true
	}
	for _, name := range strings.FieldsFunc(visible, func(r rune) bool { return strings.ContainsRune(",，、;；\n", r) }) {
		if name = strings.TrimSpace(name); name != "" && !characters[name] {
			plan.Blockers = append(plan.Blockers, "可见角色尚未绑定真实对象："+name)
		}
	}
	executionIDs := map[string]bool{}
	hasLegacyMotion := false
	for _, shot := range plan.Shots {
		if shot.Execution != nil {
			for _, actor := range shot.Execution.Actors {
				executionIDs[actor.TargetID] = true
			}
		}
		hasLegacyMotion = hasLegacyMotion || shot.Motion != nil
	}
	if len(executionIDs) > 32 {
		return fmt.Errorf("整份执行计划超过32个角色，请拆分场次")
	}
	if len(executionIDs) > 0 && hasLegacyMotion {
		return fmt.Errorf("镜内执行角色不能与旧走位混用于同一计划")
	}
	for i, original := range source.Shots {
		shot := &plan.Shots[i]
		if shot.ShotID != original.ID || shot.Duration != original.Duration || !studioExecutionTexts(shot.Evidence, true) || !studioExecutionTexts(shot.Assumptions, false) || !studioExecutionTexts(shot.Blockers, false) {
			return fmt.Errorf("第%d镜身份、时长或依据无效", i+1)
		}
		evidenceText := source.Scene.Content + "\n" + original.Description + "\n" + original.Performance + "\n" + original.Blocking + "\n" + original.CameraMovement + "\n" + original.FocusSubject
		for _, quote := range shot.Evidence {
			if !strings.Contains(evidenceText, quote) {
				return fmt.Errorf("第%d镜依据不在原文或导演字段中", i+1)
			}
		}
		if err := validateStudioExecutionTracks(shot.Execution, original, plan, objects, assetByID); err != nil {
			return err
		}
		hasActors := shot.Execution != nil && len(shot.Execution.Actors) > 0
		if hasActors && shot.Motion != nil {
			return fmt.Errorf("镜内执行不能混用旧走位")
		}
		if shot.Camera != nil {
			c := shot.Camera
			if !studioExecutionVector(c.Position, -1000, 1000) || !studioExecutionVector(c.Target, -1000, 1000) || !previsFinite(c.FOV, 15, 90) {
				return fmt.Errorf("机位无效")
			}
			distance := 0.0
			for j := 0; j < 3; j++ {
				distance += math.Pow(c.Position[j]-c.Target[j], 2)
			}
			if distance < 1e-6 {
				return fmt.Errorf("机位与焦点重合")
			}
		} else if shot.Execution == nil || shot.Execution.Camera == nil {
			shot.Blockers = append(shot.Blockers, "尚无可执行机位")
		}
		if movement := strings.TrimSpace(original.CameraMovement); movement != "" && movement != "固定" && movement != "static" && movement != "固定镜头" && !studioMovingCamera(shot.Execution) {
			shot.Blockers = append(shot.Blockers, "现有逐镜采样器不支持该运镜："+movement)
		}
		if m := shot.Motion; m != nil {
			o, ok := objects[m.ObjectID]
			if !ok || !studioExecutionVector(m.Start, -1000, 1000) || !studioExecutionVector(m.End, -1000, 1000) || m.Start[1] != m.End[1] || !previsFinite(m.Duration, 0.1, original.Duration) {
				return fmt.Errorf("表演对象、走位或时长无效")
			}
			distance := math.Hypot(m.Start[0]-m.End[0], m.Start[2]-m.End[2])
			if distance < 0.001 {
				return fmt.Errorf("当前执行结构不支持原地动作")
			}
			found := false
			for _, clip := range assetByID[o.AssetID].Clips {
				if clip.ID == m.ClipID {
					found = true
				}
			}
			if !found {
				return fmt.Errorf("表演引用的动作不在该对象资产目录")
			}
		} else if !hasActors && (strings.TrimSpace(original.Performance) != "" || len(original.ActionTimeline) > 0) {
			shot.Blockers = append(shot.Blockers, "原导演表演尚未绑定可执行动作；不可用静止画面替代")
		}
		if len(original.ActionTimeline) > 1 && !hasActors {
			shot.Blockers = append(shot.Blockers, "现有单对象直线走位不能证明覆盖多段动作时间轴")
		}
	}
	return nil
}

func (s *Service) studioExecutionSource(sceneID uint) (studioExecutionSource, string, error) {
	var source studioExecutionSource
	if err := s.DB.First(&source.Scene, sceneID).Error; err != nil {
		return source, "", err
	}
	if source.Scene.Archived || source.Scene.DirectorSourceStale || strings.TrimSpace(source.Scene.Content) == "" {
		return source, "", fmt.Errorf("场次已归档、正文为空或导演来源已过期")
	}
	if err := s.DB.Where("scene_id = ?", sceneID).Order("order_num ASC").Find(&source.Shots).Error; err != nil {
		return source, "", err
	}
	if len(source.Shots) == 0 || len(source.Shots) > 64 {
		return source, "", fmt.Errorf("需要1到64个已有导演镜头")
	}
	total := 0.0
	for i, shot := range source.Shots {
		if shot.Order != i+1 || !previsFinite(shot.Duration, 0.2, 86400) {
			return source, "", fmt.Errorf("已有导演镜头顺序或时长无效")
		}
		total += shot.Duration
	}
	if total > 86400 {
		return source, "", fmt.Errorf("镜头总时长超限")
	}
	source.Dialogues = []models.Dialogue{}
	if err := s.DB.Where("scene_id = ? AND project_id = ?", source.Scene.ID, source.Scene.ProjectID).Order("`order`, id").Find(&source.Dialogues).Error; err != nil {
		return source, "", err
	}
	source.DialogueTimeline = buildStudioExecutionDialogueTimeline(source.Scene, source.Shots, source.Dialogues)
	raw, err := json.Marshal(source)
	if err != nil {
		return source, "", err
	}
	return source, hashString(string(raw)), nil
}
func (s *Service) HandleGetStudioExecutionSource(c *gin.Context) {
	scene, ok := s.loadScene(c)
	if !ok {
		return
	}
	source, hash, err := s.studioExecutionSource(scene.ID)
	if err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"source": source, "source_hash": hash, "persisted": false})
}
func (s *Service) HandlePlanStudioExecution(c *gin.Context) {
	scene, ok := s.loadScene(c)
	if !ok {
		return
	}
	var req struct {
		SourceHash string                 `json:"source_hash"`
		Context    studioExecutionContext `json:"context"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2<<20)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&req) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		c.JSON(400, gin.H{"error": "执行规划请求无效或超过2MiB"})
		return
	}
	if err := validateStudioExecutionContext(req.Context); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	source, hash, err := s.studioExecutionSource(scene.ID)
	if err != nil || hash != req.SourceHash {
		c.JSON(409, gin.H{"error": "导演来源已变化，请重新读取"})
		return
	}
	if s.Skills == nil || s.TextProviderFact == nil {
		c.JSON(503, gin.H{"error": "执行规划Skill或文本模型未配置"})
		return
	}
	skill, err := s.Skills.ConfiguredOrFallbackSkill(scene.ProjectID, string(models.SkillStageStoryboard), studioExecutionSkillCode)
	if err != nil || skill == nil {
		c.JSON(503, gin.H{"error": "3D执行规划Skill不可用或已禁用"})
		return
	}
	// A typed zero-value is shape documentation, never a sample plan to apply.
	shape := studioExecutionPlan{Objects: []studioExecutionObject{{}}, Bindings: []studioExecutionBinding{{}}, Shots: []studioExecutionShot{{Camera: &studioExecutionCamera{}, Motion: &studioExecutionMotion{}, Execution: &studioExecutionTracks{Version: 1, Camera: &studioExecutionCameraTrack{Keys: []studioExecutionKey{{}}}, Actors: []studioExecutionActorTrack{{Beats: []studioExecutionBeat{{}}}}}, Evidence: []string{}, Assumptions: []string{}, Blockers: []string{}}}, Assumptions: []string{}, Blockers: []string{}}
	data, _ := json.Marshal(gin.H{"source": source, "source_hash": hash, "context": req.Context, "output_shape": shape, "capabilities": gin.H{"units": "meters", "y_up": true, "rotation": "radians ±2pi", "position_bounds": []int{-1000, 1000}, "fov_bounds": []int{15, 90}, "camera": "legacy camera is fixed; optional execution.version=1 camera.keys supports linear position/target/fov at strictly increasing shot-relative seconds including 0 and full duration; max64 keys; null with blockers when unresolved", "motion": "legacy motion is one nonzero straight XZ segment. Optional execution.actors supports max32 actual existing object targets and max128 ordered beats total, including in-place skeletal clips with explicit yaw. Each beat uses start/end/from/to/yaw/animation{index,name,duration}/sourceIndex; sourceIndex references original action_timeline with exact interval and bound subject, every subject/beat must be covered. Actual clip index/name/duration from that object's context asset required. No legacy motion anywhere in plans with execution actors. Constant base Y follows terrain. Overlap/collision/visual quality not approved by schema; unresolved requirements must remain blockers", "arrays": "all arrays required, empty allowed except shot evidence; preserve shot IDs/order/duration", "objects": "new placements or transform edits; reuse existing IDs without changing asset_id", "evidence": "exact quotes from scene content or corresponding shot description/performance/blocking/camera_movement/focus_subject", "validation": "structure and references only; not semantic, collision or playback acceptance"}})
	output, provenance, err := s.Skills.ChatWithConfiguredOrFallbackSkillTraced(scene.ProjectID, string(models.SkillStageStoryboard), studioExecutionSkillCode, s.TextProviderFact, "", string(data), nil)
	if err != nil {
		c.JSON(502, gin.H{"error": "执行规划失败：" + err.Error()})
		return
	}
	if provenance == nil {
		c.JSON(503, gin.H{"error": "执行规划缺少Skill审计来源"})
		return
	}
	if c.Request.Context().Err() != nil {
		return
	}
	_, latest, err := s.studioExecutionSource(scene.ID)
	if err != nil || latest != hash {
		c.JSON(409, gin.H{"error": "模型规划期间导演来源已变化，草稿已丢弃"})
		return
	}
	var plan studioExecutionPlan
	if len(output) > 1<<20 {
		c.JSON(422, gin.H{"error": "执行规划响应超过1MiB"})
		return
	}
	decode := json.NewDecoder(bytes.NewBufferString(stripH3CodeFence(output)))
	decode.DisallowUnknownFields()
	if decode.Decode(&plan) != nil || decode.Decode(&struct{}{}) != io.EOF {
		c.JSON(422, gin.H{"error": "执行规划返回了未知字段或无效JSON"})
		return
	}
	if err := validateStudioExecutionPlan(&plan, source, req.Context, hash); err != nil {
		c.JSON(422, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"plan": plan, "source": source, "skill_provenance": provenance, "persisted": false, "applied": false, "validation": "structure_and_catalog_references_only", "catalog_authority": "client_snapshot_revalidate_before_apply"})
}
