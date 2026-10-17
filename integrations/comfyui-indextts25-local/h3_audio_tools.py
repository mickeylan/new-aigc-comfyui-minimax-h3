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


def cuda_memory_snapshot() -> dict[str, Any]:
    if not torch.cuda.is_available():
        return {"available": False, "device": "cpu", "free_gib": 0.0, "total_gib": 0.0}
    device = torch.cuda.current_device()
    free_bytes, total_bytes = torch.cuda.mem_get_info(device)
    return {
        "available": True, "device": f"cuda:{device}",
        "free_gib": round(free_bytes / (1024 ** 3), 3),
        "total_gib": round(total_bytes / (1024 ** 3), 3),
    }


def connected_reference_count(ref_images: Any) -> int:
    if ref_images is None:
        return 0
    values = ref_images.values() if isinstance(ref_images, dict) else ref_images
    return sum(1 for value in values if value is not None)


def delivery_sync_audit(
    frames: torch.Tensor, audio: dict[str, Any], fps: float,
    max_av_delta_ms: float, fail_on_mismatch: bool,
) -> dict[str, Any]:
    if not isinstance(frames, torch.Tensor) or frames.ndim != 4 or frames.shape[0] <= 0:
        raise ValueError("frames must be a non-empty IMAGE batch")
    if fps <= 0 or max_av_delta_ms < 0:
        raise ValueError("fps must be positive and max_av_delta_ms cannot be negative")
    video_seconds = int(frames.shape[0]) / float(fps)
    audio_seconds = audio_duration_seconds(audio)
    delta_ms = abs(video_seconds - audio_seconds) * 1000.0
    quality = audio_quality_report(audio)
    passed = delta_ms <= float(max_av_delta_ms) and not quality["silent"]
    issues = []
    if delta_ms > float(max_av_delta_ms):
        issues.append(
            f"audio/video duration delta {delta_ms:.1f}ms exceeds {float(max_av_delta_ms):.1f}ms"
        )
    if quality["silent"]:
        issues.append("final audio is silent")
    if quality["clipping_ratio"] > 0.001:
        issues.append("final audio clipping ratio exceeds 0.1%")
    report = {
        "frame_count": int(frames.shape[0]), "fps": float(fps),
        "video_duration_seconds": round(video_seconds, 6),
        "audio_duration_seconds": round(audio_seconds, 6),
        "av_delta_ms": round(delta_ms, 3), "max_av_delta_ms": float(max_av_delta_ms),
        "passed": passed, "issues": issues, "audio_quality": quality,
    }
    if fail_on_mismatch and not passed:
        raise ValueError("; ".join(issues))
    return report


def production_preflight(
    prompt: str, width: int, height: int, frames: int, picture_count: int,
    audio_count: int = 1, free_vram_gb: float = 0.0, memory_snapshot: dict[str, Any] | None = None,
) -> dict[str, Any]:
    if width <= 0 or height <= 0 or width % 32 or height % 32:
        raise ValueError("H3 width and height must be positive multiples of 32")
    if frames < 5 or frames % 17 != 5:
        raise ValueError("H3 frame count must satisfy 17k+5")
    if not 0 <= picture_count <= 9 or not 1 <= audio_count <= 3:
        raise ValueError("H3 supports 0-9 pictures and 1-3 reviewed audio references")
    picture_refs = [int(value) for value in re.findall(r"<(?:Picture|Subject)\s+(\d+)>", prompt, re.IGNORECASE)]
    audio_refs = [int(value) for value in re.findall(r"<Audio\s+(\d+)>", prompt, re.IGNORECASE)]
    unavailable_pictures = sorted({value for value in picture_refs if value < 1 or value > picture_count})
    unavailable_audios = sorted({value for value in audio_refs if value < 1 or value > audio_count})
    if unavailable_pictures or unavailable_audios:
        issues = []
        if unavailable_pictures:
            issues.append("unavailable Picture/Subject " + ",".join(map(str, unavailable_pictures)))
        if unavailable_audios:
            issues.append("unavailable Audio " + ",".join(map(str, unavailable_audios)))
        raise ValueError("; ".join(issues))
    memory = memory_snapshot or cuda_memory_snapshot()
    measured_free = float(memory.get("free_gib", 0.0) or 0.0)
    effective_free = float(free_vram_gb) if free_vram_gb > 0 else measured_free
    megapixels = width * height / 1_000_000
    workload = megapixels * (frames / 124.0) * (1.0 + picture_count * 0.08)
    risk = "high" if workload >= 12 else "elevated" if workload >= 6 else "normal"
    warnings = []
    if frames > 362:
        warnings.append("frame count exceeds the approximate official trained range of 362 frames")
    if effective_free > 0 and effective_free < 12:
        warnings.append("measured/overridden free VRAM is below 12 GiB; queue or lower duration/resolution")
    if risk != "normal":
        warnings.append(f"estimated workload risk is {risk}; this is advisory, not a measured VRAM requirement")
    return {
        "width": width, "height": height, "frame_count": frames,
        "duration_seconds": round(frames / FPS, 6), "picture_count": picture_count,
        "audio_count": audio_count, "picture_references": sorted(set(picture_refs)),
        "audio_references": sorted(set(audio_refs)), "free_vram_gb": effective_free,
        "cuda_memory": memory, "free_vram_source": "override" if free_vram_gb > 0 else "runtime",
        "workload_units": round(workload, 4), "risk": risk, "warnings": warnings,
        "note": "VRAM risk is a deterministic workload heuristic, not a promise of memory consumption.",
    }


def report_json(report: dict[str, Any]) -> str:
    return json.dumps(report, ensure_ascii=False, sort_keys=True)
