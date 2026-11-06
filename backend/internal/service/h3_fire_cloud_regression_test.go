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

type fireCloudHTTPModel struct {
	mu       sync.Mutex
	requests []string
}

func (m *fireCloudHTTPModel) serve(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Messages []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var b strings.Builder
	for _, message := range req.Messages {
		b.WriteString(message.Content)
		b.WriteByte('\n')
	}
	prompt := b.String()
	m.mu.Lock()
	m.requests = append(m.requests, prompt)
	m.mu.Unlock()
	if strings.Contains(prompt, "actual_shot_numbers") {
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"placements\":[{\"index\":0,\"shot\":1}]}"}}]}`)
		return
	}
	fmt.Fprint(w, `{"choices":[{"message":{"content":"[Shot 1] At normal real-time speed, a fire cloud presses down onto the tall grass and vegetation ignites on contact. 齐姓道士 fixes his gaze ahead and extends his hand; his white horsetail whisk materializes in a flash of light in that hand and he sweeps it. Several pale gusts fan outward and roll burning grass deeper into the thicket. The wind fans the flames, black smoke billows, and ash is blown away."}}]}`)
}

func TestH3SourceEventLedgerPreservesQuotedSpeechAndUsesEventMarkers(t *testing.T) {
	got := h3SourceEventLedger(fireCloudStory)
	if len(got) != 4 {
		t.Fatalf("ledger entries=%d, want 4: %#v", len(got), got)
	}
	joined := strings.Join(got, "\n")
	for _, clause := range []string{"火云沿着齐人高的草丛压下", "烈焰骤然窜起", "“无量天尊”", "灵光闪现", "白蒙蒙狂风", "草灰被卷得横飞出去"} {
		if !strings.Contains(joined, clause) {
			t.Fatalf("ledger lost source clause %q: %s", clause, joined)
		}
	}
	formatted := formatH3SourceEventLedger(fireCloudStory)
	if !strings.Contains(formatted, "EVENT 1:") || strings.Contains(formatted, "Shot 1") || strings.Contains(formatted, "[Shot") {
		t.Fatalf("ledger markers are not source-event markers: %s", formatted)
	}
}

func TestFireCloudRegenerationHTTPPreservesSourceOrderAndAudibleDialogue(t *testing.T) {
	model := &fireCloudHTTPModel{}
	ai := httptest.NewServer(http.HandlerFunc(model.serve))
	defer ai.Close()
	s, scene := h3DialogueHTTPFixture(t, ai.URL)
	if err := s.DB.AutoMigrate(&models.Task{}); err != nil {
		t.Fatal(err)
	}

	if err := s.DB.Where("scene_id = ?", scene.ID).Delete(&models.Dialogue{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Where("scene_id = ? AND \"order\" > ?", scene.ID, 1).Delete(&models.Shot{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Model(&models.Shot{}).Where("scene_id = ?", scene.ID).Updates(map[string]any{"duration": 7, "description": "", "prompt_subject": "", "prompt_action": ""}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Model(&models.Scene{}).Where("id = ?", scene.ID).Updates(map[string]any{
		"content": fireCloudStory, "duration": 7, "scene_mode": "文武混合", "characters": "齐姓道士",
		"video_full_prompt": "", "video_prompt": "", "blocking_json": "", "image_file": "qi-first.png",
		"reference_images_json": `["qi-first.png","companion-a.png","companion-b.png"]`,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Create(&models.Character{ProjectID: scene.ProjectID, Name: "齐姓道士", Portrait: "qi-first.png", Role: "道士"}).Error; err != nil {
		t.Fatal(err)
	}
	// No Dialogue row is seeded: the regeneration path must recover the
	// audible invocation from the user's actual prose rather than a test fixture.

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/projects/:id/scenes/:sid/video/prompt/regenerate", s.HandleRegenerateSceneVideoPrompt)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/projects/%d/scenes/%d/video/prompt/regenerate", scene.ProjectID, scene.ID), strings.NewReader(`{"template":"minimax_h3_ref2v"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		FullPrompt          string `json:"full_prompt"`
		DialogueCount       int    `json:"dialogue_count"`
		PromptDialogueCount int    `json:"prompt_dialogue_count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	terms := []string{"fire cloud presses down", "vegetation ignites", "materializes in a flash", "fan outward", "ash is blown away"}
	last := -1
	lower := strings.ToLower(out.FullPrompt)
	for _, term := range terms {
		at := strings.Index(lower, term)
		if at < 0 || at <= last {
			t.Fatalf("missing or misordered %q in prompt: %s", term, out.FullPrompt)
		}
		last = at
	}
	if strings.Count(out.FullPrompt, "<d>[Chinese] 无量天尊</d>") != 1 || out.DialogueCount != 1 || out.PromptDialogueCount != 1 {
		t.Fatalf("audible dialogue/count mismatch: counts=%d/%d prompt=%s", out.DialogueCount, out.PromptDialogueCount, out.FullPrompt)
	}
	if strings.Contains(out.FullPrompt, "No dialogue") || strings.Contains(out.FullPrompt, "汉字禁止") {
		t.Fatalf("audible scene was marked silent or Chinese-forbidden: %s", out.FullPrompt)
	}
	model.mu.Lock()
	requests := append([]string(nil), model.requests...)
	model.mu.Unlock()
	if len(requests) == 0 {
		t.Fatal("expected explicit mock LLM requests")
	}
	allRequests := strings.Join(requests, "\n---REQUEST---\n")
	if !strings.Contains(allRequests, fireCloudStory) || !strings.Contains(allRequests, "SOURCE EVENT LEDGER") {
		t.Fatalf("generation requests omitted full source or ledger: %s", allRequests)
	}
	for _, clause := range h3SourceEventLedger(fireCloudStory) {
		if !strings.Contains(allRequests, clause) {
			t.Fatalf("generation requests omitted ledger clause %q: %s", clause, allRequests)
		}
	}
	var saved models.Scene
	if err := s.DB.First(&saved, scene.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.Content != fireCloudStory {
		t.Fatalf("source DB content modified: %q", saved.Content)
	}
	var tasks int64
	if err := s.DB.Model(&models.Task{}).Count(&tasks).Error; err != nil {
		t.Fatal(err)
	}
	if tasks != 0 {
		t.Fatalf("regeneration unexpectedly made GPU tasks: %d", tasks)
	}
}
