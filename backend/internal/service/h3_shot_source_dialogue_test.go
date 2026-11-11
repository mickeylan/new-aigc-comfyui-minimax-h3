package service

import (
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

const h3ShotSourceDialogue = `[Shot 1]燕承眼神微沉，抬高声音说道：“不知阁下如何称呼？在下血刀会燕承，正与两位道友联手捉拿这妖狐，阁下若是与此事无关，还请自行离去。”
柳石缓缓抬起木然的脸，淡淡看了燕承一眼，仍旧一言不发。柳乐儿紧贴着柳石站定，右手攥住旧拨浪鼓，左手抓着柳石的裤脚；她张开小嘴却没有出声，掌心渗出细汗。
[Shot 2]燕承、马脸男子与齐姓道士短暂交换眼色。齐姓道士仍将白马尾拂尘搭在臂上，目光落在柳石身上；马脸男子微侧身体，左手藏在袖中，嘴唇几乎不动地向同伴传音：“此人这般可疑，看来真有问题，与其在此浪费时间，不如我们……“，同时做出一个抹脖子的手势。`

func TestH3ShotSourceQuoteSpans(t *testing.T) {
	items, spans := extractQuotedDialogueSpans(h3ShotSourceDialogue)
	if len(items) != 2 || len(spans) != 2 {
		t.Fatalf("source quote omitted: %+v", items)
	}
	for i, want := range []string{h3TailClusterFirstQuote, h3TailClusterSecondQuote} {
		if items[i].Text != want || !strings.Contains(h3ShotSourceDialogue[spans[i].start:spans[i].end], want) {
			t.Fatalf("source bytes changed: %+v", items[i])
		}
	}
	// A new opening quote after an unclosed paragraph must not swallow the next line.
	got := extractQuotedDialogue("甲说：“未闭合\n乙说：“完整。”")
	if len(got) != 1 || got[0].Text != "完整。" {
		t.Fatalf("unclosed quote consumed another quote: %+v", got)
	}
}

func TestH3ShotSourceAttributionAndPrompt(t *testing.T) {
	sc := &models.Scene{Content: h3ShotSourceDialogue, Characters: "柳乐儿,燕承,齐姓道士,马脸男子,柳石", StoryContentEdited: true, Duration: 15}
	for _, content := range []string{h3ShotSourceDialogue, strings.ReplaceAll(h3ShotSourceDialogue, "……“，", "……”，")} {
		sc.Content = content
		dubs := editedSceneDialogues(sc, nil)
		if len(dubs) != 2 {
			t.Fatalf("expected both source dialogues: %+v", dubs)
		}
		if dubs[0].Character != "燕承" || dubs[0].SpeechType != "dialogue" || dubs[0].Text != h3TailClusterFirstQuote {
			t.Fatalf("Shot 1 speaker incorrectly attributed: %+v", dubs[0])
		}
		if dubs[1].Character != "马脸男子" || dubs[1].SpeechType != "offscreen_dialogue" || dubs[1].Text != h3TailClusterSecondQuote {
			t.Fatalf("Shot 2 telepathic speaker lost: %+v", dubs[1])
		}
		refs := []string{"角色「柳乐儿」", "角色「燕承」", "角色「齐姓道士」", "角色「马脸男子」", "角色「柳石」"}
		visual := "[Shot 1] <Subject 2> raises his voice. <Subject 5> looks up.\n[Shot 2] <Subject 4> conceals his hand and signals."
		plan := h3DialoguePlan{Placements: []h3DialoguePlanPlacement{{Index: 0, Shot: 1, After: "<Subject 2> raises his voice."}, {Index: 1, Shot: 2, After: "<Subject 4> conceals his hand and signals."}}}
		prompt := applyH3DialoguePlan(visual, dubs, refs, plan)
		cut := strings.Index(prompt, "[Shot 2]")
		if cut < 0 || !strings.Contains(prompt[:cut], h3TailClusterFirstQuote) || !strings.Contains(prompt[cut:], h3TailClusterSecondQuote) || strings.Contains(prompt, "待确认") {
			t.Fatalf("dialogue omitted or in wrong shot: %s", prompt)
		}
		if !strings.Contains(prompt, "<Subject 2> (S1) says:") || !strings.Contains(prompt, "<Subject 4> (S2)") {
			t.Fatalf("Subject/speaker binding lost: %s", prompt)
		}
	}
}
