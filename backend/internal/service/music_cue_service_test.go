package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"comfyui-console/internal/models"
)

func newMusicCueTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Project{}, &models.Scene{}, &models.Shot{}, &models.Dialogue{}, &models.MusicCueSheet{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestMusicCueSheetCRUDIsScopedToProject(t *testing.T) {
	db := newMusicCueTestDB(t)
	svc := NewMusicCueService(db)
	p1 := models.Project{Title: "one"}
	p2 := models.Project{Title: "two"}
	db.Create(&p1)
	db.Create(&p2)
	scene1 := models.Scene{ProjectID: p1.ID, EpisodeN: 1, Generation: 1, Order: 1, Duration: 10}
	db.Create(&scene1)

	created, err := svc.Create(p1.ID, MusicCueSheetInput{
		EpisodeN: 1, SceneID: &scene1.ID, Kind: "action",
		StartTime: 1.0, EndTime: 4.0, Intensity: "intense", Mood: "tension",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ProjectID != p1.ID || created.Kind != "action" || created.Intensity != "intense" || created.ReviewStatus != "draft" {
		t.Fatalf("unexpected cue: %+v", created)
	}

	other, err := svc.Create(p2.ID, MusicCueSheetInput{
		EpisodeN: 1, Kind: "scene", StartTime: 0, EndTime: 3, Intensity: "medium",
	})
	if err != nil {
		t.Fatal(err)
	}
	stale := models.MusicCueSheet{ProjectID: p1.ID, EpisodeN: 1, Kind: "bgm", StartTime: 0, EndTime: 3, Enabled: true, Source: "manual"}
	disabled := models.MusicCueSheet{ProjectID: p1.ID, EpisodeN: 1, Kind: "bgm", StartTime: 0, EndTime: 3, Enabled: true, Source: "manual"}
	for _, hidden := range []*models.MusicCueSheet{&stale, &disabled} {
		if err := db.Create(hidden).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Model(&stale).Update("stale", true).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&disabled).Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	cues, err := svc.List(p1.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(cues) != 1 || cues[0].ID != created.ID {
		t.Fatalf("project-scoped list returned %+v", cues)
	}

	if _, err := svc.Update(p2.ID, created.ID, MusicCueSheetInput{EpisodeN: 1, Kind: "bgm", StartTime: 0, EndTime: 2, Intensity: "soft"}); err == nil {
		t.Fatal("cross-project update should fail")
	}
	if err := svc.Delete(p1.ID, other.ID); err == nil {
		t.Fatal("cross-project delete should fail")
	}
	if err := svc.Delete(p1.ID, created.ID); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&models.MusicCueSheet{}).Where("id = ?", created.ID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("owned cue was not deleted")
	}
}

func TestProjectMusicLibraryPersistsReusableThemesOutsideEpisodeTimeline(t *testing.T) {
	db := newMusicCueTestDB(t)
	project := models.Project{Title: "reusable score"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewMusicCueService(db)
	created, err := svc.Create(project.ID, MusicCueSheetInput{
		Scope: "library", ThemeCode: "sister_bond", Name: "姐妹主题",
		EpisodeN: 0, Kind: "emotion", StartTime: 0, EndTime: 90, Duration: 90,
		Intensity: "soft", Mood: "romantic", Tags: "姐妹,温情,重逢", Source: "manual",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Scope != "library" || created.EpisodeN != 0 || created.ThemeCode != "sister_bond" {
		t.Fatalf("created=%+v", created)
	}
	rows, err := svc.ListLibrary(project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != created.ID || rows[0].Name != "姐妹主题" {
		t.Fatalf("library rows=%+v", rows)
	}
	if _, err := svc.Create(project.ID, MusicCueSheetInput{Scope: "library", Kind: "bgm", StartTime: 0, EndTime: 60}); err == nil {
		t.Fatal("library track without stable identity was accepted")
	}
}

func TestMusicCueListDoesNotInvalidateSavedPlanAfterPolicyOrServiceRestart(t *testing.T) {
	db := newMusicCueTestDB(t)
	project := models.Project{Title: "persisted music plan"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	cue := models.MusicCueSheet{
		ProjectID: project.ID, EpisodeN: 1, Kind: "bgm", StartTime: 0, EndTime: 157,
		Enabled: true, Source: "ai", SourceVersionHash: "music3-older-policy:stored",
		ReviewStatus: "draft", CaptionJSON: `{"global_metadata":"saved","vocal_details":"Instrumental","arrangement":"Intro to Outro","lyrics_input":""}`,
	}
	if err := db.Create(&cue).Error; err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		// Reconstruct the service to model a backend restart.
		rows, err := NewMusicCueService(db).List(project.ID, 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 || rows[0].ID != cue.ID || rows[0].Stale || !rows[0].Enabled {
			t.Fatalf("saved plan disappeared after list/restart attempt %d: %+v", attempt+1, rows)
		}
	}
	var persisted models.MusicCueSheet
	if err := db.First(&persisted, cue.ID).Error; err != nil {
		t.Fatal(err)
	}
	if persisted.Stale || !persisted.Enabled || persisted.SourceVersionHash != cue.SourceVersionHash {
		t.Fatalf("listing mutated saved plan: %+v", persisted)
	}
}

func TestGeneratedMusicCueNormalizesStructuredCaptionFields(t *testing.T) {
	raw := `{"cues":[{"kind":"bgm","start_time":0,"end_time":120,"intensity":"soft","mood":"calm","instrument_note":"guqin","user_requirements":"instrumental","caption":{"global_metadata":{"genre":"cinematic xianxia","tempo":["72 BPM","steady"]},"vocal_details":{"mode":"Fully instrumental","exclude":["narration","humming"]},"arrangement":[{"intro":"solo guqin"},{"development":"low strings"}],"lyrics_input":["[Intro]","[Instrumental]","[Outro]"]}}]}`
	var generated generatedMusicCueSheet
	if err := json.Unmarshal([]byte(raw), &generated); err != nil {
		t.Fatalf("structured caption should normalize instead of failing: %v", err)
	}
	if len(generated.Cues) != 1 {
		t.Fatalf("cues=%d", len(generated.Cues))
	}
	caption := generated.Cues[0].Caption
	combined := caption.GlobalMetadata + "\n" + caption.VocalDetails + "\n" + caption.Arrangement
	for _, expected := range []string{"genre: cinematic xianxia", "tempo: 72 BPM; steady", "mode: Fully instrumental", "exclude: narration; humming", "intro: solo guqin", "development: low strings"} {
		if !strings.Contains(combined, expected) {
			t.Fatalf("normalized caption missing %q: %+v", expected, caption)
		}
	}
	if caption.LyricsInput != "[Intro]; [Instrumental]; [Outro]" {
		t.Fatalf("lyrics_input=%q", caption.LyricsInput)
	}
	caption.LyricsInput = ""
	encoded, err := json.Marshal(caption)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateMusicCaptionJSON(string(encoded)); err != nil {
		t.Fatalf("normalized caption did not pass validation: %v", err)
	}
}

func TestParseGeneratedMusicCueSheetExtractsEnvelopeAndEscapesControls(t *testing.T) {
	raw := "draft follows\n```json\n{\"cues\":[{\"kind\":\"bgm\",\"start_time\":0,\"end_time\":10,\"caption\":{\"global_metadata\":\"cinematic\",\"vocal_details\":\"Fully instrumental\",\"arrangement\":\"Intro:\nstrings\"}}]}\n```\nend"
	generated, err := parseGeneratedMusicCueSheet(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(generated.Cues) != 1 || !strings.Contains(generated.Cues[0].Caption.Arrangement, "strings") {
		t.Fatalf("generated=%+v", generated)
	}
}

func TestMusicCaptionNegativeConstraintsDoNotTriggerForbiddenStyleOrVocalErrors(t *testing.T) {
	caption := musicStructuredCaption{
		GlobalMetadata: "East Asian fantasy story score with lyrical theme and deep scenic atmosphere. No Hollywood trailer music, EDM, jazz groove, or pop song structure.",
		VocalDetails:   "Fully instrumental; no singing, choir, humming, spoken word, or English lyrics.",
		Arrangement:    "Spacious phrasing follows the story environment; 禁止英雄铜管固定音型，不使用摇滚鼓组，不采用流行歌曲律动。",
		LyricsInput:    "",
	}
	encoded, err := json.Marshal(caption)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateMusicCaptionJSON(string(encoded)); err != nil {
		t.Fatalf("negative constraints were treated as affirmative content: %v", err)
	}
	caption.Arrangement = "Heroic brass ostinato drives a Hollywood trailer climax."
	if !musicCaptionHasForbiddenWesternStyle(caption) {
		t.Fatal("affirmative forbidden style was not detected")
	}
	caption.Arrangement = "A singer performs an English vocal melody."
	if !musicCaptionHasVocalContent(caption) {
		t.Fatal("affirmative vocal content was not detected")
	}
}

func TestMusicCaptionAllowsOneChinesePopDirectionWithoutGenreMixPenalty(t *testing.T) {
	caption := musicStructuredCaption{
		GlobalMetadata: "Lyrical Mandopop-style instrumental screen score for a contemporary Chinese emotional drama.",
		VocalDetails:   "Fully instrumental.",
		Arrangement:    "A memorable Chinese pop melodic contour develops with piano, restrained rock drums, and a brief jazz-inflected turnaround that supports the story environment.",
	}
	if !musicCaptionHasChineseScreenIdentity(caption) {
		t.Fatal("Mandopop instrumental direction was not recognized")
	}
	if musicCaptionDirectionCount(caption) != 1 {
		t.Fatalf("direction count=%d", musicCaptionDirectionCount(caption))
	}
	if musicCaptionHasForbiddenWesternStyle(caption) {
		t.Fatal("allowed Chinese pop/rock/jazz inflection was rejected")
	}
	encoded, _ := json.Marshal(caption)
	if err := validateMusicCaptionJSON(string(encoded)); err != nil {
		t.Fatal(err)
	}
	caption.GlobalMetadata += " Also use East Asian fantasy and wuxia action as equal primary directions."
	if musicCaptionDirectionCount(caption) <= 1 {
		t.Fatal("ambiguous multi-direction blend was not detected")
	}
}

func TestMusicCaptionValidationRequiresOrientalIdentityAndRejectsLyrics(t *testing.T) {
	valid := `{"global_metadata":"East Asian fantasy story score with a lyrical theme","vocal_details":"Fully instrumental; no vocals.","arrangement":"Spacious phrasing and poetic restraint follow the story environment without prescribing instruments.","lyrics_input":""}`
	if err := validateMusicCaptionJSON(valid); err != nil {
		t.Fatal(err)
	}
	western := `{"global_metadata":"East Asian fantasy with Hollywood trailer scale","vocal_details":"Fully instrumental","arrangement":"A lyrical theme over heroic brass ostinato","lyrics_input":""}`
	if err := validateMusicCaptionJSON(western); err == nil {
		t.Fatal("Western trailer caption was accepted without oriental identity")
	}
	vocal := `{"global_metadata":"East Asian fantasy story score","vocal_details":"Fully instrumental","arrangement":"A lyrical theme follows the story environment","lyrics_input":"[Verse]\nFly into the night"}`
	if err := validateMusicCaptionJSON(vocal); err == nil {
		t.Fatal("lyric text was accepted for instrumental score")
	}
}

func TestMusicCueSheetValidation(t *testing.T) {
	db := newMusicCueTestDB(t)
	svc := NewMusicCueService(db)
	p := models.Project{Title: "proj"}
	db.Create(&p)

	valid := MusicCueSheetInput{EpisodeN: 1, Kind: "scene", StartTime: 0, EndTime: 3, Intensity: "medium"}
	for name, mutate := range map[string]func(*MusicCueSheetInput){
		"invalid kind":      func(in *MusicCueSheetInput) { in.Kind = "trailer" },
		"negative start":    func(in *MusicCueSheetInput) { in.StartTime = -1 },
		"reversed times":    func(in *MusicCueSheetInput) { in.EndTime = in.StartTime },
		"invalid intensity": func(in *MusicCueSheetInput) { in.Intensity = "loud" },
		"invalid mood":      func(in *MusicCueSheetInput) { in.Mood = "happy" },
		"invalid episode":   func(in *MusicCueSheetInput) { in.EpisodeN = 0 },
		"invalid source":    func(in *MusicCueSheetInput) { in.Source = "auto" },
		"invalid review":    func(in *MusicCueSheetInput) { in.ReviewStatus = "approveded" },
		"bad caption_json":  func(in *MusicCueSheetInput) { in.CaptionJSON = "not json" },
	} {
		t.Run(name, func(t *testing.T) {
			input := valid
			mutate(&input)
			if _, err := svc.Create(p.ID, input); err == nil {
				t.Fatalf("expected validation error for %s", name)
			}
		})
	}
}

func TestMusicCueSheetSceneBinding(t *testing.T) {
	db := newMusicCueTestDB(t)
	svc := NewMusicCueService(db)
	p1 := models.Project{Title: "one"}
	p2 := models.Project{Title: "two"}
	db.Create(&p1)
	db.Create(&p2)
	scene1 := models.Scene{ProjectID: p1.ID, EpisodeN: 1, Generation: 1, Order: 1, Duration: 5}
	scene2 := models.Scene{ProjectID: p2.ID, EpisodeN: 1, Generation: 1, Order: 1, Duration: 5}
	db.Create(&scene1)
	db.Create(&scene2)

	// Cue bound to scene from another project should be rejected
	_, err := svc.Create(p1.ID, MusicCueSheetInput{
		EpisodeN: 1, SceneID: &scene2.ID, Kind: "action",
		StartTime: 0, EndTime: 2, Intensity: "intense",
	})
	if err == nil {
		t.Fatal("scene from another project should be rejected")
	}

	// Cue times are episode-relative; Scene binding verifies ownership, not a scene-local clock.
	if _, err = svc.Create(p1.ID, MusicCueSheetInput{
		EpisodeN: 1, SceneID: &scene1.ID, Kind: "scene",
		StartTime: 6, EndTime: 8, Intensity: "medium",
	}); err != nil {
		t.Fatalf("episode-relative cue was rejected: %v", err)
	}
}

func TestMusicCueSheetShotBinding(t *testing.T) {
	db := newMusicCueTestDB(t)
	svc := NewMusicCueService(db)
	p := models.Project{Title: "proj"}
	db.Create(&p)
	scene := models.Scene{ProjectID: p.ID, EpisodeN: 1, Generation: 1, Order: 1, Duration: 10}
	db.Create(&scene)
	shot := models.Shot{SceneID: scene.ID, Order: 1}
	db.Create(&shot)
	otherScene := models.Scene{ProjectID: p.ID, EpisodeN: 1, Generation: 1, Order: 2, Duration: 10}
	db.Create(&otherScene)
	otherShot := models.Shot{SceneID: otherScene.ID, Order: 1}
	db.Create(&otherShot)

	// Shot bound to different scene should be rejected
	_, err := svc.Create(p.ID, MusicCueSheetInput{
		EpisodeN: 1, SceneID: &scene.ID, ShotID: &otherShot.ID, Kind: "emotion",
		StartTime: 0, EndTime: 3, Intensity: "soft",
	})
	if err == nil {
		t.Fatal("shot from different scene should be rejected")
	}

	// Shot without scene binding should be rejected
	_, err = svc.Create(p.ID, MusicCueSheetInput{
		EpisodeN: 1, ShotID: &shot.ID, Kind: "emotion",
		StartTime: 0, EndTime: 3, Intensity: "soft",
	})
	if err == nil {
		t.Fatal("shot without scene should be rejected")
	}
}

func TestMusicCueSheetMarkSceneStale(t *testing.T) {
	db := newMusicCueTestDB(t)
	svc := NewMusicCueService(db)
	p := models.Project{Title: "proj"}
	db.Create(&p)
	scene := models.Scene{ProjectID: p.ID, EpisodeN: 1, Generation: 1, Order: 1, Duration: 10}
	db.Create(&scene)

	cue, err := svc.Create(p.ID, MusicCueSheetInput{
		EpisodeN: 1, SceneID: &scene.ID, Kind: "action",
		StartTime: 0, EndTime: 3, Intensity: "intense",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cue.Stale {
		t.Fatal("new cue should not be stale")
	}

	if err := svc.MarkSceneStale(p.ID, scene.ID, "scene content changed"); err != nil {
		t.Fatal(err)
	}
	var updated models.MusicCueSheet
	if err := db.First(&updated, cue.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !updated.Stale || updated.StaleReason != "scene content changed" {
		t.Fatalf("cue should be marked stale: %+v", updated)
	}
}

func TestMusicCueSheetReviewGateRequiresStructuredCaption(t *testing.T) {
	db := newMusicCueTestDB(t)
	svc := NewMusicCueService(db)
	p := models.Project{Title: "proj"}
	db.Create(&p)
	db.Create(&models.Scene{ProjectID: p.ID, EpisodeN: 1, Generation: 1, Order: 1, Duration: 3, Title: "scene"})
	cue, err := svc.Create(p.ID, MusicCueSheetInput{EpisodeN: 1, Kind: "scene", StartTime: 0, EndTime: 3, Intensity: "medium", ReviewStatus: "approved"})
	if err != nil {
		t.Fatal(err)
	}
	if cue.ReviewStatus != "draft" {
		t.Fatalf("CRUD bypassed review gate: %s", cue.ReviewStatus)
	}
	if _, err := svc.Review(p.ID, cue.ID, true, "reviewed"); err == nil {
		t.Fatal("empty Structured Caption was approved")
	}
	caption := `{"global_metadata":"East Asian fantasy story score with a lyrical, memorable theme","vocal_details":"Fully instrumental, no voices.","arrangement":"Intro: spacious phrasing follows the story environment. Outro: resolve with poetic restraint.","lyrics_input":""}`
	if err := db.Model(cue).Update("caption_json", caption).Error; err != nil {
		t.Fatal(err)
	}
	approved, err := svc.Review(p.ID, cue.ID, true, "reviewed")
	if err != nil {
		t.Fatal(err)
	}
	if approved.ReviewStatus != "approved" || approved.ReviewedAt == nil {
		t.Fatalf("review not recorded: %+v", approved)
	}
}

func TestDraftMusicCuesUsesUserRequirementsAndRewriterSchema(t *testing.T) {
	db := newMusicCueTestDB(t)
	if err := db.AutoMigrate(&models.Skill{}, &models.ProjectSkillConfig{}, &models.SkillAuditLog{}); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "仙侠短剧", Genre: "仙侠"}
	db.Create(&project)
	db.Create(&models.Scene{ProjectID: project.ID, EpisodeN: 1, Generation: 1, Order: 1, Duration: 12, Title: "决战", Content: "女主迎战"})
	old := models.MusicCueSheet{ProjectID: project.ID, EpisodeN: 1, Kind: "bgm", StartTime: 0, EndTime: 12, Source: "ai", Enabled: true, ReviewStatus: "approved", SourceVersionHash: "old-policy", UserRequirements: "固定古琴核心配器"}
	if err := db.Create(&old).Error; err != nil {
		t.Fatal(err)
	}
	provider := &stubTextProvider{response: `{"cues":[{"kind":"action","start_time":0,"end_time":12,"intensity":"climax","mood":"epic","instrument_note":"核心配器为古琴与战鼓","user_requirements":"AI自行增加的固定配器要求","caption":{"global_metadata":"Cinematic xianxia score, escalating tension.","vocal_details":"Fully instrumental, no voices, choir, chanting or humming.","arrangement":"Intro: sparse guqin. Instrumental: strings and drums enter. Outro: sudden stop.","lyrics_input":"[Intro]\n[Instrumental]\n[Outro]"}}]}`}
	skills := NewSkillService(db)
	if err := skills.InitSystemSkills(); err != nil {
		t.Fatal(err)
	}
	svc := NewMusicCueService(db, provider, skills)
	rows, err := svc.DraftEpisode(project.ID, 1, "符合故事环境；对白时克制；不要规定乐器")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ReviewStatus != "draft" || rows[0].UserRequirements != "符合故事环境；对白时克制；不要规定乐器" || rows[0].InstrumentNote != "" {
		t.Fatalf("AI output became authoritative requirements/instrument mandate: %+v", rows)
	}
	var archived models.MusicCueSheet
	if err := db.First(&archived, old.ID).Error; err != nil {
		t.Fatal(err)
	}
	if archived.Enabled || !archived.Stale {
		t.Fatalf("old approved AI plan remains current after explicit regeneration: %+v", archived)
	}
	listed, err := svc.List(project.ID, 1)
	if err != nil || len(listed) != 1 || listed[0].ID != rows[0].ID {
		t.Fatalf("current list still includes archived AI plan: %+v err=%v", listed, err)
	}
	if err := validateMusicCaptionJSON(rows[0].CaptionJSON); err != nil {
		t.Fatal(err)
	}
}

func TestMusicCueSheetEpisodeStale(t *testing.T) {
	db := newMusicCueTestDB(t)
	svc := NewMusicCueService(db)
	p := models.Project{Title: "proj"}
	db.Create(&p)

	cue1, err := svc.Create(p.ID, MusicCueSheetInput{
		EpisodeN: 1, Kind: "scene", StartTime: 0, EndTime: 3, Intensity: "medium",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cue1.ID == 0 {
		t.Fatal("cue1.ID should not be 0")
	}
	cue2, err := svc.Create(p.ID, MusicCueSheetInput{
		EpisodeN: 2, Kind: "action", StartTime: 0, EndTime: 3, Intensity: "intense",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cue2.ID == 0 {
		t.Fatal("cue2.ID should not be 0")
	}

	if err := svc.MarkEpisodeStale(p.ID, 1, "episode 1 script revised"); err != nil {
		t.Fatal(err)
	}
	// Use fresh structs to avoid GORM struct-cache issues with uint PK
	var stale1, stale2 models.MusicCueSheet
	if err := db.Where("episode_n = ?", 1).First(&stale1).Error; err != nil {
		t.Fatal(err)
	}
	if !stale1.Stale || stale1.StaleReason != "episode 1 script revised" {
		t.Fatalf("cue1 should be stale: %+v", stale1)
	}
	// cue2 (episode_n=2) should be unaffected
	if err := db.Where("episode_n = ?", 2).First(&stale2).Error; err != nil {
		t.Fatal(err)
	}
	if stale2.Stale {
		t.Fatalf("cue2 should not be stale: %+v", stale2)
	}
}
