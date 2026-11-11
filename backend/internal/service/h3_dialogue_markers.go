package service

import (
	"regexp"
	"strconv"
	"strings"

	"comfyui-console/internal/models"
)

// h3DialogueMarkerPattern matches the director-placed speech anchors ([[D1]]...).
// A trailing period added by sentence-ending repair belongs to the marker.
var h3DialogueMarkerPattern = regexp.MustCompile(`\s*\[\[D([0-9]+)\]\]\.?`)

// applyH3DialogueMarkers replaces director-placed [[Dn]] anchors with the
// deterministic rendering of Dialogue n. The model only chooses the position;
// speech bytes always come from the structured Dialogue. Any incomplete,
// duplicated or out-of-order marker set is discarded so the caller falls back
// to the existing placement path.
func applyH3DialogueMarkers(body string, dubs []models.Dialogue, referenceLines []string) (string, bool) {
	matches := h3DialogueMarkerPattern.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		return body, false
	}
	valid := validSceneDialogues(dubs)
	ok := len(valid) > 0 && len(matches) == len(valid)
	for i, match := range matches {
		if n, err := strconv.Atoi(match[1]); err != nil || n != i+1 {
			ok = false
		}
	}
	if !ok {
		return strings.TrimSpace(h3DialogueMarkerPattern.ReplaceAllString(body, "")), false
	}
	// Remove clauses that earlier compiler passes appended at Shot ends; the
	// markers survive that cleanup and become the only speech positions.
	cleaned := stripStructuredDialogueFromAction(body, valid)
	ids := dialogueSpeakerIDs(valid)
	next := 0
	out := h3DialogueMarkerPattern.ReplaceAllStringFunc(cleaned, func(string) string {
		clause := " " + renderH3PlannedDialogue(valid[next], ids[next], referenceLines)
		next++
		return clause
	})
	if next != len(valid) {
		return strings.TrimSpace(h3DialogueMarkerPattern.ReplaceAllString(body, "")), false
	}
	return strings.TrimSpace(out), true
}
