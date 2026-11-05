package service

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"comfyui-console/internal/models"
)

var h3DialogueClauseEndPattern = regexp.MustCompile(`^\s*(?:[。.!?；;]|$)`)

// reconcileExistingH3Dialogue retains only dialogue already present with an
// authoritative text/speaker binding. It returns the dialogues which still
// need placement. Retained clauses stay in their original shot; no timing is
// inferred here.
func reconcileExistingH3Dialogue(original, cleaned string, dubs []models.Dialogue, referenceLines []string) (string, []models.Dialogue) {
	valid := validSceneDialogues(dubs)
	if len(valid) == 0 || !strings.Contains(original, "<d>") {
		return cleaned, valid
	}
	ids := dialogueSpeakerIDs(valid)
	used := make([]bool, len(valid))
	for _, placed := range DiagnoseH3DialoguePlacement(cleaned, valid, referenceLines).Placed {
		used[placed.DialogueIndex] = true
	}
	type clause struct {
		shot int
		text string
	}
	var retained []clause
	markers := h3ShotMarkerPattern.FindAllStringSubmatchIndex(original, -1)
	for _, tag := range h3PlacedTagPattern.FindAllStringSubmatchIndex(original, -1) {
		text := canonicalDialogueText(original[tag[2]:tag[3]])
		clauseStart := strings.LastIndexAny(original[:tag[0]], "\n。.!?；;")
		if clauseStart < 0 {
			clauseStart = 0
		} else {
			_, width := utf8.DecodeRuneInString(original[clauseStart:])
			clauseStart += width
		}
		lead := original[clauseStart:tag[0]]
		speaker, statedSubject := 0, ""
		speakerMatches := h3SpeakerPattern.FindAllStringSubmatchIndex(lead, -1)
		if len(speakerMatches) > 0 {
			last := speakerMatches[len(speakerMatches)-1]
			speaker, _ = strconv.Atoi(lead[last[2]:last[3]])
			// Subjects after the SID can be listener mentions, so only bind the
			// nearest Subject before the speaking SID.
			for _, subject := range h3SubjectPattern.FindAllStringIndex(lead[:last[0]], -1) {
				statedSubject = lead[subject[0]:subject[1]]
			}
		}
		match := -1
		for i, d := range valid {
			if used[i] || canonicalDialogueText(d.Text) != text {
				continue
			}
			bound := speaker == ids[i] && speaker > 0
			expectedSubject := characterSubjectTagByName(d.Character, referenceLines)
			if bound && statedSubject != "" && expectedSubject != "" && statedSubject != expectedSubject {
				bound = false
			}
			if bound && statedSubject == "" {
				if speakerAt := strings.LastIndex(lead, "(S"); speakerAt >= 0 {
					prefix := strings.TrimSpace(lead[:speakerAt])
					if fields := strings.Fields(prefix); len(fields) > 0 {
						statedName := strings.Trim(fields[len(fields)-1], "：:，,。.!?；;\"'()")
						if statedName != "" && statedName != strings.TrimSpace(d.Character) {
							bound = false
						}
					}
				}
			}
			if speaker == 0 {
				name := strings.TrimSpace(d.Character)
				tag := characterSubjectTagByName(name, referenceLines)
				bound = name != "" && (strings.Contains(lead, name) || (tag != "" && strings.Contains(lead, tag)))
			}
			if bound {
				match = i
				break
			}
		}
		if match < 0 {
			continue
		}
		// Keep the speech clause, not unrelated visual prose before its speaker.
		bindingStart := -1
		if subject := characterSubjectTagByName(valid[match].Character, referenceLines); subject != "" {
			bindingStart = strings.LastIndex(lead, subject)
		}
		if nameStart := strings.LastIndex(lead, strings.TrimSpace(valid[match].Character)); nameStart > bindingStart {
			bindingStart = nameStart
		}
		if bindingStart >= 0 {
			clauseStart += bindingStart
		}
		end := tag[1]
		if suffix := h3DialogueClauseEndPattern.FindString(original[end:]); suffix != "" {
			end += len(suffix)
		}
		shot := 0
		for _, marker := range markers {
			if marker[0] > tag[0] {
				break
			}
			shot, _ = strconv.Atoi(original[marker[2]:marker[3]])
		}
		if shot == 0 {
			continue
		}
		used[match] = true
		retained = append(retained, clause{shot: shot, text: strings.TrimSpace(original[clauseStart:end])})
	}
	if len(retained) > 0 {
		matches := h3ShotMarkerPattern.FindAllStringSubmatchIndex(cleaned, -1)
		var out strings.Builder
		for i, marker := range matches {
			if i == 0 {
				out.WriteString(cleaned[:marker[0]])
			}
			next := len(cleaned)
			if i+1 < len(matches) {
				next = matches[i+1][0]
			}
			segment := strings.TrimRight(cleaned[marker[0]:next], " \n\t")
			shot, _ := strconv.Atoi(cleaned[marker[2]:marker[3]])
			var additions []string
			for _, item := range retained {
				if item.shot == shot && !strings.Contains(segment, item.text) {
					additions = append(additions, item.text)
				}
			}
			if len(additions) > 0 {
				segment = regexp.MustCompile(`([。.!?])\s+[。.!?；;]+$`).ReplaceAllString(segment, "$1")
			}
			out.WriteString(segment)
			for _, addition := range additions {
				out.WriteString(" ")
				out.WriteString(addition)
			}
			if next < len(cleaned) {
				out.WriteString("\n")
			}
		}
		if len(matches) > 0 {
			cleaned = strings.TrimSpace(out.String())
		}
	}
	remaining := make([]models.Dialogue, 0, len(valid))
	for i, d := range valid {
		if !used[i] {
			remaining = append(remaining, d)
		}
	}
	return cleaned, remaining
}
