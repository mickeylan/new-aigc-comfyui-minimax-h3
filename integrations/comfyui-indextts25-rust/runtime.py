from __future__ import annotations

import atexit
import ctypes as C
import hashlib
import json
import os
import threading
from dataclasses import dataclass
from pathlib import Path
from typing import Any

import numpy as np
import torch

INDEXTTS_OK = 0
INDEXTTS_EMOTION_NONE = 0
INDEXTTS_EMOTION_TEXT = 1


class ModelOptions(C.Structure):
    _fields_ = [("model_dir", C.c_char_p), ("device_index", C.c_int32), ("precision", C.c_int32), ("reserved", C.c_uint64 * 8)]


class GenerateOptions(C.Structure):
    _fields_ = [
        ("text", C.c_char_p), ("language", C.c_char_p), ("seed", C.c_uint64),
        ("duration_factor", C.c_float), ("do_sample", C.c_int32), ("num_beams", C.c_int32),
        ("temperature", C.c_float), ("top_k", C.c_int32), ("top_p", C.c_float),
        ("repetition_penalty", C.c_float), ("reserved", C.c_uint64 * 4),
    ]


class EmotionOptions(C.Structure):
    _fields_ = [
        ("mode", C.c_int32), ("text", C.c_char_p), ("reference", C.c_void_p),
        ("vector", C.POINTER(C.c_float)), ("vector_length", C.c_size_t),
        ("strength", C.c_float), ("reserved", C.c_uint64 * 4),
    ]


class GenerateOptionsV2(C.Structure):
    _fields_ = [("base", GenerateOptions), ("emotion", EmotionOptions), ("reserved", C.c_uint64 * 4)]


class Capabilities(C.Structure):
    _fields_ = [
        ("abi_major", C.c_uint32), ("abi_minor", C.c_uint32), ("sample_rate", C.c_uint32),
        ("max_reference_seconds", C.c_uint32), ("max_semantic_tokens", C.c_uint32),
        ("max_concurrent_requests_per_model", C.c_uint32), ("supports_cuda", C.c_int32),
        ("supports_cpu", C.c_int32), ("supports_cancellation", C.c_int32),
        ("supports_request_cancellation", C.c_int32), ("supports_voice_cache", C.c_int32),
        ("supports_emotion_text", C.c_int32), ("supports_emotion_reference", C.c_int32),
        ("supports_emotion_vector", C.c_int32), ("supports_target_duration", C.c_int32),
        ("supports_sampling", C.c_int32), ("supports_beam_search", C.c_int32),
        ("reserved", C.c_uint64 * 8),
    ]


class ModelInfo(C.Structure):
    _fields_ = [
        ("runtime_version", C.c_char * 64), ("model_version", C.c_char * 64),
        ("model_manifest_sha256", C.c_char * 65), ("backend", C.c_char * 32),
        ("device", C.c_char * 32), ("reserved", C.c_uint64 * 8),
    ]


class Health(C.Structure):
    _fields_ = [
        ("loaded", C.c_int32), ("device_healthy", C.c_int32),
        ("voice_cache_entries", C.c_uint64), ("voice_cache_bytes", C.c_uint64),
        ("active_requests", C.c_uint64), ("queued_requests", C.c_uint64),
        ("last_error", C.c_char * 512), ("reserved", C.c_uint64 * 8),
    ]


class VoiceInfo(C.Structure):
    _fields_ = [
        ("reference_sha256", C.c_char * 65), ("duration_seconds", C.c_float),
        ("source_sample_rate", C.c_uint32), ("source_channels", C.c_uint32),
        ("cache_bytes", C.c_uint64), ("reserved", C.c_uint64 * 8),
    ]


class AudioOut(C.Structure):
    _fields_ = [
        ("samples", C.POINTER(C.c_float)), ("sample_count", C.c_size_t),
        ("sample_rate", C.c_uint32), ("channels", C.c_uint32),
        ("reserved", C.c_uint64 * 4),
    ]


class GenerationInfo(C.Structure):
    _fields_ = [
        ("semantic_token_count", C.c_uint32), ("generated_seconds", C.c_float),
        ("reference_encode_ms", C.c_float), ("gpt_ms", C.c_float),
        ("semantic_codec_ms", C.c_float), ("s2mel_ms", C.c_float),
        ("bigvgan_ms", C.c_float), ("total_ms", C.c_float), ("peak", C.c_float),
        ("rms", C.c_float), ("silence_ratio", C.c_float), ("seed", C.c_uint64),
        ("reserved", C.c_uint64 * 8),
    ]


