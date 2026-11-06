package service

import (
	"strconv"
	"strings"
)

// h3SourceEventLedger splits authored scene prose into ordered clauses without
// treating punctuation inside Chinese quotation marks as event boundaries.
// It is intentionally lexical: entries are evidence for the model, not a shot plan.
func h3SourceEventLedger(source string) []string {
	var entries []string
	var clause strings.Builder
	quoteDepth := 0
	flush := func() {
		if text := strings.TrimSpace(clause.String()); text != "" {
			entries = append(entries, text)
		}
		clause.Reset()
	}
	for _, r := range source {
		switch r {
		case '“':
			quoteDepth++
		case '”':
			if quoteDepth > 0 {
				quoteDepth--
			}
		}
		clause.WriteRune(r)
		if quoteDepth == 0 {
			switch r {
			case '。', '！', '？', '；', '\n', '\r':
				flush()
			}
		}
	}
	flush()
	return entries
}

func formatH3SourceEventLedger(source string) string {
	entries := h3SourceEventLedger(source)
	if len(entries) == 0 {
		return "（当前正文为空）"
	}
	var out strings.Builder
	for i, entry := range entries {
		if i > 0 {
			out.WriteByte('\n')
		}
		out.WriteString("EVENT ")
		out.WriteString(strconv.Itoa(i + 1))
		out.WriteString(": ")
		out.WriteString(entry)
	}
	return out.String()
}
