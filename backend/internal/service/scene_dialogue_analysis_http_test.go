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
	var requests []struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode AI request: %v", err)
		}
		requests = append(requests, request)
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
	if len(requests) < 2 || len(requests[1].Messages) < 2 {
		t.Fatalf("missing captured generation request: %+v", requests)
	}
	generationUser := requests[1].Messages[len(requests[1].Messages)-1].Content
	for _, want := range []string{"【结构化对白", "燕承：" + known, known, "待确认：" + unknown} {
		if !strings.Contains(generationUser, want) {
			t.Fatalf("generation request body missing %q; user message=%s", want, generationUser)
		}
	}
	var generated map[string]any
	if err := json.Unmarshal(regen.Body.Bytes(), &generated); err != nil {
		t.Fatal(err)
	}
	prompt, _ := generated["full_prompt"].(string)
	if generated["dialogue_count"] != float64(2) || generated["prompt_dialogue_count"] != float64(2) {
		t.Fatalf("regenerate dialogue counts=%v/%v body=%v", generated["dialogue_count"], generated["prompt_dialogue_count"], generated)
	}
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

func TestDialogueAnalysisReviewedJSONFallbackReachesGenerationRequest(t *testing.T) {
	for _, tc := range []struct {
		name        string
		badDialogue bool
	}{
		{name: "dialogue row missing"},
		{name: "dialogue row has invalid type", badDialogue: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var generationUser string
			ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					Messages []struct {
						Role    string `json:"role"`
						Content string `json:"content"`
					} `json:"messages"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Errorf("decode AI request: %v", err)
				}
				if len(request.Messages) > 0 && generationUser == "" {
					generationUser = request.Messages[len(request.Messages)-1].Content
				}
				fmt.Fprint(w, `{"choices":[{"message":{"content":"[Shot 1] 燕承 faces the young man and speaks while the camera slowly pushes in."}}]}`)
			}))
			defer ai.Close()

			s, db, scene := expandHTTPService(t, ai.URL)
			if err := db.AutoMigrate(&models.Dialogue{}, &models.Template{}, &models.Episode{}, &models.PromptPolicyOverride{}, &models.GenerationCandidate{}); err != nil {
				t.Fatal(err)
			}
			s.Projects = &ProjectService{db: db, textProvider: s.TextProviderFact}
			original := "不知阁下如何称呼？在下血刀会燕承，正与两位道友联手捉拿这妖狐，阁下若是与此事无关，还请自行离去。"
			scene.Content = `燕承对青年说道：“` + original + `”`
			scene.Duration, scene.VideoTemplate = 5, "minimax_h3_first_last"
			analysis := SceneDialogueAnalysis{Source: "ai_reviewed", Items: []SceneDialogueAnalysisItem{{Index: 0, Text: original, Speaker: "燕承", Addressee: "青年", SpeechType: "dialogue"}}}
			raw, _ := json.Marshal(analysis)
			scene.DialogueAnalysisJSON = string(raw)
			if err := db.Save(scene).Error; err != nil {
				t.Fatal(err)
			}
			if tc.badDialogue {
				if err := db.Create(&models.Dialogue{ProjectID: scene.ProjectID, SceneID: scene.ID, Order: 1, Character: "错误角色", Text: original, SpeechType: "wrong", Source: "ai_reviewed"}).Error; err != nil {
					t.Fatal(err)
				}
			}

			gin.SetMode(gin.TestMode)
			r := gin.New()
			r.POST("/api/projects/:id/scenes/:sid/video/prompt/regenerate", s.HandleRegenerateSceneVideoPrompt)
			path := fmt.Sprintf("/api/projects/%d/scenes/%d/video/prompt/regenerate", scene.ProjectID, scene.ID)
			w := httptest.NewRecorder()
			q := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"template":"minimax_h3_first_last"}`))
			q.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, q)
			if w.Code != http.StatusOK {
				t.Fatalf("regenerate status=%d body=%s", w.Code, w.Body.String())
			}
			for _, want := range []string{"【结构化对白", "燕承：" + original} {
				if !strings.Contains(generationUser, want) {
					t.Fatalf("captured provider user missing %q: %s", want, generationUser)
				}
			}
			if strings.Contains(generationUser, "错误角色："+original) {
				t.Fatalf("invalid Dialogue row overrode reviewed analysis: %s", generationUser)
			}
		})
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

