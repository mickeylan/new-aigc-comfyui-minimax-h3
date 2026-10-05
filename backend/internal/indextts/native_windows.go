//go:build windows && cgo && indextts

package indextts

/*
#cgo CFLAGS: -I.
#cgo LDFLAGS: -lindextts
#include "indextts.h"
#include <stdlib.h>
*/
import "C"

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"time"
	"unsafe"
)

type nativeModel struct {
	life         sync.RWMutex
	h            C.indextts_model_t
	capabilities Capabilities
}

type nativeVoice struct {
	mu    sync.Mutex
	model *nativeModel
	h     C.indextts_voice_t
}

func nativeError() error {
	if value := C.indextts_last_error(); value != nil {
		return errors.New(C.GoString(value))
	}
	return errors.New("IndexTTS native call failed")
}

// last_error is thread-local, therefore the call and lookup must use one OS thread.
func nativeCall(call func() C.int32_t) (C.int32_t, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	status := call()
	if status != C.INDEXTTS_OK {
		return status, nativeError()
	}
	return status, nil
}

func fixedString(pointer *C.char, length int) string {
	bytes := unsafe.Slice((*byte)(unsafe.Pointer(pointer)), length)
	end := 0
	for end < len(bytes) && bytes[end] != 0 {
		end++
	}
	return string(bytes[:end])
}

func runtimeCapabilities() (Capabilities, error) {
	var value C.indextts_capabilities_t
	if _, err := nativeCall(func() C.int32_t { return C.indextts_get_capabilities(&value) }); err != nil {
		return Capabilities{}, err
	}
	return Capabilities{
		ABIMajor: uint32(value.abi_major), ABIMinor: uint32(value.abi_minor), SampleRate: uint32(value.sample_rate),
		MaxReferenceSeconds: uint32(value.max_reference_seconds), MaxSemanticTokens: uint32(value.max_semantic_tokens), MaxConcurrentRequests: uint32(value.max_concurrent_requests_per_model),
		SupportsCUDA: value.supports_cuda != 0, SupportsCPU: value.supports_cpu != 0, SupportsCancellation: value.supports_cancellation != 0,
		SupportsRequestCancel: value.supports_request_cancellation != 0, SupportsVoiceCache: value.supports_voice_cache != 0,
		SupportsEmotionText: value.supports_emotion_text != 0, SupportsEmotionReference: value.supports_emotion_reference != 0,
		SupportsEmotionVector: value.supports_emotion_vector != 0, SupportsTargetDuration: value.supports_target_duration != 0,
		SupportsSampling: value.supports_sampling != 0, SupportsBeamSearch: value.supports_beam_search != 0,
	}, nil
}

func LoadCUDA(modelDir string, deviceIndex int) (Model, error) {
	if modelDir == "" {
		return nil, errors.New("IndexTTS model directory is empty")
	}
	if deviceIndex < 0 {
		return nil, errors.New("IndexTTS CUDA device index must be non-negative")
	}
	capabilities, err := runtimeCapabilities()
	if err != nil {
		return nil, err
	}
	if capabilities.ABIMajor != 1 {
		return nil, errors.New("unsupported IndexTTS ABI major version")
	}
	if !capabilities.SupportsCUDA {
		return nil, errors.New("IndexTTS runtime does not support CUDA")
	}
	path := C.CString(modelDir)
	defer C.free(unsafe.Pointer(path))
	var options C.indextts_model_options_t
	C.indextts_model_options_init(&options)
	options.model_dir = path
	options.device_index = C.int32_t(deviceIndex)
	var handle C.indextts_model_t
	if _, err := nativeCall(func() C.int32_t { return C.indextts_model_load(&options, &handle) }); err != nil {
		return nil, err
	}
	model := &nativeModel{h: handle, capabilities: capabilities}
	runtime.SetFinalizer(model, (*nativeModel).Close)
	return model, nil
}

func (m *nativeModel) Version() string            { return C.GoString(C.indextts_version()) }
func (m *nativeModel) Capabilities() Capabilities { return m.capabilities }

func (m *nativeModel) Info() (ModelInfo, error) {
	m.life.RLock()
	defer m.life.RUnlock()
	if m.h == nil {
		return ModelInfo{}, errors.New("IndexTTS model is closed")
	}
	var v C.indextts_model_info_t
	if _, err := nativeCall(func() C.int32_t { return C.indextts_model_get_info(m.h, &v) }); err != nil {
		return ModelInfo{}, err
	}
	return ModelInfo{fixedString(&v.runtime_version[0], 64), fixedString(&v.model_version[0], 64), fixedString(&v.model_manifest_sha256[0], 65), fixedString(&v.backend[0], 32), fixedString(&v.device[0], 32)}, nil
}

func (m *nativeModel) Health() (Health, error) {
	m.life.RLock()
	defer m.life.RUnlock()
	if m.h == nil {
		return Health{}, errors.New("IndexTTS model is closed")
	}
	var v C.indextts_health_t
	if _, err := nativeCall(func() C.int32_t { return C.indextts_model_health(m.h, &v) }); err != nil {
		return Health{}, err
	}
	return Health{v.loaded != 0, v.device_healthy != 0, uint64(v.voice_cache_entries), uint64(v.voice_cache_bytes), uint64(v.active_requests), uint64(v.queued_requests), fixedString(&v.last_error[0], 512)}, nil
}

