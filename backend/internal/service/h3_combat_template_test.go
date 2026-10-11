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
			Inputs map[string]any `json:"inputs"`
		} `json:"workflow"`
	}
	if err := json.Unmarshal(data, &tpl); err != nil {
		t.Fatal(err)
	}
	if tpl.Code != h3CombatRef2VTemplate {
		t.Fatalf("code=%q", tpl.Code)
	}
	checks := []struct {
		node, name string
		strength   float64
		parent     string
	}{
		{"157", `minimax-h3\H3_Combat_V2.safetensors`, 0.7, "156"},
		{"158", `minimax-h3\Motion_Repair_V2.safetensors`, 0.5, "157"},
		{"159", `minimax-h3\H3_speed_slider_1.1.safetensors`, 3, "158"},
	}
	for _, check := range checks {
		inputs := tpl.Workflow[check.node].Inputs
		if inputs["lora_name"] != check.name || inputs["strength_model"] != check.strength {
			t.Fatalf("node %s inputs=%+v", check.node, inputs)
		}
		parent, ok := inputs["model"].([]any)
		if !ok || len(parent) != 2 || parent[0] != check.parent {
			t.Fatalf("node %s parent=%+v", check.node, inputs["model"])
		}
	}
	connected := false
	for nodeID, node := range tpl.Workflow {
		if nodeID == "159" {
			continue
		}
		parent, ok := node.Inputs["model"].([]any)
		if ok && len(parent) == 2 && parent[0] == "159" {
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
