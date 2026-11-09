package service

import (
	"comfyui-console/internal/config"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClipTransformValidationAndExpressions(t *testing.T) {
	tr := &clipTransform{clipTransformPose: clipTransformPose{Zoom: 1}, Fit: "contain", Keyframes: []clipTransformKeyframe{{clipTransformPose: clipTransformPose{Zoom: 1}, Time: 0}, {clipTransformPose: clipTransformPose{Zoom: 2, X: .2, Rotation: 30}, Time: 2}}}
	if err := validateClipTransform(tr, 2); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*clipTransform){func(v *clipTransform) { v.CropLeft = .6; v.CropRight = .6 }, func(v *clipTransform) { v.Zoom = math.NaN() }, func(v *clipTransform) { v.Keyframes[1].Time = 0 }, func(v *clipTransform) { v.Keyframes[1].Time = 3 }} {
		clone := *tr
		clone.Keyframes = append([]clipTransformKeyframe(nil), tr.Keyframes...)
		change(&clone)
		if validateClipTransform(&clone, 2) == nil {
			t.Fatal("invalid transform accepted")
		}
	}
	resolved := resolvedVideoFromClip(mergeEditClip{Transform: tr}, mergeResolvedVideo{SourceDuration: 2})
	if resolved.Transform != tr {
		t.Fatal("transform lost during resolution")
	}
	if !strings.Contains(strings.Join(buildEditedVideoGraph([]mergeResolvedVideo{resolved}, 160, 96), ";"), "if(lt(t,") {
		t.Fatal("keyframes not compiled")
	}
}
func TestClipTransformRealFFmpeg(t *testing.T) {
	ffmpeg, err := localFFmpegPath()
	if err != nil {
		t.Skip(err)
	}
	dir := t.TempDir()
	input := filepath.Join(dir, "source.mp4")
	timeout := 45 * time.Second
	if log, err := runLocalProgram(ffmpeg, []string{"-y", "-f", "lavfi", "-i", "testsrc2=s=160x96:r=24:d=2", "-c:v", "libx264", "-threads", "1", input}, timeout); err != nil {
		t.Fatalf("fixture %v %s", err, log)
	}
	hash := mergeFixtureSHA256(t, input)
	for _, fit := range []string{"contain", "cover"} {
		t.Run(fit, func(t *testing.T) {
			tr := &clipTransform{clipTransformPose: clipTransformPose{Zoom: 1}, Fit: fit, CropLeft: .1, CropBottom: .1, Keyframes: []clipTransformKeyframe{{clipTransformPose: clipTransformPose{Zoom: .5, X: -.1}, Time: 0}, {clipTransformPose: clipTransformPose{Zoom: 1.5, X: .1, Y: .1, Rotation: 30}, Time: 2}}}
			graph := buildEditedVideoGraph([]mergeResolvedVideo{{SourceDuration: 2, Transform: tr}}, 160, 96)
			out := filepath.Join(dir, fit+".mp4")
			if log, err := runLocalProgram(ffmpeg, []string{"-y", "-i", input, "-filter_complex_threads", "1", "-filter_complex", strings.Join(graph, ";"), "-map", "[v]", "-c:v", "libx264", "-threads", "1", "-pix_fmt", "yuv420p", out}, timeout); err != nil {
				t.Fatalf("transform %v\n%s", err, log)
			}
			info, err := NewRemoteExec(config.RemoteConfig{}).ProbeMedia(out)
			if err != nil || math.Abs(info.Duration-2) > .1 || info.Width != 160 || info.Height != 96 {
				t.Fatalf("output %+v %v", info, err)
			}
		})
	}
	if mergeFixtureSHA256(t, input) != hash {
		t.Fatal("source modified")
	}
}

func TestClipTransformAnimationChangesRenderedPixels(t *testing.T) {
	ffmpeg, err := localFFmpegPath()
	if err != nil {
		t.Skip(err)
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "pan.mp4")
	timeout := 45 * time.Second
	tr := &clipTransform{clipTransformPose: clipTransformPose{Zoom: .5}, Fit: "contain", Keyframes: []clipTransformKeyframe{{clipTransformPose: clipTransformPose{Zoom: .5, X: -.2}, Time: 0}, {clipTransformPose: clipTransformPose{Zoom: .5, X: .2}, Time: 1}}}
	graph := buildEditedVideoGraph([]mergeResolvedVideo{{SourceDuration: 1, Transform: tr}}, 160, 96)
	if log, err := runLocalProgram(ffmpeg, []string{"-y", "-f", "lavfi", "-i", "color=c=red:s=160x96:r=24:d=1", "-filter_complex_threads", "1", "-filter_complex", strings.Join(graph, ";"), "-map", "[v]", "-c:v", "libx264", "-threads", "1", "-pix_fmt", "yuv420p", out}, timeout); err != nil {
		t.Fatalf("render %v %s", err, log)
	}
	sample := func(at string) byte {
		t.Helper()
		file := filepath.Join(dir, "sample"+at+".rgb")
		if log, err := runLocalProgram(ffmpeg, []string{"-y", "-ss", at, "-i", out, "-vf", "crop=2:2:20:48", "-frames:v", "1", "-pix_fmt", "rgb24", "-f", "rawvideo", file}, timeout); err != nil {
			t.Fatalf("sample %v %s", err, log)
		}
		data, err := os.ReadFile(file)
		if err != nil || len(data) < 3 {
			t.Fatalf("sample bytes %v", err)
		}
		return data[0]
	}
	if first, last := sample("0"), sample("0.9"); first < 180 || last > 30 {
		t.Fatalf("pan did not move red away from x20: first=%d last=%d", first, last)
	}
}

func TestClipTransformJSONRoundtripInDraftAndSnapshot(t *testing.T) {
	raw := `{"scene_id":1,"duration":4,"transform":{"fit":"cover","zoom":1,"x":0,"y":0,"rotation":0,"crop_left":0.1,"keyframes":[{"time":0,"zoom":1,"x":0,"y":0,"rotation":0},{"time":4,"zoom":2,"x":0.2,"y":0,"rotation":90}]}}`
	var draft editingDraftClip
	if err := json.Unmarshal([]byte(raw), &draft); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal([]mergeEditClip{draft.mergeEditClip})
	if err != nil {
		t.Fatal(err)
	}
	var clips []mergeEditClip
	if err := json.Unmarshal(encoded, &clips); err != nil {
		t.Fatal(err)
	}
	tr := resolvedVideoFromClip(clips[0], mergeResolvedVideo{SourceDuration: 4}).Transform
	if tr == nil || len(tr.Keyframes) != 2 || tr.Keyframes[1].Rotation != 90 || tr.CropLeft != .1 {
		t.Fatalf("lost transform: %s", encoded)
	}
	if err := validateMergeClipEffects(resolvedVideoFromClip(clips[0], mergeResolvedVideo{SourceDuration: 4})); err != nil {
		t.Fatal(err)
	}
}
