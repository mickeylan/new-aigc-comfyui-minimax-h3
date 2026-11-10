package service

import (
	"comfyui-console/internal/models"
	"strings"
	"testing"
)

const yanAuthoredScene = `[Shot 1] 中景，燕承、马脸男子与齐姓道士同时放出神识，三道淡淡的灵光从头到脚掠过柳石的身体，没有察觉到柳石身上有任何的灵力波动。
[Shot 2] 近景，镜头切换到燕承正面特定，燕承内心独白：“没有灵力？”
[Shot 3] 中景，镜头切换到柳石全身，柳石面对镜头，仍垂着头，木然立在柳乐儿右侧；柳乐儿把旧拨浪鼓横抱在胸前，紧贴着柳石站定，警惕地看着外圈三人。齐姓道士将白马尾拂尘搭在臂上，目光在柳石与柳乐儿之间来回逡巡。马脸男子悄然收回右手，退后半步，藏在袖中的左手暗握一枚暗器，目光死死钉住柳乐儿。
[Shot 4] 中景，燕承向两名同伴各使了一个眼色，随即正对柳石，背对镜头，以试探的语气说道：“不知阁下如何称呼？在下血刀会燕承，正与两位道友联手捉拿这妖狐，阁下若是与此事无关，还请自行离去。”`

func TestAuthoredH3ShotsBypassFreeDesignAndConflictingDerivedContext(t *testing.T) {
	ps := newTestProjectService(t)
	if err := ps.db.AutoMigrate(&models.Shot{}, &models.Character{}, &models.Asset{}, &models.SharedAssetReference{}); err != nil {
		t.Fatal(err)
	}
	p := models.Project{Title: "authored source"}
	if err := ps.db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	sc := models.Scene{ProjectID: p.ID, Characters: "燕承,柳石,柳乐儿,马脸男子,齐姓道士", Duration: 15, Content: yanAuthoredScene, VideoPrompt: "OLD_PROMPT_REVERSED_POSITION", BlockingJSON: `{"summary":"OLD_BLOCKING_WRONG_FACING"}`}
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
	for _, bad := range []string{"OLD_PROMPT", "OLD_BLOCKING", "OLD_SHOT", "OLD_SUBJECT", "武戏硬任务", "INCOMPLETE DRAFT"} {
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
}
