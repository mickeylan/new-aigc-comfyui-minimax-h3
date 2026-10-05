//go:build windows && cgo && indextts

package indextts

import (
	"context"
	"math"
	"os"
	"testing"
	"time"
)

func TestNativeCUDAEndToEnd(t *testing.T) {
	modelDir := os.Getenv("INDEXTTS_TEST_MODEL")
	voicePath := os.Getenv("INDEXTTS_TEST_VOICE")
	if modelDir == "" || voicePath == "" {
		t.Skip("set INDEXTTS_TEST_MODEL and INDEXTTS_TEST_VOICE")
	}
	model, err := LoadCUDA(modelDir, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer model.Close()
	voice, err := model.PrepareVoice(voicePath)
	if err != nil {
		t.Fatal(err)
	}
	defer voice.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	result, err := model.GenerateContext(ctx, voice, "这是平台本地配音端到端测试。", Options{Language: "ZH", Seed: 1234, DurationFactor: 1, EmotionText: "沉稳、清晰", EmotionStrength: 1})
	if err != nil {
		t.Fatal(err)
	}
	audio := result.Audio
	if audio.SampleRate != 22050 || audio.Channels != 1 || len(audio.Samples) == 0 {
		t.Fatalf("invalid output: rate=%d channels=%d samples=%d", audio.SampleRate, audio.Channels, len(audio.Samples))
	}
	peak := float32(0)
	for _, sample := range audio.Samples {
		if math.IsNaN(float64(sample)) || math.IsInf(float64(sample), 0) {
			t.Fatal("non-finite output")
		}
		if value := float32(math.Abs(float64(sample))); value > peak {
			peak = value
		}
	}
	if peak == 0 {
		t.Fatal("silent output")
	}
	capabilities := model.Capabilities()
	info, _ := model.Info()
	voiceInfo, _ := voice.Info()
	health, _ := model.Health()
	t.Logf("generated samples=%d seconds=%.3f peak=%.6f semantic_tokens=%d total=%s abi=%d.%d runtime=%s model=%s voice=%s cache_entries=%d", len(audio.Samples), float64(len(audio.Samples))/22050, peak, result.Info.SemanticTokens, result.Info.Total, capabilities.ABIMajor, capabilities.ABIMinor, info.RuntimeVersion, info.ModelVersion, voiceInfo.ReferenceSHA256, health.VoiceCacheEntries)
}
