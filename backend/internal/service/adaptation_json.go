package service

import (
	"encoding/json"
	"fmt"
	"strings"
)

var adaptationJSONTextFields = map[string]bool{
	"source_chapter_ids": true,
	"must_keep_events":   true,
	"optional_events":    true,
	"omitted_events":     true,
}

// normalizeAdaptationJSON converts structured JSON values emitted for model fields
// persisted as JSON text. Models naturally emit arrays for these fields, while the
// database model keeps them as strings for backwards compatibility.
func normalizeAdaptationJSON(raw string) (string, error) {
	start, end := strings.Index(raw, "{"), strings.LastIndex(raw, "}")
	if start < 0 || end < start {
		return "", fmt.Errorf("model output does not contain a JSON object")
	}
	var root map[string]any
	if err := json.Unmarshal([]byte(raw[start:end+1]), &root); err != nil {
		return "", fmt.Errorf("invalid model JSON: %w", err)
	}
	episodes, _ := root["episodes"].([]any)
	for _, item := range episodes {
		episode, ok := item.(map[string]any)
		if !ok {
			continue
		}
		for field := range adaptationJSONTextFields {
			value, exists := episode[field]
			if !exists || value == nil {
				continue
			}
			if _, alreadyString := value.(string); alreadyString {
				continue
			}
			encoded, err := json.Marshal(value)
			if err != nil {
				return "", fmt.Errorf("encode episodes.%s: %w", field, err)
			}
			episode[field] = string(encoded)
		}
	}
	normalized, err := json.Marshal(root)
	if err != nil {
		return "", err
	}
	return string(normalized), nil
}
