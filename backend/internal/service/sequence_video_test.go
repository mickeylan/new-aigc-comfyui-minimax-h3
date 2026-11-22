package service

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSequenceFrameOrderingRejectsGapsAndShortSequences(t *testing.T) {
	names := []string{
		"clay-seq-20261122-174038-002.png",
		"clay-seq-20261122-174038-000.png",
		"depth-seq-20261122-174038-001.png",
		"clay-seq-20261122-174038-001.png",
		"clay-seq-20261122-174039-003.png",
	}
	ordered, err := orderSequenceFrames(names, "clay", "20261122-174038")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"clay-seq-20261122-174038-000.png",
		"clay-seq-20261122-174038-001.png",
		"clay-seq-20261122-174038-002.png",
	}
	if strings.Join(ordered, ",") != strings.Join(want, ",") {
		t.Fatalf("frames = %v, want %v", ordered, want)
	}

	// 缺第 001 帧必须拒绝：ffmpeg 遇到缺口会静默截断序列。
	gap := []string{"clay-seq-20261122-174038-000.png", "clay-seq-20261122-174038-002.png"}
	if _, err := orderSequenceFrames(gap, "clay", "20261122-174038"); err == nil || !strings.Contains(err.Error(), "缺第 001 帧") {
		t.Fatalf("gap must be rejected, got %v", err)
	}

	one := []string{"clay-seq-20261122-174038-000.png"}
	if _, err := orderSequenceFrames(one, "clay", "20261122-174038"); err == nil || !strings.Contains(err.Error(), "至少需要 2 帧") {
		t.Fatalf("single frame must be rejected, got %v", err)
	}
}

func TestSequenceVideoNamingAndEncodingArgs(t *testing.T) {
	pass, err := normalizeSequencePass(" Depth ")
	if err != nil || pass != "depth" {
		t.Fatalf("pass = %q, err = %v", pass, err)
	}
	if _, err := normalizeSequencePass("exec"); err == nil {
		t.Fatal("unknown pass must be rejected so the name cannot escape the materials dir")
	}
	if got := normalizeSequenceFPS(0); got != sequenceVideoDefaultFPS {
		t.Fatalf("default fps = %d", got)
	}
	if got := normalizeSequenceFPS(240); got != sequenceVideoMaxFPS {
		t.Fatalf("fps clamp = %d", got)
	}
	if got := sequenceVideoName("clay", "20261122-174038"); got != "clay-seq-20261122-174038.mp4" {
		t.Fatalf("video name = %s", got)
	}
	args := strings.Join(sequenceVideoArgs("/tmp/clay-seq-1-%03d.png", 8, "/tmp/out.mp4"), " ")
	// 输入按采样帧率 8 fps 读，输出统一 24 fps：H3 的视频参考帧要求 24 fps。
	for _, want := range []string{"-framerate 8", "-start_number 0", "-c:v libx264", "-pix_fmt yuv420p", "-r 24", "/tmp/out.mp4"} {
		if !strings.Contains(args, want) {
			t.Fatalf("args missing %q: %s", want, args)
		}
	}
}

// TestComposeSequenceVideoEncodesRealMP4 用本机 ffmpeg 真跑一次：合成出来的文件要能被
// ffmpeg 读回、帧数与时长对得上，正好守住「帧号连续 / 码率可解码」这两条。
func TestComposeSequenceVideoEncodesRealMP4(t *testing.T) {
	ffmpeg, err := localFFmpegPath()
	if err != nil {
		t.Skipf("ffmpeg not available: %v", err)
	}
	dir := t.TempDir()
	const frames = 3
	for index := 0; index < frames; index++ {
		img := image.NewRGBA(image.Rect(0, 0, 64, 36))
		for x := 0; x < 64; x++ {
			img.Set(x, 10+index*6, color.RGBA{R: 255, A: 255})
		}
		name := filepath.Join(dir, "clay-seq-20261122-174038-"+fmt.Sprintf("%03d", index)+".png")
		file, err := os.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(file, img); err != nil {
			file.Close()
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
	}
	outPath := filepath.Join(dir, "out.mp4")
	pattern := filepath.Join(dir, "clay-seq-20261122-174038-%03d.png")
	if output, err := runLocalProgram(ffmpeg, sequenceVideoArgs(pattern, 8, outPath), sequenceVideoTimeout); err != nil {
		t.Fatalf("ffmpeg failed: %v %s", err, output)
	}
	info, err := os.Stat(outPath)
	if err != nil || info.Size() == 0 {
		t.Fatalf("mp4 missing: %v", err)
	}
	probe, _ := runLocalProgram(ffmpeg, []string{"-hide_banner", "-i", outPath}, sequenceVideoTimeout)
	if !strings.Contains(probe, "Video: h264") {
		t.Fatalf("mp4 is not h264: %s", probe)
	}
	if !strings.Contains(probe, "24 fps") {
		t.Fatalf("mp4 must be re-timed to 24 fps for H3 reference frames: %s", probe)
	}
	if !strings.Contains(probe, "Duration: 00:00:00.38") {
		t.Fatalf("3 frames at 8 fps must stay 0.375s after the 24 fps re-time: %s", probe)
	}
}