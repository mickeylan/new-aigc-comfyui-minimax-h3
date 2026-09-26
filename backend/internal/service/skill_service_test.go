package service

import (
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"comfyui-console/internal/models"
)

func newTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := db.AutoMigrate(&models.Skill{}, &models.ProjectSkillConfig{}, &models.SkillAuditLog{}, &models.Project{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestSkillService_InitSystemSkills(t *testing.T) {
	db := newTestDB(t)
	svc := NewSkillService(db)

	// 首次初始化应该成功
	if err := svc.InitSystemSkills(); err != nil {
		t.Fatalf("init system skills failed: %v", err)
	}

	// 再次初始化应该幂等
	if err := svc.InitSystemSkills(); err != nil {
		t.Fatalf("init system skills idempotent failed: %v", err)
	}

	// 检查系统技能数量
	skills, err := svc.ListSkills("", false)
	if err != nil {
		t.Fatalf("list skills: %v", err)
	}
	if len(skills) == 0 {
		t.Fatal("系统技能应该已初始化")
	}

	// 检查各阶段都有技能
	stages := []string{
		models.SkillStagePlan,
		models.SkillStageCharacter,
		models.SkillStageStoryboard,
		models.SkillStageImagePrompt,
		models.SkillStageVideoPrompt,
		models.SkillStageReview,
	}
	for _, stage := range stages {
		stageSkills, err := svc.ListSkills(stage, true)
		if err != nil {
			t.Fatalf("list skills for stage %s: %v", stage, err)
		}
		if len(stageSkills) == 0 {
			t.Errorf("阶段 %s 应该有至少一个技能", stage)
		}
	}
}

func TestSkillService_CuratedDirectorSkills(t *testing.T) {
	db := newTestDB(t)
	svc := NewSkillService(db)
	if err := svc.InitSystemSkills(); err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"director-visual-beat-decomposition", "director-shot-packet", "director-faithful-prompt-polish"} {
		var skills []models.Skill
		if err := db.Where("code = ?", code).Find(&skills).Error; err != nil {
			t.Fatal(err)
		}
		if len(skills) != 1 || !skills[0].IsSystem || !skills[0].Enabled || skills[0].Version != 1 {
			t.Fatalf("curated skill %s = %+v", code, skills)
		}
		if strings.TrimSpace(skills[0].PromptTemplate) == "" || strings.TrimSpace(skills[0].SystemPrompt) == "" {
			t.Fatalf("curated skill %s lacks prompt contract", code)
		}
	}
	if err := svc.InitSystemSkills(); err != nil {
		t.Fatal(err)
	}
	var count int64
	db.Model(&models.Skill{}).Where("code IN ?", []string{"director-visual-beat-decomposition", "director-shot-packet", "director-faithful-prompt-polish"}).Count(&count)
	if count != 3 {
		t.Fatalf("curated skills are not idempotent: count=%d", count)
	}
}

func TestSkillService_CreateSkill(t *testing.T) {
	db := newTestDB(t)
	svc := NewSkillService(db)

	// 创建自定义技能
	skill, err := svc.CreateSkill(models.Skill{
		Name:           "我的自定义技能",
		Code:           "my-custom-skill",
		Stage:          models.SkillStagePlan,
		PromptTemplate: "测试模板 {{test}}",
		SystemPrompt:   "测试系统提示词",
		Description:    "这是一个测试技能",
	})
	if err != nil {
		t.Fatalf("create skill failed: %v", err)
	}
	if skill.ID == 0 {
		t.Error("skill id 应该被设置")
	}
	if skill.IsSystem {
		t.Error("自定义技能不应该是系统技能")
	}
	if skill.Version != 1 {
		t.Errorf("版本应该是 1，实际 %d", skill.Version)
	}
}

func TestSkillService_CreateSkill_Validation(t *testing.T) {
	db := newTestDB(t)
	svc := NewSkillService(db)

	cases := []struct {
		name   string
		skill  models.Skill
		errMsg string
	}{
		{
			name:   "空名称",
			skill:  models.Skill{Code: "test", Stage: models.SkillStagePlan},
			errMsg: "技能名称不能为空",
		},
		{
			name:   "空阶段",
			skill:  models.Skill{Name: "test", Code: "test", Stage: ""},
			errMsg: "适用阶段不能为空",
		},
		{
			name:   "无效阶段",
			skill:  models.Skill{Name: "test", Code: "test", Stage: "invalid"},
			errMsg: "无效的阶段",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.CreateSkill(tc.skill)
			if err == nil {
				t.Error("应该返回错误")
			}
			if err != nil && tc.errMsg != "" && !strings.Contains(err.Error(), tc.errMsg) {
				t.Errorf("错误信息应该包含 %q，实际: %v", tc.errMsg, err)
			}
		})
	}
}

