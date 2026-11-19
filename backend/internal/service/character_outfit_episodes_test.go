package service

import (
	"testing"

	"comfyui-console/internal/models"
)

func TestAssignOutfitToEpisodesPreviewThenApply(t *testing.T) {
	svc, project, character := setupAgeStageTestDB(t)
	if err := svc.db.AutoMigrate(&models.CharacterAliasCandidate{}); err != nil {
		t.Fatal(err)
	}
	if err := svc.db.Create(&models.CharacterAliasCandidate{ProjectID: project.ID, Alias: "小微", CanonicalName: character.Name, Status: "confirmed"}).Error; err != nil {
		t.Fatal(err)
	}
	stage, err := svc.SaveAgeStage(project.ID, character.ID, 0, AgeStageInput{Name: "童年", Appearance: "6岁女孩"})
	if err != nil {
		t.Fatal(err)
	}
	child := models.CharacterOutfit{ProjectID: project.ID, CharacterID: character.ID, AgeStageID: stage.ID, Name: "童年日常", AuditStatus: models.LookStatusApproved}
	manual := models.CharacterOutfit{ProjectID: project.ID, CharacterID: character.ID, Name: "节日盛装", AuditStatus: models.LookStatusApproved}
	for _, row := range []*models.CharacterOutfit{&child, &manual} {
		if err := svc.db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	name := character.Name
	scenes := []models.Scene{
		{ProjectID: project.ID, EpisodeN: 1, Generation: 2, Order: 1, Title: "可见", Characters: name, ImageFile: "keep.png"},
		{ProjectID: project.ID, EpisodeN: 1, Generation: 2, Order: 2, Title: "别名可见", Characters: "小微"},
		{ProjectID: project.ID, EpisodeN: 1, Generation: 2, Order: 3, Title: "只配音", Characters: name, VisibleCharacters: "陆川", CharacterRolesSet: true},
		{ProjectID: project.ID, EpisodeN: 1, Generation: 1, Order: 1, Title: "旧版本", Characters: name},
		{ProjectID: project.ID, EpisodeN: 2, Generation: 1, Order: 1, Title: "已手选", Characters: name},
		{ProjectID: project.ID, EpisodeN: 2, Generation: 1, Order: 2, Title: "已归档", Characters: name, Archived: true},
		{ProjectID: project.ID, EpisodeN: 3, Generation: 1, Order: 1, Title: "范围外", Characters: name},
	}
	for i := range scenes {
		if err := svc.db.Create(&scenes[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.db.Create(&models.SceneCharacterOutfit{SceneID: scenes[4].ID, CharacterID: character.ID, OutfitID: manual.ID}).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.db.Create(&models.GenerationCandidate{ProjectID: project.ID, EntityType: "scene", EntityID: scenes[0].ID, MediaType: "image", File: "keep.png", IsCurrent: true}).Error; err != nil {
		t.Fatal(err)
	}
	req := OutfitEpisodeAssignInput{FromEpisode: 1, ToEpisode: 2}
	if _, err := svc.AssignOutfitToEpisodes(project.ID, character.ID, child.ID, OutfitEpisodeAssignInput{FromEpisode: 1, ToEpisode: 2, Preview: true}); err == nil {
		t.Fatal("unapproved age stage was accepted")
	}
	if err := svc.db.Model(stage).Update("audit_status", models.LookStatusApproved).Error; err != nil {
		t.Fatal(err)
	}
	req.Preview = true
	preview, err := svc.AssignOutfitToEpisodes(project.ID, character.ID, child.ID, req)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, item := range preview.Items {
		got[item.Title] = item.Action
	}
	want := map[string]string{"可见": outfitEpisodeAssign, "别名可见": outfitEpisodeAssign, "已手选": outfitEpisodeSkip}
	if len(got) != len(want) || preview.Assign != 2 || preview.Skipped != 1 || preview.Applied {
		t.Fatalf("unexpected preview: %+v", preview)
	}
	for title, action := range want {
		if got[title] != action {
			t.Fatalf("scene %s action=%s want %s (all=%v)", title, got[title], action, got)
		}
	}
	if !preview.Items[0].HasMedia {
		t.Fatal("preview did not report existing media")
	}
	var count int64
	svc.db.Model(&models.SceneCharacterOutfit{}).Where("outfit_id = ?", child.ID).Count(&count)
	if count != 0 {
		t.Fatal("preview wrote assignments")
	}

	req.Preview = false
	applied, err := svc.AssignOutfitToEpisodes(project.ID, character.ID, child.ID, req)
	if err != nil {
		t.Fatal(err)
	}
	if !applied.Applied || applied.Assign != 2 {
		t.Fatalf("unexpected apply result: %+v", applied)
	}
	assigned := func(sceneID uint) uint {
		var row models.SceneCharacterOutfit
		if svc.db.Where("scene_id = ? AND character_id = ?", sceneID, character.ID).First(&row).Error != nil {
			return 0
		}
		return row.OutfitID
	}
	if assigned(scenes[0].ID) != child.ID || assigned(scenes[1].ID) != child.ID {
		t.Fatal("visible scenes were not assigned")
	}
	if assigned(scenes[4].ID) != manual.ID {
		t.Fatal("manual selection was overwritten without overwrite=true")
	}
	for _, i := range []int{2, 3, 5, 6} {
		if assigned(scenes[i].ID) != 0 {
			t.Fatalf("out-of-scope scene %s was assigned", scenes[i].Title)
		}
	}
	var first models.Scene
	svc.db.First(&first, scenes[0].ID)
	if !first.PromptStale || first.ImageFile != "keep.png" {
		t.Fatalf("assigned scene must be stale with media kept: %+v", first)
	}
	var candidate models.GenerationCandidate
	svc.db.Where("entity_id = ?", scenes[0].ID).First(&candidate)
	if !candidate.Stale || !candidate.IsCurrent {
		t.Fatalf("candidate must be marked stale but stay current: %+v", candidate)
	}
	var untouched models.Scene
	svc.db.First(&untouched, scenes[4].ID)
	if untouched.PromptStale {
		t.Fatal("skipped scene was marked stale")
	}

	again, err := svc.AssignOutfitToEpisodes(project.ID, character.ID, child.ID, req)
	if err != nil {
		t.Fatal(err)
	}
	if again.Assign != 0 || again.Unchanged != 2 || again.Applied {
		t.Fatalf("repeat apply must be a no-op: %+v", again)
	}

	req.Overwrite = true
	overwritten, err := svc.AssignOutfitToEpisodes(project.ID, character.ID, child.ID, req)
	if err != nil {
		t.Fatal(err)
	}
	if overwritten.Assign != 1 || assigned(scenes[4].ID) != child.ID {
		t.Fatalf("overwrite did not replace manual selection: %+v", overwritten)
	}
}

func TestAssignOutfitToEpisodesKeepsOtherCharactersAndValidatesInput(t *testing.T) {
	svc, project, character := setupAgeStageTestDB(t)
	other := models.Character{ProjectID: project.ID, Name: "陆川"}
	if err := svc.db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	mine := models.CharacterOutfit{ProjectID: project.ID, CharacterID: character.ID, Name: "日常", AuditStatus: models.LookStatusApproved}
	theirs := models.CharacterOutfit{ProjectID: project.ID, CharacterID: other.ID, Name: "夹克", AuditStatus: models.LookStatusApproved}
	draft := models.CharacterOutfit{ProjectID: project.ID, CharacterID: character.ID, Name: "草稿", AuditStatus: models.LookStatusDraft}
	for _, row := range []*models.CharacterOutfit{&mine, &theirs, &draft} {
		if err := svc.db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Generation: 1, Order: 1, Characters: character.Name + ", 陆川"}
	if err := svc.db.Create(&scene).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.db.Create(&models.SceneCharacterOutfit{SceneID: scene.ID, CharacterID: other.ID, OutfitID: theirs.ID}).Error; err != nil {
		t.Fatal(err)
	}
	for _, bad := range []OutfitEpisodeAssignInput{{FromEpisode: 0, ToEpisode: 1}, {FromEpisode: 3, ToEpisode: 2}, {FromEpisode: 1, ToEpisode: 1 + maxOutfitEpisodeRange}} {
		if _, err := svc.AssignOutfitToEpisodes(project.ID, character.ID, mine.ID, bad); err == nil {
			t.Fatalf("invalid range accepted: %+v", bad)
		}
	}
	if _, err := svc.AssignOutfitToEpisodes(project.ID, character.ID, draft.ID, OutfitEpisodeAssignInput{FromEpisode: 1, ToEpisode: 1}); err == nil {
		t.Fatal("unapproved outfit accepted")
	}
	if _, err := svc.AssignOutfitToEpisodes(project.ID, character.ID, theirs.ID, OutfitEpisodeAssignInput{FromEpisode: 1, ToEpisode: 1}); err == nil {
		t.Fatal("another character's outfit accepted")
	}
	if _, err := svc.AssignOutfitToEpisodes(project.ID, character.ID, mine.ID, OutfitEpisodeAssignInput{FromEpisode: 1, ToEpisode: 1, Overwrite: true}); err != nil {
		t.Fatal(err)
	}
	var rows []models.SceneCharacterOutfit
	svc.db.Where("scene_id = ?", scene.ID).Order("character_id").Find(&rows)
	if len(rows) != 2 || rows[0].OutfitID != mine.ID || rows[1].OutfitID != theirs.ID {
		t.Fatalf("other character's selection changed: %+v", rows)
	}
}
