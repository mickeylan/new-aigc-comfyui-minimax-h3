package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"comfyui-console/internal/config"
	"comfyui-console/internal/indextts"
	"comfyui-console/internal/models"
)

type fakeIndexVoice struct{}

func (fakeIndexVoice) Info() (indextts.VoiceInfo, error) {
	return indextts.VoiceInfo{ReferenceSHA256: "voice-hash"}, nil
}
func (fakeIndexVoice) Close() error { return nil }

type fakeIndexModel struct {
	prepared string
	options  indextts.Options
}

func (m *fakeIndexModel) PrepareVoice(path string) (indextts.Voice, error) {
	m.prepared = path
	return fakeIndexVoice{}, nil
}
func (m *fakeIndexModel) GenerateContext(_ context.Context, _ indextts.Voice, _ string, options indextts.Options) (indextts.GenerationResult, error) {
	m.options = options
	return indextts.GenerationResult{Audio: indextts.Audio{Samples: []float32{-0.5, 0, 0.5}, SampleRate: 22050, Channels: 1}, Info: indextts.GenerationInfo{SemanticTokens: 3, GeneratedSeconds: 1}}, nil
}
func (m *fakeIndexModel) Capabilities() indextts.Capabilities {
	return indextts.Capabilities{SupportsVoiceCache: true, SupportsEmotionText: true}
}
func (m *fakeIndexModel) Info() (indextts.ModelInfo, error) {
	return indextts.ModelInfo{RuntimeVersion: "test", ModelVersion: "model", ManifestSHA256: "manifest"}, nil
}
func (m *fakeIndexModel) Health() (indextts.Health, error) {
	return indextts.Health{Loaded: true, DeviceHealthy: true}, nil
}
func (m *fakeIndexModel) Cancel() error   { return nil }
func (m *fakeIndexModel) Close() error    { return nil }
func (m *fakeIndexModel) Version() string { return "test" }

func TestLocalIndexTTSStartupFailureIsReportedWithoutBecomingAvailable(t *testing.T) {
	local := NewLocalIndexTTS(&config.Config{IndexTTS: config.IndexTTSConfig{Enabled: true}}, safetyDB(t))
	if err := local.Start(); err == nil {
		t.Fatal("expected missing model_dir error")
	}
	configured, available, statusError := local.Status()
	if !configured || available || statusError == "" {
		t.Fatalf("unexpected status configured=%v available=%v error=%q", configured, available, statusError)
	}
	if _, err := local.Synthesize(context.Background(), models.Dialogue{Text: "test"}); err == nil {
		t.Fatal("unavailable runtime accepted synthesis")
	}
}

func TestLocalIndexTTSStartDefersCUDAmodelLoad(t *testing.T) {
	modelDir := t.TempDir()
	local := NewLocalIndexTTS(&config.Config{IndexTTS: config.IndexTTSConfig{Enabled: true, ModelDir: modelDir}}, safetyDB(t))
	loads := 0
	model := &fakeIndexModel{}
	local.loadModel = func(path string, device int) (indextts.Model, error) {
		loads++
		if path != modelDir || device != 0 {
			t.Fatalf("unexpected load path/device: %s %d", path, device)
		}
		return model, nil
	}
	if err := local.Start(); err != nil {
		t.Fatal(err)
	}
	if loads != 0 || local.model != nil {
		t.Fatalf("startup eagerly loaded CUDA model: loads=%d model=%v", loads, local.model)
	}
	configured, available, statusError := local.Status()
	if !configured || !available || statusError != "" {
		t.Fatalf("lazy runtime not ready: configured=%v available=%v error=%q", configured, available, statusError)
	}
	if err := local.ensureModelLoaded(); err != nil {
		t.Fatal(err)
	}
	if loads != 1 || local.model == nil {
		t.Fatalf("first use did not load model once: loads=%d", loads)
	}
	if err := local.ensureModelLoaded(); err != nil || loads != 1 {
		t.Fatalf("model reloaded: loads=%d err=%v", loads, err)
	}
}

func TestLocalIndexTTSSynthesizesCharacterVoiceRef(t *testing.T) {
	db := safetyDB(t, &models.Character{}, &models.Task{})
	root := t.TempDir()
	voiceDir := filepath.Join(root, "input", "1")
	if err := os.MkdirAll(voiceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(voiceDir, "voice.wav"), []byte("fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
	db.Create(&models.Character{ProjectID: 1, Name: "林夏", VoiceRef: "voice.wav"})
	model := &fakeIndexModel{}
	local := NewLocalIndexTTS(&config.Config{Comfy: config.ComfyConfig{ComfyDir: root}, IndexTTS: config.IndexTTSConfig{Enabled: true, Language: "ZH", QueueSize: 1}}, db)
	local.model, local.fingerprint, local.capabilities = model, "runtime", model.Capabilities()
	result, err := local.Synthesize(context.Background(), models.Dialogue{ProjectID: 1, Character: "林夏", Text: "你好", Emotion: "温柔", Delivery: "轻声"})
	if err != nil {
		t.Fatal(err)
	}
	if string(result.WAV[:4]) != "RIFF" || model.prepared != filepath.Join(voiceDir, "voice.wav") {
		t.Fatalf("unexpected local output/path: %q %q", result.WAV[:4], model.prepared)
	}
	if model.options.EmotionText != "温柔；轻声" || result.Info.SemanticTokens != 3 {
		t.Fatalf("emotion or diagnostics not propagated: options=%+v info=%+v", model.options, result.Info)
	}
}

func TestLocalIndexTTSRefusesBusyComfyGPU(t *testing.T) {
	db := safetyDB(t, &models.Character{}, &models.Task{})
	gpu := 0
	db.Create(&models.Task{TaskID: "busy", Status: "running", GPUIndex: &gpu})
	local := NewLocalIndexTTS(&config.Config{IndexTTS: config.IndexTTSConfig{Enabled: true, DeviceIndex: 0}}, db)
	if err := local.ensureGPUAvailable(); err == nil {
		t.Fatal("busy ComfyUI GPU was accepted")
	}
}

func TestEffectiveDialogueAudioHashIncludesLocalRuntimeAndVoice(t *testing.T) {
	db := safetyDB(t, &models.Character{}, &models.Task{})
	root := t.TempDir()
	voiceDir := filepath.Join(root, "input", "1")
	_ = os.MkdirAll(voiceDir, 0o755)
	voice := filepath.Join(voiceDir, "voice.wav")
	_ = os.WriteFile(voice, []byte("a"), 0o644)
	db.Create(&models.Character{ProjectID: 1, Name: "林夏", VoiceRef: "voice.wav"})
	local := NewLocalIndexTTS(&config.Config{Comfy: config.ComfyConfig{ComfyDir: root}, IndexTTS: config.IndexTTSConfig{Enabled: true}}, db)
	local.fingerprint = "runtime-a"
	service := &ProjectService{db: db, indexTTS: local}
	dialogue := models.Dialogue{ProjectID: 1, Character: "林夏", Text: "你好", Speed: 1, Volume: 1}
	first := service.effectiveDialogueAudioHash(dialogue)
	local.fingerprint = "runtime-b"
	second := service.effectiveDialogueAudioHash(dialogue)
	if first == second {
		t.Fatal("runtime fingerprint did not invalidate audio hash")
	}
}
