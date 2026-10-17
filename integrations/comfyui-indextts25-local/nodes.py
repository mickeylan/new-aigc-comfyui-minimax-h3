from __future__ import annotations

import hashlib
import json
import os
import time
from pathlib import Path

import torch
from comfy_api.latest import ComfyExtension, IO

from .asr_review import build_asr_report, transcribe_local
from .h3_audio_tools import (
    audio_quality_report, connected_reference_count, delivery_sync_audit, mix_audio_policy,
    plan_reviewed_audio_video, production_preflight, report_json,
    slice_audio_window, trim_video_frames,
)
from .preset_voices import (
    library_fingerprint,
    load_preset_audio,
    preset_options,
    select_preset_voice,
)
from .subtitle import cue_dict, parse_structured_srt, read_srt_input
from .runtime import (
    MODEL_CACHE,
    ModelHandle,
    expected_speech_seconds,
    reference_wav,
    resolve_device,
    resolve_precision,
    result_to_audio,
    validate_model_dir,
    validate_source_dir,
)
from .timeline import compose_timeline

CATEGORY = "Audio/IndexTTS 2.5 Local"
ModelType = IO.Custom("INDEXTTS25_LOCAL_MODEL")


def _default_source_dir() -> str:
    return os.environ.get("INDEXTTS25_SOURCE_DIR", "")


def _default_model_dir() -> str:
    return os.environ.get("INDEXTTS25_MODEL_DIR", "")


class IndexTTS25LocalModelLoader(IO.ComfyNode):
    @classmethod
    def define_schema(cls):
        return IO.Schema(
            node_id="IndexTTS25LocalModelLoader",
            display_name="IndexTTS 2.5 Local Model",
            category=CATEGORY,
            description="Loads a complete local IndexTTS 2.5 model without downloading anything.",
            inputs=[
                IO.String.Input("source_dir", default=_default_source_dir(), tooltip="Official index-tts source directory containing indextts/infer_v2_5.py."),
                IO.String.Input("model_dir", default=_default_model_dir(), tooltip="Complete local IndexTTS-2.5 checkpoint directory."),
                IO.Combo.Input("device", options=["auto", "cuda:0", "cpu"], default="auto"),
                IO.Combo.Input("precision", options=["auto", "bfloat16", "float32"], default="auto"),
            ],
            outputs=[ModelType.Output("model")],
        )

    @classmethod
    def validate_inputs(cls, source_dir: str, model_dir: str, **kwargs):
        try:
            validate_source_dir(Path(source_dir).expanduser())
            validate_model_dir(Path(model_dir).expanduser())
        except Exception as exc:
            return str(exc)
        return True

    @classmethod
    def execute(cls, source_dir: str, model_dir: str, device: str, precision: str):
        resolved_device = resolve_device(device)
        resolved_precision = resolve_precision(precision, resolved_device)
        handle = ModelHandle(
            source_dir=Path(source_dir).expanduser().resolve(),
            model_dir=Path(model_dir).expanduser().resolve(),
            device=resolved_device,
            precision=resolved_precision,
        )
        validate_source_dir(handle.source_dir)
        validate_model_dir(handle.model_dir)
        return IO.NodeOutput(handle)


class IndexTTS25LocalPresetVoice(IO.ComfyNode):
    @classmethod
    def define_schema(cls):
        options = preset_options()
        return IO.Schema(
            node_id="IndexTTS25LocalPresetVoice",
            display_name="IndexTTS 2.5 Local Preset Voice",
            category=CATEGORY,
            essentials_category="Audio",
            description="Selects an authorized local preset voice or chooses one deterministically from the seed.",
            inputs=[
                IO.Combo.Input("preset_voice", options=options, default=options[0]),
                IO.Combo.Input("selection_mode", options=["selected", "random_seed"], default="selected"),
                IO.Int.Input("seed", default=0, min=0, max=0x7FFFFFFFFFFFFFFF, control_after_generate=True),
                IO.Int.Input("refresh_token", default=0, min=0, max=0x7FFFFFFF, advanced=True),
            ],
            outputs=[
                IO.Audio.Output("speaker_audio"),
                IO.String.Output("voice_key"),
                IO.String.Output("voice_info"),
            ],
        )

    @classmethod
    def fingerprint_inputs(cls, preset_voice: str, selection_mode: str, seed: int, refresh_token: int):
        return f"{library_fingerprint()}:{preset_voice}:{selection_mode}:{seed}:{refresh_token}"

    @classmethod
    def execute(cls, preset_voice: str, selection_mode: str, seed: int, refresh_token: int):
        voice = select_preset_voice(preset_voice, selection_mode, seed)
        info = json.dumps(
            {
                "key": voice.key,
                "label": voice.label,
                "source": voice.source,
                "language": voice.language,
                "tags": list(voice.tags),
            },
            ensure_ascii=False,
        )
        return IO.NodeOutput(load_preset_audio(voice), voice.key, info)


