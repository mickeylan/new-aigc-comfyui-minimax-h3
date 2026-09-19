package service

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"comfyui-console/internal/models"
)

func newCharacterHistoryTestService(t *testing.T) (*CharacterHistoryService, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&models.Project{}, &models.Character{}, &models.CharacterAliasCandidate{},
		&models.Scene{}, &models.Shot{}, &models.Dialogue{}, &models.Asset{},
		&models.CharacterLook{}, &models.SceneCharacterLook{}, &models.ShotCharacterLook{},
		&models.CharacterOutfit{}, &models.SceneCharacterOutfit{}, &models.ShotCharacterOutfit{},
	); err != nil {
		t.Fatal(err)
	}
	return NewCharacterHistoryService(db), db
}

func mustCreateHistoryRow(t *testing.T, db *gorm.DB, value any) {
	t.Helper()
	if err := db.Create(value).Error; err != nil {
		t.Fatal(err)
	}
}

func TestCharacterHistoryProjectIsolationAndConservativeAliases(t *testing.T) {
	service, db := newCharacterHistoryTestService(t)
	p1, p2 := models.Project{Title: "one"}, models.Project{Title: "two"}
	mustCreateHistoryRow(t, db, &p1)
	mustCreateHistoryRow(t, db, &p2)
	ch1 := models.Character{ProjectID: p1.ID, Name: "林月"}
	ch2 := models.Character{ProjectID: p2.ID, Name: "林月"}
	mustCreateHistoryRow(t, db, &ch1)
	mustCreateHistoryRow(t, db, &ch2)
	mustCreateHistoryRow(t, db, &models.CharacterAliasCandidate{ProjectID: p1.ID, CanonicalName: "林月", Alias: "阿月", Status: "confirmed"})
	mustCreateHistoryRow(t, db, &models.CharacterAliasCandidate{ProjectID: p1.ID, CanonicalName: "林月", Alias: "月", Status: "pending"})
	mustCreateHistoryRow(t, db, &models.Scene{ProjectID: p1.ID, EpisodeN: 1, Order: 1, Characters: "阿月", Title: "alias"})
	mustCreateHistoryRow(t, db, &models.Scene{ProjectID: p1.ID, EpisodeN: 1, Order: 2, Characters: "林月光", Title: "substring must not match"})
	mustCreateHistoryRow(t, db, &models.Scene{ProjectID: p2.ID, EpisodeN: 9, Order: 1, Characters: "林月", Title: "other project"})

	id := ch1.ID
	rows, err := service.List(p1.ID, &id)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || len(rows[0].Episodes) != 1 || len(rows[0].Episodes[0].Scenes) != 1 {
		t.Fatalf("unexpected isolated history: %+v", rows)
	}
	if rows[0].Episodes[0].Scenes[0].Title != "alias" || len(rows[0].Aliases) != 1 || rows[0].Aliases[0] != "阿月" {
		t.Fatalf("alias matching was not conservative: %+v", rows[0])
	}
	if _, err := service.List(p1.ID, func() *uint { value := ch2.ID; return &value }()); err != gorm.ErrRecordNotFound {
		t.Fatalf("foreign project character should be hidden, got %v", err)
	}
}

func TestCharacterHistoryOrdersEpisodesScenesAndShots(t *testing.T) {
	service, db := newCharacterHistoryTestService(t)
	project := models.Project{Title: "order"}
	mustCreateHistoryRow(t, db, &project)
	character := models.Character{ProjectID: project.ID, Name: "周野"}
	mustCreateHistoryRow(t, db, &character)
	scene22 := models.Scene{ProjectID: project.ID, EpisodeN: 2, Order: 2, Characters: "周野", Title: "2-2"}
	scene11 := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1, Characters: "周野", Title: "1-1"}
	scene21 := models.Scene{ProjectID: project.ID, EpisodeN: 2, Order: 1, Characters: "周野", Title: "2-1"}
	mustCreateHistoryRow(t, db, &scene22)
	mustCreateHistoryRow(t, db, &scene11)
	mustCreateHistoryRow(t, db, &scene21)
	shot2 := models.Shot{SceneID: scene21.ID, Order: 2}
	shot1 := models.Shot{SceneID: scene21.ID, Order: 1}
	mustCreateHistoryRow(t, db, &shot2)
	mustCreateHistoryRow(t, db, &shot1)

	rows, err := service.List(project.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	history := rows[0]
	if len(history.Episodes) != 2 || history.Episodes[0].Number != 1 || history.Episodes[1].Number != 2 {
		t.Fatalf("episodes not ordered: %+v", history.Episodes)
	}
	if got := history.Episodes[1].Scenes; len(got) != 2 || got[0].Title != "2-1" || got[1].Title != "2-2" {
		t.Fatalf("scenes not ordered: %+v", got)
	}
	shots := history.Episodes[1].Scenes[0].Shots
	if len(shots) != 2 || shots[0].Order != 1 || shots[1].Order != 2 {
		t.Fatalf("shots not ordered: %+v", shots)
	}
}