func TestSkillService_UpdateSkill(t *testing.T) {
	db := newTestDB(t)
	svc := NewSkillService(db)
	svc.InitSystemSkills()

	// 获取一个系统技能
	skills, _ := svc.ListSkills(models.SkillStagePlan, true)
	if len(skills) == 0 {
		t.Fatal("应该有系统技能")
	}
	systemSkill := skills[0]

	// 系统技能只允许更新部分字段
	updates := map[string]any{
		"description": "更新后的描述",
		"enabled":     false,
		"sort_order":  10,
	}
	updated, err := svc.UpdateSkill(systemSkill.ID, updates)
	if err != nil {
		t.Fatalf("update system skill failed: %v", err)
	}
	if updated.Description != "更新后的描述" {
		t.Errorf("描述应该更新")
	}

	// 创建自定义技能
	custom, _ := svc.CreateSkill(models.Skill{
		Name:           "测试技能",
		Code:           "test-update-skill",
		Stage:          models.SkillStageCharacter,
		PromptTemplate: "原始模板",
	})
	updates2 := map[string]any{
		"description":     "更新描述",
		"prompt_template": "更新模板",
		"system_prompt":   "更新系统提示",
	}
	updated2, err := svc.UpdateSkill(custom.ID, updates2)
	if err != nil {
		t.Fatalf("update custom skill failed: %v", err)
	}
	if updated2.PromptTemplate != "更新模板" {
		t.Errorf("模板应该更新")
	}

	// 系统技能不允许更新 code
	badUpdates := map[string]any{"code": "new-code"}
	_, err = svc.UpdateSkill(systemSkill.ID, badUpdates)
	if err == nil {
		t.Error("系统技能不应该允许更新 code")
	}
}

func TestSkillService_DeleteSkill(t *testing.T) {
	db := newTestDB(t)
	svc := NewSkillService(db)
	svc.InitSystemSkills()

	// 创建自定义技能
	custom, _ := svc.CreateSkill(models.Skill{
		Name:           "待删除技能",
		Code:           "to-delete",
		Stage:          models.SkillStagePlan,
		PromptTemplate: "模板",
	})

	// 删除自定义技能应该成功
	if err := svc.DeleteSkill(custom.ID); err != nil {
		t.Fatalf("delete custom skill failed: %v", err)
	}

	// 删除系统技能应该失败
	skills, _ := svc.ListSkills(models.SkillStagePlan, true)
	systemSkill := skills[0]
	if err := svc.DeleteSkill(systemSkill.ID); err == nil {
		t.Error("系统技能不应该允许删除")
	}
}

