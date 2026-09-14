package service

import "testing"

func TestParsePlanJSONAcceptsArrayForScalarString(t *testing.T) {
	raw := `{
		"title":["逆光归来"],
		"logline":["她重返故城", "揭开旧案"],
		"core":"真相与亲情",
		"characters":[],
		"episodes":[{"n":1,"title":["归来"],"brief":["故人重逢"],"hook":"悬念钩"}]
	}`

	plan, err := parsePlanJSON(raw)
	if err != nil {
		t.Fatalf("parsePlanJSON returned error: %v", err)
	}
	if plan.Title != "逆光归来" {
		t.Fatalf("title = %q, want %q", plan.Title, "逆光归来")
	}
	if plan.Logline != "她重返故城；揭开旧案" {
		t.Fatalf("logline = %q", plan.Logline)
	}
	if plan.Episodes[0].Title != "归来" || plan.Episodes[0].Brief != "故人重逢" {
		t.Fatalf("episode scalar fields were not normalized: %+v", plan.Episodes[0])
	}
}

func TestParsePlanJSONStillRejectsMissingRequiredFields(t *testing.T) {
	if _, err := parsePlanJSON(`{"title":["只有标题"],"episodes":[]}`); err == nil {
		t.Fatal("expected missing logline/episodes error")
	}
}
