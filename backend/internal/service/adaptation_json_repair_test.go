package service

import (
	"context"
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

type adaptationRepairProvider struct {
	response string
	calls    int
	system   string
	user     string
}

func (p *adaptationRepairProvider) Name() string                      { return "repair-test" }
func (p *adaptationRepairProvider) HealthCheck(context.Context) error { return nil }
func (p *adaptationRepairProvider) Chat(system, user string) (string, error) {
	p.calls++
	p.system, p.user = system, user
	return p.response, nil
}

func TestNormalizeAdaptationJSONRepairsSyntaxOnce(t *testing.T) {
	malformed := `{"summary":"测试","episodes":[{"episode_n":1,"source_chapter_ids":[1] extra}]}`
	provider := &adaptationRepairProvider{response: `{"summary":"测试","episodes":[{"episode_n":1,"source_chapter_ids":[1]}]}`}
	normalized, repaired, err := normalizeAdaptationJSONWithRepair(provider, malformed)
	if err != nil {
		t.Fatal(err)
	}
	if provider.calls != 1 {
		t.Fatalf("repair calls=%d", provider.calls)
	}
	if repaired != provider.response || !strings.Contains(normalized, `"episodes"`) {
		t.Fatalf("unexpected repaired JSON: %s", normalized)
	}
	if !strings.Contains(provider.system, "不得改写") || !strings.Contains(provider.user, "invalid character") {
		t.Fatalf("repair was not constrained: %q / %q", provider.system, provider.user)
	}
}

func TestFillEpisodeSourceChapterIDsFromChapterRange(t *testing.T) {
	ep := models.EpisodeAdaptation{ChapterStart: 2, ChapterEnd: 3, SourceChapterIDs: "[]"}
	chapters := []models.Chapter{{ID: 101, Order: 1}, {ID: 205, Order: 2}, {ID: 309, Order: 3}}
	if err := fillEpisodeSourceChapterIDs(&ep, chapters); err != nil {
		t.Fatal(err)
	}
	if ep.SourceChapterIDs != `[205,309]` {
		t.Fatalf("source ids=%s", ep.SourceChapterIDs)
	}
}

func TestFillEpisodeSourceChapterIDsPreservesValidModelIDs(t *testing.T) {
	ep := models.EpisodeAdaptation{ChapterStart: 2, ChapterEnd: 3, SourceChapterIDs: `[205]`}
	if err := fillEpisodeSourceChapterIDs(&ep, []models.Chapter{{ID: 205, Order: 2}}); err != nil {
		t.Fatal(err)
	}
	if ep.SourceChapterIDs != `[205]` {
		t.Fatalf("valid ids changed: %s", ep.SourceChapterIDs)
	}
}

func TestNormalizeAdaptationJSONDoesNotCallRepairForValidJSON(t *testing.T) {
	raw := `{"summary":"测试","episodes":[]}`
	provider := &adaptationRepairProvider{response: `{}`}
	_, repaired, err := normalizeAdaptationJSONWithRepair(provider, raw)
	if err != nil {
		t.Fatal(err)
	}
	if provider.calls != 0 || repaired != raw {
		t.Fatalf("valid JSON unexpectedly repaired: calls=%d raw=%s", provider.calls, repaired)
	}
}
