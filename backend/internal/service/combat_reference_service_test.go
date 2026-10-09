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
