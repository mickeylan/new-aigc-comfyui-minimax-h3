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

func TestParseChapterAnalysisNormalizesStructuredSummary(t *testing.T) {
	result, err := parseChapterAnalysis(`{"summary":{"plot":"狐女现身","stakes":["身份暴露","石头遇险"]},"alias_candidates":[]}`)
	if err != nil {
		t.Fatalf("parse structured summary: %v", err)
	}
	if result.Summary != "plot：狐女现身；stakes：身份暴露；石头遇险" {
		t.Fatalf("unexpected normalized summary %q", result.Summary)
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
