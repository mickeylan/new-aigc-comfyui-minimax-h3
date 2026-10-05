package indextts

import (
	"context"
	"time"
)

// Audio owns normalized interleaved float PCM returned by the native runtime.
type Audio struct {
	Samples    []float32
	SampleRate uint32
	Channels   uint32
}

type Options struct {
	Language        string
	Seed            uint64
	DurationFactor  float32
	EmotionText     string
	EmotionStrength float32
}

type Capabilities struct {
	ABIMajor, ABIMinor                                                                       uint32
	SampleRate, MaxReferenceSeconds, MaxSemanticTokens, MaxConcurrentRequests                uint32
	SupportsCUDA, SupportsCPU, SupportsCancellation, SupportsRequestCancel                   bool
	SupportsVoiceCache, SupportsEmotionText, SupportsEmotionReference, SupportsEmotionVector bool
	SupportsTargetDuration, SupportsSampling, SupportsBeamSearch                             bool
}

type ModelInfo struct {
	RuntimeVersion, ModelVersion, ManifestSHA256, Backend, Device string
}

type Health struct {
	Loaded, DeviceHealthy                                              bool
	VoiceCacheEntries, VoiceCacheBytes, ActiveRequests, QueuedRequests uint64
	LastError                                                          string
}

type VoiceInfo struct {
	ReferenceSHA256                  string
	DurationSeconds                  float32
	SourceSampleRate, SourceChannels uint32
	CacheBytes                       uint64
}

type GenerationInfo struct {
	SemanticTokens                                             uint32
	GeneratedSeconds                                           float32
	ReferenceEncode, GPT, SemanticCodec, S2Mel, BigVGAN, Total time.Duration
	Peak, RMS, SilenceRatio                                    float32
	Seed                                                       uint64
}

type GenerationResult struct {
	Audio Audio
	Info  GenerationInfo
}

type Voice interface {
	Info() (VoiceInfo, error)
	Close() error
}

type Model interface {
	PrepareVoice(path string) (Voice, error)
	GenerateContext(ctx context.Context, voice Voice, text string, options Options) (GenerationResult, error)
	Capabilities() Capabilities
	Info() (ModelInfo, error)
	Health() (Health, error)
	Cancel() error
	Close() error
	Version() string
}
