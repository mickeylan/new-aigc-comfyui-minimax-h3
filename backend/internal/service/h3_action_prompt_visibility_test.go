package service

import (
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

// A real model copied the old "说明画面可从可用参考图仅作为…参考状态开始" opening
// rule verbatim into [Shot 1]. Without an opening frame the rule must ask for the
// opening staging directly, and the director rules must cover camera visibility,
// shot-duration balance and Chinese asset names.
func TestGenerateSceneVideoActionOpeningAndVisibilityRules(t *testing.T) {
	ps := newTestProjectService(t)
	if err := ps.db.AutoMigrate(&models.Shot{}, &models.Dialogue{}); err != nil {
		t.Fatal(err)
	}
	provider := &captureTextProvider{response: "[Shot 1] A medium shot frames 雷晓飞 at frame center as he taps the table once, then turns toward the doorway."}
	ps.textProvider = provider
	project := models.Project{Title: "测试"}
	if err := ps.db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	if err := ps.db.Create(&models.Character{ProjectID: project.ID, Name: "雷晓飞", Sheet: "leixiaofei-sheet.png"}).Error; err != nil {
		t.Fatal(err)
	}
	sc := &models.Scene{ProjectID: project.ID, Characters: "雷晓飞", Content: "雷晓飞轻敲桌面，随后警觉地转头"}
	if err := ps.db.Create(sc).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := ps.GenerateSceneVideoAction(sc); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(provider.system, "说明画面可从可用参考图") {
		t.Fatalf("opening rule still asks the body to restate reference usage: %s", provider.system)
	}
	for _, want := range []string{"直接写开场机位、人物位置与动作", "不得在正文解释参考图用途", "背对镜头或被遮挡的人物不写表情", "按每段动作量分配各Shot时长", "道具和场景同样逐字使用其中文名"} {
		if !strings.Contains(provider.system, want) {
			t.Fatalf("system missing %q: %s", want, provider.system)
		}
	}
}

func TestChineseAssetNamesBindToPictureReferences(t *testing.T) {
	refs := []string{"- <Picture 1>：角色「柳乐儿」四视图", "- <Picture 2>：场景「灰白巨石所在处」参考图", "- <Picture 3>：道具「旧拨浪鼓」四视图"}
	names := referenceAssetNames(refs)
	if len(names) != 2 || names[0] != "灰白巨石所在处" || names[1] != "旧拨浪鼓" {
		t.Fatalf("asset names = %q", names)
	}
	got := useSubjectTags("[Shot 1] 柳乐儿 grips 旧拨浪鼓 beside the boulder of 灰白巨石所在处.", refs)
	want := "[Shot 1] <Subject 1> grips the prop shown in <Picture 3> beside the boulder of the environment established by <Picture 2>."
	if got != want {
		t.Fatalf("binding = %q", got)
	}
}
