package service

import "testing"

func TestSourceParagraphBoundsKeepsSourceAttribution(t *testing.T) {
	text := "前一段。\n“原句！”男子喝道。\n女童闻声回头。"
	start, end := sourceParagraphBounds(text, len("前一段。\n"), len("前一段。\n“原句！”"))
	if got := text[start:end]; got != "“原句！”男子喝道。" {
		t.Fatal(got)
	}
	next := skipParagraphBreaks(text, end)
	_, nextEnd := sourceParagraphBounds(text, next, next)
	if text[next:nextEnd] != "女童闻声回头。" {
		t.Fatal("lost reaction context")
	}
}
