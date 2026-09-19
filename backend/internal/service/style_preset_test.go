package service

import (
	"testing"

	"comfyui-console/internal/models"
)

func TestStylePresetService_InitSystemPresets(t *testing.T) {
	db := newTestDBWithNewModels(t)
	svc := NewStylePresetService(db)

	// 首次初始化应该成功
	if err := svc.InitSystemPresets(); err != nil {
		t.Fatalf("init system presets failed: %v", err)
	}

	// 再次初始化应该幂等
	if err := svc.InitSystemPresets(); err != nil {
		t.Fatalf("init system presets idempotent failed: %v", err)
	}

	// 检查系统预设数量
	presets, err := svc.ListPresets("", nil, false)
	if err != nil {
		t.Fatalf("list presets: %v", err)
	}
	if len(presets) == 0 {
		t.Fatal("系统预设应该已初始化")
	}

	// 检查有推荐预设
	recommended, err := svc.ListPresets("", nil, true)
	if err != nil {
		t.Fatalf("list recommended presets: %v", err)
	}
	if len(recommended) == 0 {
		t.Error("应该有推荐预设")
	}
}

func TestStylePresetService_ListPresets(t *testing.T) {
	db := newTestDBWithNewModels(t)
	svc := NewStylePresetService(db)
	svc.InitSystemPresets()

	// 测试分类筛选
	cinematic, err := svc.ListPresets(string(models.StylePresetCinematic), nil, false)
	if err != nil {
		t.Fatalf("list cinematic presets: %v", err)
	}
	for _, p := range cinematic {
		if p.Category != models.StylePresetCinematic {
			t.Errorf("期望分类 %s，实际 %s", models.StylePresetCinematic, p.Category)
		}
	}

	// 测试标签筛选
	byTag, err := svc.ListPresets("", []string{"电影感"}, false)
	if err != nil {
		t.Fatalf("list by tag: %v", err)
	}
	if len(byTag) == 0 {
		t.Error("应该找到包含标签的预设")
	}
}

func TestStylePresetService_GetPreset(t *testing.T) {
	db := newTestDBWithNewModels(t)
	svc := NewStylePresetService(db)
	svc.InitSystemPresets()

	// 获取任意一个预设
	presets, _ := svc.ListPresets("", nil, false)
	if len(presets) == 0 {
		t.Fatal("应该有预设")
	}

	preset := presets[0]

	// 按 ID 获取
	fetched, err := svc.GetPreset(preset.ID)
	if err != nil {
		t.Fatalf("get preset: %v", err)
	}
	if fetched.ID != preset.ID {
		t.Errorf("ID 不匹配：期望 %d，实际 %d", preset.ID, fetched.ID)
	}
	if fetched.Name != preset.Name {
		t.Errorf("名称不匹配：期望 %s，实际 %s", preset.Name, fetched.Name)
	}
}

func TestStylePresetService_ApplyPreset(t *testing.T) {
	db := newTestDBWithNewModels(t)
	svc := NewStylePresetService(db)
	svc.InitSystemPresets()

	// 获取一个预设
	presets, _ := svc.ListPresets("", nil, false)
	if len(presets) == 0 {
		t.Fatal("应该有预设")
	}

	preset := presets[0]

	// 测试应用预设
	basePrompt := "a person standing in a room"
	prompt, negative, err := svc.ApplyPreset(preset.ID, basePrompt)
	if err != nil {
		t.Fatalf("apply preset: %v", err)
	}

	// 验证提示词已追加
	if prompt == basePrompt && preset.PromptTail != "" {
		t.Error("提示词应该被追加预设内容")
	}

	// 验证负面提示词
	if negative == "" {
		t.Error("应该有负面提示词")
	}
}

func TestStylePresetService_GetRecommendations(t *testing.T) {
	db := newTestDBWithNewModels(t)
	svc := NewStylePresetService(db)
	svc.InitSystemPresets()

	// 测试基于上下文的推荐
	ctx := SceneContext{
		Genre:     "复仇",
		Tone:      "暗黑",
		SceneType: "夜景",
	}

	recommendations, err := svc.GetRecommendations(ctx, 5)
	if err != nil {
		t.Fatalf("get recommendations: %v", err)
	}

	if len(recommendations) == 0 {
		t.Error("应该返回推荐")
	}

	// 验证返回的是推荐预设或热门预设
	for _, r := range recommendations {
		if r.ID == 0 {
			t.Error("推荐预设 ID 应该有效")
		}
	}
}

func TestStylePresetService_GetRecommendationsWithReasons(t *testing.T) {
	db := newTestDBWithNewModels(t)
	svc := NewStylePresetService(db)
	svc.InitSystemPresets()
	recs, err := svc.GetRecommendationsWithReasons(SceneContext{Genre: "复仇", Tone: "暗黑", SceneType: "夜景"}, 5)
	if err != nil || len(recs) == 0 {
		t.Fatalf("recommendations=%+v err=%v", recs, err)
	}
	for _, rec := range recs {
		if rec.Preset.ID == 0 || len(rec.Reasons) == 0 {
			t.Fatalf("recommendation lacks explanation: %+v", rec)
		}
	}
}

