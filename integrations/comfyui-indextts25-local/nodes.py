from __future__ import annotations

import hashlib
import json
import os
import time
from pathlib import Path

import torch
from comfy_api.latest import ComfyExtension, IO

from .asr_review import build_asr_report, transcribe_local
from .preset_voices import (
    library_fingerprint,
    load_preset_audio,
    preset_options,
    select_preset_voice,
)
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
            IndexTTS25LocalTimelineMix,
            IndexTTS25LocalASRReview,
        ]


async def comfy_entrypoint():
    return IndexTTS25LocalExtension()