class IndexTTS25LocalGenerate(IO.ComfyNode):
    @classmethod
    def define_schema(cls):
        return IO.Schema(
            node_id="IndexTTS25LocalGenerate",
            display_name="IndexTTS 2.5 Local Generate",
            category=CATEGORY,
            essentials_category="Audio",
            description="Clones one local reference voice and returns standard ComfyUI AUDIO.",
            inputs=[
                ModelType.Input("model"),
                IO.Audio.Input("speaker_audio"),
                IO.String.Input("text", multiline=True, default=""),
                IO.Combo.Input("language", options=["ZH", "EN", "JA", "ES", "AR"], default="ZH"),
                IO.Float.Input("duration_factor", default=1.0, min=0.5, max=2.0, step=0.05),
                IO.Int.Input("seed", default=0, min=0, max=0x7FFFFFFFFFFFFFFF, control_after_generate=True),
                IO.String.Input("emotion_text", multiline=True, default=""),
                IO.Float.Input("emotion_strength", default=0.6, min=0.0, max=1.0, step=0.05),
                IO.Audio.Input("emotion_audio", optional=True),
                IO.String.Input("emotion_vector_json", multiline=True, default="", optional=True),
            ],
            outputs=[IO.Audio.Output("audio"), IO.String.Output("generation_json")],
        )

    @classmethod
    def execute(cls, model: ModelHandle, speaker_audio, text: str, language: str, duration_factor: float, seed: int, emotion_text: str = "", emotion_strength: float = 0.6, emotion_audio=None, emotion_vector_json: str = ""):
        content = text.strip()
        if not content:
            raise ValueError("text is required")
        emotion_vector = None
        if emotion_vector_json.strip():
            emotion_vector = json.loads(emotion_vector_json)
            if not isinstance(emotion_vector, list) or not emotion_vector:
                raise ValueError("emotion_vector_json must be a non-empty JSON array")
            emotion_vector = [float(value) for value in emotion_vector]
        selected_modes = int(bool(emotion_text.strip())) + int(emotion_audio is not None) + int(emotion_vector is not None)
        if selected_modes > 1:
            raise ValueError("emotion text, reference audio and vector are mutually exclusive")
        reference = reference_wav(speaker_audio)
        emotion_reference = reference_wav(emotion_audio) if emotion_audio is not None else None
        expected_seconds = expected_speech_seconds(content, language, duration_factor)
        entry = MODEL_CACHE.acquire(model)
        started = time.perf_counter()
        with entry.lock:
            torch.manual_seed(int(seed))
            if torch.cuda.is_available():
                torch.cuda.manual_seed_all(int(seed))
            result = entry.model.infer(
                spk_audio_prompt=str(reference),
                text=content,
                output_path=None,
                lang=language,
                duration_factor=float(duration_factor),
                emo_audio_prompt=str(emotion_reference) if emotion_reference else None,
                emo_alpha=float(emotion_strength),
                emo_vector=emotion_vector,
                use_emo_text=bool(emotion_text.strip()),
                emo_text=emotion_text.strip() or None,
                verbose=False,
            )
        minimum_seconds = max(0.25, expected_seconds * 0.35)
        audio = result_to_audio(result, minimum_seconds=minimum_seconds)
        samples = int(audio["waveform"].shape[-1])
        rate = int(audio["sample_rate"])
        report = {
            "provider": "index_tts_python_comfyui", "seed": int(seed), "language": language,
            "duration_factor": float(duration_factor), "generated_seconds": samples / rate,
            "sample_rate": rate, "channels": int(audio["waveform"].shape[1]),
            "total_ms": round((time.perf_counter() - started) * 1000, 3),
            "text_sha256": hashlib.sha256(content.encode("utf-8")).hexdigest(),
            "emotion": {"mode": "text" if emotion_text.strip() else "reference" if emotion_reference else "vector" if emotion_vector else "none", "strength": float(emotion_strength)},
        }
        return IO.NodeOutput(audio, json.dumps(report, ensure_ascii=False, sort_keys=True))


