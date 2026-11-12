package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
)

const liuShiNetStory = "柳石依旧一言不发，稳稳挡在柳乐儿身前。燕承眼中凶光一闪，厉喝：“好，既然阁下非要与那妖女为伍，那么……动手！”话音未落，他右手握紧朴刀，刀身表面骤然浮出刺目白光；左手同时猛扬，一团黑光飞出，在半空急速旋转，展开成一张硕大的黑色大网。大网如黑云般压落，将柳乐儿与高大青年同时罩住。柳乐儿脸色愈发苍白，紧贴柳石缩起身子，右手死死攥住旧拨浪鼓；柳石站在她身前，没有后退。大网各处绳结逐一浮现锋利银色弯钩，寒光闪烁。"
const liuShiNetLine = "好，既然阁下非要与那妖女为伍，那么……动手！"

// The director response is a one-way cast: nobody blocks or resists the net.
const liuShiNetAction = "[Shot 1] A medium-wide side shot immediately establishes the standoff: 燕承 stands on screen-left facing right, while 柳石 stands center-right facing left, silent with lips closed, directly in front of 柳乐儿. The camera pushes in.\n" +
	"[Shot 2] At 00:02.500, a medium close-up frames 燕承 from a slightly low angle. 燕承 narrows his eyes at 柳石 and shouts. [[D1]] As the last word lands, a glaring white light surges along his saber blade.\n" +
	"[Shot 3] At 00:06.500, a side medium shot holds the axis as 燕承 snaps his left hand upward and a black light races over the heads of 柳石 and 柳乐儿.\n" +
	"[Shot 4] At 00:09.000, a low-angle wide shot looks up as the black light spins and unfolds into a huge black net that descends like a dark cloud over 柳石 and 柳乐儿.\n" +
	"[Shot 5] At 00:12.000, a close shot through the net: 柳乐儿 presses against 柳石's back and grips the old pellet drum; 柳石 does not step back as sharp silver hooks emerge one by one from the net's knots."

type liuShiNetHTTPModel struct {
	mu                         sync.Mutex
	actionCalls, plan, briefed int
	actionSystems               []string
}

func (m *liuShiNetHTTPModel) serve(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Messages []struct{ Role, Content string } `json:"messages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var all strings.Builder
	for _, msg := range req.Messages {
		all.WriteString(msg.Content)
		all.WriteByte('\n')
	}
	prompt := all.String()
	content := liuShiNetAction
	m.mu.Lock()
	switch {
	case strings.Contains(prompt, "actual_shot_numbers"):
		m.plan++
		content = `{"placements":[{"index":0,"shot":2}]}`
	case strings.Contains(prompt, `"retention"`):
		m.briefed++
		content = `{"summary":"[reference generation] Generate one continuous 15-second confrontation. <Subject 1> stands silent in front of <Subject 2> while <Subject 3> shouts, lights his saber and flings a black net over both; the video ends on gleaming hooks before any strike lands.","retention":["<Subject 3> stays on screen-left facing right; <Subject 1> stays center-right facing left with <Subject 2> behind him.","The black net originates only from <Subject 3>'s left hand."]}`
	default:
		m.actionCalls++
		system := []rune(req.Messages[0].Content)
		if len(system) > 60 {
			system = system[:60]
		}
		m.actionSystems = append(m.actionSystems, string(system))
	}
	m.mu.Unlock()
	reply, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": content}}}})
	w.Write(reply)
}

func TestLiuShiNetRegenerationHTTPMatchesBenchmarkStructure(t *testing.T) {
	model := &liuShiNetHTTPModel{}
	ai := httptest.NewServer(http.HandlerFunc(model.serve))
	defer ai.Close()
	s, scene := h3DialogueHTTPFixture(t, ai.URL)
	for _, table := range []any{&models.Dialogue{}, &models.Shot{}} {
		if err := s.DB.Where("scene_id = ?", scene.ID).Delete(table).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := s.DB.Where("project_id = ?", scene.ProjectID).Delete(&models.Character{}).Error; err != nil {
		t.Fatal(err)
	}
	names := []string{"柳石", "柳乐儿", "燕承"}
	for i, name := range names {
		if err := s.DB.Create(&models.Character{ProjectID: scene.ProjectID, Name: name, Sheet: fmt.Sprintf("sheet-%d.png", i+1)}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := s.DB.Model(&models.Scene{}).Where("id = ?", scene.ID).Updates(map[string]any{
		"content": liuShiNetStory, "characters": strings.Join(names, ","), "visible_characters": strings.Join(names, ","),
		"character_roles_set": true, "reference_images_json": "", "video_template": "minimax_h3_ref2v", "duration": 15,
		"scene_mode": "文武混合", "video_full_prompt": "", "story_content_edited": false, "director_source_stale": false,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Create(&models.Dialogue{ProjectID: scene.ProjectID, SceneID: scene.ID, Order: 1, Character: "燕承", SpeechType: "dialogue", Text: liuShiNetLine, Source: "manual"}).Error; err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/projects/:id/scenes/:sid/video/prompt/regenerate", s.HandleRegenerateSceneVideoPrompt)
	w := httptest.NewRecorder()
	q := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/projects/%d/scenes/%d/video/prompt/regenerate", scene.ProjectID, scene.ID), strings.NewReader(`{"template":"minimax_h3_ref2v"}`))
	q.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, q)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		FullPrompt string `json:"full_prompt"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	prompt := out.FullPrompt
	model.mu.Lock()
	actionCalls, planCalls, briefCalls := model.actionCalls, model.plan, model.briefed
	model.mu.Unlock()

	// One-way cast: the spell contract must accept the director body without a
	// repair round that would push it to invent a defense.
	if actionCalls != 1 {
		t.Fatalf("one-way net cast triggered %d action calls (repair loop): %q", actionCalls, model.actionSystems)
	}
	// Director-placed marker: speech lands at the shout beat, before the blade
	// light, without the separate placement planner.
	detail := h3PromptSection(prompt, "detailed_description:")
	shot2Start, shot3Start := strings.Index(detail, "[Shot 2]"), strings.Index(detail, "[Shot 3]")
	if shot2Start < 0 || shot3Start < shot2Start || planCalls != 0 || strings.Contains(prompt, "[[D") {
		t.Fatalf("planner=%d prompt=%s", planCalls, prompt)
	}
	shot2 := detail[shot2Start:shot3Start]
	speech := strings.Index(shot2, "<d>[Chinese] "+liuShiNetLine+"</d>")
	if strings.Count(prompt, "<d>") != 1 || speech < 0 || speech > strings.Index(shot2, "white light") || strings.Index(shot2, "shouts.") > speech {
		t.Fatalf("dialogue not at the speaking beat of Shot 2: %s", prompt)
	}
	// Director brief replaces the fixed summary and adds continuity notes.
	if briefCalls != 1 || !strings.Contains(h3PromptSection(prompt, "summary:"), "before any strike lands") || !strings.Contains(h3PromptSection(prompt, "retention_analysis:"), "screen-left facing right") {
		t.Fatalf("director brief not applied (calls=%d): %s", briefCalls, prompt)
	}
	for _, token := range []string{"[Shot 5] At 00:12.000,", "silver hooks", "does not step back"} {
		if !strings.Contains(detail, token) {
			t.Fatalf("missing %q: %s", token, prompt)
		}
	}
	for _, name := range names {
		if strings.Contains(strings.ReplaceAll(detail, liuShiNetLine, ""), name) {
			t.Fatalf("unbound name %q in visual body: %s", name, prompt)
		}
	}
}
