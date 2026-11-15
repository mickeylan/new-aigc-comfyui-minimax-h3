package service

import (
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

// TestInitSystemSkillsUpgradesStoryboardGenerateToCurrentRules pins the storyboard-generate v2
// text: the stored v1 keeps its history while the current version carries today's rules.
func TestInitSystemSkillsUpgradesStoryboardGenerateToCurrentRules(t *testing.T) {
	db := newTestDB(t)
	stale := models.Skill{
		Name: "分镜剧本生成", Code: "storyboard-generate", Operation: models.SkillStageStoryboard,
		Version: 1, Stage: models.SkillStageStoryboard, IsSystem: true, Enabled: true, SortOrder: 1,
		SystemPrompt: "质量要求：\n- 每集 3-5 个场次\n- 结尾必须有悬念钩子",
	}
	if err := db.Create(&stale).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewSkillService(db)
	if err := svc.InitSystemSkills(); err != nil {
		t.Fatal(err)
	}
	var rows []models.Skill
	db.Where("code = ?", "storyboard-generate").Order("version").Find(&rows)
	if len(rows) != 2 || rows[0].Version != 1 || rows[1].Version != 2 {
		t.Fatalf("seed must keep v1 and add one v2: %+v", rows)
	}
	current := rows[1]
	if current.ParentID == nil || *current.ParentID != stale.ID {
		t.Fatalf("v2 parent = %v want %d", current.ParentID, stale.ID)
	}
	if !strings.Contains(rows[0].SystemPrompt, "每集 3-5 个场次") {
		t.Fatal("history must keep the v1 wording it was rendered with")
	}
	for _, gone := range []string{"3-8 秒", "每集 3-5 个场次", "{{scene_count}}", "结尾必须有悬念钩子", "付费卡点"} {
		if strings.Contains(current.PromptTemplate+current.SystemPrompt, gone) {
			t.Errorf("v2 still contains the stale rule %q", gone)
		}
	}
	for _, want := range []string{"5-15 秒", "不少于 4 秒", "逐字保留", "每秒 9 字", "不额外添加悬念钩子"} {
		if !strings.Contains(current.PromptTemplate+current.SystemPrompt, want) {
			t.Errorf("v2 is missing the current rule %q", want)
		}
	}
	system, user := svc.AssemblePrompt(&current, map[string]string{"episode_n": "2", "story_prompt": "林舒入山", "characters": "林舒"})
	for _, got := range []string{system, user} {
		if strings.Contains(got, "{{") {
			t.Errorf("rendered prompt left a placeholder: %q", got)
		}
	}
	if !strings.Contains(user, "林舒入山") {
		t.Fatalf("user prompt must carry the episode context: %q", user)
	}
	// The storyboard stage still has no default Skill: without an explicit project binding the
	// legacy script path passes straight through.
	if skill, err := svc.GetEffectiveSkill(9, models.SkillStageStoryboard); err != nil || skill != nil {
		t.Fatalf("storyboard stage default = %+v err=%v", skill, err)
	}
}