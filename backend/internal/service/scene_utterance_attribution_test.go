package service

import (
	"encoding/json"
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

const fireCloudStory = `火云沿着齐人高的草丛压下，触到草木的刹那，烈焰骤然窜起，连成一片汹涌的火势。齐姓道士目光锁住前方，口中念过一声：“无量天尊”，一伸手，灵光闪现，白马尾拂尘出现在手中，他挥动白马尾拂尘。数股白蒙蒙狂风凭空涌出，迎着火势呈扇形铺开，卷起燃烧的草木，将火势进一步推向草丛深处。狂风助长烈焰，火势又借着风力迅速蔓延，黑烟翻卷而起，燃烧后的草灰被卷得横飞出去。`

func TestFireCloudAudibleInvocationIsDialogueForLegacyAndEditedScenes(t *testing.T) {
	for _, edited := range []bool{false, true} {
		sc := &models.Scene{Characters: "齐姓道士,燕承,马脸男子", VisibleCharacters: "齐姓道士,燕承,马脸男子", Content: fireCloudStory, StoryContentEdited: edited}
		var got []models.Dialogue
		if edited {
			analysis := SceneDialogueAnalysis{Source: "ai_reviewed", ContentHash: sceneDialogueContentHash(sc.Content), Items: []SceneDialogueAnalysisItem{{Index: 0, Text: "无量天尊", Speaker: "齐姓道士", SpeechType: "dialogue"}}}
			encoded, err := json.Marshal(analysis)
			if err != nil {
				t.Fatal(err)
			}
			sc.DialogueAnalysisJSON = string(encoded)
			got = editedSceneDialogues(sc, []models.Dialogue{{Character: "齐姓道士", SpeechType: "dialogue", Text: "无量天尊", Source: "ai_reviewed", Addressee: "众人", Delivery: "低声"}})
		} else {
			got = explicitSceneSpeech(sc)
		}
		if len(got) != 1 || got[0].Character != "齐姓道士" || got[0].SpeechType != "dialogue" || got[0].Text != "无量天尊" {
			t.Fatalf("edited=%v invocation=%+v", edited, got)
		}
		if edited && (got[0].Source != "ai_reviewed" || got[0].Addressee != "众人" || got[0].Delivery != "低声") {
			t.Fatalf("review metadata was not retained: %+v", got[0])
		}
		prompt := buildMiniMaxH3RefPrompt(sc, nil, got, []string{"- <Picture 1>：角色「齐姓道士」四视图"})
		if !strings.Contains(prompt, "<d>[Chinese] 无量天尊</d>") || strings.Contains(prompt, "No dialogue") {
			t.Fatalf("edited=%v prompt lost invocation: %s", edited, prompt)
		}
	}
}

func TestExplicitUtteranceMarkersRemainConservative(t *testing.T) {
	spoken := `齐姓道士将拂尘从左手换到右手，又缓缓抬眼看向烧灼的草木，并稳住呼吸，口中念诵：“天地玄宗。”`
	got := explicitSceneSpeech(&models.Scene{Characters: "齐姓道士,燕承,马脸男子", Content: spoken})
	if len(got) != 1 || got[0].Character != "齐姓道士" || got[0].Text != "天地玄宗。" {
		t.Fatalf("long explicit spoken clause=%+v", got)
	}
	for _, content := range []string{
		`齐姓道士心中默念：“不可妄动。”`,
		`齐姓道士生出一个念头：“先退再说。”`,
		`陌生道士口中念过一声：“无量天尊”。齐姓道士挥动拂尘。`,
	} {
		if got := explicitlyQuotedCharacterSpeech(content, []string{"齐姓道士", "燕承", "马脸男子"}); len(got) != 0 {
			t.Fatalf("non-audible or unknown speaker guessed for %q: %+v", content, got)
		}
	}
	thought := explicitSceneSpeech(&models.Scene{Characters: "齐姓道士", Content: `齐姓道士心中默念：“不可妄动。”`})
	if len(thought) != 1 || thought[0].SpeechType != "monologue" || thought[0].Text != "不可妄动。" {
		t.Fatalf("默念 must remain monologue: %+v", thought)
	}
}
