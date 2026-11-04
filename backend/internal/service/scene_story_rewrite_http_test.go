package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const storyRewriteText = `女童奔向草丛。身后远处，一名男子厉喝："妖孽，哪里跑！"女童闻声...`

type storyHTTPFixture struct {
	s        *Service
	db       *gorm.DB
	scene    models.Scene
	dialogue models.Dialogue
	router   *gin.Engine
	base     string
	mu       sync.Mutex
	request  map[string]any
	reply    string
}

func newStoryHTTPFixture(t *testing.T) *storyHTTPFixture {
	t.Helper()
	var f *storyHTTPFixture
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if err := json.NewDecoder(r.Body).Decode(&f.request); err != nil {
			t.Errorf("decode provider request: %v", err)
		}
		fmt.Fprintf(w, `{"choices":[{"message":{"content":%q}}]}`, f.reply)
	}))
	t.Cleanup(ai.Close)

	s, db, scene := expandHTTPService(t, ai.URL)
	f = &storyHTTPFixture{s: s, db: db, scene: scene}
	if err := db.AutoMigrate(&models.Chapter{}, &models.EpisodeAdaptation{}, &models.GenerationCandidate{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Where("code = ?", sceneContentExpandSkillCode).Delete(&models.Skill{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.Skills.InitSystemSkills(); err != nil {
		t.Fatal(err)
	}

	scene.Content = "旧场景正文"
	scene.ImageFile = "keep.png"
	scene.VideoFile = "keep.mp4"
	scene.VideoFullPrompt = "keep full prompt"
	if err := db.Save(&scene).Error; err != nil {
		t.Fatal(err)
	}
	chapter := models.Chapter{ProjectID: scene.ProjectID, Order: 3, Title: "本集章", Content: "女童奔向草丛。\n\n身后远处，一名男子厉喝：“妖孽，哪里跑！”\n\n女童闻声钻入更深的草丛。\n\n追兵拨开枝叶继续逼近。\n\n不应进入证据的后续段落。"}
	if err := db.Create(&chapter).Error; err != nil {
		t.Fatal(err)
	}
	ids, _ := json.Marshal([]uint{chapter.ID})
	if err := db.Create(&models.EpisodeAdaptation{ProjectID: scene.ProjectID, EpisodeN: scene.EpisodeN, SourceChapterIDs: string(ids)}).Error; err != nil {
		t.Fatal(err)
	}
	f.dialogue = models.Dialogue{ProjectID: scene.ProjectID, SceneID: scene.ID, Order: 1, Character: "", SpeechType: "unknown", Text: "妖孽，哪里跑！", Source: "user"}
	if err := db.Create(&f.dialogue).Error; err != nil {
		t.Fatal(err)
	}
	// These similarly-shaped sources must never enter this scene's provider evidence.
	otherChapter := models.Chapter{ProjectID: scene.ProjectID + 99, Order: 3, Content: "其他项目机密：男子厉喝：“妖孽，哪里跑！”"}
	if err := db.Create(&otherChapter).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Chapter{ProjectID: scene.ProjectID, Order: 4, Content: "本项目其他章节机密"}).Error; err != nil {
		t.Fatal(err)
	}
	f.reply = mustJSON(sceneStoryRewriteResult{Content: storyRewriteText, Dialogues: []sceneStoryDialogue{{ID: f.dialogue.ID, Speaker: "远处追兵男子", SpeechType: "offscreen_dialogue", Text: f.dialogue.Text}}})

	gin.SetMode(gin.TestMode)
	f.router = gin.New()
	base := "/api/projects/:id/scenes/:sid/story"
	f.router.POST(base+"/preview", s.HandlePreviewSceneStory)
	f.router.POST(base+"/apply", s.HandleApplySceneStory)
	f.base = fmt.Sprintf("/api/projects/%d/scenes/%d/story", scene.ProjectID, scene.ID)
	f.scene = scene
	return f
}

func (f *storyHTTPFixture) post(t *testing.T, suffix string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, f.base+suffix, bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	f.router.ServeHTTP(w, r)
	return w
}

func (f *storyHTTPFixture) preview(t *testing.T) sceneStoryApplyRequest {
	t.Helper()
	w := f.post(t, "/preview", map[string]string{"content": "女童正在逃跑的分镜草稿"})
	if w.Code != http.StatusOK {
		t.Fatalf("preview status=%d body=%s", w.Code, w.Body.String())
	}
	var out sceneStoryApplyRequest
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestSceneStoryRewriteHTTPPreviewIsReadOnlyAndUsesScopedSource(t *testing.T) {
	f := newStoryHTTPFixture(t)
	before := f.scene
	preview := f.preview(t)
	if preview.Content != storyRewriteText || len(preview.Dialogues) != 1 || preview.Dialogues[0].Speaker != "远处追兵男子" || preview.Dialogues[0].SpeechType != "offscreen_dialogue" || preview.SourceHash == "" {
		t.Fatalf("unexpected preview: %+v", preview)
	}
	var saved models.Scene
	if err := f.db.First(&saved, f.scene.ID).Error; err != nil {
		t.Fatal(err)
	}
	var dialogue models.Dialogue
	if err := f.db.First(&dialogue, f.dialogue.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.Content != before.Content || dialogue.Character != "" || dialogue.SpeechType != "unknown" || dialogue.Source != "user" {
		t.Fatalf("preview mutated DB: scene=%+v dialogue=%+v", saved, dialogue)
	}
	f.mu.Lock()
	request := f.request
	f.mu.Unlock()
	messages, ok := request["messages"].([]any)
	if !ok || len(messages) < 2 {
		t.Fatalf("provider request missing messages: %#v", request)
	}
	user := messages[len(messages)-1].(map[string]any)["content"].(string)
	for _, want := range []string{"女童正在逃跑的分镜草稿", "一名男子厉喝", "女童闻声钻入更深的草丛", f.dialogue.Text} {
		if !strings.Contains(user, want) {
			t.Errorf("provider input missing %q: %s", want, user)
		}
	}
	for _, leak := range []string{"不应进入证据的后续段落", "本项目其他章节机密", "其他项目机密"} {
		if strings.Contains(user, leak) {
			t.Errorf("provider input leaked %q: %s", leak, user)
		}
	}
}

func TestSceneStoryRewriteHTTPExplicitApplyPersistsOnlyStoryFields(t *testing.T) {
	f := newStoryHTTPFixture(t)
	shot := models.Shot{SceneID: f.scene.ID, Order: 1, Description: "keep shot", PromptAction: "keep-shot-action"}
	if err := f.db.Create(&shot).Error; err != nil {
		t.Fatal(err)
	}
	candidate := models.GenerationCandidate{ProjectID: f.scene.ProjectID, EntityType: "scene", EntityID: f.scene.ID, MediaType: "image", TaskID: "keep-task", ReviewStatus: "approved", IsCurrent: true, File: "keep-candidate.png"}
	if err := f.db.Create(&candidate).Error; err != nil {
		t.Fatal(err)
	}
	preview := f.preview(t)
	w := f.post(t, "/apply", preview)
	if w.Code != http.StatusOK {
		t.Fatalf("apply status=%d body=%s", w.Code, w.Body.String())
	}
	var saved models.Scene
	var dialogue models.Dialogue
	var savedShot models.Shot
	var savedCandidate models.GenerationCandidate
	f.db.First(&saved, f.scene.ID)
	f.db.First(&dialogue, f.dialogue.ID)
	f.db.First(&savedShot, shot.ID)
	f.db.First(&savedCandidate, candidate.ID)
	if saved.Content != storyRewriteText || !saved.PromptStale || dialogue.Character != "远处追兵男子" || dialogue.SpeechType != "offscreen_dialogue" || dialogue.Source != "ai_reviewed" {
		t.Fatalf("story fields not applied: scene=%+v dialogue=%+v", saved, dialogue)
	}
	if saved.ImageFile != "keep.png" || saved.VideoFile != "keep.mp4" || saved.VideoFullPrompt != "keep full prompt" || savedShot.Description != "keep shot" || savedShot.PromptAction != "keep-shot-action" || savedCandidate.File != "keep-candidate.png" || savedCandidate.ReviewStatus != "approved" || !savedCandidate.IsCurrent {
		t.Fatalf("media, shot, or candidate changed: scene=%+v shot=%+v candidate=%+v", saved, savedShot, savedCandidate)
	}
	ps := &ProjectService{db: f.db}
	dubs := ps.sceneVideoDialogues(&saved)
	compiled := appendStructuredDialogue("[Shot 1] 女童奔向草丛。", dubs, nil)
	if len(dubs) != 1 || dubs[0].Character != "远处追兵男子" || !strings.Contains(compiled, "远处追兵男子") || !strings.Contains(compiled, f.dialogue.Text) || strings.Contains(compiled, "女童 says") || strings.Contains(compiled, "女童说道") {
		t.Fatalf("reviewed offscreen attribution did not reach H3 dialogue compiler: dubs=%+v compiled=%s", dubs, compiled)
	}
}

func TestSceneStoryRewriteHTTPRejectsStaleAndForgedApplyAtomically(t *testing.T) {
	t.Run("stale scene", func(t *testing.T) {
		f := newStoryHTTPFixture(t)
		preview := f.preview(t)
		if err := f.db.Model(&models.Scene{}).Where("id = ?", f.scene.ID).Update("content", "并发人工修改").Error; err != nil {
			t.Fatal(err)
		}
		if w := f.post(t, "/apply", preview); w.Code != http.StatusConflict {
			t.Fatalf("stale apply status=%d body=%s", w.Code, w.Body.String())
		}
		var scene models.Scene
		var d models.Dialogue
		f.db.First(&scene, f.scene.ID)
		f.db.First(&d, f.dialogue.ID)
		if scene.Content != "并发人工修改" || d.Character != "" || d.SpeechType != "unknown" {
			t.Fatalf("stale apply partially persisted: scene=%+v dialogue=%+v", scene, d)
		}
	})

	for _, tc := range []struct {
		name string
		edit func(*sceneStoryApplyRequest)
	}{
		{name: "changed text", edit: func(p *sceneStoryApplyRequest) { p.Dialogues[0].Text = "伪造台词" }},
		{name: "missing", edit: func(p *sceneStoryApplyRequest) { p.Dialogues = nil }},
		{name: "reordered", edit: func(p *sceneStoryApplyRequest) {
			p.Dialogues = append(p.Dialogues, sceneStoryDialogue{ID: 999, Speaker: "伪造", SpeechType: "dialogue", Text: "伪造"})
			p.Dialogues[0], p.Dialogues[1] = p.Dialogues[1], p.Dialogues[0]
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newStoryHTTPFixture(t)
			preview := f.preview(t)
			tc.edit(&preview)
			w := f.post(t, "/apply", preview)
			if w.Code != http.StatusUnprocessableEntity {
				t.Fatalf("forged apply status=%d body=%s", w.Code, w.Body.String())
			}
			var scene models.Scene
			var d models.Dialogue
			f.db.First(&scene, f.scene.ID)
			f.db.First(&d, f.dialogue.ID)
			if scene.Content != "旧场景正文" || d.Character != "" || d.SpeechType != "unknown" {
				t.Fatalf("invalid apply partially persisted: scene=%+v dialogue=%+v", scene, d)
			}
		})
	}
}

func TestSceneStoryRewriteHTTPRejectsDeletionDisguisedAsSound(t *testing.T) {
	f := newStoryHTTPFixture(t)
	preview := f.preview(t)
	preview.Dialogues[0].SpeechType = "sound_effect"
	preview.Content = "女童只顾奔跑。"
	w := f.post(t, "/apply", preview)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("speech deletion disguised as sound accepted: %d %s", w.Code, w.Body.String())
	}
	var d models.Dialogue
	f.db.First(&d, f.dialogue.ID)
	if d.SpeechType != "unknown" {
		t.Fatal("failed validation mutated source dialogue")
	}
}

func TestSceneStoryRewriteHTTPRejectsChangeWithActiveAudioTask(t *testing.T) {
	f := newStoryHTTPFixture(t)
	if err := f.db.Model(&models.Dialogue{}).Where("id = ?", f.dialogue.ID).Updates(map[string]any{"status": "synthesizing", "audio_task_id": "in-flight-audio"}).Error; err != nil {
		t.Fatal(err)
	}
	preview := f.preview(t)
	w := f.post(t, "/apply", preview)
	if w.Code != http.StatusConflict {
		t.Fatalf("active audio ownership changed: %d %s", w.Code, w.Body.String())
	}
	var d models.Dialogue
	f.db.First(&d, f.dialogue.ID)
	if d.Character != "" || d.AudioTaskID != "in-flight-audio" {
		t.Fatal("active audio task changed")
	}
}

func TestSceneStoryRewriteHTTPRejectsSpeakerChangeWithReadyAudio(t *testing.T) {
	f := newStoryHTTPFixture(t)
	preview := f.preview(t)
	if err := f.db.Model(&models.Dialogue{}).Where("id = ?", f.dialogue.ID).Updates(map[string]any{"status": "ready", "audio_file": "keep.wav"}).Error; err != nil {
		t.Fatal(err)
	}
	// Refresh the source hash to exercise the explicit audio conflict, not
	// just rejection of a preview made before the audio lifecycle changed.
	chapters, err := f.s.sceneStorySourceChapters(&f.scene)
	if err != nil {
		t.Fatal(err)
	}
	var current []models.Dialogue
	if err := f.db.Where("scene_id = ?", f.scene.ID).Order("`order`, id").Find(&current).Error; err != nil {
		t.Fatal(err)
	}
	preview.SourceHash, err = sceneStorySourceHash(&f.scene, current, chapters)
	if err != nil {
		t.Fatal(err)
	}
	w := f.post(t, "/apply", preview)
	if w.Code != http.StatusConflict {
		t.Fatalf("audio conflict status=%d body=%s", w.Code, w.Body.String())
	}
	var scene models.Scene
	var d models.Dialogue
	f.db.First(&scene, f.scene.ID)
	f.db.First(&d, f.dialogue.ID)
	if scene.Content != "旧场景正文" || d.Character != "" || d.SpeechType != "unknown" || d.AudioFile != "keep.wav" || d.Status != "ready" {
		t.Fatalf("audio conflict partially persisted: scene=%+v dialogue=%+v", scene, d)
	}
}