class IndexTTS25LocalSaveReport(IO.ComfyNode):
    @classmethod
    def define_schema(cls):
        return IO.Schema(
            node_id="IndexTTS25LocalSaveReport", display_name="IndexTTS 2.5 Save Report", category=CATEGORY,
            inputs=[IO.String.Input("report_json", multiline=True, default="{}"), IO.String.Input("filename_prefix", default="indextts_local/report")],
            is_output_node=True,
        )

    @classmethod
    def execute(cls, report_json: str, filename_prefix: str):
        import re
        import folder_paths
        parsed = json.loads(report_json)
        relative = "/".join(re.sub(r"[^0-9A-Za-z._-]+", "_", part) for part in filename_prefix.replace("\\", "/").strip("/").split("/") if part not in {"", ".", ".."}) or "indextts_local/report"
        root = Path(folder_paths.get_output_directory()).resolve()
        target = (root / f"{relative}.json").resolve()
        if root not in target.parents:
            raise ValueError("report output escaped the ComfyUI output directory")
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(json.dumps(parsed, ensure_ascii=False, sort_keys=True, separators=(",", ":")), encoding="utf-8")
        return IO.NodeOutput(ui={"files": [{"filename": target.name, "subfolder": target.parent.relative_to(root).as_posix(), "type": "output"}]})


class IndexTTS25LocalSRTParse(IO.ComfyNode):
    @classmethod
    def define_schema(cls):
        return IO.Schema(
            node_id="IndexTTS25LocalSRTParse",
            display_name="IndexTTS 2.5 Structured SRT Parse",
            category=CATEGORY,
            description="Parses an UTF-8 SRT file or pasted SRT without synthesizing or rewriting its text.",
            inputs=[
                IO.String.Input("srt_text", multiline=True, default=""),
                IO.String.Input("srt_path", default="", tooltip="Optional local UTF-8 .srt path. Use either path or pasted text."),
            ],
            outputs=[IO.String.Output("cues_json"), IO.String.Output("roles_json"), IO.String.Output("diagnostics_json")],
        )

    @classmethod
    def execute(cls, srt_text: str, srt_path: str):
        cues, diagnostics = parse_structured_srt(read_srt_input(srt_text, srt_path))
        roles = sorted({cue.character for cue in cues if cue.character})
        return IO.NodeOutput(
            json.dumps([cue_dict(cue) for cue in cues], ensure_ascii=False),
            json.dumps(roles, ensure_ascii=False),
            json.dumps(diagnostics, ensure_ascii=False),
        )


