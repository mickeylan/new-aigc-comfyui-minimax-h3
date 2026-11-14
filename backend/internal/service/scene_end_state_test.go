package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func newSceneEndStateTestService(t *testing.T) (*Service, *gorm.DB, models.Project) {
	t.Helper()
	db := newTestDBWithNewModels(t)
	if err := db.AutoMigrate(&models.SceneEndState{}); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "p"}
	db.Create(&project)
	return &Service{DB: db, Skills: NewSkillService(db)}, db, project
}

func TestParseSceneEndStateOutputAcceptsObjectAndArrayForms(t *testing.T) {
	object := "```json\n{\"characters\":{\"韩立\":{\"posture\":\"单膝跪地\",\"holding\":\"右手青竹蜂云剑\",\"injury\":\"左肩剑伤\"},\"空\":{}},\"props\":{\"玉瓶\":\"碎在地上\",\"\":\"x\"}}\n```"
	state, err := parseSceneEndStateOutput(object)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Characters) != 1 || state.Characters["韩立"].Injury != "左肩剑伤" || state.Props["玉瓶"] != "碎在地上" || len(state.Props) != 1 {
		t.Fatalf("object form=%+v", state)
	}
	array := `{"characters":[{"name":"柳石","face":"嘴角带血","emotion":"咬牙"}],"props":[{"name":"木门","state":"半开"}]}`
	state, err = parseSceneEndStateOutput(array)
	if err != nil {
		t.Fatal(err)
	}
	if state.Characters["柳石"].Face != "嘴角带血" || state.Props["木门"] != "半开" {
		t.Fatalf("array form=%+v", state)
	}
	if _, err := parseSceneEndStateOutput(`{"characters":"韩立受伤"}`); err == nil {
		t.Fatal("a string characters field must be rejected, not guessed")
	}
	if _, err := parseSceneEndStateOutput("韩立受伤了"); err == nil {
		t.Fatal("non-JSON output must be rejected")
	}
	long, _ := parseSceneEndStateOutput(`{"characters":{"韩立":{"posture":"` + strings.Repeat("站", 200) + `"}}}`)
	if n := len([]rune(long.Characters["韩立"].Posture)); n != sceneEndStateFieldMax {
		t.Fatalf("posture length=%d", n)
	}
}

func TestSceneEndStateArchivistSkillIsSeeded(t *testing.T) {
	svc, db, _ := newSceneEndStateTestService(t)
	if err := db.AutoMigrate(&models.Skill{}); err != nil {
		t.Fatal(err)
	}
	if svc.sceneEndStateSkillReady() {
		t.Fatal("skill must be absent before seeding")
	}
	if err := svc.Skills.InitSystemSkills(); err != nil {
		t.Fatal(err)
	}
	skill, err := svc.Skills.latestEnabledSkillByCode(sceneEndStateSkillCode)
	if err != nil || skill.Version != 1 || skill.Stage != models.SkillStageContinuityReview || !strings.Contains(skill.SystemPrompt, "不续写") {
		t.Fatalf("skill=%+v err=%v", skill, err)
	}
}

func TestSceneBoundaryBreakDetectsPlaceTimeAndFlashback(t *testing.T) {
	prev := &models.Scene{Title: "洞府对峙", LocationName: "洞府", Content: "韩立退到石壁前。"}
	cases := []struct {
		name string
		cur  models.Scene
		want string
	}{
		{"same place", models.Scene{LocationName: "洞府", Content: "韩立抹去嘴角血迹。"}, ""},
		{"unknown location keeps continuity", models.Scene{Content: "韩立抬头。"}, ""},
		{"place change", models.Scene{LocationName: "山门", Content: "韩立走出山门。"}, "地点已变化"},
		{"time jump in opening", models.Scene{LocationName: "洞府", Content: "次日清早，韩立在洞府打坐。"}, "时段已变化"},
		{"flashback", models.Scene{Title: "回忆：初入宗门", LocationName: "洞府", Content: "少年韩立跪在殿前。"}, "回忆或梦境切换"},
		{"dialogue deep in body is not a boundary", models.Scene{LocationName: "洞府", Content: "韩立低声说道：“这洞府的禁制我早已看过三遍，昨夜梦中也在推演。”"}, ""},
	}
	for _, tc := range cases {
		if got := sceneBoundaryBreak(prev, &tc.cur); got != tc.want {
			t.Errorf("%s: got %q want %q", tc.name, got, tc.want)
		}
	}
}

