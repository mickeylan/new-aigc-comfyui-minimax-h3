package service

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"comfyui-console/internal/models"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newShotMaterializationDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Project{}, &models.Scene{}, &models.Shot{}, &models.Dialogue{}, &models.SceneContinuity{}, &models.FrameCandidate{}, &models.GenerationCandidate{}, &models.SceneCharacterLook{}, &models.ShotCharacterLook{}, &models.CharacterOutfit{}, &models.SceneCharacterOutfit{}, &models.ShotCharacterOutfit{}, &models.PromptPolicyOverride{}, &models.SharedAssetReference{}, &models.Material{}, &models.AudioLayer{}, &models.MusicCueSheet{}, &models.PromptVersion{}, &models.Episode{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestGroupedNarrativeContentPreservesAttributionAndRepeatedOccurrences(t *testing.T) {
	repeated := models.Dialogue{Character: "甲", SpeechType: "dialogue"}
	group := nativeShotGroup{
		Shots: []models.Shot{{Description: "甲说：“别动。”，乙回头。", PromptSubject: "错误身份"}},
		FragmentSets: [][]dialogueFragment{{
			{Source: repeated, Text: "别动。"},
			{Source: repeated, Text: "别动。"},
			{Source: models.Dialogue{Character: "旁白", SpeechType: "narration"}, Text: " 夜深了。"},
			{Source: models.Dialogue{Character: "乙", SpeechType: "offscreen_dialogue"}, Text: "谁？"},
			{Source: models.Dialogue{Character: "乙", SpeechType: "monologue"}, Text: "是他……"},
			{Source: models.Dialogue{SpeechType: "offscreen_dialogue"}, Text: "不许走！"},
			{Source: models.Dialogue{SpeechType: "dialogue"}, Text: "等等。"},
		}},
	}
	got := groupedNarrativeContent(group)
	for _, want := range []string{"甲说：“别动。”，乙回头。", "甲说：“别动。”", "画外旁白：“ 夜深了。”", "乙从画外说道：“谁？”", "乙的内心独白：“是他……”", "画外声音说道：“不许走！”", "说话人待确认：“等等。”"} {
		if !strings.Contains(got, want) {
			t.Fatalf("content missing %q: %s", want, got)
		}
	}
	if strings.Contains(got, "错误身份说") || strings.Count(got, "别动。") != 2 {
		t.Fatalf("content invented identity or lost repeated occurrence: %s", got)
	}
}

func TestMaterializeShotsCreatesNativeScenesAndPreservesDialogue(t *testing.T) {
	db := newShotMaterializationDB(t)
	project := models.Project{Title: "p", Generation: 1}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Character{ProjectID: project.ID, Name: "若琳"}).Error; err != nil {
		t.Fatal(err)
	}
	before := models.Scene{ProjectID: project.ID, EpisodeN: 1, Generation: 1, Order: 1, Title: "before", Content: "before", Duration: 5}
	_ = db.Create(&before).Error
	source := models.Scene{ProjectID: project.ID, EpisodeN: 1, Generation: 1, Order: 2, Title: "桃花林", Content: "姐妹对话", ImagePrompt: "桃花林", ImageFile: "source.png", VideoFile: "source.mp4", Duration: 15, LocationName: "梵心桃花林", Characters: "若彤,若琳", ReferenceImagesJSON: `[]`}
	if err := db.Create(&source).Error; err != nil {
		t.Fatal(err)
	}
	after := models.Scene{ProjectID: project.ID, EpisodeN: 1, Generation: 1, Order: 3, Title: "after", Content: "after", Duration: 5, ImageFile: "keep.png"}
	_ = db.Create(&after).Error
	d1 := models.Dialogue{ProjectID: project.ID, SceneID: source.ID, Order: 1, Character: "若彤", SpeechType: "dialogue", Text: "姐姐，你还好吗？", Status: "ready", AudioFile: "old.wav"}
	d2 := models.Dialogue{ProjectID: project.ID, SceneID: source.ID, Order: 2, Character: "若琳", SpeechType: "dialogue", Text: "我很好。"}
	_ = db.Create(&d1).Error
	_ = db.Create(&d2).Error
	candidate := models.GenerationCandidate{ProjectID: project.ID, EntityType: "scene", EntityID: source.ID, MediaType: "image", TaskID: "source-image", File: "source.png"}
	frame := models.FrameCandidate{ProjectID: project.ID, SceneID: source.ID, VideoTaskID: "source-video", FrameIndex: 1, ImageFile: "source-frame.png"}
	_ = db.Create(&candidate).Error
	_ = db.Create(&frame).Error
	shots := []models.Shot{
		{SceneID: source.ID, Order: 1, ActType: models.ShotActSetup, ShotType: "若彤近景", Duration: 4, SpeechPresentation: models.ShotSpeechPresentationVisibleSpeaker, Description: "若彤询问并同步口型", Dialogue: "姐姐，", PromptSubject: "若彤可见", PromptAction: "若彤同步口型询问", PromptCamera: "近景", PromptLighting: "夕阳", PromptStyle: "写实"},
		{SceneID: source.ID, Order: 2, ActType: models.ShotActRising, ShotType: "若琳听者反应", Duration: 5, SpeechPresentation: models.ShotSpeechPresentationListenerCarry, ListenerCharacter: "若琳", Description: "若琳闭口聆听，若彤声音跨切延续", Dialogue: "你还好吗？", PromptSubject: "若琳可见，若彤不入镜", PromptAction: "若琳保持闭口聆听，若彤声音跨切延续", PromptCamera: "特写", PromptLighting: "夕阳", PromptStyle: "写实"},
		{SceneID: source.ID, Order: 3, ActType: models.ShotActResolution, ShotType: "若琳近景", Duration: 4, SpeechPresentation: models.ShotSpeechPresentationVisibleSpeaker, Description: "若琳回答并同步口型", Dialogue: "我很好。", PromptSubject: "若琳可见", PromptAction: "若琳同步口型回答", PromptCamera: "近景", PromptLighting: "夕阳", PromptStyle: "写实"},
	}
	for i := range shots {
		if err := db.Create(&shots[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	preview, err := NewShotService(db).PreviewMaterialization(project.ID, source.ID)
	if err != nil {
		t.Fatal(err)
	}
	created, err := NewShotService(db).Materialize(project.ID, source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(created) != 1 || created[0].Duration != 13 || created[0].ShotCount != 3 {
		t.Fatalf("created=%+v", created)
	}
	if len(preview.Items) != len(created) || preview.Items[0].Content != created[0].Content {
		t.Fatal("preview and persisted narrative differ")
	}
	for _, want := range []string{"若彤说：“姐姐，”", "若彤说：“你还好吗？”", "若琳说：“我很好。”"} {
		if !strings.Contains(created[0].Content, want) {
			t.Fatalf("materialization lost narrative dialogue %q: %s", want, created[0].Content)
		}
	}
	var scenes []models.Scene
	if err := db.Where("project_id=? AND (archived = ? OR archived IS NULL)", project.ID, false).Order("`order`").Find(&scenes).Error; err != nil {
		t.Fatal(err)
	}
	if len(scenes) != 3 || scenes[0].ID != before.ID || scenes[2].ID != after.ID || scenes[2].ImageFile != "keep.png" {
		t.Fatalf("active scenes=%+v", scenes)
	}
	var archived models.Scene
	if err := db.First(&archived, source.ID).Error; err != nil || !archived.Archived || archived.Order != source.Order || archived.ImageFile != "source.png" || archived.VideoFile != "source.mp4" {
		t.Fatalf("archived source=%+v err=%v", archived, err)
	}
	var candidateCount, frameCount, sourceShotCount int64
	db.Model(&models.GenerationCandidate{}).Where("entity_type = ? AND entity_id = ?", "scene", source.ID).Count(&candidateCount)
	db.Model(&models.FrameCandidate{}).Where("scene_id = ?", source.ID).Count(&frameCount)
	db.Model(&models.Shot{}).Where("scene_id = ?", source.ID).Count(&sourceShotCount)
	if candidateCount != 1 || frameCount != 1 || sourceShotCount != 3 {
		t.Fatalf("archived dependents candidate=%d frame=%d shots=%d", candidateCount, frameCount, sourceShotCount)
	}
	var childShots []models.Shot
	if err := db.Where("scene_id = ?", created[0].ID).Order("order_num").Find(&childShots).Error; err != nil || len(childShots) != 3 {
		t.Fatalf("child shots=%+v err=%v", childShots, err)
	}
	var childDialogues []models.Dialogue
	if err := db.Where("scene_id = ?", created[0].ID).Order("`order`, id").Find(&childDialogues).Error; err != nil || len(childDialogues) != 3 {
		t.Fatalf("child dialogues=%+v err=%v", childDialogues, err)
	}
	wantRanges := [][]models.ShotDialogueRange{
		{{DialogueID: childDialogues[0].ID, GroupKey: fmt.Sprintf("source-dialogue:%d", d1.ID), StartRune: 0, EndRune: 3}},
		{{DialogueID: childDialogues[1].ID, GroupKey: fmt.Sprintf("source-dialogue:%d", d1.ID), StartRune: 3, EndRune: 8}},
		{{DialogueID: childDialogues[2].ID, GroupKey: fmt.Sprintf("source-dialogue:%d", d2.ID), StartRune: 0, EndRune: 4}},
	}
	for i := range childShots {
		if !reflect.DeepEqual(childShots[i].DialogueRanges, wantRanges[i]) {
			t.Fatalf("child shot %d dialogue ranges=%+v want=%+v", i+1, childShots[i].DialogueRanges, wantRanges[i])
		}
	}
	if childShots[1].SpeechPresentation != models.ShotSpeechPresentationListenerCarry || childShots[1].ListenerCharacter != "若琳" {
		t.Fatalf("materialization lost authoritative listener: %+v", childShots[1])
	}
	decorated, err := NewShotService(db).GetSceneShots(created[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if !decorated[0].ContinuesToNext || !decorated[1].ContinuesFromPrevious || decorated[1].ContinuesToNext || decorated[2].ContinuesFromPrevious {
		t.Fatalf("materialized dialogue continuity=%+v", decorated)
	}
	var joined string
	for _, sc := range created {
		if sc.Duration < 3 || sc.Duration > 15 {
			t.Fatalf("duration=%v", sc.Duration)
		}
		var ds []models.Dialogue
		_ = db.Where("scene_id=?", sc.ID).Order("`order`").Find(&ds).Error
		for _, d := range ds {
			joined += d.Text
			if d.AudioFile != "" || !d.AudioStale {
				t.Fatalf("audio not reset: %+v", d)
			}
		}
	}
	if joined != "姐姐，你还好吗？我很好。" {
		t.Fatalf("joined=%q", joined)
	}
}

func TestMaterializationRejectsStaleListenerBeforeReplacingSourceScene(t *testing.T) {
	db := newShotMaterializationDB(t)
	project := models.Project{Title: "stale listener", Generation: 1}
	db.Create(&project)
	source := models.Scene{ProjectID: project.ID, EpisodeN: 1, Generation: 1, Order: 1, Title: "原场景", Content: "保留内容", Duration: 6, Status: "video_ready", VideoFile: "keep.mp4"}
	db.Create(&source)
	shots := []models.Shot{
		{SceneID: source.ID, Order: 1, ShotType: "听者反应", Duration: 3, Description: "旧数据", SpeechPresentation: models.ShotSpeechPresentationListenerCarry, ListenerCharacter: "已删除角色"},
		{SceneID: source.ID, Order: 2, ShotType: "空镜", Duration: 3, Description: "落花", SpeechPresentation: models.ShotSpeechPresentationSilent},
	}
	for i := range shots {
		if err := db.Create(&shots[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	svc := NewShotService(db)
	if _, err := svc.PreviewMaterialization(project.ID, source.ID); !errors.Is(err, ErrDialogueTimingBlocked) || !strings.Contains(err.Error(), "已删除角色") {
		t.Fatalf("stale listener preview accepted: %v", err)
	}
	if _, err := svc.Materialize(project.ID, source.ID); !errors.Is(err, ErrDialogueTimingBlocked) {
		t.Fatalf("stale listener materialization accepted: %v", err)
	}
	var saved models.Scene
	if err := db.First(&saved, source.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.Content != "保留内容" || saved.VideoFile != "keep.mp4" || saved.Status != "video_ready" {
		t.Fatalf("failed materialization mutated source: %+v", saved)
	}
	var count int64
	if err := db.Model(&models.Scene{}).Where("project_id = ?", project.ID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("failed materialization created scenes: count=%d err=%v", count, err)
	}
}

func TestMaterializedSceneBaseAcceptsLegacyAndNativeSegmentTitles(t *testing.T) {
	for _, tc := range []struct {
		title string
		base  string
		part  int
	}{
		{"场景26：真相大白 · 镜头2", "场景26：真相大白", 2},
		{"场景26：真相大白 · Native段3", "场景26：真相大白", 3},
	} {
		base, part, ok := materializedSceneBase(tc.title)
		if !ok || base != tc.base || part != tc.part {
			t.Fatalf("materializedSceneBase(%q)=(%q,%d,%v)", tc.title, base, part, ok)
		}
	}
}

func TestPlanDialogueFragmentsPreservesInternalWhitespace(t *testing.T) {
	dialogues := []models.Dialogue{{ID: 1, Text: "I love you"}}
	shots := []models.Shot{{Dialogue: "I love "}, {Dialogue: "you"}}
	fragments, err := planDialogueFragments(shots, dialogues)
	if err != nil {
		t.Fatal(err)
	}
	if fragments[0][0].Text != "I love " || fragments[1][0].Text != "you" {
		t.Fatalf("fragments=%+v", fragments)
	}
	shots[0].Dialogue = "Ilove "
	if _, err := planDialogueFragments(shots, dialogues); err == nil {
		t.Fatal("whitespace rewrite accepted during materialization")
	}
}

func TestMaterializationRejectsInvalidDialoguePresentationWithoutMutation(t *testing.T) {
	db := newShotMaterializationDB(t)
	project := models.Project{Title: "guard", Generation: 1}
	db.Create(&project)
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Generation: 1, Order: 1, Title: "对白", Duration: 8}
	db.Create(&scene)
	db.Create(&models.Dialogue{ProjectID: project.ID, SceneID: scene.ID, Order: 1, Character: "阿宁", SpeechType: "dialogue", Text: "相信姐姐。"})
	shots := []models.Shot{
		{SceneID: scene.ID, Order: 1, ActType: models.ShotActSetup, ShotType: "空镜", Duration: 4, Description: "庭院空镜", Dialogue: "相信", PromptSubject: "庭院"},
		{SceneID: scene.ID, Order: 2, ActType: models.ShotActResolution, ShotType: "空镜", Duration: 4, Description: "庭院空镜", Dialogue: "姐姐。", PromptSubject: "庭院"},
	}
	for i := range shots {
		db.Create(&shots[i])
	}
	svc := NewShotService(db)
	if _, err := svc.PreviewMaterialization(project.ID, scene.ID); err == nil || !strings.Contains(err.Error(), "对白节奏导演草稿未通过自然语速审计") {
		t.Fatalf("invalid preview accepted: %v", err)
	}
	if _, err := svc.Materialize(project.ID, scene.ID); err == nil {
		t.Fatal("invalid materialization accepted")
	}
	var source models.Scene
	if err := db.First(&source, scene.ID).Error; err != nil {
		t.Fatalf("source scene was mutated or deleted: %v", err)
	}
	var count int64
	db.Model(&models.Shot{}).Where("scene_id = ?", scene.ID).Count(&count)
	if count != 2 {
		t.Fatalf("source shots changed: %d", count)
	}
}

func TestNativeSceneBaseTitleRemovesRepeatedSegmentSuffixes(t *testing.T) {
	got := nativeSceneBaseTitle("场景26：真相大白 · Native段1 · Native段1")
	if got != "场景26：真相大白" {
		t.Fatalf("base title=%q", got)
	}
}

func TestGroupNativeShotsPacksAdjacentShotsUpToFifteenSeconds(t *testing.T) {
	durations := []float64{4, 3, 3, 5, 5, 4, 5, 4, 4, 3, 4, 5, 4}
	shots := make([]models.Shot, len(durations))
	fragments := make([][]dialogueFragment, len(durations))
	for i, duration := range durations {
		shots[i] = models.Shot{ID: uint(i + 1), Duration: duration}
	}
	groups := groupNativeShots(shots, fragments)
	if len(groups) != 4 {
		t.Fatalf("groups=%d", len(groups))
	}
	want := []float64{15, 14, 15, 9}
	for i := range groups {
		if groups[i].Duration != want[i] {
			t.Fatalf("group %d duration=%v want=%v", i, groups[i].Duration, want[i])
		}
	}
}

func TestRenumberSceneTitlesUpdatesOnlyNumericPrefix(t *testing.T) {
	db := newShotMaterializationDB(t)
	project := models.Project{Title: "p", Generation: 1}
	_ = db.Create(&project).Error
	scenes := []models.Scene{
		{ProjectID: project.ID, EpisodeN: 1, Generation: 1, Order: 1, Title: "场景1：开场"},
		{ProjectID: project.ID, EpisodeN: 1, Generation: 1, Order: 2, Title: "场景3：姐妹重逢 · Native段1"},
		{ProjectID: project.ID, EpisodeN: 1, Generation: 1, Order: 3, Title: "场景4：后续"},
		{ProjectID: project.ID, EpisodeN: 1, Generation: 1, Order: 4, Title: "自定义标题"},
	}
	for i := range scenes {
		if err := db.Create(&scenes[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := renumberSceneTitles(db, project.ID, 1, 1); err != nil {
		t.Fatal(err)
	}
	var got []models.Scene
	_ = db.Where("project_id = ?", project.ID).Order("`order`").Find(&got).Error
	want := []string{"场景1：开场", "场景2：姐妹重逢 · Native段1", "场景3：后续", "自定义标题"}
	for i := range want {
		if got[i].Title != want[i] {
			t.Fatalf("title %d=%q want=%q", i, got[i].Title, want[i])
		}
	}
}

func TestPreviewMaterializationRejectsDialogueDrift(t *testing.T) {
	db := newShotMaterializationDB(t)
	p := models.Project{Title: "p", Generation: 1}
	_ = db.Create(&p).Error
	sc := models.Scene{ProjectID: p.ID, EpisodeN: 1, Generation: 1, Order: 1, Title: "s"}
	_ = db.Create(&sc).Error
	_ = db.Create(&models.Dialogue{ProjectID: p.ID, SceneID: sc.ID, Order: 1, Character: "甲", SpeechType: "dialogue", Text: "原文"}).Error
	for i, text := range []string{"改写", ""} {
		_ = db.Create(&models.Shot{SceneID: sc.ID, Order: i + 1, ActType: models.ShotActSetup, ShotType: "近景", Duration: 3, Description: "镜头", Dialogue: text}).Error
	}
	if _, err := NewShotService(db).PreviewMaterialization(p.ID, sc.ID); err == nil {
		t.Fatal("dialogue drift accepted")
	}
}

func TestResetMaterializedSceneOnlyClearsUnsavedChildren(t *testing.T) {
	gpu := 2
	parent := uint(91)
	original := models.Scene{
		ID:                     7,
		ImageFile:              "selected.png",
		ImageToken:             "image-token",
		ImageTaskID:            "image-task",
		VideoTaskID:            "video-task",
		VideoFile:              "selected.mp4",
		VideoInputFile:         "selected-browser.mp4",
		VideoGPU:               &gpu,
		ImageCandidateParentID: &parent,
		VideoCandidateParentID: &parent,
		VideoFullPrompt:        "approved full prompt",
		VideoTemplate:          "minimax_h3_first_last",
		VideoFirstFrameImg:     "first.png",
		VideoLastFrameImg:      "last.png",
		ImageLocked:            true,
		VideoLocked:            true,
		Status:                 "video_ready",
	}

	existing := original
	resetMaterializedScene(&existing, original)
	if !reflect.DeepEqual(existing, original) {
		t.Fatalf("existing scene media was reset:\n got=%+v\nwant=%+v", existing, original)
	}

	child := original
	child.ID = 0
	resetMaterializedScene(&child, original)
	if child.ImageFile != "" || child.VideoFile != "" || child.VideoFirstFrameImg != "" || child.VideoLastFrameImg != "" || child.ImageLocked || child.VideoLocked {
		t.Fatalf("new child inherited source media: %+v", child)
	}
}

func TestRegroupPreservesKeeperMediaCandidatesFramesAndLocks(t *testing.T) {
	db := newShotMaterializationDB(t)
	project := models.Project{Title: "regroup", Generation: 1}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	gpu := 1
	parent := uint(31)
	scenes := []models.Scene{
		{ProjectID: project.ID, EpisodeN: 1, Generation: 1, Order: 1, Title: "场景1：会面 · Native段1", Content: "甲", Duration: 6, ImageFile: "keeper.png", ImageTaskID: "image-task", VideoTaskID: "video-task", VideoFile: "keeper.mp4", VideoInputFile: "keeper-browser.mp4", VideoGPU: &gpu, ImageCandidateParentID: &parent, VideoCandidateParentID: &parent, VideoFirstFrameImg: "first.png", VideoLastFrameImg: "last.png", ImageLocked: true, VideoLocked: true, Status: "video_ready"},
		{ProjectID: project.ID, EpisodeN: 1, Generation: 1, Order: 2, Title: "场景1：会面 · Native段2", Content: "乙", Duration: 6},
	}
	for i := range scenes {
		if err := db.Create(&scenes[i]).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&models.Shot{SceneID: scenes[i].ID, Order: 1, Duration: 6, Description: scenes[i].Content}).Error; err != nil {
			t.Fatal(err)
		}
	}
	frame := models.FrameCandidate{ProjectID: project.ID, SceneID: scenes[0].ID, VideoTaskID: "video-task", Type: models.FrameCandidateSelected, FrameIndex: 1, ImageFile: "selected-frame.png"}
	candidate := models.GenerationCandidate{ProjectID: project.ID, EntityType: "scene", EntityID: scenes[0].ID, MediaType: "video", TaskID: "video-task", File: "keeper.mp4", IsCurrent: true}
	if err := db.Create(&frame).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&candidate).Error; err != nil {
		t.Fatal(err)
	}

	regrouped, err := NewShotService(db).RegroupExistingNativeScenes(project.ID, scenes[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(regrouped) != 1 || regrouped[0].ID != scenes[0].ID {
		t.Fatalf("regrouped=%+v", regrouped)
	}
	var keeper models.Scene
	if err := db.First(&keeper, scenes[0].ID).Error; err != nil {
		t.Fatal(err)
	}
	if keeper.ImageFile != "keeper.png" || keeper.VideoFile != "keeper.mp4" || keeper.VideoInputFile != "keeper-browser.mp4" || keeper.VideoFirstFrameImg != "first.png" || keeper.VideoLastFrameImg != "last.png" || !keeper.ImageLocked || !keeper.VideoLocked {
		t.Fatalf("keeper media or locks changed: %+v", keeper)
	}
	var frameCount, candidateCount int64
	if err := db.Model(&models.FrameCandidate{}).Where("id = ? AND scene_id = ?", frame.ID, keeper.ID).Count(&frameCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.GenerationCandidate{}).Where("id = ? AND entity_id = ? AND is_current = ?", candidate.ID, keeper.ID, true).Count(&candidateCount).Error; err != nil {
		t.Fatal(err)
	}
	if frameCount != 1 || candidateCount != 1 {
		t.Fatalf("keeper associations lost: frames=%d candidates=%d", frameCount, candidateCount)
	}
}
