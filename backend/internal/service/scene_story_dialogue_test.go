package service

import (
	"strings"
	"testing"
)

func TestSceneStoryContentWithDialogues(t *testing.T) {
	tests := []struct {
		name, content, want string
		dialogues           []scriptDialogue
	}{
		{"action and two dialogues", "【动作】他推开门。", "【动作】他推开门。\n【对白｜甲】你好！\n【旁白】天亮了。", []scriptDialogue{{Character: "甲", SpeechType: "dialogue", Text: "你好！"}, {SpeechType: "narration", Text: "天亮了。"}}},
		{"standard text already", "【动作】抬头。\n【对白｜甲】好", "【动作】抬头。\n【对白｜甲】好", []scriptDialogue{{Character: "甲", SpeechType: "dialogue", Text: "好"}}},
		{"quote already", "甲说：“好”。", "甲说：“好”。", []scriptDialogue{{Character: "甲", SpeechType: "dialogue", Text: "好"}}},
		{"repeated", "【动作】等待。\n【对白｜甲】好", "【动作】等待。\n【对白｜甲】好\n【对白｜甲】好", []scriptDialogue{{Character: "甲", Text: "好"}, {Character: "甲", Text: "好"}}},
		{"types and unknown", "动作", "动作\n【内心独白｜甲】别怕。\n【画外对白｜乙】开门。\n【对白｜待确认】是谁？", []scriptDialogue{{Character: "甲", SpeechType: "monologue", Text: "别怕。"}, {Character: "乙", SpeechType: "offscreen_dialogue", Text: "开门。"}, {Character: "unknownspeaker", SpeechType: "dialogue", Text: "是谁？"}}},
		{"empty", "【动作】静默。", "【动作】静默。", []scriptDialogue{{Character: "甲", Text: "  "}}},
		{"substring is not speech", "【动作】大家要好好干。", "【动作】大家要好好干。\n【对白｜甲】好", []scriptDialogue{{Character: "甲", Text: "好"}}},
		{"same text different types", "【旁白】走吧", "【旁白】走吧\n【内心独白｜甲】走吧", []scriptDialogue{{Character: "甲", SpeechType: "monologue", Text: "走吧"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sceneStoryContentWithDialogues(tt.content, tt.dialogues)
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
			if twice := sceneStoryContentWithDialogues(got, tt.dialogues); twice != got {
				t.Fatalf("not idempotent: %q", twice)
			}
			for _, d := range tt.dialogues {
				if strings.TrimSpace(d.Text) != "" && !strings.Contains(got, d.Text) {
					t.Fatalf("missing original dialogue bytes %q", d.Text)
				}
			}
		})
	}
}