class IndexTTS25LocalSRTSingleVoiceGenerate(IO.ComfyNode):
    @classmethod
    def define_schema(cls):
        return IO.Schema(
            node_id="IndexTTS25LocalSRTSingleVoiceGenerate",
            display_name="IndexTTS 2.5 SRT Single Voice Generate",
            category=CATEGORY,
            essentials_category="Audio",
            description="Generates one reviewed character track from SRT and places every cue at its authoritative timestamp. Other roles are skipped.",
            inputs=[
                ModelType.Input("model"), IO.Audio.Input("speaker_audio"),
                IO.String.Input("srt_text", multiline=True, default=""),
                IO.String.Input("srt_path", default=""),
                IO.String.Input("character", default="", tooltip="Required when SRT contains named roles; only exact matches are synthesized."),
                IO.Combo.Input("default_language", options=["ZH", "EN", "JA", "ES", "AR"], default="ZH"),
                IO.Float.Input("duration_factor", default=1.0, min=0.5, max=2.0, step=0.05),
                IO.Int.Input("seed", default=0, min=0, max=0x7FFFFFFFFFFFFFFF, control_after_generate=True),
                IO.Float.Input("emotion_strength", default=0.6, min=0.0, max=1.0, step=0.05),
                IO.Boolean.Input("fail_on_overflow", default=False),
            ],
            outputs=[IO.Audio.Output("timeline_audio"), IO.String.Output("batch_report_json"), IO.String.Output("selected_cues_json")],
        )

    @classmethod
    def execute(cls, model: ModelHandle, speaker_audio, srt_text: str, srt_path: str, character: str,
                default_language: str, duration_factor: float, seed: int, emotion_strength: float,
                fail_on_overflow: bool):
        cues, diagnostics = parse_structured_srt(read_srt_input(srt_text, srt_path))
        requested = character.strip()
        named_roles = sorted({cue.character for cue in cues if cue.character})
        if len(named_roles) > 1 and not requested:
            raise ValueError("SRT contains multiple roles; character is required for a single-voice track")
        if requested:
            selected = [cue for cue in cues if cue.character == requested]
            if not selected:
                raise ValueError(f"character not found in SRT: {requested}")
        else:
            selected = cues
        reference = reference_wav(speaker_audio)
        entry = MODEL_CACHE.acquire(model)
        clips = []
        starts = []
        reports = []
        started = time.perf_counter()
        with entry.lock:
            for position, cue in enumerate(selected):
                cue_seed = int(cue.seed if cue.seed is not None else (int(seed) + position)) & 0x7FFFFFFFFFFFFFFF
                torch.manual_seed(cue_seed)
                if torch.cuda.is_available():
                    torch.cuda.manual_seed_all(cue_seed)
                emotion_text = "，".join(part for part in (cue.emotion, cue.delivery) if part)
                language = cue.language or default_language
                result = entry.model.infer(
                    spk_audio_prompt=str(reference), text=cue.text, output_path=None, lang=language,
                    duration_factor=float(duration_factor), emo_alpha=float(emotion_strength),
                    use_emo_text=bool(emotion_text), emo_text=emotion_text or None, verbose=False,
                )
                minimum = max(0.25, expected_speech_seconds(cue.text, language, duration_factor) * 0.35)
                audio = result_to_audio(result, minimum_seconds=minimum)
                actual = audio["waveform"].shape[-1] / int(audio["sample_rate"])
                slot = cue.end - cue.start
                overflow = max(0.0, actual - slot)
                if fail_on_overflow and overflow > 0.001:
                    raise RuntimeError(f"cue {cue.index} exceeds its SRT slot by {overflow:.3f}s")
                clips.append(audio)
                starts.append(cue.start)
                reports.append({
                    **cue_dict(cue), "seed": cue_seed, "language": language,
                    "generated_seconds": round(actual, 6), "slot_seconds": round(slot, 6),
                    "overflow_seconds": round(overflow, 6), "status": "overflow" if overflow > 0.001 else "ok",
                    "text_sha256": hashlib.sha256(cue.text.encode("utf-8")).hexdigest(),
                })
        mixed, timeline = compose_timeline(clips, starts)
        report = {
            "provider": "index_tts_python_comfyui", "mode": "srt_single_voice",
            "character": requested or (named_roles[0] if len(named_roles) == 1 else ""),
            "cue_count": len(selected), "source_cue_count": len(cues), "diagnostics": diagnostics,
            "timeline": timeline, "cues": reports,
            "sample_rate": mixed["sample_rate"], "channels": int(mixed["waveform"].shape[1]),
            "duration": mixed["waveform"].shape[-1] / mixed["sample_rate"],
            "total_ms": round((time.perf_counter() - started) * 1000, 3),
        }
        return IO.NodeOutput(
            mixed, json.dumps(report, ensure_ascii=False, sort_keys=True),
            json.dumps([cue_dict(cue) for cue in selected], ensure_ascii=False),
        )


