package service

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func encodeTestFrame(t *testing.T, fill func(x, y int) uint8) *bytes.Buffer {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, 96, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 96; x++ {
			img.SetGray(x, y, color.Gray{Y: fill(x, y)})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return &buf
}

func TestFrameSharpnessRanksSharpOverBlurredAndSkipsBlack(t *testing.T) {
	sharp := encodeTestFrame(t, func(x, y int) uint8 {
		if (x/4+y/4)%2 == 0 {
			return 220
		}
		return 40
	})
	// A smooth ramp has the same mean brightness range but almost no edge detail.
	blurred := encodeTestFrame(t, func(x, y int) uint8 { return uint8(40 + x*180/95) })
	black := encodeTestFrame(t, func(x, y int) uint8 {
		if (x/4+y/4)%2 == 0 {
			return 6
		}
		return 0
	})

	var scores, lumas []float64
	for _, frame := range []*bytes.Buffer{blurred, sharp, black} {
		score, luma, err := frameSharpness(frame)
		if err != nil {
			t.Fatal(err)
		}
		scores, lumas = append(scores, score), append(lumas, luma)
	}
	if !(scores[1] > scores[0]) {
		t.Fatalf("sharp frame should outscore blurred: sharp=%f blurred=%f", scores[1], scores[0])
	}
	if lumas[2] >= darkFrameMeanLuma {
		t.Fatalf("black frame luma %f should be below the fade threshold", lumas[2])
	}
	if got := recommendSharpestFrame(scores, lumas); got != 1 {
		t.Fatalf("recommended frame = %d, want sharp frame 1 (scores=%v lumas=%v)", got, scores, lumas)
	}
}

func TestRecommendSharpestFrameSkipsUnreadableAndAllDark(t *testing.T) {
	if got := recommendSharpestFrame([]float64{-1, 50, 50}, []float64{0, 120, 120}); got != 2 {
		t.Fatalf("tie should keep the later frame, got %d", got)
	}
	if got := recommendSharpestFrame([]float64{900, 800}, []float64{5, 10}); got != -1 {
		t.Fatalf("all dark frames must not be recommended, got %d", got)
	}
	if _, _, err := frameSharpness(bytes.NewBufferString("not an image")); err == nil {
		t.Fatal("undecodable input should return an error")
	}
}
