package service

import (
	"encoding/json"
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

func TestParseCharacterProfileJSON(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "纯JSON",
			in:   `{"name":"林夏","role":"女主","appearance":"黑色长发","personality":"温柔","background":"出身名门","relationships":"与男主青梅竹马","emotions":"微笑","habits":"摸头发","wardrobe_detail":"淡蓝色连衣裙","lighting_mood":"柔和","color_palette":"蓝色系"}`,
			want: "林夏",
		},
		{
			name: "markdown包裹",
			in:   "```json\n{\"name\":\"林夏\",\"role\":\"女主\",\"appearance\":\"黑色长发\",\"personality\":\"温柔\",\"background\":\"出身名门\",\"relationships\":\"与男主青梅竹马\",\"emotions\":\"微笑\",\"habits\":\"摸头发\",\"wardrobe_detail\":\"淡蓝色连衣裙\",\"lighting_mood\":\"柔和\",\"color_palette\":\"蓝色系\"}\n```",
			want: "林夏",
		},
		{
			name: "前后杂文",
			in:   "好的，这是角色档案：\n{\"name\":\"林夏\",\"role\":\"女主\",\"appearance\":\"黑色长发\",\"personality\":\"温柔\",\"background\":\"出身名门\",\"relationships\":\"与男主青梅竹马\",\"emotions\":\"微笑\",\"habits\":\"摸头发\",\"wardrobe_detail\":\"淡蓝色连衣裙\",\"lighting_mood\":\"柔和\",\"color_palette\":\"蓝色系\"}\n希望你喜欢",
			want: "林夏",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := parseCharacterProfileJSON(tc.in)
			if err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if result.Name != tc.want {
				t.Fatalf("%s: name = %q, want %q", tc.name, result.Name, tc.want)
			}
			// 验证必要字段
			if result.Appearance == "" {
				t.Fatalf("%s: appearance 为空", tc.name)
			}
			if result.Personality == "" {
				t.Fatalf("%s: personality 为空", tc.name)
			}
		})
	}
}

func TestParseCharacterProfileJSONInvalid(t *testing.T) {
	cases := []string{
		`{"name":"林夏"}`, // 缺少必要字段
		`这不是 JSON`,      // 非JSON
		`{"role":"女主"}`, // 缺少name字段
	}

	for _, raw := range cases {
		_, err := parseCharacterProfileJSON(raw)
		if err == nil {
			t.Errorf("期望解析失败: %s", raw)
		}
	}
}

func TestGenerateReferencePromptCompilesLumxDimensions(t *testing.T) {
	ps := newTestProjectService(t)
	p := models.Project{Title: "测试", Style: "真人写实"}
	if err := ps.db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	ch := models.Character{ProjectID: p.ID, Name: "林夏", Role: "女主",
		Appearance:     "26岁女性，黑色长发，鹅蛋脸，杏眼，自然细眉，清瘦",
		WardrobeDetail: "象牙白羊毛风衣，深灰内搭，银色项链",
		LightingMood:   "柔和冷色侧光", ColorPalette: "象牙白、深灰、银色",
		ProfileStatus: models.ProfileStatusApproved, ReviewNote: "旧审核", Portrait: "old.png"}
	if err := ps.db.Create(&ch).Error; err != nil {
		t.Fatal(err)
	}
	service := NewCharacterProfileService(ps.db, nil)
	prompt, err := service.GenerateReferencePrompt(&ch, &p)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"26岁女性", "黑色长发", "鹅蛋脸", "杏眼", "真人照片风格", "单人正面大头贴", "肩部以上构图"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("提示词缺少 %q: %s", want, prompt)
		}
	}
	for _, forbidden := range []string{"羊毛风衣", "深灰内搭", "银色项链", "角色固定配色", "影棚布光", "禁止显老", "法令纹", "眼袋"} {
		if strings.Contains(prompt, forbidden) {
			t.Fatalf("大头贴提示词混入 %q: %s", forbidden, prompt)
		}
	}
	var got models.Character
	ps.db.First(&got, ch.ID)
	if got.ProfileStatus != models.ProfileStatusApproved || got.ReviewNote != "旧审核" || got.Portrait != "" {
		t.Fatalf("已审核档案生成确定性提示词后应保持审核状态并清除旧标准像: %+v", got)
	}
}

