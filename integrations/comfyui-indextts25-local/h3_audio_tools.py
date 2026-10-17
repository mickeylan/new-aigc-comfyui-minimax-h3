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
    quality = audio_quality_report(audio)
    if quality["silent"]:
        raise ValueError("reviewed audio is silent")
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
        "audio_quality": quality,
        "warning": "Official audio anchoring conditions generation but does not hard-lock every sampled audio latent.",
    }
    return frames, render_duration, duration, conditioned_prompt, report


def slice_audio_window(
    audio: dict[str, Any], scene_start: float, scene_duration: float,
    warmup: float, cooldown: float, max_duration_seconds: float,
) -> tuple[dict[str, Any], dict[str, Any], float, float, dict[str, Any]]:
    source_duration = audio_duration_seconds(audio)
    start = float(scene_start)
    duration = float(scene_duration) if scene_duration > 0 else source_duration - start
    if start < 0 or duration <= 0 or start + duration > source_duration + 0.001:
        raise ValueError("requested dialogue window is outside the reviewed audio")
    context_start = max(0.0, start - max(0.0, float(warmup)))
    context_end = min(source_duration, start + duration + max(0.0, float(cooldown)))
    context_duration = context_end - context_start
    if context_duration > float(max_duration_seconds) + 0.001:
        raise ValueError(
            f"audio context is {context_duration:.3f}s, above the configured H3 maximum "
            f"{float(max_duration_seconds):.3f}s; reduce warmup/cooldown or split the dialogue"
        )
    waveform = audio["waveform"]
    rate = int(audio["sample_rate"])
    context_first, context_last = round(context_start * rate), round(context_end * rate)
    final_first, final_last = round(start * rate), round((start + duration) * rate)
    context_audio = {"waveform": waveform[..., context_first:context_last].contiguous(), "sample_rate": rate}
    final_audio = {"waveform": waveform[..., final_first:final_last].contiguous(), "sample_rate": rate}
    trim_start = start - context_start
    report = {
        "source_duration_seconds": round(source_duration, 6),
        "scene_start_seconds": round(start, 6),
        "scene_duration_seconds": round(duration, 6),
        "context_start_seconds": round(context_start, 6),
        "context_duration_seconds": round(context_duration, 6),
        "trim_start_seconds": round(trim_start, 6),
        "final_duration_seconds": round(duration, 6),
    }
    return context_audio, final_audio, trim_start, duration, report


def audio_quality_report(audio: dict[str, Any]) -> dict[str, Any]:
    duration = audio_duration_seconds(audio)
    waveform = audio["waveform"].detach().float().cpu()
    peak = float(waveform.abs().max())
    rms = float(torch.sqrt(torch.mean(waveform.square())))
    clipping_ratio = float((waveform.abs() >= 0.999).float().mean())
    silent = rms < 1e-5
    return {
        "duration_seconds": round(duration, 6), "peak": round(peak, 8),
        "rms": round(rms, 8), "clipping_ratio": round(clipping_ratio, 8),
        "silent": silent, "warning": "silent audio" if silent else "clipping detected" if clipping_ratio > 0.001 else "",
    }


def trim_video_frames(frames: torch.Tensor, start_seconds: float, duration_seconds: float, fps: float = FPS) -> torch.Tensor:
    if not isinstance(frames, torch.Tensor) or frames.ndim != 4 or frames.shape[0] == 0:
        raise ValueError("frames must be a non-empty IMAGE batch")
    if fps <= 0 or start_seconds < 0 or duration_seconds <= 0:
        raise ValueError("invalid output trim timing")
    first = max(0, round(start_seconds * fps))
    last = min(int(frames.shape[0]), first + max(1, round(duration_seconds * fps)))
    if first >= last:
        raise ValueError("output trim range contains no video frames")
    return frames[first:last].contiguous()


def mix_audio_policy(source_audio: dict[str, Any], generated_audio: dict[str, Any] | None, policy: str) -> dict[str, Any]:
    mode = (policy or "source_only").strip().lower()
    if mode == "source_only":
        audio_duration_seconds(source_audio)
        return source_audio
    if generated_audio is None:
        raise ValueError(f"audio policy {mode} requires generated_audio")
    audio_duration_seconds(generated_audio)
    if mode == "generated_only":
        return generated_audio
    if mode != "mix":
        raise ValueError(f"unknown audio policy: {policy}")
    rate = max(int(source_audio["sample_rate"]), int(generated_audio["sample_rate"]))
    def normalized(value):
        waveform = value["waveform"].detach().to(device="cpu", dtype=torch.float32)
        if waveform.ndim == 2:
            waveform = waveform.unsqueeze(0)
        source_rate = int(value["sample_rate"])
        if source_rate != rate:
            target_length = max(1, round(waveform.shape[-1] * rate / source_rate))
            shape = waveform.shape
            waveform = torch.nn.functional.interpolate(
                waveform.reshape(shape[0] * shape[1], 1, shape[2]),
                size=target_length, mode="linear", align_corners=False,
            ).reshape(shape[0], shape[1], target_length)
        return waveform
    source = normalized(source_audio)
    generated = normalized(generated_audio)
    channels = max(source.shape[1], generated.shape[1])
    def channels_of(value):
        if value.shape[1] == channels:
            return value
        return value.mean(dim=1, keepdim=True).expand(-1, channels, -1)
    source, generated = channels_of(source), channels_of(generated)
    length = max(source.shape[-1], generated.shape[-1])
    result = torch.zeros((1, channels, length), dtype=torch.float32)
    result[..., :source.shape[-1]] += source
    result[..., :generated.shape[-1]] += generated * 0.25
    peak = float(result.abs().max())
    if peak > 0.98:
        result *= 0.98 / peak
    return {"waveform": result.contiguous(), "sample_rate": rate}


def report_json(report: dict[str, Any]) -> str:
    return json.dumps(report, ensure_ascii=False, sort_keys=True)
