from __future__ import annotations

import json
from typing import Any, Iterable

import torch


def parse_start_times(value: str, count: int) -> list[float]:
    """Parse a JSON array (preferred) or comma-separated seconds."""
    text = (value or "").strip()
    if not text:
        return [0.0] * count
    try:
        raw = json.loads(text)
    except json.JSONDecodeError:
        raw = [part.strip() for part in text.split(",") if part.strip()]
    if not isinstance(raw, list):
        raise ValueError("start_times must be a JSON array or comma-separated seconds")
    if len(raw) != count:
        raise ValueError(f"start_times has {len(raw)} entries, expected {count}")
    times = [float(item) for item in raw]
    if any(item < 0 for item in times):
        raise ValueError("start_times cannot contain negative values")
    return times


def _normalized_audio(audio: dict[str, Any], target_rate: int) -> torch.Tensor:
    if not isinstance(audio, dict) or "waveform" not in audio or "sample_rate" not in audio:
        raise ValueError("timeline inputs must be ComfyUI AUDIO values")
    waveform = audio["waveform"]
    if not isinstance(waveform, torch.Tensor):
        raise TypeError("audio waveform must be a torch.Tensor")
    if waveform.ndim == 2:
        waveform = waveform.unsqueeze(0)
    if waveform.ndim != 3 or waveform.shape[0] != 1 or waveform.shape[-1] == 0:
        raise ValueError(f"audio waveform must have shape [1,C,T], got {tuple(waveform.shape)}")
    waveform = waveform.detach().to(device="cpu", dtype=torch.float32)
    source_rate = int(audio["sample_rate"])
    if source_rate <= 0:
        raise ValueError("audio sample_rate must be positive")
    if source_rate != target_rate:
        import torchaudio

        waveform = torchaudio.functional.resample(waveform, source_rate, target_rate)
    return waveform


def compose_timeline(
    audios: Iterable[dict[str, Any]], start_times: Iterable[float]
) -> tuple[dict[str, Any], list[dict[str, float | int]]]:
    clips = list(audios)
    starts = [float(item) for item in start_times]
    if not clips:
        raise ValueError("at least one audio clip is required")
    if len(clips) != len(starts):
        raise ValueError("audio and start-time counts differ")
    if any(item < 0 for item in starts):
        raise ValueError("start times cannot be negative")

    sample_rate = max(int(audio["sample_rate"]) for audio in clips)
    normalized = [_normalized_audio(audio, sample_rate) for audio in clips]
    channels = max(int(audio.shape[1]) for audio in normalized)
    ends = [round(start * sample_rate) + int(audio.shape[-1]) for start, audio in zip(starts, normalized)]
    mixed = torch.zeros((1, channels, max(ends)), dtype=torch.float32)
    report: list[dict[str, float | int]] = []

    for index, (start, audio) in enumerate(zip(starts, normalized), start=1):
        if audio.shape[1] == 1 and channels > 1:
            audio = audio.expand(-1, channels, -1)
        elif audio.shape[1] != channels:
            audio = audio.mean(dim=1, keepdim=True).expand(-1, channels, -1)
        first = round(start * sample_rate)
        last = first + audio.shape[-1]
        mixed[..., first:last] += audio
        report.append(
            {
                "index": index,
                "start": round(first / sample_rate, 6),
                "end": round(last / sample_rate, 6),
                "duration": round(audio.shape[-1] / sample_rate, 6),
            }
        )

    peak = float(mixed.abs().max())
    if peak > 1.0:
        mixed /= peak
    return {"waveform": mixed.contiguous(), "sample_rate": sample_rate}, report
