package service

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"comfyui-console/internal/models"
)

var h3BriefHanOutsideNamePattern = regexp.MustCompile(`[\p{Han}]`)
var h3BriefQuotedNamePattern = regexp.MustCompile(`[「（(][^」）)]*[」）)]`)

type h3DirectorBrief struct {
	Summary   string   `json:"summary"`
	Retention []string `json:"retention"`
}

// applyH3DirectorBrief lets the model write the Ref2V summary and add
// continuity notes (screen direction, effect ownership, final state) from the
// accepted shot timeline. The template summary/retention stay authoritative
// fallbacks: any call, parse or validation failure leaves the prompt unchanged.
func (s *ProjectService) applyH3DirectorBrief(sc *models.Scene, prompt string) string {
	if s == nil || s.textProvider == nil || sc == nil || !strings.Contains(prompt, "summary:") || !strings.Contains(prompt, "retention_analysis:") {
		return prompt
	}
	definitions := h3PromptSection(prompt, "subject_definitions:")
	detail := h3PromptSection(prompt, "detailed_description:")
	if detail == "" {
		return prompt
	}
	system := `你是 MiniMax H3 Ref2V 视频导演。只输出 JSON 对象：{"summary":"...","retention":["..."]}。
summary：一段英文，以 "[reference generation]" 开头，用2至4句概括本段视频的起势、核心动作因果和结束画面，并说明时长与连续性；只能概括 detailed_description 已写出的事实，不得新增人物、道具、术法、胜负或伤害。
retention：2至5条英文，只写 detailed_description 已体现的连续性约束：人物画面左右位置与朝向（轴线不变）、道具始终在谁手中、光效/术法只从谁的哪个部位发出、结尾停在什么状态。
人物只能写 subject_definitions 中已有的 <Subject N>，不得写中文名、拼音或 <Picture N>；不得出现台词、<d> 或中文。`
	user := fmt.Sprintf("target_duration_seconds: %.0f\n\nsubject_definitions:\n%s\n\ndetailed_description:\n%s", normalizeSceneDuration(sc.Duration), definitions, detail)
	raw, err := s.textProvider.Chat(system, user)
	if err != nil {
		return prompt
	}
	var brief h3DirectorBrief
	if json.Unmarshal([]byte(extractJSON(raw)), &brief) != nil {
		return prompt
	}
	summary, notes, ok := validateH3DirectorBrief(brief, definitions)
	if !ok {
		return prompt
	}
	prompt = replaceH3PromptSection(prompt, "summary:", summary)
	retention := h3PromptSection(prompt, "retention_analysis:")
	return replaceH3PromptSection(prompt, "retention_analysis:", strings.TrimSpace(retention+"\n"+strings.Join(notes, "\n")))
}

func validateH3DirectorBrief(brief h3DirectorBrief, definitions string) (string, []string, bool) {
	summary := strings.Join(strings.Fields(brief.Summary), " ")
	if !strings.HasPrefix(summary, "[reference generation]") {
		summary = strings.TrimSpace("[reference generation] " + summary)
	}
	if len(summary) < len("[reference generation]")+40 || len(summary) > 1200 || !h3BriefTextAllowed(summary, definitions) {
		return "", nil, false
	}
	notes := make([]string, 0, len(brief.Retention))
	for _, note := range brief.Retention {
		note = strings.Join(strings.Fields(note), " ")
		if note == "" {
			continue
		}
		if len(note) > 400 || !h3BriefTextAllowed(note, definitions) {
			return "", nil, false
		}
		notes = append(notes, note)
	}
	if len(notes) > 5 {
		notes = notes[:5]
	}
	return summary, notes, true
}

func h3BriefTextAllowed(text, definitions string) bool {
	if strings.Contains(text, "<d>") || strings.Contains(text, "<Picture") {
		return false
	}
	if h3BriefHanOutsideNamePattern.MatchString(h3BriefQuotedNamePattern.ReplaceAllString(text, "")) {
		return false
	}
	for _, tag := range h3SubjectTagPattern.FindAllString(text, -1) {
		if !strings.Contains(definitions, tag) {
			return false
		}
	}
	return true
}

func replaceH3PromptSection(prompt, heading, body string) string {
	start := strings.Index(prompt, heading)
	if start < 0 {
		return prompt
	}
	contentStart := start + len(heading)
	end := len(prompt)
	for _, next := range []string{"subject_definitions:", "summary:", "retention_analysis:", "detailed_description:", "overall_soundscape:", "non_diegetic_music:"} {
		if i := strings.Index(prompt[contentStart:], next); i >= 0 && contentStart+i < end {
			end = contentStart + i
		}
	}
	tail := prompt[end:]
	if tail != "" {
		tail = "\n\n" + strings.TrimLeft(tail, "\n")
	}
	return prompt[:contentStart] + "\n" + strings.TrimSpace(body) + tail
}