class GenerationResult(C.Structure):
    _fields_ = [("audio", AudioOut), ("info", GenerationInfo)]


def _text(raw: Any) -> str:
    value = bytes(raw).split(b"\0", 1)[0]
    return value.decode("utf-8", errors="replace")


def _default_dll_candidates() -> list[Path]:
    here = Path(__file__).resolve().parent
    values = [os.environ.get("INDEXTTS_DLL", ""), str(here / "indextts.dll"), str(here / "bin" / "indextts.dll")]
    return [Path(v).expanduser() for v in values if v]


def resolve_dll(path: str = "") -> Path:
    candidates = [Path(path).expanduser()] if path.strip() else _default_dll_candidates()
    for candidate in candidates:
        if candidate.is_file():
            return candidate.resolve()
    raise FileNotFoundError("indextts.dll not found; set INDEXTTS_DLL or place it in the Rust plugin directory")


class NativeLibrary:
    def __init__(self, path: Path):
        self.path = path
        if os.name == "nt":
            if hasattr(os, "add_dll_directory"):
                os.add_dll_directory(str(path.parent))
            sibling_ort = path.parent / "onnxruntime.dll"
            if sibling_ort.is_file():
                os.environ.setdefault("ORT_DYLIB_PATH", str(sibling_ort))
        self.lib = C.CDLL(str(path))
        self._bind()

    def _bind(self) -> None:
        lib = self.lib
        lib.indextts_last_error.restype = C.c_char_p
        lib.indextts_version.restype = C.c_char_p
        lib.indextts_abi_version.restype = C.c_uint32
        lib.indextts_get_capabilities.argtypes = [C.POINTER(Capabilities)]
        lib.indextts_model_options_init.argtypes = [C.POINTER(ModelOptions)]
        lib.indextts_generate_options_v2_init.argtypes = [C.POINTER(GenerateOptionsV2)]
        lib.indextts_model_load.argtypes = [C.POINTER(ModelOptions), C.POINTER(C.c_void_p)]
        lib.indextts_model_get_info.argtypes = [C.c_void_p, C.POINTER(ModelInfo)]
        lib.indextts_model_health.argtypes = [C.c_void_p, C.POINTER(Health)]
        lib.indextts_voice_prepare_pcm.argtypes = [C.c_void_p, C.POINTER(C.c_float), C.c_size_t, C.c_uint32, C.c_uint32, C.POINTER(C.c_void_p)]
        lib.indextts_voice_get_info.argtypes = [C.c_void_p, C.POINTER(VoiceInfo)]
        lib.indextts_request_create.argtypes = [C.c_void_p, C.POINTER(C.c_void_p)]
        lib.indextts_generate_result_request_v2.argtypes = [C.c_void_p, C.c_void_p, C.c_void_p, C.POINTER(GenerateOptionsV2), C.POINTER(GenerationResult)]
        lib.indextts_audio_free.argtypes = [C.POINTER(AudioOut)]
        lib.indextts_request_free.argtypes = [C.c_void_p]
        lib.indextts_voice_free.argtypes = [C.c_void_p]
        lib.indextts_model_free.argtypes = [C.c_void_p]

    def error(self, operation: str, code: int) -> RuntimeError:
        raw = self.lib.indextts_last_error()
        detail = raw.decode("utf-8", errors="replace") if raw else "unknown native error"
        return RuntimeError(f"{operation} failed ({code}): {detail}")

    def capabilities(self) -> dict[str, Any]:
        value = Capabilities()
        code = self.lib.indextts_get_capabilities(C.byref(value))
        if code != INDEXTTS_OK:
            raise self.error("indextts_get_capabilities", code)
        return {
            "abi_major": value.abi_major, "abi_minor": value.abi_minor,
            "sample_rate": value.sample_rate, "max_reference_seconds": value.max_reference_seconds,
            "max_semantic_tokens": value.max_semantic_tokens,
            "max_concurrent_requests_per_model": value.max_concurrent_requests_per_model,
            "supports_cuda": bool(value.supports_cuda), "supports_cpu": bool(value.supports_cpu),
            "supports_cancellation": bool(value.supports_cancellation),
            "supports_request_cancellation": bool(value.supports_request_cancellation),
            "supports_voice_cache": bool(value.supports_voice_cache),
            "supports_emotion_text": bool(value.supports_emotion_text),
            "supports_emotion_reference": bool(value.supports_emotion_reference),
            "supports_emotion_vector": bool(value.supports_emotion_vector),
            "supports_target_duration": bool(value.supports_target_duration),
            "supports_sampling": bool(value.supports_sampling), "supports_beam_search": bool(value.supports_beam_search),
            "runtime_version": (self.lib.indextts_version() or b"").decode("utf-8", errors="replace"),
            "dll": str(self.path),
        }


