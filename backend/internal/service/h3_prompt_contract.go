package service

import (
	"fmt"
	"regexp"
	"strconv"
)

// h3ContractShotPattern matches exactly the spans of h3ShotMarkerPattern, with extra groups
// for the optional At timestamp.
var h3ContractShotPattern = regexp.MustCompile(`\[Shot\s+([0-9]+)(?:\s*\|[^\]]*)?\](?:\s+At\s+([0-9]{2}):([0-9]{2})\.([0-9]{3}),)?`)

// h3ContractTimestampTolerance absorbs millisecond rounding when comparing cut times.
const h3ContractTimestampTolerance = 0.08

type h3PromptShot struct {
	Number int
	// At is the cut time in seconds, or -1 when the marker carries no At timestamp.
	At   float64
	Body string
}

type h3SubjectRef struct {
	Number int
	Shot   int // number of the enclosing Shot marker; 0 before the first marker
}

// h3PromptDoc is the structural reading of an H3 detailed_description body.
type h3PromptDoc struct {
	Preamble       string
	Shots          []h3PromptShot
	Subjects       []h3SubjectRef
	BareTimestamps int
}

// parseH3PromptContract reads Shot markers, At timestamps, Subject tags and bare At prose in one
// pass. It only reports; callers decide which findings trigger a repair.
func parseH3PromptContract(text string) h3PromptDoc {
	doc := h3PromptDoc{BareTimestamps: len(h3BareTimestampPattern.FindAllString(text, -1))}
	matches := h3ContractShotPattern.FindAllStringSubmatchIndex(text, -1)
	bodyStart := len(text)
	if len(matches) > 0 {
		bodyStart = matches[0][0]
	}
	doc.Preamble = text[:bodyStart]
	for i, match := range matches {
		// An overflowing number reads as MaxInt, as the inline checks did; range checks reject it.
		n, _ := strconv.Atoi(text[match[2]:match[3]])
		shot := h3PromptShot{Number: n, At: -1}
		if match[4] >= 0 {
			minutes, _ := strconv.Atoi(text[match[4]:match[5]])
			seconds, _ := strconv.Atoi(text[match[6]:match[7]])
			millis, _ := strconv.Atoi(text[match[8]:match[9]])
			shot.At = float64(minutes*60+seconds) + float64(millis)/1000
		}
		end := len(text)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		shot.Body = text[match[1]:end]
		doc.Shots = append(doc.Shots, shot)
	}
	// Scan Subjects over the whole text: a "[Shot N | ...]" marker may itself carry a tag.
	for _, tag := range h3SubjectTagPattern.FindAllStringSubmatchIndex(text, -1) {
		ref := h3SubjectRef{}
		ref.Number, _ = strconv.Atoi(text[tag[2]:tag[3]])
		for i, match := range matches {
			if match[0] <= tag[0] {
				ref.Shot = doc.Shots[i].Number
			}
		}
		doc.Subjects = append(doc.Subjects, ref)
	}
	return doc
}

// SubjectRangeIssues reports every Subject tag outside the selected reference images, once per
// occurrence and in text order.
func (doc h3PromptDoc) SubjectRangeIssues(referenceCount int) []string {
	var issues []string
	for _, subject := range doc.Subjects {
		if subject.Number < 1 || subject.Number > referenceCount {
			issues = append(issues, fmt.Sprintf("Subject编号%d超出已选参考图范围1-%d", subject.Number, referenceCount))
		}
	}
	return issues
}

// ShotStructureIssues reports out-of-range, duplicate and missing Shot numbers. Nothing is
// checked when expectedShotCount is 0, because the director then owns the cut count.
func (doc h3PromptDoc) ShotStructureIssues(expectedShotCount int) []string {
	if expectedShotCount <= 0 {
		return nil
	}
	var issues []string
	seen := map[int]bool{}
	for _, shot := range doc.Shots {
		if shot.Number < 1 || shot.Number > expectedShotCount {
			issues = append(issues, fmt.Sprintf("Shot %d超出范围1-%d", shot.Number, expectedShotCount))
		}
		if seen[shot.Number] {
			issues = append(issues, fmt.Sprintf("Shot %d重复", shot.Number))
		}
		seen[shot.Number] = true
	}
	if len(seen) != expectedShotCount {
		issues = append(issues, fmt.Sprintf("Shot数量为%d，期望%d", len(seen), expectedShotCount))
	}
	return issues
}

// TimelineNotes are review hints only and never trigger a repair: Shot 1 carrying an At time,
// cut times that do not increase, and cuts past the clip end.
func (doc h3PromptDoc) TimelineNotes(duration float64) []string {
	var notes []string
	previous := -1.0
	for _, shot := range doc.Shots {
		if shot.At < 0 {
			continue
		}
		if shot.Number == 1 {
			notes = append(notes, "Shot 1不应带At时间")
		}
		if previous >= 0 && shot.At+h3ContractTimestampTolerance <= previous {
			notes = append(notes, fmt.Sprintf("Shot %d的At时间%s没有晚于上一切点", shot.Number, formatH3Timestamp(shot.At)))
		}
		if duration > 0 && shot.At > duration+h3ContractTimestampTolerance {
			notes = append(notes, fmt.Sprintf("Shot %d的At时间%s超出片长%.1f秒", shot.Number, formatH3Timestamp(shot.At), duration))
		}
		previous = shot.At
	}
	return notes
}
