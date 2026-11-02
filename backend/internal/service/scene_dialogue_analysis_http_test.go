package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
)

func TestDialogueAnalysisMockHTTPPreservesLongSourceTextAndAppliesSafely(t *testing.T) {
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"items\":[{\"index\":0,\"speaker\":\"燕承\",\"addressee\":\"青年\",\"speech_type\":\"dialogue\",\"time_segment\":\"2—5秒\"},{\"index\":1,\"speaker\":\"待确认\",\"addressee\":\"柳乐儿\",\"speech_type\":\"offscreen_dialogue\",\"time_segment\":\"5—6秒\"}]}"}}]}`)
	}))
	defer ai.Close()
	s, db, scene := expandHTTPService(t, ai.URL)
	if err := db.AutoMigrate(&models.Dialogue{}); err != nil {
		t.Fatal(err)
	}
	long := "不知阁下如何称呼？在下血刀会燕承，正与两位道友联手捉拿这妖狐，阁下若是与此事无关，还请自行离去。"
	scene.Content = `2—5秒｜燕承对青年抱拳说道：“` + long + `”5—6秒｜虬髯大汉朝柳乐儿厉喝：“妖孽，哪里走！！！”`
	if err := db.Save(scene).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.Skills.InitSystemSkills(); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/projects/:id/scenes/:sid/dialogue/analyze", s.HandleAnalyzeSceneDialogue)
	r.POST("/api/projects/:id/scenes/:sid/dialogue/apply", s.HandleApplySceneDialogue)
	path := fmt.Sprintf("/api/projects/%d/scenes/%d/dialogue", scene.ProjectID, scene.ID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path+"/analyze", nil))
	if w.Code != 200 {
		t.Fatalf("analyze status=%d body=%s", w.Code, w.Body.String())
	}
	var response struct {
		Analysis SceneDialogueAnalysis `json:"analysis"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Analysis.Items) != 2 || response.Analysis.Items[0].Text != long || response.Analysis.Items[1].Text != "妖孽，哪里走！！！" {
		t.Fatalf("source text changed: %+v", response.Analysis.Items)
	}
	var afterPreview models.Scene
	if err := db.First(&afterPreview, scene.ID).Error; err != nil || afterPreview.DialogueAnalysisJSON != "" {
		t.Fatalf("analyze preview persisted: json=%q err=%v", afterPreview.DialogueAnalysisJSON, err)
	}
	if !strings.Contains(w.Body.String(), `"persisted":false`) {
		t.Fatalf("preview response did not declare non-persistence: %s", w.Body.String())
	}
	body, _ := json.Marshal(response.Analysis)
	w = httptest.NewRecorder()
	q := httptest.NewRequest(http.MethodPost, path+"/apply", bytes.NewReader(body))
	q.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, q)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"applied":2`) {
		t.Fatalf("apply status=%d body=%s", w.Code, w.Body.String())
	}
	var dialogues []models.Dialogue
	db.Where("project_id=? AND scene_id=?", scene.ProjectID, scene.ID).Order("`order`,id").Find(&dialogues)
	if len(dialogues) != 2 || dialogues[0].Text != long || dialogues[0].Character != "燕承" || dialogues[0].Source != "ai_reviewed" || dialogues[0].TimeSegment != "2—5秒" {
		t.Fatalf("dialogues=%+v", dialogues)
	}
	if dialogues[1].Text != "妖孽，哪里走！！！" || dialogues[1].Character != "待确认" {
		t.Fatalf("unknown speaker caused source dialogue loss: %+v", dialogues[1])
	}
}

func TestDialogueAnalysisApplyRegenerateTranslateAndGetPersistsCompleteFL2VADialoguePrompt(t *testing.T) {
	var calls int
	var translationReply string
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"items\":[{\"index\":0,\"speaker\":\"燕承\",\"addressee\":\"青年\",\"speech_type\":\"dialogue\",\"time_segment\":\"0—5秒\"},{\"index\":1,\"speaker\":\"待确认\",\"addressee\":\"燕承\",\"speech_type\":\"offscreen_dialogue\",\"time_segment\":\"5—10秒\"}]}"}}]}`)
			return
		}
		if calls == 2 {
			fmt.Fprint(w, `{"choices":[{"message":{"content":"[Shot 1] The young man faces 燕承 as the camera slowly pushes in. At 00:05.000, cut to [Shot 2], where both visible people turn toward an unidentified voice from outside the frame."}}]}`)
			return
		}
		fmt.Fprintf(w, `{"choices":[{"message":{"content":%q}}]}`, translationReply)
	}))
	defer ai.Close()

	s, db, scene := expandHTTPService(t, ai.URL)
	if err := db.AutoMigrate(&models.Template{}, &models.Episode{}, &models.PromptPolicyOverride{}, &models.GenerationCandidate{}); err != nil {
		t.Fatal(err)
	}
	factory := s.TextProviderFact
	s.Projects = &ProjectService{db: db, textProvider: factory}
	if err := s.Skills.InitSystemSkills(); err != nil {
		t.Fatal(err)
	}
	known, unknown := "不知阁下如何称呼？", "妖孽，哪里走！！！"
	scene.Content = `0—5秒｜燕承对青年说道：“` + known + `”5—10秒｜画外有人厉喝：“` + unknown + `”`
	scene.Duration, scene.VideoTemplate = 10, "minimax_h3_first_last"
	scene.VideoFirstFrameImg, scene.VideoLastFrameImg = "first.png", "last.png"
	scene.VideoFullPrompt, scene.ImageFile, scene.VideoFile, scene.VideoInputFile = "old full without dialogue", "keep.png", "keep.mp4", "keep-input.mp4"
	if err := db.Save(&scene).Error; err != nil {
		t.Fatal(err)
	}
	for _, shot := range []models.Shot{
		{SceneID: scene.ID, Order: 1, Duration: 5, Description: "conversation", DialogueRanges: []models.ShotDialogueRange{{DialogueID: 901, GroupKey: "old", StartRune: 0, EndRune: 2}}},
		{SceneID: scene.ID, Order: 2, Duration: 5, Description: "reaction", DialogueRanges: []models.ShotDialogueRange{{DialogueID: 902, GroupKey: "old", StartRune: 0, EndRune: 2}}},
	} {
		if err := db.Create(&shot).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&models.Template{Name: "FL2VA", Code: "minimax_h3_first_last", WorkflowJSON: "{}", InputsJSON: "{}", Enabled: true}).Error; err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	base := "/api/projects/:id/scenes/:sid"
	r.POST(base+"/dialogue/analyze", s.HandleAnalyzeSceneDialogue)
	r.POST(base+"/dialogue/apply", s.HandleApplySceneDialogue)
	r.POST(base+"/video/prompt/regenerate", s.HandleRegenerateSceneVideoPrompt)
	r.POST(base+"/video-prompt/translate", s.HandleTranslateSceneVideoPrompt)
	r.GET(base+"/video-prompt", s.HandleGetSceneVideoPrompt)
	r.PUT(base+"/video-prompt", s.HandleUpdateSceneVideoPrompt)
	path := fmt.Sprintf("/api/projects/%d/scenes/%d", scene.ProjectID, scene.ID)
	do := func(method, suffix string, body []byte) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		q := httptest.NewRequest(method, path+suffix, bytes.NewReader(body))
		q.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, q)
		return w
	}
	analyzed := do(http.MethodPost, "/dialogue/analyze", nil)
	if analyzed.Code != 200 {
		t.Fatalf("analyze status=%d body=%s", analyzed.Code, analyzed.Body.String())
	}
	var preview struct {
		Analysis SceneDialogueAnalysis `json:"analysis"`
	}
	if err := json.Unmarshal(analyzed.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(preview.Analysis)
	if w := do(http.MethodPost, "/dialogue/apply", body); w.Code != 200 {
		t.Fatalf("apply status=%d body=%s", w.Code, w.Body.String())
	}
	regen := do(http.MethodPost, "/video/prompt/regenerate", []byte(`{"template":"minimax_h3_first_last"}`))
	if regen.Code != 200 {
		t.Fatalf("regenerate status=%d body=%s", regen.Code, regen.Body.String())
	}
	var generated map[string]any
	if err := json.Unmarshal(regen.Body.Bytes(), &generated); err != nil {
		t.Fatal(err)
	}
	prompt, _ := generated["full_prompt"].(string)
	for _, want := range []string{"integrated_multimodal_description:", "[Shot 1]", "[Shot 2]", "00:05", "<d>[Chinese] " + known + "</d>", "<d>[Chinese] " + unknown + "</d>", "燕承", "待确认"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("regenerated prompt missing %q: %s", want, prompt)
		}
	}
	if !strings.Contains(prompt, "待确认 (S2) says in a clearly identified off-screen voice") || strings.Contains(prompt, "<Subject 1> (S2)") || strings.Contains(prompt, "<Subject 2> (S2)") || strings.Contains(strings.ToLower(prompt), "no dialogue") {
		t.Fatalf("unknown speaker was not preserved as unbound off-screen speech: %s", prompt)
	}
	// Regeneration must expose a reviewable preview, not overwrite the saved execution.
	var beforeSave models.Scene
	if err := db.First(&beforeSave, scene.ID).Error; err != nil {
		t.Fatal(err)
	}
	if beforeSave.VideoFullPrompt != "old full without dialogue" {
		t.Fatal("preview overwrote saved execution")
	}
	masked, _ := maskH3Structure(prompt)
	translationReply = masked
	translateBody, _ := json.Marshal(map[string]any{"prompt": prompt, "direction": "to_zh", "template": "minimax_h3_first_last"})
	translatedResponse := do(http.MethodPost, "/video-prompt/translate", translateBody)
	if translatedResponse.Code != 200 {
		t.Fatalf("translate status=%d body=%s", translatedResponse.Code, translatedResponse.Body.String())
	}
	var translated map[string]any
	if err := json.Unmarshal(translatedResponse.Body.Bytes(), &translated); err != nil {
		t.Fatal(err)
	}
	translatedPrompt, _ := translated["preview"].(string)
	for _, want := range []string{"<d>[Chinese] " + known + "</d>", "<d>[Chinese] " + unknown + "</d>"} {
		if !strings.Contains(translatedPrompt, want) {
			t.Fatalf("to_zh preview lost dialogue %q: %s", want, translatedPrompt)
		}
	}
	saveBody, _ := json.Marshal(map[string]any{"prompt": translatedPrompt, "template": "minimax_h3_first_last"})
	if savedResponse := do(http.MethodPut, "/video-prompt", saveBody); savedResponse.Code != 200 {
		t.Fatalf("save status=%d body=%s", savedResponse.Code, savedResponse.Body.String())
	}
	got := do(http.MethodGet, "/video-prompt", nil)
	if got.Code != 200 {
		t.Fatalf("get status=%d body=%s", got.Code, got.Body.String())
	}
	var persisted map[string]any
	_ = json.Unmarshal(got.Body.Bytes(), &persisted)
	if persisted["full_prompt"] != translatedPrompt {
		t.Fatalf("GET full prompt not persisted: got=%q want=%q", persisted["full_prompt"], translatedPrompt)
	}
	var saved models.Scene
	if err := db.First(&saved, scene.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.ImageFile != "keep.png" || saved.VideoFile != "keep.mp4" || saved.VideoInputFile != "keep-input.mp4" {
		t.Fatalf("media changed: %+v", saved)
	}
}

