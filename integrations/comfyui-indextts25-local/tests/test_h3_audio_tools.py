import pytest
import torch

from h3_audio_tools import aligned_h3_frames, ensure_primary_audio_binding, plan_reviewed_audio_video


def audio(seconds: float, sample_rate: int = 22050):
    return {"waveform": torch.zeros(1, 1, round(seconds * sample_rate)), "sample_rate": sample_rate}


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
