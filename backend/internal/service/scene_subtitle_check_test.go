package service

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
)

const testFrameW, testFrameH = 640, 360

// textLine paints subtitle-like strokes (3 px wide, 6 px period) in the given horizontal
// spans of a band at 85–90% of the frame height.
type textLine [][2]float64

var (
	subtitleLineA = textLine{{0.25, 0.75}}
	subtitleLineB = textLine{{0.35, 0.55}, {0.60, 0.65}}
)

func syntheticFrame(line textLine) grayFrame {
	f := grayFrame{W: testFrameW, H: testFrameH, Pix: make([]byte, testFrameW*testFrameH)}
	for y := 0; y < f.H; y++ {
		for x := 0; x < f.W; x++ {
			f.Pix[y*f.W+x] = byte(60 + 60*y/f.H) // smooth gradient, no edges
		}
	}
	y0, y1 := f.H*85/100, f.H*90/100
	for _, span := range line {
		for x := int(span[0] * float64(f.W)); x < int(span[1]*float64(f.W)); x++ {
			if x%6 < 3 {
				for y := y0; y < y1; y++ {
					f.Pix[y*f.W+x] = 240
				}
			}
		}
	}
	return f
}

func TestFindSubtitleBandLocatesLowerCentreTextOnly(t *testing.T) {
	band := findSubtitleBand(syntheticFrame(subtitleLineA))
	if band == nil {
		t.Fatal("subtitle-like strip not found")
	}
	if band.Y0 < testFrameH*80/100 || band.Y1 > testFrameH*92/100 {
		t.Fatalf("band rows %d–%d outside the painted strip", band.Y0, band.Y1)
	}
	if findSubtitleBand(syntheticFrame(nil)) != nil {
		t.Fatal("plain gradient must have no band")
	}
	// Text in the top half (a title card or signboard) is outside the subtitle area.
	top := syntheticFrame(nil)
	for x := 160; x < 480; x++ {
		if x%6 < 3 {
			for y := 40; y < 58; y++ {
				top.Pix[y*top.W+x] = 240
			}
		}
	}
	if findSubtitleBand(top) != nil {
		t.Fatal("text in the upper half must not count as a subtitle")
	}
	// Dense texture over the whole lower half (grass, rain) has no isolated text band.
	texture := syntheticFrame(nil)
	for y := testFrameH / 2; y < testFrameH; y++ {
		for x := 0; x < testFrameW; x++ {
			if (x+y)%5 < 2 {
				texture.Pix[y*texture.W+x] = 240
			}
		}
	}
	if findSubtitleBand(texture) != nil {
		t.Fatal("uniform texture must not count as a subtitle")
	}
	// A small off-centre object at subtitle height is not a subtitle line.
	logo := syntheticFrame(textLine{{0.80, 0.84}})
	if findSubtitleBand(logo) != nil {
		t.Fatal("off-centre narrow mark must not count as a subtitle")
	}
}

func TestClassifySubtitleSamplesNeedsChangingTextForSubtitles(t *testing.T) {
	frames := func(lines ...textLine) ([]float64, []*subtitleBand) {
		times := make([]float64, len(lines))
		bands := make([]*subtitleBand, len(lines))
		for i, line := range lines {
			times[i] = float64(i) * 0.5
			bands[i] = findSubtitleBand(syntheticFrame(line))
		}
		return times, bands
	}
	times, bands := frames(subtitleLineA, subtitleLineA, nil, nil, subtitleLineB, subtitleLineB)
	status, _, hints := classifySubtitleSamples(times, 0.5, bands, testFrameH)
	if status != subtitleStatusSubtitles {
		t.Fatalf("changing lines classified as %q", status)
	}
	if len(hints) != 2 || hints[0].Start != 0 || hints[0].End != 1 || hints[1].Start != 2 || hints[1].End != 3 {
		t.Fatalf("hints must cover each subtitle run: %+v", hints)
	}
	times, bands = frames(subtitleLineA, subtitleLineA, subtitleLineA, subtitleLineA)
	if status, _, hints := classifySubtitleSamples(times, 0.5, bands, testFrameH); status != subtitleStatusUncertain || len(hints) != 1 {
		t.Fatalf("static text must be uncertain with a hint, got %q %+v", status, hints)
	}
	times, bands = frames(nil, nil, nil)
	if status, _, hints := classifySubtitleSamples(times, 0.5, bands, testFrameH); status != subtitleStatusClean || len(hints) != 0 {
		t.Fatalf("no text must be clean, got %q %+v", status, hints)
	}
}

// writeSyntheticVideo encodes the frames at 2 fps with ffmpeg.
func writeSyntheticVideo(t *testing.T, dir, name string, lines []textLine) string {
	t.Helper()
	ffmpeg, err := localFFmpegPath()
	if err != nil {
		t.Skip("ffmpeg not available")
	}
	frameDir := t.TempDir()
	for i, line := range lines {
		f := syntheticFrame(line)
		img := image.NewGray(image.Rect(0, 0, f.W, f.H))
		copy(img.Pix, f.Pix)
		file, err := os.Create(filepath.Join(frameDir, fmt.Sprintf("f_%03d.png", i)))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(file, img); err != nil {
			t.Fatal(err)
		}
		file.Close()
	}
	out := filepath.Join(dir, name)
	args := []string{"-hide_banner", "-loglevel", "error", "-y", "-framerate", "2", "-i", filepath.Join(frameDir, "f_%03d.png"),
		"-c:v", "mpeg4", "-q:v", "2", "-pix_fmt", "yuv420p", out}
	if output, err := runLocalProgram(ffmpeg, args, time.Minute); err != nil {
		t.Skipf("ffmpeg cannot encode test video: %v %s", err, output)
	}
	return out
}