func TestMergeExtractedCharacterSpeechKeepsReviewedAttribution(t *testing.T) {
	reviewed := models.Dialogue{Text: "别动。", Character: "燕承", SpeechType: "offscreen_dialogue", Source: "ai_reviewed"}
	got := mergeExtractedCharacterSpeech([]models.Dialogue{reviewed}, []models.Dialogue{{Text: "别动。", Character: "错误角色", SpeechType: "dialogue"}})
	if len(got) != 1 || got[0].Character != "燕承" || got[0].SpeechType != "offscreen_dialogue" {
		t.Fatalf("reviewed attribution overwritten: %+v", got)
	}
}

func TestMergeSavedReviewedSceneDialogueRecoversOnlyCurrentReviewedSource(t *testing.T) {
	content := `燕承说道：“完整原文。”又说道：“重复。”随后再说：“重复。”画外传来：“未知。”`
	analysis := SceneDialogueAnalysis{Source: "ai_reviewed", Items: []SceneDialogueAnalysisItem{
		{Index: 0, Text: "完整原文。", Speaker: "燕承", SpeechType: ""},
		{Index: 1, Text: "重复。", Speaker: "错误归属", SpeechType: "wrong"},
		{Index: 2, Text: "重复。", Speaker: "燕承", SpeechType: "dialogue"},
		{Index: 3, Text: "未知。", Speaker: "未知", SpeechType: "dialogue"},
	}}
	raw, _ := json.Marshal(analysis)
	sc := &models.Scene{ID: 7, ProjectID: 9, Content: content, DialogueAnalysisJSON: string(raw)}
	existing := []models.Dialogue{{ID: 3, SceneID: 7, Text: "重复。", Character: "权威角色", SpeechType: "dialogue", AudioFile: "keep.wav", Source: "ai_reviewed"}}
	got := mergeSavedReviewedSceneDialogue(sc, existing)
	if len(got) != 4 {
		t.Fatalf("duplicate source lines collapsed or missing: %+v", got)
	}
	if got[0].Character != "权威角色" || got[0].AudioFile != "keep.wav" {
		t.Fatalf("persisted dialogue was overwritten: %+v", got[0])
	}
	for _, index := range []int{1, 2} {
		if got[index].Source != "ai_reviewed" || got[index].SpeechType != "dialogue" {
			t.Fatalf("invalid speech type was not safely normalized: %+v", got[index])
		}
	}
	if got[3].Character != "待确认" || got[3].SpeechType != "offscreen_dialogue" {
		t.Fatalf("unknown speaker was not made reviewable offscreen speech: %+v", got[3])
	}

	stale := *sc
	stale.Content = `燕承说道：“新正文。”`
	if recovered := mergeSavedReviewedSceneDialogue(&stale, nil); len(recovered) != 0 {
		t.Fatalf("stale reviewed text was restored: %+v", recovered)
	}
	preview := *sc
	preview.DialogueAnalysisJSON = strings.Replace(string(raw), `"ai_reviewed"`, `"ai_preview"`, 1)
	if recovered := mergeSavedReviewedSceneDialogue(&preview, nil); len(recovered) != 0 {
		t.Fatalf("preview analysis was treated as saved review: %+v", recovered)
	}
}

