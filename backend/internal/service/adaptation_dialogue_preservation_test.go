package service

import (
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

const realPostposedDialogueFixture = `虬髯大汉没有说话，朝眼前齐人高的杂草丛指了指，草丛前的纤小足印依旧清晰可见。
“那还等什么？我等这便一鼓作气，将之擒下吧。”马脸男子见此，便要冲入草丛。
“此妖这般逃了一日一夜，怕是也快山穷水尽了，不过其擅使木属性幻术，在这片草丛中可谓如鱼得水，贸然追进去，怕是不妥。”齐姓道士眼睛微眯了一下，说道。
“难道就任其这般逃走？这妖孽小小年纪便如此狡诈，任其成年还不得遗祸苍生。”马脸男子略一迟疑，倒是停下了身形，不甘地说道。
“我等正道之士，除妖降魔自是义不容辞，这妖狐既被我等三人寻到，自不会再纵虎归山。”虬髯大汉正气凛然地说道。
“燕道友言下之意，看来已有所对策了？”齐姓道士眼珠一转，问道。
“这片草丛面积不小，若是我等三人这般盲目追入，虽最终也能将此妖手到擒来，但恐怕还要花费不少功夫。听说冯道友前不久购入了一张火云符，而齐道友的引风术可谓炉火纯青，两相配合下，这片草丛自将荡然无存，此妖也无所遁形了。”虬髯大汉缓缓地说道。
“火云符……也不是不行。不过抓住这只妖狐后，皮毛归我，其他我什么都不要，如何？”马脸男子沉吟了一下后，道。
“不行，这妖狐必须活捉。”虬髯大汉摇了摇头，干脆的拒绝道。
“燕道友这是何意？我这火云符可是初级中阶，花了我不少身家。”马脸男子面色一沉，声音蓦然一寒。
“冯道友稍安勿躁，若我没猜错话，燕道友应是血刀会外堂弟子，不久前会中可是下了任务，若能活捉此妖狐，不仅可成为内堂弟子，还能获一枚叱血丹和一千灵石奖励吧。”齐姓道士大有深意的望了虬髯大汉一眼，说道。
“没想到齐道友消息灵通，对于我们血刀会之事倒是知道不少。明人不说暗话，两位若能助我活捉这只妖狐，所有灵石奖励给两位平分，如何？”虬髯大汉面不改色的冲二人上说道。
“燕道友这般说了，贫道自然没有意见。”齐姓道士将手中拂尘往胳膊上随意的一搭。
“既然如此，那就事不宜迟，动手吧。”马脸男子说着，从怀中掏出一张符箓，有些肉疼的看了一眼后，往前方一抛，并念念有词起来。
那符箓表面顿时泛起一层红光，继而爆裂开来，化为一团赤红色烈焰，犹如一片火云一般，朝下方杂草丛压下。`

func TestExtractSourceDialoguesSupportsRealPostposedAttributions(t *testing.T) {
	ledger := extractSourceDialogues(models.Chapter{ID: 7, Order: 3, Content: realPostposedDialogueFixture}, "虬髯大汉", "马脸男子", "齐姓道士")
	if len(ledger) != 13 {
		t.Fatalf("ledger got %d, want 13: %#v", len(ledger), ledger)
	}
	want := []string{"马脸男子", "齐姓道士", "马脸男子", "虬髯大汉", "齐姓道士", "虬髯大汉", "马脸男子", "虬髯大汉", "马脸男子", "齐姓道士", "虬髯大汉", "齐姓道士", "马脸男子"}
	for i := range want {
		if ledger[i].Speaker != want[i] {
			t.Fatalf("speaker %d=%q want %q", i, ledger[i].Speaker, want[i])
		}
	}
}

func TestUnknownQuotedSpeakerRemainsInLedger(t *testing.T) {
	ledger := extractSourceDialogues(models.Chapter{Content: `风里传来一句：“别追了。”随后四野寂静。`})
	if len(ledger) != 1 || ledger[0].Speaker != "" || ledger[0].Text != "别追了。" {
		t.Fatalf("unknown quote was lost or guessed: %#v", ledger)
	}
}

func TestUnknownSpeakerCoverageAcceptsActualSpeakerAndExplicitSpeakerIsCorrected(t *testing.T) {
	ledger := []SourceDialogue{{Speaker: "", Text: "来迟了。"}, {Speaker: "甲", Text: "走吧。"}}
	raw := `{"script":"x","scenes":[{"content":"乙说道：来迟了。甲说道：走吧。","dialogues":[{"character":"乙","speech_type":"dialogue","text":"来迟了。"},{"character":"乙","speech_type":"dialogue","text":"走吧。"}]}]}`
	fixed, err := restoreDialoguesAlreadyLocatedByContent(raw, ledger)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parseScriptJSON(fixed)
	if err != nil {
		t.Fatal(err)
	}
	got := parsed.Scenes[0].Dialogues
	if len(got) != 2 || got[0].Character != "乙" || got[1].Character != "甲" {
		t.Fatalf("speaker reconciliation=%+v", got)
	}
	coverage := dialogueCoverage(fixed, ledger)
	if coverage.Covered != 2 || len(coverage.Missing) != 0 {
		t.Fatalf("coverage=%+v", coverage)
	}
}

func TestDialogueCoverageDoesNotAppendFakeScenesAndCountsOccurrences(t *testing.T) {
	ledger := []SourceDialogue{{Speaker: "甲", Text: "重复。", Offset: 1}, {Speaker: "甲", Text: "重复。", Offset: 9}, {Speaker: "乙", Text: "缺失。", Offset: 20}}
	raw := `{"script":"paid output","visual_bible":"x","scenes":[{"title":"原场景","content":"甲说道：重复。","image_prompt":"草地","duration":5,"visual_type":"action","mega_type":"normal","dialogues":[{"character":"甲","speech_type":"dialogue","text":"重复。"}]}]}`
	fixed, err := restoreDialoguesAlreadyLocatedByContent(raw, ledger)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parseScriptJSON(fixed)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Scenes) != 1 || parsed.Scenes[0].VisualType != "action" {
		t.Fatalf("scene structure was fabricated or normalized: %+v", parsed.Scenes)
	}
	coverage := dialogueCoverage(fixed, ledger)
	if coverage.Total != 3 || coverage.Covered != 1 || len(coverage.Missing) != 2 || coverage.Missing[0].Text != "重复。" || coverage.Missing[1].Text != "缺失。" {
		t.Fatalf("occurrence coverage=%+v", coverage)
	}
	if strings.Count(fixed, `"text":"重复。"`) != 1 {
		t.Fatalf("one content occurrence restored more than once: %s", fixed)
	}
	if strings.Contains(fixed, "待安排原文对白") {
		t.Fatal("fake review scene appended")
	}
}
