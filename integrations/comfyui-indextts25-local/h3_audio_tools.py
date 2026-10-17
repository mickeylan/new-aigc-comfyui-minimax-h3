from __future__ import annotations

import json
import math
import re
from typing import Any

import torch

FPS = 24.0
ABSOLUTE_MAX_SECONDS = 60.0
_AUDIO_TAG = re.compile(r"<Audio\s+1>", re.IGNORECASE)
_BINDING_SENTENCE = (
    "The approved dialogue track is <Audio 1>; synchronize the visible speaker's mouth "
    "movements and performance timing to it without adding, omitting, or changing speech."
)


def audio_duration_seconds(audio: dict[str, Any]) -> float:
    if not isinstance(audio, dict) or "waveform" not in audio or "sample_rate" not in audio:
        raise ValueError("reviewed_audio must be a ComfyUI AUDIO value")
    waveform = audio["waveform"]
    if not isinstance(waveform, torch.Tensor) or waveform.ndim not in (2, 3) or waveform.shape[-1] <= 0:
        raise ValueError("reviewed_audio waveform must be non-empty [C,T] or [B,C,T]")
    sample_rate = int(audio["sample_rate"])
    if sample_rate <= 0:
        raise ValueError("reviewed_audio sample_rate must be positive")
    return waveform.shape[-1] / sample_rate


def aligned_h3_frames(duration_seconds: float, fps: float = FPS) -> int:
    if duration_seconds <= 0:
        raise ValueError("audio duration must be positive")
    frames = max(5, math.ceil(duration_seconds * fps))
    while frames % 17 != 5:
        frames += 1
    return frames


def ensure_primary_audio_binding(prompt: str) -> str:
    text = (prompt or "").strip()
    if not text:
        raise ValueError("H3 prompt is required")
    if _AUDIO_TAG.search(text):
        return text
    marker = "detailed_description:"
    position = text.find(marker)
    if position < 0:
        return text + "\n\n" + _BINDING_SENTENCE
    line_end = text.find("\n", position + len(marker))
    if line_end < 0:
        return text + "\n" + _BINDING_SENTENCE
    return text[: line_end + 1] + _BINDING_SENTENCE + "\n" + text[line_end + 1 :]


def plan_reviewed_audio_video(audio: dict[str, Any], prompt: str, max_duration_seconds: float) -> tuple[int, float, float, str, dict[str, Any]]:
    duration = audio_duration_seconds(audio)
    maximum = float(max_duration_seconds)
    if maximum < 3 or maximum > ABSOLUTE_MAX_SECONDS:
        raise ValueError(f"max_duration_seconds must be between 3 and {ABSOLUTE_MAX_SECONDS:g}")
    if duration > maximum + 0.001:
        raise ValueError(
            f"reviewed audio is {duration:.3f}s, above the configured H3 maximum {maximum:.3f}s; "
            "split the authoritative dialogue instead of truncating it"
        )
    frames = aligned_h3_frames(duration)
    render_duration = frames / FPS
    conditioned_prompt = ensure_primary_audio_binding(prompt)
    report = {
        "contract": "official_h3_audio_anchor_v1",
        "audio_duration_seconds": round(duration, 6),
        "frame_count": frames,
        "fps": FPS,
        "render_duration_seconds": round(render_duration, 6),
        "final_trim_duration_seconds": round(duration, 6),
        "padding_seconds": round(render_duration - duration, 6),
        "audio_tag": "<Audio 1>",
        "source_audio_policy": "preserve_reviewed_audio",
        "generated_audio_policy": "discard",
        "official_nodes": ["MiniMaxH3ReferenceToVideo", "MiniMaxH3AddGuide"],
        "warning": "Official audio anchoring conditions generation but does not hard-lock every sampled audio latent.",
    }
    return frames, render_duration, duration, conditioned_prompt, report


def report_json(report: dict[str, Any]) -> str:
    return json.dumps(report, ensure_ascii=False, sort_keys=True)
