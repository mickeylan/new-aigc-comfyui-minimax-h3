from __future__ import annotations

import importlib.metadata as metadata
import sys

from flatten_dict import flatten  # noqa: F401
from google.protobuf import runtime_version  # noqa: F401
from google.protobuf.internal import builder  # noqa: F401
from munch import Munch  # noqa: F401
from whisper.tokenizer import Tokenizer  # noqa: F401

import audiotools  # noqa: F401


MINIMUMS = {
    "protobuf": (5, 29, 6),
    "descript-audiotools": (0, 7, 2),
}


def version_tuple(value: str) -> tuple[int, ...]:
    parts: list[int] = []
    for token in value.split("."):
        digits = "".join(ch for ch in token if ch.isdigit())
        if not digits:
            break
        parts.append(int(digits))
    return tuple(parts)


def main() -> None:
    actual = {name: metadata.version(name) for name in MINIMUMS}
    bad = {
        name: (".".join(map(str, minimum)), actual[name])
        for name, minimum in MINIMUMS.items()
        if version_tuple(actual[name]) < minimum
    }
    if bad:
        raise SystemExit(f"IndexTTS dependency version mismatch: {bad}")
    print("IndexTTS dependency imports are valid")
    print("Python:", sys.executable)
    print("Versions:", actual)


if __name__ == "__main__":
    main()
