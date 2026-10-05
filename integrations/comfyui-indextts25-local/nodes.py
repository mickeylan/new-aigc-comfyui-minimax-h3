from __future__ import annotations

import json
import os
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
            ],
            outputs=[IO.Audio.Output("audio")],
        )

    @classmethod
    def execute(cls, model: ModelHandle, speaker_audio, text: str, language: str, duration_factor: float, seed: int):
        content = text.strip()
        if not content:
            raise ValueError("text is required")
        reference = reference_wav(speaker_audio)
        expected_seconds = expected_speech_seconds(content, language, duration_factor)
        entry = MODEL_CACHE.acquire(model)
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
                # Keep IndexTTS's trained generation defaults. Artificial token
                # floors can create short, fluent-looking but incorrect speech.
                verbose=False,
            )
        minimum_seconds = max(0.25, expected_seconds * 0.35)
        return IO.NodeOutput(result_to_audio(result, minimum_seconds=minimum_seconds))


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
            IndexTTS25LocalTimelineMix,
            IndexTTS25LocalASRReview,
        ]


async def comfy_entrypoint():
    return IndexTTS25LocalExtension()
