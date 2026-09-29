package service

import (
	"testing"

	"comfyui-console/internal/models"
)

func TestCreateProjectIsIdempotentWithCreateToken(t *testing.T) {
	db := setupTestDB(t)
	svc := &ProjectService{db: db}
	token := "create-once"
	first, err := svc.CreateProject(models.Project{Title: "唯一项目", Synopsis: "故事", CreateToken: &token})
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.CreateProject(models.Project{Title: "重复请求", Synopsis: "故事", CreateToken: &token})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("duplicate projects created: %d != %d", first.ID, second.ID)
	}
	var count int64
	if err := db.Model(&models.Project{}).Where("create_token = ?", token).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("project count=%d", count)
	}
}

func TestDeleteProjectRemovesProductionGraph(t *testing.T) {
	db := setupTestDB(t)
	if err := db.AutoMigrate(
		&models.Episode{}, &models.ScriptRevision{}, &models.MergeTask{}, &models.Material{}, &models.Asset{}, &models.AssetVariant{},
		&models.Dialogue{}, &models.ProjectSkillConfig{}, &models.SkillAuditLog{}, &models.Chapter{}, &models.ChapterTask{},
		&models.StoryArc{}, &models.StoryBible{}, &models.AdaptationStrategy{}, &models.EpisodeAdaptation{},
		&models.CharacterAliasCandidate{}, &models.NovelJob{}, &models.PlanningBatch{}, &models.BatchEpisode{},
		&models.BatchStateSnapshot{}, &models.StoryClue{}, &models.PromptVersion{}, &models.CharacterOutfit{},
		&models.CharacterOutfitLook{}, &models.SceneCharacterOutfit{}, &models.ShotCharacterOutfit{}, &models.FrameCandidate{},
		&models.GenerationCandidate{}, &models.AudioLayer{}, &models.PromptPolicyOverride{}, &models.CharacterMotionReference{},
		&models.SharedAssetReference{}, &models.SceneContinuity{}, &models.UploadFile{}, &models.Task{}, &models.Event{},
	); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "可删除项目"}
	db.Create(&project)
	character := models.Character{ProjectID: project.ID, Name: "角色"}
	db.Create(&character)
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1}
	db.Create(&scene)
	shot := models.Shot{SceneID: scene.ID, Order: 1, Duration: 5}
	db.Create(&shot)
	look := models.CharacterLook{ProjectID: project.ID, CharacterID: character.ID, Name: "造型"}
	db.Create(&look)
	outfit := models.CharacterOutfit{ProjectID: project.ID, CharacterID: character.ID, Name: "套装"}
	db.Create(&outfit)
	db.Create(&models.CharacterOutfitLook{OutfitID: outfit.ID, LookID: look.ID})
	db.Create(&models.SceneCharacterLook{SceneID: scene.ID, LookID: look.ID})
	db.Create(&models.ShotCharacterOutfit{ShotID: shot.ID, CharacterID: character.ID, OutfitID: outfit.ID})
	db.Create(&models.GenerationCandidate{ProjectID: project.ID, EntityType: "scene", EntityID: scene.ID, MediaType: "image", TaskID: "candidate", File: "x.png"})
	db.Create(&models.PromptPolicyOverride{ProjectID: project.ID, SceneID: &scene.ID, ShotID: &shot.ID, PolicyKey: "test"})

	svc := &ProjectService{db: db}
	if err := svc.DeleteProject(project.ID); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&models.Project{}).Where("id = ?", project.ID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("project still exists")
	}
}
