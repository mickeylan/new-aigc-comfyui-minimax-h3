package service

import (
	"math"
	"strconv"

	"comfyui-console/internal/models"
)

// withSceneDialogueTiming returns a copy whose missing dialogue timing is filled only
// when explicit Scene timeline markers and quoted source occurrences agree exactly.
// It is intentionally pure: callers decide whether and how to persist the result.
func withSceneDialogueTiming(sc *models.Scene, dubs []models.Dialogue) []models.Dialogue {
	out := append([]models.Dialogue(nil), dubs...)
	if sc == nil || len(out) == 0 {
		return out
	}

	matches := sceneTimedBeatPattern.FindAllStringSubmatchIndex(sc.Content, -1)
	if len(matches) == 0 {
		return out
	}
	type interval struct {
		start, end float64
		from, to   int
	}
	intervals := make([]interval, 0, len(matches))
	previousEnd := -1.0
	for i, match := range matches {
		start, err1 := strconv.ParseFloat(sc.Content[match[2]:match[3]], 64)
		end, err2 := strconv.ParseFloat(sc.Content[match[4]:match[5]], 64)
		if err1 != nil || err2 != nil || math.IsNaN(start) || math.IsNaN(end) || math.IsInf(start, 0) || math.IsInf(end, 0) || start < 0 || end <= start || (i > 0 && start < previousEnd) || (sc.Duration > 0 && end > sc.Duration+0.001) {
			return out
		}
		to := len(sc.Content)
		if i+1 < len(matches) {
			to = matches[i+1][0]
		}
		intervals = append(intervals, interval{start: start, end: end, from: match[1], to: to})
		previousEnd = end
	}

	type occurrence struct {
		text string
		span interval
	}
	var occurrences []occurrence
	for _, iv := range intervals {
		for _, item := range extractQuotedDialogue(sc.Content[iv.from:iv.to]) {
			occurrences = append(occurrences, occurrence{text: item.Text, span: iv})
		}
	}

	sourceByText := make(map[string][]interval)
	for _, occurrence := range occurrences {
		sourceByText[occurrence.text] = append(sourceByText[occurrence.text], occurrence.span)
	}
	recordsByText := make(map[string][]int)
	for i := range out {
		recordsByText[out[i].Text] = append(recordsByText[out[i].Text], i)
	}
	for text, recordIndexes := range recordsByText {
		source := sourceByText[text]
		// A count mismatch means exclusions, duplicate records, or non-source text;
		// none supplies enough evidence to choose a particular occurrence.
		if len(source) != len(recordIndexes) {
			continue
		}
		for occurrenceIndex, recordIndex := range recordIndexes {
			if _, _, valid := parseDialogueTimeSegment(out[recordIndex].TimeSegment); valid {
				continue
			}
			iv := source[occurrenceIndex]
			out[recordIndex].TimeSegment = strconv.FormatFloat(iv.start, 'f', -1, 64) + "—" + strconv.FormatFloat(iv.end, 'f', -1, 64) + "秒"
		}
	}
	return out
}