@dataclass(frozen=True, slots=True)
class ModelHandle:
    dll_path: Path
    model_dir: Path
    device_index: int

    @property
    def key(self) -> tuple[str, str, int]:
        return str(self.dll_path), str(self.model_dir), self.device_index


@dataclass(frozen=True, slots=True)
class VoiceHandle:
    model_key: tuple[str, str, int]
    voice_key: str


@dataclass(slots=True)
class ModelEntry:
    native: NativeLibrary
    pointer: C.c_void_p
    lock: threading.RLock
    info: dict[str, Any]


@dataclass(slots=True)
class VoiceEntry:
    native: NativeLibrary
    pointer: C.c_void_p
    info: dict[str, Any]


class RuntimeCache:
    def __init__(self):
        self.lock = threading.RLock()
        self.models: dict[tuple[str, str, int], ModelEntry] = {}
        self.voices: dict[tuple[tuple[str, str, int], str], VoiceEntry] = {}

    def acquire_model(self, handle: ModelHandle) -> ModelEntry:
        with self.lock:
            if handle.key in self.models:
                return self.models[handle.key]
            if not handle.model_dir.is_dir():
                raise FileNotFoundError(f"IndexTTS model directory not found: {handle.model_dir}")
            native = NativeLibrary(handle.dll_path)
            options = ModelOptions()
            native.lib.indextts_model_options_init(C.byref(options))
            model_dir = str(handle.model_dir).encode("utf-8")
            options.model_dir = model_dir
            options.device_index = int(handle.device_index)
            pointer = C.c_void_p()
            code = native.lib.indextts_model_load(C.byref(options), C.byref(pointer))
            if code != INDEXTTS_OK or not pointer.value:
                raise native.error("indextts_model_load", code)
            raw_info = ModelInfo()
            code = native.lib.indextts_model_get_info(pointer, C.byref(raw_info))
            if code != INDEXTTS_OK:
                native.lib.indextts_model_free(pointer)
                raise native.error("indextts_model_get_info", code)
            info = native.capabilities() | {
                "model_dir": str(handle.model_dir), "model_version": _text(raw_info.model_version),
                "model_manifest_sha256": _text(raw_info.model_manifest_sha256),
                "backend": _text(raw_info.backend), "device": _text(raw_info.device),
            }
            entry = ModelEntry(native, pointer, threading.RLock(), info)
            self.models[handle.key] = entry
            return entry

    def prepare_voice(self, model: ModelHandle, audio: dict[str, Any], voice_key: str = "") -> tuple[VoiceHandle, dict[str, Any]]:
        entry = self.acquire_model(model)
        waveform, sample_rate = validate_audio(audio)
        mono = waveform.mean(dim=1)[0].numpy().astype(np.float32, copy=False)
        digest = hashlib.sha256(mono.tobytes() + str(sample_rate).encode()).hexdigest()
        stable_key = voice_key.strip() or digest
        cache_key = (model.key, f"{stable_key}:{digest}")
        with self.lock:
            cached = self.voices.get(cache_key)
            if cached:
                return VoiceHandle(model.key, cache_key[1]), cached.info
            pointer = C.c_void_p()
            code = entry.native.lib.indextts_voice_prepare_pcm(entry.pointer, mono.ctypes.data_as(C.POINTER(C.c_float)), mono.size, sample_rate, 1, C.byref(pointer))
            if code != INDEXTTS_OK or not pointer.value:
                raise entry.native.error("indextts_voice_prepare_pcm", code)
            raw = VoiceInfo()
            code = entry.native.lib.indextts_voice_get_info(pointer, C.byref(raw))
            if code != INDEXTTS_OK:
                entry.native.lib.indextts_voice_free(pointer)
                raise entry.native.error("indextts_voice_get_info", code)
            info = {"voice_key": stable_key, "input_sha256": digest, "reference_sha256": _text(raw.reference_sha256), "duration_seconds": raw.duration_seconds, "source_sample_rate": raw.source_sample_rate, "source_channels": raw.source_channels, "cache_bytes": raw.cache_bytes}
            self.voices[cache_key] = VoiceEntry(entry.native, pointer, info)
            return VoiceHandle(model.key, cache_key[1]), info

    def generate(self, model: ModelHandle, voice: VoiceHandle, text: str, language: str, seed: int, duration_factor: float, emotion_text: str, emotion_strength: float) -> tuple[dict[str, Any], dict[str, Any]]:
        model_entry = self.acquire_model(model)
        if voice.model_key != model.key:
            raise ValueError("prepared voice belongs to a different model")
        voice_entry = self.voices.get((model.key, voice.voice_key))
        if not voice_entry:
            raise RuntimeError("prepared voice is unavailable; run IndexTTS25RustPrepareVoice again")
        text_bytes, language_bytes, emotion_bytes = text.encode("utf-8"), language.encode("utf-8"), emotion_text.encode("utf-8")
        options = GenerateOptionsV2()
        model_entry.native.lib.indextts_generate_options_v2_init(C.byref(options))
        options.base.text, options.base.language = text_bytes, language_bytes
        options.base.seed, options.base.duration_factor = int(seed), float(duration_factor)
        if emotion_text.strip():
            if not model_entry.info.get("supports_emotion_text"):
                raise RuntimeError("loaded IndexTTS runtime does not support emotion text")
            options.emotion.mode, options.emotion.text, options.emotion.strength = INDEXTTS_EMOTION_TEXT, emotion_bytes, float(emotion_strength)
        else:
            options.emotion.mode = INDEXTTS_EMOTION_NONE
        request = C.c_void_p()
        result = GenerationResult()
        with model_entry.lock:
            code = model_entry.native.lib.indextts_request_create(model_entry.pointer, C.byref(request))
            if code != INDEXTTS_OK or not request.value:
                raise model_entry.native.error("indextts_request_create", code)
            try:
                code = model_entry.native.lib.indextts_generate_result_request_v2(model_entry.pointer, request, voice_entry.pointer, C.byref(options), C.byref(result))
                if code != INDEXTTS_OK:
                    raise model_entry.native.error("indextts_generate_result_request_v2", code)
                count, channels, rate = int(result.audio.sample_count), int(result.audio.channels), int(result.audio.sample_rate)
                if count <= 0 or channels <= 0 or rate <= 0 or not result.audio.samples:
                    raise RuntimeError("IndexTTS returned empty audio")
                samples = np.ctypeslib.as_array(result.audio.samples, shape=(count,)).copy()
            finally:
                if result.audio.samples:
                    model_entry.native.lib.indextts_audio_free(C.byref(result.audio))
                model_entry.native.lib.indextts_request_free(request)
        if not np.isfinite(samples).all():
            raise RuntimeError("IndexTTS returned NaN or Inf audio")
        frames = samples.size // channels
        waveform = torch.from_numpy(samples[: frames * channels].reshape(frames, channels).T.copy()).unsqueeze(0)
        info = result.info
        report = {
            "semantic_tokens": info.semantic_token_count, "generated_seconds": info.generated_seconds,
            "reference_encode_ms": info.reference_encode_ms, "gpt_ms": info.gpt_ms,
            "semantic_codec_ms": info.semantic_codec_ms, "s2mel_ms": info.s2mel_ms,
            "bigvgan_ms": info.bigvgan_ms, "total_ms": info.total_ms,
            "peak": info.peak, "rms": info.rms, "silence_ratio": info.silence_ratio,
            "seed": int(info.seed), "sample_rate": rate, "channels": channels, "samples": samples.size,
            "model": model_entry.info, "voice": voice_entry.info,
        }
        return {"waveform": waveform.contiguous(), "sample_rate": rate}, report

    def generate_batch(self, model: ModelHandle, voice: VoiceHandle, cues: list[dict[str, Any]], character: str, default_language: str, base_seed: int, emotion_strength: float) -> tuple[dict[str, Any], dict[str, Any]]:
        selected = [cue for cue in cues if not character.strip() or str(cue.get("character", "")).strip() == character.strip()]
        if not selected:
            raise ValueError("no SRT cues match the selected character")
        characters = {str(cue.get("character", "")).strip() for cue in selected}
        if len(characters) > 1:
            raise ValueError("character batch must contain exactly one character voice")
        generated: list[tuple[dict[str, Any], dict[str, Any], dict[str, Any]]] = []
        max_end, rate = 0.0, 0
        for offset, cue in enumerate(selected):
            text = str(cue.get("text", "")).strip()
            emotion = "，".join(value for value in [str(cue.get("emotion", "")).strip(), str(cue.get("delivery", "")).strip()] if value)
            seed = int(cue.get("seed", 0)) or int(base_seed) + offset
            audio, info = self.generate(model, voice, text, str(cue.get("language", default_language)).upper(), seed, 1.0, emotion, emotion_strength)
            generated.append((cue, audio, info))
            rate = int(audio["sample_rate"])
            max_end = max(max_end, float(cue.get("end", 0)), float(cue.get("start", 0)) + audio["waveform"].shape[-1] / rate)
        channels = max(item[1]["waveform"].shape[1] for item in generated)
        timeline = torch.zeros((1, channels, max(1, int(round(max_end * rate)))), dtype=torch.float32)
        reports = []
        for cue, audio, info in generated:
            waveform = audio["waveform"]
            if waveform.shape[1] == 1 and channels > 1:
                waveform = waveform.expand(1, channels, -1)
            start = max(0, int(round(float(cue.get("start", 0)) * rate)))
            end = min(timeline.shape[-1], start + waveform.shape[-1])
            timeline[..., start:end] += waveform[..., : end - start]
            generated_seconds = waveform.shape[-1] / rate
            speed = max(0.5, min(2.0, float(cue.get("speed", 1.0))))
            playback_seconds = generated_seconds / speed
            slot_seconds = float(cue.get("end", 0)) - float(cue.get("start", 0))
            reports.append({
                "index": cue.get("index"), "character": cue.get("character"), "role": cue.get("role"),
                "start": cue.get("start"), "end": cue.get("end"), "slot_seconds": slot_seconds,
                "generated_seconds": generated_seconds, "playback_seconds": playback_seconds,
                "overflow_seconds": max(0.0, playback_seconds - slot_seconds), "speed": speed,
                "seed": info.get("seed"), "semantic_tokens": info.get("semantic_tokens"),
                "status": "overflow" if playback_seconds > slot_seconds + 0.001 else "ok",
            })
        peak = float(timeline.abs().max()) if timeline.numel() else 0.0
        if peak > 1.0:
            timeline /= peak
        report = {"version": 1, "character": next(iter(characters)), "cue_count": len(reports), "sample_rate": rate, "duration": timeline.shape[-1] / rate, "peak_before_normalize": peak, "segments": reports}
        return {"waveform": timeline.contiguous(), "sample_rate": rate}, report

    def close(self) -> None:
        with self.lock:
            for voice in self.voices.values():
                voice.native.lib.indextts_voice_free(voice.pointer)
            self.voices.clear()
            for model in self.models.values():
                model.native.lib.indextts_model_free(model.pointer)
            self.models.clear()


