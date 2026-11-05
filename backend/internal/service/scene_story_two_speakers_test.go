package service

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

const (
	twoSpeakerFirst  = "那还等什么？我等这便一鼓作气，将之擒下吧。"
	twoSpeakerSecond = "此妖这般逃了一日一夜，怕是也快山穷水尽了，不过其擅使木属性幻术，在这片草丛中可谓如鱼得水，贸然追进去，怕是不妥。"
	twoSpeakerSource = "“" + twoSpeakerFirst + "”马脸男子见此，便要冲入草丛。\n“" + twoSpeakerSecond + "”齐姓道士眼睛微眯了一下，说道。"
	twoSpeakerStory  = "马脸男子见同伴已围住草丛，按捺不住地说道：“" + twoSpeakerFirst + "”话音未落，他便要冲入草丛。\n齐姓道士眼睛微眯，抬手拦住他：“" + twoSpeakerSecond + "”马脸男子闻言顿住脚步。"
)

func prepareTwoSpeakerStory(t *testing.T) (*storyHTTPFixture, models.Dialogue) {
	t.Helper()
	f := newStoryHTTPFixture(t)
	var ep models.EpisodeAdaptation
	if err := f.db.Where("project_id = ? AND episode_n = ?", f.scene.ProjectID, f.scene.EpisodeN).First(&ep).Error; err != nil {
		t.Fatal(err)
	}
	var chapterIDs []uint
	if err := json.Unmarshal([]byte(ep.SourceChapterIDs), &chapterIDs); err != nil || len(chapterIDs) != 1 {
		t.Fatalf("mapped chapter ids=%q err=%v", ep.SourceChapterIDs, err)
	}
	if err := f.db.Model(&models.Chapter{}).Where("id = ?", chapterIDs[0]).Update("content", twoSpeakerSource).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Model(&models.Dialogue{}).Where("id = ?", f.dialogue.ID).Update("text", twoSpeakerFirst).Error; err != nil {
		t.Fatal(err)
	}
	f.dialogue.Text = twoSpeakerFirst
	second := models.Dialogue{ProjectID: f.scene.ProjectID, SceneID: f.scene.ID, Order: 2, SpeechType: "unknown", Text: twoSpeakerSecond, Source: "user"}
	if err := f.db.Create(&second).Error; err != nil {
		t.Fatal(err)
	}
	f.reply = mustJSON(sceneStoryRewriteResult{Content: twoSpeakerStory, Dialogues: []sceneStoryDialogue{
		{ID: f.dialogue.ID, Speaker: "马脸男子", SpeechType: "dialogue", Text: twoSpeakerFirst},
		{ID: second.ID, Speaker: "齐姓道士", SpeechType: "dialogue", Text: twoSpeakerSecond},
	}})
	return f, second
}

