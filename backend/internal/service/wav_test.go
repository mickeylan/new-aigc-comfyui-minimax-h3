package service

import (
	"encoding/binary"
	"math"
	"testing"

	"comfyui-console/internal/indextts"
)

func TestEncodePCM16WAV(t *testing.T) {
	data, err := encodePCM16WAV(indextts.Audio{Samples: []float32{-1, 0, 1}, SampleRate: 22050, Channels: 1})
	if err != nil {
		t.Fatal(err)
	}
	if string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" || string(data[36:40]) != "data" {
		t.Fatalf("invalid WAV header: %q", data[:44])
	}
	if got := binary.LittleEndian.Uint32(data[24:28]); got != 22050 {
		t.Fatalf("sample rate=%d", got)
	}
	if got := int16(binary.LittleEndian.Uint16(data[44:46])); got != math.MinInt16 {
		t.Fatalf("negative clip=%d", got)
	}
	if got := int16(binary.LittleEndian.Uint16(data[48:50])); got != math.MaxInt16 {
		t.Fatalf("positive clip=%d", got)
	}
}

func TestEncodePCM16WAVRejectsInvalidAudio(t *testing.T) {
	cases := []indextts.Audio{
		{},
		{Samples: []float32{0}, SampleRate: 22050, Channels: 2},
		{Samples: []float32{float32(math.NaN())}, SampleRate: 22050, Channels: 1},
	}
	for _, audio := range cases {
		if _, err := encodePCM16WAV(audio); err == nil {
			t.Fatalf("expected error for %+v", audio)
		}
	}
}
