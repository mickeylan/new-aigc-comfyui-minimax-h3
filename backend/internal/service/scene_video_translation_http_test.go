package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const translationMaster = "subject_definitions:\n无参考主体。\n\nsummary:\n人物走过空房间。\n\nretention_analysis:\n无。\n\ndetailed_description:\n[Shot 1] 人物立即以正常速度走过空房间。\n\noverall_soundscape:\n安静的室内环境声，无对白。\n\nnon_diegetic_music:\nN/A"
const translationEnglish = "subject_definitions:\nNo reference subjects.\n\nsummary:\nA person crosses an empty room.\n\nretention_analysis:\nNone.\n\ndetailed_description:\n[Shot 1] A person immediately crosses an empty room at normal speed.\n\noverall_soundscape:\nQuiet indoor ambience, no dialogue.\n\nnon_diegetic_music:\nN/A"

type translationHTTPFixture struct {
	svc         *Service
	db          *gorm.DB
	p           models.Project
	sc          models.Scene
	ai          *httptest.Server
	calls       atomic.Int32
	beforeReply func()
	response    string
}

func newTranslationHTTPFixture(t *testing.T) *translationHTTPFixture {
	t.Helper()
	f := &translationHTTPFixture{}
	f.ai = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" {
			t.Errorf("upstream %s %s", r.Method, r.URL.Path)
		}
		if f.beforeReply != nil {
			f.beforeReply()
		}
		response := f.response
		if response == "" {
			response = translationEnglish
		}
		// The real provider receives and returns the masked protocol, not the raw H3 prompt.
		response, _ = maskH3Structure(response)
		fmt.Fprintf(w, `{"choices":[{"message":{"content":%q}}]}`, response)
	}))
	t.Cleanup(f.ai.Close)
	f.db = newTestDB(t)
	if err := f.db.AutoMigrate(&models.Project{}, &models.Scene{}, &models.Shot{}, &models.Dialogue{}, &models.Template{}, &models.Character{}, &models.Asset{}, &models.SceneContinuity{}, &models.UploadFile{}); err != nil {
		t.Fatal(err)
	}
	f.p = models.Project{Title: "translation", Style: "cinematic", Genre: "drama", AspectRatio: "16:9"}
	if err := f.db.Create(&f.p).Error; err != nil {
		t.Fatal(err)
	}
	f.sc = models.Scene{ProjectID: f.p.ID, EpisodeN: 1, Order: 1, Content: "walk", Duration: 5, VideoTemplate: "minimax_h3_t2v", VideoPromptMasterZH: translationMaster, VideoFullPrompt: "old reviewed prompt", VideoFile: "old.mp4", VideoInputFile: "old-input.mp4"}
	if err := f.db.Create(&f.sc).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Create(&models.Shot{SceneID: f.sc.ID, Order: 1, Duration: 5, Description: "walk"}).Error; err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"minimax_h3_t2v", "minimax_h3_ref2v"} {
		if err := f.db.Create(&models.Template{Name: code, Code: code, WorkflowJSON: "{}", InputsJSON: "{}", Enabled: true}).Error; err != nil {
			t.Fatal(err)
		}
	}
	projects := &ProjectService{db: f.db}
	factory := NewTextProviderFactory(&mockGetter{settings: map[string]string{SettingTextProvider: "llama", SettingLlamaBaseURL: f.ai.URL, SettingLlamaModel: "mock"}})
	f.svc = &Service{DB: f.db, Projects: projects, TextProviderFact: factory}
	return f
}

func translationRequest(s *Service, method string, pid, sid uint, action, body string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	base := "/api/projects/:id/scenes/:sid/video-prompt"
	r.PUT(base+"/master", s.HandlePutSceneVideoPromptMaster)
	r.POST(base+"/translate", s.HandleTranslateSceneVideoPrompt)
	r.POST(base+"/confirm", s.HandleConfirmSceneVideoPrompt)
	r.GET(base, s.HandleGetSceneVideoPrompt)
	path := fmt.Sprintf("/api/projects/%d/scenes/%d/video-prompt%s", pid, sid, action)
	q := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	q.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, q)
	return w
}

func decodeTranslationBody(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode status=%d body=%s: %v", w.Code, w.Body.String(), err)
	}
	return v
}

