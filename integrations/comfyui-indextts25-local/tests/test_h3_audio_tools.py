import pytest
import torch

from h3_audio_tools import (
    aligned_h3_frames, audio_quality_report, ensure_primary_audio_binding,
    mix_audio_policy, plan_reviewed_audio_video, production_preflight,
    slice_audio_window, trim_video_frames,
)


def audio(seconds: float, sample_rate: int = 22050):
    return {"waveform": torch.full((1, 1, round(seconds * sample_rate)), 0.1), "sample_rate": sample_rate}


def test_aligned_frames_follow_official_17k_plus_5_grid():
    assert aligned_h3_frames(5.0) == 124
    assert aligned_h3_frames(1.0) % 17 == 5
    assert aligned_h3_frames(14.2) >= 14.2 * 24


def test_plan_uses_reviewed_audio_duration_and_preserves_source_policy():
    frames, render_duration, duration, prompt, report = plan_reviewed_audio_video(
        audio(4.25),
        "subject_definitions:\nA\n\nsummary:\nB\n\nretention_analysis:\nC\n\ndetailed_description:\n[Shot 1] D\n\noverall_soundscape:\nE\n\nnon_diegetic_music:\nF",
        15,
    )
    assert duration == pytest.approx(4.25, abs=1 / 22050)
    assert frames % 17 == 5 and render_duration >= duration
    assert "<Audio 1>" in prompt
    assert report["source_audio_policy"] == "preserve_reviewed_audio"
    assert report["generated_audio_policy"] == "discard"
    assert report["final_trim_duration_seconds"] == pytest.approx(4.25, abs=1 / 22050)


def test_plan_rejects_audio_over_limit_instead_of_truncating():
    with pytest.raises(ValueError, match="split the authoritative dialogue"):
        plan_reviewed_audio_video(audio(15.2), "detailed_description:\n[Shot 1] A", 15)


def test_audio_binding_is_idempotent_and_does_not_modify_dialogue_text():
    prompt = "detailed_description:\n<Subject 1> says: <d>[Chinese] 原文对白</d>."
    once = ensure_primary_audio_binding(prompt)
    twice = ensure_primary_audio_binding(once)
    assert once == twice
    assert once.count("<Audio 1>") == 1
    assert "<d>[Chinese] 原文对白</d>" in once


def test_audio_window_preserves_context_and_exact_final_source_segment():
    source = {"waveform": torch.arange(100, dtype=torch.float32).view(1, 1, -1) / 100, "sample_rate": 10}
    context, final, trim_start, duration, report = slice_audio_window(source, 2, 3, 1, 1, 15)
    assert context["waveform"].shape[-1] == 50
    assert final["waveform"].shape[-1] == 30
    assert trim_start == 1 and duration == 3
    assert report["context_start_seconds"] == 1


def test_output_trim_and_audio_policies_are_deterministic():
    frames = torch.zeros(120, 4, 4, 3)
    assert trim_video_frames(frames, 1, 2, 24).shape[0] == 48
    source, generated = audio(1), audio(1)
    assert mix_audio_policy(source, generated, "source_only") is source
    assert mix_audio_policy(source, generated, "generated_only") is generated
    mixed = mix_audio_policy(source, generated, "mix")
    assert mixed["waveform"].shape == source["waveform"].shape


def test_quality_audit_rejects_silent_reviewed_audio():
    silent = {"waveform": torch.zeros(1, 1, 22050), "sample_rate": 22050}
    assert audio_quality_report(silent)["silent"] is True
    with pytest.raises(ValueError, match="silent"):
        plan_reviewed_audio_video(silent, "detailed_description:\n[Shot 1] A", 15)


def test_production_preflight_validates_tags_geometry_and_reports_risk():
    report = production_preflight("<Picture 1> <Subject 1> <Audio 1>", 832, 480, 124, 1, 1, 11.5)
    assert report["picture_references"] == [1]
    assert report["audio_references"] == [1]
    assert any("below 12 GiB" in warning for warning in report["warnings"])
    with pytest.raises(ValueError, match="unavailable Picture"):
        production_preflight("<Picture 2> <Audio 1>", 832, 480, 124, 1)
    with pytest.raises(ValueError, match="multiples of 32"):
        production_preflight("<Audio 1>", 830, 480, 124, 0)
