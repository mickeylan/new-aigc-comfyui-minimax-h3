package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
)

type authoredBindingsHTTPModel struct {
	mu       sync.Mutex
	requests []string
}

func (m *authoredBindingsHTTPModel) serve(w http.ResponseWriter, r *http.Request) {
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
	m.mu.Lock()
	m.requests = append(m.requests, prompt)
	m.mu.Unlock()
	if strings.Contains(prompt, "actual_shot_numbers") {
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"placements\":[{\"index\":0,\"shot\":2,\"after\":\"A frontal close-up of <Subject 2>, lips closed during his inner voice.\"},{\"index\":1,\"shot\":4,\"after\":\"<Subject 2> signals <Subject 3> and <Subject 4>, then faces <Subject 5> with his back to the camera.\"}]}"}}]}`)
		return
	}
	fmt.Fprint(w, `{"choices":[{"message":{"content":"[Shot 1] Three faint scanning lights pass over 柳石 from head to toe while 燕承, 马脸男子, and 齐姓道士 observe; no spiritual-energy fluctuation appears. [Shot 2] A frontal close-up of 燕承, lips closed during his inner voice. [Shot 3] 柳石 faces the camera with his head lowered, standing to 柳乐儿's right; 柳乐儿 hugs an old pellet drum while 齐姓道士 and 马脸男子 watch. [Shot 4] 燕承 signals 齐姓道士 and 马脸男子, then faces 柳石 with his back to the camera."}}]}`)
}

func TestAuthoredBindingsHTTPDefaultRef2V(t *testing.T) {
	model := &authoredBindingsHTTPModel{}
	ai := httptest.NewServer(http.HandlerFunc(model.serve))
	defer ai.Close()
	s, scene := h3DialogueHTTPFixture(t, ai.URL)

	if err := s.DB.Where("scene_id = ?", scene.ID).Delete(&models.Dialogue{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Where("scene_id = ?", scene.ID).Delete(&models.Shot{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Where("project_id = ?", scene.ProjectID).Delete(&models.Character{}).Error; err != nil {
		t.Fatal(err)
	}
	names := []string{"柳乐儿", "燕承", "齐姓道士", "马脸男子", "柳石"}
	for i, name := range names {
		if err := s.DB.Create(&models.Character{ProjectID: scene.ProjectID, Name: name, Sheet: fmt.Sprintf("sheet-%d.png", i+1)}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := s.DB.Model(&models.Scene{}).Where("id = ?", scene.ID).Updates(map[string]any{
		"content": yanAuthoredScene, "characters": strings.Join(names, ","), "visible_characters": strings.Join(names, ","),
		"character_roles_set": true, "reference_images_json": "", "video_template": "minimax_h3_ref2v", "duration": 15,
		"video_full_prompt": "incoming stored full prompt", "story_content_edited": false, "director_source_stale": true,
	}).Error; err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 4; i++ {
		if err := s.DB.Create(&models.Shot{SceneID: scene.ID, Order: i, Duration: 3, Description: "STALE_CONFLICT", PromptAction: "STALE_ACTION"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, d := range []models.Dialogue{
		{ProjectID: scene.ProjectID, SceneID: scene.ID, Order: 1, Character: "燕承", SpeechType: "monologue", Text: "没有灵力？", Source: "manual"},
		{ProjectID: scene.ProjectID, SceneID: scene.ID, Order: 2, Character: "燕承", SpeechType: "dialogue", Text: "不知阁下如何称呼？在下血刀会燕承，正与两位道友联手捉拿这妖狐，阁下若是与此事无关，还请自行离去。", Source: "manual"},
	} {
		if err := s.DB.Create(&d).Error; err != nil {
			t.Fatal(err)
		}
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/projects/:id/scenes/:sid/video/prompt/regenerate", s.HandleRegenerateSceneVideoPrompt)
	w := httptest.NewRecorder()
	q := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/projects/%d/scenes/%d/video/prompt/regenerate", scene.ProjectID, scene.ID), strings.NewReader(`{"template":"minimax_h3_ref2v"}`))
	q.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, q)
	if w.Code != http.StatusOK {
		var current models.Scene
		s.DB.First(&current, scene.ID)
		t.Fatalf("status=%d body=%s dialogues=%+v requests=%v", w.Code, w.Body.String(), s.Projects.sceneVideoDialogues(&current), model.requests)
	}
	var out struct {
		FullPrompt      string   `json:"full_prompt"`
		ReferenceImages []string `json:"reference_images"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	for i, name := range names {
		if !strings.Contains(h3PromptSection(out.FullPrompt, "subject_definitions:"), fmt.Sprintf("「%s」", name)) || !strings.Contains(out.FullPrompt, fmt.Sprintf("<Picture %d>", i+1)) {
			t.Fatalf("reference binding missing: %s", out.FullPrompt)
		}
	}
	for _, token := range []string{"[Shot 1]", "[Shot 2]", "[Shot 3]", "[Shot 4]", "(S1)", "<Subject 1>", "<Subject 2>", "<Subject 3>", "<Subject 4>", "<Subject 5>", "back to the camera"} {
		if !strings.Contains(out.FullPrompt, token) {
			t.Fatalf("missing %q: %s", token, out.FullPrompt)
		}
	}
	visual := regexp.MustCompile(`(?s)<d>.*?</d>`).ReplaceAllString(h3PromptSection(out.FullPrompt, "detailed_description:"), "")
	for _, name := range names {
		if strings.Contains(visual, name) {
			t.Fatalf("unbound visual name %q outside dialogue: %s", name, visual)
		}
	}
	for _, stale := range []string{"STALE_CONFLICT", "STALE_ACTION"} {
		if strings.Contains(out.FullPrompt, stale) {
			t.Fatalf("stale fact leaked: %s", out.FullPrompt)
		}
	}
	var saved models.Scene
	if err := s.DB.First(&saved, scene.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.VideoFullPrompt != "incoming stored full prompt" {
		t.Fatalf("regeneration persisted preview: %q", saved.VideoFullPrompt)
	}
}
