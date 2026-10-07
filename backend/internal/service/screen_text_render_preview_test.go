package service

import (
	"strings"
	"testing"
)

func TestScreenTextPreviewFFmpegArgsUseTransparentRGBAAndApprovedFonts(t *testing.T) {
	args := screenTextPreviewFFmpegArgs(1080, 1920, `C:\temp\preview.ass`, `C:\app\data\fonts`, `C:\temp\preview.png`)
	joined := strings.Join(args, " ")
	for _, want := range []string{"color=c=black@0.0:s=1080x1920:d=1,format=rgba", "subtitles=filename=", "fontsdir=", "alpha=1", "-c:v png", `C:\temp\preview.png`} {
		if !strings.Contains(joined, want) {
			t.Fatalf("preview args missing %q: %s", want, joined)
		}
	}
	if strings.Contains(joined, "color=c=black:") {
		t.Fatalf("opaque black background used: %s", joined)
	}
}

func TestScreenTextPreviewFFmpegArgsBoundedSingleFrame(t *testing.T) {
	args := screenTextPreviewFFmpegArgs(1920, 1080, "preview.ass", "fonts", "preview.png")
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-frames:v 1") || !strings.Contains(joined, "d=1") {
		t.Fatalf("preview is not bounded to one frame: %s", joined)
	}
}
