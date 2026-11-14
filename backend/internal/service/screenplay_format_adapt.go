package service

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"comfyui-console/internal/models"
	"comfyui-console/internal/screenplay"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	screenplayFormatAdaptSkillCode = "screenplay-format-adapt"
	// An external script is sent to the text model in one call, so it must fit its context.
	maxScreenplayAdaptRunes       = 50000
	screenplayAdaptConflictMarker = "【格式冲突】"
	// Only these characters are format shell: dialogue quotes, the colon after a speaker and
	// the brackets around a delivery note. Everything else counts as content.
	screenplayAdaptShellRunes  = "\"'“”‘’「」『』：:（）()"
	screenplayAdaptTerminators = "。！？!?；;…"
	maxScreenplayAdaptIssues   = 50
)

// screenplayFormatAdaptSkill only rewrites the format shell of an external script. Its wording
// is user-approved; raise Version whenever the prompt changes so InitSystemSkills publishes it.
var screenplayFormatAdaptSkill = models.Skill{
	Name: "剧本格式整理", Code: screenplayFormatAdaptSkillCode, Operation: screenplayFormatAdaptSkillCode, Version: 1,
	Description:    "把外部剧本整理成可导入格式，只改格式外壳，原文每句话原样保留且只出现一次",
	Stage:          models.SkillStageStoryboard,
	PromptTemplate: "外部剧本原文：{{source_text}}",
	SystemPrompt:   "你是剧本格式整理员。把用户给的外部剧本整理成下面的导入格式，只改格式外壳（标题行、场次标记、人物与台词的排版、动作行的位置），不改任何内容。原文的每一句话都必须原样出现在结果中且只出现一次：不增删字词、不改标点、不合并或拆分句子、不调整顺序、不翻译、不润色。原文缺少的格式要素（如场次地点、时间）留空，不要猜。导入格式：{{format_spec}}。先输出整理后的剧本，再另起一行输出「【格式冲突】」，逐条列出放不进格式或归属不确定的原文句子，没有就写「无」。不要输出解释或思考过程。",
	IsSystem:       true, Enabled: true, SortOrder: 21,
}

// screenplayImportFormatSpec describes the Fountain subset screenplay.ParseFountain reads. A
// non-blank line right after a line of Chinese text is read as dialogue, so every element
// must be one line followed by a blank line.
const screenplayImportFormatSpec = "Fountain纯文本，元素之间空一行；场次标题单独一行，以英文句点开头，后接原文的场次标记（如“.第1场 洞府 夜”），原文完全没有场次标记时只在开头写一行“.场景1”；每段动作描写写成一行；对白先单独一行写人物名（不加冒号），下一行写台词；台词前的语气说明写在人物名的下一行并用英文括号括起，如“(低声)”；转场单独一行，以“>”开头；除场次标题外，任何行都不得以英文句点开头"

// screenplayAdaptIssue is one original sentence whose occurrence count changed.
type screenplayAdaptIssue struct {
	Sentence string `json:"sentence"`
	Expected int    `json:"expected"`
	Actual   int    `json:"actual"`
}

type screenplayAdaptResult struct {
	Preview   screenplay.Preview
	Script    string
	Conflicts []string
	Warnings  []string
	Issues    []screenplayAdaptIssue
}

type screenplayAdaptResponse struct {
	screenplayPreviewResponse
	Conflicts []string `json:"conflicts"`
	Warnings  []string `json:"warnings"`
	Adapted   bool     `json:"adapted"`
}

func screenplayAdaptKey(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || strings.ContainsRune(screenplayAdaptShellRunes, r) {
			return -1
		}
		return r
	}, text)
}

// splitScreenplaySentences cuts after sentence-ending punctuation; a run of terminators such as
// "……" or "？！" stays with its sentence.
func splitScreenplaySentences(line string) []string {
	runes := []rune(line)
	var out []string
	start := 0
	for i, r := range runes {
		if !strings.ContainsRune(screenplayAdaptTerminators, r) {
			continue
		}
		if i+1 < len(runes) && strings.ContainsRune(screenplayAdaptTerminators, runes[i+1]) {
			continue
		}
		out = append(out, string(runes[start:i+1]))
		start = i + 1
	}
	if start < len(runes) {
		out = append(out, string(runes[start:]))
	}
	return out
}

