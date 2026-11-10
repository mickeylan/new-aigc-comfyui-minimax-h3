package service

import (
	"comfyui-console/internal/models"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGlobalTracksDraftCASAndImmutableMerge(t *testing.T) {
	db := safetyDB(t, &models.EditingDraft{}, &models.MergeTask{}, &models.Material{})
	p := models.Project{Title: "global", Generation: 1}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	a := readyMergeScene(p.ID, 1, 1, 1, "a", "a.mp4")
	b := readyMergeScene(p.ID, 1, 2, 1, "b", "b.mp4")
	db.Create(&a)
	db.Create(&b)
	m := models.Material{ProjectID: &p.ID, Type: "image", Path: "1/overlay.png", Size: 20}
	db.Create(&m)
	tracks := []clipVisualTrack{{MaterialID: m.ID, Start: .5, End: 3.5, Width: .5, Opacity: 1}}
	clips := []mergeEditClip{{SceneID: &a.ID}, {SceneID: &b.ID}}
	drafts := []editingDraftClip{{mergeEditClip: clips[0], Duration: 2}, {mergeEditClip: clips[1], Duration: 2}}
	draft, err := saveEditingDraft(db, p.ID, 1, 0, drafts, tracks)
	if err != nil {
		t.Fatal(err)
	}
	var saved []clipVisualTrack
	if err := json.Unmarshal([]byte(draft.VisualTracks), &saved); err != nil || len(saved) != 1 || saved[0].End != 3.5 {
		t.Fatalf("lost global draft: %+v %v", saved, err)
	}
	if _, err := saveEditingDraft(db, p.ID, 1, 0, drafts, nil); err != errEditingDraftConflict {
		t.Fatalf("stale overwrite accepted %v", err)
	}
	bad := append([]clipVisualTrack(nil), tracks...)
	bad[0].End = 5
	if _, err := saveEditingDraft(db, p.ID, 1, draft.Revision, drafts, bad); err == nil {
		t.Fatal("draft overflow accepted")
	}
	db.First(&draft, draft.ID)
	if draft.Revision != 1 || !strings.Contains(draft.VisualTracks, "3.5") {
		t.Fatal("failed save changed draft")
	}
	legacy, err := saveEditingDraft(db, p.ID, 1, draft.Revision, drafts)
	if err != nil || legacy.VisualTracks != draft.VisualTracks {
		t.Fatalf("legacy client erased global tracks %v", err)
	}
	cleared, err := saveEditingDraft(db, p.ID, 1, legacy.Revision, drafts, []clipVisualTrack{})
	if err != nil || cleared.VisualTracks != "[]" {
		t.Fatalf("explicit clear failed %v", err)
	}
	svc := &ProjectService{db: db}
	audio, _ := mergeAudioOptions(nil, nil, nil)
	mt, _, err := svc.createMergeTaskRecord(&p, []uint{a.ID, b.ID}, false, false, audio, mergeTaskSequence{Clips: clips, EpisodeN: 1, VisualTracks: tracks})
	if err != nil {
		t.Fatal(err)
	}
	var snap mergeInputSnapshot
	json.Unmarshal([]byte(mt.InputSnapshot), &snap)
	if snap.Version != 8 || len(snap.VisualTracks) != 1 || len(snap.Materials) != 1 {
		t.Fatalf("snapshot %+v", snap)
	}
	if _, err := svc.validateMergeInputs(db, mt); err != nil {
		t.Fatalf("fresh snapshot rejected %v", err)
	}
	db.Model(mt).Update("visual_tracks", `[{"material_id":1,"start":0,"end":1,"width":1,"opacity":1}]`)
	if _, err := svc.validateMergeInputs(db, mt); err == nil {
		t.Fatal("modified global parameters accepted")
	}
	db.Model(mt).Update("visual_tracks", draft.VisualTracks)
	db.Model(&m).Update("size", 21)
	if _, err := svc.validateMergeInputs(db, mt); err == nil {
		t.Fatal("modified global source accepted")
	}
}
func TestGlobalTracksRealFFmpegAcrossTransitionAndText(t *testing.T) {
	ff, err := localFFmpegPath()
	if err != nil {
		t.Skip(err)
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "global.mp4")
	timeout := 45 * time.Second
	videos := []mergeResolvedVideo{{SourceDuration: 2, Transition: "fade", TransitionDuration: .5}, {SourceDuration: 2}}
	global := []clipVisualTrack{{MaterialID: 1, Start: .5, End: 3, Width: .5, Opacity: 1, InputIndex: 2}, {MaterialID: 2, Start: 1.75, End: 2.25, Width: .5, Opacity: 1, InputIndex: 3}}
	if validateGlobalVisualTracks(global, 3.5) != nil {
		t.Fatal("valid global rejected")
	}
	if validateGlobalVisualTracks(global, 2) == nil {
		t.Fatal("global beyond edited duration accepted")
	}
	graph := applyGlobalVisualTracks(buildEditedVideoGraph(videos, 160, 96), global, 160)
	graph = applyMergeTextFilters(graph, []string{"drawbox=x=140:y=80:w=10:h=10:color=white:t=fill"})
	args := []string{"-y"}
	for _, color := range []string{"black", "green", "red", "blue"} {
		args = append(args, "-f", "lavfi", "-i", "color=c="+color+":s=160x96:r=24:d=4")
	}
	args = append(args, "-filter_complex_threads", "1", "-filter_complex", strings.Join(graph, ";"), "-map", "[v]", "-c:v", "libx264", "-threads", "1", "-pix_fmt", "yuv420p", out)
	if log, err := runLocalProgram(ff, args, timeout); err != nil {
		t.Fatalf("render %v %s", err, log)
	}
	pixel := func(at, pos string) []byte {
		t.Helper()
		file := filepath.Join(dir, at+pos+".rgb")
		if log, err := runLocalProgram(ff, []string{"-y", "-ss", at, "-i", out, "-vf", "crop=2:2:" + pos, "-frames:v", "1", "-pix_fmt", "rgb24", "-f", "rawvideo", file}, timeout); err != nil {
			t.Fatalf("pixel %v %s", err, log)
		}
		data, err := os.ReadFile(file)
		if err != nil || len(data) < 3 {
			t.Fatal("missing pixel")
		}
		return data[:3]
	}
	// Red stays continuous through the 1.5–2s main-video transition; blue sits
	// above it only within its own output-local interval. No per-clip restart.
	before, red1, blue, red2, after := pixel("0.1", "20:20"), pixel("1.6", "20:20"), pixel("2.1", "20:20"), pixel("2.6", "20:20"), pixel("3.2", "20:20")
	if before[0] > 30 || red1[0] < 180 || blue[2] < 180 || red2[0] < 180 || after[0] > 30 {
		t.Fatalf("global continuity/order %v %v %v %v %v", before, red1, blue, red2, after)
	}
	white := pixel("2.1", "142:82")
	if white[0] < 180 || white[1] < 180 || white[2] < 180 {
		t.Fatalf("text/filter layer lost %v", white)
	}
}