func TestSkillService_GetEffectiveSkill(t *testing.T) {
	db := newTestDB(t)
	svc := NewSkillService(db)
	svc.InitSystemSkills()

	// 创建测试项目
	project := models.Project{Title: "测试项目"}
	db.Create(&project)

	// 获取有效技能（无项目配置时应该返回系统默认）
	skill, err := svc.GetEffectiveSkill(project.ID, models.SkillStagePlan)
	if err != nil {
		t.Fatalf("get effective skill failed: %v", err)
	}
	if skill == nil {
		t.Fatal("应该返回系统默认技能")
	}
	if !skill.IsSystem {
		t.Error("无项目配置时应该返回系统技能")
	}

	// 设置项目级技能
	skillID := skill.ID
	_, err = svc.SetProjectSkillConfig(project.ID, models.SkillStagePlan, &skillID, true)
	if err != nil {
		t.Fatalf("set project skill config failed: %v", err)
	}

	// 再次获取应该返回项目配置的技能
	effective, err := svc.GetEffectiveSkill(project.ID, models.SkillStagePlan)
	if err != nil {
		t.Fatalf("get effective skill after config failed: %v", err)
	}
	if effective.ID != skillID {
		t.Errorf("应该返回项目配置的技能，期望 %d，实际 %d", skillID, effective.ID)
	}

	// 禁用项目配置
	_, err = svc.SetProjectSkillConfig(project.ID, models.SkillStagePlan, &skillID, false)
	if err != nil {
		t.Fatalf("disable project skill failed: %v", err)
	}

	// 项目明确禁用时不得回退到系统默认，也就不会注入技能。
	effective2, err := svc.GetEffectiveSkill(project.ID, models.SkillStagePlan)
	if err != nil {
		t.Fatalf("get effective skill after disable failed: %v", err)
	}
	if effective2 != nil {
		t.Errorf("禁用后应该返回 nil，实际 %+v", effective2)
	}

	// 重置项目配置
	err = svc.ResetProjectStageConfig(project.ID, models.SkillStagePlan)
	if err != nil {
		t.Fatalf("reset config failed: %v", err)
	}

	effective3, err := svc.GetEffectiveSkill(project.ID, models.SkillStagePlan)
	if err != nil {
		t.Fatalf("get effective skill after reset failed: %v", err)
	}
	if effective3 == nil {
		t.Error("重置后应该返回系统默认技能")
	}
}

func TestSkillService_AssemblePrompt(t *testing.T) {
	db := newTestDB(t)
	svc := NewSkillService(db)

	skill := &models.Skill{
		SystemPrompt:   "系统提示词：{{project_title}}，阶段：{{stage}}",
		PromptTemplate: "用户提示：{{user_input}}，项目：{{project_title}}",
	}

	params := map[string]string{
		"project_title": "测试项目",
		"stage":         "plan",
		"user_input":    "测试输入",
	}

	systemPrompt, userPrompt := svc.AssemblePrompt(skill, params)

	if systemPrompt != "系统提示词：测试项目，阶段：plan" {
		t.Errorf("系统提示词替换失败: %s", systemPrompt)
	}
	if userPrompt != "用户提示：测试输入，项目：测试项目" {
		t.Errorf("用户提示词替换失败: %s", userPrompt)
	}

	// 测试缺少参数的情况
	params2 := map[string]string{"project_title": "项目"}
	systemPrompt2, _ := svc.AssemblePrompt(skill, params2)
	if systemPrompt2 != "系统提示词：项目，阶段：{{stage}}" {
		t.Errorf("缺少参数时应该保留占位符: %s", systemPrompt2)
	}
}

func TestSkillService_StageValidation(t *testing.T) {
	validStages := []string{
		models.SkillStagePlan,
		models.SkillStageCharacter,
		models.SkillStageStoryboard,
		models.SkillStageImagePrompt,
		models.SkillStageVideoPrompt,
		models.SkillStageReview,
		models.SkillStageChapterAnalysis,
		models.SkillStageArcMerge,
		models.SkillStageStoryBible,
		models.SkillStageAdaptationPlan,
		models.SkillStageEpisodeAdaptation,
		models.SkillStageContinuityReview,
		models.SkillStageMegastructure,
		models.SkillStageQwenCharacterCard,
	}

	for _, stage := range validStages {
		if !isValidStage(stage) {
			t.Errorf("阶段 %s 应该是有效的", stage)
		}
	}

	invalidStages := []string{"", "invalid", "planx", "PLAN"}
	for _, stage := range invalidStages {
		if isValidStage(stage) {
			t.Errorf("阶段 %s 应该是无效的", stage)
		}
	}
}

