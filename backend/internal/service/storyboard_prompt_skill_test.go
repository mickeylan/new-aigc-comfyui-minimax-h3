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
		{h3StoryboardImageSkillCode, 2, []string{
			"真正可见的人物", "支撑脚与重心", "道具由谁持有", "正向空间拓扑", "800至1800",
			// v2 turns the prohibition list into a working method with graded examples.
			"【工作方法】", "写完自检", "【正例与反例】",
		}},
		{QwenImage21T2IProgram, 4, []string{
			"exact visible subject count", "support foot and weight", "prop ownership",
			"Method: settle these six decisions", "Graded examples", "Wherever they would have to guess",
		}},
		{QwenImage21EditProgram, 4, []string{
			"references do not automatically become visible subjects", "state each reference duty exactly once", "positive final topology",
			"Method: settle these decisions", "Graded examples",
		}},
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

func TestCompactStaticVisualPromptKeepsSentencesAndClosingComposition(t *testing.T) {
	english := strings.TrimSpace(strings.Repeat("A weathered courtyard wall rises along the left edge while a lone woman holds a bronze lantern at the centre. ", 26)) +
		" Overall composition centres the woman against the wall with warm rim light from the right."
	gotEN := compactStaticVisualPrompt(english, 1400)
	if n := len([]rune(gotEN)); n > 1400 {
		t.Fatalf("english prompt over budget: %d", n)
	}
	if !strings.HasSuffix(gotEN, "Overall composition centres the woman against the wall with warm rim light from the right.") {
		t.Fatalf("english closing composition dropped: %q", gotEN)
	}
	if !strings.HasSuffix(strings.TrimSpace(gotEN), ".") {
		t.Fatalf("english prompt cut mid-sentence: %q", gotEN)
	}

	var zh strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&zh, "可见证据%d：人物与道具在前中后景形成清楚关系，支撑脚与重心均可辨。", i)
	}
	zh.WriteString("整体构图为固定中近景，人物居中偏右，暖色侧光在地面留下同向阴影。")
	gotZH := compactStaticVisualPrompt(zh.String(), 1400)
	if n := len([]rune(gotZH)); n > 1400 {
		t.Fatalf("chinese prompt over budget: %d", n)
	}
	if !strings.HasSuffix(gotZH, "整体构图为固定中近景，人物居中偏右，暖色侧光在地面留下同向阴影。") {
		t.Fatalf("chinese closing composition dropped: %q", gotZH)
	}
	if strings.Count(gotZH, "。") < 2 {
		t.Fatalf("chinese sentence punctuation was flattened: %q", gotZH)
	}

	repeated := strings.Repeat("人物位于前景偏右。", 200) + "整体构图为固定全景。"
	gotRepeat := compactStaticVisualPrompt(repeated, 1400)
	if gotRepeat != "人物位于前景偏右。整体构图为固定全景。" {
		t.Fatalf("duplicate sentences were not collapsed: %q", gotRepeat)
	}

	kept := "人物位于前景偏右，手持铜灯。整体构图为固定中景。"
	if got := compactStaticVisualPrompt(kept, 1400); got != kept {
		t.Fatalf("in-budget prompt was rewritten: %q", got)
	}
}

