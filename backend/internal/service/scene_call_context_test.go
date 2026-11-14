package service

import (
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

func TestSceneCallTypeAllowsGoldenOpeningOnlyForFirstActiveScene(t *testing.T) {
	db := newTestDBWithNewModels(t)
	project := models.Project{Title: "开场门控"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	scenes := []models.Scene{
		{ProjectID: project.ID, EpisodeN: 1, Generation: 1, Order: 1, Title: "废弃旧开场", Archived: true},
		{ProjectID: project.ID, EpisodeN: 1, Generation: 1, Order: 2, Title: "场景1：山门夜雨"},
		{ProjectID: project.ID, EpisodeN: 1, Generation: 1, Order: 3, Title: "场景2：殿前对峙"},
		{ProjectID: project.ID, EpisodeN: 1, Generation: 1, Order: 4, Title: "场景2：殿前对峙（续二）"},
		{ProjectID: project.ID, EpisodeN: 2, Generation: 1, Order: 1, Title: "场景1：重逢 · Native段2"},
		{ProjectID: project.ID, EpisodeN: 3, Generation: 1, Order: 1, Title: "场景1：渡口", Content: "\n（续）船未靠岸。"},
	}
	if err := db.Create(&scenes).Error; err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		scene  models.Scene
		want   string
		golden bool
	}{
		{scenes[1], sceneCallEpisodeOpening, true}, // archived Order 1 does not count
		{scenes[2], sceneCallNewScene, false},
		{scenes[3], sceneCallContinuation, false},
		{scenes[4], sceneCallContinuation, false}, // first of its episode, but a later Native segment
		{scenes[5], sceneCallContinuation, false}, // continuation marker on the first content line
	}
	for _, tc := range cases {
		got, golden := sceneCallType(db, &tc.scene)
		if got != tc.want || golden != tc.golden {
			t.Fatalf("%s: got (%s, %v), want (%s, %v)", tc.scene.Title, got, golden, tc.want, tc.golden)
		}
	}
	if !strings.Contains(sceneCallContext(db, &scenes[1]), "允许黄金3秒开场=是") {
		t.Fatal("episode opening should allow the golden opening")
	}
	if block := sceneCallContext(db, &scenes[3]); !strings.Contains(block, "允许黄金3秒开场=否") || !strings.Contains(block, "场景调用类型=同场景续段") {
		t.Fatalf("continuation block = %q", block)
	}
}

func TestSceneIsContinuationIgnoresSameLocationNewScene(t *testing.T) {
	for _, title := range []string{"场景5：山门（白天）", "场景6：续命丹", "场景7 · Native段1"} {
		if sceneIsContinuation(&models.Scene{Title: title}) {
			t.Fatalf("%q must not be treated as a continuation", title)
		}
	}
}
