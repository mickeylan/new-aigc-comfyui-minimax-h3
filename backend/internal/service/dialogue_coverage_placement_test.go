package service

import (
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

func TestPersistedDialogueCoverageHintsNeighbourScenesForMissingLines(t *testing.T) {
	rows := []models.Dialogue{
		{SceneID: 11, Character: "甲", Text: "一"},
		{SceneID: 12, Character: "乙", Text: "三"},
	}
	ledger := []SourceDialogue{{Speaker: "甲", Text: "一"}, {Speaker: "丙", Text: "二"}, {Speaker: "乙", Text: "三"}, {Speaker: "丁", Text: "四"}}
	coverage := persistedDialogueCoverage(rows, ledger, map[uint]int{11: 4, 12: 5})
	if coverage.Covered != 2 || len(coverage.Missing) != 2 {
		t.Fatalf("coverage=%+v", coverage)
	}
	if got := coverage.Missing[0]; got.Text != "二" || got.PrevSceneOrder != 4 || got.NextSceneOrder != 5 {
		t.Fatalf("middle missing line should sit between scenes 4 and 5: %+v", got)
	}
	if got := coverage.Missing[1]; got.Text != "四" || got.PrevSceneOrder != 5 || got.NextSceneOrder != 0 {
		t.Fatalf("trailing missing line should only point back to scene 5: %+v", got)
	}
	// Without scene orders the result is unchanged apart from the hints.
	if plain := persistedDialogueCoverage(rows, ledger, nil); plain.Covered != 2 || plain.Missing[0].PrevSceneOrder != 0 || plain.Missing[0].NextSceneOrder != 0 {
		t.Fatalf("nil scene orders must not invent hints: %+v", plain)
	}
}

func TestDialogueCoverageHintsUseScriptSceneNumbers(t *testing.T) {
	raw := `{"scenes":[{"title":"一","content":"甲开口。","dialogues":[{"character":"甲","text":"走。"}]},{"title":"二","content":"乙回头。","dialogues":[{"character":"乙","text":"等等。"}]}]}`
	coverage := dialogueCoverage(raw, []SourceDialogue{{Speaker: "甲", Text: "走。"}, {Speaker: "丙", Text: "别动。"}, {Speaker: "乙", Text: "等等。"}})
	if len(coverage.Missing) != 1 || coverage.Missing[0].PrevSceneOrder != 1 || coverage.Missing[0].NextSceneOrder != 2 {
		t.Fatalf("coverage=%+v", coverage)
	}
}

func TestSceneDialogueConsistencyFlagsRepeatedQuoteWithOneRow(t *testing.T) {
	rows := []models.Dialogue{{Character: "甲", Text: "快走！"}}
	issues := strings.Join(sceneDialogueConsistencyIssues("甲喊：“快走！”乙愣住。甲又喊：“快走！”", rows), "；")
	if !strings.Contains(issues, "“快走！”没有对应的对白行") {
		t.Fatalf("second identical quote should not reuse the only row: %q", issues)
	}
	// One row quoted in two pieces is still a single consistent line.
	split := []models.Dialogue{{Character: "甲", Text: "你先走，我断后。"}}
	if issues := sceneDialogueConsistencyIssues("甲道：“你先走，”转身拔剑，“我断后。”", split); len(issues) != 0 {
		t.Fatalf("split quote of one row flagged: %v", issues)
	}
}
