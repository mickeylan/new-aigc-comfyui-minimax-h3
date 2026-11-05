package service

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"comfyui-console/internal/models"
)

// H3DialoguePlacementReport is advisory only.
type H3DialoguePlacementReport struct {
	Placed   []H3PlacedDialogue   `json:"placed"`
	Unplaced []H3UnplacedDialogue `json:"unplaced"`
	Warnings []string             `json:"warnings,omitempty"`
}

type H3PlacedDialogue struct {
	DialogueIndex int    `json:"-"`
	DialogueID    uint   `json:"dialogue_id"`
	Character     string `json:"character"`
	ShotNumber    int    `json:"shot_number"`
	TimeSegment   string `json:"time_segment"`
}

type H3UnplacedDialogue struct {
	DialogueID uint   `json:"dialogue_id"`
	Order      int    `json:"order"`
	Character  string `json:"character"`
	Text       string `json:"text"`
	Reason     string `json:"reason"`
}

var h3ClockPattern = regexp.MustCompile(`(?i)(\d{1,2}):(\d{2})(?:[.:](\d{1,3}))?`)
var h3SecondRangePattern = regexp.MustCompile(`(?i)^\s*(\d+(?:\.\d+)?)\s*(?:-|~|–|—|至|到)\s*(\d+(?:\.\d+)?)\s*(?:s|sec|seconds?|秒)?\s*$`)
var h3PlacedTagPattern = regexp.MustCompile(`(?s)<d>(?:\[Chinese\]\s*)?(.*?)</d>`)

// Carry-over ranges intentionally have no opening <d>; keep this pattern narrow so
// arbitrary prose ending in </d> can never be mistaken for spoken text.
var h3CarryFragmentPattern = regexp.MustCompile(`(?is)((?:<Subject\s+\d+>|[^\s。.!?；;<>()]+)\s*)\(S(\d+)\)'s\s+words\s+carry\s+over\s+from\s+the\s+previous\s+shot\s*<scenetrans>(.*?)</d>`)
var h3SpeakerPattern = regexp.MustCompile(`\(S(\d+)\)`)
var h3SubjectPattern = regexp.MustCompile(`(?i)<Subject\s+(\d+)>`)
var h3BareShotPattern = regexp.MustCompile(`\[Shot\s+([0-9]+)(?:\s*\|[^\]]*)?\]`)
var h3HeaderTimePattern = regexp.MustCompile(`(?i)^\s*(?:\[(\d+(?:\.\d+)?)\s*s\]|At\s+(\d+(?:\.\d+)?)\s+seconds?\b|At\s+(\d{1,2}):(\d{2})(?:[.:](\d{1,3}))?\s*,?)`)

func parseDialogueTimeSegment(value string) (float64, float64, bool) {
	clock := h3ClockPattern.FindAllStringSubmatch(value, -1)
	toSeconds := func(m []string) float64 {
		minutes, _ := strconv.Atoi(m[1])
		seconds, _ := strconv.Atoi(m[2])
		fraction := 0.0
		if m[3] != "" {
			fraction, _ = strconv.ParseFloat("0."+m[3], 64)
		}
		return float64(minutes*60+seconds) + fraction
	}
	if len(clock) == 2 {
		start, end := toSeconds(clock[0]), toSeconds(clock[1])
		return start, end, end > start
	}
	if m := h3SecondRangePattern.FindStringSubmatch(value); len(m) == 3 {
		start, _ := strconv.ParseFloat(m[1], 64)
		end, _ := strconv.ParseFloat(m[2], 64)
		return start, end, end > start
	}
	return 0, 0, false
}

// promptShotStarts accepts timestamps only when they immediately follow a shot
// marker. A multi-shot prompt is unmappable unless every start is explicit and
// strictly increasing.
func promptShotStarts(body string) ([]int, []float64) {
	matches := h3BareShotPattern.FindAllStringSubmatchIndex(body, -1)
	positions, starts := make([]int, 0, len(matches)), make([]float64, 0, len(matches))
	for _, match := range matches {
		m := h3HeaderTimePattern.FindStringSubmatch(body[match[1]:])
		if len(m) == 0 {
			// H3's first Shot begins at 0.00 by definition; later cuts must
			// carry a real timestamp, never all default to zero/the last Shot.
			if len(positions) == 0 {
				positions = append(positions, match[0])
				starts = append(starts, 0)
				continue
			}
			return nil, nil
		}
		var start float64
		switch {
		case m[1] != "":
			start, _ = strconv.ParseFloat(m[1], 64)
		case m[2] != "":
			start, _ = strconv.ParseFloat(m[2], 64)
		default:
			min, _ := strconv.Atoi(m[3])
			sec, _ := strconv.Atoi(m[4])
			start = float64(min*60 + sec)
			if m[5] != "" {
				f, _ := strconv.ParseFloat("0."+m[5], 64)
				start += f
			}
		}
		if len(starts) > 0 && start <= starts[len(starts)-1] {
			return nil, nil
		}
		positions = append(positions, match[0])
		starts = append(starts, start)
	}
	return positions, starts
}

