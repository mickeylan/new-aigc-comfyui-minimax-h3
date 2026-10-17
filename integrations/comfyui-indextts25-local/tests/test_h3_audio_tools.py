import pytest
import torch

from h3_audio_tools import (
    aligned_h3_frames, audio_quality_report, connected_reference_count,
    audio_sha256, delivery_sync_audit,
    ensure_primary_audio_binding, reference_image_fingerprints, tensor_sha256,
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
    source_only = mix_audio_policy(source, generated, "source_only")
    generated_only = mix_audio_policy(source, generated, "generated_only")
    assert torch.equal(source_only["waveform"], source["waveform"])
    assert torch.allclose(generated_only["waveform"], generated["waveform"] * (10 ** (-12 / 20)))
    mixed = mix_audio_policy(source, generated, "mix")
    assert mixed["waveform"].shape == source["waveform"].shape
    assert float(mixed["waveform"].abs().max()) <= 10 ** (-1 / 20) + 1e-6


def test_audio_mix_ducks_generated_track_during_reviewed_speech():
    source = {"waveform": torch.cat([torch.full((1, 1, 100), 0.2), torch.zeros(1, 1, 100)], dim=-1), "sample_rate": 1000}
    generated = {"waveform": torch.full((1, 1, 200), 0.2), "sample_rate": 1000}
    mixed = mix_audio_policy(source, generated, "mix", 0, 0, 0.75, -1, "source")
    speech_region = float(mixed["waveform"][..., 20:80].mean())
    quiet_region = float(mixed["waveform"][..., 130:180].mean())
    assert speech_region > quiet_region
    assert quiet_region == pytest.approx(0.2, abs=0.01)


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
    assert report["free_vram_source"] == "override"
    with pytest.raises(ValueError, match="unavailable Picture"):
        production_preflight("<Picture 2> <Audio 1>", 832, 480, 124, 1)
    with pytest.raises(ValueError, match="multiples of 32"):
        production_preflight("<Audio 1>", 830, 480, 124, 0)


def test_connected_reference_count_uses_actual_non_null_inputs():
    assert connected_reference_count(None) == 0
    assert connected_reference_count({"ref_image_0": torch.zeros(1), "ref_image_1": None}) == 1


def test_delivery_audit_blocks_large_av_delta_and_reports_quality():
    frames = torch.zeros(48, 4, 4, 3)
    report = delivery_sync_audit(frames, audio(2), 24, 1, True)
    assert report["passed"] is True and report["av_delta_ms"] == pytest.approx(0)
    with pytest.raises(ValueError, match="duration delta"):
        delivery_sync_audit(frames, audio(1.5), 24, 50, True)
    warning = delivery_sync_audit(frames, audio(1.5), 24, 50, False, "mix")
    assert warning["passed"] is False
    assert warning["audio_policy"] == "mix"
    assert len(warning["final_frames_sha256"]) == 64
    assert len(warning["final_audio_sha256"]) == 64


def test_content_hashes_are_deterministic_and_reference_ordered():
    first = torch.zeros(1, 2, 2, 3)
    second = torch.ones(1, 2, 2, 3)
    assert tensor_sha256(first) == tensor_sha256(first.clone())
    assert audio_sha256(audio(1)) == audio_sha256(audio(1))
    fingerprints = reference_image_fingerprints({"ref_image_1": second, "ref_image_0": first})
    assert [item["picture"] for item in fingerprints] == [1, 2]
    assert fingerprints[0]["sha256"] == tensor_sha256(first)
    report = production_preflight(
        "<Picture 1> <Picture 2> <Audio 1>", 832, 480, 124, 2,
        memory_snapshot={"available": False, "device": "cpu", "free_gib": 0, "total_gib": 0},
        reference_fingerprints=fingerprints,
    )
    assert report["reference_images"] == fingerprints
    assert len(report["conditioned_prompt_sha256"]) == 64