class IndexTTS25H3AudioWindow(IO.ComfyNode):
    @classmethod
    def define_schema(cls):
        return IO.Schema(
            node_id="IndexTTS25H3AudioWindow", display_name="IndexTTS → H3 Audio Window",
            category=CATEGORY, essentials_category="Audio",
            description="Slices a reviewed long track into an H3 context window and an exact final source segment without rewriting or truncating speech.",
            inputs=[
                IO.Audio.Input("reviewed_audio"),
                IO.Float.Input("scene_start_seconds", default=0.0, min=0.0, max=86400.0, step=0.01),
                IO.Float.Input("scene_duration_seconds", default=0.0, min=0.0, max=60.0, step=0.01, tooltip="0 uses the remainder of the source track."),
                IO.Float.Input("warmup_seconds", default=0.0, min=0.0, max=10.0, step=0.01),
                IO.Float.Input("cooldown_seconds", default=0.0, min=0.0, max=10.0, step=0.01),
                IO.Float.Input("max_duration_seconds", default=15.0, min=3.0, max=60.0, step=0.1),
            ],
            outputs=[
                IO.Audio.Output("context_audio"), IO.Audio.Output("final_source_audio"),
                IO.Float.Output("trim_start_seconds"), IO.Float.Output("final_duration_seconds"),
                IO.String.Output("window_report_json"),
            ],
        )

    @classmethod
    def execute(cls, reviewed_audio, scene_start_seconds: float, scene_duration_seconds: float,
                warmup_seconds: float, cooldown_seconds: float, max_duration_seconds: float):
        context, final, trim_start, duration, report = slice_audio_window(
            reviewed_audio, scene_start_seconds, scene_duration_seconds,
            warmup_seconds, cooldown_seconds, max_duration_seconds,
        )
        return IO.NodeOutput(context, final, trim_start, duration, report_json(report))


class IndexTTS25H3ReviewedAudioPlan(IO.ComfyNode):
    @classmethod
    def define_schema(cls):
        return IO.Schema(
            node_id="IndexTTS25H3ReviewedAudioPlan",
            display_name="IndexTTS → Official H3 Reviewed Audio Plan",
            category=CATEGORY,
            essentials_category="Audio",
            description="Plans official H3 frame count from reviewed audio, inserts the <Audio 1> binding, and refuses truncation.",
            inputs=[
                IO.Audio.Input("reviewed_audio"),
                IO.String.Input("h3_prompt", multiline=True, default=""),
                IO.Float.Input("max_duration_seconds", default=15.0, min=3.0, max=60.0, step=0.1),
            ],
            outputs=[
                IO.Audio.Output("reviewed_audio"), IO.Int.Output("length"),
                IO.Float.Output("render_duration_seconds"), IO.Float.Output("audio_duration_seconds"),
                IO.String.Output("conditioned_prompt"), IO.String.Output("preflight_report_json"),
            ],
        )

    @classmethod
    def execute(cls, reviewed_audio, h3_prompt: str, max_duration_seconds: float):
        frames, render_duration, audio_duration, prompt, report = plan_reviewed_audio_video(
            reviewed_audio, h3_prompt, max_duration_seconds
        )
        return IO.NodeOutput(
            reviewed_audio, frames, render_duration, audio_duration, prompt, report_json(report)
        )


class IndexTTS25H3ProductionPreflight(IO.ComfyNode):
    @classmethod
    def define_schema(cls):
        return IO.Schema(
            node_id="IndexTTS25H3ProductionPreflight", display_name="IndexTTS → Official H3 Production Preflight",
            category=CATEGORY,
            description="Validates official H3 geometry, 17k+5 frames, media tags, and reports deterministic workload/VRAM risk before sampling.",
            inputs=[
                IO.String.Input("conditioned_prompt", multiline=True, default=""),
                IO.Int.Input("width", default=832, min=32, max=16384, step=32),
                IO.Int.Input("height", default=480, min=32, max=16384, step=32),
                IO.Int.Input("length", default=124, min=5, max=3600),
                IO.Int.Input("audio_count", default=1, min=1, max=3),
                IO.Float.Input("free_vram_override_gb", default=0.0, min=0.0, max=256.0, step=0.1, tooltip="0 reads the active CUDA device at execution time."),
                IO.Autogrow.Input("ref_images", optional=True, template=IO.Autogrow.TemplatePrefix(
                    input=IO.Image.Input("ref_image"), prefix="ref_image_", min=0, max=9)),
            ],
            outputs=[IO.String.Output("conditioned_prompt"), IO.String.Output("production_preflight_json")],
        )

    @classmethod
    def execute(cls, conditioned_prompt: str, width: int, height: int, length: int,
                audio_count: int, free_vram_override_gb: float, ref_images=None):
        picture_count = connected_reference_count(ref_images)
        report = production_preflight(
            conditioned_prompt, int(width), int(height), int(length),
            picture_count, int(audio_count), float(free_vram_override_gb),
        )
        return IO.NodeOutput(conditioned_prompt, report_json(report))


