package service

import "strings"

// Bind only the visual body at preview boundaries. Definitions intentionally keep
// human-readable asset names; authored speech bytes must never be substituted.
// This is not applied to confirmed/saved execution prompts.
func bindH3ReferencePreview(prompt string, referenceLines []string) string {
	body := h3PromptSection(prompt, "detailed_description:")
	if body == "" || !strings.Contains(prompt, "subject_definitions:") {
		return prompt
	}
	var bound strings.Builder
	rest := body
	for {
		loc := dialogueBlockRE.FindStringIndex(rest)
		if loc == nil {
			bound.WriteString(useSubjectTags(rest, referenceLines))
			break
		}
		bound.WriteString(useSubjectTags(rest[:loc[0]], referenceLines))
		bound.WriteString(rest[loc[0]:loc[1]])
		rest = rest[loc[1]:]
	}
	visual := bound.String()
	if visual == body {
		return prompt
	}
	prompt = strings.Replace(prompt, body, visual, 1)
	start := strings.Index(prompt, "retention_analysis:")
	end := strings.Index(prompt, "detailed_description:")
	if start >= 0 && end > start {
		_, retention, _ := h3VideoSubjects(referenceLines)
		retention = h3VisibleRetention(visual, retention)
		prompt = prompt[:start] + "retention_analysis:\n" + strings.Join(retention, "\n") + "\n\n" + prompt[end:]
	}
	return prompt
}