func TestSkillService_GetAvailableStages(t *testing.T) {
	db := newTestDB(t)
	svc := NewSkillService(db)

	stages := svc.GetAvailableStages()
	if len(stages) != 17 {
		t.Errorf("应该有 17 个阶段，实际 %d", len(stages))
	}

	stageSet := make(map[string]bool)
	for _, s := range stages {
		stageSet[s["value"]] = true
	}

	expectedStages := []string{
		models.SkillStagePlan,
		models.SkillStageCharacter,
		models.SkillStageStoryboard,
		models.SkillStageImagePrompt,
		models.SkillStageVideoPrompt,
		models.SkillStageReview,
		models.SkillStageChapterAnalysis,
		models.SkillStageArcMerge,
		models.SkillStageStoryBible,
		models.SkillStageAdaptationPlan,
		models.SkillStageEpisodeAdaptation,
		models.SkillStageContinuityReview,
		models.SkillStageMegastructure,
		models.SkillStageCreativeIntent,
		models.SkillStageQwenImageT2I,
		models.SkillStageQwenImageEdit,
		models.SkillStageQwenCharacterCard,
	}

	for _, expected := range expectedStages {
		if !stageSet[expected] {
			t.Errorf("缺少阶段: %s", expected)
		}
	}
}

func TestSkillService_CountSkillsByStage(t *testing.T) {
	db := newTestDB(t)
	svc := NewSkillService(db)
	svc.InitSystemSkills()

	counts, err := svc.CountSkillsByStage()
	if err != nil {
		t.Fatalf("count skills failed: %v", err)
	}

	if len(counts) == 0 {
		t.Error("应该有统计数据")
	}

	// 每个阶段至少应该有一个技能
	for stage, count := range counts {
		if count < 1 {
			t.Errorf("阶段 %s 应该有至少一个技能", stage)
		}
	}
}

func TestSkillService_ProjectSkillConfig(t *testing.T) {
	db := newTestDB(t)
	svc := NewSkillService(db)
	svc.InitSystemSkills()

	// 创建测试项目
	project := models.Project{Title: "配置测试项目"}
	db.Create(&project)

	// 设置项目技能配置
	skills, _ := svc.ListSkills(models.SkillStageCharacter, true)
	if len(skills) == 0 {
		t.Fatal("应该有角色技能")
	}
	skillID := skills[0].ID

	config, err := svc.SetProjectSkillConfig(project.ID, models.SkillStageCharacter, &skillID, true)
	if err != nil {
		t.Fatalf("set config failed: %v", err)
	}
	if config.SkillID == nil || *config.SkillID != skillID {
		t.Error("技能 ID 应该匹配")
	}
	if !config.Enabled {
		t.Error("应该启用")
	}
	if config.VersionSnapshot == 0 {
		t.Error("版本快照应该被设置")
	}

	// 获取项目配置
	configs, err := svc.GetProjectConfig(project.ID)
	if err != nil {
		t.Fatalf("get configs failed: %v", err)
	}
	if len(configs) != 1 {
		t.Errorf("应该有一个配置，实际 %d", len(configs))
	}

	// 获取特定阶段配置
	stageConfig, err := svc.GetProjectStageConfig(project.ID, models.SkillStageCharacter)
	if err != nil {
		t.Fatalf("get stage config failed: %v", err)
	}
	if stageConfig == nil {
		t.Error("应该返回阶段配置")
	}
}