func TestSceneVideoTranslationHTTPMasterTranslateConfirmStableAndInvalidate(t *testing.T) {
	f := newTranslationHTTPFixture(t)
	empty := ""
	putBody, _ := json.Marshal(map[string]any{"master_prompt_zh": translationMaster, "first_frame_img": empty, "last_frame_img": empty})
	w := translationRequest(f.svc, http.MethodPut, f.p.ID, f.sc.ID, "/master", string(putBody))
	if w.Code != 200 {
		t.Fatalf("put status=%d %s", w.Code, w.Body.String())
	}
	var saved models.Scene
	f.db.First(&saved, f.sc.ID)
	if saved.VideoFullPrompt != "old reviewed prompt" || saved.VideoFile != "old.mp4" || saved.VideoInputFile != "old-input.mp4" || saved.VideoFirstFrameImg != "" {
		t.Fatalf("PUT damaged reviewed/media fields: %+v", saved)
	}

	w = translationRequest(f.svc, http.MethodPost, f.p.ID, f.sc.ID, "/translate", `{"language":"en"}`)
	if w.Code != 200 {
		t.Fatalf("translate status=%d %s", w.Code, w.Body.String())
	}
	out := decodeTranslationBody(t, w)
	sourceHash, _ := out["source_hash"].(string)
	if out["prompt"] != translationEnglish || sourceHash == "" || f.calls.Load() != 1 {
		t.Fatalf("translate=%v calls=%d", out, f.calls.Load())
	}
	if err := validateH3Translation(translationMaster, out["prompt"].(string)); err != nil {
		t.Fatalf("English did not preserve exact protocol/dialogue gate: %v", err)
	}

	confirm, _ := json.Marshal(map[string]any{"exec_prompt": translationEnglish, "source_hash": sourceHash})
	w = translationRequest(f.svc, http.MethodPost, f.p.ID, f.sc.ID, "/confirm", string(confirm))
	if w.Code != 200 {
		t.Fatalf("confirm status=%d %s", w.Code, w.Body.String())
	}
	for i := 0; i < 2; i++ {
		w = translationRequest(f.svc, http.MethodGet, f.p.ID, f.sc.ID, "", "")
		got := decodeTranslationBody(t, w)
		if w.Code != 200 || got["exec_status"] != "confirmed" || got["source_hash"] != sourceHash || got["current_source_hash"] != sourceHash {
			t.Fatalf("unstable GET %d: %v", i, got)
		}
	}
	f.db.First(&saved, f.sc.ID)
	if err := f.db.Model(&models.Scene{}).Where("id = ?", f.sc.ID).Update("content", "changed after confirmation").Error; err != nil {
		t.Fatal(err)
	}
	f.db.First(&saved, f.sc.ID)
	if err := f.svc.Projects.GenerateSceneVideo(&f.p, &saved); err != nil && strings.Contains(err.Error(), "尚未翻译并确认") {
		t.Fatalf("source drift still blocked generation: %v", err)
	}
	w = translationRequest(f.svc, http.MethodGet, f.p.ID, f.sc.ID, "", "")
	if got := decodeTranslationBody(t, w); got["exec_status"] != "confirmed" {
		t.Fatalf("source drift revoked saved English prompt: %v", got)
	}
}

