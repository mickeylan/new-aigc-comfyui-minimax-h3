package service

import (
	"encoding/json"
	"os"
	"testing"

	"comfyui-console/internal/models"
)

func TestCombatH3TemplateUsesDedicatedLoraChain(t *testing.T) {
	data, err := os.ReadFile("templates/minimax_h3_ref2v_combat.json")
	if err != nil {
		t.Fatal(err)
	}
	var tpl struct {
		Code     string `json:"code"`
		Workflow map[string]struct {
			Inputs    map[string]any `json:"inputs"`
			ClassType string         `json:"class_type"`
		} `json:"workflow"`
	}
	if err := json.Unmarshal(data, &tpl); err != nil {
		t.Fatal(err)
	}
	if tpl.Code != h3CombatRef2VTemplate {
		t.Fatalf("code=%q", tpl.Code)
	}
	if got := tpl.Workflow["127"].Inputs["unet_name"]; got != `minimax-h3\DasiwaMinimaxH3_dasiwaHybridV4_int8.safetensors` {
		t.Fatalf("combat UNET=%q", got)
	}
	loader := tpl.Workflow["156"]
	if loader.ClassType != combatPowerClass {
		t.Fatalf("loader=%s", loader.ClassType)
	}
	parent, ok := loader.Inputs["model"].([]any)
	if !ok || len(parent) != 2 || parent[0] != "127" {
		t.Fatal("Power loader lost model input")
	}
	checks := []struct {
		key, name string
		weight    float64
	}{
		{"lora_1", `minimax-h3\minimax_h3_fl2v_turbo_8step_v1.0_comfyui_bf16.safetensors`, .7},
		{"lora_2", `minimax-h3\H3_Combat_V2.safetensors`, .7},
		{"lora_3", `minimax-h3\Motion_Repair_V2.safetensors`, .5},
		{"lora_4", `minimax-h3\H3_speed_slider_1.1.safetensors`, 3},
	}
	for _, check := range checks {
		entry, ok := loader.Inputs[check.key].(map[string]any)
		if !ok || entry["lora"] != check.name || entry["strength"] != check.weight || entry["on"] != true {
			t.Fatalf("Power default %s=%v", check.key, entry)
		}
	}
	for _, id := range []string{"157", "158", "159"} {
		if _, exists := tpl.Workflow[id]; exists {
			t.Fatalf("legacy loader %s retained", id)
		}
	}
	connected := false
	for nodeID, node := range tpl.Workflow {
		if nodeID == "156" {
			continue
		}
		parent, ok := node.Inputs["model"].([]any)
		if ok && len(parent) == 2 && parent[0] == "156" {
			connected = true
			break
		}
	}
	if !connected {
		t.Fatal("combat LoRA chain is not connected downstream")
	}
}

func TestAuditedCombatShotsRequireCombatVideoTemplate(t *testing.T) {
	shots := []models.Shot{{DirectorSource: "fight-video-action-design"}}
	if !hasAuditedCombatShots(shots) {
		t.Fatal("audited combat shot was not detected")
	}
	if !isH3Ref2VTemplate(h3CombatRef2VTemplate) {
		t.Fatal("combat template was not treated as Ref2VA")
	}
	if hasAuditedCombatShots([]models.Shot{{DirectorSource: "manual"}}) {
		t.Fatal("ordinary shot was routed to combat template")
	}
}
