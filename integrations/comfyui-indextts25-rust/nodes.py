from __future__ import annotations

import os
from pathlib import Path

from comfy_api.latest import ComfyExtension, IO

from .runtime import (
    RUNTIME_CACHE,
    ModelHandle,
    VoiceHandle,
    probe_runtime,
    report_json,
    resolve_dll,
)

CATEGORY = "Audio/IndexTTS 2.5 Rust"
ModelType = IO.Custom("INDEXTTS25_RUST_MODEL")
VoiceType = IO.Custom("INDEXTTS25_RUST_VOICE")


def _default_dll() -> str:
    return os.environ.get("INDEXTTS_DLL", "")


def _default_model() -> str:
    return os.environ.get("INDEXTTS25_RUST_MODEL_DIR", os.environ.get("INDEXTTS25_MODEL_DIR", ""))


class IndexTTS25RustRuntimeStatus(IO.ComfyNode):
    @classmethod
    def define_schema(cls):
        return IO.Schema(
            node_id="IndexTTS25RustRuntimeStatus",
            display_name="IndexTTS 2.5 Rust Runtime Status",
            category=CATEGORY,
            description="Probes the optional Rust runtime without loading a model or preventing ComfyUI startup.",
            inputs=[IO.String.Input("dll_path", default=_default_dll())],
            outputs=[IO.Boolean.Output("available"), IO.String.Output("capabilities_json")],
        )

    @classmethod
    def execute(cls, dll_path: str):
        report = probe_runtime(dll_path)
        return IO.NodeOutput(bool(report.get("available")), report_json(report))


class IndexTTS25RustModelLoader(IO.ComfyNode):
    @classmethod
    def define_schema(cls):
        return IO.Schema(
            node_id="IndexTTS25RustModelLoader",
            display_name="IndexTTS 2.5 Rust Model",
            category=CATEGORY,
            description="Lazily loads one local Rust/CUDA IndexTTS model and exposes its auditable capabilities.",
            inputs=[
                IO.String.Input("dll_path", default=_default_dll()),
                IO.String.Input("model_dir", default=_default_model()),
                IO.Int.Input("device_index", default=0, min=-1, max=31),
            ],
            outputs=[ModelType.Output("model"), IO.String.Output("runtime_json")],
        )

    @classmethod
    def validate_inputs(cls, dll_path: str, model_dir: str, device_index: int, **kwargs):
        try:
            resolve_dll(dll_path)
            if not Path(model_dir).expanduser().is_dir():
                return f"IndexTTS model directory not found: {model_dir}"
        except Exception as exc:
            return str(exc)
        return True

    @classmethod
    def execute(cls, dll_path: str, model_dir: str, device_index: int):
        handle = ModelHandle(resolve_dll(dll_path), Path(model_dir).expanduser().resolve(), int(device_index))
        entry = RUNTIME_CACHE.acquire_model(handle)
        return IO.NodeOutput(handle, report_json(entry.info))


class IndexTTS25RustPrepareVoice(IO.ComfyNode):
    @classmethod
    def define_schema(cls):
        return IO.Schema(
            node_id="IndexTTS25RustPrepareVoice",
            display_name="IndexTTS 2.5 Rust Prepare Voice",
            category=CATEGORY,
            essentials_category="Audio",
            description="Prepares and caches an authorized reference voice for reuse across dialogue lines.",
            inputs=[
                ModelType.Input("model"),
                IO.Audio.Input("speaker_audio"),
                IO.String.Input("voice_key", default="", tooltip="Stable character or voice ID; audio bytes remain part of the cache identity."),
            ],
            outputs=[VoiceType.Output("voice"), IO.String.Output("voice_json")],
        )

    @classmethod
    def execute(cls, model: ModelHandle, speaker_audio, voice_key: str):
        voice, info = RUNTIME_CACHE.prepare_voice(model, speaker_audio, voice_key)
        return IO.NodeOutput(voice, report_json(info))


class IndexTTS25RustGenerate(IO.ComfyNode):
    @classmethod
    def define_schema(cls):
        return IO.Schema(
            node_id="IndexTTS25RustGenerate",
            display_name="IndexTTS 2.5 Rust Generate",
            category=CATEGORY,
            essentials_category="Audio",
            description="Generates one authoritative dialogue line with deterministic seed and optional native emotion text.",
            inputs=[
                ModelType.Input("model"),
                VoiceType.Input("voice"),
                IO.String.Input("text", multiline=True, default=""),
                IO.Combo.Input("language", options=["ZH", "EN", "JA", "ES", "AR"], default="ZH"),
                IO.Float.Input("duration_factor", default=1.0, min=0.5, max=2.0, step=0.05),
                IO.Int.Input("seed", default=0, min=0, max=0x7FFFFFFFFFFFFFFF, control_after_generate=True),
                IO.String.Input("emotion_text", multiline=True, default=""),
                IO.Float.Input("emotion_strength", default=0.6, min=0.0, max=1.0, step=0.05),
            ],
            outputs=[IO.Audio.Output("audio"), IO.String.Output("generation_json")],
        )

    @classmethod
    def execute(cls, model: ModelHandle, voice: VoiceHandle, text: str, language: str, duration_factor: float, seed: int, emotion_text: str, emotion_strength: float):
        content = text.strip()
        if not content:
            raise ValueError("text is required")
        audio, report = RUNTIME_CACHE.generate(model, voice, content, language, seed, duration_factor, emotion_text.strip(), emotion_strength)
        report["text_sha256"] = __import__("hashlib").sha256(content.encode("utf-8")).hexdigest()
        report["language"] = language
        report["duration_factor"] = float(duration_factor)
        report["emotion_text"] = emotion_text.strip()
        report["emotion_strength"] = float(emotion_strength)
        return IO.NodeOutput(audio, report_json(report))


class IndexTTS25RustExtension(ComfyExtension):
    async def get_node_list(self):
        return [
            IndexTTS25RustRuntimeStatus,
            IndexTTS25RustModelLoader,
            IndexTTS25RustPrepareVoice,
            IndexTTS25RustGenerate,
        ]


async def comfy_entrypoint():
    return IndexTTS25RustExtension()
