package service

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

//go:embed all:combat_reference_data/plain combat_reference_data/SKILL.md
var combatReferenceFS embed.FS

var combatReferenceScopes = []string{"scenes", "design", "moves", "skills", "scripts"}

type combatRouterConfig struct {
	Weights    map[string]map[string]int `json:"weights"`
	WeakMinLen int                       `json:"weak_min_len"`
	WeakTerms  []string                  `json:"weak_terms"`
}

type combatReferenceIndex struct {
	Available          []map[string]any `json:"available"`
	RoutingRules       []any            `json:"routing_rules"`
	ConflictResolution []any            `json:"conflict_resolution"`
}

type CombatReferenceCandidate struct {
	Scope           string              `json:"scope"`
	ID              string              `json:"id"`
	File            string              `json:"file"`
	Name            string              `json:"name"`
	HitCount        int                 `json:"hit_count"`
	WeightedScore   int                 `json:"weighted_score"`
	FieldHits       map[string][]string `json:"field_hits"`
	MatchedKeywords []string            `json:"matched_keywords"`
	RetrievalHint   string              `json:"retrieval_hint"`
	AvoidWhen       []string            `json:"avoid_when"`
	Showcase        string              `json:"showcase,omitempty"`
	SelectionNotice map[string]any      `json:"selection_notice,omitempty"`
	strong          bool
	multi           bool
}

type CombatReferenceSearchResult struct {
	Scope              string                     `json:"scope"`
	Query              string                     `json:"query"`
	MatchRule          string                     `json:"match_rule"`
	Primary            *CombatReferenceCandidate  `json:"primary"`
	Eligible           []CombatReferenceCandidate `json:"eligible"`
	WeakFallback       bool                       `json:"weak_fallback"`
	RoutingRules       []any                      `json:"routing_rules"`
	ConflictResolution []any                      `json:"conflict_resolution"`
}

type CombatReferenceDocument struct {
	Scope    string         `json:"scope"`
	ID       string         `json:"id"`
	Metadata map[string]any `json:"metadata"`
	Content  string         `json:"content"`
}

type CombatReferenceService struct {
	config  combatRouterConfig
	indexes map[string]combatReferenceIndex
	items   map[string]map[string]map[string]any
	content map[string]map[string]string
	weak    map[string]bool
}

func NewCombatReferenceService() (*CombatReferenceService, error) {
	service := &CombatReferenceService{indexes: map[string]combatReferenceIndex{}, items: map[string]map[string]map[string]any{}, content: map[string]map[string]string{}, weak: map[string]bool{}}
	if err := readCombatJSON("combat_reference_data/plain/router_config.json", &service.config); err != nil {
		return nil, err
	}
	if service.config.WeakMinLen <= 0 {
		service.config.WeakMinLen = 2
	}
	for _, term := range service.config.WeakTerms {
		service.weak[combatCompact(term)] = true
	}
	for _, scope := range combatReferenceScopes {
		var index combatReferenceIndex
		if err := readCombatJSON(fmt.Sprintf("combat_reference_data/plain/%s/_index.json", scope), &index); err != nil {
			return nil, err
		}
		service.indexes[scope], service.items[scope], service.content[scope] = index, map[string]map[string]any{}, map[string]string{}
		for _, raw := range index.Available {
			id := strings.TrimSpace(fmt.Sprint(raw["id"]))
			if id == "" {
				continue
			}
			metadata := cloneCombatMap(raw)
			var extra map[string]any
			if err := readCombatJSON(fmt.Sprintf("combat_reference_data/plain/%s/%s.meta.json", scope, id), &extra); err == nil {
				for key, value := range extra {
					metadata[key] = value
				}
			}
			service.items[scope][id] = metadata
			for _, extension := range []string{".md", ".txt"} {
				path := fmt.Sprintf("combat_reference_data/plain/%s/%s%s", scope, id, extension)
				if data, err := combatReferenceFS.ReadFile(path); err == nil {
					service.content[scope][id] = string(data)
					break
				}
			}
		}
	}
	return service, nil
}

func readCombatJSON(path string, target any) error {
	data, err := combatReferenceFS.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

func cloneCombatMap(source map[string]any) map[string]any {
	out := make(map[string]any, len(source))
	for key, value := range source {
		out[key] = value
	}
	return out
}

func combatCompact(value string) string {
	var out strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			out.WriteRune(r)
		}
	}
	return out.String()
}

func combatStringSlice(value any) []string {
	items, ok := value.([]any)
	if !ok {
		if strings.TrimSpace(fmt.Sprint(value)) == "" || value == nil {
			return nil
		}
		return []string{fmt.Sprint(value)}
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		if text := strings.TrimSpace(fmt.Sprint(item)); text != "" {
			result = append(result, text)
		}
	}
	return result
}

func combatKeywordMatches(query, keyword string) bool {
	needle := combatCompact(keyword)
	if needle == "" {
		return false
	}
	if regexp.MustCompile(`^[a-z0-9]+$`).MatchString(needle) {
		pattern := regexp.MustCompile(`(?i)(^|[^a-z])` + regexp.QuoteMeta(needle) + `s?([^a-z]|$)`)
		return pattern.MatchString(strings.ToLower(query)) || pattern.MatchString(combatCompact(query))
	}
	return strings.Contains(combatCompact(query), needle)
}