func TestSceneVideoTranslationHTTPConfirmRepairsSpacedInternalMarkers(t *testing.T) {
	f := newTranslationHTTPFixture(t)
	masked, _ := maskH3Structure(translationMaster)
	malformed := strings.Replace(masked, "[[[T1]]]", "[ [[T1]]]", 1)
	confirm, _ := json.Marshal(map[string]any{"exec_prompt": malformed})
	w := translationRequest(f.svc, http.MethodPost, f.p.ID, f.sc.ID, "/confirm", string(confirm))
	if w.Code != http.StatusOK {
		t.Fatalf("confirm status=%d body=%s", w.Code, w.Body.String())
	}
	var saved models.Scene
	if err := f.db.First(&saved, f.sc.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.VideoFullPrompt != translationMaster || h3MarkerRE.MatchString(saved.VideoFullPrompt) {
		t.Fatalf("internal marker leaked into saved prompt: %q", saved.VideoFullPrompt)
	}
}

func TestSceneVideoTranslationHTTPBrokenMarkersReturnNonBlockingPreview(t *testing.T) {
	f := newTranslationHTTPFixture(t)
	masked, _ := maskH3Structure(translationEnglish)
	f.response = strings.Replace(masked, "[[[T1]]]", "[[[T99]]]", 1)

	w := translationRequest(f.svc, http.MethodPost, f.p.ID, f.sc.ID, "/translate", `{"language":"en"}`)
	if w.Code != http.StatusOK || f.calls.Load() != 1 {
		t.Fatalf("status=%d calls=%d body=%s", w.Code, f.calls.Load(), w.Body.String())
	}
	out := decodeTranslationBody(t, w)
	if out["prompt"] != translationEnglish || out["warnings"] == nil {
		t.Fatalf("broken markers were not repaired non-blockingly: %v", out)
	}
	var saved models.Scene
	if err := f.db.First(&saved, f.sc.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.VideoFullPrompt != "old reviewed prompt" || saved.VideoPromptMasterZH != translationMaster {
		t.Fatalf("failed translation persisted data: %+v", saved)
	}
}

func TestSceneVideoTranslationHTTPReturnsPreviewDespiteSourceChange(t *testing.T) {
	f := newTranslationHTTPFixture(t)
	before := f.sc
	originalHash, err := f.svc.sceneVideoPromptSourceHash(&before)
	if err != nil {
		t.Fatal(err)
	}
	f.beforeReply = func() {
		f.beforeReply = nil
		if err := f.db.Model(&models.Scene{}).Where("id = ?", f.sc.ID).Update("content", "changed concurrently").Error; err != nil {
			t.Error(err)
		}
	}
	w := translationRequest(f.svc, http.MethodPost, f.p.ID, f.sc.ID, "/translate", `{"language":"en"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("concurrent source change discarded preview status=%d %s", w.Code, w.Body.String())
	}
	out := decodeTranslationBody(t, w)
	if out["prompt"] != translationEnglish || out["source_hash"] != originalHash || f.calls.Load() != 1 {
		t.Fatalf("preview must retain request snapshot without retries: %v calls=%d", out, f.calls.Load())
	}
	var saved models.Scene
	if err := f.db.First(&saved, f.sc.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.Content != "changed concurrently" || saved.VideoFullPrompt != "old reviewed prompt" {
		t.Fatal("translation overwrote concurrent edits or the execution prompt")
	}
}

func TestSceneVideoTranslationHTTPConfirmDoesNotBlockChangedSource(t *testing.T) {
	f := newTranslationHTTPFixture(t)
	w := translationRequest(f.svc, http.MethodPost, f.p.ID, f.sc.ID, "/translate", `{"language":"en"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("translate status=%d %s", w.Code, w.Body.String())
	}
	oldHash := decodeTranslationBody(t, w)["source_hash"].(string)
	if err := f.db.Model(&models.Scene{}).Where("id = ?", f.sc.ID).Updates(map[string]any{
		"content":                "changed while user reviews English",
		"video_prompt_master_zh": translationMaster + "\n用户随后补充",
	}).Error; err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"exec_prompt": translationEnglish, "source_hash": oldHash})
	w = translationRequest(f.svc, http.MethodPost, f.p.ID, f.sc.ID, "/confirm", string(body))
	if w.Code != http.StatusOK {
		t.Fatalf("changed source blocked English save status=%d %s", w.Code, w.Body.String())
	}
	var saved models.Scene
	if err := f.db.First(&saved, f.sc.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.VideoFullPrompt != translationEnglish || saved.PromptStale {
		t.Fatalf("English prompt was not saved against current source: %+v", saved)
	}
}

func TestSceneVideoTranslationHTTPValidationIsolationAndNilProvider(t *testing.T) {
	f := newTranslationHTTPFixture(t)
	if w := translationRequest(f.svc, http.MethodPost, f.p.ID+99, f.sc.ID, "/translate", `{"language":"en"}`); w.Code != 404 {
		t.Fatalf("cross project=%d", w.Code)
	}
	nilSvc := *f.svc
	nilSvc.TextProviderFact = nil
	if w := translationRequest(&nilSvc, http.MethodPost, f.p.ID, f.sc.ID, "/translate", `{"language":"en"}`); w.Code != 503 {
		t.Fatalf("nil provider=%d %s", w.Code, w.Body.String())
	}
	w := translationRequest(f.svc, http.MethodPost, f.p.ID, f.sc.ID, "/translate", `{"language":"en"}`)
	h := decodeTranslationBody(t, w)["source_hash"].(string)
	for _, prompt := range []string{strings.Replace(translationEnglish, "[Shot 1]", "[Shot 2]", 1), strings.Replace(translationEnglish, "no dialogue", "<d>changed</d>", 1)} {
		b, _ := json.Marshal(map[string]any{"exec_prompt": prompt, "source_hash": h})
		if x := translationRequest(f.svc, http.MethodPost, f.p.ID, f.sc.ID, "/confirm", string(b)); x.Code != http.StatusOK {
			t.Fatalf("user-edited execution prompt was blocked status=%d %s", x.Code, x.Body.String())
		}
	}
	other := models.Project{Title: "other"}
	f.db.Create(&other)
	otherScene := models.Scene{ProjectID: other.ID, EpisodeN: 1, Order: 1, Duration: 5, VideoTemplate: "minimax_h3_ref2v", VideoPromptMasterZH: translationMaster, VideoFullPrompt: "other-old"}
	f.db.Create(&otherScene)
	body, _ := json.Marshal(map[string]any{"master_prompt_zh": translationMaster, "template": "minimax_h3_t2v"})
	if x := translationRequest(f.svc, http.MethodPut, other.ID, otherScene.ID, "/master", string(body)); x.Code != 200 {
		t.Fatalf("other draft=%d %s", x.Code, x.Body.String())
	}
	var original models.Scene
	f.db.First(&original, f.sc.ID)
	lastConfirmed := strings.Replace(translationEnglish, "no dialogue", "<d>changed</d>", 1)
	if original.VideoTemplate != "minimax_h3_t2v" || original.VideoFullPrompt != lastConfirmed {
		t.Fatalf("other project draft affected original: %+v", original)
	}
}
