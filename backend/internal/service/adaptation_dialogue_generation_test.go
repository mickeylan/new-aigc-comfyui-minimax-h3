package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

type adaptationMockTextProvider struct {
	responses []string
	errors    []error
	systems   []string
	users     []string
}

func (m *adaptationMockTextProvider) Name() string                      { return "adaptation-dialogue-test" }
func (m *adaptationMockTextProvider) HealthCheck(context.Context) error { return nil }
func (m *adaptationMockTextProvider) Chat(system, user string) (string, error) {
	m.systems = append(m.systems, system)
	m.users = append(m.users, user)
	i := len(m.users) - 1
	if i >= len(m.responses) {
		return "", fmt.Errorf("unexpected provider call %d", i+1)
	}
	var err error
	if i < len(m.errors) {
		err = m.errors[i]
	}
	return m.responses[i], err
}

func adaptationScriptFixture(t *testing.T, script string, ledger []SourceDialogue, includeDialogues bool) string {
	t.Helper()
	contents := []string{
		"晨雾掠过荒原，远山露出青色轮廓。", "马脸男子俯身辨认泥地足印。", "齐姓道士抬手感知林间风向。", "虬髯大汉拨开挡路的枯枝。",
		"一只惊鸟从树冠突然飞起。", "镜头越过溪水照见摇晃倒影。", "三人停在烧焦的古老界碑旁。", "夕光将众人的影子拉向坡底。",
		"远处狐影闪入遍布荆棘的山坳。", "马脸男子取出符箓仔细查看。", "齐姓道士把拂尘搭在左臂。", "虬髯大汉观察草叶折断的方向。",
		"风卷落叶穿过寂静的石缝。", "乌云逐渐遮住荒野上空的日光。", "坡下河流发出沉闷的轰鸣声。", "林中萤火围绕腐朽树桩明灭。",
		"一道兽爪痕留在灰白岩壁上。", "众人绕过长满苔藓的废弃木桥。", "月色落在草尖凝结的露珠上。", "山谷出口浮现村落微弱灯火。",
	}
	scenes := make([]scriptScene, len(contents))
	for i := range scenes {
		scenes[i] = scriptScene{
			Title: fmt.Sprintf("场景%d", i+1), Content: contents[i],
			ImagePrompt: "古代荒野草丛，三名修士，电影感画面", Duration: 9,
			VisibleCharacters: []string{"虬髯大汉", "马脸男子", "齐姓道士"},
			Location:          "荒野草丛", VisualType: "normal",
		}
		if includeDialogues && i < len(ledger) {
			d := ledger[i]
			scenes[i].Content = d.Speaker + "说道：" + d.Text
			scenes[i].Dialogues = []scriptDialogue{{Character: d.Speaker, SpeechType: "dialogue", Text: d.Text}}
		}
	}
	b, err := json.Marshal(scriptResult{Script: script, VisualBible: "三名修士造型固定", Scenes: scenes})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func newAdaptationDialogueTestService(t *testing.T, provider *adaptationMockTextProvider) (*AdaptationService, models.Project, models.Chapter) {
	t.Helper()
	projects := newTestProjectService(t)
	db := projects.db
	if err := db.AutoMigrate(
		&models.Dialogue{}, &models.Chapter{}, &models.StoryBible{}, &models.EpisodeAdaptation{},
		&models.CharacterAliasCandidate{}, &models.Skill{}, &models.ProjectSkillConfig{}, &models.SkillAuditLog{},
	); err != nil {
		t.Fatal(err)
	}
	skills := NewSkillService(db)
	if err := skills.InitSystemSkills(); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "妖狐追捕", SourceType: models.ProjectSourceNovel, Plan: `{"episodes":[{"n":1,"title":"草丛围捕","brief":"三人商议围捕妖狐","target_duration":180,"target_scenes":20}]}`}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"虬髯大汉", "马脸男子", "齐姓道士"} {
		if err := db.Create(&models.Character{ProjectID: project.ID, Name: name, Role: "围捕修士"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	chapter := models.Chapter{ProjectID: project.ID, Order: 1, Title: "围捕", Content: realPostposedDialogueFixture, ContentHash: novelHash(realPostposedDialogueFixture), AnalysisJSON: `{"summary":"三名修士围捕妖狐"}`, AnalysisStatus: "ready"}
	if err := db.Create(&chapter).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.StoryBible{ProjectID: project.ID, Premise: "围捕妖狐", MainPlot: "三人合作", WorldRules: "修真世界", Status: "approved"}).Error; err != nil {
		t.Fatal(err)
	}
	ep := models.EpisodeAdaptation{ProjectID: project.ID, EpisodeN: 1, Title: "草丛围捕", ChapterStart: 1, ChapterEnd: 1, SourceChapterIDs: fmt.Sprintf("[%d]", chapter.ID), TargetDuration: 180, TargetScenes: 20, Status: "approved", ContinuityStatus: "pending"}
	if err := db.Create(&ep).Error; err != nil {
		t.Fatal(err)
	}
	projects.textProvider = provider
	return NewAdaptationService(db, provider, skills, projects), project, chapter
}

func TestGenerateScriptRevisesMissingSourceDialoguesThroughSkillV2(t *testing.T) {
	provider := &adaptationMockTextProvider{}
	svc, project, chapter := newAdaptationDialogueTestService(t, provider)
	ledger := extractSourceDialogues(chapter, "虬髯大汉", "马脸男子", "齐姓道士")
	first := adaptationScriptFixture(t, "first-paid-script", ledger, false)
	second := adaptationScriptFixture(t, "first-paid-script revised", ledger, true)
	provider.responses = []string{first, second}

	ep, err := svc.GenerateScript(project.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(provider.users) != 2 {
		t.Fatalf("provider calls=%d, want initial generation plus one revision", len(provider.users))
	}
	revisionRequest := provider.systems[1] + provider.users[1]
	for _, want := range []string{"first-paid-script", "missing_dialogues", ledger[0].Text, "虬髯大汉没有说话，朝眼前齐人高的杂草丛指了指"} {
		if !strings.Contains(revisionRequest, want) {
			t.Fatalf("revision request does not contain %q", want)
		}
	}
	if strings.Contains(revisionRequest, `"current_structured_script":{}`) {
		t.Fatal("revision used an empty script template instead of the paid output")
	}
	var coverage DialogueCoverage
	if err := json.Unmarshal([]byte(ep.DialogueCoverageJSON), &coverage); err != nil {
		t.Fatal(err)
	}
	if coverage.Total != 13 || coverage.Covered != 13 || len(coverage.Missing) != 0 || !coverage.OrderPassed {
		t.Fatalf("coverage=%+v", coverage)
	}
	for i, d := range ledger {
		if d.ChapterID != chapter.ID || d.ChapterNo != chapter.Order {
			t.Fatalf("ledger source at %d=%+v", i, d)
		}
	}
	var got []models.Dialogue
	if err := svc.db.Joins("JOIN scenes ON scenes.id = dialogues.scene_id").Where("dialogues.project_id = ?", project.ID).Order("scenes.`order`, dialogues.`order`").Find(&got).Error; err != nil {
		t.Fatal(err)
	}
	if len(got) != 13 {
		t.Fatalf("stored dialogues=%d, want 13: %+v", len(got), got)
	}
	for i := range ledger {
		if got[i].Character != ledger[i].Speaker || got[i].Text != ledger[i].Text {
			t.Fatalf("stored dialogue %d=%q/%q want %q/%q", i, got[i].Character, got[i].Text, ledger[i].Speaker, ledger[i].Text)
		}
	}
	if strings.Contains(strings.Join(provider.responses, "\n"), "念咒") {
		t.Fatal("invented chant found in generated scripts")
	}
}

func TestGenerateScriptBadDialogueRevisionKeepsFirstPaidScriptAndDiagnostics(t *testing.T) {
	provider := &adaptationMockTextProvider{}
	svc, project, chapter := newAdaptationDialogueTestService(t, provider)
	ledger := extractSourceDialogues(chapter, "虬髯大汉", "马脸男子", "齐姓道士")
	first := adaptationScriptFixture(t, "first-paid-script", ledger, false)
	provider.responses = []string{first, ""}
	provider.errors = []error{nil, errors.New("revision unavailable")}

	ep, err := svc.GenerateScript(project.ID, 1)
	if err != nil {
		t.Fatalf("bad optional revision should preserve usable first output: %v", err)
	}
	if ep.Script != "first-paid-script" {
		t.Fatalf("script=%q, want first paid output", ep.Script)
	}
	var coverage DialogueCoverage
	if err := json.Unmarshal([]byte(ep.DialogueCoverageJSON), &coverage); err != nil {
		t.Fatal(err)
	}
	if coverage.Total != 13 || coverage.Covered != 0 || len(coverage.Missing) != 13 {
		t.Fatalf("missing diagnostics=%+v", coverage)
	}
	var scenes []models.Scene
	if err := svc.db.Where("project_id = ? AND archived = ?", project.ID, false).Order("`order`").Find(&scenes).Error; err != nil {
		t.Fatal(err)
	}
	if len(scenes) != 20 {
		t.Fatalf("scenes=%d, want exactly original 20", len(scenes))
	}
	for _, scene := range scenes {
		if strings.Contains(scene.Title+scene.Content, "待安排原文对白") {
			t.Fatalf("fake repair scene persisted: %+v", scene)
		}
	}
}

func TestGenerateScriptPreservesSpeakerAliasesThroughAllDialogueStages(t *testing.T) {
	provider := &adaptationMockTextProvider{}
	svc, project, chapter := newAdaptationDialogueTestService(t, provider)
	ledger := extractSourceDialogues(chapter, "虬髯大汉", "马脸男子", "齐姓道士")
	raw := adaptationScriptFixture(t, "alias-script", ledger, true)
	raw = strings.ReplaceAll(raw, `"character":`, `"speaker":`)
	raw = strings.ReplaceAll(raw, `"speech_type":"dialogue"`, `"speechType":"对白"`)
	provider.responses = []string{raw}
	ep, err := svc.GenerateScript(project.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	var got []models.Dialogue
	if err := svc.db.Joins("JOIN scenes ON scenes.id = dialogues.scene_id").Where("dialogues.project_id = ? AND scenes.archived = ?", project.ID, false).Order("scenes.`order`, dialogues.`order`").Find(&got).Error; err != nil {
		t.Fatal(err)
	}
	if len(got) != 13 {
		t.Fatalf("aliases lost before persistence: got %d want 13", len(got))
	}
	for i := range ledger {
		if got[i].Text != ledger[i].Text || got[i].Character != ledger[i].Speaker || got[i].SpeechType != "dialogue" {
			t.Fatalf("dialogue %d = %+v", i, got[i])
		}
	}
	var coverage DialogueCoverage
	if err := json.Unmarshal([]byte(ep.DialogueCoverageJSON), &coverage); err != nil {
		t.Fatal(err)
	}
	if coverage.Covered != len(got) || len(coverage.Missing) != 0 {
		t.Fatalf("report disagrees with persisted rows: %+v", coverage)
	}
	if len(provider.users) != 1 {
		t.Fatalf("normalized aliases unnecessarily caused revision: %d calls", len(provider.users))
	}
}

func TestAdaptationDialogueNormalizationKeepsUnboundAndUnknownForReview(t *testing.T) {
	raw := `{"scenes":[{"dialogues":[{"speaker":"甲","speechType":"对白","text":"一"},{"character_name":"乙","speech_type":"","text":"二"},{"character":"","speech_type":"unexpected","text":"三"},{"character":"","speech_type":"","text":"四"}]}]}`
	parsed, err := parseScriptJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	applyAdaptationDialogueAliases(raw, parsed)
	wantTypes := []string{"dialogue", "dialogue", "unexpected", "unknown"}
	wantCharacters := []string{"甲", "乙", "", ""}
	rows := make([]models.Dialogue, 0, 13)
	for round := 0; round < 13; round++ {
		d := parsed.Scenes[0].Dialogues[round%len(parsed.Scenes[0].Dialogues)]
		typ, character := normalizeAdaptationDialogue(d)
		if round < 4 && (typ != wantTypes[round] || character != wantCharacters[round]) {
			t.Fatalf("normalized %d=%q/%q want %q/%q", round, typ, character, wantTypes[round], wantCharacters[round])
		}
		rows = append(rows, models.Dialogue{Order: round + 1, Character: character, SpeechType: typ, Text: d.Text})
	}
	coverage := persistedDialogueCoverage(rows, []SourceDialogue{{Speaker: "甲", Text: "一"}, {Speaker: "乙", Text: "二"}, {Speaker: "丙", Text: "三"}})
	if len(rows) != 13 || coverage.Covered != 2 || len(coverage.Missing) != 1 || coverage.Unbound != 6 {
		t.Fatalf("rows=%d coverage=%+v", len(rows), coverage)
	}
}
