package service

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"comfyui-console/internal/models"
)

const h3DialogueTimelineSkillCode = "scene_video_dialogue_timeline"

type h3DialoguePlan struct {
	Placements []h3DialoguePlanPlacement `json:"placements"`
}
type h3DialoguePlanPlacement struct {
	Index int    `json:"index"`
	Shot  int    `json:"shot"`
	After string `json:"after,omitempty"`
}

var h3PlanFence = regexp.MustCompile("(?s)^\\s*```(?:json)?\\s*(.*?)\\s*```\\s*$")

func parseH3DialoguePlan(raw string) (h3DialoguePlan, error) {
	if m := h3PlanFence.FindStringSubmatch(raw); len(m) == 2 {
		raw = m[1]
	}
	var plan h3DialoguePlan
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &plan); err != nil {
		return plan, fmt.Errorf("invalid JSON plan: %w", err)
	}
	return plan, nil
}

// composeH3DialogueTimeline lets the model make only the semantic placement
// decision. Rendering is deterministic, so accepted visual prose is never
// rewritten by the model.
func (s *ProjectService) composeH3DialogueTimeline(sc *models.Scene, visual string, dubs []models.Dialogue, referenceLines []string) (string, error) {
	valid := validSceneDialogues(dubs)
	if len(valid) == 0 {
		return visual, nil
	}
	diagnosis := DiagnoseH3DialoguePlacement(visual, valid, referenceLines)
	if len(diagnosis.Unplaced) == 0 && len(placedOccurrences(visual)) == len(valid) && !h3DialogueOnlyAtShotEnds(visual) {
		return visual, nil
	}
	if s == nil || s.textProvider == nil {
		return "", fmt.Errorf("文本生成服务未配置，AI未能完整安排对白")
	}

	markers := h3BareShotPattern.FindAllStringSubmatch(visual, -1)
	shots := make([]int, 0, len(markers))
	shotSet := map[int]bool{}
	for _, marker := range markers {
		n, _ := strconv.Atoi(marker[1])
		shots, shotSet[n] = append(shots, n), true
	}
	if len(shots) == 0 {
		return "", fmt.Errorf("AI未能完整安排对白: 视觉正文没有镜头标记")
	}

	known := map[int]int{}
	for _, p := range diagnosis.Placed {
		known[p.DialogueIndex] = p.ShotNumber
	}
	type quote struct {
		Index       int    `json:"index"`
		Order       int    `json:"order"`
		Character   string `json:"character"`
		SpeechType  string `json:"speech_type"`
		Text        string `json:"text"`
		TimeSegment string `json:"time_segment,omitempty"`
	}
	quotes := make([]quote, len(valid))
	for i, d := range valid {
		quotes[i] = quote{i, d.Order, d.Character, d.SpeechType, d.Text, d.TimeSegment}
	}
	content := sceneDialogueGenerationContent(sc)
	payload, _ := json.Marshal(map[string]any{
		"scene_content": content, "accepted_visual_timeline": visual,
		"actual_shot_numbers": shots, "quotes": quotes, "known_placements_must_preserve": known,
	})
	system := `Place each reviewed quote at its actual spoken beat, using scene_content and accepted_visual_timeline as planning context. Do not rewrite prose. Return only JSON: {"placements":[{"index":0,"shot":1,"after":"exact complete visual clause"}]}. "after" must be an exact, unique, complete substring from that shot's visual prose: the visual beat after which speech occurs and before subsequent action. Include every quote index exactly once in source order. Use only actual_shot_numbers. Preserve every known shot placement exactly.`

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		request := string(payload)
		if attempt > 0 {
			request += "\nPrevious plan invalid: " + lastErr.Error()
		}
		var raw string
		var err error
		if s.skills != nil {
			projectID := uint(0)
			if sc != nil {
				projectID = sc.ProjectID
			}
			raw, err = s.skills.ChatWithConfiguredOrFallbackSkill(projectID, models.SkillStageVideoPrompt, h3DialogueTimelineSkillCode, s.textProvider, system, request, map[string]string{"request": request})
		} else {
			raw, err = s.textProvider.Chat(system, request)
		}
		if err != nil {
			lastErr = err
			continue
		}
		plan, parseErr := parseH3DialoguePlan(raw)
		if parseErr != nil {
			lastErr = parseErr
			continue
		}
		if err = validateH3DialoguePlan(plan, valid, visual, shotSet, known); err != nil {
			lastErr = err
			continue
		}
		return applyH3DialoguePlan(visual, valid, referenceLines, plan), nil
	}
	return "", fmt.Errorf("AI未能完整安排对白: %v", lastErr)
}