func TestNormalizeSavedH3AudioRecoveredDialogueRemovesSilentConflict(t *testing.T) {
	prompt := "integrated_multimodal_description:\n[Shot 1] 燕承抬头。 There is no structured Dialogue: all visible characters keep their lips closed; remove every mouth-opening, preparing-to-speak, shouting, calling, or speaking cue.\n\noverall_soundscape:\nNo dialogue, human voice.\n\nnon_diegetic_music:\nN/A"
	got := normalizeSavedH3Audio(prompt, []models.Dialogue{{Character: "燕承", SpeechType: "dialogue", Text: "完整原文。", Source: "ai_reviewed"}}, nil)
	if !strings.Contains(got, "<d>[Chinese] 完整原文。</d>") || strings.Contains(got, "No dialogue") || strings.Contains(got, "all visible characters keep their lips closed") {
		t.Fatalf("recovered FL2VA audio contract conflicts with dialogue: %s", got)
	}
}

func TestDialogueExclusionRepeatedOccurrenceRestoreAndMediaSafety(t *testing.T) {
	s, db, scene := expandHTTPService(t, "http://unused.invalid")
	if err := db.AutoMigrate(&models.Dialogue{}); err != nil {
		t.Fatal(err)
	}
	s.Projects = &ProjectService{db: db}
	scene.Content = `甲说：“重复。”乙说：“重复。”`
	scene.ImageFile, scene.VideoFile, scene.VideoFullPrompt = "keep.png", "keep.mp4", "keep prompt"
	if err := db.Save(scene).Error; err != nil {
		t.Fatal(err)
	}
	rows := []models.Dialogue{
		{ProjectID: scene.ProjectID, SceneID: scene.ID, Order: 1, Character: "甲", Text: "重复。", SpeechType: "dialogue", Source: "ai_reviewed", AudioFile: "one.wav", Status: "ready"},
		{ProjectID: scene.ProjectID, SceneID: scene.ID, Order: 2, Character: "乙", Text: "重复。", SpeechType: "dialogue", Source: "ai_reviewed", AudioFile: "two.wav", Status: "ready"},
	}
	for i := range rows {
		if err := db.Create(&rows[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	r := gin.New()
	r.POST("/api/projects/:id/scenes/:sid/dialogue/apply", s.HandleApplySceneDialogue)
	r.POST("/api/projects/:id/scenes/:sid/dialogue/analyze", s.HandleAnalyzeSceneDialogue)
	apply := func(items []SceneDialogueAnalysisItem) {
		body, _ := json.Marshal(SceneDialogueAnalysis{Items: items, ContentHash: sceneDialogueContentHash(scene.Content)})
		w := httptest.NewRecorder()
		q := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/projects/%d/scenes/%d/dialogue/apply", scene.ProjectID, scene.ID), bytes.NewReader(body))
		q.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, q)
		if w.Code != 200 {
			t.Fatalf("apply: %s", w.Body.String())
		}
	}
	items := []SceneDialogueAnalysisItem{{Index: 0, Text: "重复。", Speaker: "甲", SpeechType: "dialogue", Excluded: true}, {Index: 1, Text: "重复。", Speaker: "乙", SpeechType: "dialogue"}}
	apply(items)
	var got []models.Dialogue
	db.Where("scene_id=?", scene.ID).Order("`order`").Find(&got)
	if len(got) != 2 || got[0].Source != "user_excluded" || got[0].AudioFile != "one.wav" || got[1].Source == "user_excluded" {
		t.Fatalf("occurrence/media mismatch: %+v", got)
	}
	var current models.Scene
	db.First(&current, scene.ID)
	video := s.Projects.sceneVideoDialogues(&current)
	if len(video) != 1 || video[0].Character != "乙" {
		t.Fatalf("excluded dialogue reached video: %+v", video)
	}
	items[0].Excluded = false
	apply(items)
	db.First(&current, scene.ID)
	video = s.Projects.sceneVideoDialogues(&current)
	if len(video) != 2 {
		t.Fatalf("restore failed: %+v", video)
	}
	db.Order("`order`").Where("scene_id=?", scene.ID).Find(&got)
	if got[0].AudioFile != "one.wav" || got[1].AudioFile != "two.wav" || current.ImageFile != "keep.png" || current.VideoFile != "keep.mp4" || current.VideoFullPrompt != "keep prompt" {
		t.Fatal("restore altered media")
	}
	current.Content = `甲说：“变化。”`
	db.Save(&current)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/projects/%d/scenes/%d/dialogue/analyze", scene.ProjectID, scene.ID), nil))
	if strings.Contains(w.Body.String(), `"excluded":true`) {
		t.Fatalf("stale exclusion survived body change: %s", w.Body.String())
	}
}

func TestDialogueExclusionCreatesTombstoneAndRejectsStaleHash(t *testing.T) {
	s, db, scene := expandHTTPService(t, "http://unused.invalid")
	if err := db.AutoMigrate(&models.Dialogue{}); err != nil {
		t.Fatal(err)
	}
	s.Projects = &ProjectService{db: db}
	scene.Content = `甲说：“重复。”乙说：“重复。”`
	if err := db.Save(scene).Error; err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.POST("/api/projects/:id/scenes/:sid/dialogue/apply", s.HandleApplySceneDialogue)
	post := func(analysis SceneDialogueAnalysis) *httptest.ResponseRecorder {
		body, _ := json.Marshal(analysis)
		w := httptest.NewRecorder()
		q := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/projects/%d/scenes/%d/dialogue/apply", scene.ProjectID, scene.ID), bytes.NewReader(body))
		q.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, q)
		return w
	}
	items := []SceneDialogueAnalysisItem{
		{Index: 0, Text: "重复。", Speaker: "甲", SpeechType: "dialogue", Excluded: true},
		{Index: 1, Text: "重复。", Speaker: "乙", SpeechType: "dialogue"},
	}
	analysis := SceneDialogueAnalysis{Items: items, ContentHash: sceneDialogueContentHash(scene.Content)}
	if w := post(analysis); w.Code != http.StatusOK {
		t.Fatalf("apply status=%d body=%s", w.Code, w.Body.String())
	}
	var rows []models.Dialogue
	db.Where("scene_id=?", scene.ID).Order("`order`,id").Find(&rows)
	if len(rows) != 2 || rows[0].Source != "user_excluded" || rows[0].SpeechType != "unknown" || rows[1].Source != "ai_reviewed" {
		t.Fatalf("expected one tombstone per occurrence: %+v", rows)
	}
	var current models.Scene
	db.First(&current, scene.ID)
	if got := s.Projects.sceneVideoDialogues(&current); len(got) != 1 || got[0].Character != "乙" {
		t.Fatalf("excluded occurrence resurrected: %+v", got)
	}
	var ready []models.Dialogue
	ready, skipped, err := s.Projects.loadReadyDialogues(db, []uint{scene.ID})
	if err != nil || len(ready) != 0 || len(skipped) != 1 || skipped[0].Dialogue.Source == "user_excluded" {
		t.Fatalf("mix load included tombstone: ready=%+v skipped=%+v err=%v", ready, skipped, err)
	}
	beforeJSON := current.DialogueAnalysisJSON
	current.Content = `甲说：“已变化。”`
	if err := db.Save(&current).Error; err != nil {
		t.Fatal(err)
	}
	if w := post(analysis); w.Code != http.StatusConflict {
		t.Fatalf("stale apply status=%d body=%s", w.Code, w.Body.String())
	}
	var after models.Scene
	db.First(&after, scene.ID)
	var count int64
	db.Model(&models.Dialogue{}).Where("scene_id=?", scene.ID).Count(&count)
	if count != 2 || after.DialogueAnalysisJSON != beforeJSON {
		t.Fatalf("stale request wrote data: count=%d json=%q", count, after.DialogueAnalysisJSON)
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
