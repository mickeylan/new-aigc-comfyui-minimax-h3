from __future__ import annotations

import hashlib
import os
import sys
import threading
import wave
from dataclasses import dataclass
from pathlib import Path
from typing import Any

import numpy as np
import torch

SAMPLE_RATE = 22_050
_REQUIRED_MODEL_FILES = (
    "config.yaml",
    "bpe.model",
    "gpt.pth",
    "s2mel.pth",
    "codec.pth",
    "feat1.pt",
    "feat2.pt",
    "wav2vec2bert_stats.pt",
    "multilingual_zh_ja_yue_char_del.tiktoken",
    "pinyin.vocab",
    "hf_cache/w2v-bert-2.0/config.json",
    "hf_cache/w2v-bert-2.0/model.safetensors",
    "hf_cache/w2v-bert-2.0/preprocessor_config.json",
    "hf_cache/campplus_cn_common.bin",
    "hf_cache/bigvgan/config.json",
    "hf_cache/bigvgan/bigvgan_generator.pt",
)


@dataclass(frozen=True, slots=True)
class ModelHandle:
    model_dir: Path
    source_dir: Path
    device: str
    precision: str

    @property
    def cache_key(self) -> tuple[str, str, str, str]:
        return (
            str(self.model_dir.resolve()),
            str(self.source_dir.resolve()),
            self.device,
            self.precision,
        )


def validate_model_dir(model_dir: Path) -> None:
    missing = [name for name in _REQUIRED_MODEL_FILES if not (model_dir / name).is_file()]
    if missing:
        raise FileNotFoundError(
            "IndexTTS 2.5 model directory is incomplete; missing: " + ", ".join(missing)
        )


def validate_source_dir(source_dir: Path) -> None:
    required = source_dir / "indextts" / "infer_v2_5.py"
    if not required.is_file():
        raise FileNotFoundError(f"IndexTTS 2.5 source not found: {required}")


def resolve_device(device: str) -> str:
    value = (device or "auto").strip().lower()
    if value == "auto":
        return "cuda:0" if torch.cuda.is_available() else "cpu"
    if value == "cpu":
        return value
    if value.startswith("cuda"):
        if not torch.cuda.is_available():
            raise RuntimeError("CUDA was requested but is unavailable")
        return value
    raise ValueError(f"Unsupported device: {device}")


def resolve_precision(precision: str, device: str) -> str:
    value = (precision or "auto").strip().lower()
    if value == "auto":
        if device.startswith("cuda") and torch.cuda.is_bf16_supported():
            return "bfloat16"
        return "float32"
    if value not in {"bfloat16", "float32"}:
        raise ValueError(f"Unsupported precision: {precision}")
    if device == "cpu" and value != "float32":
        return "float32"
    if value == "bfloat16" and device.startswith("cuda") and not torch.cuda.is_bf16_supported():
        return "float32"
    return value


def validate_comfy_audio(audio: dict[str, Any]) -> tuple[torch.Tensor, int]:
    if not isinstance(audio, dict) or "waveform" not in audio or "sample_rate" not in audio:
        raise ValueError("speaker_audio must be a ComfyUI AUDIO value")
    waveform = audio["waveform"]
    if not isinstance(waveform, torch.Tensor):
        raise TypeError("speaker_audio.waveform must be a torch.Tensor")
    if waveform.ndim == 2:
        waveform = waveform.unsqueeze(0)
    if waveform.ndim != 3 or waveform.shape[0] != 1 or waveform.shape[-1] == 0:
        raise ValueError(f"speaker_audio waveform must have shape [1,C,T], got {tuple(waveform.shape)}")
    waveform = waveform.detach().to(device="cpu", dtype=torch.float32)
    if not torch.isfinite(waveform).all():
        raise ValueError("speaker_audio contains NaN or Inf")
    sample_rate = int(audio["sample_rate"])
    if sample_rate <= 0:
        raise ValueError("speaker_audio sample_rate must be positive")
    return waveform, sample_rate


