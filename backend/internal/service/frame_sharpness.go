package service

import (
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
)

const (
	// Frames are scored on a downsampled copy; sharpness ranking does not need full resolution.
	sharpnessMaxSide = 480
	// Mean luma below this (0–255) is treated as a fade to black and never recommended.
	darkFrameMeanLuma = 18.0
)

// frameSharpness returns the variance of the 4-neighbour Laplacian over a grayscale,
// downsampled frame, plus its mean luma. Higher variance means more edge detail, so
// motion-blurred or ghosted tail frames score lower.
func frameSharpness(r io.Reader) (score, meanLuma float64, err error) {
	img, _, err := image.Decode(r)
	if err != nil {
		return 0, 0, err
	}
	b := img.Bounds()
	step := int(math.Ceil(float64(max(b.Dx(), b.Dy())) / sharpnessMaxSide))
	if step < 1 {
		step = 1
	}
	w, h := b.Dx()/step, b.Dy()/step
	if w < 3 || h < 3 {
		return 0, 0, nil
	}
	gray := make([]float64, w*h)
	var sum float64
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, bl, _ := img.At(b.Min.X+x*step, b.Min.Y+y*step).RGBA()
			v := (0.299*float64(r) + 0.587*float64(g) + 0.114*float64(bl)) / 257
			gray[y*w+x] = v
			sum += v
		}
	}
	meanLuma = sum / float64(w*h)
	var n, mean, m2 float64
	for y := 1; y < h-1; y++ {
		for x := 1; x < w-1; x++ {
			i := y*w + x
			lap := gray[i-w] + gray[i+w] + gray[i-1] + gray[i+1] - 4*gray[i]
			n++
			delta := lap - mean
			mean += delta / n
			m2 += delta * (lap - mean)
		}
	}
	return m2 / n, meanLuma, nil
}

// recommendSharpestFrame returns the index of the highest-scoring frame that is not a
// fade to black, or -1 when no frame qualifies. Ties keep the later frame, which sits
// closer to the cut the next Scene continues from.
func recommendSharpestFrame(scores, lumas []float64) int {
	best := -1
	for i := range scores {
		if scores[i] < 0 || lumas[i] < darkFrameMeanLuma {
			continue
		}
		if best < 0 || scores[i] >= scores[best] {
			best = i
		}
	}
	return best
}
