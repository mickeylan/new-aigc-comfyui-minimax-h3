package service

import (
	"comfyui-console/internal/models"
	"encoding/json"
	"strings"
	"testing"
)

func reflowFixture(t *testing.T) (*AdaptationService, uint, models.Scene) {
	t.Helper()
	ps, s, p, ep := newEpisodeAdaptationTestService(t, &captureTextProvider{})
	if err := ps.db.AutoMigrate(&models.ScriptRevision{}, &models.Shot{}); err != nil {
		t.Fatal(err)
	}
	q := "上一集已经说完且不可重新使用的完整原文台词"
	if err := ps.db.Model(&models.Chapter{}).Where("project_id=?", p.ID).Update("content", "“"+q+"”").Error; err != nil {
		t.Fatal(err)
	}
	events, _ := json.Marshal([]string{"青年说“" + q + "”"})
	if err := ps.db.Model(&models.EpisodeAdaptation{}).Where("id=?", ep.ID).Updates(map[string]any{"must_keep_events": string(events), "script": "错误旧剧本"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := ps.db.Model(&models.EpisodeAdaptation{}).Where("project_id=? AND episode_n=1", p.ID).Update("source_chapter_ids", ep.SourceChapterIDs).Error; err != nil {
		t.Fatal(err)
	}
	prior := models.Scene{ProjectID: p.ID, EpisodeN: 1, Generation: 1, Order: 1, Content: "保留前集", VideoFile: "keep.mp4"}
	if err := ps.db.Create(&prior).Error; err != nil {
		t.Fatal(err)
	}
	if err := ps.db.Create(&models.Dialogue{ProjectID: p.ID, SceneID: prior.ID, Text: q}).Error; err != nil {
		t.Fatal(err)
	}
	old := models.Scene{ProjectID: p.ID, EpisodeN: 2, Generation: 2, Order: 1, Content: "错误重复剧本暂存"}
	if err := ps.db.Create(&old).Error; err != nil {
		t.Fatal(err)
	}
	for n := 3; n <= 4; n++ {
		if err := ps.db.Create(&models.EpisodeAdaptation{ProjectID: p.ID, EpisodeN: n, Title: []string{"", "", "", "血仇与远行", "相依岁月"}[n], SourceChapterIDs: "[99]", ChapterStart: n, ChapterEnd: n, Status: "approved", MustKeepEvents: `["下一段原著剧情"]`, AdaptationGoal: "后续原著", OpeningState: "战后", EndingState: "离开"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	return s, p.ID, old
}
func TestAdaptationReflowPreviewApplyKeepsScenesAndEarlierEpisodes(t *testing.T) {
	s, p, old := reflowFixture(t)
	preview, _, err := s.previewReflow(p, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Items) != 2 || preview.Items[0].Title != "血仇与远行" || preview.UnplannedEpisode != 4 {
		t.Fatalf("preview=%+v", preview)
	}
	var before models.EpisodeAdaptation
	s.db.Where("project_id=? AND episode_n=1", p).First(&before)
	if err := s.applyReflow(p, 2, preview.Token); err != nil {
		t.Fatal(err)
	}
	var got models.Scene
	if err := s.db.First(&got, old.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.Archived || got.Content != old.Content {
		t.Fatal("old Scene modified before new script success")
	}
	var ep models.EpisodeAdaptation
	s.db.Where("project_id=? AND episode_n=2", p).First(&ep)
	if ep.Title != "血仇与远行" || ep.Script != "错误旧剧本" || ep.Status != "approved" {
		t.Fatalf("ep=%+v", ep)
	}
	var tail models.EpisodeAdaptation
	s.db.Where("project_id=? AND episode_n=4", p).First(&tail)
	if tail.Status != "draft" || tail.SourceChapterIDs != "[]" {
		t.Fatalf("duplicate tail retained: %+v", tail)
	}
	var after models.EpisodeAdaptation
	s.db.First(&after, before.ID)
	a, _ := json.Marshal(before)
	b, _ := json.Marshal(after)
	if string(a) != string(b) {
		t.Fatal("earlier episode modified")
	}
	var count int64
	s.db.Model(&models.ScriptRevision{}).Where("project_id=?", p).Count(&count)
	if count != 4 {
		t.Fatalf("backup count=%d", count)
	}
	if err := s.applyReflow(p, 2, preview.Token); err == nil {
		t.Fatal("preview replay accepted")
	}
}
func TestAdaptationReflowRejectsStalePreviewAndMediaWithoutWrites(t *testing.T) {
	s, p, old := reflowFixture(t)
	preview, _, err := s.previewReflow(p, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.db.Model(&models.EpisodeAdaptation{}).Where("project_id=? AND episode_n=3", p).Update("title", "并发编辑").Error; err != nil {
		t.Fatal(err)
	}
	if err := s.applyReflow(p, 2, preview.Token); err == nil || !strings.Contains(err.Error(), "已变化") {
		t.Fatalf("err=%v", err)
	}
	var count int64
	s.db.Model(&models.ScriptRevision{}).Count(&count)
	if count != 0 {
		t.Fatal("stale confirmation wrote snapshots")
	}
	if err := s.db.Model(&models.Scene{}).Where("id=?", old.ID).Update("image_file", "reviewed.png").Error; err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.previewReflow(p, 2); err == nil {
		t.Fatal("media protected episode eligible")
	}
}
