from pathlib import Path

import pytest
import torch

from runtime import EmotionHandle, ModelHandle, VoiceHandle, probe_runtime, report_json, validate_audio, watch_request_cancellation


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
    emotion = EmotionHandle(model.key, "emotion-sha")
    assert emotion.model_key == model.key
    assert emotion.emotion_key == "emotion-sha"


def test_report_json_preserves_chinese():
    assert "悲伤" in report_json({"emotion": "悲伤"})


def test_cancellation_watcher_forwards_comfy_interrupt(monkeypatch):
    import ctypes
    import threading
    import runtime

    class FakeFunction:
        def __init__(self):
            self.requests = []
        def __call__(self, request):
            self.requests.append(request)
            return 0

    class FakeNative:
        class Library:
            indextts_request_cancel = FakeFunction()
        lib = Library()

    monkeypatch.setattr(runtime, "processing_interrupted", lambda: True)
    done = threading.Event()
    request = ctypes.c_void_p(123)
    watch_request_cancellation(FakeNative(), request, done, poll_seconds=0)
    assert FakeNative.lib.indextts_request_cancel.requests == [request]
