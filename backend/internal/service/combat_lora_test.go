package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func powerGraphForTest() map[string]any {
	var g map[string]any
	_ = json.Unmarshal([]byte(`{"127":{"class_type":"UNETLoader","inputs":{}},"156":{"class_type":"Power Lora Loader (rgthree)","inputs":{"model":["127",0],"lora_1":{"on":true,"lora":"a.safetensors","strength":0.7}}},"200":{"class_type":"Sampler","inputs":{"model":["156",0]}}}`), &g)
	return g
}
func TestDeriveCombatPowerLoaderZeroOneSixOrderedAndNativeOff(t *testing.T) {
	for _, loras := range [][]CombatLora{
		{},
		{{true, "one.safetensors", .5}},
		{{true, "a", 1}, {false, "b", 2}, {true, "c", 3}, {false, "d", 4}, {true, "e", 5}, {true, "f", 6}},
	} {
		g := powerGraphForTest()
		if err := deriveCombatWorkflow(g, loras); err != nil {
			t.Fatal(err)
		}
		inputs := g["156"].(map[string]any)["inputs"].(map[string]any)
		for i, want := range loras {
			got := inputs[fmt.Sprintf("lora_%09d", i+1)].(map[string]any)
			if got["lora"] != want.Lora || got["on"] != want.On || got["strength"] != want.Strength {
				t.Fatalf("entry %d=%v want=%+v", i, got, want)
			}
		}
		if _, ok := g["200"]; !ok {
			t.Fatal("off entries must not remove graph objects")
		}
	}
}
func TestPowerLoraJSONKeepsTwelveEntriesInLoadingOrder(t *testing.T) {
	loras := []CombatLora{}
	for i := 1; i <= 12; i++ {
		loras = append(loras, CombatLora{On: i%2 == 0, Lora: fmt.Sprintf("model%d.safetensors", i), Strength: float64(i) / 10})
	}
	g := powerGraphForTest()
	if err := deriveCombatWorkflow(g, loras); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(g)
	if err != nil {
		t.Fatal(err)
	}
	last := -1
	for i := 1; i <= 12; i++ {
		at := strings.Index(string(raw), fmt.Sprintf(`"lora_%09d"`, i))
		if at <= last {
			t.Fatal("serialized kwargs loading order changed")
		}
		last = at
	}
	got, err := powerLorasFromWorkflow(g)
	if err != nil || len(got) != 12 {
		t.Fatalf("entries=%v err=%v", got, err)
	}
	for i := range got {
		if got[i] != loras[i] {
			t.Fatalf("entry %d changed", i)
		}
	}
	// Native exported workflows may have gaps after removing a widget.
	g["156"].(map[string]any)["inputs"] = map[string]any{"model": []any{"127", 0}, "lora_3": map[string]any{"on": true, "lora": "third", "strength": float64(3)}, "lora_1": map[string]any{"on": false, "lora": "first", "strength": float64(1)}}
	got, err = powerLorasFromWorkflow(g)
	if err != nil || len(got) != 2 || got[1].Lora != "third" {
		t.Fatalf("sparse entries lost: %v %v", got, err)
	}
}

func TestCombatLoraValidationPathsAndWeights(t *testing.T) {
	good := CombatLoraConfig{Mode: "custom", Loras: []CombatLora{{false, `folder\x.safetensors`, -100}}}
	if err := validateCombatConfig(good, false); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../x", "a/../x", "/x", `C:\x`, ""} {
		c := CombatLoraConfig{Mode: "custom", Loras: []CombatLora{{true, name, 1}}}
		if validateCombatConfig(c, false) == nil {
			t.Fatalf("accepted %q", name)
		}
	}
}
func TestLegacyRuntimeConversionPreservesFilesWeights(t *testing.T) {
	var g map[string]any
	_ = json.Unmarshal([]byte(`{"127":{"class_type":"UNETLoader","inputs":{}},"156":{"class_type":"LoraLoaderModelOnly","inputs":{"model":["127",0],"lora_name":"a","strength_model":0.1}},"157":{"class_type":"LoraLoaderModelOnly","inputs":{"model":["156",0],"lora_name":"b","strength_model":0.2}},"158":{"class_type":"LoraLoaderModelOnly","inputs":{"model":["157",0],"lora_name":"c","strength_model":0.3}},"159":{"class_type":"LoraLoaderModelOnly","inputs":{"model":["158",0],"lora_name":"d","strength_model":0.4}},"200":{"inputs":{"model":["159",0]}}}`), &g)
	loras, err := powerLorasFromWorkflow(g)
	if err != nil || loras[3].Lora != "d" || loras[3].Strength != .4 {
		t.Fatalf("loras=%v err=%v", loras, err)
	}
	convertLegacyCombatWorkflow(g)
	if err := deriveCombatWorkflow(g, loras); err != nil {
		t.Fatal(err)
	}
	if g["157"] != nil || g["200"].(map[string]any)["inputs"].(map[string]any)["model"].([]any)[0] != "156" {
		t.Fatal("legacy chain not converted")
	}
}
func TestCloneCombatWorkflowSnapshotOnlyChangesFilenamePrefix(t *testing.T) {
	raw := `{"9":{"inputs":{"filename_prefix":"old/output","promptInputsJSON":"{\"task_id\":\"old\"}"}}}`
	cloned := cloneCombatWorkflowSnapshot(raw, "old", "new")
	var g map[string]any
	_ = json.Unmarshal([]byte(cloned), &g)
	in := g["9"].(map[string]any)["inputs"].(map[string]any)
	if in["filename_prefix"] != "new/output" || in["promptInputsJSON"] != `{"task_id":"old"}` {
		t.Fatal(in)
	}
}
