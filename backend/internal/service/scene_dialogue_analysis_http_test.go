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
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"applied":1`) {
		t.Fatalf("apply status=%d body=%s", w.Code, w.Body.String())
	}
	var dialogues []models.Dialogue
	db.Where("project_id=? AND scene_id=?", scene.ProjectID, scene.ID).Find(&dialogues)
	if len(dialogues) != 1 || dialogues[0].Text != long || dialogues[0].Character != "燕承" || dialogues[0].Source != "ai_reviewed" || dialogues[0].TimeSegment != "2—5秒" {
		t.Fatalf("dialogues=%+v", dialogues)
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
