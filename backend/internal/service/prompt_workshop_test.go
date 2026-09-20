package service

import (
	"errors"
	"testing"

	"comfyui-console/internal/models"
)

func TestPromptWorkshopProjectIsolationAndDraftHistory(t *testing.T) {
	db := newTestDBWithNewModels(t)
	svc := NewPromptWorkshopService(db, nil)
	p1 := models.Project{Title: "p1"}
	p2 := models.Project{Title: "p2"}
	db.Create(&p1)
	db.Create(&p2)
	s1 := models.Scene{ProjectID: p1.ID, Order: 1}
	s2 := models.Scene{ProjectID: p2.ID, Order: 1}
	db.Create(&s1)
	db.Create(&s2)
	shot1 := models.Shot{SceneID: s1.ID, Order: 1, ShotType: "中景", Duration: 2}
	shot2 := models.Shot{SceneID: s2.ID, Order: 1, ShotType: "中景", Duration: 2}
	db.Create(&shot1)
	db.Create(&shot2)

	if _, err := svc.BuildPromptForProject(p1.ID, "shot", shot1.ID, "{{subject}}", map[string]string{"subject": "镜头一"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.BuildPromptForProject(p1.ID, "shot", shot2.ID, "x", nil); err == nil {
		t.Fatal("cross-project shot was accepted")
	}
	history, err := svc.GetHistoryForProject(p1.ID, "shot", shot1.ID, 10)
	if err != nil || len(history) != 1 || history[0].ProjectID != p1.ID {
		t.Fatalf("scoped history = %+v, err=%v", history, err)
	}
	if _, err := svc.GetHistoryForProject(p2.ID, "shot", shot1.ID, 10); err == nil {
		t.Fatal("cross-project history was accepted")
	}
	if _, err := svc.RollbackForProject(p2.ID, "shot", shot2.ID, history[0].ID); err == nil {
		t.Fatal("cross-project rollback was accepted")
	}
	if _, err := svc.BuildPromptForProject(p1.ID, "shot", 0, "draft {{subject}}", map[string]string{"subject": "镜头"}); err != nil {
		t.Fatal(err)
	}
	var draftCount int64
	db.Model(&models.PromptVersion{}).Where("project_id = ? AND entity_id = 0", p1.ID).Count(&draftCount)
	if draftCount != 0 {
		t.Fatalf("unsaved draft wrote %d history rows", draftCount)
	}
}

func TestPromptWorkshopShotActionsNormalizeFivePartsAndRejectProse(t *testing.T) {
	db := newTestDBWithNewModels(t)
	project := models.Project{Title: "five parts"}
	db.Create(&project)
	scene := models.Scene{ProjectID: project.ID, Order: 1}
	db.Create(&scene)
	shot := models.Shot{SceneID: scene.ID, Order: 1, ShotType: "close", Duration: 1}
	db.Create(&shot)
	provider := &stubTextProvider{response: "```json\n{\"subject\":\"face\",\"action\":\"turns\",\"camera\":\"close-up\",\"lighting\":\"moonlight\",\"style\":\"film\"}\n```"}
	svc := NewPromptWorkshopService(db, provider)

	got, err := svc.OptimizePromptForProject(project.ID, "shot", shot.ID, "original", "context")
	want := "face\nturns\nclose-up\nmoonlight\nfilm"
	if err != nil || got != want {
		t.Fatalf("normalized optimize = %q, err=%v", got, err)
	}
	provider.response = "人物\n转身\n近景\n月光\n电影感"
	got, err = svc.TranslatePromptForProject(project.ID, "shot", shot.ID, want, "zh")
	if err != nil || got != provider.response {
		t.Fatalf("normalized translate = %q, err=%v", got, err)
	}

	var count int64
	db.Model(&models.PromptVersion{}).Where("project_id = ? AND entity_type = 'shot' AND entity_id = ?", project.ID, shot.ID).Count(&count)
	if count != 2 {
		t.Fatalf("valid actions recorded %d versions", count)
	}
	provider.response = "Here is an improved prompt with arbitrary prose."
	got, err = svc.OptimizePromptForProject(project.ID, "shot", shot.ID, want, "context")
	if err == nil || got != want {
		t.Fatalf("invalid prose accepted: got=%q err=%v", got, err)
	}
	db.Model(&models.PromptVersion{}).Where("project_id = ? AND entity_type = 'shot' AND entity_id = ?", project.ID, shot.ID).Count(&count)
	if count != 2 {
		t.Fatalf("invalid output recorded history; count=%d", count)
	}
}

func TestPromptWorkshopPairedOptimizationAndEchoMetadata(t *testing.T) {
	db := newTestDBWithNewModels(t)
	project := models.Project{Title: "paired"}
	db.Create(&project)
	scene := models.Scene{ProjectID: project.ID}
	db.Create(&scene)
	provider := &stubTextProvider{response: `{"chinese":"雨夜中的侦探，电影侧光","english":"detective in rain, cinematic side light"}`}
	svc := NewPromptWorkshopService(db, provider)
	result, err := svc.OptimizePromptDetailed(project.ID, "scene", scene.ID, "雨夜侦探", "", PromptOptimizationOptions{Paired: true, Iterations: 1, English: "detective in rain"})
	if err != nil || result.Chinese == "" || result.English == "" || !result.Metadata.Paired {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	provider.response = `{"chinese":"雨夜侦探","english":"detective in rain"}`
	result, err = svc.OptimizePromptDetailed(project.ID, "scene", scene.ID, "雨夜侦探", "", PromptOptimizationOptions{Paired: true, Iterations: 1, English: "detective in rain"})
	var echoErr *PromptOptimizationError
	if !errors.As(err, &echoErr) || echoErr.Code != "model_echo" || !result.Metadata.EchoDetected {
		t.Fatalf("expected echo metadata, result=%+v err=%v", result, err)
	}
	var count int64
	db.Model(&models.PromptVersion{}).Where("entity_id = ?", scene.ID).Count(&count)
	if count != 1 {
		t.Fatalf("echo wrote history; count=%d", count)
	}
}

func TestPromptWorkshopService_BuildPrompt(t *testing.T) {
	db := newTestDBWithNewModels(t)
	svc := NewPromptWorkshopService(db, nil)

	template := "请生成一张{{subject}}的{{style}}图片，尺寸{{size}}"
	params := map[string]string{
		"subject": "人物肖像",
		"style":   "写实风格",
		"size":    "1024x1024",
	}

	result, err := svc.BuildPrompt("scene", 1, template, params)
	if err != nil {
		t.Fatalf("build prompt failed: %v", err)
	}

	expected := "请生成一张人物肖像的写实风格图片，尺寸1024x1024"
	if result != expected {
		t.Errorf("提示词构建错误：\n期望：%s\n实际：%s", expected, result)
	}
}

func TestPromptWorkshopService_BuildPrompt_PartialReplacement(t *testing.T) {
	db := newTestDBWithNewModels(t)
	svc := NewPromptWorkshopService(db, nil)

	template := "请生成{{subject}}的{{style}}图片"
	params := map[string]string{
		"subject": "风景",
	}

	result, err := svc.BuildPrompt("scene", 1, template, params)
	if err != nil {
		t.Fatalf("build prompt failed: %v", err)
	}

	// 部分替换时，未提供的占位符应该保留
	if result != "请生成风景的{{style}}图片" {
		t.Errorf("部分替换结果错误：%s", result)
	}
}

func TestPromptWorkshopService_GetHistory(t *testing.T) {
	db := newTestDBWithNewModels(t)
	svc := NewPromptWorkshopService(db, nil)

	// 创建一些历史记录
	versions := []models.PromptVersion{
		{EntityType: "scene", EntityID: 1, Content: "内容1", Action: "build"},
		{EntityType: "scene", EntityID: 1, Content: "内容2", Action: "optimize"},
		{EntityType: "scene", EntityID: 1, Content: "内容3", Action: "translate"},
	}
	for _, v := range versions {
		db.Create(&v)
	}

	// 测试获取历史
	history, err := svc.GetHistory("scene", 1, 10)
	if err != nil {
		t.Fatalf("get history failed: %v", err)
	}

	if len(history) != 3 {
		t.Errorf("期望 3 条历史，实际 %d 条", len(history))
	}

	// 验证按时间降序
	for i := 1; i < len(history); i++ {
		if history[i-1].CreatedAt.Before(history[i].CreatedAt) {
			t.Error("历史应该按时间降序排列")
		}
	}
}

func TestPromptWorkshopService_GetHistory_Limit(t *testing.T) {
	db := newTestDBWithNewModels(t)
	svc := NewPromptWorkshopService(db, nil)

	// 创建 5 条历史
	for i := 0; i < 5; i++ {
		db.Create(&models.PromptVersion{
			EntityType: "scene",
			EntityID:   1,
			Content:    "内容",
			Action:     "build",
		})
	}

	// 测试限制
	history, _ := svc.GetHistory("scene", 1, 3)
	if len(history) != 3 {
		t.Errorf("期望 3 条历史，实际 %d 条", len(history))
	}
}

func TestPromptWorkshopService_GetVersion(t *testing.T) {
	db := newTestDBWithNewModels(t)
	svc := NewPromptWorkshopService(db, nil)

	// 创建版本
	version := models.PromptVersion{
		EntityType: "shot",
		EntityID:   1,
		Content:    "测试内容",
		Action:     "optimize",
	}
	db.Create(&version)

	// 测试获取特定版本
	fetched, err := svc.GetVersion(version.ID)
	if err != nil {
		t.Fatalf("get version failed: %v", err)
	}

	if fetched.Content != version.Content {
		t.Errorf("内容不匹配：期望 %s，实际 %s", version.Content, fetched.Content)
	}
}

func TestPromptWorkshopService_Rollback(t *testing.T) {
	db := newTestDBWithNewModels(t)
	svc := NewPromptWorkshopService(db, nil)

	// 创建版本链
	v1 := models.PromptVersion{
		EntityType: "scene",
		EntityID:   1,
		Content:    "版本1",
		Action:     "build",
	}
	db.Create(&v1)

	v2 := models.PromptVersion{
		EntityType: "scene",
		EntityID:   1,
		Content:    "版本2",
		Action:     "optimize",
	}
	db.Create(&v2)

	// 回滚到 v1
	rolledBack, err := svc.Rollback("scene", 1, v1.ID)
	if err != nil {
		t.Fatalf("rollback failed: %v", err)
	}

	if rolledBack != "版本1" {
		t.Errorf("回滚内容错误：期望 版本1，实际 %s", rolledBack)
	}

	// 验证新版本已创建
	var newVersion models.PromptVersion
	db.Where("entity_type = ? AND entity_id = ?", "scene", 1).
		Order("created_at DESC").First(&newVersion)

	if newVersion.Content != "版本1" {
		t.Errorf("新版本内容错误：%s", newVersion.Content)
	}
}

func TestPromptWorkshopService_Rollback_EntityMismatch(t *testing.T) {
	db := newTestDBWithNewModels(t)
	svc := NewPromptWorkshopService(db, nil)

	// 创建版本
	v1 := models.PromptVersion{
		EntityType: "scene",
		EntityID:   1,
		Content:    "内容",
		Action:     "build",
	}
	db.Create(&v1)

	// 尝试用错误的实体类型回滚
	_, err := svc.Rollback("shot", 1, v1.ID)
	if err == nil {
		t.Error("应该返回错误：版本不匹配")
	}
}

func TestPromptWorkshopService_CompareVersions(t *testing.T) {
	db := newTestDBWithNewModels(t)
	svc := NewPromptWorkshopService(db, nil)

	// 创建两个版本
	v1 := models.PromptVersion{
		EntityType: "scene",
		EntityID:   1,
		Content:    "abc",
		Action:     "build",
	}
	db.Create(&v1)

	v2 := models.PromptVersion{
		EntityType: "scene",
		EntityID:   1,
		Content:    "abcdefg",
		Action:     "optimize",
	}
	db.Create(&v2)

	// 比较版本
	comparison, err := svc.CompareVersions(v1.ID, v2.ID)
	if err != nil {
		t.Fatalf("compare versions failed: %v", err)
	}

	if comparison["is_same"].(bool) {
		t.Error("版本应该不同")
	}

	if comparison["length_change"].(int) != 4 {
		t.Errorf("长度变化错误：期望 4，实际 %d", comparison["length_change"])
	}
}

func TestPromptWorkshopService_DeleteOldHistory(t *testing.T) {
	db := newTestDBWithNewModels(t)
	svc := NewPromptWorkshopService(db, nil)

	// 创建 5 条历史
	for i := 0; i < 5; i++ {
		db.Create(&models.PromptVersion{
			EntityType: "scene",
			EntityID:   1,
			Content:    "内容",
			Action:     "build",
		})
	}

	// 删除旧历史，保留 2 条
	deleted, err := svc.DeleteOldHistory("scene", 1, 2)
	if err != nil {
		t.Fatalf("delete old history failed: %v", err)
	}

	if deleted != 3 {
		t.Errorf("应该删除 3 条，实际删除 %d 条", deleted)
	}

	// 验证剩余数量
	var count int64
	db.Model(&models.PromptVersion{}).
		Where("entity_type = ? AND entity_id = ?", "scene", 1).
		Count(&count)

	if count != 2 {
		t.Errorf("应该剩余 2 条，实际剩余 %d 条", count)
	}
}

func TestPromptWorkshopService_GetHistoryStatistics(t *testing.T) {
	db := newTestDBWithNewModels(t)
	svc := NewPromptWorkshopService(db, nil)

	// 创建不同操作的版本
	versions := []models.PromptVersion{
		{EntityType: "scene", EntityID: 1, Content: "1", Action: "build"},
		{EntityType: "scene", EntityID: 1, Content: "2", Action: "build"},
		{EntityType: "scene", EntityID: 1, Content: "3", Action: "optimize"},
		{EntityType: "scene", EntityID: 1, Content: "4", Action: "translate"},
	}
	for _, v := range versions {
		db.Create(&v)
	}

	// 获取统计
	stats, err := svc.GetHistoryStatistics("scene", 1)
	if err != nil {
		t.Fatalf("get history statistics failed: %v", err)
	}

	if stats["total_versions"].(int64) != 4 {
		t.Errorf("总版本数错误：期望 4，实际 %d", stats["total_versions"])
	}

	byAction := stats["by_action"].(map[string]int64)
	if byAction["build"] != 2 {
		t.Errorf("build 操作数错误：期望 2，实际 %d", byAction["build"])
	}
}

func TestPromptWorkshopService_ParsePromptTemplate(t *testing.T) {
	db := newTestDBWithNewModels(t)
	svc := NewPromptWorkshopService(db, nil)

	tests := []struct {
		name      string
		template  string
		wantCount int
		wantFirst string
	}{
		{
			name:      "单个占位符",
			template:  "请生成{{subject}}",
			wantCount: 1,
			wantFirst: "subject",
		},
		{
			name:      "多个占位符",
			template:  "{{a}}和{{b}}以及{{c}}",
			wantCount: 3,
			wantFirst: "a",
		},
		{
			name:      "无占位符",
			template:  "普通文本",
			wantCount: 0,
			wantFirst: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			placeholders, err := svc.ParsePromptTemplate(tt.template)
			if err != nil {
				t.Fatalf("parse template failed: %v", err)
			}

			if len(placeholders) != tt.wantCount {
				t.Errorf("占位符数量错误：期望 %d，实际 %d", tt.wantCount, len(placeholders))
			}

			if tt.wantCount > 0 && placeholders[0] != tt.wantFirst {
				t.Errorf("第一个占位符错误：期望 %s，实际 %s", tt.wantFirst, placeholders[0])
			}
		})
	}
}

func TestPromptWorkshopService_ValidatePromptTemplate(t *testing.T) {
	db := newTestDBWithNewModels(t)
	svc := NewPromptWorkshopService(db, nil)

	tests := []struct {
		name      string
		template  string
		wantValid bool
		wantMsg   string
	}{
		{
			name:      "有效模板",
			template:  "请生成{{subject}}的{{style}}图片",
			wantValid: true,
		},
		{
			name:      "空模板",
			template:  "",
			wantValid: false,
			wantMsg:   "模板不能为空",
		},
		{
			name:      "未闭合的占位符",
			template:  "请生成{{subject的图片",
			wantValid: false,
			wantMsg:   "占位符未匹配",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			valid, msg := svc.ValidatePromptTemplate(tt.template)

			if valid != tt.wantValid {
				t.Errorf("验证结果错误：期望 %v，实际 %v", tt.wantValid, valid)
			}

			if !tt.wantValid && tt.wantMsg != "" {
				if msg == "" {
					t.Error("应该返回错误信息")
				}
			}
		})
	}
}

func TestPromptWorkshopService_GeneratePromptFromPreset(t *testing.T) {
	db := newTestDBWithNewModels(t)
	svc := NewPromptWorkshopService(db, nil)

	base := "a person"
	presetTail := "cinematic style, film grain"
	negativeTail := "blurry, cartoon"

	prompt, negative := svc.GeneratePromptFromPreset(base, presetTail, negativeTail)

	expectedPrompt := "a person, cinematic style, film grain"
	if prompt != expectedPrompt {
		t.Errorf("提示词错误：\n期望：%s\n实际：%s", expectedPrompt, prompt)
	}

	if negative != negativeTail {
		t.Errorf("负面提示词错误：%s", negative)
	}
}
