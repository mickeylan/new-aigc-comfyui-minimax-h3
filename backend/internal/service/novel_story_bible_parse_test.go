package service

import "testing"

func TestParseGeneratedStoryBibleNormalizesStructuredPremise(t *testing.T) {
	result, err := parseGeneratedStoryBible(`{"premise":{"core":"凡人踏入修仙路","stakes":["守护亲人","查明身世"]},"world_rules":{"cultivation":"有代价"},"main_plot":[{"event":"启程"}],"subplots":[],"timeline":[],"relationships":[],"clue_ledger":[],"locations":[],"props":[]}`)
	if err != nil {
		t.Fatalf("parse structured premise: %v", err)
	}
	if result.Premise != "core：凡人踏入修仙路；stakes：守护亲人；查明身世" {
		t.Fatalf("unexpected premise %q", result.Premise)
	}
	if result.WorldRules == nil || result.MainPlot == nil {
		t.Fatal("structured story bible fields were lost")
	}
}

func TestParseGeneratedStoryBibleKeepsStringPremise(t *testing.T) {
	result, err := parseGeneratedStoryBible("```json\n{\"premise\":\"凡人修仙\",\"world_rules\":{},\"main_plot\":[]}\n```")
	if err != nil {
		t.Fatalf("parse string premise: %v", err)
	}
	if result.Premise != "凡人修仙" {
		t.Fatalf("unexpected premise %q", result.Premise)
	}
}
