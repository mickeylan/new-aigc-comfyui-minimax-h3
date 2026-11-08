package service

import (
	"encoding/json"
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

func TestSharedChapterContextDoesNotRequireWholeChapterDialogue(t *testing.T) {
	ps, adapt, p, ep := newEpisodeAdaptationTestService(t, &captureTextProvider{})
	firstQuote := "本集终点之前已经说过的原文台词"
	nextQuote := "后集才发生的反杀事件对应原文台词"
	if err := ps.db.Model(&models.Chapter{}).Where("project_id=?", p.ID).Update("content", "“"+firstQuote+"”“"+nextQuote+"”").Error; err != nil {
		t.Fatal(err)
	}
	mustKeep, _ := json.Marshal([]string{"青年说“" + firstQuote + "”，随后收手等待"})
	if err := ps.db.Model(&models.EpisodeAdaptation{}).Where("id=?", ep.ID).Update("must_keep_events", string(mustKeep)).Error; err != nil {
		t.Fatal(err)
	}
	next := models.EpisodeAdaptation{ProjectID: p.ID, EpisodeN: 3, SourceChapterIDs: ep.SourceChapterIDs, Status: "approved", MustKeepEvents: `["青年反杀三名追捕者"]`}
	if err := ps.db.Create(&next).Error; err != nil {
		t.Fatal(err)
	}
	ctx, err := adapt.BuildContext(p.ID, 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(ctx.AvailableDialogues) != 2 || len(ctx.DialogueLedger) != 1 || ctx.DialogueLedger[0].Text != firstQuote {
		t.Fatalf("whole chapter still compulsory: %+v", ctx)
	}
	if len(ctx.ReservedFutureEvents) != 1 || !strings.Contains(ctx.ReservedFutureEvents[0], "反杀") {
		t.Fatalf("future reservation missing: %+v", ctx.ReservedFutureEvents)
	}
}

func TestCompletedSourceDialoguesPreserveOccurrences(t *testing.T) {
	ledger := []SourceDialogue{{ChapterID: 2, Text: "小心！"}, {ChapterID: 2, Text: "小心！"}, {ChapterID: 2, Text: "接下来尚未使用的对白"}}
	remaining, used := excludeCompletedSourceDialogues(ledger, map[string]int{sourceQuoteKey("小心！"): 1})
	if len(remaining) != 2 || len(used) != 1 || remaining[0].Text != "小心！" {
		t.Fatalf("remaining=%+v used=%+v", remaining, used)
	}
}

func TestCompletedSourceUsesOnlyActiveSharedChapterEpisodes(t *testing.T) {
	ps, adapt, p, ep := newEpisodeAdaptationTestService(t, &captureTextProvider{})
	previous := models.Scene{ProjectID: p.ID, EpisodeN: 1, Generation: 2, Order: 1, Content: "已完成内容"}
	if err := ps.db.Create(&previous).Error; err != nil {
		t.Fatal(err)
	}
	old := models.Scene{ProjectID: p.ID, EpisodeN: 1, Generation: 1, Order: 1, Archived: true}
	if err := ps.db.Create(&old).Error; err != nil {
		t.Fatal(err)
	}
	for _, d := range []models.Dialogue{
		{ProjectID: p.ID, SceneID: previous.ID, Text: "先前已经使用的这条原文对白不会再分配"},
		{ProjectID: p.ID, SceneID: old.ID, Text: "废弃版本不算已经完成的内容"},
	} {
		if err := ps.db.Create(&d).Error; err != nil {
			t.Fatal(err)
		}
	}
	var ch models.Chapter
	if err := ps.db.First(&ch).Error; err != nil {
		t.Fatal(err)
	}
	sources := []SourceChapter{{ChapterID: ch.ID}}
	counts, err := adapt.completedSourceDialogueCounts(p.ID, ep.EpisodeN, sources)
	if err != nil {
		t.Fatal(err)
	}
	if len(counts) != 0 {
		t.Fatalf("unrelated chapter consumed: %v", counts)
	}
	if err := ps.db.Model(&models.EpisodeAdaptation{}).Where("project_id=? AND episode_n=1", p.ID).Update("source_chapter_ids", ep.SourceChapterIDs).Error; err != nil {
		t.Fatal(err)
	}
	counts, err = adapt.completedSourceDialogueCounts(p.ID, ep.EpisodeN, sources)
	if err != nil {
		t.Fatal(err)
	}
	if len(counts) != 1 || counts[sourceQuoteKey("先前已经使用的这条原文对白不会再分配")] != 1 {
		t.Fatalf("wrong authority: %v", counts)
	}
}

func TestEpisodeSourceExhaustedFailsBeforeModelOrMutation(t *testing.T) {
	provider := &captureTextProvider{response: faithfulEpisodeScript()}
	ps, adapt, p, ep := newEpisodeAdaptationTestService(t, provider)
	quote := "石头哥哥，乐儿总算找到你了！"
	if err := ps.db.Model(&models.Chapter{}).Where("project_id=?", p.ID).Update("content", "柳乐儿说：“"+quote+"”").Error; err != nil {
		t.Fatal(err)
	}
	events, _ := json.Marshal([]string{"柳乐儿说“" + quote + "”"})
	if err := ps.db.Model(&models.EpisodeAdaptation{}).Where("id=?", ep.ID).Update("must_keep_events", string(events)).Error; err != nil {
		t.Fatal(err)
	}
	ctx, err := adapt.BuildContext(p.ID, ep.EpisodeN, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(ctx.DialogueLedger) == 0 {
		t.Fatal("fixture needs source dialogue")
	}
	if err := ps.db.Model(&models.EpisodeAdaptation{}).Where("project_id=? AND episode_n=1", p.ID).Update("source_chapter_ids", ep.SourceChapterIDs).Error; err != nil {
		t.Fatal(err)
	}
	prior := models.Scene{ProjectID: p.ID, EpisodeN: 1, Generation: 1, Order: 1, Content: strings.Join(ctx.MustKeepEvents, "。")}
	// All speech used does not by itself rule out new silent action.
	probe := *ctx
	probe.CompletedDialogues, probe.AvailableDialogues = probe.DialogueLedger, nil
	probe.MustKeepEvents = []string{"暴雨后两人离开旷野走向远处村庄"}
	if consumed, err := adapt.episodeSourceScopeConsumed(p.ID, 2, &probe); err != nil || consumed {
		t.Fatalf("new silent event incorrectly exhausted: consumed=%v err=%v", consumed, err)
	}
	if err := ps.db.Create(&prior).Error; err != nil {
		t.Fatal(err)
	}
	if consumed, err := adapt.episodeSourceScopeConsumed(p.ID, 2, &probe); err != nil || consumed {
		t.Fatalf("new silent event incorrectly exhausted against prior production: consumed=%v err=%v", consumed, err)
	}
	for _, line := range ctx.DialogueLedger {
		if err := ps.db.Create(&models.Dialogue{ProjectID: p.ID, SceneID: prior.ID, Text: line.Text, Character: line.Speaker}).Error; err != nil {
			t.Fatal(err)
		}
	}
	existing := models.Scene{ProjectID: p.ID, EpisodeN: 2, Generation: 2, Order: 1, Content: "保留本集现有内容", VideoFile: "original.mp4"}
	if err := ps.db.Create(&existing).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := adapt.GenerateScript(p.ID, 2); err == nil || !strings.Contains(err.Error(), "全部使用") {
		t.Fatalf("err=%v", err)
	}
	if provider.firstUser != "" {
		t.Fatal("spent model call on exhausted chapter")
	}
	var got models.Scene
	if err := ps.db.First(&got, existing.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.Archived || got.VideoFile != "original.mp4" {
		t.Fatalf("mutated: %+v", got)
	}
}

func TestFinalReplayDetectsParaphrasedVisualWithSameSourceSpeech(t *testing.T) {
	quote := "石头哥哥，乐儿总算找到你了！这些坏人要抓乐儿，石头哥哥帮我打他们！"
	raw := `{"script":"正文","scenes":[{"title":"新标题","content":"全新的视觉措辞","dialogues":[{"character":"女童","text":"` + quote + `"}]}]}`
	issues, err := replayedCompletedSourceDialogues(raw, []SourceDialogue{{Text: quote}})
	if err != nil || len(issues) != 1 {
		t.Fatalf("issues=%v err=%v", issues, err)
	}
	issues, err = replayedCompletedSourceDialogues(raw, []SourceDialogue{{Text: "小心！"}})
	if err != nil || len(issues) != 0 {
		t.Fatalf("short utterance false positive: %v %v", issues, err)
	}
}

func TestDialogueRepairCannotReintroducePreviousEpisodeBeforeSave(t *testing.T) {
	oldQuote := "先前的追捕试探已经全部结束不能再重演了"
	newQuote := "现在是下一个尚未发生的新事件必须继续前进"
	initial := faithfulEpisodeScript()
	var wire map[string]any
	if err := json.Unmarshal([]byte(initial), &wire); err != nil {
		t.Fatal(err)
	}
	first := wire["scenes"].([]any)[0].(map[string]any)
	first["content"] = "他说：“" + oldQuote + "”。接着又说：“" + newQuote + "”。"
	first["dialogues"] = []any{map[string]any{"character": "", "speech_type": "dialogue", "text": oldQuote}, map[string]any{"character": "", "speech_type": "dialogue", "text": newQuote}}
	revised, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	provider := &sequenceTextProvider{responses: []string{initial, string(revised)}}
	ps, adapt, p, ep := newEpisodeAdaptationTestService(t, provider)
	if err := ps.db.Model(&models.Chapter{}).Where("project_id=?", p.ID).Update("content", "“"+oldQuote+"”“"+newQuote+"”").Error; err != nil {
		t.Fatal(err)
	}
	if err := ps.db.Model(&models.EpisodeAdaptation{}).Where("id=?", ep.ID).Update("must_keep_events", "[]").Error; err != nil {
		t.Fatal(err)
	}
	if err := ps.db.Model(&models.EpisodeAdaptation{}).Where("project_id=? AND episode_n=1", p.ID).Update("source_chapter_ids", ep.SourceChapterIDs).Error; err != nil {
		t.Fatal(err)
	}
	prior := models.Scene{ProjectID: p.ID, EpisodeN: 1, Generation: 1, Order: 1, Content: "前集的另一种视觉叙述"}
	if err := ps.db.Create(&prior).Error; err != nil {
		t.Fatal(err)
	}
	if err := ps.db.Create(&models.Dialogue{ProjectID: p.ID, SceneID: prior.ID, Text: oldQuote}).Error; err != nil {
		t.Fatal(err)
	}
	before := models.Scene{ProjectID: p.ID, EpisodeN: 2, Generation: 2, Order: 1, Content: "本集原来的生产内容", VideoFile: "keep.mp4"}
	if err := ps.db.Create(&before).Error; err != nil {
		t.Fatal(err)
	}
	_, err = adapt.GenerateScript(p.ID, 2)
	if err == nil || !strings.Contains(err.Error(), "最终稿重演") {
		t.Fatalf("err=%v", err)
	}
	var got models.Scene
	if err := ps.db.First(&got, before.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.Archived || got.Content != before.Content || got.VideoFile != before.VideoFile {
		t.Fatalf("failed final gate mutated production: %+v", got)
	}
	var count int64
	if err := ps.db.Model(&models.Scene{}).Where("project_id=? AND episode_n=2", p.ID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("new scenes created despite replay: %d", count)
	}
	if len(provider.users) != 2 {
		t.Fatalf("did not exercise dialogue repair: calls=%d", len(provider.users))
	}
}

func TestAdaptationGenerationPreservesApprovedBoundaries(t *testing.T) {
	var wire map[string]any
	if err := json.Unmarshal([]byte(faithfulEpisodeScript()), &wire); err != nil {
		t.Fatal(err)
	}
	wire["opening_state"] = map[string]any{"action": "模型把开场倒退到追逐"}
	wire["ending_state"] = map[string]any{"casualties": []string{"三人死亡"}}
	raw, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	provider := &captureTextProvider{response: string(raw)}
	_, adapt, p, ep := newEpisodeAdaptationTestService(t, provider)
	got, err := adapt.GenerateScript(p.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got.OpeningState != ep.OpeningState || got.EndingState != ep.EndingState {
		t.Fatalf("approved states overwritten: %+v", got)
	}
	if !strings.Contains(got.ReviewJSON, "generated_ending_state") {
		t.Fatal("generated state evidence missing")
	}
}
