package service

import (
	"comfyui-console/internal/models"
	"testing"
)

func TestLowVoiceSpeechRemainsVisibleDialogue(t *testing.T) {
	sc := &models.Scene{Characters: "马脸男子,柳石"}
	sc.Content = "4—7秒｜马脸男子僵在原地，伸出的右手悬在柳石颈侧前，瞳孔放大，难以置信地盯着自己的手，低声道：“不可能”。他右掌翻转，袖底黑色铁尺悄然滑入掌中。柳石整个人依旧被黑色大网紧裹。"
	got := explicitSceneSpeech(sc)
	if len(got) != 1 || got[0].Character != "马脸男子" || got[0].SpeechType != "dialogue" || got[0].Text != "不可能" {
		t.Fatalf("visible low-volume speech lost or misclassified: %+v", got)
	}
}
