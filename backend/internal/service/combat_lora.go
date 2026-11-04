package service

import (
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"comfyui-console/internal/models"
)

const combatTemplateCode = "minimax_h3_ref2v_combat"
const combatPowerNodeID = "156"
const combatPowerClass = "Power Lora Loader (rgthree)"
const combatLoraWeightLimit = 100.0

// CombatLora is one ordered rgthree Power Lora Loader entry.
type CombatLora struct {
	On       bool    `json:"on"`
	Lora     string  `json:"lora"`
	Strength float64 `json:"strength"`
}
type CombatLoraConfig struct {
	Mode  string       `json:"mode"` // inherit or custom
	Loras []CombatLora `json:"loras"`
}
type CombatLoraResolved struct {
	Config CombatLoraConfig `json:"config"`
	Source string           `json:"source"` // template, project, scene
	Files  []string         `json:"files"`
}

func cloneCombatLoras(in []CombatLora) []CombatLora { return append([]CombatLora{}, in...) }

func validCombatLoraPath(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || filepath.IsAbs(name) || strings.HasPrefix(name, "/") || strings.HasPrefix(name, `\`) || strings.ContainsAny(name, "\x00\r\n") || (len(name) >= 2 && name[1] == ':') {
		return false
	}
	for _, part := range strings.FieldsFunc(name, func(r rune) bool { return r == '/' || r == '\\' }) {
		if part == ".." {
			return false
		}
	}
	return true
}
func validateCombatConfig(c CombatLoraConfig, allowInherit bool) error {
	if c.Mode == "inherit" {
		if !allowInherit {
			return fmt.Errorf("project config cannot inherit")
		}
		return nil
	}
	if c.Mode != "custom" {
		return fmt.Errorf("mode must be inherit or custom")
	}
	for i, v := range c.Loras {
		if !validCombatLoraPath(v.Lora) {
			return fmt.Errorf("loras[%d].lora must be a safe relative filename", i)
		}
		if math.IsNaN(v.Strength) || math.IsInf(v.Strength, 0) || v.Strength < -combatLoraWeightLimit || v.Strength > combatLoraWeightLimit {
			return fmt.Errorf("loras[%d].strength must be finite and within -100..100", i)
		}
	}
	return nil
}
func parseCombatConfig(raw string) (CombatLoraConfig, bool) {
	if strings.TrimSpace(raw) == "" {
		return CombatLoraConfig{}, false
	}
	var c CombatLoraConfig
	if json.Unmarshal([]byte(raw), &c) != nil || validateCombatConfig(c, true) != nil {
		return CombatLoraConfig{}, false
	}
	return c, true
}

func powerLorasFromWorkflow(workflow map[string]any) ([]CombatLora, error) {
	node, ok := workflow[combatPowerNodeID].(map[string]any)
	if !ok {
		return legacyLorasFromWorkflow(workflow)
	}
	if node["class_type"] != combatPowerClass {
		return legacyLorasFromWorkflow(workflow)
	}
	inputs, ok := node["inputs"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("combat power loader inputs mismatch")
	}
	out := []CombatLora{}
	keys := []string{}
	for key := range inputs {
		if strings.HasPrefix(key, "lora_") {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		a, _ := strconv.Atoi(strings.TrimPrefix(keys[i], "lora_"))
		b, _ := strconv.Atoi(strings.TrimPrefix(keys[j], "lora_"))
		return a < b
	})
	for _, key := range keys {
		raw := inputs[key]
		obj, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("combat power loader %s mismatch", key)
		}
		on, ok1 := obj["on"].(bool)
		name, ok2 := obj["lora"].(string)
		strength, ok3 := obj["strength"].(float64)
		if !ok1 || !ok2 || !ok3 {
			return nil, fmt.Errorf("combat power loader %s mismatch", key)
		}
		out = append(out, CombatLora{on, name, strength})
	}
	return out, validateCombatConfig(CombatLoraConfig{Mode: "custom", Loras: out}, false)
}
func legacyLorasFromWorkflow(workflow map[string]any) ([]CombatLora, error) {
	out := []CombatLora{}
	for _, id := range []string{"156", "157", "158", "159"} {
		node, ok := workflow[id].(map[string]any)
		if !ok || node["class_type"] != "LoraLoaderModelOnly" {
			return nil, fmt.Errorf("combat workflow has no supported LoRA loader")
		}
		inputs, ok := node["inputs"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("legacy combat loader %s mismatch", id)
		}
		name, nOK := inputs["lora_name"].(string)
		strength, sOK := inputs["strength_model"].(float64)
		if !nOK || !sOK {
			return nil, fmt.Errorf("legacy combat loader %s mismatch", id)
		}
		out = append(out, CombatLora{true, name, strength})
	}
	return out, nil
}
func combatTemplateLoras(db *gorm.DB) ([]CombatLora, error) {
	var tpl models.Template
	if err := db.Where("code = ?", combatTemplateCode).First(&tpl).Error; err != nil {
		return nil, err
	}
	var workflow map[string]any
	if err := json.Unmarshal([]byte(tpl.WorkflowJSON), &workflow); err != nil {
		return nil, fmt.Errorf("combat template workflow invalid: %w", err)
	}
	return powerLorasFromWorkflow(workflow)
}
func resolveCombatLorasDB(db *gorm.DB, projectID, sceneID uint) (CombatLoraResolved, error) {
	base := CombatLoraResolved{Config: CombatLoraConfig{Mode: "custom", Loras: []CombatLora{}}, Source: "template", Files: []string{}}
	var p models.Project
	if err := db.First(&p, projectID).Error; err != nil {
		return base, err
	}
	if c, ok := parseCombatConfig(p.CombatLorasJSON); ok && c.Mode == "custom" {
		base.Config, base.Source = c, "project"
	}
	if sceneID != 0 {
		var sc models.Scene
		if err := db.Where("id=? AND project_id=?", sceneID, projectID).First(&sc).Error; err != nil {
			return base, err
		}
		if c, ok := parseCombatConfig(sc.CombatLorasJSON); ok && c.Mode == "custom" {
			base.Config, base.Source = c, "scene"
		}
	}
	if base.Source == "template" {
		loras, err := combatTemplateLoras(db)
		if err != nil {
			return base, err
		}
		base.Config.Loras = loras
	}
	for _, l := range base.Config.Loras {
		base.Files = append(base.Files, l.Lora)
	}
	return base, nil
}
func (s *Service) resolveCombatLoras(projectID, sceneID uint) (CombatLoraResolved, error) {
	return resolveCombatLorasDB(s.DB, projectID, sceneID)
}
func bindCombatPatch(c *gin.Context, allowInherit bool) (CombatLoraConfig, bool, error) {
	var raw json.RawMessage
	if err := c.ShouldBindJSON(&raw); err != nil {
		return CombatLoraConfig{}, false, err
	}
	if string(raw) == "null" || string(raw) == "{}" {
		return CombatLoraConfig{}, true, nil
	}
	var cfg CombatLoraConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return cfg, false, err
	}
	return cfg, false, validateCombatConfig(cfg, allowInherit)
}
func parseRouteUint(c *gin.Context, key string) (uint, error) {
	n, err := strconv.ParseUint(c.Param(key), 10, 32)
	return uint(n), err
}
func (s *Service) HandleGetCombatLoras(c *gin.Context) {
	pid, err := parseRouteUint(c, "id")
	if err != nil || pid == 0 {
		c.JSON(400, gin.H{"error": "invalid project id"})
		return
	}
	var sid uint
	if c.Param("sid") != "" {
		sid, err = parseRouteUint(c, "sid")
		if err != nil || sid == 0 {
			c.JSON(400, gin.H{"error": "invalid scene id"})
			return
		}
	}
	resolved, err := s.resolveCombatLoras(pid, sid)
	if err != nil {
		status := 500
		if err == gorm.ErrRecordNotFound {
			status = 404
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, resolved)
}
func (s *Service) HandlePatchProjectCombatLoras(c *gin.Context) { s.patchCombatLoras(c, false) }
func (s *Service) HandlePatchSceneCombatLoras(c *gin.Context)   { s.patchCombatLoras(c, true) }
func (s *Service) patchCombatLoras(c *gin.Context, scene bool) {
	pid, e := parseRouteUint(c, "id")
	if e != nil || pid == 0 {
		c.JSON(400, gin.H{"error": "invalid id"})
		return
	}
	var sid uint
	if scene {
		sid, e = parseRouteUint(c, "sid")
		if e != nil {
			c.JSON(400, gin.H{"error": "invalid id"})
			return
		}
		var sc models.Scene
		if e = s.DB.Where("id=? AND project_id=?", sid, pid).First(&sc).Error; e != nil {
			c.JSON(404, gin.H{"error": "scene not found"})
			return
		}
	} else {
		var p models.Project
		if s.DB.First(&p, pid).Error != nil {
			c.JSON(404, gin.H{"error": "project not found"})
			return
		}
	}
	cfg, clear, e := bindCombatPatch(c, scene)
	if e != nil {
		c.JSON(400, gin.H{"error": e.Error()})
		return
	}
	raw := ""
	if !clear {
		b, _ := json.Marshal(cfg)
		raw = string(b)
	}
	var q *gorm.DB
	if scene {
		q = s.DB.Model(&models.Scene{}).Where("id=? AND project_id=?", sid, pid)
	} else {
		q = s.DB.Model(&models.Project{}).Where("id=?", pid)
	}
	if e = q.Update("combat_loras_json", raw).Error; e != nil {
		c.JSON(500, gin.H{"error": e.Error()})
		return
	}
	resolved, e := s.resolveCombatLoras(pid, sid)
	if e != nil {
		c.JSON(500, gin.H{"error": e.Error()})
		return
	}
	c.JSON(200, resolved)
}

func convertLegacyCombatWorkflow(workflow map[string]any) {
	first, ok := workflow["156"].(map[string]any)
	if !ok || first["class_type"] != "LoraLoaderModelOnly" {
		return
	}
	oldInputs, _ := first["inputs"].(map[string]any)
	model := oldInputs["model"]
	workflow[combatPowerNodeID] = map[string]any{"class_type": combatPowerClass, "inputs": map[string]any{"model": model}, "_meta": map[string]any{"title": "Power Lora Loader (rgthree)"}}
	delete(workflow, "157")
	delete(workflow, "158")
	delete(workflow, "159")
	for _, raw := range workflow {
		node, _ := raw.(map[string]any)
		inputs, _ := node["inputs"].(map[string]any)
		for k, v := range inputs {
			a, ok := v.([]any)
			if ok && len(a) == 2 {
				id := fmt.Sprint(a[0])
				if id == "157" || id == "158" || id == "159" {
					inputs[k] = []any{combatPowerNodeID, a[1]}
				}
			}
		}
	}
}

func deriveCombatWorkflow(workflow map[string]any, loras []CombatLora) error {
	if err := validateCombatConfig(CombatLoraConfig{Mode: "custom", Loras: loras}, false); err != nil {
		return err
	}
	node, ok := workflow[combatPowerNodeID].(map[string]any)
	if !ok || node["class_type"] != combatPowerClass {
		return fmt.Errorf("combat power loader graph mismatch")
	}
	inputs, ok := node["inputs"].(map[string]any)
	if !ok {
		return fmt.Errorf("combat power loader inputs mismatch")
	}
	for k := range inputs {
		if strings.HasPrefix(k, "lora_") {
			delete(inputs, k)
		}
	}
	for i, l := range loras {
		// rgthree applies kwargs in JSON order; padding prevents lora_10 sorting before lora_2.
		inputs[fmt.Sprintf("lora_%09d", i+1)] = map[string]any{"on": l.On, "lora": l.Lora, "strength": l.Strength}
	}
	return nil
}
func cloneCombatWorkflowSnapshot(raw, oldTaskID, newTaskID string) string {
	if strings.TrimSpace(raw) == "" {
		return raw
	}
	var workflow map[string]any
	if json.Unmarshal([]byte(raw), &workflow) != nil {
		return raw
	}
	for _, r := range workflow {
		n, _ := r.(map[string]any)
		in, _ := n["inputs"].(map[string]any)
		if p, ok := in["filename_prefix"].(string); ok {
			in["filename_prefix"] = strings.ReplaceAll(p, oldTaskID, newTaskID)
		}
	}
	b, e := json.Marshal(workflow)
	if e != nil {
		return raw
	}
	return string(b)
}
func snapshotCombatLoras(loras []CombatLora) []CombatLora { return cloneCombatLoras(loras) }
