package service

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

var testSubjectTagPattern = regexp.MustCompile(`<Subject (\d+)>`)

// 视频提示词原先把任何角色参考都写成 four-view。只有标准像的角色被这样描述时，
// 模型会去找并不存在的多视角信息，而分镜图提示词写的是「标准像」，两份提示词
// 就对同一张图给出了互相矛盾的类型。
func TestVideoReferenceDescriptionKeepsActualReferenceType(t *testing.T) {
	for _, tc := range []struct {
		desc string
		want string
	}{
		{"角色「金虎」四视图", "four-view reference"},
		{"角色「钢蛋儿」标准像", "single standard portrait reference"},
		{"角色「某人」标准照（历史版本）", "single standard portrait reference"},
		{"角色「某人」定妆照", "single standard portrait reference"},
	} {
		got := englishH3ReferenceDescription(tc.desc, 1)
		if !strings.Contains(got, tc.want) {
			t.Fatalf("%q 应描述为 %q，实际为 %q", tc.desc, tc.want, got)
		}
		if tc.want != "four-view reference" && strings.Contains(got, "four-view") {
			t.Fatalf("%q 被误报成四视图: %q", tc.desc, got)
		}
	}
}

// subjectCharacterBindings 从 subject_definitions 段落里提取 <Subject N> 绑定的角色名。
func subjectCharacterBindings(section string, names []string) map[string]string {
	out := map[string]string{}
	for _, raw := range strings.Split(section, "\n") {
		line := strings.TrimSpace(raw)
		match := testSubjectTagPattern.FindStringSubmatch(line)
		if line == "" || match == nil {
			continue
		}
		for _, name := range names {
			// 最长匹配优先：「金虎手下甲」包含「金虎」。
			if prev, seen := out[match[1]]; strings.Contains(line, name) && (!seen || len(name) > len(prev)) {
				out[match[1]] = name
			}
		}
	}
	return out
}

// 这是用户报告的原始故障：同一个 <Subject N> 在分镜图提示词和视频提示词里
// 指向不同的人物。两份提示词由两个独立构造器生成，但共用同一套位置编号，
// 所以参考图顺序一旦分叉，正文里的人物绑定就会错位。
func TestSubjectBindingsAgreeBetweenImageAndVideoPrompts(t *testing.T) {
	db := setupTestDB(t)
	if err := db.AutoMigrate(&models.Asset{}, &models.CharacterOutfit{}, &models.SceneCharacterOutfit{}); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "绑定一致", Style: "3D国漫"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	lead := models.Character{ProjectID: project.ID, Name: "金虎", Sheet: "jinhu-sheet.png"}
	guard := models.Character{ProjectID: project.ID, Name: "金虎手下甲", Sheet: "shouxia-sheet.png"}
	sidekick := models.Character{ProjectID: project.ID, Name: "钢蛋儿", Sheet: "gangdan-sheet.png"}
	for _, ch := range []*models.Character{&lead, &guard, &sidekick} {
		if err := db.Create(ch).Error; err != nil {
			t.Fatal(err)
		}
	}
	office := models.Asset{ProjectID: project.ID, Kind: AssetKindLocation, Name: "金煌娱乐城顶楼办公室", Image: "office.png"}
	cigar := models.Asset{ProjectID: project.ID, Kind: AssetKindProp, Name: "金虎雪茄", Image: "cigar.png"}
	for _, a := range []*models.Asset{&office, &cigar} {
		if err := db.Create(a).Error; err != nil {
			t.Fatal(err)
		}
	}

	// 界面勾选四视图时两个用途标志同时为真；这里保存顺序把场景排在最前，
	// 且角色保存顺序与出场顺序不同，正是旧代码两侧分叉的条件。
	saved := fmt.Sprintf(`[
		{"source_type":"asset","source_id":%d,"variant":"image","use_krea2":true,"use_h3":true},
		{"source_type":"character","source_id":%d,"variant":"sheet","use_krea2":true,"use_h3":true},
		{"source_type":"character","source_id":%d,"variant":"sheet","use_krea2":true,"use_h3":true},
		{"source_type":"character","source_id":%d,"variant":"sheet","use_krea2":true,"use_h3":true},
		{"source_type":"asset","source_id":%d,"variant":"image","use_krea2":true,"use_h3":true}
	]`, office.ID, lead.ID, guard.ID, sidekick.ID, cigar.ID)
	scene := models.Scene{
		ProjectID: project.ID, EpisodeN: 1, Order: 1,
		Characters: "金虎,钢蛋儿,金虎手下甲", VisibleCharacters: "金虎,钢蛋儿,金虎手下甲",
		LocationName: "金煌娱乐城顶楼办公室", Props: "金虎雪茄",
		ImageFile: "storyboard.png", Duration: 8,
		ImagePrompt:         "[Shot 1] 金虎坐在办公桌后，金虎手下甲站在门边，钢蛋儿低头站在地毯中央。",
		VideoPrompt:         "[Shot 1] 金虎抬手，金虎手下甲上前一步，钢蛋儿后退。",
		ReferenceImagesJSON: saved,
	}
	if err := db.Create(&scene).Error; err != nil {
		t.Fatal(err)
	}

	ps := &ProjectService{db: db}
	_, imageLines, _ := ps.selectedSceneReferenceFiles(&scene, "krea2")
	_, videoLines := ps.sceneVideoReferenceFiles(&scene, fmt.Sprint(project.ID))
	imagePrompt := buildH3StoryboardPrompt(&scene, &project, imageLines)
	videoPrompt := buildMiniMaxH3RefPrompt(&scene, &project, nil, videoLines)

	names := []string{"金虎", "金虎手下甲", "钢蛋儿"}
	imageBindings := subjectCharacterBindings(h3PromptSection(imagePrompt, "subject_definitions:"), names)
	videoBindings := subjectCharacterBindings(h3PromptSection(videoPrompt, "subject_definitions:"), names)

	if len(imageBindings) != len(names) {
		t.Fatalf("分镜图提示词未绑定全部人物: %v\n%s", imageBindings, imagePrompt)
	}
	if len(videoBindings) != len(names) {
		t.Fatalf("视频提示词未绑定全部人物: %v\n%s", videoBindings, videoPrompt)
	}
	for n, imageName := range imageBindings {
		if videoName := videoBindings[n]; videoName != imageName {
			t.Fatalf("<Subject %s> 在两份提示词里指向不同人物: 分镜图=%q 视频=%q\n分镜图参考:\n%s\n视频参考:\n%s",
				n, imageName, videoName, strings.Join(imageLines, "\n"), strings.Join(videoLines, "\n"))
		}
	}
	// 所选四视图必须真的被引用，而不是被静默丢弃。
	if !strings.Contains(imagePrompt, "四视图") {
		t.Fatalf("分镜图提示词没有引用所选四视图:\n%s", imagePrompt)
	}
}

