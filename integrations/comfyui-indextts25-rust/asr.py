from __future__ import annotations

import hashlib
import threading
import wave
from pathlib import Path
from typing import Any

import torch

try:
    from .runtime import validate_audio
except ImportError:
    from runtime import validate_audio


def canonical_text(value: str) -> str:
    return "".join(character for character in str(value or "") if not character.isspace())


def edit_distance(left: str, right: str) -> int:
    a, b = canonical_text(left), canonical_text(right)
    previous = list(range(len(b) + 1))
    for row, char_a in enumerate(a, 1):
        current = [row]
        for column, char_b in enumerate(b, 1):
            current.append(min(current[-1] + 1, previous[column] + 1, previous[column - 1] + (char_a != char_b)))
        previous = current
    return previous[-1]


def build_asr_report(expected: str, transcript: str, max_error_rate: float) -> dict[str, Any]:
    expected_clean, transcript_clean = canonical_text(expected), canonical_text(transcript)
    distance = edit_distance(expected_clean, transcript_clean)
    error_rate = distance / max(1, len(expected_clean))
    return {"passed": bool(expected_clean) and error_rate <= float(max_error_rate), "expected": expected, "transcript": transcript, "expected_length": len(expected_clean), "transcript_length": len(transcript_clean), "edit_distance": distance, "error_rate": round(error_rate, 6), "threshold": float(max_error_rate)}


def _temporary_wav(audio: dict[str, Any]) -> Path:
    import folder_paths

    waveform, rate = validate_audio(audio)
    mono = waveform.mean(dim=1)[0].clamp(-1, 1)
    digest = hashlib.sha256(mono.numpy().tobytes()).hexdigest()
    root = Path(folder_paths.get_temp_directory()) / "indextts25_rust" / "asr"
    root.mkdir(parents=True, exist_ok=True)
    target = root / f"review-{digest}.wav"
    if not target.is_file():
        pcm = (mono * 32767).round().to(torch.int16).numpy()
        with wave.open(str(target), "wb") as output:
            output.setnchannels(1); output.setsampwidth(2); output.setframerate(rate); output.writeframes(pcm.tobytes())
    return target


class WhisperCache:
    def __init__(self):
        self.lock = threading.RLock()
        self.entries: dict[tuple[str, str], tuple[Any, threading.RLock]] = {}

    def acquire(self, model_path: str, device: str):
        path = Path(model_path).expanduser().resolve()
        if not path.is_file():
            raise FileNotFoundError(f"local Whisper model not found: {path}")
        key = (str(path), device)
        with self.lock:
            if key not in self.entries:
                try:
                    import whisper
                except ImportError as exc:
                    raise RuntimeError("ASR unavailable: openai-whisper is not installed") from exc
                self.entries[key] = (whisper.load_model(str(path), device=device, download_root=str(path.parent)), threading.RLock())
            return self.entries[key]


WHISPER_CACHE = WhisperCache()


def review_audio(audio: dict[str, Any], expected: str, model_path: str, language: str, device: str, threshold: float) -> tuple[str, dict[str, Any]]:
    model, lock = WHISPER_CACHE.acquire(model_path, device)
    with lock:
        result = model.transcribe(str(_temporary_wav(audio)), language=language.lower(), fp16=device.startswith("cuda"), verbose=False)
    transcript = str(result.get("text", "")).strip()
    if not transcript:
        raise RuntimeError("ASR returned an empty transcript")
    return transcript, build_asr_report(expected, transcript, threshold)
