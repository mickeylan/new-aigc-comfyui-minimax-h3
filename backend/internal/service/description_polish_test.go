package service

import (
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

func TestDescriptionPolishSkillsAreSeeded(t *testing.T) {
	db := newTestDB(t)
	if err := NewSkillService(db).InitSystemSkills(); err != nil {
		t.Fatal(err)
	}
	for code, want := range map[string]string{assetSceneDescriptionPolishCode: "画面中不得出现任何人物或人影", assetCharacterDescriptionPolishCode: "服装类型必须保留原样"} {
		var skill models.Skill
		if err := db.Where("code = ?", code).First(&skill).Error; err != nil {
			t.Fatal(err)
		}
		if skill.Version != 1 || !strings.Contains(skill.SystemPrompt, want) {
			t.Fatalf("%s skill=%+v", code, skill)
		}
	}
}

func TestPolishDescriptionRoutesToSkillAndReturnsDraftOnly(t *testing.T) {
	ps := newTestProjectService(t)
	if err := ps.db.AutoMigrate(&models.Skill{}, &models.ProjectSkillConfig{}, &models.SkillAuditLog{}, &models.Asset{}); err != nil {
		t.Fatal(err)
	}
	ps.skills = NewSkillService(ps.db)
	if err := ps.skills.InitSystemSkills(); err != nil {
		t.Fatal(err)
	}
	provider := &captureTextProvider{response: "```云隐宗大殿，清晨，内景，青石地面……```"}
	ps.textProvider = provider
	got, err := ps.PolishDescription(1, "scene", "云隐宗大殿", "宗门大殿，晨雾，正门朝南")
	if err != nil {
		t.Fatal(err)
	}
	if got != "云隐宗大殿，清晨，内景，青石地面……" {
		t.Fatalf("polished=%q", got)
	}
	if !strings.Contains(provider.system, "场景描述") || !strings.Contains(provider.user, "原描述：宗门大殿，晨雾，正门朝南") {
		t.Fatalf("scene skill not used: system=%q user=%q", provider.system, provider.user)
	}
	if _, err := ps.PolishDescription(1, "character", "林舒", "22岁女性，白色交领仙裙"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(provider.system, "角色设定编辑") || !strings.Contains(provider.user, "角色名：林舒") {
		t.Fatalf("character skill not used: system=%q user=%q", provider.system, provider.user)
	}
	if _, err := ps.PolishDescription(1, "scene", "空", "  "); err == nil {
		t.Fatal("empty description must be rejected")
	}
	if _, err := ps.PolishDescription(1, "prop", "玉佩", "青玉"); err == nil {
		t.Fatal("unsupported target must be rejected")
	}
}