func TestCharacterHistoryDerivesLastContextWithoutPersistence(t *testing.T) {
	service, db := newCharacterHistoryTestService(t)
	project := models.Project{Title: "context"}
	mustCreateHistoryRow(t, db, &project)
	character := models.Character{ProjectID: project.ID, Name: "沈星", Voice: "Cherry", VoiceRef: "voice.wav", VoiceID: "clone-1", VoiceModel: "qwen3-tts"}
	mustCreateHistoryRow(t, db, &character)
	prop := models.Asset{ProjectID: project.ID, Kind: "prop", Name: "银钥匙"}
	mustCreateHistoryRow(t, db, &prop)
	oldScene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 3, VisibleCharacters: "沈星", Props: "银钥匙", Title: "旧场"}
	lastScene := models.Scene{ProjectID: project.ID, EpisodeN: 2, Order: 4, VisibleCharacters: "沈星", Props: "银钥匙, 无资产道具", Title: "终场"}
	mustCreateHistoryRow(t, db, &oldScene)
	mustCreateHistoryRow(t, db, &lastScene)
	lastShot := models.Shot{SceneID: lastScene.ID, Order: 5}
	mustCreateHistoryRow(t, db, &lastShot)
	approvedLook := models.CharacterLook{ProjectID: project.ID, CharacterID: character.ID, Name: "夜行装", AuditStatus: models.LookStatusApproved}
	draftLook := models.CharacterLook{ProjectID: project.ID, CharacterID: character.ID, Name: "未审核", AuditStatus: models.LookStatusDraft}
	approvedOutfit := models.CharacterOutfit{ProjectID: project.ID, CharacterID: character.ID, Name: "潜入套装", AuditStatus: models.LookStatusApproved}
	mustCreateHistoryRow(t, db, &approvedLook)
	mustCreateHistoryRow(t, db, &draftLook)
	mustCreateHistoryRow(t, db, &approvedOutfit)
	mustCreateHistoryRow(t, db, &models.SceneCharacterLook{SceneID: oldScene.ID, LookID: approvedLook.ID})
	mustCreateHistoryRow(t, db, &models.SceneCharacterLook{SceneID: lastScene.ID, LookID: draftLook.ID})
	mustCreateHistoryRow(t, db, &models.ShotCharacterOutfit{ShotID: lastShot.ID, CharacterID: character.ID, OutfitID: approvedOutfit.ID})

	rows, err := service.List(project.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	history := rows[0]
	if history.LastSeen == nil || history.LastSeen.EpisodeN != 2 || history.LastSeen.SceneID != lastScene.ID || history.LastSeen.ShotID == nil || *history.LastSeen.ShotID != lastShot.ID {
		t.Fatalf("wrong last seen context: %+v", history.LastSeen)
	}
	if history.LastApprovedLook == nil || history.LastApprovedLook.ID != approvedLook.ID || history.LastApprovedOutfit == nil || history.LastApprovedOutfit.ID != approvedOutfit.ID {
		t.Fatalf("wrong approved context: look=%+v outfit=%+v", history.LastApprovedLook, history.LastApprovedOutfit)
	}
	if len(history.Props) != 2 || history.Props[0].ID != prop.ID || history.Props[1].ID != 0 {
		t.Fatalf("wrong props: %+v", history.Props)
	}
	if history.VoiceConfig.VoiceID != "clone-1" || history.VoiceConfig.Voice != "Cherry" {
		t.Fatalf("wrong voice config: %+v", history.VoiceConfig)
	}
	for _, table := range []string{"character_histories", "character_history"} {
		if db.Migrator().HasTable(table) {
			t.Fatalf("derived history unexpectedly persisted in %s", table)
		}
	}
}
