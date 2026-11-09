package service

import (
	"comfyui-console/internal/models"
	"math"
	"strings"
	"testing"
)

func clipTimelineFixture(t *testing.T) (*ProjectService, models.Project, models.MergeTask, []mergeResolvedVideo, models.Scene, models.Scene) {
	t.Helper()
	db := safetyDB(t, &models.MergeTask{}, &models.ScreenTextCue{})
	p := models.Project{Title: "clip timeline", Generation: 1}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	a := readyMergeScene(p.ID, 1, 1, 1, "a", "a.mp4")
	a.Duration = 10
	b := readyMergeScene(p.ID, 1, 2, 1, "b", "b.mp4")
	b.Duration = 8
	for _, sc := range []*models.Scene{&a, &b} {
		if err := db.Create(sc).Error; err != nil {
			t.Fatal(err)
		}
	}
	return &ProjectService{db: db}, p, models.MergeTask{ProjectID: p.ID, EpisodeN: 1, Generation: 1}, []mergeResolvedVideo{
		{SceneID: b.ID, SourceStart: 2, SourceEnd: float64Ptr(5), SourceDuration: 8},
		{SceneID: a.ID, SourceStart: 1, SourceEnd: float64Ptr(3), SourceDuration: 10},
		{SceneID: a.ID, SourceStart: 5, SourceEnd: float64Ptr(7), SourceDuration: 10},
		{SourceStart: 1, SourceEnd: float64Ptr(3), SourceDuration: 4},
	}, a, b
}
func nearClipTime(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-6 {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestMergeClipTimelineMapsAudioAndTextByOccurrenceWithoutMutatingSources(t *testing.T) {
	s, p, mt, videos, a, b := clipTimelineFixture(t)
	layers := []models.AudioLayer{{ID: 1, StartTime: 12.5, EndTime: 14, SourceStart: .25}, {ID: 2, SceneID: &a.ID, StartTime: 1.5, EndTime: 6, SourceStart: 1}}
	mapped, err := s.mapMergeAudioLayers(&p, &mt, videos, layers)
	if err != nil {
		t.Fatal(err)
	}
	if len(mapped) != 3 {
		t.Fatalf("mapped layers: %+v", mapped)
	}
	nearClipTime(t, mapped[0].StartTime, .5)
	nearClipTime(t, mapped[0].EndTime, 2)
	nearClipTime(t, mapped[1].StartTime, 3.5)
	nearClipTime(t, mapped[1].EndTime, 5)
	nearClipTime(t, mapped[2].StartTime, 5)
	nearClipTime(t, mapped[2].EndTime, 6)
	nearClipTime(t, mapped[2].SourceStart, 4.5)
	if layers[1].SourceStart != 1 || layers[1].StartTime != 1.5 {
		t.Fatal("source layer mutated")
	}
	cues := []models.ScreenTextCue{{SceneID: &b.ID, StartTime: 2.5, EndTime: 4, Text: "乙"}, {StartTime: 1.5, EndTime: 2.5, Text: "全局"}}
	text, err := s.mapMergeScreenTextCues(&p, &mt, videos, cues)
	if err != nil {
		t.Fatal(err)
	}
	if len(text) != 2 || text[0].SceneID != nil || text[1].SceneID != nil {
		t.Fatalf("absolute mapped cues: %+v", text)
	}
	nearClipTime(t, text[0].StartTime, .5)
	nearClipTime(t, text[1].StartTime, 3.5)
	if cues[0].SceneID == nil || cues[0].StartTime != 2.5 {
		t.Fatal("source cue mutated")
	}
	durs := makeVideoDurations(videos)
	_, total, err := mergeTimelineClips(videos, durs)
	if err != nil {
		t.Fatal(err)
	}
	nearClipTime(t, total, 9)
}
func TestMergeClipTimelineSubtitlesIntersectSourceRangesAndPreserveSpeaker(t *testing.T) {
	s, _, _, videos, a, b := clipTimelineFixture(t)
	for _, d := range []models.Dialogue{{SceneID: a.ID, Order: 1, Character: "甲", Text: "原话", TimeSegment: "1.5-2.5秒"}, {SceneID: b.ID, Order: 1, Character: "乙", Text: "回答", TimeSegment: "2.5-4秒"}, {SceneID: a.ID, Order: 2, Text: "排除", Source: "user_excluded"}} {
		if err := s.db.Create(&d).Error; err != nil {
			t.Fatal(err)
		}
	}
	text, count, err := s.buildMergeTimelineSRT(videos, makeVideoDurations(videos))
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 || !strings.Contains(text, "00:00:00,500 --> 00:00:02,000\n乙：回答") || !strings.Contains(text, "00:00:03,500 --> 00:00:04,500\n甲：原话") || strings.Contains(text, "排除") {
		t.Fatalf("count=%d subtitles=%s", count, text)
	}
}
func TestMergeClipTimelineMaterialOnlyKeepsExplicitOutputAudio(t *testing.T) {
	s, p, mt, _, _, _ := clipTimelineFixture(t)
	layers := []models.AudioLayer{{StartTime: 0, EndTime: 2, File: "music.wav"}}
	got, err := s.mapMergeAudioLayers(&p, &mt, []mergeResolvedVideo{{SourceDuration: 3}}, layers)
	if err != nil || len(got) != 1 || got[0].File != "music.wav" {
		t.Fatalf("material-only lost output audio: %+v %v", got, err)
	}
}

func TestMergeClipTimelineSpeedAndTransition(t *testing.T) {
	videos := []mergeResolvedVideo{
		{SourceDuration: 6, Speed: 2, Transition: "fade", TransitionDuration: .5},
		{SourceDuration: 2},
	}
	clips, total, err := mergeTimelineClips(videos, makeVideoDurations(videos))
	if err != nil {
		t.Fatal(err)
	}
	nearClipTime(t, clips[1].outStart, 2.5)
	nearClipTime(t, total, 4.5)
}

func TestMergeClipTimelineSRTScalesSpeedAndTransition(t *testing.T) {
	s, _, _, _, a, b := clipTimelineFixture(t)
	for _, d := range []models.Dialogue{
		{SceneID: a.ID, Order: 1, Text: "A", TimeSegment: "2-4秒"},
		{SceneID: b.ID, Order: 1, Text: "B", TimeSegment: "0-1秒"},
	} {
		if err := s.db.Create(&d).Error; err != nil {
			t.Fatal(err)
		}
	}
	videos := []mergeResolvedVideo{
		{SceneID: a.ID, SourceDuration: 6, Speed: 2, Transition: "fade", TransitionDuration: .5},
		{SceneID: b.ID, SourceDuration: 2},
	}
	text, count, err := s.buildMergeTimelineSRT(videos, makeVideoDurations(videos))
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 || !strings.Contains(text, "00:00:01,000 --> 00:00:02,000\nA") ||
		!strings.Contains(text, "00:00:02,500 --> 00:00:03,500\nB") {
		t.Fatalf("count=%d subtitles=%s", count, text)
	}
}
