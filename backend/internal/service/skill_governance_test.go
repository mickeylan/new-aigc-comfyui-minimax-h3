package service

import (
	"testing"

	"comfyui-console/internal/models"
)

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
