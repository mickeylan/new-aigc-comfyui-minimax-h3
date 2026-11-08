package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

func newEpisodeAdaptationTestService(t *testing.T, provider TextProvider) (*ProjectService, *AdaptationService, *models.Project, *models.EpisodeAdaptation) {
	t.Helper()
	db := newTestDB(t)
	if err := db.AutoMigrate(&models.Chapter{}, &models.EpisodeAdaptation{}, &models.StoryBible{}, &models.Scene{}, &models.Dialogue{}, &models.Character{}, &models.CharacterAliasCandidate{}, &models.Asset{}, &models.GenerationCandidate{}, &models.Task{}, &models.MergeTask{}, &models.Episode{}, &models.PromptPolicyOverride{}); err != nil {
		t.Fatal(err)
	}
	projects := NewProjectService(nil, db, nil, nil, nil, nil, nil, nil, nil)
	projects.stopped = make(chan struct{})
	projects.textProvider = provider
	skills := NewSkillService(db)
	if err := skills.InitSystemSkills(); err != nil {
		t.Fatal(err)
	}
	adapt := NewAdaptationService(db, provider, skills, projects)
	// Keep the structural scene floor at 2 so the fidelity tests stay focused.
	project := models.Project{Title: "仙途", Synopsis: "狐女逃亡", Episodes: 10, Plan: `{"episodes":[{"n":1,"title":"狐女","target_duration":180,"target_scenes":3},{"n":2,"title":"石头哥哥","target_duration":180,"target_scenes":3}]}`}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.StoryBible{ProjectID: project.ID, Premise: "修仙世界", WorldRules: "法力", MainPlot: "逃亡", Status: "approved", Version: 1}).Error; err != nil {
		t.Fatal(err)
	}
	chapter := models.Chapter{ProjectID: project.ID, Order: 2, Title: "石头哥哥", Content: "“石头哥哥！”女童从地上爬起，一把抱住高大青年的大腿。三名修士放出神识扫过，却察觉不到丝毫法力。燕承报出身份，随后祭出黑色大网罩住两人。青年徒手撕裂黑网，又硬抗飞针、金蜂锥与雷电，最终抓住燕承手腕将其震碎。"}
	if err := db.Create(&chapter).Error; err != nil {
		t.Fatal(err)
	}
	ep := models.EpisodeAdaptation{
		ProjectID: project.ID, EpisodeN: 2, Title: "石头哥哥", ChapterStart: 2, ChapterEnd: 2,
		SourceChapterIDs: fmt.Sprintf("[%d]", chapter.ID), TargetDuration: 180, TargetScenes: 3,
		AdaptationGoal: "从巨石现身之后继续，让乐儿认出石头哥哥并触发三人围攻。",
		MustKeepEvents: `["女童抱住高大青年并呼喊石头哥哥","三人用神识探查青年却察觉不到法力","青年徒手撕裂黑色大网"]`,
		OmittedEvents:  `["不得提前杀死三人"]`,
		OpeningState:   `"巨石已经爆碎，青年站在碎石中央"`,
		EndingState:    `"青年撕裂黑网，三人惊骇后退"`,
		Hook:           "黑网裂开的一刻，三人意识到眼前之人绝非凡人。",
		Status:         "approved",
	}
	if err := db.Create(&ep).Error; err != nil {
		t.Fatal(err)
	}
	// Episode 2 generation requires the previous episode to be scripted.
	if err := db.Create(&models.EpisodeAdaptation{ProjectID: project.ID, EpisodeN: 1, Title: "第一章", Status: "scripted", Script: "第一集已完成剧本", SourceChapterIDs: "[]"}).Error; err != nil {
		t.Fatal(err)
	}
	return projects, adapt, &project, &ep
}