def reference_wav(audio: dict[str, Any]) -> Path:
    import folder_paths
    import torchaudio

    waveform, sample_rate = validate_comfy_audio(audio)
    mono = waveform.mean(dim=1)
    seconds = mono.shape[-1] / sample_rate
    if seconds < 0.25:
        raise ValueError("speaker reference must be at least 0.25 seconds")
    mono = mono[..., : int(15 * sample_rate)]
    if sample_rate != SAMPLE_RATE:
        mono = torchaudio.functional.resample(mono, sample_rate, SAMPLE_RATE)
    mono = mono.clamp(-1, 1).contiguous()
    digest = hashlib.sha256(mono.numpy().tobytes()).hexdigest()
    root = Path(folder_paths.get_temp_directory()) / "indextts25_local" / "references"
    root.mkdir(parents=True, exist_ok=True)
    target = root / f"speaker-{digest}.wav"
    if target.is_file():
        return target
    pcm = (mono * 32767).round().to(torch.int16)[0]
    with wave.open(str(target), "wb") as output:
        output.setnchannels(1)
        output.setsampwidth(2)
        output.setframerate(SAMPLE_RATE)
        output.writeframes(pcm.numpy().tobytes())
    return target


def expected_speech_seconds(text: str, language: str, duration_factor: float = 1.0) -> float:
    content = "".join(str(text).split())
    if not content:
        return 0.0
    if str(language).upper() in {"ZH", "JA"}:
        base = len(content) / 3.8
    else:
        words = max(1, len(str(text).split()))
        base = words / 2.5
    return max(0.25, base * max(0.5, min(2.0, float(duration_factor))))


def result_to_audio(result: Any, minimum_seconds: float = 0.0) -> dict[str, Any]:
    if not isinstance(result, tuple) or len(result) != 2:
        raise RuntimeError(f"Unexpected IndexTTS result: {type(result).__name__}")
    sample_rate, raw = result
    tensor = raw.detach().cpu() if isinstance(raw, torch.Tensor) else torch.from_numpy(np.asarray(raw).copy())
    if tensor.ndim == 2:
        tensor = tensor.mean(dim=-1) if tensor.shape[-1] != 1 else tensor[:, 0]
    tensor = tensor.reshape(-1)
    if tensor.dtype in (torch.int8, torch.int16, torch.int32, torch.int64, torch.uint8):
        tensor = tensor.float() / 32768.0
    else:
        tensor = tensor.float()
        if tensor.numel() and float(tensor.abs().max()) > 2:
            tensor = tensor / 32768.0
    tensor = tensor.clamp(-1, 1).view(1, 1, -1).contiguous()
    if not tensor.numel() or not torch.isfinite(tensor).all():
        raise RuntimeError("IndexTTS returned empty or invalid audio")
    rate = int(sample_rate)
    seconds = tensor.shape[-1] / rate
    if minimum_seconds > 0 and seconds < minimum_seconds:
        raise RuntimeError(
            f"IndexTTS generated unusably short audio: {seconds:.3f}s "
            f"({tensor.shape[-1]} samples at {rate}Hz), expected at least {minimum_seconds:.3f}s"
        )
    return {"waveform": tensor, "sample_rate": rate}


@dataclass(slots=True)
class _CacheEntry:
    model: Any
    lock: threading.RLock


class ModelCache:
    def __init__(self) -> None:
        self._guard = threading.RLock()
        self._entries: dict[tuple[str, str, str, str], _CacheEntry] = {}

    def acquire(self, handle: ModelHandle) -> _CacheEntry:
        with self._guard:
            cached = self._entries.get(handle.cache_key)
            if cached is not None:
                return cached
            validate_model_dir(handle.model_dir)
            validate_source_dir(handle.source_dir)
            source = str(handle.source_dir)
            if source not in sys.path:
                sys.path.insert(0, source)
            existing = sys.modules.get("indextts")
            if existing is not None:
                loaded = Path(getattr(existing, "__file__", "")).resolve()
                if handle.source_dir.resolve() not in loaded.parents:
                    raise RuntimeError(f"A different IndexTTS package is already loaded: {loaded}")
            from .transformers_compat import (
                install as install_transformers_compat,
                install_bigvgan_compat,
            )

            install_transformers_compat()
            from indextts.gpt import model_v2
            from indextts.infer_v2_5 import IndexTTS2, bigvgan

            # BigVGAN needs a Hub API signature adapter. GPT generation is
            # already provided by the coordinated IndexTTS compatibility stack.
            install_bigvgan_compat(bigvgan.BigVGAN)
            precision = handle.precision
            model = IndexTTS2(
                cfg_path=str(handle.model_dir / "config.yaml"),
                model_dir=str(handle.model_dir),
                device=handle.device,
                use_bf16=precision == "bfloat16",
                use_qwen_emo=False,
                use_cuda_kernel=False,
                use_deepspeed=False,
                use_accel=False,
                use_torch_compile=False,
            )
            entry = _CacheEntry(model=model, lock=threading.RLock())
            self._entries[handle.cache_key] = entry
            return entry


MODEL_CACHE = ModelCache()