class IndexTTS25H3OutputTrim(IO.ComfyNode):
    @classmethod
    def define_schema(cls):
        return IO.Schema(
            node_id="IndexTTS25H3OutputTrim", display_name="IndexTTS → H3 Exact Output Trim",
            category=CATEGORY,
            description="Trims decoded H3 IMAGE frames to the reviewed dialogue window. Audio is passed separately by policy.",
            inputs=[
                IO.Image.Input("frames"),
                IO.Float.Input("trim_start_seconds", default=0.0, min=0.0, max=60.0, step=0.001),
                IO.Float.Input("final_duration_seconds", default=5.0, min=0.04, max=60.0, step=0.001),
                IO.Float.Input("fps", default=24.0, min=1.0, max=240.0, step=0.001),
            ],
            outputs=[IO.Image.Output("frames"), IO.String.Output("trim_report_json")],
        )

    @classmethod
    def execute(cls, frames, trim_start_seconds: float, final_duration_seconds: float, fps: float):
        trimmed = trim_video_frames(frames, trim_start_seconds, final_duration_seconds, fps)
        report = {"input_frames": int(frames.shape[0]), "output_frames": int(trimmed.shape[0]),
                  "trim_start_seconds": float(trim_start_seconds),
                  "final_duration_seconds": float(final_duration_seconds), "fps": float(fps)}
        return IO.NodeOutput(trimmed, report_json(report))


class IndexTTS25H3AudioPolicy(IO.ComfyNode):
    @classmethod
    def define_schema(cls):
        return IO.Schema(
            node_id="IndexTTS25H3AudioPolicy", display_name="IndexTTS → H3 Final Audio Policy",
            category=CATEGORY, essentials_category="Audio",
            description="Explicitly chooses reviewed source audio, generated H3 audio, or a conservative mix. No silent fallback is allowed.",
            inputs=[
                IO.Audio.Input("source_audio"),
                IO.Combo.Input("policy", options=["source_only", "generated_only", "mix"], default="source_only"),
                IO.Audio.Input("generated_audio", optional=True),
            ],
            outputs=[IO.Audio.Output("final_audio"), IO.String.Output("audio_report_json")],
        )

    @classmethod
    def execute(cls, source_audio, policy: str, generated_audio=None):
        final = mix_audio_policy(source_audio, generated_audio, policy)
        report = {"policy": policy, "source": audio_quality_report(source_audio),
                  "generated": audio_quality_report(generated_audio) if generated_audio is not None else None,
                  "final": audio_quality_report(final)}
        return IO.NodeOutput(final, report_json(report))


class IndexTTS25H3DeliveryAudit(IO.ComfyNode):
    @classmethod
    def define_schema(cls):
        return IO.Schema(
            node_id="IndexTTS25H3DeliveryAudit", display_name="IndexTTS → H3 Final A/V Delivery Audit",
            category=CATEGORY,
            description="Audits final frame/audio duration sync, silence, and clipping before MP4 delivery; optionally blocks mismatched output.",
            inputs=[
                IO.Image.Input("frames"), IO.Audio.Input("final_audio"),
                IO.Float.Input("fps", default=24.0, min=1.0, max=240.0, step=0.001),
                IO.Float.Input("max_av_delta_ms", default=50.0, min=0.0, max=5000.0, step=1.0),
                IO.Boolean.Input("fail_on_mismatch", default=True),
            ],
            outputs=[
                IO.Image.Output("frames"), IO.Audio.Output("final_audio"),
                IO.String.Output("delivery_report_json"), IO.Boolean.Output("passed"),
            ],
        )

    @classmethod
    def execute(cls, frames, final_audio, fps: float, max_av_delta_ms: float, fail_on_mismatch: bool):
        report = delivery_sync_audit(frames, final_audio, fps, max_av_delta_ms, fail_on_mismatch)
        return IO.NodeOutput(frames, final_audio, report_json(report), report["passed"])


