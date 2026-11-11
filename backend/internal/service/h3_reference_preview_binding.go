package service

import (
	"regexp"
	"strings"
)

// Binding phrases from useSubjectTags are English; inside translated Chinese prose
// they would leave mixed-language residue such as "攥紧the prop shown in <Picture 7>".
var h3PreviewPictureBindingPattern = regexp.MustCompile(`(?:the prop shown in |the environment established by |the reference from )(<Picture [0-9]+>)`)

// Template retention lines ("<Subject N> (appears in ...)", also when translated)
// open with a parenthesis right after the tag; director notes do not.
var h3RetentionTemplateLinePattern = regexp.MustCompile(`^<Subject [0-9]+>\s*[(（]`)

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
			bound.WriteString(bindH3PreviewProse(rest, referenceLines))
			break
		}
		bound.WriteString(bindH3PreviewProse(rest[:loc[0]], referenceLines))
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
		// Only the per-Subject template lines are rebuilt; director continuity
		// notes (screen direction, effect ownership, final state) are kept.
		for _, line := range strings.Split(h3PromptSection(prompt, "retention_analysis:"), "\n") {
			if line = strings.TrimSpace(line); line != "" && !h3RetentionTemplateLinePattern.MatchString(line) {
				retention = append(retention, line)
			}
		}
		prompt = prompt[:start] + "retention_analysis:\n" + strings.Join(retention, "\n") + "\n\n" + prompt[end:]
	}
	return prompt
}

func bindH3PreviewProse(text string, referenceLines []string) string {
	bound := useSubjectTags(text, referenceLines)
	if bound == text || !h3BriefHanOutsideNamePattern.MatchString(h3PreviewPictureBindingPattern.ReplaceAllString(bound, "")) {
		return bound
	}
	return h3PreviewPictureBindingPattern.ReplaceAllStringFunc(bound, func(phrase string) string {
		picture := h3PreviewPictureBindingPattern.FindStringSubmatch(phrase)[1]
		switch {
		case strings.HasPrefix(phrase, "the prop shown in "):
			return picture + " 中的道具"
		case strings.HasPrefix(phrase, "the environment established by "):
			return picture + " 所示环境"
		}
		return picture
	})
}
