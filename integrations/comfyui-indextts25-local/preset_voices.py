from __future__ import annotations

import hashlib
import json
import random
from dataclasses import dataclass
from pathlib import Path
from typing import Any


@dataclass(frozen=True, slots=True)
class PresetVoice:
    key: str
    label: str
    path: Path
    source: str
    language: str = "ZH"
    tags: tuple[str, ...] = ()


def default_library_root() -> Path:
    import folder_paths

    return Path(folder_paths.models_dir) / "TTS" / "IndexTTS-2.5" / "voices"


def _safe_relative(root: Path, value: str) -> Path:
    candidate = (root / value).resolve()
    resolved = root.resolve()
    if candidate == resolved or resolved not in candidate.parents:
        raise ValueError(f"preset voice path escapes library root: {value}")
    return candidate


def scan_preset_voices(root: Path | None = None) -> list[PresetVoice]:
    library = Path(root or default_library_root()).expanduser().resolve()
    if not library.is_dir():
        return []
    manifest_path = library / "manifest.json"
    metadata: dict[str, dict[str, Any]] = {}
    if manifest_path.is_file():
        payload = json.loads(manifest_path.read_text(encoding="utf-8"))
        rows = payload.get("voices", []) if isinstance(payload, dict) else []
        for row in rows:
            if isinstance(row, dict) and str(row.get("file", "")).strip():
                metadata[str(row["file"]).replace("\\", "/")] = row

    result: list[PresetVoice] = []
    for path in sorted(library.rglob("*.wav"), key=lambda item: item.as_posix().casefold()):
        relative = path.relative_to(library).as_posix()
        row = metadata.get(relative, {})
        source = str(row.get("source", "local preset")).strip()
        label = str(row.get("label", path.stem)).strip() or path.stem
        language = str(row.get("language", "ZH")).strip().upper() or "ZH"
        tags = tuple(str(item).strip() for item in row.get("tags", []) if str(item).strip())
        result.append(PresetVoice(relative, label, path, source, language, tags))
    return result


def preset_options(root: Path | None = None) -> list[str]:
    voices = scan_preset_voices(root)
    return [voice.key for voice in voices] or ["NO_PRESET_VOICES_FOUND"]


def select_preset_voice(
    selected: str,
    selection_mode: str,
    seed: int,
    root: Path | None = None,
) -> PresetVoice:
    voices = scan_preset_voices(root)
    if not voices:
        raise FileNotFoundError(
            "No local preset voices found under models/TTS/IndexTTS-2.5/voices"
        )
    if selection_mode == "random_seed":
        return voices[random.Random(int(seed)).randrange(len(voices))]
    for voice in voices:
        if voice.key == selected:
            return voice
    raise ValueError(f"Preset voice not found: {selected}")


def load_preset_audio(voice: PresetVoice) -> dict[str, Any]:
    # Reuse ComfyUI's native LoadAudio decoder. `comfy.audio` does not expose
    # a file loader in ComfyUI 0.38; the canonical implementation lives here.
    from comfy_extras.nodes_audio import load

    waveform, sample_rate = load(str(voice.path))
    if waveform.ndim == 2:
        waveform = waveform.unsqueeze(0)
    return {"waveform": waveform, "sample_rate": int(sample_rate)}


def library_fingerprint(root: Path | None = None) -> str:
    voices = scan_preset_voices(root)
    digest = hashlib.sha256()
    for voice in voices:
        stat = voice.path.stat()
        digest.update(voice.key.encode("utf-8"))
        digest.update(str(stat.st_size).encode("ascii"))
        digest.update(str(stat.st_mtime_ns).encode("ascii"))
    return digest.hexdigest()