// 分镜图提示词与视频提示词共用同一套 <Picture N> 编号。两条路径各自排序时，
// 同一个编号会在两份提示词里指向不同的人物，LLM 写进提示词正文的 <Subject N>
// 也就跟着错位。这里锁定“两侧顺序逐位一致、视频只在末尾多一张分镜帧”。
func TestImageAndVideoReferenceOrderStayIdentical(t *testing.T) {
	db := setupTestDB(t)
	if err := db.AutoMigrate(&models.Asset{}, &models.CharacterOutfit{}, &models.SceneCharacterOutfit{}); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "参考图顺序"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	lead := models.Character{ProjectID: project.ID, Name: "金虎", Sheet: "jinhu-sheet.png"}
	guard := models.Character{ProjectID: project.ID, Name: "金虎手下甲", Sheet: "shouxia-sheet.png"}
	sidekick := models.Character{ProjectID: project.ID, Name: "钢蛋儿", Sheet: "gangdan-sheet.png"}
	for _, ch := range []*models.Character{&lead, &guard, &sidekick} {
		if err := db.Create(ch).Error; err != nil {
			t.Fatal(err)
		}
	}
	office := models.Asset{ProjectID: project.ID, Kind: AssetKindLocation, Name: "金煌娱乐城顶楼办公室", Image: "office.png"}
	cigar := models.Asset{ProjectID: project.ID, Kind: AssetKindProp, Name: "金虎雪茄", Image: "cigar.png"}
	for _, a := range []*models.Asset{&office, &cigar} {
		if err := db.Create(a).Error; err != nil {
			t.Fatal(err)
		}
	}

	// 保存顺序把场景排在最前，且手下甲在钢蛋儿之前；而出场顺序是
	// 金虎→钢蛋儿→金虎手下甲。视频侧若按出场顺序重排，第 2、3 位就会与分镜图互换。
	saved := fmt.Sprintf(`[
		{"source_type":"asset","source_id":%d,"variant":"image","use_krea2":true,"use_h3":true},
		{"source_type":"character","source_id":%d,"variant":"sheet","use_krea2":true,"use_h3":true},
		{"source_type":"character","source_id":%d,"variant":"sheet","use_krea2":true,"use_h3":true},
		{"source_type":"character","source_id":%d,"variant":"sheet","use_krea2":true,"use_h3":true},
		{"source_type":"asset","source_id":%d,"variant":"image","use_krea2":true,"use_h3":true}
	]`, office.ID, lead.ID, guard.ID, sidekick.ID, cigar.ID)
	scene := models.Scene{
		ProjectID: project.ID, EpisodeN: 1, Order: 1,
		Characters: "金虎,钢蛋儿,金虎手下甲", VisibleCharacters: "金虎,钢蛋儿,金虎手下甲",
		ImageFile: "storyboard.png", ReferenceImagesJSON: saved,
	}
	if err := db.Create(&scene).Error; err != nil {
		t.Fatal(err)
	}

	ps := &ProjectService{db: db}
	imageRefs, imageLines, explicit := ps.selectedSceneReferenceFiles(&scene, "krea2")
	if !explicit {
		t.Fatalf("显式选择未被识别: refs=%+v", imageRefs)
	}
	videoRefs, videoLines := ps.sceneVideoReferenceFiles(&scene, fmt.Sprint(project.ID))

	names := func(metas []FileMeta) []string {
		out := make([]string, 0, len(metas))
		for _, m := range metas {
			out = append(out, m.Name)
		}
		return out
	}
	imageFiles, videoFiles := names(imageRefs), names(videoRefs)

	if len(videoFiles) != len(imageFiles)+1 {
		t.Fatalf("视频应为分镜图参考图加一张分镜帧: image=%v video=%v", imageFiles, videoFiles)
	}
	if videoFiles[len(videoFiles)-1] != scene.ImageFile {
		t.Fatalf("分镜帧必须排在最后，否则会顶掉前面的编号: %v", videoFiles)
	}
	for i := range imageFiles {
		if imageFiles[i] != videoFiles[i] {
			t.Fatalf("<Picture %d> 在两份提示词里指向不同素材: image=%q video=%q\nimage=%v\nvideo=%v",
				i+1, imageFiles[i], videoFiles[i], imageLines, videoLines)
		}
	}
	// 人物身份图必须先于环境图：SelfLift 对前序图片权重更敏感。
	if imageFiles[0] != lead.Sheet || imageFiles[3] != office.Image {
		t.Fatalf("人物参考图未被提到环境之前: %v", imageFiles)
	}
}