// stripScreenplaySpeakerLabel removes a leading "人物：" or a line holding only a speaker name,
// because the importer moves that name onto its own character line. A delivery note in the
// label stays, since the format keeps it as a parenthetical.
func stripScreenplaySpeakerLabel(line string, names map[string]bool) string {
	isName := func(text string) bool {
		key := supplementSpeechKey(text)
		return key != "" && names[key]
	}
	head, rest, hasColon := line, "", false
	if i := strings.IndexAny(line, "：:"); i >= 0 {
		_, width := utf8.DecodeRuneInString(line[i:])
		head, rest, hasColon = line[:i], line[i+width:], true
	}
	name, note := head, ""
	if j := strings.IndexAny(head, "（("); j >= 0 {
		name, note = head[:j], head[j:]
	}
	if hasColon {
		if isName(name) {
			return note + rest
		}
		return line
	}
	trimmed := strings.TrimSpace(line)
	if isName(trimmed) {
		return ""
	}
	if note != "" && (strings.HasSuffix(trimmed, "）") || strings.HasSuffix(trimmed, ")")) && isName(name) {
		return note
	}
	return line
}

// The importer's fallback heading when the source has no scene marker.
var screenplayFallbackHeading = regexp.MustCompile(`^(场景|SCENE ?)\d+$`)

// checkScreenplayAdaptation is the deterministic content freeze: once the format shell is
// removed, each original sentence must occur in the parsed result exactly as often as in the
// source. Text or speakers found only in the result are reported as warnings.
func checkScreenplayAdaptation(source string, preview screenplay.Preview) ([]screenplayAdaptIssue, []string) {
	names := map[string]bool{}
	var nameList []string
	var out strings.Builder
	out.WriteString(screenplayAdaptKey(preview.Title))
	for _, scene := range preview.Scenes {
		if !screenplayFallbackHeading.MatchString(scene.Heading) {
			out.WriteString(screenplayAdaptKey(scene.Heading))
		}
		for _, element := range scene.Elements {
			if element.Type == "character" {
				if key := supplementSpeechKey(element.Text); key != "" && !names[key] {
					names[key] = true
					nameList = append(nameList, strings.TrimSpace(element.Text))
				}
				continue
			}
			out.WriteString(screenplayAdaptKey(element.Text))
		}
	}
	outStream := out.String()
	titleKey := supplementSpeechKey(preview.Title)

	var sentences []string
	var src strings.Builder
	for _, line := range strings.Split(strings.ReplaceAll(source, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "\ufeff"))
		if line == "" {
			continue
		}
		if i := strings.IndexAny(line, "：:"); i >= 0 && titleKey != "" {
			_, width := utf8.DecodeRuneInString(line[i:])
			if supplementSpeechKey(line[i+width:]) == titleKey {
				line = line[i+width:]
			}
		}
		for _, sentence := range splitScreenplaySentences(stripScreenplaySpeakerLabel(line, names)) {
			if key := screenplayAdaptKey(sentence); key != "" {
				sentences = append(sentences, key)
				src.WriteString(key)
			}
		}
	}
	srcStream := src.String()

	var issues []screenplayAdaptIssue
	checked := map[string]bool{}
	for _, key := range sentences {
		if checked[key] {
			continue
		}
		checked[key] = true
		// Counting the same way on both sides keeps short sentences that also occur inside
		// longer ones (such as "好。" in "很好。") from being reported as duplicates.
		want, got := strings.Count(srcStream, key), strings.Count(outStream, key)
		if want != got && len(issues) < maxScreenplayAdaptIssues {
			issues = append(issues, screenplayAdaptIssue{Sentence: key, Expected: want, Actual: got})
		}
	}
	var warnings []string
	sourceKey := supplementSpeechKey(source)
	for _, name := range nameList {
		if !strings.Contains(sourceKey, supplementSpeechKey(name)) {
			warnings = append(warnings, fmt.Sprintf("说话人「%s」在原文里没有出现，请确认台词归属", name))
		}
	}
	for _, scene := range preview.Scenes {
		if key := screenplayAdaptKey(scene.Heading); !screenplayFallbackHeading.MatchString(scene.Heading) && key != "" && !strings.Contains(srcStream, key) {
			warnings = append(warnings, fmt.Sprintf("原文里找不到这个场次标题：「%s」", truncateRunes(scene.Heading, 40)))
		}
		for _, element := range scene.Elements {
			if element.Type == "character" {
				continue
			}
			if key := screenplayAdaptKey(element.Text); key != "" && !strings.Contains(srcStream, key) {
				warnings = append(warnings, fmt.Sprintf("原文里找不到这段文字：「%s」", truncateRunes(strings.TrimSpace(element.Text), 40)))
			}
		}
	}
	return issues, warnings
}

func truncateRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "…"
}

// splitScreenplayAdaptOutput separates the adapted script from the trailing conflict list.
func splitScreenplayAdaptOutput(output string) (string, []string) {
	output = strings.TrimSpace(stripThinkBlocks(output))
	if strings.HasPrefix(output, "```") {
		if i := strings.Index(output, "\n"); i >= 0 {
			output = output[i+1:]
		}
		output = strings.TrimSuffix(strings.TrimSpace(output), "```")
	}
	script, tail := output, ""
	if i := strings.LastIndex(output, screenplayAdaptConflictMarker); i >= 0 {
		script, tail = output[:i], output[i+len(screenplayAdaptConflictMarker):]
	}
	script = strings.TrimRight(script, " \t\r\n「")
	conflicts := []string{}
	for _, line := range strings.Split(tail, "\n") {
		line = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "」：:-*•·0123456789.、 "))
		if line != "" && line != "无" && line != "「无」" {
			conflicts = append(conflicts, line)
		}
	}
	return script, conflicts
}

func adaptScreenplay(source, output string) (screenplayAdaptResult, error) {
	script, conflicts := splitScreenplayAdaptOutput(output)
	result := screenplayAdaptResult{Script: script, Conflicts: conflicts}
	preview, err := screenplay.ParseFountain(script)
	if err != nil {
		return result, err
	}
	if err := validateScreenplayPreview(preview); err != nil {
		return result, err
	}
	result.Preview = preview
	result.Issues, result.Warnings = checkScreenplayAdaptation(source, preview)
	return result, nil
}

// HandleAdaptScreenplayImport asks the format Skill to reshape an external script into the
// import format and returns a preview only when every original sentence survived. Nothing is
// persisted; the user still confirms the preview through the regular import apply.
func (s *Service) HandleAdaptScreenplayImport(c *gin.Context) {
	project, ok := s.loadProject(c)
	if !ok {
		return
	}
	number, ok := episodeNumberParam(c)
	if !ok {
		return
	}
	if _, err := loadOwnedEpisode(s.DB, project.ID, number); errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "episode not found"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	_, text, err := readScreenplayInput(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	text = strings.TrimSpace(text)
	if text == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "剧本内容为空"})
		return
	}
	if utf8.RuneCountInString(text) > maxScreenplayAdaptRunes {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("剧本超过%d字，请分段整理或直接导入", maxScreenplayAdaptRunes)})
		return
	}
	if s.Skills == nil || s.TextProviderFact == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "AI 文本服务未配置"})
		return
	}
	output, err := s.Skills.ChatWithConfiguredOrFallbackSkill(project.ID, screenplayFormatAdaptSkill.Stage, screenplayFormatAdaptSkillCode, s.TextProviderFact, "", "", map[string]string{
		"source_text": text,
		"format_spec": screenplayImportFormatSpec,
	})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "剧本格式整理失败: " + err.Error()})
		return
	}
	result, err := adaptScreenplay(text, output)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "整理结果无法解析：" + err.Error(), "adapted_text": result.Script, "conflicts": result.Conflicts})
		return
	}
	if len(result.Issues) > 0 {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "整理结果改动了原文内容，已拒绝导入", "issues": result.Issues, "conflicts": result.Conflicts, "warnings": result.Warnings, "adapted_text": result.Script})
		return
	}
	preview := result.Preview
	c.JSON(http.StatusOK, screenplayAdaptResponse{
		screenplayPreviewResponse: screenplayPreviewResponse{Preview: preview, SceneCount: len(preview.Scenes), ElementCount: preview.ElementCount()},
		Conflicts:                 result.Conflicts, Warnings: result.Warnings, Adapted: true,
	})
}