def validate_audio(audio: dict[str, Any]) -> tuple[torch.Tensor, int]:
    if not isinstance(audio, dict) or "waveform" not in audio or "sample_rate" not in audio:
        raise ValueError("speaker_audio must be a ComfyUI AUDIO value")
    waveform = audio["waveform"]
    if not isinstance(waveform, torch.Tensor):
        raise TypeError("speaker_audio.waveform must be a torch.Tensor")
    if waveform.ndim == 2:
        waveform = waveform.unsqueeze(0)
    if waveform.ndim != 3 or waveform.shape[0] != 1 or waveform.shape[-1] == 0:
        raise ValueError(f"speaker_audio waveform must have shape [1,C,T], got {tuple(waveform.shape)}")
    waveform = waveform.detach().to(device="cpu", dtype=torch.float32).clamp(-1, 1).contiguous()
    if not torch.isfinite(waveform).all():
        raise ValueError("speaker_audio contains NaN or Inf")
    sample_rate = int(audio["sample_rate"])
    if sample_rate <= 0:
        raise ValueError("speaker_audio sample_rate must be positive")
    seconds = waveform.shape[-1] / sample_rate
    if seconds < 0.25:
        raise ValueError("speaker reference must be at least 0.25 seconds")
    return waveform, sample_rate


def probe_runtime(dll_path: str = "") -> dict[str, Any]:
    try:
        path = resolve_dll(dll_path)
        report = NativeLibrary(path).capabilities()
        return {"available": True, **report}
    except Exception as exc:
        return {"available": False, "error": str(exc), "dll": dll_path or os.environ.get("INDEXTTS_DLL", "")}


RUNTIME_CACHE = RuntimeCache()
atexit.register(RUNTIME_CACHE.close)


def report_json(value: dict[str, Any]) -> str:
    return json.dumps(value, ensure_ascii=False, sort_keys=True)