func faithfulEpisodeScript() string {
	scene := func(title, content string) map[string]any {
		return map[string]any{
			"title": title, "content": content, "image_prompt": "荒野，固定机位", "duration": 6.0,
			"characters": []string{}, "visible_characters": []string{}, "voice_characters": []string{},
			"mentioned_characters": []string{}, "location": "碎石荒野", "props": []string{},
			"visual_type": "normal", "mega_type": "", "dialogues": []any{},
		}
	}
	scenes := []any{
		scene("石头哥哥", "女童从碎石间爬起，一把抱住高大青年，仰头呼喊石头哥哥；青年没有回答，双目空洞。"),
		scene("神识探查", "三名修士停在十步外，用神识探查青年，却察觉不到丝毫法力，燕承报出身份后示意动手。"),
		scene("撕网", "黑色大网罩下，女童被倒刺划伤，青年抓住网绳，徒手撕裂黑色大网，三人惊骇后退。"),
	}
	payload := map[string]any{
		"script": "乐儿认出石头哥哥，三人神识探查无果后祭出黑网，青年徒手撕网。", "visual_bible": "写实仙侠",
		"scenes": scenes,
	}
	raw, _ := json.Marshal(payload)
	return string(raw)
}

func inventedEpisodeScript() string {
	scene := func(title, content string) map[string]any {
		return map[string]any{
			"title": title, "content": content, "image_prompt": "火海，固定机位", "duration": 6.0,
			"characters": []string{}, "visible_characters": []string{}, "voice_characters": []string{},
			"mentioned_characters": []string{}, "location": "火海", "props": []string{},
			"visual_type": "normal", "mega_type": "", "dialogues": []any{},
		}
	}
	scenes := []any{
		scene("火墙", "青年扯下燃烧的草茎引出一面火墙，把追兵隔在另一侧。"),
		scene("藤蔓", "女童摇动拨浪鼓，枯根破土化为藤蔓缠住追兵的腿脚。"),
		scene("鼓声", "两枚鼓珠撞响，藤蔓收紧，三名追兵被分隔在三个方向。"),
	}
	payload := map[string]any{
		"script": "众人在火墙两侧对峙，藤蔓困住追兵。", "visual_bible": "写实仙侠",
		"scenes": scenes,
	}
	raw, _ := json.Marshal(payload)
	return string(raw)
}

func TestEpisodeContextCarriesApprovedScopeToTheModel(t *testing.T) {
	_, adapt, project, ep := newEpisodeAdaptationTestService(t, &captureTextProvider{})
	ctx, err := adapt.BuildContext(project.ID, ep.EpisodeN, 0)
	if err != nil {
		t.Fatal(err)
	}
	if ctx.EpisodeTitle != "石头哥哥" || !strings.Contains(ctx.AdaptationGoal, "石头哥哥") {
		t.Fatalf("approved title/goal missing: %+v", ctx)
	}
	if len(ctx.MustKeepEvents) != 3 || !strings.Contains(strings.Join(ctx.MustKeepEvents, "|"), "神识") {
		t.Fatalf("must-keep events missing: %+v", ctx.MustKeepEvents)
	}
	if len(ctx.OmittedEvents) != 1 || !strings.Contains(ctx.OmittedEvents[0], "杀死三人") {
		t.Fatalf("omitted events missing: %+v", ctx.OmittedEvents)
	}
	if !strings.Contains(ctx.EndingState, "撕裂黑网") {
		t.Fatalf("approved ending missing: %q", ctx.EndingState)
	}
	if len(ctx.SourceChapters) != 1 || !strings.Contains(ctx.SourceChapters[0].Content, "石头哥哥") {
		t.Fatalf("source chapter text missing: %+v", ctx.SourceChapters)
	}
}

