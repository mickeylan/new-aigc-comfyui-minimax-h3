package service

import (
	"context"
	"strings"
	"testing"
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
