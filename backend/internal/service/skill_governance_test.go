package service

import (
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

// TestSkillOutputRuleIsSharedButNotAppliedToApprovedSkills pins the approved P5-3 wording and
// keeps it off the already approved Skills: their prompts and versions must not change.
// When a new Skill adopts the rule, allow that Skill's code here instead of rewriting wording.
func TestSkillOutputRuleIsSharedButNotAppliedToApprovedSkills(t *testing.T) {
	const approved = "只输出最终结果：不输出思考过程，不使用 Markdown 或代码块，不输出解释、前言或总结。"
	if skillOutputRule != approved {
		t.Fatalf("shared output rule = %q", skillOutputRule)
	}
	if got := withSkillOutputRule("BASE"); got != "BASE\n"+approved {
		t.Fatalf("append = %q", got)
	}
	if got := withSkillOutputRule("BASE\n" + approved); got != "BASE\n"+approved {
		t.Fatalf("a prompt already carrying the rule must not repeat it: %q", got)
	}
	if got := withSkillOutputRule("   "); got != approved {
		t.Fatalf("empty prompt = %q", got)
	}

	db := newTestDB(t)
	svc := NewSkillService(db)
	if err := svc.InitSystemSkills(); err != nil {
		t.Fatal(err)
	}
	var skills []models.Skill
	if err := db.Find(&skills).Error; err != nil {
		t.Fatal(err)
	}
	if len(skills) == 0 {
		t.Fatal("no seeded skills")
	}
	for _, skill := range skills {
		if strings.Contains(skill.SystemPrompt, skillOutputRule) {
			t.Errorf("skill %s v%d already carries the new rule; approved wording must stay unchanged", skill.Code, skill.Version)
		}
	}
}

func TestStageDefaultSkillIgnoresSpecializedSkills(t *testing.T) {
	db := newTestDB(t)
	svc := NewSkillService(db)
	if err := svc.InitSystemSkills(); err != nil {
		t.Fatal(err)
	}
	for stage, want := range map[string]string{
		models.SkillStageImagePrompt:      "image-prompt-optimize",
		models.SkillStageVideoPrompt:      "video-prompt-enhance",
		models.SkillStageCharacter:        "character-standard",
		models.SkillStageContinuityReview: "novel-continuity-review",
	} {
		skill, err := svc.GetEffectiveSkill(1, stage)
		if err != nil || skill == nil || skill.Code != want {
			t.Errorf("stage %s default = %+v err=%v, want %s", stage, skill, err, want)
		}
	}
	// Storyboard script calls carry the current rules in their own prompts; the outdated generic
	// storyboard Skill is no longer appended by default.
	if skill, err := svc.GetEffectiveSkill(1, models.SkillStageStoryboard); err != nil || skill != nil {
		t.Fatalf("storyboard stage must have no default skill, got %+v err=%v", skill, err)
	}
	provider := &captureTextProvider{}
	if _, err := svc.ChatWithSkill(1, models.SkillStageStoryboard, provider, "BASE", "USER", nil); err != nil {
		t.Fatal(err)
	}
	if provider.system != "BASE" || provider.user != "USER" {
		t.Fatalf("storyboard call must not append a default skill: system=%q user=%q", provider.system, provider.user)
	}
	// A stage without a generic Skill has no default instead of borrowing a specialized one.
	if skill, err := svc.GetEffectiveSkill(1, models.SkillStageQwenImageEdit); err == nil || skill != nil {
		t.Fatalf("qwen edit stage must have no stage default, got %+v err=%v", skill, err)
	}
	// Specialized Skills still resolve by their own operation.
	skill, err := svc.GetEffectiveOperationSkill(1, models.SkillStageStoryboard, sceneContentExpandSkillCode)
	if err != nil || skill.Code != sceneContentExpandSkillCode {
		t.Fatalf("specialized lookup = %+v err=%v", skill, err)
	}
}

func TestChatWithSkillTracedReturnsAuditedProvenance(t *testing.T) {
	db := newTestDB(t)
	svc := NewSkillService(db)
	if err := svc.InitSystemSkills(); err != nil {
		t.Fatal(err)
	}
	provider := &captureTextProvider{response: "ok"}
	out, provenance, err := svc.ChatWithConfiguredOrFallbackSkillTraced(3, models.SkillStageStoryboard, shotRefinePatchSkillCode, provider, "BASE", "USER", map[string]string{"instruction": "x"})
	if err != nil || out != "ok" || provenance == nil {
		t.Fatalf("out=%q provenance=%+v err=%v", out, provenance, err)
	}
	if provenance.Code != shotRefinePatchSkillCode || provenance.Version != 1 || provenance.InputSHA256 != skillInputSHA256(provider.user) {
		t.Fatalf("provenance=%+v", provenance)
	}
	var audit models.SkillAuditLog
	if err := db.Where("project_id = ?", 3).First(&audit).Error; err != nil || audit.InputHash != provenance.InputSHA256 || audit.SkillVersion != provenance.Version {
		t.Fatalf("audit=%+v err=%v", audit, err)
	}
	if !svc.VerifySkillProvenance(3, provenance) {
		t.Fatal("audited provenance must verify")
	}
	forged := *provenance
	forged.InputSHA256 = skillInputSHA256("forged")
	if svc.VerifySkillProvenance(3, &forged) || svc.VerifySkillProvenance(4, provenance) || svc.VerifySkillProvenance(3, nil) {
		t.Fatal("forged, foreign-project or missing provenance must not verify")
	}
	// A project-disabled Skill sends the base prompts unchanged and reports no provenance.
	disabled := models.ProjectSkillConfig{ProjectID: 5, Stage: models.SkillStageStoryboard, Operation: shotRefinePatchSkillCode}
	db.Create(&disabled)
	db.Model(&disabled).Update("enabled", false) // default:true ignores a zero value on Create
	if _, provenance, err := svc.ChatWithConfiguredOrFallbackSkillTraced(5, models.SkillStageStoryboard, shotRefinePatchSkillCode, provider, "BASE", "USER", nil); err != nil || provenance != nil || provider.user != "USER" {
		t.Fatalf("disabled skill provenance=%+v user=%q err=%v", provenance, provider.user, err)
	}
}

func TestInitSystemSkillsMigratesGenericOperationAndTagsModelFamilies(t *testing.T) {
	db := newTestDB(t)
	if err := db.AutoMigrate(&models.ProjectSkillConfig{}); err != nil {
		t.Fatal(err)
	}
	// A database seeded before the fix: generic Skill operation equals its code, and a project
	// config was saved under that code.
	old := models.Skill{Name: "分镜剧本生成", Code: "storyboard-generate", Operation: "storyboard-generate", Version: 1, Stage: models.SkillStageStoryboard, IsSystem: true, Enabled: true, SortOrder: 1}
	db.Create(&old)
	db.Create(&models.ProjectSkillConfig{ProjectID: 7, Stage: models.SkillStageStoryboard, Operation: "storyboard-generate", SkillID: &old.ID, Enabled: true})
	svc := NewSkillService(db)
	if err := svc.InitSystemSkills(); err != nil {
		t.Fatal(err)
	}
	var migrated models.Skill
	db.First(&migrated, old.ID)
	if migrated.Operation != models.SkillStageStoryboard || migrated.Version != 1 {
		t.Fatalf("generic skill operation must follow the stage without a version bump: %+v", migrated)
	}
	var cfg models.ProjectSkillConfig
	db.Where("project_id = ?", 7).First(&cfg)
	if cfg.Operation != models.SkillStageStoryboard {
		t.Fatalf("project config operation = %q", cfg.Operation)
	}
	if effective, err := svc.GetEffectiveOperationSkill(7, models.SkillStageStoryboard, models.SkillStageStoryboard); err != nil || effective.ID != old.ID {
		t.Fatalf("migrated project config must still resolve: %+v err=%v", effective, err)
	}
	for code, want := range map[string]string{
		h3StoryboardImageSkillCode:  models.ModelFamilyMiniMaxH3,
		h3CombatActionSkillCode:     models.ModelFamilyMiniMaxH3,
		h3DialogueTimelineSkillCode: models.ModelFamilyMiniMaxH3,
		QwenImage21EditProgram:      models.ModelFamilyQwenImage21,
		"storyboard-generate":       "",
	} {
		var rows []models.Skill
		db.Where("code = ?", code).Find(&rows)
		if len(rows) == 0 {
			t.Fatalf("missing skill %s", code)
		}
		for _, row := range rows {
			if row.ModelFamilies != want {
				t.Errorf("%s v%d model_families=%q want %q", code, row.Version, row.ModelFamilies, want)
			}
		}
	}
}
