package service

import (
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
	caption := `{"global_metadata":"cinematic score","vocal_details":"Fully instrumental, no voices.","arrangement":"Intro: sparse strings. Outro: resolve."}`
	updated, err := svc.Update(p.ID, cue.ID, MusicCueSheetInput{EpisodeN: 1, Kind: "scene", StartTime: 0, EndTime: 3, Intensity: "medium", CaptionJSON: caption, Enabled: func() *bool { v := true; return &v }()})
	if err != nil {
		t.Fatal(err)
	}
	approved, err := svc.Review(p.ID, updated.ID, true, "reviewed")
	if err != nil {
		t.Fatal(err)
	}
	if approved.ReviewStatus != "approved" || approved.ReviewedAt == nil {
		t.Fatalf("review not recorded: %+v", approved)
	}
}

func TestRenderMusicCaptionUsesExactlyThreeHeadings(t *testing.T) {
	rendered, err := renderMusicCaption(`{"global_metadata":"score","vocal_details":"Fully instrumental","arrangement":"Intro: build. Outro: stop."}`)
	if err != nil {
		t.Fatal(err)
	}
	last := -1
	for _, heading := range []string{"### Global Metadata", "### Vocal Details", "### Arrangement"} {
		index := strings.Index(rendered, heading)
		if index <= last {
			t.Fatalf("heading order invalid: %s", rendered)
		}
		last = index
	}
	if strings.Count(rendered, "### ") != 3 {
		t.Fatalf("unexpected headings: %s", rendered)
	}
}

func TestMusicCaptionStrictContractRejectsUnsafeShapes(t *testing.T) {
	valid := `{"global_metadata":"cinematic score","vocal_details":"Fully instrumental, no voices.","arrangement":"Intro: sparse strings. Outro: resolve."}`
	if err := validateMusicCaptionJSON(valid); err != nil {
		t.Fatal(err)
	}
	invalid := []string{
		`{"global_metadata":"x","vocal_details":"Fully instrumental","arrangement":"Intro: x","lyrics_input":"[Intro]"}`,
		`{"global_metadata":"x","vocal_details":"not instrumental","arrangement":"Intro: x"}`,
		`{"global_metadata":"x","vocal_details":"lead vocal performance","arrangement":"Intro: x"}`,
		`{"global_metadata":"x","vocal_details":"Fully instrumental","arrangement":"static texture"}`,
	}
	for _, raw := range invalid {
		if err := validateMusicCaptionJSON(raw); err == nil {
			t.Fatalf("unsafe caption accepted: %s", raw)
		}
	}
}