func TestApproveProfileRequiresCompleteProfileAndPrompt(t *testing.T) {
	ps := newTestProjectService(t)
	service := NewCharacterProfileService(ps.db, nil)
	ch := models.Character{ProjectID: 1, Name: "林夏", Role: "女主"}
	if err := ps.db.Create(&ch).Error; err != nil {
		t.Fatal(err)
	}
	if err := service.ApproveProfile(&ch, ""); err == nil || !strings.Contains(err.Error(), "不完整") {
		t.Fatalf("不完整档案应拒绝审核，err=%v", err)
	}
	ch.Appearance, ch.Personality, ch.Background = "外貌", "性格", "背景"
	ch.Relationships, ch.Emotions, ch.Habits = "关系", "情绪", "习惯"
	ch.WardrobeDetail, ch.LightingMood, ch.ColorPalette = "服装", "光影", "色板"
	if err := service.ApproveProfile(&ch, ""); err == nil || !strings.Contains(err.Error(), "参考像提示词") {
		t.Fatalf("缺少提示词应拒绝审核，err=%v", err)
	}
	ch.ReferencePrompt = "单人参考像"
	if err := service.ApproveProfile(&ch, "通过"); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateProfileAllowsClearingAndResetsApproval(t *testing.T) {
	ps := newTestProjectService(t)
	service := NewCharacterProfileService(ps.db, nil)
	ch := models.Character{ProjectID: 1, Name: "林夏", Appearance: "旧外貌", ReferencePrompt: "旧提示词", ProfileStatus: models.ProfileStatusApproved, ReviewNote: "旧审核", Portrait: "old.png"}
	if err := ps.db.Create(&ch).Error; err != nil {
		t.Fatal(err)
	}
	if err := service.UpdateProfile(&ch, models.Character{}); err != nil {
		t.Fatal(err)
	}
	var got models.Character
	ps.db.First(&got, ch.ID)
	if got.Appearance != "" || got.ReferencePrompt != "" || got.ProfileStatus != models.ProfileStatusDraft || got.ReviewNote != "" || got.Portrait != "" {
		t.Fatalf("完整替换未生效或旧标准像未失效: %+v", got)
	}
}

func TestGenerateProfileClearsDerivedState(t *testing.T) {
	ps := newTestProjectService(t)
	provider := &stubTextProvider{response: `{"name":"林夏","role":"女主","appearance":"新外貌","personality":"坚韧","background":"背景","relationships":"关系","emotions":"克制","habits":"握项链","wardrobe_detail":"白风衣","lighting_mood":"柔光","color_palette":"白灰"}`}
	service := NewCharacterProfileService(ps.db, provider)
	p := models.Project{Title: "测试"}
	ps.db.Create(&p)
	ch := models.Character{ProjectID: p.ID, Name: "林夏", ReferencePrompt: "旧提示词", ProfileStatus: models.ProfileStatusApproved, ReviewNote: "旧审核", Portrait: "old.png"}
	ps.db.Create(&ch)
	if err := service.GenerateProfile(&ch, &p, ""); err != nil {
		t.Fatal(err)
	}
	var got models.Character
	ps.db.First(&got, ch.ID)
	if got.ReferencePrompt != "" || got.ProfileStatus != models.ProfileStatusDraft || got.ReviewNote != "" || got.Portrait != "" {
		t.Fatalf("重生成档案未清除派生状态: %+v", got)
	}
}

func TestExtractJSON(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"无包裹", `{"name":"test"}`, `{"name":"test"}`},
		{"json包裹", "```json\n{\"name\":\"test\"}\n```", `{"name":"test"}`},
		{"code包裹", "```\n{\"name\":\"test\"}\n```", `{"name":"test"}`},
		{"多余空白", "  ```json\n{\"name\":\"test\"}\n```  ", `{"name":"test"}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractJSON(tc.in)
			if got != tc.want {
				t.Fatalf("%s: got %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

func TestCoalesceField(t *testing.T) {
	cases := []struct {
		name     string
		primary  string
		fallback string
		want     string
	}{
		{"primary有值", "primary", "fallback", "primary"},
		{"primary为空", "", "fallback", "fallback"},
		{"primary为空白", "  ", "fallback", "fallback"},
		{"两者都有值", "primary", "fallback", "primary"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := coalesceField(tc.primary, tc.fallback)
			if got != tc.want {
				t.Fatalf("coalesceField(%q, %q) = %q, want %q", tc.primary, tc.fallback, got, tc.want)
			}
		})
	}
}

func TestCharacterProfileResultJSON(t *testing.T) {
	// 验证 JSON 序列化
	result := &characterProfileResult{
		Name:           "林夏",
		Role:           "女主",
		Appearance:     "黑色长发",
		Personality:    "温柔",
		Background:     "出身名门",
		Relationships:  "与男主青梅竹马",
		Emotions:       "微笑",
		Habits:         "摸头发",
		WardrobeDetail: "淡蓝色连衣裙",
		LightingMood:   "柔和",
		ColorPalette:   "蓝色系",
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("JSON 序列化失败: %v", err)
	}

	var parsed characterProfileResult
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("JSON 反序列化失败: %v", err)
	}

	if parsed.Name != result.Name {
		t.Errorf("Name 不匹配: got %s, want %s", parsed.Name, result.Name)
	}
	if parsed.Appearance != result.Appearance {
		t.Errorf("Appearance 不匹配: got %s, want %s", parsed.Appearance, result.Appearance)
	}
}
