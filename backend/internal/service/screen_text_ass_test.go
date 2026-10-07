package service

import (
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

func TestBuildScreenTextASSUsesSceneOffsetsAndVerticalChinese(t *testing.T) {
	a := models.Scene{ID: 10, Duration: 5}
	b := models.Scene{ID: 20, Duration: 6}
	cues := []models.ScreenTextCue{
		{ID: 1, SceneID: &b.ID, Text: "上官若琳", Subtext: "玉霄宫主", StartTime: 1, EndTime: 3, WritingMode: "vertical-rl", Anchor: "top_right", Animation: "fade", Enabled: true, ReviewStatus: "approved"},
		{ID: 2, Text: `三日后{禁}`, StartTime: 0.5, EndTime: 1.5, WritingMode: "horizontal-ltr", Anchor: "center", Enabled: true, ReviewStatus: "approved"},
	}
	data, count := buildScreenTextASS(cues, []models.Scene{a, b}, []sceneVideo{{dur: 5.5}, {dur: 6}}, 1080, 1920, "Noto Sans SC")
	text := string(data)
	if count != 2 {
		t.Fatalf("count=%d\n%s", count, text)
	}
	for _, want := range []string{"PlayResX: 1080", "Style: ScreenText,Noto Sans SC", "0:00:06.50,0:00:08.50", `上\N官\N若\N琳`, `玉霄宫主`, `三日后\{禁\}`} {
		if !strings.Contains(text, want) {
			t.Fatalf("ASS missing %q:\n%s", want, text)
		}
	}
}

func TestBuildScreenTextASSUsesNormalizedCustomPositionAcrossAspectRatios(t *testing.T) {
	x, y := .9, .1
	cue := models.ScreenTextCue{Text: "人物题名", StartTime: 0, EndTime: 2, WritingMode: "vertical-rl", Anchor: "custom", PositionX: &x, PositionY: &y, Enabled: true, ReviewStatus: "approved"}
	wide, _ := buildScreenTextASS([]models.ScreenTextCue{cue}, []models.Scene{{ID: 1, Duration: 3}}, []sceneVideo{{dur: 3}}, 1920, 1080, "Noto Sans SC")
	portrait, _ := buildScreenTextASS([]models.ScreenTextCue{cue}, []models.Scene{{ID: 1, Duration: 3}}, []sceneVideo{{dur: 3}}, 1080, 1920, "Noto Sans SC")
	if !strings.Contains(string(wide), `\an5\pos(1728,108)`) {
		t.Fatalf("wide custom position wrong:\n%s", wide)
	}
	if !strings.Contains(string(portrait), `\an5\pos(972,192)`) {
		t.Fatalf("portrait custom position wrong:\n%s", portrait)
	}
}

func TestBuildScreenTextASSExcludesDraftDisabledAndUnselectedScenes(t *testing.T) {
	scene := models.Scene{ID: 1, Duration: 5}
	other := uint(2)
	cues := []models.ScreenTextCue{
		{Text: "草稿", StartTime: 0, EndTime: 1, Enabled: true, ReviewStatus: "draft"},
		{Text: "关闭", StartTime: 0, EndTime: 1, Enabled: false, ReviewStatus: "approved"},
		{SceneID: &other, Text: "未选择", StartTime: 0, EndTime: 1, Enabled: true, ReviewStatus: "approved"},
	}
	_, count := buildScreenTextASS(cues, []models.Scene{scene}, []sceneVideo{{dur: 5}}, 1920, 1080, "Noto Sans SC")
	if count != 0 {
		t.Fatalf("excluded cues rendered: %d", count)
	}
}