func validateH3DialoguePlan(plan h3DialoguePlan, dubs []models.Dialogue, visual string, shotSet map[int]bool, known map[int]int) error {
	if len(plan.Placements) != len(dubs) {
		return fmt.Errorf("plan must contain exactly %d placements", len(dubs))
	}
	lastShot := -1
	_, starts := promptShotStarts(visual)
	markers := h3BareShotPattern.FindAllStringSubmatch(visual, -1)
	for position, p := range plan.Placements {
		if p.Index != position || p.Index < 0 || p.Index >= len(dubs) {
			return fmt.Errorf("indices must occur exactly once in source order")
		}
		if !shotSet[p.Shot] {
			return fmt.Errorf("unknown shot %d", p.Shot)
		}
		if p.Shot < lastShot {
			return fmt.Errorf("placements must be monotonic")
		}
		lastShot = p.Shot
		if shot, ok := known[p.Index]; ok && shot != p.Shot {
			return fmt.Errorf("known placement changed")
		}
		if p.After != "" {
			body := stripStructuredDialogueFromAction(visual, dubs)
			shotBody, ok := h3ShotBody(body, p.Shot)
			if !ok || strings.Count(shotBody, p.After) != 1 {
				return fmt.Errorf("quote %d after anchor must occur exactly once in shot %d", p.Index, p.Shot)
			}
		}
		if start, _, ok := parseDialogueTimeSegment(dubs[p.Index].TimeSegment); ok && len(starts) == len(markers) {
			i := sort.Search(len(starts), func(i int) bool { return starts[i] > start }) - 1
			if i < 0 || p.Shot != mustShotNumber(markers[i][1]) {
				return fmt.Errorf("quote %d conflicts with its explicit time segment", p.Index)
			}
		}
	}
	return nil
}

func mustShotNumber(s string) int { n, _ := strconv.Atoi(s); return n }

func renderH3PlannedDialogue(d models.Dialogue, id int, refs []string) string {
	speaker, text := h3DialogueSpeaker(d, id, refs), strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(d.Text, "<scenetrans>", ""), "<cutoff>", ""))
	var clause string
	switch d.SpeechType {
	case "narration":
		clause = speaker + " narrates: <d>[Chinese] " + text + "</d>."
	case "monologue":
		// Use the same audio-only/closed-mouth contract as direct compilation.
		// The planning pass must not downgrade thought audio into spoken dialogue.
		return renderStructuredDialogue(d, id, refs)
	case "offscreen_dialogue":
		clause = speaker + " says in a clearly identified off-screen voice: <d>[Chinese] " + text + "</d>."
	default:
		clause = speaker + " says: <d>[Chinese] " + text + "</d>."
	}
	if strings.Contains(d.Text, "<scenetrans>") {
		clause += "<scenetrans>"
	}
	if strings.Contains(d.Text, "<cutoff>") {
		clause += "<cutoff>"
	}
	return clause
}

func applyH3DialoguePlan(visual string, dubs []models.Dialogue, refs []string, plan h3DialoguePlan) string {
	body := stripStructuredDialogueFromAction(visual, dubs)
	ids := dialogueSpeakerIDs(dubs)
	type insertion struct {
		at, order int
		text      string
	}
	var inserts []insertion
	for order, p := range plan.Placements {
		clause := renderH3PlannedDialogue(dubs[p.Index], ids[p.Index], refs)
		start, end, ok := h3ShotBounds(body, p.Shot)
		if !ok {
			continue
		} // validated plans cannot reach this path
		at := end
		if p.After != "" {
			at = start + strings.Index(body[start:end], p.After) + len(p.After)
		} else {
			for at > start && strings.ContainsRune(" \t\r\n", rune(body[at-1])) {
				at--
			}
		}
		inserts = append(inserts, insertion{at, order, " " + clause})
	}
	sort.SliceStable(inserts, func(i, j int) bool {
		if inserts[i].at == inserts[j].at {
			return inserts[i].order < inserts[j].order
		}
		return inserts[i].at < inserts[j].at
	})
	var out strings.Builder
	last := 0
	for i := 0; i < len(inserts); {
		at := inserts[i].at
		out.WriteString(body[last:at])
		for i < len(inserts) && inserts[i].at == at {
			out.WriteString(inserts[i].text)
			i++
		}
		last = at
	}
	out.WriteString(body[last:])
	return out.String()
}

func h3ShotBounds(body string, shot int) (int, int, bool) {
	matches := h3BareShotPattern.FindAllStringSubmatchIndex(body, -1)
	for i, m := range matches {
		n, _ := strconv.Atoi(body[m[2]:m[3]])
		if n == shot {
			end := len(body)
			if i+1 < len(matches) {
				end = matches[i+1][0]
			}
			return m[0], end, true
		}
	}
	return 0, 0, false
}

func h3ShotBody(body string, shot int) (string, bool) {
	start, end, ok := h3ShotBounds(body, shot)
	if !ok {
		return "", false
	}
	return body[start:end], true
}

// A fully reconciled timeline is normally left untouched. The exception is the
// old renderer's tell-tale form: every dialogue tag is the final content in its shot.
func h3DialogueOnlyAtShotEnds(body string) bool {
	occ := placedOccurrences(body)
	if len(occ) == 0 {
		return false
	}
	for _, o := range occ {
		_, end, ok := h3ShotBounds(body, o.shot)
		if !ok {
			return false
		}
		tagEnd := strings.Index(body[o.position:end], "</d>")
		if tagEnd < 0 {
			return false
		}
		tail := strings.Trim(body[o.position+tagEnd+len("</d>"):end], " \t\r\n.!?。；;<scenetrans><cutoff>")
		if tail != "" {
			return false
		}
	}
	return true
}
