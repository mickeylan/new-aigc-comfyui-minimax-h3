package service

import (
	"comfyui-console/internal/models"
	"strings"
	"testing"
)

func TestProductionIdentitiesReuseConfirmedNamesAndRejectAmbiguousGuess(t *testing.T) {
	r := &productionIdentityRegistry{
		Characters: []models.Character{{Name: "柳石"}, {Name: "齐姓道士"}, {Name: "柳乐儿"}},
		Assets:     []models.Asset{{Kind: AssetKindLocation, Name: "苍黄荒地"}, {Kind: AssetKindProp, Name: "旧拨浪鼓"}},
		Aliases:    []models.CharacterAliasCandidate{{Alias: "青衣高大男子", CanonicalName: "柳石"}},
	}
	scenes := []scriptScene{{Characters: []string{"青衣高大男子", "轻飘飘落地的齐姓道士", "齐姓道士"}, Location: "战后的苍黄荒地，雷法浅坑附近", Props: []string{"旧拨浪鼓"}}}
	r.normalizeScenes(scenes)
	if got := strings.Join(scenes[0].Characters, ","); got != "柳石,齐姓道士" {
		t.Fatalf("characters=%s", got)
	}
	if scenes[0].Location != "苍黄荒地" {
		t.Fatalf("location=%s", scenes[0].Location)
	}
	for _, name := range []string{"女童", "高大青年", "柳乐儿与齐姓道士"} {
		if got := r.character(name); got != name {
			t.Fatalf("guessed %s as %s", name, got)
		}
	}
	for _, name := range []string{"苍黄荒地神殿", "另一处荒地"} {
		if got := r.asset(AssetKindLocation, name); got != name {
			t.Fatalf("merged distinct location %s", name)
		}
	}
	if got := r.asset(AssetKindProp, "新拨浪鼓"); got != "新拨浪鼓" {
		t.Fatal("merged different prop")
	}
}

func TestProductionEntitySyncIsIdempotentAndIgnoresArchivedScenes(t *testing.T) {
	ps, _, p, _ := newEpisodeAdaptationTestService(t, &captureTextProvider{})
	ch := models.Character{ProjectID: p.ID, Name: "柳石", Portrait: "reviewed.png", ProfileStatus: models.ProfileStatusApproved}
	if err := ps.db.Create(&ch).Error; err != nil {
		t.Fatal(err)
	}
	if err := ps.db.Create(&models.CharacterAliasCandidate{ProjectID: p.ID, Alias: "青衣高大男子", CanonicalName: "柳石", Status: "confirmed"}).Error; err != nil {
		t.Fatal(err)
	}
	for _, a := range []models.Asset{{ProjectID: p.ID, Kind: AssetKindLocation, Name: "苍黄荒地", Image: "place.png"}, {ProjectID: p.ID, Kind: AssetKindProp, Name: "旧拨浪鼓", Image: "prop.png"}} {
		if err := ps.db.Create(&a).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, sc := range []models.Scene{
		{ProjectID: p.ID, EpisodeN: 2, Generation: 1, Archived: true, LocationName: "废稿虚构场景", VisibleCharacters: "废稿人物", Props: "废稿法器"},
		{ProjectID: p.ID, EpisodeN: 2, Generation: 2, LocationName: "战后的苍黄荒地，原巨石所在处", VisibleCharacters: "青衣高大男子", Props: "旧拨浪鼓"},
		{ProjectID: p.ID, EpisodeN: 3, Generation: 3, LocationName: "苍黄荒地上空", VisibleCharacters: "青衣高大男子", Props: "旧拨浪鼓"},
	} {
		if err := ps.db.Create(&sc).Error; err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 3; i++ {
		for _, ep := range []int{2, 3} {
			if err := ps.SyncEpisodeProductionEntities(p.ID, ep); err != nil {
				t.Fatal(err)
			}
		}
	}
	var cc, ac int64
	ps.db.Model(&models.Character{}).Where("project_id=?", p.ID).Count(&cc)
	ps.db.Model(&models.Asset{}).Where("project_id=?", p.ID).Count(&ac)
	if cc != 1 || ac != 2 {
		t.Fatalf("duplicate cards created characters=%d assets=%d", cc, ac)
	}
	var got models.Character
	ps.db.First(&got, ch.ID)
	if got.Portrait != "reviewed.png" || got.ProfileStatus != models.ProfileStatusApproved {
		t.Fatal("reviewed identity overwritten")
	}
}