func TestSkillService_SkillVersionHistory(t *testing.T) {
	db := newTestDB(t)
	svc := NewSkillService(db)
	svc.InitSystemSkills()

	// 获取 plan 技能的版本历史
	history, err := svc.SkillVersionHistory("short-drama-plan")
	if err != nil {
		t.Fatalf("get version history failed: %v", err)
	}
	if len(history) == 0 {
		t.Error("应该有版本历史")
	}

	// 检查版本顺序（降序）
	for i := 1; i < len(history); i++ {
		if history[i-1].Version < history[i].Version {
			t.Error("版本应该降序排列")
		}
	}
}

func TestSkillService_UpgradeSkill(t *testing.T) {
	db := newTestDB(t)
	svc := NewSkillService(db)
	svc.InitSystemSkills()

	// 获取系统技能
	skills, _ := svc.ListSkills(models.SkillStagePlan, true)
	systemSkill := skills[0]

	// 升级系统技能
	newSkill, err := svc.UpgradeSkill(
		systemSkill.ID,
		systemSkill.Version+1,
		"新模板内容",
		"新系统提示词",
	)
	if err != nil {
		t.Fatalf("upgrade skill failed: %v", err)
	}
	if newSkill.ID == systemSkill.ID {
		t.Error("新版本技能应该有新 ID")
	}
	if newSkill.Version != systemSkill.Version+1 {
		t.Errorf("版本应该是 %d，实际 %d", systemSkill.Version+1, newSkill.Version)
	}
	if newSkill.ParentID == nil || *newSkill.ParentID != systemSkill.ID {
		t.Error("应该设置父版本 ID")
	}
	if !newSkill.IsSystem {
		t.Error("升级后仍然是系统技能")
	}

	// 升级自定义技能应该失败
	custom, _ := svc.CreateSkill(models.Skill{
		Name:           "自定义技能",
		Code:           "custom-upgrade-test",
		Stage:          models.SkillStagePlan,
		PromptTemplate: "模板",
	})
	_, err = svc.UpgradeSkill(custom.ID, 2, "新模板", "新系统提示")
	if err == nil {
		t.Error("自定义技能不应该支持版本升级")
	}
}

func TestSkillService_StrictMutationWhitelist(t *testing.T) {
	db := newTestDB(t)
	svc := NewSkillService(db)

	if _, err := svc.CreateSkill(models.Skill{Name: "x", Code: "x", Stage: models.SkillStagePlan, Version: 9}); err == nil {
		t.Fatal("create must reject server-managed version")
	}
	skill, err := svc.CreateSkill(models.Skill{Name: "x", Code: "x", Stage: models.SkillStagePlan})
	if err != nil {
		t.Fatal(err)
	}
	for name, updates := range map[string]map[string]any{
		"unknown": {"created_at": "bad"},
		"code":    {"code": "changed"},
		"type":    {"enabled": "true"},
		"stage":   {"stage": "invalid"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := svc.UpdateSkill(skill.ID, updates); err == nil {
				t.Fatal("update must reject non-whitelisted or invalid value")
			}
		})
	}
}

func TestSkillService_VersionUniquenessAndLatestSemantics(t *testing.T) {
	db := newTestDB(t)
	svc := NewSkillService(db)
	if err := svc.InitSystemSkills(); err != nil {
		t.Fatal(err)
	}
	v1, err := svc.GetSkillByCode("short-drama-plan")
	if err != nil {
		t.Fatal(err)
	}
	v3, err := svc.UpgradeSkill(v1.ID, 3, "v3", "v3-system")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpgradeSkill(v1.ID, 2, "v2", "v2-system"); err == nil {
		t.Fatal("upgrade from an old ID must still be monotonic against latest")
	}
	if _, err := svc.UpgradeSkill(v1.ID, 3, "duplicate", "duplicate"); err == nil {
		t.Fatal("duplicate code/version must be rejected")
	}
	if err := db.Create(&models.Skill{Name: "duplicate", Code: v1.Code, Version: 3, Stage: v1.Stage}).Error; err == nil {
		t.Fatal("database must enforce unique (code, version)")
	}
	latest, err := svc.GetSkillByCode(v1.Code)
	if err != nil || latest.ID != v3.ID {
		t.Fatalf("GetSkillByCode must return latest: skill=%+v err=%v", latest, err)
	}
	if err := svc.InitSystemSkills(); err != nil {
		t.Fatalf("init with a newer existing version must remain idempotent: %v", err)
	}
	var count int64
	db.Model(&models.Skill{}).Where("code = ?", v1.Code).Count(&count)
	if count != 2 {
		t.Fatalf("expected exactly v1 and v3, got %d records", count)
	}
	effective, err := svc.GetEffectiveSkill(1, models.SkillStagePlan)
	if err != nil || effective.ID != v3.ID {
		t.Fatalf("system default must be latest version: skill=%+v err=%v", effective, err)
	}
}

