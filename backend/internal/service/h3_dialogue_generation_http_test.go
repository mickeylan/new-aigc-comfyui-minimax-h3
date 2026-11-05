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

type h3HTTPMockState struct {
	mu        sync.Mutex
	planCalls int
	alwaysBad bool
	planUsers []string
}

func (m *h3HTTPMockState) serve(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Messages []struct {
			Role, Content string
		} `json:"messages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var all, user strings.Builder
	for _, msg := range req.Messages {
		all.WriteString(msg.Content)
		all.WriteByte('\n')
		if msg.Role == "user" {
			user.WriteString(msg.Content)
		}
	}
	if strings.Contains(all.String(), "accepted_visual_timeline") && strings.Contains(all.String(), `"placements"`) {
		m.mu.Lock()
		m.planCalls++
		call := m.planCalls
		m.planUsers = append(m.planUsers, user.String())
		bad := m.alwaysBad
		m.mu.Unlock()
		if bad || call == 1 {
			fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"placements\":[{\"index\":0,\"shot\":1}]}"}}]}`)
			return
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"placements\":[{\"index\":0,\"shot\":1},{\"index\":1,\"shot\":2}]}"}}]}`)
		return
	}
	// The visual-generation response deliberately contains no dialogue. It is
	// already a valid three-shot body, so quality/normalization calls can safely
	// receive the same deterministic answer.
	fmt.Fprint(w, `{"choices":[{"message":{"content":"[Shot 1] A stern horse-faced man blocks the path and points at the traveler. [Shot 2] Yan Cheng holds his ground and answers with a calm gesture. [Shot 3] Both men remain apart as the camera settles on the tense courtyard."}}]}`)
}

func h3DialogueHTTPFixture(t *testing.T, aiURL string) (*Service, models.Scene) {
	t.Helper()
	s, db, scene := expandHTTPService(t, aiURL)
	if err := db.AutoMigrate(&models.Template{}, &models.Episode{}, &models.PromptPolicyOverride{}, &models.GenerationCandidate{}, &models.SkillAuditLog{}); err != nil {
		t.Fatal(err)
	}
	s.Projects = &ProjectService{db: db, textProvider: s.TextProviderFact, skills: s.Skills}
	if err := s.Skills.InitSystemSkills(); err != nil {
		t.Fatal(err)
	}
	var skill models.Skill
	if err := db.Where("code = ?", h3DialogueTimelineSkillCode).First(&skill).Error; err != nil || skill.Operation != h3DialogueTimelineSkillCode {
		t.Fatalf("dialogue timeline skill operation not initialized: skill=%+v err=%v", skill, err)
	}
	q1, q2 := "你究竟是什么人？", "在下燕承，与阁下并无恶意。"
	scene.Content = `马脸男子质问：“` + q1 + `”燕承应答：“` + q2 + `”`
	scene.Duration, scene.SceneMode, scene.VideoTemplate = 9, "drama", "minimax_h3_first_last"
	scene.VideoFullPrompt, scene.ImageFile, scene.VideoFile, scene.VideoInputFile = "manual saved full prompt", "keep.png", "keep.mp4", "keep-input.mp4"
	if err := db.Save(&scene).Error; err != nil {
		t.Fatal(err)
	}
	for i, description := range []string{"马脸男子拦路质问", "燕承从容回应", "二人对峙收束"} {
		if err := db.Create(&models.Shot{SceneID: scene.ID, Order: i + 1, Duration: 3, Description: description, PromptSubject: description, PromptAction: description}).Error; err != nil {
			t.Fatal(err)
		}
	}
	for i, d := range []models.Dialogue{
		{ProjectID: scene.ProjectID, SceneID: scene.ID, Order: 1, Character: "马脸男子", SpeechType: "dialogue", Text: q1, Source: "ai_reviewed"},
		{ProjectID: scene.ProjectID, SceneID: scene.ID, Order: 2, Character: "燕承", SpeechType: "dialogue", Text: q2, Source: "ai_reviewed"},
	} {
		_ = i
		if err := db.Create(&d).Error; err != nil {
			t.Fatal(err)
		}
	}
	return s, scene
}

func TestH3DialogueGenerationHTTPFinalComposition(t *testing.T) {
	for _, tc := range []struct {
		name       string
		alwaysBad  bool
		wantStatus int
	}{
		{name: "repairs omitted placement and returns composed preview", wantStatus: http.StatusOK},
		{name: "rejects persistently incomplete plan", alwaysBad: true, wantStatus: http.StatusBadGateway},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := &h3HTTPMockState{alwaysBad: tc.alwaysBad}
			ai := httptest.NewServer(http.HandlerFunc(state.serve))
			defer ai.Close()
			s, scene := h3DialogueHTTPFixture(t, ai.URL)
			gin.SetMode(gin.TestMode)
			r := gin.New()
			r.POST("/api/projects/:id/scenes/:sid/video/prompt/regenerate", s.HandleRegenerateSceneVideoPrompt)
			w := httptest.NewRecorder()
			q := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/projects/%d/scenes/%d/video/prompt/regenerate", scene.ProjectID, scene.ID), strings.NewReader(`{"template":"minimax_h3_first_last"}`))
			q.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, q)
			if w.Code != tc.wantStatus {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			state.mu.Lock()
			planCalls := state.planCalls
			users := append([]string(nil), state.planUsers...)
			state.mu.Unlock()
			if planCalls != 2 || len(users) != 2 || !strings.Contains(users[1], "Previous plan invalid") {
				t.Fatalf("expected exactly one plan repair: calls=%d users=%q", planCalls, users)
			}
			var saved models.Scene
			if err := s.DB.First(&saved, scene.ID).Error; err != nil {
				t.Fatal(err)
			}
			if saved.VideoFullPrompt != "manual saved full prompt" || saved.ImageFile != "keep.png" || saved.VideoFile != "keep.mp4" || saved.VideoInputFile != "keep-input.mp4" {
				t.Fatalf("regenerate persisted preview/media changes: %+v", saved)
			}
			if tc.alwaysBad {
				if !strings.Contains(w.Body.String(), `"code":"dialogue_composition_failed"`) {
					t.Fatalf("missing composition error code: %s", w.Body.String())
				}
				return
			}
			var out struct {
				FullPrompt string `json:"full_prompt"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
			q1, q2 := "你究竟是什么人？", "在下燕承，与阁下并无恶意。"
			if strings.Count(out.FullPrompt, q1) != 1 || strings.Count(out.FullPrompt, q2) != 1 || strings.Index(out.FullPrompt, q1) < strings.Index(out.FullPrompt, "[Shot 1]") || strings.Index(out.FullPrompt, q2) < strings.Index(out.FullPrompt, "[Shot 2]") || strings.Index(out.FullPrompt, q2) > strings.Index(out.FullPrompt, "[Shot 3]") {
				t.Fatalf("dialogue not composed into shots 1/2 exactly once: %s", out.FullPrompt)
			}
			if !strings.Contains(out.FullPrompt, "(S1)") || !strings.Contains(out.FullPrompt, "(S2)") {
				t.Fatalf("stable independent speaker identities missing: %s", out.FullPrompt)
			}
		})
	}
}
