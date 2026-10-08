from __future__ import annotations

import json
import re
import shlex
from dataclasses import dataclass, asdict
from typing import Any

_TIME = re.compile(r"^(\d{1,3}):(\d{2}):(\d{2})[,.](\d{3})\s*-->\s*(\d{1,3}):(\d{2}):(\d{2})[,.](\d{3})$")
_ALLOWED_ROLES = {"dialogue", "offscreen_dialogue", "narration", "monologue"}
_ALIASES = {
    "role": "role", "character": "character", "角色": "character", "voice": "voice", "音色": "voice",
    "speed": "speed", "语速": "speed", "emotion": "emotion", "感情": "emotion", "情绪": "emotion",
    "delivery": "delivery", "表达": "delivery", "language": "language", "语言": "language",
    "seed": "seed", "种子": "seed",
}


@dataclass(slots=True)
class Cue:
    index: int
    start: float
    end: float
    text: str
    role: str = "dialogue"
    character: str = ""
    voice: str = ""
    speed: float = 1.0
    emotion: str = ""
    delivery: str = ""
    language: str = "ZH"
    seed: int = 0

    def json(self) -> dict[str, Any]:
        return asdict(self)


def _seconds(parts: tuple[str, ...]) -> float:
    h, m, s, ms = map(int, parts)
    return h * 3600 + m * 60 + s + ms / 1000


def _metadata(line: str) -> dict[str, Any] | None:
    value = line.strip()
    if value.startswith("{") and value.endswith("}"):
        raw = json.loads(value)
    elif value.startswith("[") and value.endswith("]"):
        body = value[1:-1].strip()
        if body.lower().startswith("voice-studio"):
            body = body[len("voice-studio"):].strip()
        raw = {}
        for token in shlex.split(body):
            if "=" in token:
                key, item = token.split("=", 1)
                raw[key.strip()] = item.strip()
    else:
        return None
    out = {}
    for key, item in raw.items():
        normalized = _ALIASES.get(str(key).strip().lower(), _ALIASES.get(str(key).strip()))
        if normalized:
            out[normalized] = item
    return out


def parse_structured_srt(source: str) -> tuple[list[Cue], list[dict[str, Any]]]:
    text = str(source or "").lstrip("\ufeff").replace("\r\n", "\n").replace("\r", "\n")
    blocks = re.split(r"\n\s*\n", text.strip()) if text.strip() else []
    cues: list[Cue] = []
    diagnostics: list[dict[str, Any]] = []
    previous_start = -1.0
    for block_no, block in enumerate(blocks, 1):
        lines = [line.rstrip() for line in block.split("\n") if line.strip()]
        if not lines:
            continue
        try:
            index = int(lines[0])
        except ValueError:
            diagnostics.append({"block": block_no, "level": "error", "code": "index", "message": "字幕序号无效"})
            continue
        if len(lines) < 3:
            diagnostics.append({"block": block_no, "level": "error", "code": "shape", "message": "字幕缺少时间或正文"})
            continue
        match = _TIME.match(lines[1].strip())
        if not match:
            diagnostics.append({"block": block_no, "level": "error", "code": "time", "message": "时间码无效"})
            continue
        start, end = _seconds(match.groups()[:4]), _seconds(match.groups()[4:])
        if end <= start:
            diagnostics.append({"block": block_no, "level": "error", "code": "range", "message": "结束时间必须晚于开始时间"})
            continue
        meta = _metadata(lines[2])
        content_lines = lines[3:] if meta is not None else lines[2:]
        content = "\n".join(content_lines).strip()
        if not content:
            diagnostics.append({"block": block_no, "level": "error", "code": "text", "message": "字幕正文为空"})
            continue
        meta = meta or {}
        role = str(meta.get("role", "dialogue")).strip().lower()
        if role not in _ALLOWED_ROLES:
            diagnostics.append({"block": block_no, "level": "error", "code": "role", "message": f"不支持的发声类型: {role}"})
            continue
        character = str(meta.get("character", "旁白" if role == "narration" else "")).strip()
        if role != "narration" and not character:
            diagnostics.append({"block": block_no, "level": "error", "code": "character", "message": "对白、画外对白和内心独白必须指定角色"})
            continue
        try:
            speed = float(meta.get("speed", 1.0))
            seed = int(meta.get("seed", 0))
        except (TypeError, ValueError):
            diagnostics.append({"block": block_no, "level": "error", "code": "metadata", "message": "语速或种子无效"})
            continue
        if not 0.5 <= speed <= 2.0:
            diagnostics.append({"block": block_no, "level": "error", "code": "speed", "message": "语速必须为0.5至2.0"})
            continue
        cue = Cue(index, start, end, content, role, character, str(meta.get("voice", "")).strip(), speed, str(meta.get("emotion", "")).strip(), str(meta.get("delivery", "")).strip(), str(meta.get("language", "ZH")).strip().upper() or "ZH", seed)
        if start < previous_start:
            diagnostics.append({"block": block_no, "level": "warning", "code": "order", "message": "字幕起始时间未按顺序排列"})
        if cues and start < cues[-1].end:
            diagnostics.append({"block": block_no, "level": "info", "code": "overlap", "message": "字幕与上一条重叠；时间轴混音将保留重叠"})
        previous_start = start
        cues.append(cue)
    return cues, diagnostics


def preview_report(source: str) -> dict[str, Any]:
    cues, diagnostics = parse_structured_srt(source)
    characters = sorted({cue.character for cue in cues if cue.character})
    return {
        "version": 1,
        "dialogue_authority": "external-read-only-preview",
        "valid": not any(item["level"] == "error" for item in diagnostics),
        "cue_count": len(cues),
        "characters": characters,
        "cues": [cue.json() for cue in cues],
        "diagnostics": diagnostics,
    }