class IndexTTS25LocalTimelineMix(IO.ComfyNode):
    @classmethod
    def define_schema(cls):
        return IO.Schema(
            node_id="IndexTTS25LocalTimelineMix",
            display_name="IndexTTS 2.5 Dialogue Timeline Mix",
            category=CATEGORY,
            essentials_category="Audio",
            description="Places a second generated line on a deterministic timeline; chain the node for more lines.",
            inputs=[
                IO.Audio.Input("timeline_audio"),
                IO.Audio.Input("next_line_audio"),
                IO.Float.Input("next_line_start", default=0.0, min=0.0, max=86400.0, step=0.01),
            ],
            outputs=[IO.Audio.Output("audio"), IO.String.Output("timeline_json")],
        )

    @classmethod
    def execute(cls, timeline_audio, next_line_audio, next_line_start: float):
        mixed, report = compose_timeline(
            [timeline_audio, next_line_audio], [0.0, float(next_line_start)]
        )
        return IO.NodeOutput(mixed, json.dumps(report, ensure_ascii=False))


class IndexTTS25LocalASRReview(IO.ComfyNode):
    @classmethod
    def define_schema(cls):
        return IO.Schema(
            node_id="IndexTTS25LocalASRReview",
            display_name="IndexTTS 2.5 ASR Dialogue Review",
            category=CATEGORY,
            essentials_category="Audio",
            description="Transcribes generated speech with a local Whisper checkpoint and reports differences without rewriting the authoritative dialogue.",
            inputs=[
                IO.Audio.Input("audio"),
                IO.String.Input("expected_text", multiline=True, default=""),
                IO.String.Input("whisper_model_path", default="", tooltip="Existing local Whisper .pt checkpoint; no download is attempted."),
                IO.Combo.Input("language", options=["zh", "en", "ja", "es", "ar"], default="zh"),
                IO.Combo.Input("device", options=["cuda", "cpu"], default="cuda"),
                IO.Float.Input("max_error_rate", default=0.05, min=0.0, max=1.0, step=0.01),
            ],
            outputs=[
                IO.String.Output("transcript"),
                IO.String.Output("review_json"),
                IO.Boolean.Output("passed"),
            ],
        )

    @classmethod
    def execute(cls, audio, expected_text: str, whisper_model_path: str, language: str, device: str, max_error_rate: float):
        if not expected_text.strip():
            raise ValueError("expected_text is required")
        transcript = transcribe_local(audio, whisper_model_path, language, device)
        report = build_asr_report(expected_text, transcript, max_error_rate)
        return IO.NodeOutput(transcript, json.dumps(report, ensure_ascii=False), report["passed"])


class IndexTTS25LocalExtension(ComfyExtension):
    async def get_node_list(self):
        return [
            IndexTTS25LocalModelLoader,
            IndexTTS25LocalPresetVoice,
            IndexTTS25LocalGenerate,
            IndexTTS25LocalSaveReport,
            IndexTTS25LocalSRTParse,
            IndexTTS25LocalSRTSingleVoiceGenerate,
            IndexTTS25H3AudioWindow,
            IndexTTS25H3ReviewedAudioPlan,
            IndexTTS25H3ProductionPreflight,
            IndexTTS25H3OutputTrim,
            IndexTTS25H3AudioPolicy,
            IndexTTS25H3DeliveryAudit,
            IndexTTS25LocalTimelineMix,
            IndexTTS25LocalASRReview,
        ]


async def comfy_entrypoint():
    return IndexTTS25LocalExtension()