type placedOccurrence struct {
	text                   string
	speaker, subject, shot int
	position               int
}

func placedOccurrences(body string) []placedOccurrence {
	markers := h3ShotMarkerPattern.FindAllStringSubmatchIndex(body, -1)
	shotAt := func(position int) int {
		shot := 0
		for _, marker := range markers {
			if marker[0] >= position {
				break
			}
			shot, _ = strconv.Atoi(body[marker[2]:marker[3]])
		}
		return shot
	}
	var out []placedOccurrence
	for _, tag := range h3PlacedTagPattern.FindAllStringSubmatchIndex(body, -1) {
		clauseStart := strings.LastIndexAny(body[:tag[0]], "\n。.!?；;")
		if clauseStart < 0 {
			clauseStart = 0
		} else {
			_, width := utf8.DecodeRuneInString(body[clauseStart:])
			clauseStart += width
		}
		lead := body[clauseStart:tag[0]]
		speakers, subjects := h3SpeakerPattern.FindAllStringSubmatch(lead, -1), h3SubjectPattern.FindAllStringSubmatch(lead, -1)
		speaker, subject := 0, 0
		if len(speakers) > 0 {
			speaker, _ = strconv.Atoi(speakers[len(speakers)-1][1])
		}
		if len(subjects) > 0 {
			subject, _ = strconv.Atoi(subjects[len(subjects)-1][1])
		}
		text := body[tag[2]:tag[3]]
		// renderDialogueRange marks a non-final range with a synthetic hyphen.
		if strings.HasPrefix(body[tag[1]:], " <scenetrans>") {
			text = strings.TrimSuffix(text, "-")
		}
		out = append(out, placedOccurrence{text: canonicalDialogueText(text), speaker: speaker, subject: subject, shot: shotAt(tag[0]), position: tag[0]})
	}
	for _, carry := range h3CarryFragmentPattern.FindAllStringSubmatchIndex(body, -1) {
		subject, speaker := 0, 0
		leadSubjects := h3SubjectPattern.FindAllStringSubmatch(body[carry[2]:carry[3]], -1)
		if len(leadSubjects) > 0 {
			subject, _ = strconv.Atoi(leadSubjects[len(leadSubjects)-1][1])
		}
		speaker, _ = strconv.Atoi(body[carry[4]:carry[5]])
		text := body[carry[6]:carry[7]]
		if strings.HasPrefix(body[carry[1]:], " <scenetrans>") {
			text = strings.TrimSuffix(text, "-")
		}
		out = append(out, placedOccurrence{text: canonicalDialogueText(text), speaker: speaker, subject: subject, shot: shotAt(carry[6]), position: carry[6]})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].position < out[j].position })
	return out
}

