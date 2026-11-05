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

type latestStoryHTTPModel struct {
	mu       sync.Mutex
	requests []string
}

func (m *latestStoryHTTPModel) serve(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Messages []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var text strings.Builder
	for _, message := range req.Messages {
		text.WriteString(message.Content)
		text.WriteByte('\n')
	}
	prompt := text.String()
	m.mu.Lock()
	m.requests = append(m.requests, prompt)
	m.mu.Unlock()

	if strings.Contains(prompt, "actual_shot_numbers") {
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"placements\":[{\"index\":0,\"shot\":1}]}"}}]}`)
		return
	}
	fmt.Fprint(w, `{"choices":[{"message":{"content":"[Shot 1] NEW_VISUAL_SHOT_ONE: Lin raises the new lantern beside the gate. [Shot 2] NEW_VISUAL_SHOT_TWO: the camera follows Lin into the bright courtyard."}}]}`)
}

func TestLatestStoryEditThenRegenerateHTTPUsesNoRetainedDirectorFacts(t *testing.T) {
	model := &latestStoryHTTPModel{}
	ai := httptest.NewServer(http.HandlerFunc(model.serve))
	defer ai.Close()
	s, scene := h3DialogueHTTPFixture(t, ai.URL)

	const (
		oldMovement = "OLD_MOVEMENT_RED_UMBRELLA"
		oldQuote    = "OLD_QUOTED_DIALOGUE"
		oldBlocking = "OLD_BLOCKING_POSITION"
		newQuote    = "现在只按新故事出发。"
	)
	oldContent := "Aold " + oldMovement + ` 旧人说道：“` + oldQuote + `”`
	if err := s.DB.Model(&models.Scene{}).Where("id = ?", scene.ID).Updates(map[string]any{
		"content": oldContent, "blocking_json": `{"summary":"` + oldBlocking + `"}`,
		"blocking_content_hash": "", "image_file": "keep.png", "video_file": "keep.mp4", "video_input_file": "keep-input.mp4",
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Where("scene_id = ?", scene.ID).Delete(&models.Dialogue{}).Error; err != nil {
		t.Fatal(err)
	}
	oldDialogue := models.Dialogue{ProjectID: scene.ProjectID, SceneID: scene.ID, Order: 1, Character: "旧人", Text: oldQuote, SpeechType: "dialogue", Source: "ai_reviewed", AudioFile: "old-audio.wav"}
	if err := s.DB.Create(&oldDialogue).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Model(&models.Shot{}).Where("scene_id = ?", scene.ID).Updates(map[string]any{"description": "OLD_SHOT_FACT", "prompt_action": oldMovement}).Error; err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.PUT("/api/projects/:id/scenes/:sid", s.HandleUpdateScene)
	r.POST("/api/projects/:id/scenes/:sid/video/prompt/regenerate", s.HandleRegenerateSceneVideoPrompt)
	base := fmt.Sprintf("/api/projects/%d/scenes/%d", scene.ProjectID, scene.ID)
	edit := httptest.NewRecorder()
	newContent := `林在明亮庭院举起新灯，说道：“` + newQuote + `”`
	editReq := httptest.NewRequest(http.MethodPut, base, strings.NewReader(fmt.Sprintf(`{"content":%q,"image_prompt":"new image","duration":9,"scene_mode":"文戏"}`, newContent)))
	editReq.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(edit, editReq)
	if edit.Code != http.StatusOK {
		t.Fatalf("edit status=%d body=%s", edit.Code, edit.Body.String())
	}

	regen := httptest.NewRecorder()
	regenReq := httptest.NewRequest(http.MethodPost, base+"/video/prompt/regenerate", strings.NewReader(`{"template":"minimax_h3_first_last"}`))
	regenReq.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(regen, regenReq)
	if regen.Code != http.StatusOK {
		t.Fatalf("regenerate status=%d body=%s", regen.Code, regen.Body.String())
	}
	var result struct{ FullPrompt string `json:"full_prompt"` }
	if err := json.Unmarshal(regen.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.FullPrompt, newQuote) || !strings.Contains(result.FullPrompt, "NEW_VISUAL_SHOT_ONE") || !strings.Contains(result.FullPrompt, "(S1)") {
		t.Fatalf("latest quote/speaker or generated visuals missing: %s", result.FullPrompt)
	}
	for _, stale := range []string{oldQuote, oldMovement, "OLD_SHOT_FACT", oldBlocking} {
		if strings.Contains(result.FullPrompt, stale) {
			t.Fatalf("final prompt leaked stale %q: %s", stale, result.FullPrompt)
		}
	}
	model.mu.Lock()
	requests := strings.Join(model.requests, "\n---REQUEST---\n")
	model.mu.Unlock()
	if !strings.Contains(requests, newContent) {
		t.Fatalf("model requests omitted latest source: %s", requests)
	}
	for _, stale := range []string{oldQuote, oldMovement, "OLD_SHOT_FACT", oldBlocking} {
		if strings.Contains(requests, stale) {
			t.Fatalf("model request leaked stale %q: %s", stale, requests)
		}
	}

	var saved models.Scene
	if err := s.DB.First(&saved, scene.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.Content != newContent || !saved.StoryContentEdited || !saved.DirectorSourceStale || saved.ImageFile != "keep.png" || saved.VideoFile != "keep.mp4" || saved.VideoInputFile != "keep-input.mp4" {
		t.Fatalf("latest source markers/media incorrect: %+v", saved)
	}
	var retained models.Dialogue
	if err := s.DB.First(&retained, oldDialogue.ID).Error; err != nil {
		t.Fatal(err)
	}
	if retained.Text != oldQuote || retained.AudioFile != "old-audio.wav" {
		t.Fatalf("historical raw dialogue/audio was mutated: %+v", retained)
	}
}

func TestFullShotReplacementClearsDirectorStaleButNotStoryEdited(t *testing.T) {
	db := newTestDBWithNewModels(t)
	project := models.Project{Title: "latest authority flags"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	scene := models.Scene{ProjectID: project.ID, Content: "new source", StoryContentEdited: true, DirectorSourceStale: true}
	if err := db.Create(&scene).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := NewShotService(db).ReplaceShots(scene.ID, []models.Shot{{ShotType: "wide", Duration: 3, Description: "new source only"}}); err != nil {
		t.Fatal(err)
	}
	var saved models.Scene
	if err := db.First(&saved, scene.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.DirectorSourceStale || !saved.StoryContentEdited {
		t.Fatalf("replacement flags incorrect: %+v", saved)
	}
}
