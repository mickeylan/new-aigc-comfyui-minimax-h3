package service

import (
	"regexp"
	"strings"
)

var (
	quotedSoundEventPattern  = regexp.MustCompile(`^(?:只听|忽听|但听|听得|听见)?[^“”\n]{0,24}“[^”\n]+”(?:的)?(?:一声|一下|两声|数声|声响|声大作|作响|响起)`)       // a narrated event, rather than the spelling of its sound
	quotedTermPattern        = regexp.MustCompile(`“[^”\n]+”(?:二字|两字|三字|四字|这个词|一词|一语|字样|称呼|名号)`)                                        // prose discussing written/quoted terminology
	humanVoiceContextPattern = regexp.MustCompile(`(?:人|他|她|少年|少女|男子|女子|老者|众人|声音)[^。！？\n]{0,16}(?:说|道|叫|喊|喝|厉喝|轻咦|惊呼|怒吼|低语|问|答|笑|骂|叹)`) // positive evidence wins
)

// sourceQuoteIsSpeech is deliberately conservative: an unattributed or very short
// quote remains dialogue unless its paragraph positively describes a sound event or
// discusses the quoted text itself. quote contains byte offsets produced by regexp.
func sourceQuoteIsSpeech(content string, quote []int) bool {
	if len(quote) < 4 || quote[0] < 0 || quote[1] > len(content) || quote[0] >= quote[1] {
		return true
	}

	paragraphStart := strings.LastIndex(content[:quote[0]], "\n") + 1
	paragraphEnd := len(content)
	if n := strings.Index(content[quote[1]:], "\n"); n >= 0 {
		paragraphEnd = quote[1] + n
	}
	paragraph := content[paragraphStart:paragraphEnd]

	// Explicit human vocal action is stronger evidence than an onomatopoeic-looking
	// quote (for example, a person can say “啊” or lightly utter “咦”).
	if humanVoiceContextPattern.MatchString(paragraph) {
		return true
	}
	return !quotedSoundEventPattern.MatchString(paragraph) && !quotedTermPattern.MatchString(paragraph)
}