// These are the approved events the user's episode 2 was blocked on. A faithful
// adaptation paraphrases them; the old whole-string metric rejected paraphrases.
var approvedChapterTwoEvents = []string{
	"柳乐儿从伏坐状态翻身爬起，一把抱住高大青年的大腿，惊喜地呼喊“石头哥哥”",
	"柳乐儿带哭腔说自己总算找到哥哥，请求石头哥哥帮她打走这些坏人",
	"高大青年听到呼唤后头颅缓缓垂下，双目空洞地看向柳乐儿，柳乐儿因此短暂害怕后退",
	"柳乐儿松手后紧握旧拨浪鼓，三人则用神识扫查青年，发现其身上丝毫法力迹象没有",
	"马脸男子暗握暗器后退半步，齐姓道士目光在青年与女童之间来回逡巡",
	"燕承自报“在下血刀会燕承”，表示正与两位道友联手捉拿妖狐，要求与此事无关者自行离去",
	"燕承提高声音重复一遍，青年仍无任何反应，柳乐儿抓着他裤脚的手心冒汗",
	"马脸男子与齐姓道士传音进言，建议把可疑青年视为妖族同党、先下手为强",
	"燕承厉喝“动手”，祭出黑色大网将柳乐儿与高大青年一同罩住，网结上浮现锋利银色弯钩",
	"柳乐儿把血雾喷在拨浪鼓上，化作一截枯木并在十余丈外重现，却被倒刺划出十余处伤口，摔倒在地无法起身",
	"柳乐儿忍痛高喊“小心”，提醒石头哥哥躲避接下来的攻击",
	"银色弯钩挂在青年身上无法刺入体表分毫，青年缓缓低头，像是觉得挂在身上的东西碍事",
}

// paraphrasedEpisodeScript rewrites the same beats the way a competent screenwriter
// would, so it must pass the fidelity gate.
func paraphrasedEpisodeScript() string {
	scene := func(title, content string, dialogues ...map[string]any) map[string]any {
		if dialogues == nil {
			dialogues = []map[string]any{}
		}
		return map[string]any{
			"title": title, "content": content, "image_prompt": "碎石荒野，固定机位", "duration": 6.0,
			"characters": []string{}, "visible_characters": []string{}, "voice_characters": []string{},
			"mentioned_characters": []string{}, "location": "碎石荒野", "props": []string{},
			"visual_type": "normal", "mega_type": "", "dialogues": dialogues,
		}
	}
	line := func(character, text string) map[string]any {
		return map[string]any{"character": character, "text": text, "speech_type": "dialogue"}
	}
	scenes := []any{
		scene("石头哥哥", "女童翻身爬起，一把抱住高大青年的腿，惊喜喊他石头哥哥。青年毫无表情，仍直勾勾发呆。",
			line("柳乐儿", "石头哥哥，乐儿总算找到你了！这些坏人要抓乐儿，石头哥哥帮我打他们！")),
		scene("空洞的目光", "青年听到呼唤，头颅缓缓垂下几分。女童抬头迎上目光，见那双眼空洞无神，心里咯噔一下。"),
		scene("神识扫查", "女童松手攥紧拨浪鼓。三人放出神识扫过青年，察觉不到丝毫法力迹象，顿时生出狐疑。"),
		scene("暗器与逡巡", "马脸男子收回右手后退半步，左手藏在袖中捏住暗器。齐姓道士把拂尘搭在胳膊上，目光在两人之间来回逡巡。"),
		scene("自报家门", "燕承拱手试探，自称血刀会燕承，正在追拿妖狐，请无关之人自行离去。",
			line("燕承", "在下血刀会燕承，正与两位道友联手捉拿这妖狐，阁下若是与此事无关，还请自行离去。")),
		scene("重复与冷汗", "燕承提高声音把话重复一遍，青年依旧没有反应。女童张了张嘴，抓着他裤脚的手心全是冷汗。"),
		scene("传音进言", "马脸男子与齐姓道士嘴唇微动，传音进言，认定此人可疑，建议视作妖族同党先下手为强。"),
		scene("动手", "燕承厉喝动手，朴刀泛白，另一手扬出黑光。黑色大网当空罩下，把女童与青年一同扣住，绳结上浮现锋利银色弯钩。"),
		scene("血雾木遁", "女童尖叫，喷出血雾在鼓面，手腕一转，身影溃散成一截枯木。十余丈外她重新现形，却被倒刺划出十余处伤口，摔倒在地。"),
		scene("小心", "倒地的女童见青芒飞针射向青年，忍痛高喊提醒他躲开。", line("柳乐儿", "小心！")),
		scene("弯钩无效", "银色弯钩挂在青年身上却刺不进分毫。他缓缓低头，像是觉得挂着的东西碍事。"),
	}
	payload := map[string]any{"script": "乐儿认出石头哥哥，三人试探后动手，黑网与飞针都伤不到青年。", "visual_bible": "写实仙侠", "scenes": scenes}
	raw, _ := json.Marshal(payload)
	return string(raw)
}