func TestFormatInheritedSceneStateFiltersVisibleCharactersAndDowngradesAcrossBoundary(t *testing.T) {
	state := sceneEndStateData{
		Characters: map[string]sceneEndCharacterState{
			"韩立": {Posture: "单膝跪地", Holding: "右手青竹蜂云剑", Emotion: "强撑", Injury: "左肩剑伤"},
			"厉飞雨": {Posture: "倚门", Injury: "右腿骨折"},
		},
		Props: map[string]string{"玉瓶": "碎在地上"},
	}
	current := &models.Scene{VisibleCharacters: "韩立", CharacterRolesSet: true}
	strong := formatInheritedSceneState(state, current, "")
	for _, want := range []string{"同一时空连续", "单膝跪地", "青竹蜂云剑", "左肩剑伤", "道具 玉瓶"} {
		if !strings.Contains(strong, want) {
			t.Errorf("strong inherit missing %q:\n%s", want, strong)
		}
	}
	if strings.Contains(strong, "厉飞雨") {
		t.Fatalf("a character not visible in this Scene must not be injected:\n%s", strong)
	}
	weak := formatInheritedSceneState(state, current, "地点已变化")
	if !strings.Contains(weak, "左肩剑伤") || !strings.Contains(weak, "地点已变化") {
		t.Fatalf("weak inherit must keep injuries:\n%s", weak)
	}
	for _, banned := range []string{"单膝跪地", "青竹蜂云剑", "强撑", "玉瓶"} {
		if strings.Contains(weak, banned) {
			t.Errorf("weak inherit leaked %q:\n%s", banned, weak)
		}
	}
	uninjured := sceneEndStateData{Characters: map[string]sceneEndCharacterState{"韩立": {Posture: "站立"}}}
	if got := formatInheritedSceneState(uninjured, current, "时段已变化"); got != "" {
		t.Fatalf("nothing carries over, want empty block, got:\n%s", got)
	}
}

func TestInheritedSceneStateGoesStaleWhenPreviousSceneChanges(t *testing.T) {
	svc, db, p := newSceneEndStateTestService(t)
	prev := models.Scene{ProjectID: p.ID, EpisodeN: 1, Generation: 1, Order: 1, LocationName: "洞府", VisibleCharacters: "韩立", Content: "韩立被击退", VideoPrompt: "fallback prompt"}
	cur := models.Scene{ProjectID: p.ID, EpisodeN: 1, Generation: 1, Order: 2, LocationName: "洞府", VisibleCharacters: "韩立", CharacterRolesSet: true, Content: "韩立起身"}
	db.Create(&prev)
	db.Create(&cur)
	db.Create(&models.Shot{SceneID: prev.ID, Order: 1, ShotType: "近景", Duration: 3, EndState: "韩立跪地"})
	hash, _, err := svc.sceneEndStateSource(&prev)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.saveSceneEndState(&prev, sceneEndStateData{Characters: map[string]sceneEndCharacterState{"韩立": {Posture: "跪地", Injury: "左肩剑伤"}}}, hash, "manual", 0); err != nil {
		t.Fatal(err)
	}
	if got := svc.previousSceneEndState(&cur); !strings.Contains(got, "左肩剑伤") || !strings.Contains(got, "跪地") {
		t.Fatalf("fresh state must replace the fallback, got %q", got)
	}
	db.Model(&models.Shot{}).Where("scene_id = ?", prev.ID).Update("end_state", "韩立站起")
	view, err := svc.loadSceneEndState(&prev)
	if err != nil || !view.Stale {
		t.Fatalf("editing a Shot end state must mark the state stale: view=%+v err=%v", view, err)
	}
	if got := svc.previousSceneEndState(&cur); got != "fallback prompt" {
		t.Fatalf("a stale state must not be injected, got %q", got)
	}
}

func TestSceneEndStateHTTPSaveAndGetReportsSkillPending(t *testing.T) {
	svc, db, p := newSceneEndStateTestService(t)
	if err := db.AutoMigrate(&models.Skill{}); err != nil {
		t.Fatal(err)
	}
	sc := models.Scene{ProjectID: p.ID, EpisodeN: 1, Generation: 1, Order: 1, Content: "韩立被击退"}
	db.Create(&sc)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/projects/:id/scenes/:sid/end-state", svc.HandleGetSceneEndState)
	r.PUT("/api/projects/:id/scenes/:sid/end-state", svc.HandleSaveSceneEndState)
	url := fmt.Sprintf("/api/projects/%d/scenes/%d/end-state", p.ID, sc.ID)
	body := `{"characters":{"韩立":{"posture":"跪地","injury":"左肩剑伤"}},"props":{}}`
	req := httptest.NewRequest("PUT", url, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("save status=%d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", url, nil))
	var out sceneEndStateView
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.State == nil || out.State.Characters["韩立"].Injury != "左肩剑伤" || out.Stale || out.Record.ExtractedBy != "manual" || out.SkillReady {
		t.Fatalf("get=%s", w.Body.String())
	}
}
