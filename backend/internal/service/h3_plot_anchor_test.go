package service

import (
	"reflect"
	"strings"
	"testing"
)

func TestH3PlotNodesDropQuotedSpeechAndShotMarkers(t *testing.T) {
	nodes := h3PlotNodes("[Shot 1] 韩立拔剑。燕承冷声道：“站住！”\n“走。”\n剑光劈下；")
	want := []string{"韩立拔剑。", "燕承冷声道：", "剑光劈下；"}
	if !reflect.DeepEqual(nodes, want) {
		t.Fatalf("nodes=%q", nodes)
	}
}

func TestCheckH3PlotAnchorsAcceptsCompleteOrderedAnchorsAndStripsThem(t *testing.T) {
	text := "[Shot 1] <Subject 1> draws the sword.〔P01〕 [Shot 2] At 00:04.000, he slashes down.〔P02〕 The blade stops [P03]."
	stripped, issues := checkH3PlotAnchors(text, 3)
	if len(issues) != 0 {
		t.Fatalf("issues=%q", issues)
	}
	want := "[Shot 1] <Subject 1> draws the sword. [Shot 2] At 00:04.000, he slashes down. The blade stops."
	if stripped != want {
		t.Fatalf("stripped=%q", stripped)
	}
	if strings.Contains(stripped, "P0") || strings.ContainsAny(stripped, "〔〕") {
		t.Fatalf("anchor residue: %q", stripped)
	}
}

func TestCheckH3PlotAnchorsReportsMissingDuplicateDisorderAndLastShot(t *testing.T) {
	cases := []struct {
		name string
		text string
		want []string
	}{
		{"missing", "[Shot 1] A.〔P01〕 C.〔P03〕", []string{"缺少剧情锚点P02：对应剧情节点未完成或未标注"}},
		{"duplicate", "[Shot 1] A.〔P01〕 B.〔P02〕 again〔P02〕 C.〔P03〕", []string{"剧情锚点P02重复：每个节点只能完成一次"}},
		{"disorder", "[Shot 1] B.〔P02〕 A.〔P01〕 C.〔P03〕", []string{"剧情锚点顺序错乱：剧情节点必须按原文顺序完成"}},
		{"outside", "[Shot 1] A.〔P01〕 B.〔P02〕 C.〔P03〕 D.〔P09〕", []string{"出现账本外的剧情锚点P09"}},
		{"last shot", "[Shot 1] A.〔P01〕 B.〔P02〕 C.〔P03〕 [Shot 2] At 00:05.000, aftermath.", []string{"最后一个剧情节点没有落在最后一个Shot"}},
		{"none", "[Shot 1] A. B. C.", []string{"缺少剧情锚点P01、P02、P03：对应剧情节点未完成或未标注"}},
	}
	for _, tc := range cases {
		stripped, issues := checkH3PlotAnchors(tc.text, 3)
		if !reflect.DeepEqual(issues, tc.want) {
			t.Errorf("%s: issues=%q want=%q", tc.name, issues, tc.want)
		}
		if h3PlotAnchorPattern.MatchString(stripped) {
			t.Errorf("%s: anchor residue %q", tc.name, stripped)
		}
	}
}

func TestCheckH3PlotAnchorsWithoutLedgerOnlyStrips(t *testing.T) {
	stripped, issues := checkH3PlotAnchors("[Shot 1] A.〔P01〕", 0)
	if issues != nil || stripped != "[Shot 1] A." {
		t.Fatalf("stripped=%q issues=%q", stripped, issues)
	}
}

func TestFormatH3PlotLedgerListsNodesAndVoices(t *testing.T) {
	if got := formatH3PlotLedger(nil, []string{"D1 x"}); got != "" {
		t.Fatalf("empty ledger rendered %q", got)
	}
	got := formatH3PlotLedger([]string{"韩立拔剑。", "剑光劈下。"}, []string{"D1 [speech_type=dialogue] 燕承：站住！"})
	for _, want := range []string{"〔剧情节点账本〕", "\nP01 韩立拔剑。", "\nP02 剑光劈下。", "〔人声账本〕", "\nD1 [speech_type=dialogue] 燕承：站住！"} {
		if !strings.Contains(got, want) {
			t.Fatalf("ledger missing %q: %s", want, got)
		}
	}
}
