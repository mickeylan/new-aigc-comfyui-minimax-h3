from pathlib import Path

import pytest
import torch

from runtime import ModelHandle, VoiceHandle, probe_runtime, report_json, validate_audio


def test_missing_dll_is_optional_and_reported(tmp_path):
    report = probe_runtime(str(tmp_path / "missing.dll"))
    assert report["available"] is False
    assert "not found" in report["error"]


def test_validate_audio_accepts_standard_comfy_shape():
    waveform, rate = validate_audio({"waveform": torch.zeros(1, 2, 22050), "sample_rate": 22050})
    assert tuple(waveform.shape) == (1, 2, 22050)
    assert rate == 22050


def test_validate_audio_rejects_empty_or_too_short_reference():
    with pytest.raises(ValueError, match="at least"):
        validate_audio({"waveform": torch.zeros(1, 1, 100), "sample_rate": 22050})


def test_handles_keep_model_and_voice_identity(tmp_path):
    model = ModelHandle(tmp_path / "indextts.dll", tmp_path / "model", 2)
    voice = VoiceHandle(model.key, "character-7:sha")
    assert model.key[-1] == 2
    assert voice.model_key == model.key
    assert "character-7" in voice.voice_key


def test_report_json_preserves_chinese():
    assert "悲伤" in report_json({"emotion": "悲伤"})