// 分用途勾选仍然有效：只勾“生成分镜图时使用”的参考图不进入视频参考列表。
func TestPerPurposeReferenceSelectionStillFilters(t *testing.T) {
	db := setupTestDB(t)
	if err := db.AutoMigrate(&models.Asset{}, &models.CharacterOutfit{}, &models.SceneCharacterOutfit{}); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "分用途勾选"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	character := models.Character{ProjectID: project.ID, Name: "金虎", Sheet: "jinhu-sheet.png"}
	if err := db.Create(&character).Error; err != nil {
		t.Fatal(err)
	}
	cigar := models.Asset{ProjectID: project.ID, Kind: AssetKindProp, Name: "金虎雪茄", Image: "cigar.png"}
	if err := db.Create(&cigar).Error; err != nil {
		t.Fatal(err)
	}
	saved := fmt.Sprintf(`[
		{"source_type":"character","source_id":%d,"variant":"sheet","use_krea2":true,"use_h3":false},
		{"source_type":"asset","source_id":%d,"variant":"image","use_krea2":true,"use_h3":true}
	]`, character.ID, cigar.ID)
	scene := models.Scene{
		ProjectID: project.ID, EpisodeN: 1, Order: 1, Characters: "金虎",
		ImageFile: "storyboard.png", ReferenceImagesJSON: saved,
	}
	if err := db.Create(&scene).Error; err != nil {
		t.Fatal(err)
	}

	ps := &ProjectService{db: db}
	imageRefs, _, _ := ps.selectedSceneReferenceFiles(&scene, "krea2")
	videoRefs, _ := ps.sceneVideoReferenceFiles(&scene, fmt.Sprint(project.ID))
	if len(imageRefs) != 2 || imageRefs[0].Name != character.Sheet || imageRefs[1].Name != cigar.Image {
		t.Fatalf("分镜图应收到两张参考图: %v", imageRefs)
	}
	if len(videoRefs) != 2 || videoRefs[0].Name != cigar.Image || videoRefs[1].Name != scene.ImageFile {
		t.Fatalf("未勾选视频用途的角色图不应进入视频参考: %v", videoRefs)
	}
}
