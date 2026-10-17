from __future__ import annotations

import json
import re
from dataclasses import asdict, dataclass
from pathlib import Path

_TIMESTAMP = re.compile(
    r"^(\d{1,2}):(\d{2}):(\d{2})[,.](\d{1,3})\s*-->\s*"
    r"(\d{1,2}):(\d{2}):(\d{2})[,.](\d{1,3})(?:\s+.*)?$"
)
_METADATA_LINE = re.compile(r"^\s*[\[{（(](.*?)[\]}）)]\s*$")
_KEY_ALIASES = {
    "role": "character", "character": "character", "角色": "character",
    "voice": "voice", "音色": "voice", "speed": "speed", "语速": "speed",
    "emotion": "emotion", "感情": "emotion", "情绪": "emotion",
    "delivery": "delivery", "表达": "delivery", "language": "language", "语言": "language",
    "seed": "seed", "种子": "seed",
}


@dataclass(frozen=True, slots=True)
class SubtitleCue:
    index: int
    start: float
    end: float
    text: str
    character: str = ""
    voice: str = ""
    speed: float = 1.0
    emotion: str = ""
    delivery: str = ""
    language: str = ""
    seed: int | None = None


def _seconds(parts: tuple[str, str, str, str]) -> float:
    hours, minutes, seconds, millis = (int(item) for item in parts)
    return hours * 3600 + minutes * 60 + seconds + millis / (10 ** len(parts[3]))


def _metadata(line: str) -> dict[str, object] | None:
    match = _METADATA_LINE.match(line)
    if not match:
        return None
    body = match.group(1).strip()
    if not body:
        return {}
    if line.lstrip().startswith("{"):
        try:
            raw = json.loads(line)
        except json.JSONDecodeError:
            raw = None
        if isinstance(raw, dict):
            return {_KEY_ALIASES.get(str(key).strip().lower(), str(key).strip().lower()): value for key, value in raw.items()}
    result: dict[str, object] = {}
    for item in re.split(r"[;；,，]", body):
        pair = re.split(r"[:：=]", item, maxsplit=1)
        if len(pair) != 2:
            continue
        key = _KEY_ALIASES.get(pair[0].strip().lower())
        if key:
            result[key] = pair[1].strip()
    return result if result else None


def parse_structured_srt(value: str) -> tuple[list[SubtitleCue], list[str]]:
    text = (value or "").lstrip("\ufeff").replace("\r\n", "\n").replace("\r", "\n").strip()
    if not text:
        raise ValueError("SRT content is required")
    cues: list[SubtitleCue] = []
    diagnostics: list[str] = []
    for block_number, block in enumerate(re.split(r"\n\s*\n", text), start=1):
        lines = [line.rstrip() for line in block.split("\n")]
        if not lines:
            continue
        cursor = 0
        try:
            cue_index = int(lines[0].strip())
            cursor = 1
        except ValueError:
            cue_index = block_number
        if cursor >= len(lines):
            diagnostics.append(f"block {block_number}: missing timestamp")
            continue
        timestamp = _TIMESTAMP.match(lines[cursor].strip())
        if not timestamp:
            diagnostics.append(f"block {block_number}: invalid timestamp")
            continue
        cursor += 1
        start = _seconds(timestamp.groups()[:4])
        end = _seconds(timestamp.groups()[4:])
        if end <= start:
            diagnostics.append(f"cue {cue_index}: end must be after start")
            continue
        metadata: dict[str, object] = {}
        if cursor < len(lines):
            parsed = _metadata(lines[cursor])
            if parsed is not None:
                metadata = parsed
                cursor += 1
        content = "\n".join(lines[cursor:]).strip()
        if not content:
            diagnostics.append(f"cue {cue_index}: empty text")
            continue
        try:
            speed = float(metadata.get("speed", 1.0) or 1.0)
        except (TypeError, ValueError):
            diagnostics.append(f"cue {cue_index}: invalid speed")
            speed = 1.0
        seed_value = metadata.get("seed")
        try:
            seed = int(seed_value) if seed_value not in (None, "") else None
        except (TypeError, ValueError):
            diagnostics.append(f"cue {cue_index}: invalid seed")
            seed = None
        cues.append(SubtitleCue(
            index=cue_index, start=start, end=end, text=content,
            character=str(metadata.get("character", "")).strip(),
            voice=str(metadata.get("voice", "")).strip(), speed=speed,
            emotion=str(metadata.get("emotion", "")).strip(),
            delivery=str(metadata.get("delivery", "")).strip(),
            language=str(metadata.get("language", "")).strip().upper(), seed=seed,
        ))
    if not cues:
        raise ValueError("SRT contains no valid cues: " + "; ".join(diagnostics))
    return cues, diagnostics


def read_srt_input(srt_text: str, srt_path: str) -> str:
    inline = (srt_text or "").strip()
    path_text = (srt_path or "").strip()
    if inline and path_text:
        raise ValueError("use either srt_text or srt_path, not both")
    if inline:
        return inline
    if not path_text:
        raise ValueError("srt_text or srt_path is required")
    path = Path(path_text).expanduser().resolve()
    if not path.is_file() or path.suffix.lower() != ".srt":
        raise ValueError(f"SRT file not found: {path}")
    if path.stat().st_size > 2 * 1024 * 1024:
        raise ValueError("SRT file exceeds 2 MiB")
    return path.read_text(encoding="utf-8-sig")


def cue_dict(cue: SubtitleCue) -> dict[str, object]:
    return asdict(cue)