func (s *CombatReferenceService) Search(scope, query string) (*CombatReferenceSearchResult, error) {
	scope, query = strings.TrimSpace(scope), strings.TrimSpace(query)
	index, ok := s.indexes[scope]
	if !ok {
		return nil, fmt.Errorf("未知战斗资料范围: %s", scope)
	}
	if query == "" {
		return nil, fmt.Errorf("检索词不能为空")
	}
	candidates := make([]CombatReferenceCandidate, 0)
	for id, item := range s.items[scope] {
		candidate := CombatReferenceCandidate{Scope: scope, ID: id, File: fmt.Sprint(item["file"]), Name: fmt.Sprint(item["name"]), FieldHits: map[string][]string{}, RetrievalHint: fmt.Sprint(item["retrieval_hint"]), AvoidWhen: combatStringSlice(item["avoid_when"]), Showcase: fmt.Sprint(item["showcase"])}
		if candidate.Name == "<nil>" || strings.TrimSpace(candidate.Name) == "" {
			candidate.Name = strings.TrimSuffix(filepath.Base(candidate.File), filepath.Ext(candidate.File))
		}
		unique, strong, multi := map[string]bool{}, false, false
		for field, weight := range s.config.Weights[scope] {
			for _, keyword := range combatStringSlice(item[field]) {
				if !combatKeywordMatches(query, keyword) {
					continue
				}
				candidate.FieldHits[field] = append(candidate.FieldHits[field], keyword)
				candidate.MatchedKeywords = append(candidate.MatchedKeywords, keyword)
				candidate.WeightedScore += weight
				compact := combatCompact(keyword)
				unique[compact] = true
				if len([]rune(compact)) >= s.config.WeakMinLen {
					multi = true
					if !s.weak[compact] {
						strong = true
					}
				}
			}
		}
		candidate.HitCount = len(unique)
		if candidate.HitCount == 0 {
			continue
		}
		candidate.strong, candidate.multi = strong, multi
		if scope == "skills" {
			candidate.SelectionNotice = map[string]any{"secondary_keywords": item["secondary_keywords"], "condition_prompt": item["condition_prompt"], "condition_options": item["condition_options"], "move_library_exclusions": item["move_library_exclusions"]}
		}
		candidates = append(candidates, candidate)
	}
	hasStrong := false
	for _, candidate := range candidates {
		if candidate.strong {
			hasStrong = true
			break
		}
	}
	eligible := make([]CombatReferenceCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if hasStrong && !candidate.strong {
			continue
		}
		if !hasStrong && !candidate.multi && len(candidates) > 1 {
			continue
		}
		eligible = append(eligible, candidate)
	}
	sort.SliceStable(eligible, func(i, j int) bool {
		if eligible[i].HitCount != eligible[j].HitCount {
			return eligible[i].HitCount > eligible[j].HitCount
		}
		if eligible[i].WeightedScore != eligible[j].WeightedScore {
			return eligible[i].WeightedScore > eligible[j].WeightedScore
		}
		return eligible[i].ID < eligible[j].ID
	})
	result := &CombatReferenceSearchResult{Scope: scope, Query: query, Eligible: eligible, WeakFallback: len(candidates) > 0 && !hasStrong, RoutingRules: index.RoutingRules, ConflictResolution: index.ConflictResolution, MatchRule: "至少命中一个路由关键词；按唯一命中数与字段权重排序；弱词仅在无强匹配时回退"}
	if scope == "skills" {
		result.MatchRule, result.Primary = "技能仅按单体、群体或buff召回候选，不自动选择主技能", nil
	} else if len(eligible) > 0 {
		primary := eligible[0]
		result.Primary = &primary
	}
	return result, nil
}

func (s *CombatReferenceService) Showcase(scope, id string) (string, error) {
	items, ok := s.items[strings.TrimSpace(scope)]
	if !ok {
		return "", fmt.Errorf("未知战斗资料范围: %s", scope)
	}
	metadata, ok := items[strings.TrimSpace(id)]
	if !ok {
		return "", fmt.Errorf("战斗资料不存在: %s/%s", scope, id)
	}
	showcase := strings.TrimSpace(fmt.Sprint(metadata["showcase"]))
	if showcase == "" || showcase == "<nil>" {
		return "", fmt.Errorf("该资料暂无展示样本")
	}
	showcase = filepath.ToSlash(filepath.Clean(strings.ReplaceAll(showcase, "\\", "/")))
	if strings.HasPrefix(showcase, "../") || strings.HasPrefix(showcase, "/") || strings.Contains(showcase, ":") || filepath.Ext(showcase) != ".gif" {
		return "", fmt.Errorf("展示样本路径无效")
	}
	return showcase, nil
}

func (s *CombatReferenceService) Read(scope, id string) (*CombatReferenceDocument, error) {
	items, ok := s.items[strings.TrimSpace(scope)]
	if !ok {
		return nil, fmt.Errorf("未知战斗资料范围: %s", scope)
	}
	metadata, ok := items[strings.TrimSpace(id)]
	if !ok {
		return nil, fmt.Errorf("战斗资料不存在: %s/%s", scope, id)
	}
	content := s.content[scope][id]
	if strings.TrimSpace(content) == "" {
		return nil, fmt.Errorf("战斗资料正文缺失: %s/%s", scope, id)
	}
	return &CombatReferenceDocument{Scope: scope, ID: id, Metadata: cloneCombatMap(metadata), Content: content}, nil
}

func CombatReferenceDataFiles() ([]string, error) {
	return fs.Glob(combatReferenceFS, "combat_reference_data/plain/*/*")
}