func (m *nativeModel) PrepareVoice(path string) (Voice, error) {
	m.life.RLock()
	defer m.life.RUnlock()
	if m.h == nil {
		return nil, errors.New("IndexTTS model is closed")
	}
	value := C.CString(path)
	defer C.free(unsafe.Pointer(value))
	var handle C.indextts_voice_t
	if _, err := nativeCall(func() C.int32_t { return C.indextts_voice_prepare(m.h, value, &handle) }); err != nil {
		return nil, err
	}
	voice := &nativeVoice{model: m, h: handle}
	runtime.SetFinalizer(voice, (*nativeVoice).Close)
	return voice, nil
}

func (v *nativeVoice) Info() (VoiceInfo, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.h == nil {
		return VoiceInfo{}, errors.New("IndexTTS voice is closed")
	}
	var x C.indextts_voice_info_t
	if _, err := nativeCall(func() C.int32_t { return C.indextts_voice_get_info(v.h, &x) }); err != nil {
		return VoiceInfo{}, err
	}
	return VoiceInfo{fixedString(&x.reference_sha256[0], 65), float32(x.duration_seconds), uint32(x.source_sample_rate), uint32(x.source_channels), uint64(x.cache_bytes)}, nil
}

func (m *nativeModel) GenerateContext(ctx context.Context, voice Voice, text string, options Options) (GenerationResult, error) {
	v, ok := voice.(*nativeVoice)
	if !ok || v == nil {
		return GenerationResult{}, errors.New("IndexTTS voice does not belong to the native model")
	}
	if ctx == nil {
		return GenerationResult{}, errors.New("context is nil")
	}
	m.life.RLock()
	defer m.life.RUnlock()
	v.mu.Lock()
	defer v.mu.Unlock()
	if m.h == nil || v.h == nil || v.model != m {
		return GenerationResult{}, errors.New("IndexTTS model or voice is closed or mismatched")
	}
	if options.Language == "" {
		options.Language = "ZH"
	}
	if options.DurationFactor == 0 {
		options.DurationFactor = 1
	}
	if options.EmotionText != "" && !m.capabilities.SupportsEmotionText {
		return GenerationResult{}, errors.New("IndexTTS runtime does not support emotion text")
	}
	if err := ctx.Err(); err != nil {
		return GenerationResult{}, err
	}
	ctext, clang := C.CString(text), C.CString(options.Language)
	defer C.free(unsafe.Pointer(ctext))
	defer C.free(unsafe.Pointer(clang))
	var config C.indextts_generate_options_v2_t
	C.indextts_generate_options_v2_init(&config)
	config.base.text, config.base.language = ctext, clang
	config.base.seed = C.uint64_t(options.Seed)
	config.base.duration_factor = C.float(options.DurationFactor)
	var emotionText *C.char
	if options.EmotionText != "" {
		emotionText = C.CString(options.EmotionText)
		defer C.free(unsafe.Pointer(emotionText))
		config.emotion.mode = C.INDEXTTS_EMOTION_TEXT
		config.emotion.text = emotionText
		strength := options.EmotionStrength
		if strength <= 0 {
			strength = 1
		}
		config.emotion.strength = C.float(strength)
	}
	var request C.indextts_request_t
	if _, err := nativeCall(func() C.int32_t { return C.indextts_request_create(m.h, &request) }); err != nil {
		return GenerationResult{}, err
	}
	defer C.indextts_request_free(request)
	done, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		select {
		case <-ctx.Done():
			_, _ = nativeCall(func() C.int32_t { return C.indextts_request_cancel(request) })
		case <-done:
		}
	}()
	var output C.indextts_generation_result_t
	status, callErr := nativeCall(func() C.int32_t { return C.indextts_generate_result_request_v2(m.h, request, v.h, &config, &output) })
	close(done)
	<-stopped
	if status == C.INDEXTTS_CANCELLED {
		if err := ctx.Err(); err != nil {
			return GenerationResult{}, err
		}
		return GenerationResult{}, errors.New("IndexTTS generation cancelled")
	}
	if status != C.INDEXTTS_OK {
		return GenerationResult{}, callErr
	}
	defer C.indextts_audio_free(&output.audio)
	count := int(output.audio.sample_count)
	if count < 0 || C.size_t(count) != output.audio.sample_count {
		return GenerationResult{}, errors.New("native audio is too large for this process")
	}
	samples := append([]float32(nil), unsafe.Slice((*float32)(unsafe.Pointer(output.audio.samples)), count)...)
	ms := func(x C.float) time.Duration { return time.Duration(float64(x) * float64(time.Millisecond)) }
	return GenerationResult{Audio: Audio{samples, uint32(output.audio.sample_rate), uint32(output.audio.channels)}, Info: GenerationInfo{uint32(output.info.semantic_token_count), float32(output.info.generated_seconds), ms(output.info.reference_encode_ms), ms(output.info.gpt_ms), ms(output.info.semantic_codec_ms), ms(output.info.s2mel_ms), ms(output.info.bigvgan_ms), ms(output.info.total_ms), float32(output.info.peak), float32(output.info.rms), float32(output.info.silence_ratio), uint64(output.info.seed)}}, nil
}

func (m *nativeModel) Cancel() error {
	m.life.RLock()
	defer m.life.RUnlock()
	if m.h == nil {
		return errors.New("IndexTTS model is closed")
	}
	_, err := nativeCall(func() C.int32_t { return C.indextts_model_cancel(m.h) })
	return err
}
func (v *nativeVoice) Close() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.h != nil {
		C.indextts_voice_free(v.h)
		v.h = nil
	}
	return nil
}
func (m *nativeModel) Close() error {
	m.life.Lock()
	defer m.life.Unlock()
	if m.h != nil {
		C.indextts_model_free(m.h)
		m.h = nil
	}
	return nil
}