func TestSceneStoryRewriteHTTPTwoSpeakersPreviewAndApply(t *testing.T) {
	f, second := prepareTwoSpeakerStory(t)
	requestContent := "用户坏稿 Summary warns the user. 【对白汇总】"
	w := f.post(t, "/preview", map[string]string{"content": requestContent})
	if w.Code != http.StatusOK {
		t.Fatalf("preview status=%d body=%s", w.Code, w.Body.String())
	}
	var preview sceneStoryApplyRequest
	if err := json.Unmarshal(w.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	if preview.Content != twoSpeakerStory || len(preview.Dialogues) != 2 || preview.Dialogues[0].Speaker != "马脸男子" || preview.Dialogues[1].Speaker != "齐姓道士" {
		t.Fatalf("unexpected preview: %+v", preview)
	}
	var beforeScene models.Scene
	var beforeDialogues []models.Dialogue
	f.db.First(&beforeScene, f.scene.ID)
	f.db.Where("scene_id = ?", f.scene.ID).Order("`order`, id").Find(&beforeDialogues)
	if beforeScene.Content != "旧场景正文" || len(beforeDialogues) != 2 || beforeDialogues[0].Character != "" || beforeDialogues[1].Character != "" {
		t.Fatalf("preview mutated DB: scene=%+v dialogues=%+v", beforeScene, beforeDialogues)
	}
	f.mu.Lock()
	providerRequest := f.request
	f.mu.Unlock()
	messages := providerRequest["messages"].([]any)
	system := messages[0].(map[string]any)["content"].(string)
	user := messages[len(messages)-1].(map[string]any)["content"].(string)
	for _, want := range []string{requestContent, "“" + twoSpeakerFirst + "”马脸男子见此，便要冲入草丛。", "“" + twoSpeakerSecond + "”齐姓道士眼睛微眯了一下，说道。", "已保存对白", "原文匹配段落"} {
		if !strings.Contains(user, want) {
			t.Errorf("provider request missing %q: %s", want, user)
		}
	}
	if strings.Contains(system, "director_note") || !strings.Contains(system, "逐字、按原顺序及原出现次数") || !strings.Contains(system, "不得在正文末尾附对白清单") {
		t.Fatalf("wrong story contract: %s", system)
	}
	if w = f.post(t, "/apply", preview); w.Code != http.StatusOK {
		t.Fatalf("apply status=%d body=%s", w.Code, w.Body.String())
	}
	var saved models.Scene
	var dialogues []models.Dialogue
	f.db.First(&saved, f.scene.ID)
	f.db.Where("scene_id = ?", f.scene.ID).Order("`order`, id").Find(&dialogues)
	if saved.Content != twoSpeakerStory || len(dialogues) != 2 || dialogues[0].ID != f.dialogue.ID || dialogues[1].ID != second.ID || dialogues[0].Character != "马脸男子" || dialogues[1].Character != "齐姓道士" {
		t.Fatalf("two-speaker apply mismatch: scene=%+v dialogues=%+v", saved, dialogues)
	}
	dubs := (&ProjectService{db: f.db}).sceneVideoDialogues(&saved)
	compiled := appendStructuredDialogue("[Shot 1] 围捕。", dubs, nil)
	if len(dubs) != 2 || strings.Count(compiled, twoSpeakerFirst) != 1 || strings.Count(compiled, twoSpeakerSecond) != 1 || !strings.Contains(compiled, "马脸男子") || !strings.Contains(compiled, "齐姓道士") {
		t.Fatalf("incorrect H3 dialogue compilation: dubs=%+v compiled=%s", dubs, compiled)
	}
}

func TestSceneStoryRewriteHTTPTwoSpeakersRejectsIncompleteOrRepeatedContent(t *testing.T) {
	cases := []struct{ name, content string }{
		{"second replaced by tagged summary", "马脸男子说道：“" + twoSpeakerFirst + "”\nSummary warns the user. 【对白汇总】齐姓道士劝他谨慎。"},
		{"second replaced by Chinese summary", "马脸男子说道：“" + twoSpeakerFirst + "”齐姓道士说明妖物擅长木属性幻术，劝他不要追。"},
		{"first repeated", "马脸男子说道：“" + twoSpeakerFirst + "”他又重复：“" + twoSpeakerFirst + "”齐姓道士说道：“" + twoSpeakerSecond + "”"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, second := prepareTwoSpeakerStory(t)
			f.reply = mustJSON(sceneStoryRewriteResult{Content: tc.content, Dialogues: []sceneStoryDialogue{
				{ID: f.dialogue.ID, Speaker: "马脸男子", SpeechType: "dialogue", Text: twoSpeakerFirst},
				{ID: second.ID, Speaker: "齐姓道士", SpeechType: "dialogue", Text: twoSpeakerSecond},
			}})
			w := f.post(t, "/preview", map[string]string{"content": "用户坏稿 Summary warns the user. 【对白汇总】"})
			if w.Code != http.StatusUnprocessableEntity {
				t.Fatalf("invalid rewrite status=%d body=%s", w.Code, w.Body.String())
			}
			var scene models.Scene
			var dialogues []models.Dialogue
			f.db.First(&scene, f.scene.ID)
			f.db.Where("scene_id = ?", f.scene.ID).Order("`order`, id").Find(&dialogues)
			if scene.Content != "旧场景正文" || len(dialogues) != 2 || dialogues[0].Character != "" || dialogues[1].Character != "" || dialogues[0].Text != twoSpeakerFirst || dialogues[1].Text != twoSpeakerSecond {
				t.Fatalf("failed preview mutated DB: scene=%+v dialogues=%+v", scene, dialogues)
			}
		})
	}
}
