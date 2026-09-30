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

func TestParseCharacterProfileJSONRepairsCommaAfterKnownObjectKey(t *testing.T) {
	raw := `{"name":"柳乐儿","role":"配角","appearance":"六七岁狐女","personality":"机敏","background":"幼年逃亡","relationships", {"舒寒":"同伴"},"emotions":"惊慌时睁大眼睛","habits":"抱紧拨浪鼓","wardrobe_detail":"鹅黄罗衣，软底绣鞋","lighting_mood":"柔和日光","color_palette":"鹅黄与翠绿"}`
	result, err := parseCharacterProfileJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if result.Name != "柳乐儿" || !strings.Contains(result.Relationships, "舒寒") {
		t.Fatalf("malformed known-key separator was not repaired: %+v", result)
	}
}

func TestParseCharacterProfileJSONNormalizesStructuredFields(t *testing.T) {
	raw := `{"name":"马脸男子","role":"配角","appearance":"马脸，细长眼","personality":["谨慎","善于观察"],"background":"外堂弟子","relationships":{"狐女":{"relation":"追捕目标","attitude":"戒备"},"同伴":"临时合作"},"emotions":"紧张时眯眼","habits":"握紧刀柄","wardrobe_detail":"青色道袍，黑色布靴","lighting_mood":"冷色侧光","color_palette":"青灰色"}`
	result, err := parseCharacterProfileJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"狐女", "追捕目标", "戒备", "同伴", "临时合作"} {
		if !strings.Contains(result.Relationships, want) {
			t.Fatalf("normalized relationships missing %q: %s", want, result.Relationships)
		}
	}
	for _, want := range []string{"谨慎", "善于观察"} {
		if !strings.Contains(result.Personality, want) {
			t.Fatalf("normalized personality missing %q: %s", want, result.Personality)
		}
	}
}

func TestValidateProfileAgeAgainstNovelSource(t *testing.T) {
	char := &models.Character{Name: "菜头"}
	source := `{"source_chapters":[{"content":"舒寒带着菜头离开。菜头是一名6到7岁的狐女，仍是幼童体态。"}]}`
	if err := validateProfileAgeAgainstSources("18岁江南少女，鹅蛋脸", char, source); err == nil {
		t.Fatal("expected novel age conflict to be rejected")
	}
	if err := validateProfileAgeAgainstSources("6到7岁狐女，幼童体态", char, source); err != nil {
		t.Fatalf("matching novel age rejected: %v", err)
	}
	liu := &models.Character{Name: "柳乐儿", Appearance: "18岁江南少女"}
	mixedSource := `其他角色是一名18岁少女。柳乐儿随即显出原形，她本是六七岁狐女，仍是幼童体态。`
	if err := validateProfileAgeAgainstSources("六七岁女童，狐女幼童体态", liu, mixedSource); err != nil {
		t.Fatalf("Chinese child age near character must override stale profile age: %v", err)
	}
	man := &models.Character{Name: "马脸男子"}
	unrelatedAge := `柳乐儿十二岁时曾见过此人。后来马脸男子拦住去路，书中没有交代他的年龄。`
	if err := validateProfileAgeAgainstSources("三十余岁男子，面部狭长", man, unrelatedAge); err != nil {
		t.Fatalf("unbound nearby age must not be attributed to character: %v", err)
	}
}

