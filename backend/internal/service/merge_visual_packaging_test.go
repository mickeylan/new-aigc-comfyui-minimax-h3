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

func TestVisualPackagingDraftAndSnapshot(t *testing.T) {
	db := safetyDB(t, &models.EditingDraft{}, &models.MergeTask{}, &models.Material{})
	p := models.Project{Title: "packaging", Generation: 1}
	db.Create(&p)
	a := readyMergeScene(p.ID, 1, 1, 1, "a", "a.mp4")
	b := readyMergeScene(p.ID, 1, 2, 1, "b", "b.mp4")
	db.Create(&a)
	db.Create(&b)
	m := models.Material{ProjectID: &p.ID, Type: "image", Path: "1/overlay.png", Size: 20}
	db.Create(&m)
	clips := []mergeEditClip{{SceneID: &a.ID, Packaging: &clipVisualPackaging{Look: "warm", TitleAnimation: "slide", TitleStyle: "nameplate"}, VisualTracks: []clipVisualTrack{{MaterialID: m.ID, Start: 0, End: 1, Width: .5, Opacity: 1, Mask: "rounded"}}}, {SceneID: &b.ID}}
	draft, err := saveEditingDraft(db, p.ID, 1, 0, []editingDraftClip{{mergeEditClip: clips[0], Duration: 2}, {mergeEditClip: clips[1], Duration: 2}})
	if err != nil {
		t.Fatal(err)
	}
	var restored []mergeEditClip
	if err := json.Unmarshal([]byte(draft.EditSequence), &restored); err != nil || restored[0].Packaging.Look != "warm" || restored[0].VisualTracks[0].Mask != "rounded" {
		t.Fatalf("draft lost packaging %v", err)
	}
	svc := &ProjectService{db: db}
	audio, _ := mergeAudioOptions(nil, nil, nil)
	mt, _, err := svc.createMergeTaskRecord(&p, []uint{a.ID, b.ID}, false, false, audio, mergeTaskSequence{Clips: clips, EpisodeN: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.validateMergeInputs(db, mt); err != nil {
		t.Fatal(err)
	}
	clips[0].Packaging.Look = "cool"
	encoded, _ := json.Marshal(clips)
	db.Model(mt).Update("edit_sequence", string(encoded))
	if _, err := svc.validateMergeInputs(db, mt); err == nil {
		t.Fatal("changed packaging reused old snapshot")
	}
}

func TestVisualPackagingContractAndTitleCues(t *testing.T) {
	for _, tr := range []string{"fadeblack", "fadewhite", "dissolve", "circleopen", "circleclose", "wipeup", "slideleft"} {
		if !validMergeTransition(tr) {
			t.Fatal(tr)
		}
	}
	for _, p := range []*clipVisualPackaging{{Look: "unknown"}, {TitleAnimation: "shell"}, {TitleStyle: "random"}} {
		if validateVisualPackaging(p) == nil {
			t.Fatal("unknown packaging accepted")
		}
	}
	if validateOverlayMask("movie=outside") == nil {
		t.Fatal("unsafe mask accepted")
	}
	raw := `{"scene_id":1,"packaging":{"look":"warm","title_style":"chapter","title_animation":"fade"}}`
	var c mergeEditClip
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		t.Fatal(err)
	}
	v := resolvedVideoFromClip(c, mergeResolvedVideo{SourceDuration: 2})
	v.Title = "测试标题"
	cues := buildMergeTitleCues([]mergeResolvedVideo{v})
	if len(cues) != 1 || cues[0].Animation != "fade" || cues[0].StyleCode != "xianxia-gold-title" {
		t.Fatalf("title lost packaging: %+v", cues)
	}
	for _, animation := range []string{"fade", "slide", "typewriter"} {
		v.Packaging.TitleAnimation = animation
		cue := buildMergeTitleCues([]mergeResolvedVideo{v})[0]
		ass, _ := buildScreenTextASS([]models.ScreenTextCue{cue}, []models.Scene{{Duration: 2}}, []sceneVideo{{dur: 2}}, 160, 96, "Noto Sans SC")
		token := map[string]string{"fade": `\fad(`, "slide": `\move(`, "typewriter": `\kf`}[animation]
		if !strings.Contains(string(ass), token) {
			t.Fatalf("animation missing %s: %s", animation, ass)
		}
	}
	encoded, _ := json.Marshal([]mergeEditClip{c})
	if !strings.Contains(string(encoded), `"look":"warm"`) {
		t.Fatal("snapshot lost packaging")
	}
}
func TestVisualPackagingRealFFmpegMasksLooksAndTransitions(t *testing.T) {
	ff, err := localFFmpegPath()
	if err != nil {
		t.Skip(err)
	}
	dir := t.TempDir()
	timeout := 45 * time.Second
	pixel := func(file, at, pos string) []byte {
		t.Helper()
		raw := filepath.Join(dir, "sample.rgb")
		if log, err := runLocalProgram(ff, []string{"-y", "-ss", at, "-i", file, "-vf", "crop=2:2:" + pos, "-frames:v", "1", "-pix_fmt", "rgb24", "-f", "rawvideo", raw}, timeout); err != nil {
			t.Fatalf("pixel %v %s", err, log)
		}
		data, err := os.ReadFile(raw)
		if err != nil || len(data) < 3 {
			t.Fatal("missing pixel")
		}
		return data[:3]
	}
	for _, mask := range []string{"ellipse", "rounded"} {
		t.Run(mask, func(t *testing.T) {
			v := mergeResolvedVideo{SourceDuration: 1, VisualTracks: []clipVisualTrack{{MaterialID: 1, Start: 0, End: 1, Width: 1, Opacity: 1, InputIndex: 1, Mask: mask}}}
			graph := buildEditedVideoGraph([]mergeResolvedVideo{v}, 160, 96)
			out := filepath.Join(dir, mask+".mp4")
			if log, err := runLocalProgram(ff, []string{"-y", "-f", "lavfi", "-i", "color=c=black:s=160x96:r=24:d=1", "-f", "lavfi", "-i", "color=c=red:s=160x96:r=24:d=1", "-filter_complex_threads", "1", "-filter_complex", strings.Join(graph, ";"), "-map", "[v]", "-c:v", "libx264", "-threads", "1", "-pix_fmt", "yuv420p", out}, timeout); err != nil {
				t.Fatalf("mask %v %s", err, log)
			}
			corner, center := pixel(out, "0.5", "0:0"), pixel(out, "0.5", "78:46")
			if corner[0] > 40 || center[0] < 180 {
				t.Fatalf("mask invisible/opaque %v %v", corner, center)
			}
		})
	}
	for _, look := range []string{"warm", "cool", "muted", "monochrome"} {
		t.Run(look, func(t *testing.T) {
			v := mergeResolvedVideo{SourceDuration: 1, Packaging: &clipVisualPackaging{Look: look}}
			out := filepath.Join(dir, look+".mp4")
			if log, err := runLocalProgram(ff, []string{"-y", "-f", "lavfi", "-i", "color=c=gray:s=160x96:r=24:d=1", "-filter_complex_threads", "1", "-filter_complex", strings.Join(buildEditedVideoGraph([]mergeResolvedVideo{v}, 160, 96), ";"), "-map", "[v]", "-c:v", "libx264", "-threads", "1", "-pix_fmt", "yuv420p", out}, timeout); err != nil {
				t.Fatalf("look %v %s", err, log)
			}
			rgb := pixel(out, "0.5", "20:20")
			if look == "warm" && int(rgb[0])-int(rgb[2]) < 8 {
				t.Fatalf("warm LUT inactive %v", rgb)
			}
			if look == "cool" && int(rgb[2])-int(rgb[0]) < 8 {
				t.Fatalf("cool LUT inactive %v", rgb)
			}
		})
	}
	for _, tr := range []string{"dissolve", "fadeblack", "fadewhite", "wiperight", "wipeup", "wipedown", "slideleft", "slideup", "slidedown", "circleopen", "circleclose"} {
		t.Run(tr, func(t *testing.T) {
			graph := buildEditedVideoGraph([]mergeResolvedVideo{{SourceDuration: 1, Transition: tr, TransitionDuration: .25}, {SourceDuration: 1}}, 160, 96)
			if log, err := runLocalProgram(ff, []string{"-y", "-f", "lavfi", "-i", "color=c=red:s=160x96:r=24:d=1", "-f", "lavfi", "-i", "color=c=blue:s=160x96:r=24:d=1", "-filter_complex_threads", "1", "-filter_complex", strings.Join(graph, ";"), "-map", "[v]", "-f", "null", "-"}, timeout); err != nil {
				t.Fatalf("transition %v %s", err, log)
			}
		})
	}
}