func TestSkillService_ChatWithSkillInjectsAndAudits(t *testing.T) {
	db := newTestDB(t)
	svc := NewSkillService(db)
	if err := svc.InitSystemSkills(); err != nil {
		t.Fatal(err)
	}
	provider := &stubTextProvider{response: `{"ok":true}`}
	out, err := svc.ChatWithSkill(42, models.SkillStagePlan, provider, "BASE SYSTEM", "BASE USER", map[string]string{"project_info": "故事", "episode_count": "12"})
	if err != nil || out == "" {
		t.Fatalf("chat failed: out=%q err=%v", out, err)
	}
	if provider.calls != 1 {
		t.Fatalf("expected one provider call, got %d", provider.calls)
	}
	var logs []models.SkillAuditLog
	if err := db.Where("project_id = ? AND stage = ?", 42, models.SkillStagePlan).Find(&logs).Error; err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 || !logs[0].Success || logs[0].SkillVersion != 1 {
		t.Fatalf("unexpected audit: %+v", logs)
	}
}

func TestSkillService_ConfiguredOrFallbackDirectorSkillAudits(t *testing.T) {
	db := newTestDB(t)
	svc := NewSkillService(db)
	if err := svc.InitSystemSkills(); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "director"}
	db.Create(&project)
	provider := &stubTextProvider{response: `{"subject":"人物","action":"抬手","camera":"中景","lighting":"侧光","style":"国漫"}`}
	out, err := svc.ChatWithConfiguredOrFallbackSkill(project.ID, models.SkillStageVideoPrompt, "director-shot-packet", provider, "", "", map[string]string{"scene_content": "人物抬手", "duration": "2"})
	if err != nil || !strings.Contains(out, `"subject"`) {
		t.Fatalf("fallback director skill failed: out=%q err=%v", out, err)
	}
	var logs []models.SkillAuditLog
	db.Where("project_id = ? AND skill_code = ?", project.ID, "director-shot-packet").Find(&logs)
	if len(logs) != 1 || !logs[0].Success {
		t.Fatalf("fallback director skill audit=%+v", logs)
	}
}

func TestSkillService_DisabledConfiguredSkillUnavailable(t *testing.T) {
	db := newTestDB(t)
	svc := NewSkillService(db)
	custom, err := svc.CreateSkill(models.Skill{Name: "custom", Code: "disabled-custom", Stage: models.SkillStagePlan})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateSkill(custom.ID, map[string]any{"enabled": false}); err != nil {
		t.Fatal(err)
	}
	id := custom.ID
	if _, err := svc.SetProjectSkillConfig(1, models.SkillStagePlan, &id, true); err == nil {
		t.Fatal("globally disabled skill must not be configurable")
	}

	// Existing configurations must also stop resolving after a global disable.
	if err := db.Model(&models.Skill{}).Where("id = ?", id).Update("enabled", true).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetProjectSkillConfig(1, models.SkillStagePlan, &id, true); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.Skill{}).Where("id = ?", id).Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	if skill, err := svc.GetEffectiveSkill(1, models.SkillStagePlan); err == nil || skill != nil {
		t.Fatalf("disabled configured skill must be unavailable: skill=%+v err=%v", skill, err)
	}
}
