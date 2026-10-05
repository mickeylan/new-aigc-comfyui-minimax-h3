import json
from pathlib import Path

import pytest

from preset_voices import scan_preset_voices, select_preset_voice


def make_library(tmp_path: Path):
    (tmp_path / "official-demo").mkdir()
    for name in ("voice_01.wav", "voice_02.wav", "voice_03.wav"):
        (tmp_path / "official-demo" / name).write_bytes(b"RIFF-test")
    (tmp_path / "manifest.json").write_text(
        json.dumps(
            {
                "voices": [
                    {
                        "file": "official-demo/voice_01.wav",
                        "label": "官方示例 01",
                        "source": "IndexTeam official demo",
                        "language": "ZH",
                        "tags": ["official-demo", "unclassified"],
                    }
                ]
            },
            ensure_ascii=False,
        ),
        encoding="utf-8",
    )


def test_scan_preset_voices_uses_manifest_and_discovers_other_wavs(tmp_path):
    make_library(tmp_path)
    voices = scan_preset_voices(tmp_path)
    assert [voice.key for voice in voices] == [
        "official-demo/voice_01.wav",
        "official-demo/voice_02.wav",
        "official-demo/voice_03.wav",
    ]
    assert voices[0].label == "官方示例 01"
    assert voices[0].tags == ("official-demo", "unclassified")


def test_random_preset_selection_is_stable_for_seed(tmp_path):
    make_library(tmp_path)
    first = select_preset_voice("", "random_seed", 42, tmp_path)
    second = select_preset_voice("", "random_seed", 42, tmp_path)
    assert first.key == second.key


def test_selected_preset_must_exist(tmp_path):
    make_library(tmp_path)
    with pytest.raises(ValueError, match="not found"):
        select_preset_voice("missing.wav", "selected", 0, tmp_path)
