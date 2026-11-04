package service

import (
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

func TestSourceQuoteIsSpeechUsesNarrativeContext(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{"heard single sound", `黑暗中，只听“喀”的一声，门闩断了。`, false},
		{"explosive sound", `紧接着“轰”的一声，石门四分五裂。`, false},
		{"hissing noise", `耳畔忽有“嗤嗤”声响，白烟冒了出来。`, false},
		{"facial change", `他的脸色“唰”的一下白了。`, false},
		{"wailing noise", `洞外“呜呜”声大作，阴风卷入殿中。`, false},
		{"two impacts", `院门外传来“砰砰”两声，尘土簌簌落下。`, false},
		{"written term", `石碑上刻着“禁地”二字。`, false},
		{"light interjection", `少女轻咦道：“咦？”`, true},
		{"exclamation", `那人惊呼：“啊！”`, true},
		{"human cry", `少年叫道：“哎哟！”`, true},
		{"unknown command", `“追！”`, true},
		{"standalone sound-like utterance before human context", "“喀啦”\n\n下一段那人厉喝道：“站住！”", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			quote := sourceQuotePattern.FindStringSubmatchIndex(tt.content)
			if quote == nil {
				t.Fatal("fixture contains no quote")
			}
			if got := sourceQuoteIsSpeech(tt.content, quote); got != tt.want {
				t.Fatalf("sourceQuoteIsSpeech(%q) = %v, want %v", tt.content, got, tt.want)
			}
		})
	}
}

func TestExtractSourceDialoguesChapterFixtureSkipsOnlySevenClearNonSpeechQuotes(t *testing.T) {
	speech := []string{
		`“追！”`, `他道：“咦？”`, `她惊呼：“啊！”`, `少年叫道：“哎哟！”`,
		`“你是谁？”`, `“我不知道。”`, `“快走！”`, `“等等。”`, `“当心！”`,
		`“是你？”`, `“不错。”`, `“不可能！”`, `“为什么？”`, `“别怕。”`,
		`“跟我来。”`, `“住手！”`, `“多谢。”`, `“不用。”`, `“天亮了。”`,
		`“有人来了。”`, `“退后。”`, `“让我看看。”`, `“原来如此。”`,
		`“你听见了吗？”`, `“听见了。”`, `“我们走。”`, `“喀啦”`,
	}
	effects := []string{
		`只听“喀”的一声，锁开了。`, `忽然“砰砰”两声，门板震动。`,
		`风中“呜呜”声大作。`, `火星发出“嗤嗤”声响。`,
		`他的脸色“唰”的一下白了。`, `远处“轰”的一声，山石崩落。`,
		`机关转动，“喀啦”一声合拢。`,
	}
	content := strings.Join(append(speech, effects...), "\n\n")
	all := sourceQuotePattern.FindAllStringSubmatchIndex(content, -1)
	if len(all) != 34 {
		t.Fatalf("fixture has %d quotes, want 34", len(all))
	}

	got := extractSourceDialogues(models.Chapter{ID: 1, Order: 1, Content: content})
	if len(got) != 27 {
		t.Fatalf("extracted %d source dialogues, want 27", len(got))
	}
	if got[0].Text != "追！" || got[0].Speaker != "" {
		t.Fatalf("unattributed speech changed or guessed: %+v", got[0])
	}
	if got[len(got)-1].Text != "喀啦" {
		t.Fatalf("standalone ambiguous quote should remain: %+v", got[len(got)-1])
	}
}
