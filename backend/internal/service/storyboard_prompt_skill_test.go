package service

import (
	"fmt"
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

func TestReviewedStoryboardPromptSkillsAreCuratedAndVersioned(t *testing.T) {
	db := newTestDB(t)
	svc := NewSkillService(db)
	if err := svc.InitSystemSkills(); err != nil {
		t.Fatal(err)
	}
	checks := []struct {
		code    string
		version int
		phrases []string
	}{
		{h3StoryboardImageSkillCode, 1, []string{"真正可见的人物", "支撑脚与重心", "道具由谁持有", "正向空间拓扑", "800至1800"}},
		{QwenImage21T2IProgram, 3, []string{"exact visible subject count", "support foot and weight", "prop ownership"}},
		{QwenImage21EditProgram, 3, []string{"references do not automatically become visible subjects", "state each reference duty exactly once", "positive final topology"}},
	}
	for _, check := range checks {
		var skill models.Skill
		if err := db.Where("code=?", check.code).Order("version DESC,id DESC").First(&skill).Error; err != nil {
			t.Fatalf("load %s: %v", check.code, err)
		}
		if skill.Version != check.version {
			t.Fatalf("%s version=%d want=%d", check.code, skill.Version, check.version)
		}
		for _, phrase := range check.phrases {
			if !strings.Contains(skill.SystemPrompt, phrase) {
				t.Fatalf("%s missing reviewed contract %q", check.code, phrase)
			}
		}
	}
}

func TestReviewedStoryboardPromptBudgetsPreserveUsefulComposition(t *testing.T) {
	var body strings.Builder
	for i := 0; i < 45; i++ {
		fmt.Fprintf(&body, "可见构图证据%d：人物与道具在前中后景形成清楚关系，支撑脚、重心、视线与手部接触均可辨；", i)
	}
	body.WriteString("最终关键证据：暖色侧光照亮人物面部并在地面留下同向阴影。")

	qwen := ensureQwenSceneReferenceBindings(body.String()+"\n负向硬约束：禁止额外人物，禁止字幕水印，禁止现代服饰。", nil)
	if strings.Contains(qwen, "负向硬约束") {
		t.Fatalf("Qwen prompt retained rejected negative list: %s", qwen)
	}
	if len([]rune(qwen)) <= 1100 || len([]rune(qwen)) > 1400 {
		t.Fatalf("Qwen reviewed detail budget=%d want 1101..1400", len([]rune(qwen)))
	}

	h3 := buildH3StoryboardPrompt(&models.Scene{ImagePrompt: "[Shot 1] " + body.String()}, &models.Project{}, nil)
	detail := h3PromptSection(h3, "detailed_description:")
	if len([]rune(detail)) <= 1400 || len([]rune(detail)) > 1900 {
		t.Fatalf("H3 reviewed detail budget=%d want 1401..1900", len([]rune(detail)))
	}
}

func TestH3StoryboardRedesignUsesAuditedDedicatedSkill(t *testing.T) {
	ps := newTestProjectService(t)
	if err := ps.db.AutoMigrate(&models.Skill{}, &models.ProjectSkillConfig{}, &models.SkillAuditLog{}); err != nil {
		t.Fatal(err)
	}
	ps.skills = NewSkillService(ps.db)
	if err := ps.skills.InitSystemSkills(); err != nil {
		t.Fatal(err)
	}
	provider := &captureTextProvider{response: "[Shot 1] 中近景固定机位，女童位于前景偏右，右脚支撑重心，双手握住旧拨浪鼓，视线朝向左前方草丛，暖色侧光留下同向阴影。"}
	ps.textProvider = provider
	project := models.Project{Title: "审核分镜", Genre: "古典修仙", Style: "写实仙侠"}
	if err := ps.db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1, Title: "草丛前", Content: "女童握住拨浪鼓停在草丛前", ImageEngine: ImageEngineMiniMaxH3}
	if err := ps.db.Create(&scene).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := ps.RedesignSceneImagePrompt(&scene); err != nil {
		t.Fatal(err)
	}
	for _, phrase := range []string{"真正可见的人物", "支撑脚与重心", "只输出一段以“[Shot 1]”开头"} {
		if !strings.Contains(provider.system, phrase) {
			t.Fatalf("runtime system missing %q", phrase)
		}
	}
	var audit models.SkillAuditLog
	if err := ps.db.Where("project_id=? AND skill_code=?", project.ID, h3StoryboardImageSkillCode).First(&audit).Error; err != nil {
		t.Fatalf("dedicated storyboard skill was not audited: %v", err)
	}
}
