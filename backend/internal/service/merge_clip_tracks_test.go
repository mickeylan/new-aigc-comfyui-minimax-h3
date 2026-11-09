package service

import (
	"comfyui-console/internal/models"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClipTracksValidateOwnershipAndBounds(t *testing.T) {
	v := mergeResolvedVideo{SourceDuration: 2, VisualTracks: []clipVisualTrack{{MaterialID: 1, Start: .2, End: 1.5, Width: .5, Opacity: 1}}, VolumeEnvelope: []clipVolumePoint{{Time: 0, Gain: 0}, {Time: 2, Gain: 1}}, Markers: []clipMarker{{Time: 1, Label: "beat"}}}
	if err := validateMergeClipEffects(v); err != nil {
		t.Fatal(err)
	}
	bad := v
	bad.VisualTracks = append([]clipVisualTrack(nil), v.VisualTracks...)
	bad.VisualTracks[0].End = 3
	if validateMergeClipEffects(bad) == nil {
		t.Fatal("overflow accepted")
	}
	bad = v
	bad.VolumeEnvelope = []clipVolumePoint{{Time: 1, Gain: 1}, {Time: 0, Gain: 2}}
	if validateMergeClipEffects(bad) == nil {
		t.Fatal("unordered envelope accepted")
	}
	db := newTestDB(t)
	db.AutoMigrate(&models.Material{})
	other, own := uint(2), uint(1)
	m := models.Material{ProjectID: &other, Type: "image", Path: "2/other.png"}
	db.Create(&m)
	if _, err := fingerprintClipTracks(db, 1, []mergeEditClip{{VisualTracks: []clipVisualTrack{{MaterialID: m.ID}}}}); err == nil {
		t.Fatal("cross project track accepted")
	}
	m.ProjectID = &own
	db.Save(&m)
	fp, err := fingerprintClipTracks(db, 1, []mergeEditClip{{VisualTracks: []clipVisualTrack{{MaterialID: m.ID}}}})
	if err != nil || len(fp) != 1 || fp[0].Path != "2/other.png" {
		t.Fatalf("fingerprint %+v %v", fp, err)
	}
}
func TestClipTracksRealFFmpegTimingOrderAndEnvelope(t *testing.T) {
	ff, err := localFFmpegPath()
	if err != nil {
		t.Skip(err)
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "tracks.mp4")
	timeout := 45 * time.Second
	v := mergeResolvedVideo{SourceDuration: 2, VisualTracks: []clipVisualTrack{{MaterialID: 1, Start: .25, End: 1.75, Width: .5, Opacity: 1, InputIndex: 1}, {MaterialID: 2, Start: .75, End: 1.25, Width: .5, Opacity: 1, InputIndex: 2}}, VolumeEnvelope: []clipVolumePoint{{Time: 0, Gain: 0}, {Time: .25, Gain: 0}, {Time: .5, Gain: 1}, {Time: 2, Gain: 1}}}
	graph := buildEditedVideoGraph([]mergeResolvedVideo{v}, 160, 96)
	audio := buildEditedNativeAudioGraph([]mergeResolvedVideo{v}, []bool{true}, 2, 1)
	// Input zero is a black video with a tone. Two independent visual tracks cover
	// the same region; later tracks must win only within their own active windows.
	source := filepath.Join(dir, "source.mp4")
	if log, err := runLocalProgram(ff, []string{"-y", "-f", "lavfi", "-i", "color=c=black:s=160x96:r=24:d=2", "-f", "lavfi", "-i", "sine=frequency=440:duration=2", "-c:v", "libx264", "-threads", "1", "-c:a", "aac", source}, timeout); err != nil {
		t.Fatalf("source %v %s", err, log)
	}
	hash := mergeFixtureSHA256(t, source)
	filters := strings.Join(append(graph, audio.Filters), ";")
	if log, err := runLocalProgram(ff, []string{"-y", "-i", source, "-f", "lavfi", "-i", "color=c=red:s=80x48:r=24:d=2", "-f", "lavfi", "-i", "color=c=blue:s=80x48:r=24:d=2", "-filter_complex_threads", "1", "-filter_complex", filters, "-map", "[v]", "-map", audio.Label, "-c:v", "libx264", "-threads", "1", "-pix_fmt", "yuv420p", "-c:a", "aac", out}, timeout); err != nil {
		t.Fatalf("render %v\n%s", err, log)
	}
	pixel := func(at string) []byte {
		t.Helper()
		file := filepath.Join(dir, at+".rgb")
		if log, err := runLocalProgram(ff, []string{"-y", "-ss", at, "-i", out, "-vf", "crop=2:2:20:20", "-frames:v", "1", "-pix_fmt", "rgb24", "-f", "rawvideo", file}, timeout); err != nil {
			t.Fatalf("pixel %v %s", err, log)
		}
		data, err := os.ReadFile(file)
		if err != nil || len(data) < 3 {
			t.Fatal("pixel absent")
		}
		return data[:3]
	}
	before, red, blue, after := pixel("0.1"), pixel("0.5"), pixel("1"), pixel("1.9")
	if before[0] > 30 || red[0] < 180 || blue[2] < 180 || after[2] > 30 || after[0] > 30 {
		t.Fatalf("timing/layer order %v %v %v %v", before, red, blue, after)
	}
	raw := filepath.Join(dir, "audio.raw")
	if log, err := runLocalProgram(ff, []string{"-y", "-i", out, "-vn", "-ac", "1", "-ar", "48000", "-f", "s16le", raw}, timeout); err != nil {
		t.Fatalf("audio %v %s", err, log)
	}
	data, _ := os.ReadFile(raw)
	rms := func(start, end float64) float64 {
		sum := 0.0
		n := 0
		for i := int(start*48000) * 2; i < int(end*48000)*2 && i+1 < len(data); i += 2 {
			x := float64(int16(binary.LittleEndian.Uint16(data[i : i+2])))
			sum += x * x
			n++
		}
		return math.Sqrt(sum / math.Max(1, float64(n)))
	}
	if quiet, loud := rms(.05, .15), rms(.8, 1); loud < 500 || quiet > loud*.1 {
		t.Fatalf("envelope not applied quiet=%f loud=%f", quiet, loud)
	}
	if mergeFixtureSHA256(t, source) != hash {
		t.Fatal("source modified")
	}
}
