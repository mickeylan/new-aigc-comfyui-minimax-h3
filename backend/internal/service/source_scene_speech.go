package service

import "strings"

// sourceParagraphBounds returns the containing source paragraph without changing
// any source bytes. Offsets use regexp's byte coordinates.
func sourceParagraphBounds(content string, start, end int) (int, int) {
	paragraphStart := strings.LastIndex(content[:start], "\n") + 1
	paragraphEnd := len(content)
	if n := strings.Index(content[end:], "\n"); n >= 0 {
		paragraphEnd = end + n
	}
	return paragraphStart, paragraphEnd
}

func skipParagraphBreaks(content string, at int) int {
	for at < len(content) && (content[at] == '\n' || content[at] == '\r') {
		at++
	}
	return at
}