func TestMissingMustKeepEventsAcceptsFaithfulParaphrase(t *testing.T) {
	for _, item := range missingMustKeepEvents(paraphrasedEpisodeScript(), approvedChapterTwoEvents) {
		t.Logf("rejected event: %s clauses=%.2f", item.description(), item.Clauses)
	}
	if missing := missingMustKeepEvents(paraphrasedEpisodeScript(), approvedChapterTwoEvents); len(missing) != 0 {
		t.Fatalf("faithful paraphrase rejected: %d/%d events", len(missing), len(approvedChapterTwoEvents))
	}
	// The compact three-scene fixture covers only its own three events.
	compact := []string{"女童抱住高大青年并呼喊石头哥哥", "三人用神识探查青年却察觉不到法力", "青年徒手撕裂黑色大网"}
	if missing := missingMustKeepEvents(faithfulEpisodeScript(), compact); len(missing) != 0 {
		t.Fatalf("faithful draft rejected: %v", missing)
	}
}

func TestMissingMustKeepEventsRejectsInventedPlot(t *testing.T) {
	missing := missingMustKeepEvents(inventedEpisodeScript(), approvedChapterTwoEvents)
	if ratio := float64(len(missing)) / float64(len(approvedChapterTwoEvents)); ratio <= mustKeepBlockRatio {
		t.Fatalf("invented plot was not blocked: %d/%d missing", len(missing), len(approvedChapterTwoEvents))
	}
}

func TestAdaptationScriptGenerationReadsNovelAndMustKeep(t *testing.T) {
	provider := &captureTextProvider{response: faithfulEpisodeScript()}
	_, adapt, project, ep := newEpisodeAdaptationTestService(t, provider)
	got, err := adapt.GenerateScript(project.ID, ep.EpisodeN)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(provider.firstUser, "石头哥哥") || !strings.Contains(provider.firstSystem, "source_chapters") {
		t.Fatalf("novel source was not injected:\nsystem=%s\nuser=%s", provider.firstSystem, provider.firstUser)
	}
	for _, want := range []string{"must_keep_events", "神识探查", "approved_ending_state", "omitted_events"} {
		if !strings.Contains(provider.firstUser, want) {
			t.Fatalf("approved scope field %q was not injected: %s", want, provider.firstUser)
		}
	}
	if got.Status != "scripted" {
		t.Fatalf("status=%s", got.Status)
	}
}

func TestAdaptationScriptGenerationRefusesInventedPlotWithoutOverwriting(t *testing.T) {
	provider := &sequenceTextProvider{responses: []string{inventedEpisodeScript(), inventedEpisodeScript()}}
	projects, adapt, project, ep := newEpisodeAdaptationTestService(t, provider)
	existing := models.Scene{ProjectID: project.ID, EpisodeN: ep.EpisodeN, Generation: 6, Order: 1, Title: "旧版第2集", Content: "旧版已存在的内容", ImagePrompt: "旧图"}
	if err := projects.db.Create(&existing).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := adapt.GenerateScript(project.ID, ep.EpisodeN); err == nil || !strings.Contains(err.Error(), "未覆盖现有剧本") {
		t.Fatalf("unfaithful script was accepted: %v", err)
	}
	var scenes []models.Scene
	if err := projects.db.Where("project_id = ? AND episode_n = ?", project.ID, ep.EpisodeN).Find(&scenes).Error; err != nil {
		t.Fatal(err)
	}
	if len(scenes) != 1 || scenes[0].Title != "旧版第2集" {
		t.Fatalf("existing episode was overwritten by a rejected script: %+v", scenes)
	}
}