func TestGenerateProfileAcceptsExpandedAliasAndDoesNotBlockOnAge(t *testing.T) {
	ps := newTestProjectService(t)
	project := models.Project{Title: "小说改编"}
	if err := ps.db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	character := models.Character{ProjectID: project.ID, Name: "神秘男子", Role: "旧身份", Trait: "旧外貌", Style: "旧服装", ProfileStatus: models.ProfileStatusDraft}
	if err := ps.db.Create(&character).Error; err != nil {
		t.Fatal(err)
	}
	provider := &stubTextProvider{response: `{"name":"神秘男子（柳石）","role":"配角","appearance":"三十余岁男子，面容冷峻","personality":"谨慎","background":"身份隐秘","relationships":"与主角暂时同行","emotions":"沉默时目光锐利","habits":"下意识握住刀柄","wardrobe_detail":"深色劲装，黑色布靴","lighting_mood":"冷色侧光","color_palette":"黑灰色"}`}
	service := NewCharacterProfileService(ps.db, provider)
	if err := service.GenerateProfile(&character, &project, `神秘男子在十二岁少年身后现身，原文未说明其年龄。`); err != nil {
		t.Fatal(err)
	}
	var got models.Character
	if err := ps.db.First(&got, character.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.Appearance != "三十余岁男子，面容冷峻" {
		t.Fatalf("profile was not saved: %+v", got)
	}
	if got.Role != "配角" || got.Trait != got.Appearance || got.Style != got.WardrobeDetail {
		t.Fatalf("AI profile must refresh basic role/trait/style summaries: %+v", got)
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
	for _, want := range []string{"26岁女性", "黑色长发", "鹅蛋脸", "杏眼", "真人照片风格", "单人正面大头贴", "肩部以上构图", "象牙白羊毛风衣", "深灰内搭", "银色项链", "衣料完整覆盖肩部与胸口"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("提示词缺少 %q: %s", want, prompt)
		}
	}
	for _, forbidden := range []string{"角色固定配色", "影棚布光", "禁止显老", "法令纹", "眼袋"} {
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

func TestGenerateReferencePromptExcludesBelowShoulderWardrobeDetails(t *testing.T) {
	ps := newTestProjectService(t)
	p := models.Project{Title: "问仙", Genre: "古风仙侠、东方玄幻", Style: "古风仙侠人物设定、东方幻想美学"}
	ps.db.Create(&p)
	ch := models.Character{ProjectID: p.ID, Name: "清虚道人",
		Appearance:     "30岁左右的清瘦青年男性，面容清癯古拙，剑眉斜飞，双目细长有神，鼻梁挺直，唇薄色淡，皮肤偏白，头顶挽成道士发髻以乌木簪固定，身穿一件洗得有些发白的青色道袍，道袍下摆略有磨损",
		WardrobeDetail: "日常服饰：上身青色交领道袍以粗麻布制成颜色洗褪成淡青色，衣袖宽大袖口收紧以便施展法术，腰部系一条黑色丝绦，下摆长至膝下配有暗纹刺绣，脚穿黑色布靴"}
	ps.db.Create(&ch)
	prompt, err := NewCharacterProfileService(ps.db, nil).GenerateReferencePrompt(&ch, &p)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"单人正面大头贴", "肩部以上构图", "青色交领道袍", "粗麻布", "衣袖"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("portrait prompt missing %q: %s", want, prompt)
		}
	}
	for _, forbidden := range []string{"洗得有些发白", "道袍下摆", "腰部", "黑色丝绦", "膝下", "暗纹刺绣", "布靴"} {
		if strings.Contains(prompt, forbidden) {
			t.Fatalf("headshot prompt contains below-shoulder detail %q: %s", forbidden, prompt)
		}
	}
}

func TestHistoricalMalePortraitForbidsModernCrewCut(t *testing.T) {
	ps := newTestProjectService(t)
	project := models.Project{Title: "问仙", Genre: "古典修仙", Style: "真人写实"}
	ps.db.Create(&project)
	character := models.Character{ProjectID: project.ID, Name: "马脸男子", Role: "追兵", Appearance: "三十余岁男子，面部狭长，颧骨高耸，眼窝深陷，皮肤粗糙黝黑", WardrobeDetail: "深色交领短打，粗麻布材质"}
	ps.db.Create(&character)
	prompt, err := NewCharacterProfileService(ps.db, nil).GenerateReferencePrompt(&character, &project)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"中国古典男子蓄发", "黑色长发束起为发髻或束发", "严禁现代平头", "寸头", "侧剃", "渐变推剪"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("historical male portrait missing hair constraint %q: %s", want, prompt)
		}
	}
	// 即使数据库仍保存旧提示词，正式提交也必须补上时代发型约束。
	character.ReferencePrompt = "单人正面大头贴，三十余岁男子，深色交领短打"
	submission := buildPortraitPrompt(&project, &character)
	if !strings.Contains(submission, "严禁现代平头") || !strings.Contains(submission, "中国古典男子蓄发") {
		t.Fatalf("portrait submission omitted historical hair constraint: %s", submission)
	}
}

