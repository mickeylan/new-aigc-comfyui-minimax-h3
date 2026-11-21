package service

import (
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

const yanAuthoredScene = `[Shot 1] 中景，燕承、马脸男子与齐姓道士同时放出神识，三道淡淡的灵光从头到脚掠过柳石的身体，没有察觉到柳石身上有任何的灵力波动。
[Shot 2] 近景，镜头切换到燕承正面特定，燕承内心独白：“没有灵力？”
[Shot 3] 中景，镜头切换到柳石全身，柳石面对镜头，仍垂着头，木然立在柳乐儿右侧；柳乐儿把旧拨浪鼓横抱在胸前，紧贴着柳石站定，警惕地看着外圈三人。齐姓道士将白马尾拂尘搭在臂上，目光在柳石与柳乐儿之间来回逡巡。马脸男子悄然收回右手，退后半步，藏在袖中的左手暗握一枚暗器，目光死死钉住柳乐儿。
[Shot 4] 中景，燕承向两名同伴各使了一个眼色，随即正对柳石，背对镜头，以试探的语气说道：“不知阁下如何称呼？在下血刀会燕承，正与两位道友联手捉拿这妖狐，阁下若是与此事无关，还请自行离去。”`

// authored 原文仍是唯一动作权威；已保存站位与上一场结尾状态只能作为从属空间上下文出现，
// 旧提示词、旧Shot与武戏模板不得借这条路径回流。
func TestAuthoredH3TranslationKeepsContinuityContextSubordinate(t *testing.T) {
	ps := newTestProjectService(t)
	if err := ps.db.AutoMigrate(&models.Shot{}, &models.Character{}, &models.Asset{}, &models.SharedAssetReference{}, &models.SceneEndState{}); err != nil {
		t.Fatal(err)
	}
	p := models.Project{Title: "authored source"}
	if err := ps.db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	previous := models.Scene{ProjectID: p.ID, EpisodeN: 1, Generation: 1, Order: 1, Title: "前场", Content: "上一场，燕承退到门边。\n本场结尾状态\n燕承：姿态 半跪；伤势 左肩刀伤"}
	if err := ps.db.Create(&previous).Error; err != nil {
		t.Fatal(err)
	}
	sc := models.Scene{ProjectID: p.ID, EpisodeN: 1, Generation: 1, Order: 2, Characters: "燕承,柳石,柳乐儿,马脸男子,齐姓道士", Duration: 15, Content: yanAuthoredScene, VideoPrompt: "OLD_PROMPT_REVERSED_POSITION", BlockingJSON: `{"summary":"OLD_BLOCKING_WRONG_FACING"}`}
	if err := ps.db.Create(&sc).Error; err != nil {
		t.Fatal(err)
	}
	if err := ps.db.Create(&models.Shot{SceneID: sc.ID, Order: 1, Duration: 15, PromptAction: "OLD_SHOT_REWRITES_EVENTS", PromptSubject: "OLD_SUBJECT"}).Error; err != nil {
		t.Fatal(err)
	}
	// This mock proves source isolation and compilation, not model translation quality.
	translated := "[Shot 1] Three faint scanning lights pass over 柳石 from head to toe; the observers detect no spiritual-energy fluctuations.\n[Shot 2] A frontal close-up of 燕承, lips closed during his inner voice.\n[Shot 3] 柳石 faces the camera with his head lowered, standing to 柳乐儿's right.\n[Shot 4] 燕承 signals each companion, then faces 柳石 with his back to the camera."
	provider := &captureTextProvider{response: translated}
	ps.textProvider = provider
	got, err := ps.GenerateSceneVideoAction(&sc)
	if err != nil {
		t.Fatal(err)
	}
	if got != translated {
		t.Fatalf("authored translation changed by free design: %s", got)
	}
	if !strings.Contains(provider.user, yanAuthoredScene) {
		t.Fatal("original authored shots not passed intact")
	}
	if !strings.Contains(provider.user, "SPATIAL CONTINUITY CONTEXT") || !strings.Contains(provider.user, "OLD_BLOCKING_WRONG_FACING") {
		t.Fatalf("saved blocking missing from the translation context: %s", provider.user)
	}
	if !strings.Contains(provider.user, "上一场结束状态") || !strings.Contains(provider.user, "半跪") {
		t.Fatalf("previous end state missing from the translation context: %s", provider.user)
	}
	if !strings.Contains(provider.user, "only fill unspecified spatial details") || !strings.Contains(provider.user, "authored shots, dialogue and movement remain authoritative") {
		t.Fatalf("continuity context is not marked subordinate: %s", provider.user)
	}
	for _, bad := range []string{"OLD_PROMPT", "OLD_SHOT", "OLD_SUBJECT", "武戏硬任务", "INCOMPLETE DRAFT"} {
		if strings.Contains(provider.user, bad) {
			t.Fatalf("derived context leaked: %s", provider.user)
		}
	}
	for _, rule := range []string{"NOT a director-design", "scope of every negation", "right side", "Back to camera", "inner monologue"} {
		if !strings.Contains(provider.system, rule) {
			t.Fatalf("missing source conversion contract %q", rule)
		}
	}
	var saved models.Scene
	if err := ps.db.First(&saved, sc.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.Content != yanAuthoredScene || saved.VideoPrompt != "OLD_PROMPT_REVERSED_POSITION" {
		t.Fatal("preview overwrote authored story or saved prompt")
	}

	// 正文改过之后站位已失效，翻译分支必须同样丢弃它，而不是只在新设计分支里遵守这个规则。
	stale := sc
	stale.ID = 0
	stale.Order = 3
	stale.BlockingContentHash = models.DialogueContentHash("另一版正文")
	if err := ps.db.Create(&stale).Error; err != nil {
		t.Fatal(err)
	}
	staleProvider := &captureTextProvider{response: translated}
	ps.textProvider = staleProvider
	if _, err := ps.GenerateSceneVideoAction(&stale); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(staleProvider.user, "OLD_BLOCKING_WRONG_FACING") {
		t.Fatalf("stale blocking leaked into authored translation: %s", staleProvider.user)
	}
}
