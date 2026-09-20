package service

import (
	"encoding/json"
	"testing"

	"comfyui-console/internal/models"
)

func TestNormalizeAdaptationJSONAcceptsStructuredArrays(t *testing.T) {
	raw := `{"summary":"测试","episodes":[{"episode_n":1,"source_chapter_ids":[1,2],"must_keep_events":["相遇"],"optional_events":[],"omitted_events":["支线"]}]}`
	normalized, err := normalizeAdaptationJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Episodes []models.EpisodeAdaptation `json:"episodes"`
	}
	if err := json.Unmarshal([]byte(normalized), &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed.Episodes) != 1 || parsed.Episodes[0].SourceChapterIDs != `[1,2]` {
		t.Fatalf("unexpected source ids: %+v", parsed.Episodes)
	}
	if parsed.Episodes[0].MustKeepEvents != `["相遇"]` || parsed.Episodes[0].OptionalEvents != `[]` || parsed.Episodes[0].OmittedEvents != `["支线"]` {
		t.Fatalf("event arrays not normalized: %+v", parsed.Episodes[0])
	}
}

func TestNormalizeAdaptationJSONPreservesTextFields(t *testing.T) {
	raw := `{"episodes":[{"source_chapter_ids":"[3]","must_keep_events":"[]"}]}`
	normalized, err := normalizeAdaptationJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Episodes []models.EpisodeAdaptation `json:"episodes"`
	}
	if err := json.Unmarshal([]byte(normalized), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Episodes[0].SourceChapterIDs != `[3]` || parsed.Episodes[0].MustKeepEvents != `[]` {
		t.Fatalf("text fields changed: %+v", parsed.Episodes[0])
	}
}