func TestApplyDialogueAnalysisKeepsRepeatedLinesAndMedia(t *testing.T) {
	s, db, scene := expandHTTPService(t, "http://unused.invalid")
	if err := db.AutoMigrate(&models.Dialogue{}); err != nil {
		t.Fatal(err)
	}
	scene.Content = `甲说道：“等等！”乙答道：“等等！”`
	scene.ImageFile, scene.VideoFile, scene.VideoFullPrompt = "keep.png", "keep.mp4", "saved execution"
	if err := db.Save(scene).Error; err != nil {
		t.Fatal(err)
	}
	shot := models.Shot{SceneID: scene.ID, Order: 1, Dialogue: "等等！", PromptAction: "keep-shot-action", DialogueRanges: []models.ShotDialogueRange{{DialogueID: 999, GroupKey: "stale", StartRune: 0, EndRune: 3}}}
	if err := db.Create(&shot).Error; err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.POST("/api/projects/:id/scenes/:sid/dialogue/apply", s.HandleApplySceneDialogue)
	analysis := SceneDialogueAnalysis{Items: []SceneDialogueAnalysisItem{{Index: 0, Text: "等等！", Speaker: "甲", SpeechType: "dialogue"}, {Index: 1, Text: "等等！", Speaker: "乙", SpeechType: "dialogue"}}}
	body, _ := json.Marshal(analysis)
	request := func(project uint) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		q := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/projects/%d/scenes/%d/dialogue/apply", project, scene.ID), bytes.NewReader(body))
		q.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, q)
		return w
	}
	if w := request(scene.ProjectID + 99); w.Code != 404 {
		t.Fatalf("ownership status=%d", w.Code)
	}
	for i := 0; i < 2; i++ {
		if w := request(scene.ProjectID); w.Code != 200 {
			t.Fatalf("apply=%s", w.Body.String())
		}
	}
	var rows []models.Dialogue
	if err := db.Where("scene_id=?", scene.ID).Order("`order`,id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Character != "甲" || rows[1].Character != "乙" {
		t.Fatalf("repeated lines collapsed or duplicated: %+v", rows)
	}
	var saved models.Scene
	if err := db.First(&saved, scene.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.ImageFile != "keep.png" || saved.VideoFile != "keep.mp4" || saved.VideoFullPrompt != "saved execution" {
		t.Fatal("analysis application altered media or saved prompt")
	}
	var savedShot models.Shot
	if err := db.First(&savedShot, shot.ID).Error; err != nil {
		t.Fatal(err)
	}
	if len(savedShot.DialogueRanges) != 0 || savedShot.PromptAction != "keep-shot-action" || savedShot.Dialogue != "等等！" {
		t.Fatalf("stale ranges not invalidated without preserving shot/audio: %+v", savedShot)
	}
}

func TestExtractQuotedDialogueKeepsPunctuationAndUnknownOnBadAI(t *testing.T) {
	items := extractQuotedDialogue(`石碑刻着“太虚门”，虬髯大汉说：“住手？！ ”`)
	if len(items) != 2 || items[0].Text != "太虚门" || items[1].Text != "住手？！ " || !items[1].NeedsReview {
		t.Fatalf("items=%+v", items)
	}
	got, err := parseDialogueAI(`{"items":[{"index":1,"speaker":"","text":"被AI篡改"}]}`, items)
	if err != nil || got[1].Text != "住手？！ " || got[1].Speaker != "待确认" {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}