func TestMusicCuePreflightRejectsBrokenBinding(t *testing.T) {
	db := newMusicCueTestDB(t)
	svc := NewMusicCueService(db)
	p := models.Project{Title: "p"}
	db.Create(&p)
	scene := models.Scene{ProjectID: p.ID, EpisodeN: 1, Duration: 5}
	db.Create(&scene)
	caption := `{"global_metadata":"score","vocal_details":"Fully instrumental","arrangement":"Intro: build. Outro: stop."}`
	cue, err := svc.Create(p.ID, MusicCueSheetInput{EpisodeN: 1, SceneID: &scene.ID, Kind: "scene", StartTime: 0, EndTime: 5, CaptionJSON: caption})
	if err != nil {
		t.Fatal(err)
	}
	db.Model(cue).Update("scene_id", 999)
	if _, err := svc.Review(p.ID, cue.ID, true, ""); err == nil {
		t.Fatal("broken binding was approved")
	}
	result, err := svc.Preflight(p.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, issue := range result.Issues {
		found = found || issue.Code == "binding"
	}
	if !found {
		t.Fatalf("broken binding not reported: %+v", result.Issues)
	}
}

func TestMusicCuePreflightReportsOverlapAndInvalidCaption(t *testing.T) {
	db := newMusicCueTestDB(t)
	svc := NewMusicCueService(db)
	p := models.Project{Title: "p"}
	db.Create(&p)
	db.Create(&models.Scene{ProjectID: p.ID, EpisodeN: 1, Generation: 1, Order: 1, Duration: 10})
	valid := `{"global_metadata":"score","vocal_details":"Fully instrumental","arrangement":"Intro: build. Outro: stop."}`
	inputs := []MusicCueSheetInput{{EpisodeN: 1, Kind: "scene", StartTime: 0, EndTime: 6, CaptionJSON: valid}, {EpisodeN: 1, Kind: "action", StartTime: 5, EndTime: 12, CaptionJSON: "{}"}}
	for _, input := range inputs {
		if _, err := svc.Create(p.ID, input); err != nil {
			t.Fatal(err)
		}
	}
	result, err := svc.Preflight(p.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if result.Ready || result.TotalDuration != 10 {
		t.Fatalf("unexpected preflight: %+v", result)
	}
	codes := map[string]bool{}
	for _, issue := range result.Issues {
		codes[issue.Code] = true
	}
	for _, code := range []string{"overlap", "timeline", "caption", "review"} {
		if !codes[code] {
			t.Fatalf("missing %s: %+v", code, result.Issues)
		}
	}
	db.Model(&models.Scene{}).Where("project_id = ?", p.ID).Update("title", "changed")
	result, err = svc.Preflight(p.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	codes = map[string]bool{}
	for _, issue := range result.Issues {
		codes[issue.Code] = true
	}
	if !codes["source"] {
		t.Fatalf("source drift not detected: %+v", result.Issues)
	}
}

func TestMusicCueSourceHashIncludesEditableCueContent(t *testing.T) {
	cue := models.MusicCueSheet{Kind: "scene", StartTime: 0, EndTime: 5, Intensity: "medium", CaptionJSON: `{"global_metadata":"a"}`, Enabled: true}
	first := musicCueSourceHash("context", &cue)
	cue.CaptionJSON = `{"global_metadata":"b"}`
	if first == musicCueSourceHash("context", &cue) {
		t.Fatal("caption edit did not change source hash")
	}
	cue.CaptionJSON = `{"global_metadata":"a"}`
	cue.StartTime = 1
	if first == musicCueSourceHash("context", &cue) {
		t.Fatal("timeline edit did not change source hash")
	}
}

func TestMusicCueApprovalRejectsOutOfEpisodeRange(t *testing.T) {
	db := newMusicCueTestDB(t)
	svc := NewMusicCueService(db)
	p := models.Project{Title: "p"}
	db.Create(&p)
	db.Create(&models.Scene{ProjectID: p.ID, EpisodeN: 1, Generation: 1, Order: 1, Duration: 5})
	cue, err := svc.Create(p.ID, MusicCueSheetInput{EpisodeN: 1, Kind: "scene", StartTime: 0, EndTime: 8, CaptionJSON: `{"global_metadata":"score","vocal_details":"Fully instrumental","arrangement":"Intro: build. Outro: stop."}`})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Review(p.ID, cue.ID, true, ""); err == nil {
		t.Fatal("out-of-range cue approved")
	}
}

func TestMusicCuePreflightRejectsEmptySheet(t *testing.T) {
	db := newMusicCueTestDB(t)
	svc := NewMusicCueService(db)
	p := models.Project{Title: "p"}
	db.Create(&p)
	db.Create(&models.Scene{ProjectID: p.ID, EpisodeN: 1, Duration: 5})
	result, err := svc.Preflight(p.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if result.Ready || len(result.Issues) != 1 || result.Issues[0].Code != "empty" {
		t.Fatalf("unexpected empty preflight: %+v", result)
	}
}

func TestDraftMusicCuesPreservesApprovedSheet(t *testing.T) {
	db := newMusicCueTestDB(t)
	if err := db.AutoMigrate(&models.Skill{}, &models.ProjectSkillConfig{}, &models.SkillAuditLog{}); err != nil {
		t.Fatal(err)
	}
	p := models.Project{Title: "p"}
	db.Create(&p)
	db.Create(&models.Scene{ProjectID: p.ID, EpisodeN: 1, Duration: 5})
	db.Create(&models.MusicCueSheet{ProjectID: p.ID, EpisodeN: 1, ReviewStatus: "approved", Enabled: true})
	svc := NewMusicCueService(db, &stubTextProvider{response: `{"cues":[]}`}, NewSkillService(db))
	if _, err := svc.DraftEpisode(p.ID, 1, ""); err == nil || !strings.Contains(err.Error(), "已审核Cue") {
		t.Fatalf("approved sheet was not protected: %v", err)
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
	provider := &stubTextProvider{response: `{"cues":[{"scene_id":1,"shot_id":null,"kind":"action","start_time":0,"end_time":12,"intensity":"climax","mood":"epic","instrument_note":"古琴与战鼓","user_requirements":"不要电子鼓","caption":{"global_metadata":"Cinematic xianxia score, escalating tension.","vocal_details":"Fully instrumental, no voices, choir, chanting or humming.","arrangement":"Intro: sparse guqin. Instrumental: strings and drums enter. Outro: sudden stop."}}]}`}
	skills := NewSkillService(db)
	if err := skills.InitSystemSkills(); err != nil {
		t.Fatal(err)
	}
	svc := NewMusicCueService(db, provider, skills)
	rows, err := svc.DraftEpisode(project.ID, 1, "古琴；对白时克制；不要电子鼓")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ReviewStatus != "draft" || rows[0].SceneID == nil || *rows[0].SceneID != 1 || !strings.Contains(rows[0].UserRequirements, "不要电子鼓") {
		t.Fatalf("unexpected cues: %+v", rows)
	}
	if err := validateMusicCaptionJSON(rows[0].CaptionJSON); err != nil {
		t.Fatal(err)
	}
}

func TestRegenerateMusicCuePreservesTimelineAndReturnsDraft(t *testing.T) {
	db := newMusicCueTestDB(t)
	if err := db.AutoMigrate(&models.Skill{}, &models.ProjectSkillConfig{}, &models.SkillAuditLog{}, &models.MusicCandidate{}); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "p"}
	db.Create(&project)
	db.Create(&models.Scene{ProjectID: project.ID, EpisodeN: 1, Generation: 1, Order: 1, Duration: 12, Title: "scene"})
	caption := `{"global_metadata":"old","vocal_details":"Fully instrumental","arrangement":"old"}`
	cue := models.MusicCueSheet{ProjectID: project.ID, EpisodeN: 1, Kind: "emotion", StartTime: 2, EndTime: 8, Intensity: "soft", UserRequirements: "古典吉他", CaptionJSON: caption, SourceVersionHash: "old", ReviewStatus: "approved", Enabled: true}
	db.Create(&cue)
	provider := &stubTextProvider{response: `{"cue":{"kind":"emotion","start_time":2,"end_time":8,"intensity":"medium","mood":"romantic","instrument_note":"nylon guitar","caption":{"global_metadata":"intimate score","vocal_details":"Fully instrumental, no voices.","arrangement":"Intro: fingerpicked guitar. Outro: harmonics."}}}`}
	skills := NewSkillService(db)
	if err := skills.InitSystemSkills(); err != nil {
		t.Fatal(err)
	}
	svc := NewMusicCueService(db, provider, skills)
	updated, err := svc.RegenerateCue(project.ID, cue.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.StartTime != 2 || updated.EndTime != 8 || updated.ReviewStatus != "draft" || updated.InstrumentNote != "nylon guitar" {
		t.Fatalf("unexpected regenerated cue: %+v", updated)
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
