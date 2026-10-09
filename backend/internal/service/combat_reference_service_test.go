package service

import (
	"strings"
	"testing"
)

func TestCombatReferenceSearchRanksStrongDesignMatch(t *testing.T) {
	service, err := NewCombatReferenceService()
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Search("design", "雨夜屋顶，角色从参考图姿态立即开打，要求电影级稳定连续和动作因果")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.SourceCommit) != 40 || len(result.SourceContentSHA256) != 64 {
		t.Fatalf("source manifest missing: %+v", result)
	}
	if result.Primary == nil || result.Primary.ID != "02" {
		t.Fatalf("primary=%+v", result.Primary)
	}
	if result.WeakFallback {
		t.Fatal("strong match was marked weak fallback")
	}
	if len(result.Primary.MatchedKeywords) < 2 {
		t.Fatalf("matched=%v", result.Primary.MatchedKeywords)
	}
}

func TestCombatReferenceSearchDoesNotAutoSelectSkill(t *testing.T) {
	service, err := NewCombatReferenceService()
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Search("skills", "群体")
	if err != nil {
		t.Fatal(err)
	}
	if result.Primary != nil || len(result.Eligible) == 0 {
		t.Fatalf("skills must be candidates without primary: %+v", result)
	}
	if result.Eligible[0].SelectionNotice == nil {
		t.Fatal("skill condition notice missing")
	}
}

func TestCombatReferenceReadReturnsExactEmbeddedBody(t *testing.T) {
	service, err := NewCombatReferenceService()
	if err != nil {
		t.Fatal(err)
	}
	document, err := service.Read("design", "02")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(document.Content) == "" || document.Metadata["file"] == nil {
		t.Fatalf("document incomplete: %+v", document)
	}
	files, err := CombatReferenceDataFiles()
	if err != nil || len(files) < 260 {
		t.Fatalf("embedded files=%d err=%v", len(files), err)
	}
}

func TestCombatReferenceCompileSelectionAuditsAndRejectsUnsafeChoices(t *testing.T) {
	service, err := NewCombatReferenceService()
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := service.CompileSelection([]CombatReferenceSelection{{Scope: "design", ID: "02"}, {Scope: "moves", ID: "27"}}, 40000)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Audit) != 2 || len(bundle.Audit[0].ContentSHA256) != 64 || !strings.Contains(bundle.Text, "sha256:") {
		t.Fatalf("bundle=%+v", bundle)
	}
	lowerBundle := strings.ToLower(bundle.Text)
	for _, forbidden := range []string{"time_range", "system prompt", "输出格式", "```"} {
		if strings.Contains(lowerBundle, forbidden) {
			t.Fatalf("compiled prompt leaked source instruction %q: %s", forbidden, bundle.Text)
		}
	}
	if !strings.Contains(bundle.Text, "动作") && !strings.Contains(bundle.Text, "镜头") {
		t.Fatalf("compiled prompt lost combat mechanisms: %s", bundle.Text)
	}
	if _, err := service.CompileSelection([]CombatReferenceSelection{{Scope: "skills", ID: "01"}}, 40000); err == nil || !strings.Contains(err.Error(), "前置条件") {
		t.Fatalf("unconfirmed skill error=%v", err)
	}
	if _, err := service.CompileSelection([]CombatReferenceSelection{{Scope: "design", ID: "01"}, {Scope: "design", ID: "02"}}, 40000); err == nil || !strings.Contains(err.Error(), "一个主动作") {
		t.Fatalf("multiple designs error=%v", err)
	}
	if _, err := service.CompileSelection([]CombatReferenceSelection{{Scope: "moves", ID: "27"}, {Scope: "moves", ID: "27"}}, 40000); err == nil || !strings.Contains(err.Error(), "重复") {
		t.Fatalf("duplicate error=%v", err)
	}
	if err := ValidateCombatReferenceAudit(bundle.Audit, bundle.Audit); err != nil {
		t.Fatal(err)
	}
	changed := append([]CombatReferenceAudit(nil), bundle.Audit...)
	changed[0].ContentSHA256 = strings.Repeat("0", 64)
	if err := ValidateCombatReferenceAudit(bundle.Audit, changed); err == nil || !strings.Contains(err.Error(), "重新生成") {
		t.Fatalf("changed audit accepted: %v", err)
	}
}

func TestCombatReferenceShowcaseIsSafeAndScoped(t *testing.T) {
	service, err := NewCombatReferenceService()
	if err != nil {
		t.Fatal(err)
	}
	showcase, err := service.Showcase("moves", "27")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(showcase, "27-居合拔刀与重兵器抢线专项.gif") || strings.Contains(showcase, "..") {
		t.Fatalf("showcase=%q", showcase)
	}
	if _, err := service.Showcase("design", "02"); err == nil {
		t.Fatal("missing showcase accepted")
	}
}

func TestCombatReferenceRejectsUnknownScopeAndMissingBody(t *testing.T) {
	service, err := NewCombatReferenceService()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Search("unknown", "打斗"); err == nil {
		t.Fatal("unknown scope accepted")
	}
	if _, err := service.Read("moves", "999"); err == nil {
		t.Fatal("missing reference accepted")
	}
}