func TestStylePresetService_GetCategories(t *testing.T) {
	db := newTestDBWithNewModels(t)
	svc := NewStylePresetService(db)
	svc.InitSystemPresets()

	categories, err := svc.GetCategories()
	if err != nil {
		t.Fatalf("get categories: %v", err)
	}

	if len(categories) == 0 {
		t.Error("应该有分类")
	}

	// 验证分类结构
	for _, c := range categories {
		if c["value"] == nil || c["label"] == nil {
			t.Error("分类应该有 value 和 label")
		}
	}
}

func TestStylePresetService_CreatePreset(t *testing.T) {
	db := newTestDBWithNewModels(t)
	svc := NewStylePresetService(db)

	// 创建自定义预设
	preset := models.StylePreset{
		Name:         "我的自定义预设",
		Category:     models.StylePresetCinematic,
		PromptTail:   "custom style, unique look",
		NegativeTail: "blurry, low quality",
		Reason:       "测试用途",
		Tags:         "测试,自定义",
	}

	created, err := svc.CreatePreset(preset)
	if err != nil {
		t.Fatalf("create preset failed: %v", err)
	}

	if created.ID == 0 {
		t.Error("预设 ID 应该被设置")
	}

	if created.IsSystem {
		t.Error("自定义预设不应该是系统预设")
	}

	// 验证可以获取
	fetched, err := svc.GetPreset(created.ID)
	if err != nil {
		t.Fatalf("fetch created preset: %v", err)
	}
	if fetched.Name != preset.Name {
		t.Errorf("名称不匹配：期望 %s，实际 %s", preset.Name, fetched.Name)
	}
}

func TestStylePresetService_DeletePreset(t *testing.T) {
	db := newTestDBWithNewModels(t)
	svc := NewStylePresetService(db)

	// 创建自定义预设
	preset := models.StylePreset{
		Name:     "待删除预设",
		Category: models.StylePresetCinematic,
	}

	created, _ := svc.CreatePreset(preset)

	// 删除自定义预设应该成功
	err := svc.DeletePreset(created.ID)
	if err != nil {
		t.Fatalf("delete preset failed: %v", err)
	}

	// 验证删除
	_, err = svc.GetPreset(created.ID)
	if err == nil {
		t.Error("预设应该已被删除")
	}
}

func TestStylePresetService_IncrementUsageCount(t *testing.T) {
	db := newTestDBWithNewModels(t)
	svc := NewStylePresetService(db)
	svc.InitSystemPresets()

	// 获取一个预设
	presets, _ := svc.ListPresets("", nil, false)
	if len(presets) == 0 {
		t.Fatal("应该有预设")
	}

	initialCount := presets[0].UsageCount

	// 增加使用计数
	err := svc.IncrementUsageCount(presets[0].ID)
	if err != nil {
		t.Fatalf("increment usage count: %v", err)
	}

	// 验证计数增加
	fetched, _ := svc.GetPreset(presets[0].ID)
	if fetched.UsageCount != initialCount+1 {
		t.Errorf("使用计数应该增加：期望 %d，实际 %d", initialCount+1, fetched.UsageCount)
	}
}

func TestStylePresetService_SearchPresets(t *testing.T) {
	db := newTestDBWithNewModels(t)
	svc := NewStylePresetService(db)
	svc.InitSystemPresets()

	// 搜索预设
	results, err := svc.SearchPresets("电影", 10)
	if err != nil {
		t.Fatalf("search presets: %v", err)
	}

	if len(results) == 0 {
		t.Error("应该找到匹配的预设")
	}

	// 验证匹配
	for _, r := range results {
		found := false
		for _, tag := range []string{"电影感", "电影", "cinematic"} {
			if containsString(r.Tags, tag) {
				found = true
				break
			}
		}
		if !found && !containsString(r.Name, "电影") && !containsString(r.Reason, "电影") {
			t.Errorf("预设 %s 应该匹配搜索关键词", r.Name)
		}
	}
}

func containsString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestStylePresetService_ExportImport(t *testing.T) {
	db := newTestDBWithNewModels(t)
	svc := NewStylePresetService(db)
	svc.InitSystemPresets()

	// 导出预设
	exported, err := svc.ExportPresets("")
	if err != nil {
		t.Fatalf("export presets: %v", err)
	}

	if len(exported) == 0 {
		t.Error("应该导出预设数据")
	}

	// 导入预设（验证 JSON 格式）
	imported, err := svc.ImportPresets(exported)
	if err != nil {
		t.Fatalf("import presets: %v", err)
	}

	// 已存在的预设不会被重复导入
	if imported > 0 {
		// 验证导入数量合理
		t.Logf("导入了 %d 个新预设", imported)
	}
}
