package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

func TestDraftAssetDesignSendsDirectionsBaseAndSceneSnippets(t *testing.T) {
	ps := newTestProjectService(t)
	provider := &captureTextProvider{response: `{"description":"巨石爆碎后的旷野，碎石散落在焦黑的地面上，远处山脊轮廓沿用原景，天色转为深夜","sources":[{"tag":"沿用","text":"远处山脊轮廓沿用原景"},{"tag":"按你的要求","text":"天色转为深夜"},{"tag":"乱写","text":"碎石"},{"tag":"AI补充","text":"不在原文里的句子"}],"changes":["巨石已碎成散落的碎块"," ","天色转为深夜"]}`}
	ps.textProvider = provider
	p := models.Project{Title: "狐仙", Genre: "玄幻", Style: "国风写实", Synopsis: "柳乐儿出世"}
	ps.db.Create(&p)
	base := models.Asset{ProjectID: p.ID, Kind: AssetKindLocation, Name: "巨石所在处", Description: "旷野中央矗立一块三丈高的青灰巨石"}
	ps.db.Create(&base)
	ps.db.Create(&models.Scene{ProjectID: p.ID, EpisodeN: 1, Order: 2, Title: "巨石", Content: "巨石静静立在旷野", LocationName: "巨石所在处"})
	ps.db.Create(&models.Scene{ProjectID: p.ID, EpisodeN: 1, Order: 3, Title: "别处", Content: "这里不该出现", LocationName: "山洞"})

	draft, err := ps.DraftAssetDesign(&p, AssetKindLocation, AssetDesignRequest{Name: "巨石爆碎后", BaseID: base.ID, Directions: []string{"深夜", "深夜", " "}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"基础资产（本资产由它派生）：场景「巨石所在处」：旷野中央矗立一块三丈高的青灰巨石", "1. 深夜", "第1集场景2「巨石」：巨石静静立在旷野", "未提供，由你做主"} {
		if !strings.Contains(provider.user, want) {
			t.Fatalf("user prompt missing %q:\n%s", want, provider.user)
		}
	}
	if strings.Contains(provider.user, "这里不该出现") || strings.Count(provider.user, "深夜") != 1 {
		t.Fatalf("unrelated scene or duplicate direction leaked:\n%s", provider.user)
	}
	if !strings.Contains(provider.system, "沿用基础资产") || !strings.Contains(provider.system, "与原文不同") {
		t.Fatalf("system prompt lacks derivation/source contract: %s", provider.system)
	}
	// 未知标签与不在正文中的摘录被丢弃，变化去空。
	if len(draft.Sources) != 2 || draft.Sources[1].Tag != "按你的要求" || len(draft.Changes) != 2 || draft.Warning != "" {
		t.Fatalf("draft=%+v", draft)
	}
}

func TestAssetDesignSceneSnippetsAreCappedAndFallBackToBase(t *testing.T) {
	ps := newTestProjectService(t)
	p := models.Project{Title: "狐仙"}
	ps.db.Create(&p)
	long := strings.Repeat("长", 500)
	for i := 1; i <= 9; i++ {
		ps.db.Create(&models.Scene{ProjectID: p.ID, EpisodeN: 1, Order: i, Title: fmt.Sprint("s", i), Content: long, Props: "青玉佩, 铜镜"})
	}
	snippets := ps.assetDesignSceneSnippets(p.ID, AssetKindProp, "新玉佩", "青玉佩")
	if len(snippets) != assetDesignSceneLimit {
		t.Fatalf("snippets=%d want %d", len(snippets), assetDesignSceneLimit)
	}
	if n := len([]rune(snippets[0])); n > assetDesignSnippetRunes+40 {
		t.Fatalf("snippet not truncated: %d runes", n)
	}
}

func TestAssetDesignFallsBackToRawTextWithoutRepairCall(t *testing.T) {
	ps := newTestProjectService(t)
	provider := &stubTextProvider{response: "青玉佩，扁平椭圆轮廓，温润半透明青色玉石，边缘刻浅金云纹，以成年手掌为尺寸参照"}
	ps.textProvider = provider
	p := models.Project{Title: "狐仙"}
	ps.db.Create(&p)
	draft, err := ps.DraftAssetDesign(&p, AssetKindProp, AssetDesignRequest{Name: "青玉佩"})
	if err != nil {
		t.Fatal(err)
	}
	if provider.calls != 1 || draft.Description != provider.response || draft.Warning == "" || len(draft.Sources) != 0 {
		t.Fatalf("calls=%d draft=%+v", provider.calls, draft)
	}
	provider.response = "太短"
	if _, err := ps.DraftAssetDesign(&p, AssetKindProp, AssetDesignRequest{Name: "青玉佩"}); err == nil {
		t.Fatal("too-short output accepted")
	}
}

func TestReviseAssetDesignSendsSelectionAndRejectsStaleSelection(t *testing.T) {
	ps := newTestProjectService(t)
	provider := &captureTextProvider{response: `{"description":"旷野地面布满纵横交错的深裂缝，远山轮廓不变，夜色浓重","sources":[{"tag":"按你的要求","text":"旷野地面布满纵横交错的深裂缝"}],"changes":[]}`}
	ps.textProvider = provider
	p := models.Project{Title: "狐仙"}
	ps.db.Create(&p)
	current := "旷野地面有几道裂缝，远山轮廓不变，夜色浓重"
	draft, err := ps.ReviseAssetDesign(&p, AssetKindLocation, AssetDesignRequest{Name: "巨石爆碎后", Current: current, Selection: "旷野地面有几道裂缝", Instruction: "裂缝再多一些"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"当前稿：" + current, "用户选中的片段：旷野地面有几道裂缝", "这次的修改方向：裂缝再多一些"} {
		if !strings.Contains(provider.user, want) {
			t.Fatalf("revise prompt missing %q:\n%s", want, provider.user)
		}
	}
	if !strings.Contains(provider.system, "其余句子逐字保留") || !strings.Contains(provider.system, "完整变化清单") || !strings.Contains(draft.Description, "纵横交错") {
		t.Fatalf("system=%s draft=%+v", provider.system, draft)
	}
	if _, err := ps.ReviseAssetDesign(&p, AssetKindLocation, AssetDesignRequest{Name: "巨石爆碎后", Current: current, Selection: "不存在的片段", Instruction: "改"}); err == nil {
		t.Fatal("stale selection accepted")
	}
	if _, err := ps.ReviseAssetDesign(&p, AssetKindLocation, AssetDesignRequest{Name: "巨石爆碎后", Current: current}); err == nil {
		t.Fatal("empty instruction accepted")
	}
}

func TestAssetDesignOptionsParsesThreeDirections(t *testing.T) {
	ps := newTestProjectService(t)
	ps.textProvider = &stubTextProvider{response: "```json\n{\"options\":[{\"title\":\"焦土深夜\",\"detail\":\"地面焦黑\"},{\"title\":\"\",\"detail\":\"丢弃\"},{\"title\":\"晨雾残骸\",\"detail\":\"雾中碎石\"},{\"title\":\"烈日裂原\",\"detail\":\"干裂地表\"},{\"title\":\"第四个\",\"detail\":\"超出\"}]}\n```"}
	p := models.Project{Title: "狐仙"}
	ps.db.Create(&p)
	options, err := ps.AssetDesignOptions(&p, AssetKindLocation, AssetDesignRequest{Name: "巨石爆碎后"})
	if err != nil {
		t.Fatal(err)
	}
	if len(options) != 3 || options[1].Title != "晨雾残骸" {
		t.Fatalf("options=%+v", options)
	}
}

func TestAssetDerivationIsValidatedAndPersisted(t *testing.T) {
	ps := newTestProjectService(t)
	p := models.Project{Title: "狐仙"}
	ps.db.Create(&p)
	base, err := ps.CreateAsset(models.Asset{ProjectID: p.ID, Kind: AssetKindLocation, Name: "巨石所在处"})
	if err != nil {
		t.Fatal(err)
	}
	prop, _ := ps.CreateAsset(models.Asset{ProjectID: p.ID, Kind: AssetKindProp, Name: "青玉佩"})
	if _, err := ps.CreateAsset(models.Asset{ProjectID: p.ID, Kind: AssetKindLocation, Name: "跨类", DerivedFromID: prop.ID}); err == nil {
		t.Fatal("cross-kind derivation accepted")
	}
	derived, err := ps.CreateAsset(models.Asset{ProjectID: p.ID, Kind: AssetKindLocation, Name: "巨石爆碎后", DerivedFromID: base.ID,
		DesignDirections: "深夜\n\n深夜\n地面焦痕", DeriveChanges: "巨石碎裂\n"})
	if err != nil {
		t.Fatal(err)
	}
	if derived.DesignDirections != "深夜\n地面焦痕" || derived.DeriveChanges != "巨石碎裂" {
		t.Fatalf("design fields not normalized: %+v", derived)
	}
	// 来源改为派生资产自身的后代会成环。
	cycle := derived.ID
	if err := ps.UpdateAsset(base, models.Asset{Name: base.Name}, AssetDesignFields{DerivedFromID: &cycle}); err == nil {
		t.Fatal("derivation cycle accepted")
	}
	// 未传设计字段时保持原值。
	if err := ps.UpdateAsset(derived, models.Asset{Name: derived.Name, Description: "新描述"}, AssetDesignFields{}); err != nil {
		t.Fatal(err)
	}
	var saved models.Asset
	ps.db.First(&saved, derived.ID)
	if saved.DerivedFromID != base.ID || saved.DesignDirections != "深夜\n地面焦痕" || saved.DeriveChanges != "巨石碎裂" {
		t.Fatalf("omitted design fields changed: %+v", saved)
	}
	// 取消派生时清空变化清单。
	zero := uint(0)
	if err := ps.UpdateAsset(&saved, models.Asset{Name: saved.Name}, AssetDesignFields{DerivedFromID: &zero}); err != nil {
		t.Fatal(err)
	}
	ps.db.First(&saved, derived.ID)
	if saved.DerivedFromID != 0 || saved.DeriveChanges != "" {
		t.Fatalf("un-derive left changes: %+v", saved)
	}
}

func TestDerivedAssetImageUsesQwenEditWithBaseImage(t *testing.T) {
	ps := newSheetGenerationService(t)
	edit := loadTemplateForTest(t, "qwen_image_2_1_multi_edit.json")
	if err := ps.db.Create(&edit).Error; err != nil {
		t.Fatal(err)
	}
	p := models.Project{Title: "狐仙", Style: "国风写实", AspectRatio: "16:9"}
	ps.db.Create(&p)
	pid := fmt.Sprint(p.ID)
	base := models.Asset{ProjectID: p.ID, Kind: AssetKindProp, Name: "完整玉佩", Image: "prop_base.png", Sheet: "prop_sheet.png", ImageEngine: ImageEngineMoss}
	ps.db.Create(&base)
	for _, name := range []string{"prop_base.png", "prop_sheet.png"} {
		ps.db.Create(&models.UploadFile{TaskID: pid, Type: "image", Name: name, Path: pid + "/" + name})
	}
	// 派生资产即使选的是 Moss，也只能走 Qwen 编辑。
	derived := models.Asset{ProjectID: p.ID, Kind: AssetKindProp, Name: "碎裂玉佩", Description: "玉佩从中间裂成两半", DerivedFromID: base.ID, DeriveChanges: "玉佩从中间裂成两半\n裂口泛白", ImageEngine: ImageEngineMoss}
	ps.db.Create(&derived)
	if err := ps.StartAssetImage(&derived); err != nil {
		t.Fatal(err)
	}
	var saved models.Asset
	ps.db.First(&saved, derived.ID)
	var task models.Task
	if err := ps.db.Where("task_id = ?", saved.ImageTaskID).First(&task).Error; err != nil {
		t.Fatalf("derived task not recorded: %v", err)
	}
	if task.TemplateName != edit.Name {
		t.Fatalf("derived asset must use Qwen edit, got %s", task.TemplateName)
	}
	if i, j := strings.Index(task.InputsJSON, "prop_base.png"), strings.Index(task.InputsJSON, "prop_sheet.png"); i < 0 || j < 0 || i > j {
		t.Fatalf("base image must be image1 before the sheet: %s", task.InputsJSON)
	}
	for _, want := range []string{"<image1>是道具「完整玉佩」的参考图", "1. 玉佩从中间裂成两半", "2. 裂口泛白", "<image2>是同一道具的四视图", "人物数量必须为零"} {
		if !strings.Contains(task.Prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, task.Prompt)
		}
	}
	var params map[string]any
	if err := json.Unmarshal([]byte(task.ParamsJSON), &params); err != nil {
		t.Fatal(err)
	}
	if params["aspect_ratio"] != "1:1 (Square)" {
		t.Fatalf("prop edit ratio=%v", params["aspect_ratio"])
	}
}

func TestDerivedAssetImageRefusesWithoutBaseImageOrTemplate(t *testing.T) {
	ps := newSheetGenerationService(t)
	p := models.Project{Title: "狐仙"}
	ps.db.Create(&p)
	base := models.Asset{ProjectID: p.ID, Kind: AssetKindLocation, Name: "巨石所在处"}
	ps.db.Create(&base)
	derived := models.Asset{ProjectID: p.ID, Kind: AssetKindLocation, Name: "巨石爆碎后", Description: "巨石碎裂", DerivedFromID: base.ID, DeriveChanges: "巨石碎裂"}
	ps.db.Create(&derived)

	err := ps.StartAssetImage(&derived)
	if err == nil || !strings.Contains(err.Error(), "请先给场景「巨石所在处」生成或上传参考图") {
		t.Fatalf("missing base image err=%v", err)
	}
	ps.db.Model(&base).Update("image", "loc_base.png")
	// Qwen 编辑模板未安装：显式报错，不退回文生图。
	if err := ps.StartAssetImage(&derived); err == nil || !strings.Contains(err.Error(), TemplateQwen21MultiEdit) {
		t.Fatalf("missing edit template err=%v", err)
	}
	ps.db.Delete(&base)
	if err := ps.StartAssetImage(&derived); err == nil || !strings.Contains(err.Error(), "派生来源已删除") {
		t.Fatalf("deleted base err=%v", err)
	}
	var saved models.Asset
	ps.db.First(&saved, derived.ID)
	if saved.ImageTaskID != "" {
		t.Fatalf("a refused derivation must not record a task: %+v", saved)
	}
}

func TestGenerateAllAssetImagesSkipsDerivedAssetWaitingForBase(t *testing.T) {
	ps := newSheetGenerationService(t)
	moss := loadTemplateForTest(t, "moss_image_t2i.json")
	if err := ps.db.Create(&moss).Error; err != nil {
		t.Fatal(err)
	}
	p := models.Project{Title: "狐仙", AspectRatio: "16:9"}
	ps.db.Create(&p)
	base := models.Asset{ProjectID: p.ID, Kind: AssetKindLocation, Name: "巨石所在处", ImageEngine: ImageEngineMoss}
	ps.db.Create(&base)
	ps.db.Create(&models.Asset{ProjectID: p.ID, Kind: AssetKindLocation, Name: "巨石爆碎后", DerivedFromID: base.ID, DeriveChanges: "巨石碎裂", ImageEngine: ImageEngineMoss})
	n, err := ps.GenerateAllAssetImages(&p, AssetKindLocation)
	if err != nil || n != 1 {
		t.Fatalf("submitted=%d err=%v", n, err)
	}
}