func TestGenerateReferencePromptPreservesBeardAndCultivationIdentity(t *testing.T) {
	ps := newTestProjectService(t)
	p := models.Project{Title: "问仙", Genre: "古典修仙", Style: "真人写实", Synopsis: "宗门林立，修士争夺大道机缘"}
	ps.db.Create(&p)
	ch := models.Character{ProjectID: p.ID, Name: "铁山", Role: "散修刀客，筑基期修士", Appearance: "30岁壮年男子，浓眉如刀，眼窝微陷，古铜肤色，唇上留短髭，下颌蓄有修剪整齐的络腮胡", Trait: "常年独行猎杀妖兽，气息沉稳凌厉", Background: "出身边荒散修，凭刀法踏入筑基期", WardrobeDetail: "黑色粗布劲装，暗红系绳，黑色布靴"}
	if err := ps.db.Create(&ch).Error; err != nil {
		t.Fatal(err)
	}
	prompt, err := NewCharacterProfileService(ps.db, nil).GenerateReferencePrompt(&ch, &p)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"30岁壮年男子", "短髭", "络腮胡", "题材：古典修仙", "画风：真人写实", "中国古典修仙世界", "身份：散修刀客，筑基期修士", "禁止现代造型"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("cultivation portrait missing %q: %s", want, prompt)
		}
	}
	if genreAt, faceAt := strings.Index(prompt, "题材：古典修仙"), strings.Index(prompt, "30岁壮年男子"); genreAt < 0 || faceAt < 0 || genreAt > faceAt {
		t.Fatalf("project genre must precede long character details: %s", prompt)
	}
}

func TestGenerateReferencePromptInfersXianxiaStyleForFoxCharacter(t *testing.T) {
	ps := newTestProjectService(t)
	p := models.Project{Title: "狐女"}
	if err := ps.db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	ch := models.Character{
		ProjectID:      p.ID,
		Name:           "小狐",
		Role:           "配角",
		Appearance:     "17-18岁年轻狐女，乌黑秀发束成双髻，一双琥珀色大眼睛",
		Trait:          "想要融入普通人的生活又无法完全隐藏妖精的身份",
		WardrobeDetail: "鹅黄色罗衣，交领襦裙，淡青色云纹",
	}
	if err := ps.db.Create(&ch).Error; err != nil {
		t.Fatal(err)
	}
	prompt, err := NewCharacterProfileService(ps.db, nil).GenerateReferencePrompt(&ch, &p)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"题材：古风仙侠、东方玄幻", "画风：古风仙侠人物设定、东方幻想美学", "中国古典修仙世界", "17-18岁年轻狐女"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("fox portrait missing inferred genre %q: %s", want, prompt)
		}
	}
	if genreAt, faceAt := strings.Index(prompt, "题材："), strings.Index(prompt, "17-18岁年轻狐女"); genreAt < 0 || faceAt < 0 || genreAt > faceAt {
		t.Fatalf("inferred genre must precede character details: %s", prompt)
	}
	for _, forbidden := range []string{"角色身份必须可辨：配角", "想要融入普通人的生活", "身份经历的可见气质依据"} {
		if strings.Contains(prompt, forbidden) {
			t.Fatalf("portrait prompt includes non-visual narrative %q: %s", forbidden, prompt)
		}
	}
}

