import json

import pytest
import torch

from timeline import compose_timeline, parse_start_times


def audio(values, sample_rate=10):
    return {"waveform": torch.tensor(values, dtype=torch.float32).view(1, 1, -1), "sample_rate": sample_rate}


def test_parse_start_times_accepts_json_and_csv():
    assert parse_start_times("[0, 1.5]", 2) == [0.0, 1.5]
    assert parse_start_times("0, 1.5", 2) == [0.0, 1.5]


def test_parse_start_times_rejects_ambiguous_count():
    with pytest.raises(ValueError, match="expected 2"):
        parse_start_times("[0]", 2)


def test_compose_timeline_places_clips_without_changing_content():
    mixed, report = compose_timeline([audio([0.25, 0.5]), audio([0.75, -0.5])], [0, 0.3])
    assert mixed["sample_rate"] == 10
    assert tuple(mixed["waveform"].shape) == (1, 1, 5)
    assert torch.allclose(mixed["waveform"][0, 0], torch.tensor([0.25, 0.5, 0.0, 0.75, -0.5]))
    assert report == [
        {"index": 1, "start": 0.0, "end": 0.2, "duration": 0.2},
        {"index": 2, "start": 0.3, "end": 0.5, "duration": 0.2},
    ]


def test_compose_timeline_normalizes_overlap_only_when_clipping():
    mixed, _ = compose_timeline([audio([0.8]), audio([0.8])], [0, 0])
    assert float(mixed["waveform"].max()) == pytest.approx(1.0)
