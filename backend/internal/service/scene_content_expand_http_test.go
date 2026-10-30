package service

import (
	"bytes"
	"comfyui-console/internal/models"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func expandHTTPService(t *testing.T, url string) (*Service, *gorm.DB, models.Scene) {
	db := newTestDB(t)
	if err := db.AutoMigrate(&models.Scene{}, &models.Character{}, &models.Asset{}, &models.Dialogue{}); err != nil {
		t.Fatal(err)
	}
	p := models.Project{Title: "project"}
	db.Create(&p)
	sc := models.Scene{ProjectID: p.ID, EpisodeN: 2, Generation: 3, Order: 5, Content: "saved", Duration: 4, SceneMode: "dialogue"}
	db.Create(&sc)
	for _, x := range []models.Scene{{ProjectID: p.ID, EpisodeN: 2, Generation: 3, Order: 4, Content: "previous"}, {ProjectID: p.ID, EpisodeN: 2, Generation: 3, Order: 6, Content: "next"}, {ProjectID: p.ID, EpisodeN: 2, Generation: 4, Order: 6, Content: "wrong-generation"}, {ProjectID: p.ID + 9, EpisodeN: 2, Generation: 3, Order: 6, Content: "wrong-project"}} {
		db.Create(&x)
	}
	db.Create(&models.Skill{Name: "expand", Code: sceneContentExpandSkillCode, Operation: sceneContentExpandSkillCode, Version: 1, Stage: models.SkillStageStoryboard, Enabled: true})
	g := &mockGetter{settings: map[string]string{SettingTextProvider: "llama", SettingLlamaBaseURL: url, SettingLlamaModel: "mock"}}
	return &Service{DB: db, Skills: NewSkillService(db), TextProviderFact: NewTextProviderFactory(g)}, db, sc
}
func expandRequest(s *Service, p, sid uint, body string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/projects/:id/scenes/:sid/content/expand", s.HandleExpandSceneContent)
	q := httptest.NewRequest("POST", fmt.Sprintf("/api/projects/%d/scenes/%d/content/expand", p, sid), bytes.NewBufferString(body))
	q.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, q)
	return w
}

func TestExpandSceneContentHTTPDraftContextAndReadOnly(t *testing.T) {
	var calls atomic.Int32
	var request map[string]any
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "POST" || r.URL.Path != "/chat/completions" {
			t.Errorf("request %s %s", r.Method, r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&request)
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"content\":\"expanded\",\"director_note\":\"note\"}"}}]}`)
	}))
	defer ai.Close()
	s, db, sc := expandHTTPService(t, ai.URL)
	draft := "She says: KEEP THIS EXACT. Then turns."
	w := expandRequest(s, sc.ProjectID, sc.ID, `{"content":"`+draft+`"}`)
	if w.Code != 200 {
		t.Fatalf("status=%d %s", w.Code, w.Body.String())
	}
	var out struct {
		Content   string `json:"content"`
		Persisted bool   `json:"persisted"`
	}
	json.Unmarshal(w.Body.Bytes(), &out)
	if out.Content != "expanded" || out.Persisted || calls.Load() != 1 {
		t.Fatalf("out=%+v calls=%d", out, calls.Load())
	}
	msgs := request["messages"].([]any)
	user := msgs[1].(map[string]any)["content"].(string)
	for _, v := range []string{draft, "previous", "next", "KEEP THIS EXACT"} {
		if !strings.Contains(user, v) {
			t.Errorf("missing %q", v)
		}
	}
	for _, v := range []string{"saved", "wrong-generation", "wrong-project"} {
		if strings.Contains(user, v) {
			t.Errorf("leaked %q", v)
		}
	}
	var after models.Scene
	db.First(&after, sc.ID)
	if after.Content != "saved" {
		t.Fatalf("persisted %q", after.Content)
	}
}

// Exercise production startup seeding rather than relying on the manually-created fixture Skill.
func TestExpandSceneContentHTTPAfterSystemSkillInitialization(t *testing.T) {
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"content\":\"expanded\",\"director_note\":\"note\"}"}}]}`)
	}))
	defer ai.Close()
	s, db, sc := expandHTTPService(t, ai.URL)
	if err := db.Where("code = ?", sceneContentExpandSkillCode).Delete(&models.Skill{}).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := s.Skills.InitSystemSkills(); err != nil {
			t.Fatal(err)
		}
	}
	var skills []models.Skill
	if err := db.Where("code = ?", sceneContentExpandSkillCode).Find(&skills).Error; err != nil {
		t.Fatal(err)
	}
	if len(skills) != 1 || !skills[0].IsSystem || !skills[0].Enabled || skills[0].Operation != sceneContentExpandSkillCode || skills[0].Stage != models.SkillStageStoryboard {
		t.Fatalf("missing or duplicate production expansion skill: %+v", skills)
	}
	w := expandRequest(s, sc.ProjectID, sc.ID, `{"content":"current draft"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d %s", w.Code, w.Body.String())
	}
	var after models.Scene
	if err := db.First(&after, sc.ID).Error; err != nil {
		t.Fatal(err)
	}
	if after.Content != sc.Content {
		t.Fatal("preview persisted scene content")
	}
	var count int64
	if err := db.Model(&models.SkillAuditLog{}).Where("project_id = ? AND skill_id = ? AND success = ?", sc.ProjectID, skills[0].ID, true).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected one successful expansion audit, got %d", count)
	}
}

func TestExpandSceneContentHTTPValidationAndFailures(t *testing.T) {
	var calls atomic.Int32
	var upstream atomic.Bool
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if upstream.Load() {
			http.Error(w, "down", 500)
			return
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"invalid"}}]}`)
	}))
	defer ai.Close()
	s, _, sc := expandHTTPService(t, ai.URL)
	cases := []struct {
		p, s uint
		b    string
		code int
	}{{sc.ProjectID + 1, sc.ID, `{"content":"x"}`, 404}, {sc.ProjectID, sc.ID + 99, `{"content":"x"}`, 404}, {sc.ProjectID, sc.ID, `{`, 400}, {sc.ProjectID, sc.ID, `{"content":" "}`, 400}}
	for _, c := range cases {
		before := calls.Load()
		w := expandRequest(s, c.p, c.s, c.b)
		if w.Code != c.code || calls.Load() != before {
			t.Fatalf("validation status=%d calls=%d", w.Code, calls.Load())
		}
	}
	upstream.Store(true)
	if w := expandRequest(s, sc.ProjectID, sc.ID, `{"content":"x"}`); w.Code != 502 {
		t.Fatalf("upstream=%d", w.Code)
	}
	upstream.Store(false)
	if w := expandRequest(s, sc.ProjectID, sc.ID, `{"content":"x"}`); w.Code != 502 {
		t.Fatalf("format=%d", w.Code)
	}
	// llama retries a transient 5xx once, then the malformed successful response is one request.
	if calls.Load() != 3 {
		t.Fatalf("calls=%d", calls.Load())
	}
}