func TestQwenSceneBindingIsIdempotentAndStripsUntaggedDutyRepetition(t *testing.T) {
	refs := []string{
		"- <Picture 1>：角色「柳乐儿」四视图",
		"- <Picture 2>：角色「齐姓道士」四视图",
		"- <Picture 3>：角色「燕承」四视图；别名「虬髯大汉」",
		"- <Picture 4>：场景「灰白巨石所在处」参考图",
	}
	// Shape taken from a real generated prompt: the model restates every reference
	// duty in Chinese asset labels with no <imageN> tag, and packs one duty clause
	// into a sentence that also carries real composition.
	body := "构建一幅全新的横向静态画面：角色「柳乐儿」四视图仅用于确定远处柳乐儿的人物身份、面部、年龄感与身体比例，角色「齐姓道士」四视图仅用于确定齐姓道士的人物身份、面部、年龄感与身体比例。" +
		"场景「灰白巨石所在处」参考图仅用于确定苍黄荒地的环境结构、空间布局、材质与固定陈设。" +
		"画面仅呈现三名人物：燕承位于左侧前景，柳乐儿位于远处背景偏左。" +
		"燕承左臂弯曲并单手握住旧拨浪鼓，角色「燕承」四视图仅用于确定燕承的身份与外形，重量稳落左脚。" +
		"最终画面构成清晰的三层静态叙事。"

	first := ensureQwenSceneReferenceBindings(body, refs)

	// Defect A: the stored prompt is bound again at execution time.
	second := ensureQwenSceneReferenceBindings(first, refs)
	third := ensureQwenSceneReferenceBindings(second, refs)
	if n := strings.Count(second, "静止剧情首帧。参考绑定："); n != 1 {
		t.Fatalf("binding header stacked %d times on reprocess: %s", n, second)
	}
	if second != first || third != first {
		t.Fatalf("binding is not idempotent:\nfirst=%s\nsecond=%s", first, second)
	}

	// Defect B: untagged duty bookkeeping must go, and the authoritative header
	// must still state each duty exactly once.
	if strings.Contains(first, "仅用于确定") {
		t.Fatalf("untagged duty repetition survived: %s", first)
	}
	if n := strings.Count(first, "人物身份、面部、年龄感与身体比例"); n != 0 {
		t.Fatalf("identity duty prose repeated %d times in body: %s", n, first)
	}
	for i := 1; i <= 4; i++ {
		tag := fmt.Sprintf("<image%d>", i)
		if n := strings.Count(first, tag); n != 1 {
			t.Fatalf("%s bound %d times, want exactly 1: %s", tag, n, first)
		}
	}

	// Clause-level stripping must not take real composition with it.
	for _, want := range []string{
		"画面仅呈现三名人物",
		"燕承位于左侧前景",
		"燕承左臂弯曲并单手握住旧拨浪鼓",
		"重量稳落左脚",
		"最终画面构成清晰的三层静态叙事。",
	} {
		if !strings.Contains(first, want) {
			t.Fatalf("visual evidence %q was dropped with the duty text: %s", want, first)
		}
	}
	if !strings.HasSuffix(first, "最终画面构成清晰的三层静态叙事。") {
		t.Fatalf("closing composition is no longer last: %s", first)
	}
	if err := validateQwenScenePrompt(first, len(refs)); err != nil {
		t.Fatalf("bound prompt fails the gate: %v", err)
	}

	// A "仅" that is not reference bookkeeping must survive.
	kept := ensureQwenSceneReferenceBindings("柳石仅用右手抵住网面，左手垂在身侧。", nil)
	if !strings.Contains(kept, "柳石仅用右手抵住网面") {
		t.Fatalf("non-duty wording was stripped as bookkeeping: %s", kept)
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

func TestQwenSceneKeyframeContractFixesOneLanguageAndOneLengthBudget(t *testing.T) {
	ps := newTestProjectService(t)
	if err := ps.db.AutoMigrate(&models.Skill{}, &models.ProjectSkillConfig{}, &models.SkillAuditLog{}); err != nil {
		t.Fatal(err)
	}
	ps.skills = NewSkillService(ps.db)
	if err := ps.skills.InitSystemSkills(); err != nil {
		t.Fatal(err)
	}
	body := "女子停在院墙左侧，右手提着铜灯，左手垂在身侧，视线朝向画面右前方的门洞。暖色侧光来自右前方，在夯土地面留下同向阴影。整体构图为固定中近景，人物居中偏左。"
	provider := &captureTextProvider{response: `{"rewritten_prompt":"` + body + `","wh_ratio":"16:9"}`}
	ps.textProvider = provider
	project := models.Project{Title: "静帧契约", Genre: "古典修仙", Style: "写实仙侠", AspectRatio: "16:9"}
	if err := ps.db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1, Title: "院墙前", Content: "女子提灯停在院墙前", ImageEngine: ImageEngineQwen21}
	if err := ps.db.Create(&scene).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := ps.RedesignSceneImagePrompt(&scene); err != nil {
		t.Fatal(err)
	}

	// One language and one length budget must reach the model, and the budget has
	// to match the downstream compaction cap of 1400 runes.
	for _, want := range []string{
		"these rules win",
		"one Chinese paragraph of 800 to 1400 Chinese characters",
		"Output one continuous Chinese paragraph inside rewritten_prompt",
	} {
		if !strings.Contains(provider.system, want) {
			t.Fatalf("keyframe contract missing %q", want)
		}
	}
	for _, conflicting := range []string{"300-450 English words", "in the request language"} {
		if strings.Contains(provider.system, conflicting) {
			t.Fatalf("keyframe contract still carries conflicting instruction %q", conflicting)
		}
	}
	// The base contract is concatenated with a real newline, not a literal "\n".
	if strings.Contains(provider.system, `\nThis is a Qwen-Image-2.1 single static`) {
		t.Fatal("keyframe contract is glued to the base contract by a literal backslash-n")
	}
	if !strings.Contains(provider.system, "\nThis is a Qwen-Image-2.1 single static") {
		t.Fatal("keyframe contract was not appended after a newline")
	}
}

func TestQwenSceneRejectedPromptIsRepairedInsteadOfDiscarded(t *testing.T) {
	ps := newTestProjectService(t)
	if err := ps.db.AutoMigrate(&models.Skill{}, &models.ProjectSkillConfig{}, &models.SkillAuditLog{}); err != nil {
		t.Fatal(err)
	}
	ps.skills = NewSkillService(ps.db)
	if err := ps.skills.InitSystemSkills(); err != nil {
		t.Fatal(err)
	}
	// First answer carries forbidden moving-camera wording, second is clean.
	bad := "女子停在院墙左侧，右手提着铜灯，镜头跟随她的脚步缓慢推镜，暖色侧光在地面留下同向阴影。"
	good := "女子停在院墙左侧，右手提着铜灯，左手垂在身侧，视线朝向右前方门洞。暖色侧光来自右前方，在夯土地面留下同向阴影。整体构图为固定中近景。"
	provider := &sequenceTextProvider{responses: []string{
		`{"rewritten_prompt":"` + bad + `","wh_ratio":"16:9"}`,
		`{"rewritten_prompt":"` + good + `","wh_ratio":"16:9"}`,
	}}
	ps.textProvider = provider
	project := models.Project{Title: "静帧修复", Genre: "古典修仙", Style: "写实仙侠", AspectRatio: "16:9"}
	if err := ps.db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1, Title: "院墙前", Content: "女子提灯停在院墙前", ImageEngine: ImageEngineQwen21}
	if err := ps.db.Create(&scene).Error; err != nil {
		t.Fatal(err)
	}

	got, err := ps.RedesignSceneImagePrompt(&scene)
	if err != nil {
		t.Fatalf("rejected first answer was not repaired: %v", err)
	}
	if !strings.Contains(got, "整体构图为固定中近景") {
		t.Fatalf("repaired prompt was not returned: %s", got)
	}
	if len(provider.users) != 2 {
		t.Fatalf("repair round not attempted: %d model calls", len(provider.users))
	}
	// The repair request must name the violation and keep the original brief.
	if !strings.Contains(provider.users[1], "跟拍") && !strings.Contains(provider.users[1], "推镜") {
		t.Fatalf("repair request did not report the violation: %s", provider.users[1])
	}
	if !strings.Contains(provider.users[1], "只修正该问题") {
		t.Fatalf("repair request lost its repair instruction: %s", provider.users[1])
	}

	// Two bad answers must still fail rather than silently ship a broken prompt.
	provider2 := &sequenceTextProvider{responses: []string{
		`{"rewritten_prompt":"` + bad + `","wh_ratio":"16:9"}`,
		`{"rewritten_prompt":"` + bad + `","wh_ratio":"16:9"}`,
	}}
	ps.textProvider = provider2
	if _, err := ps.RedesignSceneImagePrompt(&scene); err == nil {
		t.Fatal("two rejected answers were accepted")
	}
	if len(provider2.users) != 2 {
		t.Fatalf("repair is not capped at one round: %d model calls", len(provider2.users))
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
