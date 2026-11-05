package service

import (
	"strings"
	"testing"
)

func TestNormalizeSceneTimedBeatsBracketedFourAndSingleFull(t *testing.T) {
	four := "【0—2秒】甲。\n【2—4秒】乙。\n【4—6秒】丙。\n【6—8秒】丁。"
	got, count := normalizeSceneTimedBeats(four, 8)
	if count != 4 {
		t.Fatalf("four bracketed intervals count=%d: %s", count, got)
	}
	for _, want := range []string{"[Shot 1] 甲。", "[Shot 2] At 00:02.000, 乙。", "[Shot 4] At 00:06.000, 丁。"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %s", want, got)
		}
	}

	single, count := normalizeSceneTimedBeats("【0—3秒】甲说：“原话。”", 3)
	if count != 1 || single != "[Shot 1] 甲说：“原话。”" {
		t.Fatalf("single full interval count=%d: %s", count, single)
	}
}

func TestNormalizeSceneTimedBeatsRejectsMissingFullAuthority(t *testing.T) {
	content := "【0—2秒】第一段。\n后续无时间标记。"
	got, count := normalizeSceneTimedBeats(content, 5)
	if count != 0 || got != content {
		t.Fatalf("partial single interval changed count=%d: %s", count, got)
	}
}

func TestNormalizeSceneTimedBeatsDoesNotTouchDoubledShotTextOrDialogue(t *testing.T) {
	content := "0—2秒｜甲说：“[[Shot 9]]别动。”\n2—4秒｜乙回答：“好。”"
	got, count := normalizeSceneTimedBeats(content, 4)
	if count != 2 || !strings.Contains(got, "“[[Shot 9]]别动。”") || !strings.Contains(got, "“好。”") {
		t.Fatalf("dialogue changed count=%d: %s", count, got)
	}
}