// The project-detail "重新生成第N集剧本" button posts to /projects/:id/script.
// For a novel project with an approved EpisodeAdaptation it must use the adaptation
// path (chapter text + must-keep + ledger), never the legacy plan generator.
func TestGenerateScriptHandlerRoutesApprovedAdaptationThroughNovelSource(t *testing.T) {
	provider := &captureTextProvider{response: faithfulEpisodeScript()}
	projects, adapt, project, ep := newEpisodeAdaptationTestService(t, provider)
	svc := &Service{DB: projects.db, Projects: projects, Adaptations: adapt}
	w := generateScriptRequest(svc, project.ID, "?episode_n=2", `{"episode_n":2}`)
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var payload struct {
		Source string `json:"source"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Source != "episode_adaptation" {
		t.Fatalf("legacy generator handled an approved adaptation: source=%q body=%s", payload.Source, w.Body.String())
	}
	if !strings.Contains(provider.firstUser, "石头哥哥") || !strings.Contains(provider.firstUser, "must_keep_events") {
		t.Fatalf("handler did not read the approved novel scope: %s", provider.firstUser)
	}
	var stored models.EpisodeAdaptation
	if err := projects.db.Where("project_id = ? AND episode_n = ?", project.ID, ep.EpisodeN).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != "scripted" || !strings.Contains(stored.Script, "石头哥哥") {
		t.Fatalf("adaptation was not updated from the novel: %+v", stored)
	}
}

func TestPreviousEpisodeProductionFactsUsesAuthoritativeGenerationOnly(t *testing.T) {
	ps := newTestProjectService(t)
	if err := ps.db.AutoMigrate(&models.EpisodeAdaptation{}, &models.Dialogue{}); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "分集"}
	if err := ps.db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	// Old archived generation must never leak into the dedup authority.
	for i := 1; i <= 3; i++ {
		if err := ps.db.Create(&models.Scene{ProjectID: project.ID, EpisodeN: 1, Generation: 1, Order: i, Archived: true, Title: fmt.Sprintf("废弃旧稿%d", i), Content: "旧稿中被删掉的剧情"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	for i := 1; i <= 5; i++ {
		if err := ps.db.Create(&models.Scene{ProjectID: project.ID, EpisodeN: 1, Generation: 2, Order: i, Title: fmt.Sprintf("正式场景%d", i), Content: fmt.Sprintf("正式剧情第%d场", i)}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := ps.db.Create(&models.EpisodeAdaptation{ProjectID: project.ID, EpisodeN: 1, Status: "scripted", EndingState: `"青年已从巨石中现身"`}).Error; err != nil {
		t.Fatal(err)
	}
	facts, err := ps.previousEpisodeProductionFacts(project.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(facts, "废弃旧稿") || strings.Contains(facts, "旧稿中被删掉") {
		t.Fatalf("archived generation leaked into dedup authority: %s", facts)
	}
	for _, want := range []string{"正式场景1", "正式场景5", "正式剧情第5场", "青年已从巨石中现身"} {
		if !strings.Contains(facts, want) {
			t.Fatalf("authoritative fact %q missing: %s", want, facts)
		}
	}
}

func TestPreviousEpisodeProductionFactsKeepsTheEndingUnderBudget(t *testing.T) {
	ps := newTestProjectService(t)
	if err := ps.db.AutoMigrate(&models.EpisodeAdaptation{}, &models.Dialogue{}); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "长集"}
	if err := ps.db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("旧事", 450)
	for i := 1; i <= 60; i++ {
		title := fmt.Sprintf("场景%d", i)
		content := long
		if i == 60 {
			title = "结尾现身"
			content = "巨石爆碎后，青衣高大青年站在碎石中央，三人停步对峙，女童伏在地上。"
		}
		if err := ps.db.Create(&models.Scene{ProjectID: project.ID, EpisodeN: 1, Generation: 3, Order: i, Title: title, Content: content}).Error; err != nil {
			t.Fatal(err)
		}
	}
	facts, err := ps.previousEpisodeProductionFacts(project.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(facts, "结尾现身") || !strings.Contains(facts, "青衣高大青年站在碎石中央") {
		t.Fatalf("episode ending was truncated out of context: %s", facts)
	}
	if len(facts) > previousEpisodeContextLimit+4000 {
		t.Fatalf("facts exceeded budget: %d", len(facts))
	}
}
