package service

import (
	"regexp"
	"strings"
)

var sceneStoryStandardSpeechRE = regexp.MustCompile(`(?m)【(对白|旁白|内心独白|画外对白)(?:[｜|]([^】]*))?】([^\r\n]*)`)
var sceneStoryNaturalSpeechRE = regexp.MustCompile(`(?m)^\s*([^\r\n：:]{1,40}?)(?:说道|说|问道|答道|喊道|喝道|道)[：:]([^\r\n]*)$`)
var sceneStoryQuotedSpeechRE = regexp.MustCompile(`[“"]([^”"\r\n]+)[”"]|[‘']([^’'\r\n]+)[’']`)

// sceneStoryContentWithDialogues keeps the authored scene text intact and adds
// any structured dialogue which is not already explicitly present in it.
func sceneStoryContentWithDialogues(content string, dialogues []scriptDialogue) string {
	type speechKey struct{ kind, character, text string }
	existing := make(map[speechKey]int)
	for _, match := range sceneStoryStandardSpeechRE.FindAllStringSubmatch(content, -1) {
		character := match[2]
		if match[1] == "旁白" {
			character = ""
		}
		existing[speechKey{match[1], character, match[3]}]++
	}
	for _, match := range sceneStoryNaturalSpeechRE.FindAllStringSubmatch(content, -1) {
		text := match[2]
		if quotes := sceneStoryQuotedSpeechRE.FindStringSubmatch(text); len(quotes) > 0 {
			text = quotes[1]
			if text == "" {
				text = quotes[2]
			}
		}
		existing[speechKey{"对白", match[1], text}]++
	}
	quoted := make(map[string]int)
	// A quote within a labelled speech line is the same occurrence, not a
	// second credit for another identical structured line.
	unlabelled := sceneStoryStandardSpeechRE.ReplaceAllString(content, "")
	unlabelled = sceneStoryNaturalSpeechRE.ReplaceAllString(unlabelled, "")
	for _, match := range sceneStoryQuotedSpeechRE.FindAllStringSubmatch(unlabelled, -1) {
		text := match[1]
		if text == "" {
			text = match[2]
		}
		quoted[text]++
	}

	var additions []string
	for _, dialogue := range dialogues {
		if strings.TrimSpace(dialogue.Text) == "" {
			continue
		}
		kind, character := sceneStoryDialogueLabel(dialogue)
		key := speechKey{kind, character, dialogue.Text}
		if existing[key] > 0 {
			existing[key]--
			continue
		}
		if quoted[dialogue.Text] > 0 {
			quoted[dialogue.Text]--
			continue
		}
		label := "【" + kind + "】"
		if kind != "旁白" {
			label = "【" + kind + "｜" + character + "】"
		}
		additions = append(additions, label+dialogue.Text)
	}
	if len(additions) == 0 {
		return content
	}
	separator := ""
	if content != "" && !strings.HasSuffix(content, "\n") && !strings.HasSuffix(content, "\r") {
		separator = "\n"
	}
	return content + separator + strings.Join(additions, "\n")
}

func sceneStoryDialogueLabel(dialogue scriptDialogue) (kind, character string) {
	character = dialogue.Character
	switch strings.ToLower(strings.TrimSpace(dialogue.SpeechType)) {
	case "narration", "narrator", "旁白":
		return "旁白", ""
	case "monologue", "inner_monologue", "内心独白":
		kind = "内心独白"
	case "offscreen_dialogue", "offscreen", "画外对白":
		kind = "画外对白"
	default:
		kind = "对白"
	}
	if strings.TrimSpace(character) == "" || strings.EqualFold(strings.TrimSpace(character), "unknownspeaker") || strings.EqualFold(strings.TrimSpace(character), "unknown") {
		character = "待确认"
	}
	return kind, character
}
