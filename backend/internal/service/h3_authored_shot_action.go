package service

import (
	"comfyui-console/internal/models"
	"fmt"
	"strings"
)

// Explicitly authored shots are already a director plan. Translate them rather
// than run them through the free-form action designer and its repair templates.
func (s *ProjectService) compileAuthoredH3ShotAction(sc *models.Scene, source string, refs []string) (string, error) {
	system := `Translate the author's already planned Chinese shots into executable English H3 prose. This is a translation task, NOT a director-design, polishing or summarization task.
Preserve every [Shot N] once, in the same order. Preserve shot size, camera-relative facing, who stands on whose side, object ownership, gestures, gaze, and the exact sequence of visible actions. A named person's right side is not the other person's right side. Facing another person does not mean facing the camera. Back to camera must remain back to camera.
Preserve the scope of every negation and observation: an effect that is visible and a property that was not detected are different facts. Do not erase the visible effect to express the negative detection result. Do not invent visual energy, movement, facial expressions, positions, reactions or new results to illustrate an invisible observation.
Keep factual observations as observations, not fabricated visual effects. Omit quoted speech text and <d> blocks ONLY because the system separately inserts the exact reviewed speech. Retain the distinction between inner monologue (audible inner voice, lips closed in that interval) and on-screen dialogue (normal speaking in that interval). Do not replace an inner voice with visible speaking.
Copy Chinese character names verbatim for deterministic Subject binding. Translate all other visual prose into natural English. References identify assets only; never derive story or staging from them. Do not add a reference-use preamble, general tempo rules, timing, new cuts or a summary. Output only the translated shot paragraphs.`
	request := "AUTHORITATIVE AUTHORED SHOTS:\n" + source
	expected := h3ShotMarkerPattern.FindAllStringSubmatch(source, -1)
	for attempt := 0; attempt < 2; attempt++ {
		out, err := s.textProvider.Chat(system, request)
		if err != nil {
			return "", fmt.Errorf("转换原文镜头提示词失败: %w", err)
		}
		out = stripH3CodeFence(out)
		actual := h3ShotMarkerPattern.FindAllStringSubmatch(out, -1)
		complete := len(actual) == len(expected)
		if complete {
			for i := range expected {
				if expected[i][1] != actual[i][1] {
					complete = false
					break
				}
			}
		}
		if complete {
			return useSubjectTags(applyAuthoritativeSceneShotTimeline(strings.TrimSpace(out), source), refs), nil
		}
		request = "The previous translation omitted or reordered authored Shot markers. Translate ALL original shots below, in order, without designing replacement content.\n" + source
	}
	// A malformed translation is not authority to delete authored shots or block
	// manual editing: retain the source plan for the user's review instead.
	return useSubjectTags(applyAuthoritativeSceneShotTimeline(source, source), refs), nil
}
