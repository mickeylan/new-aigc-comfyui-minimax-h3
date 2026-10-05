from __future__ import annotations

import json
import threading
from pathlib import Path
from typing import Any

import torch

try:
    from .runtime import reference_wav
except ImportError:  # Standalone unit tests.
    from runtime import reference_wav


def canonical_text(value: str) -> str:
    return "".join(character for character in (value or "") if not character.isspace())


def edit_distance(left: str, right: str) -> int:
    a, b = canonical_text(left), canonical_text(right)
    previous = list(range(len(b) + 1))
    for row, char_a in enumerate(a, start=1):
        current = [row]
        for column, char_b in enumerate(b, start=1):
            current.append(
                min(
                    current[-1] + 1,
                    previous[column] + 1,
                    previous[column - 1] + (char_a != char_b),
                )
            )
        previous = current
    return previous[-1]


def build_asr_report(expected: str, transcript: str, max_error_rate: float) -> dict[str, Any]:
    expected_clean = canonical_text(expected)
    transcript_clean = canonical_text(transcript)
    distance = edit_distance(expected_clean, transcript_clean)
    denominator = max(1, len(expected_clean))
    error_rate = distance / denominator
    return {
        "passed": bool(expected_clean) and error_rate <= float(max_error_rate),
        "expected": expected,
        "transcript": transcript,
        "expected_length": len(expected_clean),
        "transcript_length": len(transcript_clean),
        "edit_distance": distance,
        "error_rate": round(error_rate, 6),
        "threshold": float(max_error_rate),
    }


class WhisperCache:
    def __init__(self) -> None:
        self._guard = threading.RLock()
        self._entries: dict[tuple[str, str], tuple[Any, threading.RLock]] = {}

    def acquire(self, model_path: str, device: str):
        path = Path(model_path).expanduser().resolve()
        if not path.is_file():
            raise FileNotFoundError(f"local Whisper model not found: {path}")
        key = (str(path), device)
        with self._guard:
            cached = self._entries.get(key)
            if cached is not None:
                return cached
            try:
                import whisper
            except ImportError as exc:
                raise RuntimeError("openai-whisper is required for ASR review") from exc
            model = whisper.load_model(str(path), device=device, download_root=str(path.parent))
            cached = (model, threading.RLock())
            self._entries[key] = cached
            return cached


WHISPER_CACHE = WhisperCache()


def transcribe_local(audio: dict[str, Any], model_path: str, language: str, device: str) -> str:
    model, lock = WHISPER_CACHE.acquire(model_path, device)
    wav_path = reference_wav(audio)
    with lock:
        result = model.transcribe(
            str(wav_path),
            language=(language or "zh").lower(),
            fp16=device.startswith("cuda"),
            verbose=False,
        )
    transcript = str(result.get("text", "")).strip()
    if not transcript:
        raise RuntimeError("ASR returned an empty transcript")
    return transcript
