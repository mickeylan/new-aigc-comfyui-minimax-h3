from pathlib import Path

import pytest
import torch

from runtime import (
    ModelHandle,
    expected_speech_seconds,
    resolve_precision,
    result_to_audio,
    validate_model_dir,
)


def test_precision_is_safe_without_bf16(monkeypatch):
    monkeypatch.setattr(torch.cuda, "is_bf16_supported", lambda: False)
    assert resolve_precision("auto", "cuda:0") == "float32"
    assert resolve_precision("bfloat16", "cuda:0") == "float32"
    assert resolve_precision("auto", "cpu") == "float32"


def test_result_to_audio_normalizes_pcm16():
    got = result_to_audio((22050, torch.tensor([[0], [32767], [-32768]], dtype=torch.int16).numpy()))
    assert got["sample_rate"] == 22050
    assert tuple(got["waveform"].shape) == (1, 1, 3)
    assert float(got["waveform"].max()) <= 1
    assert float(got["waveform"].min()) >= -1


def test_expected_speech_duration_scales_with_dialogue():
    assert expected_speech_seconds("相信姐姐", "ZH") > 1
    assert expected_speech_seconds("相信姐姐，姐姐无论如何也不会让你去罗刹魔域！", "ZH") > expected_speech_seconds("你好", "ZH")


def test_result_to_audio_rejects_unusable_duration():
    raw = torch.zeros(100, dtype=torch.int16).numpy()
    with pytest.raises(RuntimeError, match="unusably short audio"):
        result_to_audio((22050, raw), minimum_seconds=0.25)


def test_model_validation_names_missing_files(tmp_path: Path):
    with pytest.raises(FileNotFoundError) as exc:
        validate_model_dir(tmp_path)
    assert "config.yaml" in str(exc.value)
    assert "hf_cache/bigvgan/bigvgan_generator.pt" in str(exc.value)


def test_model_handle_cache_key_is_stable(tmp_path: Path):
    handle = ModelHandle(tmp_path / "model", tmp_path / "source", "cpu", "float32")
    assert handle.cache_key == handle.cache_key