func diagnose(body string, dubs []models.Dialogue, reasons map[int]string, refs ...[]string) H3DialoguePlacementReport {
	ids, occurrences := dialogueSpeakerIDs(dubs), placedOccurrences(body)
	used := make([]bool, len(occurrences))
	report := H3DialoguePlacementReport{}
	for i, d := range dubs {
		found, foundEnd, mismatch := -1, -1, false
		target := canonicalDialogueText(strings.ReplaceAll(strings.ReplaceAll(d.Text, "<scenetrans>", ""), "<cutoff>", ""))
		for j, first := range occurrences {
			if used[j] || (ids[i] > 0 && first.speaker != ids[i]) {
				continue
			}
			assembled := ""
			for k := j; k < len(occurrences); k++ {
				o := occurrences[k]
				// Fragments are occurrence-adjacent, from one voice, across ordered
				// consecutive shots. Repeated/out-of-order markers remain unresolved.
				if used[k] || o.speaker != first.speaker || (k > j && o.shot != occurrences[k-1].shot+1) {
					break
				}
				assembled += o.text
				if !strings.HasPrefix(target, assembled) {
					break
				}
				bindingOK := true
				if len(refs) > 0 && o.subject > 0 {
					expected := characterSubjectTagByName(d.Character, refs[0])
					if expected != "" && expected != fmt.Sprintf("<Subject %d>", o.subject) {
						bindingOK, mismatch = false, true
					}
				}
				if assembled == target && bindingOK {
					found, foundEnd = j, k
					break
				}
				if !bindingOK {
					break
				}
			}
			if found >= 0 {
				break
			}
		}
		if found >= 0 {
			for j := found; j <= foundEnd; j++ {
				used[j] = true
			}
			report.Placed = append(report.Placed, H3PlacedDialogue{DialogueIndex: i, DialogueID: d.ID, Character: d.Character, ShotNumber: occurrences[found].shot, TimeSegment: d.TimeSegment})
			continue
		}
		reason := reasons[i]
		if mismatch {
			reason = "speaker_binding_mismatch"
		}
		if reason == "" {
			reason = "no reliable shot mapping"
		}
		report.Unplaced = append(report.Unplaced, H3UnplacedDialogue{DialogueID: d.ID, Order: d.Order, Character: d.Character, Text: d.Text, Reason: reason})
	}
	return report
}

// DiagnoseH3DialoguePlacement performs no writes.
func DiagnoseH3DialoguePlacement(body string, dubs []models.Dialogue, referenceLines []string) H3DialoguePlacementReport {
	return diagnose(body, validSceneDialogues(dubs), nil, referenceLines)
}

func ResolveH3DialoguePlacement(body string, dubs []models.Dialogue, referenceLines []string, shots []models.Shot) (string, H3DialoguePlacementReport) {
	valid := validSceneDialogues(dubs)
	positions, starts := promptShotStarts(body)
	reasons := map[int]string{}
	if len(positions) == 0 {
		for i := range valid {
			reasons[i] = "prompt shot timestamps are missing or non-increasing"
		}
		return body, diagnose(body, valid, reasons, referenceLines)
	}
	ids := dialogueSpeakerIDs(valid)
	existing := diagnose(body, valid, nil, referenceLines)
	already := map[int]bool{}
	for _, p := range existing.Placed {
		already[p.DialogueIndex] = true
	}
	byMarker := map[int][]string{}
	spans := map[int]string{}
	for i, d := range valid {
		if already[i] {
			continue
		}
		start, end, ok := parseDialogueTimeSegment(d.TimeSegment)
		if !ok {
			reasons[i] = "missing or unsupported time segment"
			continue
		}
		marker := sort.Search(len(starts), func(j int) bool { return starts[j] > start }) - 1
		if marker < 0 {
			reasons[i] = "time segment does not map to a prompt shot"
			continue
		}
		presentation := models.ShotSpeechPresentation("")
		if len(shots) == len(positions) {
			presentation = shots[marker].SpeechPresentation
		}
		byMarker[marker] = append(byMarker[marker], renderDialogueRange(d, ids[i], referenceLines, d.Text, false, false, presentation == models.ShotSpeechPresentationVisibleSpeaker, nil))
		if marker+1 < len(starts) && end > starts[marker+1] {
			spans[i] = fmt.Sprintf("dialogue %d spans shot %d into shot %d; retained as continuous speech", d.ID, marker+1, marker+2)
		}
	}
	if len(byMarker) == 0 {
		return body, diagnose(body, valid, reasons, referenceLines)
	}
	var out strings.Builder
	for i, pos := range positions {
		if i == 0 {
			out.WriteString(body[:pos])
		}
		next := len(body)
		if i+1 < len(positions) {
			next = positions[i+1]
		}
		out.WriteString(strings.TrimRight(body[pos:next], " \n\t"))
		if lines := byMarker[i]; len(lines) > 0 {
			out.WriteString(" ")
			out.WriteString(strings.Join(lines, " "))
		}
		if next < len(body) {
			out.WriteString("\n")
		}
	}
	report := diagnose(out.String(), valid, reasons, referenceLines)
	for _, warning := range spans {
		report.Warnings = append(report.Warnings, warning)
	}
	sort.Strings(report.Warnings)
	return out.String(), report
}