func TestDetectBurnedSubtitlesOnEncodedVideo(t *testing.T) {
	dir := t.TempDir()
	subtitled := writeSyntheticVideo(t, dir, "sub.mp4", []textLine{subtitleLineA, subtitleLineA, subtitleLineA, nil, nil, subtitleLineB, subtitleLineB, subtitleLineB})
	status, _, hints, samples, err := detectBurnedSubtitles(subtitled)
	if err != nil {
		t.Fatal(err)
	}
	if status != subtitleStatusSubtitles || samples < 6 || len(hints) < 2 {
		t.Fatalf("subtitled video: status=%q samples=%d hints=%+v", status, samples, hints)
	}
	clean := writeSyntheticVideo(t, dir, "clean.mp4", []textLine{nil, nil, nil, nil, nil, nil})
	if status, _, hints, _, err := detectBurnedSubtitles(clean); err != nil || status != subtitleStatusClean || len(hints) != 0 {
		t.Fatalf("clean video: status=%q hints=%+v err=%v", status, hints, err)
	}
}

func TestSceneSubtitleCheckDecisionFollowsCurrentVideo(t *testing.T) {
	if _, err := localFFmpegPath(); err != nil {
		t.Skip("ffmpeg not available")
	}
	ps := newTestProjectService(t)
	if err := ps.db.AutoMigrate(&models.SceneSubtitleCheck{}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	cfg := &config.Config{Comfy: config.ComfyConfig{ComfyDir: filepath.Join(root, "ComfyUI")}, Storage: config.StorageConfig{DataDir: filepath.Join(root, "data")}}
	remote := NewRemoteExec(config.RemoteConfig{})
	remote.SetLocalRoots([]string{cfg.Comfy.ComfyDir, cfg.Storage.DataDir, cfg.AssetsRoot()})
	ps.upload = NewUploadManager(cfg, ps.db, remote)
	p := models.Project{Title: "字幕检测"}
	ps.db.Create(&p)
	videoDir := filepath.Join(cfg.AssetsRoot(), fmt.Sprint(p.ID))
	if err := os.MkdirAll(videoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSyntheticVideo(t, videoDir, "scene.mp4", []textLine{subtitleLineA, subtitleLineA, subtitleLineA, subtitleLineA})
	scene := models.Scene{ProjectID: p.ID, EpisodeN: 1, Order: 1, VideoInputFile: "scene.mp4"}
	ps.db.Create(&scene)

	if view, err := ps.SceneSubtitleCheck(&p, &scene); err != nil || view != nil {
		t.Fatalf("unchecked Scene must have no result: %+v %v", view, err)
	}
	if _, err := ps.DecideSceneSubtitleCheck(&p, &scene, "no_subtitles"); err == nil {
		t.Fatal("a decision without a check must be rejected")
	}
	view, err := ps.RunSceneSubtitleCheck(&p, &scene)
	if err != nil {
		t.Fatal(err)
	}
	if view.Status != subtitleStatusUncertain || view.Stale || view.Decision != "" {
		t.Fatalf("static text video: %+v", view)
	}
	if _, err := ps.DecideSceneSubtitleCheck(&p, &scene, "maybe"); err == nil {
		t.Fatal("unknown decision accepted")
	}
	decided, err := ps.DecideSceneSubtitleCheck(&p, &scene, "no_subtitles")
	if err != nil || decided.Decision != "no_subtitles" || decided.DecidedAt == nil {
		t.Fatalf("decision not saved: %+v %v", decided, err)
	}
	// Re-checking replaces the analysis and clears the manual decision; the video stays untouched.
	again, err := ps.RunSceneSubtitleCheck(&p, &scene)
	if err != nil || again.Decision != "" {
		t.Fatalf("re-check must clear the decision: %+v %v", again, err)
	}
	var count int64
	ps.db.Model(&models.SceneSubtitleCheck{}).Count(&count)
	if count != 1 {
		t.Fatalf("one row per Scene, got %d", count)
	}
	// Replacing the video marks the old result stale and blocks a decision on it.
	writeSyntheticVideo(t, videoDir, "scene.mp4", []textLine{nil, nil, nil, nil, nil})
	stale, err := ps.SceneSubtitleCheck(&p, &scene)
	if err != nil || !stale.Stale {
		t.Fatalf("replaced video must mark the result stale: %+v %v", stale, err)
	}
	if _, err := ps.DecideSceneSubtitleCheck(&p, &scene, "has_subtitles"); err == nil {
		t.Fatal("a decision on a stale result must be rejected")
	}
	var after models.Scene
	ps.db.First(&after, scene.ID)
	if after.VideoInputFile != "scene.mp4" {
		t.Fatalf("subtitle check must not change the Scene video: %q", after.VideoInputFile)
	}
}
