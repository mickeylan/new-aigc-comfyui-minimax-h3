package service

import (
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

func TestFemaleCharacterBeautySkillIsCuratedWithoutLayoutNoise(t *testing.T) {
	db := newTestDB(t)
	svc := NewSkillService(db)
	if err := svc.InitSystemSkills(); err != nil {
		t.Fatal(err)
	}
	var skill models.Skill
	if err := db.Where("code=?", femaleCharacterBeautySkillCode).Order("version DESC,id DESC").First(&skill).Error; err != nil {
		t.Fatal(err)
	}
	if skill.Version != 1 || skill.Operation != femaleCharacterBeautySkillCode || skill.Stage != models.SkillStageCharacter {
		t.Fatalf("unexpected beauty skill: %+v", skill)
	}
	for _, want := range []string{"妆容写清底妆冷暖", "发髻/环髻/编发/马尾几何", "2至4个核心色相", "材质必须具有可见证据", "三视图、由左到右排列", "不属于角色DNA", "禁止复制样例"} {
		if !strings.Contains(skill.SystemPrompt, want) {
			t.Fatalf("beauty skill missing %q", want)
		}
	}
	if strings.Contains(skill.PromptTemplate, "三视图") || strings.Contains(skill.PromptTemplate, "由左到右排列") {
		t.Fatalf("layout leaked into generated profile request: %s", skill.PromptTemplate)
	}
}

func TestGenerateAdultFemaleProfileUsesAuditedBeautySkill(t *testing.T) {
	ps := newTestProjectService(t)
	if err := ps.db.AutoMigrate(&models.Skill{}, &models.ProjectSkillConfig{}, &models.SkillAuditLog{}); err != nil {
		t.Fatal(err)
	}
	skills := NewSkillService(ps.db)
	if err := skills.InitSystemSkills(); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "问仙", Genre: "古典修仙", Style: "写实国风玄幻"}
	if err := ps.db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	character := models.Character{ProjectID: project.ID, Name: "沈霜", Gender: "女性", Role: "女剑修", Appearance: "20岁女性，黑色长发，目光沉静", ProfileStatus: models.ProfileStatusDraft}
	if err := ps.db.Create(&character).Error; err != nil {
		t.Fatal(err)
	}
	provider := &captureTextProvider{response: `{"name":"沈霜","role":"女剑修","gender":"女性","appearance":"20岁女性，修长鹅蛋脸，下颌利落，细长剑眉，狭长凤眼与深褐瞳色，鼻梁挺直，薄唇低饱和豆沙色，乌黑长发高束后编入单股长辫","personality":"沉静克制","background":"宗门剑修","relationships":"同门关系","emotions":"警觉时眉峰微压","habits":"右手轻按剑鞘","wardrobe_detail":"青灰交领窄袖内袍叠象牙白大袖外衫，银线竹叶纹仅落在领缘与袖口，青玉腰扣，青灰平底云头履","lighting_mood":"左上柔和冷日光，发丝与玉扣出现克制轮廓光","color_palette":"青灰、象牙白、青玉色与少量银色"}`}
	profile := NewCharacterProfileService(ps.db, provider)
	profile.skills = skills
	if err := profile.GenerateProfile(&character, &project, ""); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"女性角色美术增强契约", "妆容写清底妆冷暖", "三视图、由左到右排列", "不属于角色DNA"} {
		if !strings.Contains(provider.system, want) {
			t.Fatalf("runtime prompt missing %q", want)
		}
	}
	var audit models.SkillAuditLog
	if err := ps.db.Where("project_id=? AND skill_code=?", project.ID, femaleCharacterBeautySkillCode).First(&audit).Error; err != nil {
		t.Fatalf("beauty skill was not audited: %v", err)
	}
}

func TestFemaleBeautySkillRequiresAuthoritativeAdultEvidence(t *testing.T) {
	cases := []struct {
		character models.Character
		want      bool
	}{
		{models.Character{Gender: "女性", Appearance: "20岁女性，黑色长发"}, true},
		{models.Character{Gender: "女性", Appearance: "成年女子，乌黑长发"}, true},
		{models.Character{Gender: "女性", Appearance: "六岁女童，乌黑长发"}, false},
		{models.Character{Gender: "女性", Appearance: "年龄原文未明确"}, false},
		{models.Character{Gender: "原文未明确", Appearance: "年轻角色"}, false},
	}
	for _, tc := range cases {
		if got := authoritativeAdultFemale(&tc.character); got != tc.want {
			t.Fatalf("authoritativeAdultFemale(%+v)=%v want=%v", tc.character, got, tc.want)
		}
	}
}