func TestGenerateReferencePromptOmitsMotivationAndNonVisibleWardrobe(t *testing.T) {
	ps := newTestProjectService(t)
	p := models.Project{Title: "血刀会", Genre: "古风仙侠"}
	ps.db.Create(&p)
	ch := models.Character{
		ProjectID:      p.ID,
		Role:           "配角",
		Appearance:     "30岁左右的清瘦青年男性，面容清癯古拙，剑眉斜飞，双目细长有神，鼻梁挺直，唇薄色淡，皮肤偏白，头顶挽成道士发髻以乌木簪固定",
		Background:     "出身低微，从杂役爬到外堂弟子，渴望立功进入内堂获得修炼资源，此次追捕狐女是进阶的重要机会",
		Trait:          "善于钻营投机取巧但修为平平",
		WardrobeDetail: "30岁左右的清瘦青年男性，面容清癯，身穿洗得发白的青色道袍，道袍下摆略有磨损，足踏黑色薄底布靴，手持白马尾拂尘，日常服饰：上身青色交领道袍以粗麻布制成，衣袖宽大",
	}
	if err := ps.db.Create(&ch).Error; err != nil {
		t.Fatal(err)
	}
	prompt, err := NewCharacterProfileService(ps.db, nil).GenerateReferencePrompt(&ch, &p)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"题材：古风仙侠", "30岁左右的清瘦青年男性", "青色道袍"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("concise portrait missing %q: %s", want, prompt)
		}
	}
	for _, forbidden := range []string{"配角", "出身低微", "渴望立功", "追捕狐女", "投机取巧", "布靴", "手持白马尾拂尘"} {
		if strings.Contains(prompt, forbidden) {
			t.Fatalf("concise portrait includes non-visual or out-of-frame detail %q: %s", forbidden, prompt)
		}
	}
}

func TestPortraitStylingSelectsOneCoherentLook(t *testing.T) {
	ch := &models.Character{
		Appearance:     "乌黑长发及腰，日常梳低挽发髻，用一根素雅木簪固定。鹅蛋脸，丹凤眼。",
		WardrobeDetail: "日常服装——浅蓝与鹅黄两套常服。浅蓝色装扮：浅蓝色立领斜襟布衫，白色里衣，浅蓝腰带。鹅黄色装扮：鹅黄色对襟布衫。工作装束：挽起袖口。重要场合造型：月白礼服。",
	}
	style := portraitStyling(ch)
	for _, want := range []string{"浅蓝色立领斜襟布衫", "白色里衣"} {
		if !strings.Contains(style, want) {
			t.Fatalf("styling missing %q: %s", want, style)
		}
	}
	for _, forbidden := range []string{"低挽发髻", "木簪", "鹅黄色对襟", "工作装束", "月白礼服"} {
		if strings.Contains(style, forbidden) {
			t.Fatalf("styling mixed alternative %q: %s", forbidden, style)
		}
	}
}

func TestPortraitPromptRemovesConflictingRealisticSuffixForAnime(t *testing.T) {
	prompt := normalizePortraitStyle("国漫插画风格，肩部以上构图，写实风格", &models.Project{Style: "国漫插画"})
	if strings.Contains(prompt, "，写实风格") {
		t.Fatalf("conflicting style remains: %s", prompt)
	}
	if !strings.Contains(prompt, "国漫插画风格") {
		t.Fatalf("primary style lost: %s", prompt)
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

func TestValidateCharacterProfileAllowsOptionalRole(t *testing.T) {
	char := &models.Character{Name: "无明确身份角色", Appearance: "青年女性，黑色长发，清晰五官", Personality: "冷静", Background: "暂未揭示", Relationships: "暂无", Emotions: "克制", Habits: "轻敲桌面", WardrobeDetail: "深色长衫与布靴", LightingMood: "柔和侧光", ColorPalette: "深蓝灰", ReferencePrompt: "单人正面大头贴"}
	if err := validateCharacterProfile(char); err != nil {
		t.Fatalf("optional role blocked review: %v", err)
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
