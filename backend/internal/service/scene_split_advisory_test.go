package service

import (
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

func TestSoundEffectRowsMoveToContentButSpeechStays(t *testing.T) {
	scenes := []scriptScene{{
		Content: "青年肩头绽开黑光。",
		Dialogues: []scriptDialogue{
			{Character: "", Text: "轰！"},
			{Character: "破空声（画外）", SpeechType: "offscreen_dialogue", Text: "嗖——"},
			{Character: "燕承", SpeechType: "dialogue", Text: "砰！"},
			{Character: "", Text: "追！"},
			{Character: "", Text: "嗤嗤"},
		},
	}}
	moveSoundEffectsToContent(scenes, map[string]bool{speechMatchKey("嗤嗤"): true})
	var kept []string
	for _, d := range scenes[0].Dialogues {
		kept = append(kept, d.Character+":"+d.Text)
	}
	if got := strings.Join(kept, "|"); got != "燕承:砰！|:追！|:嗤嗤" {
		t.Fatalf("kept rows=%q; named speech, unknown speech and source-ledger lines must stay", got)
	}
	for _, sound := range []string{"【音效】轰！", "【音效】嗖——"} {
		if !strings.Contains(scenes[0].Content, sound) {
			t.Fatalf("removed SFX %q missing from content: %q", sound, scenes[0].Content)
		}
	}
	if !strings.HasPrefix(scenes[0].Content, "青年肩头绽开黑光。") {
		t.Fatalf("authored content changed: %q", scenes[0].Content)
	}
}

func TestSceneDialogueConsistencyIssuesAreAdvisory(t *testing.T) {
	rows := []models.Dialogue{{Character: "马脸男子", Text: "那还等什么？"}, {Character: "齐姓道士", Text: "贸然追进去，怕是不妥。"}}
	if issues := sceneDialogueConsistencyIssues("马脸男子喝道：“那还等什么？”齐姓道士摇头：“贸然追进去，怕是不妥。”只听“砰”的一声。", rows); len(issues) != 0 {
		t.Fatalf("consistent scene flagged: %v", issues)
	}
	issues := strings.Join(sceneDialogueConsistencyIssues("齐姓道士道：“贸然追进去，怕是不妥。”马脸男子道：“那还等什么？”燕承道：“动手吧。”\n【对白｜燕承】那还等什么？", rows), "；")
	for _, want := range []string{"顺序不一致", "“动手吧。”没有对应", "说话人“燕承”与对白行“马脸男子”"} {
		if !strings.Contains(issues, want) {
			t.Fatalf("issues %q missing %q", issues, want)
		}
	}
}

func TestSceneAdvisoryWarningsUseSceneOrder(t *testing.T) {
	scenes := []models.Scene{{ID: 7, Order: 3, Duration: 8, Content: "风吹过草丛。"}}
	warnings := sceneAdvisoryWarnings(scenes, nil)
	if len(warnings) != 1 || !strings.HasPrefix(warnings[0], "第3场过疏") {
		t.Fatalf("warnings=%v", warnings)
	}
}
