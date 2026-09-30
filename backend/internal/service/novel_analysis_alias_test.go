package service

import (
	"strings"
	"testing"
)

func TestParseChapterAnalysisRejectsStringAliasCandidateForFocusedRetry(t *testing.T) {
	_, err := parseChapterAnalysis(`{"summary":"ok","alias_candidates":["石头哥哥"]}`)
	if err == nil {
		t.Fatal("expected string alias candidate to be rejected")
	}
	if !isAliasCandidateSchemaError(err) {
		t.Fatalf("expected alias candidate schema error, got %v", err)
	}
}

func TestParseChapterAnalysisAcceptsObjectAliasCandidatesAndEmptyArray(t *testing.T) {
	for _, raw := range []string{
		`{"summary":"ok","alias_candidates":[{"canonical_name":"石头","alias":"石头哥哥"}]}`,
		`{"summary":"ok","alias_candidates":[]}`,
	} {
		result, err := parseChapterAnalysis(raw)
		if err != nil {
			t.Fatalf("parse valid analysis: %v", err)
		}
		if strings.TrimSpace(result.Summary) != "ok" {
			t.Fatalf("unexpected summary %q", result.Summary)
		}
	}
}
